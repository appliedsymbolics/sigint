package events

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultReplayLimit = 100
	MaxReplayLimit     = 1000
)

type ReplayCursor int64

func ParseReplayCursor(value string) (ReplayCursor, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("after_cursor is required")
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("after_cursor must be a base-10 integer: %w", err)
	}
	if parsed < 0 {
		return 0, errors.New("after_cursor must be greater than or equal to 0")
	}
	return ReplayCursor(parsed), nil
}

func (c ReplayCursor) String() string {
	return strconv.FormatInt(int64(c), 10)
}

type EventQuery struct {
	AfterCursor     *ReplayCursor
	Limit           int
	ProducerService *string
	EventName       *string
	SubjectType     *string
	SubjectID       *string
	AggregateType   *string
	AggregateID     *string
	CorrelationID   *string
}

func NewEventQuery(afterCursor string, limit int) (EventQuery, error) {
	return NewEventQueryWithLimits(afterCursor, limit, DefaultReplayLimit, MaxReplayLimit)
}

func NewEventQueryWithLimits(afterCursor string, limit int, defaultLimit int, maxLimit int) (EventQuery, error) {
	normalizedLimit, err := NormalizeReplayLimitWithBounds(limit, defaultLimit, maxLimit)
	if err != nil {
		return EventQuery{}, err
	}
	query := EventQuery{Limit: normalizedLimit}
	if strings.TrimSpace(afterCursor) == "" {
		return query, nil
	}
	cursor, err := ParseReplayCursor(afterCursor)
	if err != nil {
		return EventQuery{}, err
	}
	query.AfterCursor = &cursor
	return query, nil
}

func NormalizeReplayLimit(limit int) (int, error) {
	return NormalizeReplayLimitWithBounds(limit, DefaultReplayLimit, MaxReplayLimit)
}

func NormalizeReplayLimitWithBounds(limit int, defaultLimit int, maxLimit int) (int, error) {
	if defaultLimit < 1 {
		return 0, errors.New("default limit must be at least 1")
	}
	if maxLimit < defaultLimit {
		return 0, errors.New("max limit must be greater than or equal to default limit")
	}
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 0 {
		return 0, errors.New("limit must be greater than or equal to 0")
	}
	if limit > maxLimit {
		return 0, fmt.Errorf("limit must be less than or equal to %d", maxLimit)
	}
	return limit, nil
}

type ReplayEvent struct {
	Cursor             ReplayCursor `json:"cursor"`
	EventID            string       `json:"event_id"`
	EventName          string       `json:"event_name"`
	EventVersion       string       `json:"event_version"`
	ProducerService    string       `json:"producer_service"`
	ProducerInstance   *string      `json:"producer_instance,omitempty"`
	ProducerDeployment *string      `json:"producer_deployment,omitempty"`
	OccurredAt         time.Time    `json:"occurred_at"`
	ObservedAt         *time.Time   `json:"observed_at,omitempty"`
	PartitionID        *string      `json:"partition_id,omitempty"`
	SubjectType        *string      `json:"subject_type,omitempty"`
	SubjectID          *string      `json:"subject_id,omitempty"`
	AggregateType      *string      `json:"aggregate_type,omitempty"`
	AggregateID        *string      `json:"aggregate_id,omitempty"`
	CorrelationID      *string      `json:"correlation_id,omitempty"`
	CausationID        *string      `json:"causation_id,omitempty"`
	ActorType          *string      `json:"actor_type,omitempty"`
	ActorID            *string      `json:"actor_id,omitempty"`
	PayloadSHA256      string       `json:"payload_sha256"`
	EventSHA256        string       `json:"event_sha256"`
	Status             string       `json:"status"`
	StorageURI         *string      `json:"storage_uri,omitempty"`
	ReceivedAt         time.Time    `json:"received_at"`
	StoredAt           *time.Time   `json:"stored_at,omitempty"`
	RawEnvelopeJSON    string       `json:"raw_envelope_json"`
}

func ReplayEventFromRecord(record EventRecord) (ReplayEvent, error) {
	if record.Cursor == nil {
		return ReplayEvent{}, errors.New("event record cursor is required for replay")
	}
	return ReplayEvent{
		Cursor:             *record.Cursor,
		EventID:            record.EventID,
		EventName:          record.EventName,
		EventVersion:       record.EventVersion,
		ProducerService:    record.ProducerService,
		ProducerInstance:   record.ProducerInstance,
		ProducerDeployment: record.ProducerDeployment,
		OccurredAt:         record.OccurredAt,
		ObservedAt:         record.ObservedAt,
		PartitionID:        record.PartitionID,
		SubjectType:        record.SubjectType,
		SubjectID:          record.SubjectID,
		AggregateType:      record.AggregateType,
		AggregateID:        record.AggregateID,
		CorrelationID:      record.CorrelationID,
		CausationID:        record.CausationID,
		ActorType:          record.ActorType,
		ActorID:            record.ActorID,
		PayloadSHA256:      record.PayloadSHA256,
		EventSHA256:        record.EventSHA256,
		Status:             record.Status,
		StorageURI:         record.StorageURI,
		ReceivedAt:         record.ReceivedAt,
		StoredAt:           record.StoredAt,
		RawEnvelopeJSON:    record.RawEnvelopeJSON,
	}, nil
}

type ReplayPage struct {
	Events     []ReplayEvent `json:"events"`
	NextCursor *ReplayCursor `json:"next_cursor,omitempty"`
	Limit      int           `json:"limit"`
}

type ReplayWindowExpiredError struct {
	Requested ReplayCursor
	Floor     ReplayCursor
}

func (e ReplayWindowExpiredError) Error() string {
	return fmt.Sprintf("replay_window_expired: after_cursor %s is older than retained cursor floor %s", e.Requested.String(), e.Floor.String())
}

type RetentionResult struct {
	RequestedThrough ReplayCursor  `json:"requested_through"`
	RetainedThrough  *ReplayCursor `json:"retained_through,omitempty"`
	Confirmed        int           `json:"confirmed"`
	Deleted          int           `json:"deleted"`
	Skipped          int           `json:"skipped"`
}
