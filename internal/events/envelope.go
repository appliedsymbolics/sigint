package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	Type string  `json:"type"`
	ID   *string `json:"id,omitempty"`
}

type Envelope struct {
	EventID            uuid.UUID      `json:"event_id"`
	EventName          string         `json:"event_name"`
	EventVersion       string         `json:"event_version"`
	ProducerService    string         `json:"producer_service"`
	ProducerInstance   *string        `json:"producer_instance,omitempty"`
	ProducerDeployment *string        `json:"producer_deployment,omitempty"`
	OccurredAt         time.Time      `json:"occurred_at"`
	ObservedAt         *time.Time     `json:"observed_at,omitempty"`
	PartitionID        *string        `json:"partition_id,omitempty"`
	SubjectType        *string        `json:"subject_type,omitempty"`
	SubjectID          *string        `json:"subject_id,omitempty"`
	AggregateType      *string        `json:"aggregate_type,omitempty"`
	AggregateID        *string        `json:"aggregate_id,omitempty"`
	CorrelationID      *string        `json:"correlation_id,omitempty"`
	CausationID        *string        `json:"causation_id,omitempty"`
	Actor              *Actor         `json:"actor,omitempty"`
	PayloadSHA256      string         `json:"payload_sha256"`
	EventSHA256        string         `json:"event_sha256"`
	Payload            map[string]any `json:"payload"`
}

type BatchRequest struct {
	Events []Envelope `json:"events"`
}

type IngestResult struct {
	EventID    string     `json:"event_id"`
	Status     string     `json:"status"`
	StorageURI *string    `json:"storage_uri"`
	ReceivedAt time.Time  `json:"received_at"`
	StoredAt   *time.Time `json:"stored_at"`
	Message    *string    `json:"message,omitempty"`
}

type EventRecord struct {
	Cursor             *ReplayCursor
	EventID            string
	EventName          string
	EventVersion       string
	ProducerService    string
	ProducerInstance   *string
	ProducerDeployment *string
	OccurredAt         time.Time
	ObservedAt         *time.Time
	PartitionID        *string
	SubjectType        *string
	SubjectID          *string
	AggregateType      *string
	AggregateID        *string
	CorrelationID      *string
	CausationID        *string
	ActorType          *string
	ActorID            *string
	PayloadSHA256      string
	EventSHA256        string
	Status             string
	StorageURI         *string
	ReceivedAt         time.Time
	StoredAt           *time.Time
	LastError          *string
	RawEnvelopeJSON    string
}

func DecodeEnvelope(reader io.Reader) (Envelope, error) {
	var envelope Envelope
	if err := decodeStrict(reader, &envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, envelope.Validate()
}

func DecodeBatch(reader io.Reader) (BatchRequest, error) {
	rawEvents, err := DecodeBatchRaw(reader)
	if err != nil {
		return BatchRequest{}, err
	}
	batch := BatchRequest{Events: make([]Envelope, 0, len(rawEvents))}
	for index, rawEvent := range rawEvents {
		envelope, err := DecodeEnvelope(bytes.NewReader(rawEvent))
		if err != nil {
			return BatchRequest{}, fmt.Errorf("events[%d]: %w", index, err)
		}
		batch.Events = append(batch.Events, envelope)
	}
	return batch, nil
}

func DecodeBatchRaw(reader io.Reader) ([]json.RawMessage, error) {
	var batch struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := decodeStrict(reader, &batch); err != nil {
		return nil, err
	}
	if len(batch.Events) < 1 {
		return nil, errors.New("events must contain at least one item")
	}
	if len(batch.Events) > 1000 {
		return nil, errors.New("events must contain no more than 1000 items")
	}
	return batch.Events, nil
}

func decodeStrict(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func (e Envelope) NormalizedEventID() string {
	return e.EventID.String()
}

func (e Envelope) CanonicalPayload() map[string]any {
	return e.Payload
}

func (e Envelope) CanonicalEventWithoutEventHash() map[string]any {
	data := e.canonicalEventMap()
	delete(data, "event_sha256")
	return data
}

func (e Envelope) CanonicalEvent() map[string]any {
	return e.canonicalEventMap()
}

func (e Envelope) ReceivedFallback() time.Time {
	if e.ObservedAt != nil {
		return *e.ObservedAt
	}
	return e.OccurredAt
}

func (e Envelope) canonicalEventMap() map[string]any {
	data := map[string]any{
		"event_id":         e.EventID.String(),
		"event_name":       e.EventName,
		"event_version":    e.EventVersion,
		"producer_service": e.ProducerService,
		"occurred_at":      formatTime(e.OccurredAt),
		"payload":          e.Payload,
		"payload_sha256":   e.PayloadSHA256,
		"event_sha256":     e.EventSHA256,
	}
	addStringPtr(data, "producer_instance", e.ProducerInstance)
	addStringPtr(data, "producer_deployment", e.ProducerDeployment)
	if e.ObservedAt != nil {
		data["observed_at"] = formatTime(*e.ObservedAt)
	}
	addStringPtr(data, "partition_id", e.PartitionID)
	addStringPtr(data, "subject_type", e.SubjectType)
	addStringPtr(data, "subject_id", e.SubjectID)
	addStringPtr(data, "aggregate_type", e.AggregateType)
	addStringPtr(data, "aggregate_id", e.AggregateID)
	addStringPtr(data, "correlation_id", e.CorrelationID)
	addStringPtr(data, "causation_id", e.CausationID)
	if e.Actor != nil {
		actor := map[string]any{"type": e.Actor.Type}
		addStringPtr(actor, "id", e.Actor.ID)
		data["actor"] = actor
	}
	return data
}

func addStringPtr(data map[string]any, key string, value *string) {
	if value != nil {
		data[key] = *value
	}
}

func formatTime(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}
