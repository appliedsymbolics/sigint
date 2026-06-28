package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/appliedsymbolics/sigint/internal/events"
)

const replayWatermarkName = "hot_retention"

var errReadinessRollback = errors.New("rollback readiness probe")

type GORM struct {
	db *gorm.DB
}

type Event struct {
	ID                 int64  `gorm:"primaryKey;autoIncrement"`
	EventID            string `gorm:"uniqueIndex;not null"`
	EventName          string `gorm:"not null;index:idx_ingest_event_filters,priority:2"`
	EventVersion       string `gorm:"not null"`
	ProducerService    string `gorm:"not null;index:idx_ingest_event_filters,priority:1"`
	ProducerInstance   *string
	ProducerDeployment *string
	OccurredAt         time.Time
	ObservedAt         *time.Time
	PartitionID        *string
	SubjectType        *string `gorm:"index:idx_ingest_event_filters,priority:3"`
	SubjectID          *string `gorm:"index:idx_ingest_event_filters,priority:4"`
	AggregateType      *string `gorm:"index:idx_ingest_event_filters,priority:5"`
	AggregateID        *string `gorm:"index:idx_ingest_event_filters,priority:6"`
	CorrelationID      *string `gorm:"index:idx_ingest_event_filters,priority:7"`
	CausationID        *string
	ActorType          *string
	ActorID            *string
	PayloadSHA256      string `gorm:"not null"`
	EventSHA256        string `gorm:"not null"`
	Status             string `gorm:"not null;index:idx_ingest_event_replay,priority:1"`
	StorageURI         *string
	ReceivedAt         time.Time `gorm:"not null"`
	StoredAt           *time.Time
	LastError          *string
	RawEnvelopeJSON    string `gorm:"not null"`
}

func (Event) TableName() string {
	return "ingest_event"
}

type Attempt struct {
	ID            int64  `gorm:"primaryKey;autoIncrement"`
	EventID       string `gorm:"not null;index"`
	AttemptStatus string `gorm:"not null"`
	Message       *string
	CreatedAt     time.Time `gorm:"not null"`
}

func (Attempt) TableName() string {
	return "ingest_attempt"
}

type ReplayWatermark struct {
	Name      string `gorm:"primaryKey"`
	Cursor    int64  `gorm:"not null"`
	UpdatedAt time.Time
}

func (ReplayWatermark) TableName() string {
	return "replay_watermark"
}

func NewSQLite(path string) (*GORM, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	return &GORM{db: db}, nil
}

func NewPostgres(ctx context.Context, dsn string) (*GORM, error) {
	db, err := gorm.Open(postgres.Open(dsn), gormConfig())
	if err != nil {
		return nil, fmt.Errorf("connect postgres ledger: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres ledger: %w", err)
	}
	return &GORM{db: db}, nil
}

func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

func (g *GORM) Close() error {
	sqlDB, err := g.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (g *GORM) Initialize(ctx context.Context) error {
	return g.db.WithContext(ctx).AutoMigrate(&Event{}, &Attempt{}, &ReplayWatermark{})
}

func (g *GORM) IsReady(ctx context.Context) bool {
	sqlDB, err := g.db.DB()
	if err != nil || sqlDB.PingContext(ctx) != nil {
		return false
	}
	err = g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		attempt := Attempt{
			EventID:       "__readyz__",
			AttemptStatus: "ready",
			CreatedAt:     time.Now().UTC(),
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		return errReadinessRollback
	})
	return errors.Is(err, errReadinessRollback)
}

func (g *GORM) GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error) {
	var event Event
	err := g.db.WithContext(ctx).Where("event_id = ?", eventID).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record := event.toRecord()
	return &record, nil
}

