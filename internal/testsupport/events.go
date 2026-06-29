package testsupport

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/google/uuid"
)

func EventMap(t *testing.T, overrides map[string]any) map[string]any {
	t.Helper()
	payload := map[string]any{"order_id": "ORD-001", "status": "created"}
	raw := map[string]any{
		"event_id":            uuid.NewString(),
		"event_name":          "example.order.created",
		"event_version":       "1.0",
		"producer_service":    "example-service",
		"producer_instance":   "test-1",
		"producer_deployment": "test",
		"occurred_at":         "2026-04-30T12:00:00Z",
		"observed_at":         "2026-04-30T12:00:01Z",
		"partition_id":        "example-account",
		"subject_type":        "order",
		"subject_id":          "ORD-001",
		"aggregate_type":      "order",
		"aggregate_id":        "ORD-001",
		"correlation_id":      "corr-001",
		"causation_id":        "cmd-001",
		"actor": map[string]any{
			"type": "user",
			"id":   "user-001",
		},
		"payload": payload,
	}
	for key, value := range overrides {
		raw[key] = value
	}
	if replacement, ok := overrides["payload"].(map[string]any); ok {
		payload = replacement
	}
	payloadHash, err := events.SHA256JSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw["payload_sha256"] = payloadHash
	if value, ok := overrides["payload_sha256"]; ok {
		raw["payload_sha256"] = value
	}
	eventHashInput := copyMap(raw)
	delete(eventHashInput, "event_sha256")
	eventHash, err := events.SHA256JSON(eventHashInput)
	if err != nil {
		t.Fatal(err)
	}
	raw["event_sha256"] = eventHash
	if value, ok := overrides["event_sha256"]; ok {
		raw["event_sha256"] = value
	}
	return raw
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func SampleEnvelope() events.Envelope {
	payload := map[string]any{"order_id": "ORD-001", "status": "created"}
	raw := map[string]any{
		"event_id":            "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
		"event_name":          "example.order.created",
		"event_version":       "1.0",
		"producer_service":    "example-service",
		"producer_instance":   "test-1",
		"producer_deployment": "test",
		"occurred_at":         "2026-04-30T12:00:00Z",
		"observed_at":         "2026-04-30T12:00:01Z",
		"partition_id":        "example-account",
		"subject_type":        "order",
		"subject_id":          "ORD-001",
		"aggregate_type":      "order",
		"aggregate_id":        "ORD-001",
		"correlation_id":      "corr-001",
		"causation_id":        "cmd-001",
		"actor": map[string]any{
			"type": "user",
			"id":   "user-001",
		},
		"payload": payload,
	}
	payloadHash, err := events.SHA256JSON(payload)
	if err != nil {
		panic(err)
	}
	raw["payload_sha256"] = payloadHash
	eventHashInput := copyMap(raw)
	delete(eventHashInput, "event_sha256")
	eventHash, err := events.SHA256JSON(eventHashInput)
	if err != nil {
		panic(err)
	}
	raw["event_sha256"] = eventHash
	data, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	envelope, err := events.DecodeEnvelope(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	return envelope
}

func MustCanonicalEventJSON(t *testing.T, envelope events.Envelope) string {
	t.Helper()
	raw, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func EmptyEventQuery() events.EventQuery {
	return events.EventQuery{}
}
