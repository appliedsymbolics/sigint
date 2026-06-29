package controller

import (
	"encoding/json"
	"time"

	"github.com/appliedsymbolics/sigint/internal/events"
)

type ingestResponse struct {
	EventID    string     `json:"event_id"`
	Status     string     `json:"status"`
	StorageURI *string    `json:"storage_uri"`
	ReceivedAt time.Time  `json:"received_at"`
	StoredAt   *time.Time `json:"stored_at"`
	Message    *string    `json:"message,omitempty"`
}

type batchResponse struct {
	Results []ingestResponse `json:"results"`
}

type recordResponse struct {
	EventID       string          `json:"event_id"`
	EventName     string          `json:"event_name"`
	EventVersion  string          `json:"event_version"`
	ProducerSvc   string          `json:"producer_service"`
	PartitionID   *string         `json:"partition_id"`
	SubjectType   *string         `json:"subject_type"`
	SubjectID     *string         `json:"subject_id"`
	PayloadSHA256 string          `json:"payload_sha256"`
	EventSHA256   string          `json:"event_sha256"`
	Status        string          `json:"status"`
	StorageURI    *string         `json:"storage_uri"`
	ReceivedAt    time.Time       `json:"received_at"`
	StoredAt      *time.Time      `json:"stored_at"`
	LastError     *string         `json:"last_error"`
	RawEnvelope   json.RawMessage `json:"raw_envelope" swaggertype:"object"`
}

func responseFromResult(result events.IngestResult) ingestResponse {
	return ingestResponse{
		EventID:    result.EventID,
		Status:     result.Status,
		StorageURI: result.StorageURI,
		ReceivedAt: result.ReceivedAt,
		StoredAt:   result.StoredAt,
		Message:    result.Message,
	}
}

func responseFromRecord(record events.EventRecord) (recordResponse, error) {
	raw := json.RawMessage(record.RawEnvelopeJSON)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return recordResponse{}, err
	}
	return recordResponse{
		EventID:       record.EventID,
		EventName:     record.EventName,
		EventVersion:  record.EventVersion,
		ProducerSvc:   record.ProducerService,
		PartitionID:   record.PartitionID,
		SubjectType:   record.SubjectType,
		SubjectID:     record.SubjectID,
		PayloadSHA256: record.PayloadSHA256,
		EventSHA256:   record.EventSHA256,
		Status:        record.Status,
		StorageURI:    record.StorageURI,
		ReceivedAt:    record.ReceivedAt,
		StoredAt:      record.StoredAt,
		LastError:     record.LastError,
		RawEnvelope:   raw,
	}, nil
}
