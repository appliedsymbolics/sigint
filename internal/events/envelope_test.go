package events_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestDecodeEnvelopeAcceptsGenericCollectorFixtures(t *testing.T) {
	for _, fixture := range []struct {
		name            string
		producerService string
		eventName       string
	}{
		{
			name:            "order-created-event.json",
			producerService: "events-ingest-service",
			eventName:       "example.event.received",
		},
		{
			name:            "inventory-adjusted-event.json",
			producerService: "events-ingest-service",
			eventName:       "example.event.processed",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			envelope, err := events.DecodeEnvelope(bytes.NewReader(readExampleFixture(t, fixture.name)))
			if err != nil {
				t.Fatal(err)
			}
			if envelope.ProducerService != fixture.producerService {
				t.Fatalf("producer_service = %q", envelope.ProducerService)
			}
			if envelope.EventName != fixture.eventName {
				t.Fatalf("event_name = %q", envelope.EventName)
			}
			if envelope.PayloadSHA256 == "" || envelope.EventSHA256 == "" {
				t.Fatalf("fixture missing hashes: %+v", envelope)
			}
		})
	}
}

func TestDecodeBatchAcceptsCollectorFixtureBatch(t *testing.T) {
	batchBody, err := json.Marshal(map[string]any{
		"events": []json.RawMessage{
			readExampleFixture(t, "order-created-event.json"),
			readExampleFixture(t, "inventory-adjusted-event.json"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	batch, err := events.DecodeBatch(bytes.NewReader(batchBody))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 2 {
		t.Fatalf("expected 2 fixture events, got %d", len(batch.Events))
	}
}

func TestDecodeEnvelopeRejectsNonIngestEnvelopeFixture(t *testing.T) {
	_, err := events.DecodeEnvelope(bytes.NewReader(readExampleFixture(t, "non-ingest-webhook-envelope.json")))
	if err == nil {
		t.Fatal("expected non-ingest envelope to fail collector decoding")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestDecodeEnvelopeRejectsUnknownTopLevelFields(t *testing.T) {
	raw := testsupport.EventMap(t, map[string]any{"extra": "nope"})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = events.DecodeEnvelope(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestDecodeEnvelopeRejectsUppercaseHashes(t *testing.T) {
	raw := testsupport.EventMap(t, nil)
	raw["payload_sha256"] = strings.ToUpper(raw["payload_sha256"].(string))
	raw["event_sha256"] = strings.ToUpper(raw["event_sha256"].(string))
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = events.DecodeEnvelope(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "lowercase") {
		t.Fatalf("expected lowercase hash error, got %v", err)
	}
}

func TestDecodeEnvelopeRejectsPathSeparatorInEventName(t *testing.T) {
	raw := testsupport.EventMap(t, map[string]any{"event_name": "bad/name"})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = events.DecodeEnvelope(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeEnvelopeRejectsUnsafeProducerService(t *testing.T) {
	for name, tc := range map[string]struct {
		value   string
		wantErr string
	}{
		"slash": {
			value:   "bad/name",
			wantErr: "path separators",
		},
		"backslash": {
			value:   `bad\name`,
			wantErr: "path separators",
		},
		"parent_directory_marker": {
			value:   " .. ",
			wantErr: "path traversal",
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := testsupport.EventMap(t, map[string]any{"producer_service": tc.value})
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, err = events.DecodeEnvelope(bytes.NewReader(data))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDecodeEnvelopeRejectsTimezoneLessTimestamps(t *testing.T) {
	for name, overrides := range map[string]map[string]any{
		"occurred_at": {"occurred_at": "2026-04-30T12:00:00"},
		"observed_at": {"observed_at": "2026-04-30T12:00:01"},
	} {
		t.Run(name, func(t *testing.T) {
			raw := testsupport.EventMap(t, overrides)
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, err = events.DecodeEnvelope(bytes.NewReader(data))
			if err == nil {
				t.Fatal("expected timestamp validation error")
			}
		})
	}
}

func TestDecodeBatchRejectsOutOfRangeSizes(t *testing.T) {
	empty, err := json.Marshal(map[string]any{"events": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = events.DecodeBatch(bytes.NewReader(empty))
	if err == nil {
		t.Fatal("expected empty batch validation error")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected minimum batch size error, got %v", err)
	}

	tooManyEvents := make([]any, 1001)
	tooMany, err := json.Marshal(map[string]any{"events": tooManyEvents})
	if err != nil {
		t.Fatal(err)
	}
	_, err = events.DecodeBatch(bytes.NewReader(tooMany))
	if err == nil {
		t.Fatal("expected maximum batch size validation error")
	}
	if !strings.Contains(err.Error(), "no more than 1000") {
		t.Fatalf("expected maximum batch size error, got %v", err)
	}
}

func readExampleFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