func (g *GORM) InsertReceived(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string) (events.EventRecord, error) {
	now := time.Now().UTC()
	actorType, actorID := actorFields(envelope)
	event := Event{
		EventID:            envelope.NormalizedEventID(),
		EventName:          envelope.EventName,
		EventVersion:       envelope.EventVersion,
		ProducerService:    envelope.ProducerService,
		ProducerInstance:   envelope.ProducerInstance,
		ProducerDeployment: envelope.ProducerDeployment,
		OccurredAt:         envelope.OccurredAt.UTC(),
		ObservedAt:         utcPtr(envelope.ObservedAt),
		PartitionID:        envelope.PartitionID,
		SubjectType:        envelope.SubjectType,
		SubjectID:          envelope.SubjectID,
		AggregateType:      envelope.AggregateType,
		AggregateID:        envelope.AggregateID,
		CorrelationID:      envelope.CorrelationID,
		CausationID:        envelope.CausationID,
		ActorType:          actorType,
		ActorID:            actorID,
		PayloadSHA256:      envelope.PayloadSHA256,
		EventSHA256:        envelope.EventSHA256,
		Status:             "received",
		ReceivedAt:         now,
		RawEnvelopeJSON:    rawEnvelopeJSON,
	}

	err := g.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
	if err != nil {
		return events.EventRecord{}, err
	}
	record, err := g.GetEvent(ctx, envelope.NormalizedEventID())
	if err != nil {
		return events.EventRecord{}, err
	}
	if record == nil {
		return events.EventRecord{}, fmt.Errorf("inserted event not found: %s", envelope.NormalizedEventID())
	}
	return *record, nil
}

func (g *GORM) MarkStored(ctx context.Context, eventID string, storageURI string) (events.EventRecord, error) {
	now := time.Now().UTC()
	updates := map[string]any{
		"status":      "stored",
		"storage_uri": storageURI,
		"stored_at":   now,
		"last_error":  nil,
	}
	if err := g.db.WithContext(ctx).Model(&Event{}).Where("event_id = ?", eventID).Updates(updates).Error; err != nil {
		return events.EventRecord{}, err
	}
	return g.requireEvent(ctx, eventID)
}

func (g *GORM) MarkFailed(ctx context.Context, eventID string, message string) (events.EventRecord, error) {
	if err := g.db.WithContext(ctx).Model(&Event{}).Where("event_id = ?", eventID).Updates(map[string]any{
		"status":     "failed",
		"last_error": message,
	}).Error; err != nil {
		return events.EventRecord{}, err
	}
	return g.requireEvent(ctx, eventID)
}

func (g *GORM) RecordAttempt(ctx context.Context, eventID string, status string, message *string) error {
	return g.db.WithContext(ctx).Create(&Attempt{
		EventID:       eventID,
		AttemptStatus: status,
		Message:       message,
		CreatedAt:     time.Now().UTC(),
	}).Error
}

func (g *GORM) ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error) {
	limit, err := events.NormalizeReplayLimit(query.Limit)
	if err != nil {
		return events.ReplayPage{}, err
	}

	db := g.replayQuery(ctx, query).Limit(limit + 1).Order("id ASC")
	var rows []Event
	if err := db.Find(&rows).Error; err != nil {
		return events.ReplayPage{}, err
	}

	pageRows := rows
	var nextCursor *events.ReplayCursor
	if len(rows) > limit {
		pageRows = rows[:limit]
		if len(pageRows) > 0 {
			cursor := events.ReplayCursor(pageRows[len(pageRows)-1].ID)
			nextCursor = &cursor
		}
	}

	replayEvents := make([]events.ReplayEvent, 0, len(pageRows))
	for _, row := range pageRows {
		replayEvent, err := events.ReplayEventFromRecord(row.toRecord())
		if err != nil {
			return events.ReplayPage{}, err
		}
		replayEvents = append(replayEvents, replayEvent)
	}
	return events.ReplayPage{Events: replayEvents, NextCursor: nextCursor, Limit: limit}, nil
}

func (g *GORM) RetentionCandidates(ctx context.Context, after *events.ReplayCursor, through events.ReplayCursor, limit int) ([]events.EventRecord, error) {
	if limit <= 0 {
		limit = events.MaxReplayLimit
	}
	db := g.db.WithContext(ctx).Where("status = ? AND id <= ?", "stored", int64(through)).Limit(limit).Order("id ASC")
	if after != nil {
		db = db.Where("id > ?", int64(*after))
	}
	var rows []Event
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]events.EventRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, row.toRecord())
	}
	return records, nil
}

func (g *GORM) DeleteStoredEvents(ctx context.Context, cursors []events.ReplayCursor) (int, error) {
	if len(cursors) == 0 {
		return 0, nil
	}
	ids := cursorIDs(cursors)
	result := g.db.WithContext(ctx).Where("status = ? AND id IN ?", "stored", ids).Delete(&Event{})
	return int(result.RowsAffected), result.Error
}

