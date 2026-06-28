package ingest

import (
	"context"
	"errors"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/events"
	eventstorage "github.com/appliedsymbolics/sigint/internal/storage"
)

type Ledger interface {
	Initialize(ctx context.Context) error
	IsReady(ctx context.Context) bool
	GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error)
	InsertReceived(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string) (events.EventRecord, error)
	MarkStored(ctx context.Context, eventID string, storageURI string) (events.EventRecord, error)
	MarkFailed(ctx context.Context, eventID string, message string) (events.EventRecord, error)
	RecordAttempt(ctx context.Context, eventID string, status string, message *string) error
	ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error)
}

type Storage interface {
	IsReady() bool
	WriteRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error)
}

type archiveFirstStorage interface {
	ArchiveFirst() bool
}

type archiveConfirmingStorage interface {
	ConfirmRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error)
}

type retentionLedger interface {
	GetReplayWatermark(ctx context.Context) (*events.ReplayCursor, error)
	SetReplayWatermark(ctx context.Context, cursor events.ReplayCursor) error
	RetentionCandidates(ctx context.Context, after *events.ReplayCursor, through events.ReplayCursor, limit int) ([]events.EventRecord, error)
	DeleteStoredEvents(ctx context.Context, cursors []events.ReplayCursor) (int, error)
	RetainStoredEvents(ctx context.Context, cursors []events.ReplayCursor, watermark events.ReplayCursor) (int, error)
}

type Service struct {
	ledger  Ledger
	storage Storage
	config  config.Ingest
}

func NewService(ledger Ledger, storage Storage, cfg config.Ingest) *Service {
	return &Service{ledger: ledger, storage: storage, config: cfg}
}

func (s *Service) Initialize(ctx context.Context) error {
	return s.ledger.Initialize(ctx)
}

func (s *Service) LedgerReady(ctx context.Context) bool {
	return s.ledger.IsReady(ctx)
}

func (s *Service) StorageReady() bool {
	return s.storage.IsReady()
}

func (s *Service) GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error) {
	return s.ledger.GetEvent(ctx, eventID)
}

func (s *Service) ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error) {
	if ledger, ok := s.ledger.(retentionLedger); ok {
		floor, err := ledger.GetReplayWatermark(ctx)
		if err != nil {
			return events.ReplayPage{}, err
		}
		if floor != nil {
			requested := events.ReplayCursor(0)
			if query.AfterCursor != nil {
				requested = *query.AfterCursor
			}
			if requested < *floor {
				return events.ReplayPage{}, events.ReplayWindowExpiredError{Requested: requested, Floor: *floor}
			}
		}
	}
	return s.ledger.ReplayEvents(ctx, query)
}

func (s *Service) Ingest(ctx context.Context, envelope events.Envelope) (events.IngestResult, error) {
	if err := s.validateHashes(envelope); err != nil {
		return events.IngestResult{}, err
	}
	rawEnvelopeJSON, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		return events.IngestResult{}, err
	}
	eventID := envelope.NormalizedEventID()

	existing, err := s.ledger.GetEvent(ctx, eventID)
	if err != nil {
		return events.IngestResult{}, err
	}
	if existing != nil {
		return s.handleExisting(ctx, *existing, envelope, rawEnvelopeJSON)
	}
	if s.archiveFirst() {
		return s.storeInsertAndMark(ctx, envelope, rawEnvelopeJSON)
	}

	record, err := s.ledger.InsertReceived(ctx, envelope, rawEnvelopeJSON)
	if err != nil {
		return events.IngestResult{}, err
	}
	if record.EventSHA256 != envelope.EventSHA256 {
		return s.handleExisting(ctx, record, envelope, rawEnvelopeJSON)
	}
	return s.storeAndMark(ctx, envelope, rawEnvelopeJSON)
}

func (s *Service) validateHashes(envelope events.Envelope) error {
	if s.config.RequirePayloadHash {
		actual, err := events.SHA256JSON(envelope.CanonicalPayload())
		if err != nil {
			return HashValidationError{Message: err.Error()}
		}
		if actual != envelope.PayloadSHA256 {
			return HashValidationError{Message: "payload_sha256 does not match canonical payload JSON"}
		}
	}
	if s.config.RequireEventHash {
		actual, err := events.SHA256JSON(envelope.CanonicalEventWithoutEventHash())
		if err != nil {
			return HashValidationError{Message: err.Error()}
		}
		if actual != envelope.EventSHA256 {
			return HashValidationError{Message: "event_sha256 does not match canonical event JSON without event_sha256"}
		}
	}
	return nil
}