func (g *GORM) SetReplayWatermark(ctx context.Context, cursor events.ReplayCursor) error {
	return g.setReplayWatermark(ctx, g.db.WithContext(ctx), cursor)
}

func (g *GORM) RetainStoredEvents(ctx context.Context, cursors []events.ReplayCursor, watermark events.ReplayCursor) (int, error) {
	if len(cursors) == 0 {
		return 0, nil
	}
	var deleted int
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := g.setReplayWatermark(ctx, tx, watermark); err != nil {
			return err
		}
		result := tx.Where("status = ? AND id IN ?", "stored", cursorIDs(cursors)).Delete(&Event{})
		deleted = int(result.RowsAffected)
		return result.Error
	})
	return deleted, err
}

func (g *GORM) GetReplayWatermark(ctx context.Context) (*events.ReplayCursor, error) {
	var watermark ReplayWatermark
	err := g.db.WithContext(ctx).Where("name = ?", replayWatermarkName).First(&watermark).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cursor := events.ReplayCursor(watermark.Cursor)
	return &cursor, nil
}

func (g *GORM) requireEvent(ctx context.Context, eventID string) (events.EventRecord, error) {
	record, err := g.GetEvent(ctx, eventID)
	if err != nil {
		return events.EventRecord{}, err
	}
	if record == nil {
		return events.EventRecord{}, fmt.Errorf("unknown event_id: %s", eventID)
	}
	return *record, nil
}

func (g *GORM) replayQuery(ctx context.Context, query events.EventQuery) *gorm.DB {
	db := g.db.WithContext(ctx).Where("status = ?", "stored")
	if query.AfterCursor != nil {
		db = db.Where("id > ?", int64(*query.AfterCursor))
	}
	db = addFilter(db, "producer_service", query.ProducerService)
	db = addFilter(db, "event_name", query.EventName)
	db = addFilter(db, "subject_type", query.SubjectType)
	db = addFilter(db, "subject_id", query.SubjectID)
	db = addFilter(db, "aggregate_type", query.AggregateType)
	db = addFilter(db, "aggregate_id", query.AggregateID)
	db = addFilter(db, "correlation_id", query.CorrelationID)
	return db
}

func (g *GORM) setReplayWatermark(_ context.Context, db *gorm.DB, cursor events.ReplayCursor) error {
	watermark := ReplayWatermark{
		Name:      replayWatermarkName,
		Cursor:    int64(cursor),
		UpdatedAt: time.Now().UTC(),
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"cursor", "updated_at"}),
	}).Create(&watermark).Error
}

func (e Event) toRecord() events.EventRecord {
	cursor := events.ReplayCursor(e.ID)
	return events.EventRecord{
		Cursor:             &cursor,
		EventID:            e.EventID,
		EventName:          e.EventName,
		EventVersion:       e.EventVersion,
		ProducerService:    e.ProducerService,
		ProducerInstance:   e.ProducerInstance,
		ProducerDeployment: e.ProducerDeployment,
		OccurredAt:         e.OccurredAt,
		ObservedAt:         e.ObservedAt,
		PartitionID:        e.PartitionID,
		SubjectType:        e.SubjectType,
		SubjectID:          e.SubjectID,
		AggregateType:      e.AggregateType,
		AggregateID:        e.AggregateID,
		CorrelationID:      e.CorrelationID,
		CausationID:        e.CausationID,
		ActorType:          e.ActorType,
		ActorID:            e.ActorID,
		PayloadSHA256:      e.PayloadSHA256,
		EventSHA256:        e.EventSHA256,
		Status:             e.Status,
		StorageURI:         e.StorageURI,
		ReceivedAt:         e.ReceivedAt,
		StoredAt:           e.StoredAt,
		LastError:          e.LastError,
		RawEnvelopeJSON:    e.RawEnvelopeJSON,
	}
}

func addFilter(db *gorm.DB, column string, value *string) *gorm.DB {
	if value == nil {
		return db
	}
	return db.Where(column+" = ?", *value)
}

func actorFields(envelope events.Envelope) (*string, *string) {
	if envelope.Actor == nil {
		return nil, nil
	}
	return &envelope.Actor.Type, envelope.Actor.ID
}

func cursorIDs(cursors []events.ReplayCursor) []int64 {
	ids := make([]int64, 0, len(cursors))
	for _, cursor := range cursors {
		ids = append(ids, int64(cursor))
	}
	return ids
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows)
}