func (s *Service) archiveFirst() bool {
	storage, ok := s.storage.(archiveFirstStorage)
	return ok && storage.ArchiveFirst()
}

func (s *Service) handleExisting(ctx context.Context, existing events.EventRecord, envelope events.Envelope, rawEnvelopeJSON string) (events.IngestResult, error) {
	eventID := envelope.NormalizedEventID()
	if existing.EventSHA256 != envelope.EventSHA256 || existing.PayloadSHA256 != envelope.PayloadSHA256 {
		message := "event_id already exists with different content hash"
		_ = s.ledger.RecordAttempt(ctx, eventID, "hash_conflict", &message)
		if s.config.RejectHashConflicts {
			return events.IngestResult{}, HashConflictError{Message: message}
		}
		return events.IngestResult{
			EventID:    eventID,
			Status:     "hash_conflict",
			StorageURI: existing.StorageURI,
			ReceivedAt: existing.ReceivedAt,
			StoredAt:   existing.StoredAt,
			Message:    &message,
		}, nil
	}

	if existing.Status == "stored" && existing.StorageURI != nil {
		_ = s.ledger.RecordAttempt(ctx, eventID, "duplicate", nil)
		return events.IngestResult{
			EventID:    eventID,
			Status:     "duplicate",
			StorageURI: existing.StorageURI,
			ReceivedAt: existing.ReceivedAt,
			StoredAt:   existing.StoredAt,
		}, nil
	}

	return s.storeAndMark(ctx, envelope, rawEnvelopeJSON)
}

func (s *Service) storeInsertAndMark(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string) (events.IngestResult, error) {
	eventID := envelope.NormalizedEventID()
	storageURI, err := s.writeRawEvent(ctx, envelope, rawEnvelopeJSON, false)
	if err != nil {
		return events.IngestResult{}, err
	}

	record, err := s.ledger.InsertReceived(ctx, envelope, rawEnvelopeJSON)
	if err != nil {
		message := "insert received: " + err.Error()
		_ = s.ledger.RecordAttempt(ctx, eventID, "failed", &message)
		return events.IngestResult{}, LedgerError{Message: message}
	}
	if record.EventSHA256 != envelope.EventSHA256 || record.PayloadSHA256 != envelope.PayloadSHA256 {
		return s.handleExisting(ctx, record, envelope, rawEnvelopeJSON)
	}
	if record.Status == "stored" && record.StorageURI != nil {
		_ = s.ledger.RecordAttempt(ctx, eventID, "duplicate", nil)
		return events.IngestResult{
			EventID:    eventID,
			Status:     "duplicate",
			StorageURI: record.StorageURI,
			ReceivedAt: record.ReceivedAt,
			StoredAt:   record.StoredAt,
		}, nil
	}
	return s.markStored(ctx, eventID, storageURI)
}

func (s *Service) storeAndMark(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string) (events.IngestResult, error) {
	eventID := envelope.NormalizedEventID()
	storageURI, err := s.writeRawEvent(ctx, envelope, rawEnvelopeJSON, true)
	if err != nil {
		return events.IngestResult{}, err
	}
	return s.markStored(ctx, eventID, storageURI)
}

func (s *Service) writeRawEvent(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string, markFailed bool) (string, error) {
	eventID := envelope.NormalizedEventID()
	storageURI, err := s.storage.WriteRawEvent(envelope, rawEnvelopeJSON)
	if err == nil {
		return storageURI, nil
	}
	message := err.Error()
	var storageConflict eventstorage.ConflictError
	if errors.As(err, &storageConflict) {
		_ = s.ledger.RecordAttempt(ctx, eventID, "hash_conflict", &message)
		return "", HashConflictError{Message: message}
	}
	_ = s.ledger.RecordAttempt(ctx, eventID, "failed", &message)
	if markFailed {
		_, _ = s.ledger.MarkFailed(ctx, eventID, message)
	}
	return "", StorageError{Message: message}
}

func (s *Service) markStored(ctx context.Context, eventID string, storageURI string) (events.IngestResult, error) {
	stored, err := s.ledger.MarkStored(ctx, eventID, storageURI)
	if err != nil {
		message := "mark stored: " + err.Error()
		_ = s.ledger.RecordAttempt(ctx, eventID, "failed", &message)
		return events.IngestResult{}, LedgerError{Message: message}
	}
	_ = s.ledger.RecordAttempt(ctx, eventID, "stored", nil)
	return events.IngestResult{
		EventID:    stored.EventID,
		Status:     "stored",
		StorageURI: stored.StorageURI,
		ReceivedAt: stored.ReceivedAt,
		StoredAt:   stored.StoredAt,
	}, nil
}
