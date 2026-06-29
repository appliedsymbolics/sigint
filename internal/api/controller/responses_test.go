package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/events"
)

func TestResponseFromRecordPreservesRawEnvelopeJSONSemantics(t *testing.T) {
	raw := `{"event_id":"3ee6c93d-1f50-4e65-a867-f2f998be9ada","payload":{"amount":1.2300,"count":9007199254740993}}` + "\n"
	response, err := responseFromRecord(events.EventRecord{
		EventID:         "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
		EventName:       "example.amount.observed",
		EventVersion:    "1.0",
		ProducerService: "example-service",
		Status:          "stored",
		RawEnvelopeJSON: raw,
	})
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"raw_envelope":{"event_id":"3ee6c93d-1f50-4e65-a867-f2f998be9ada","payload":{"amount":1.2300,"count":9007199254740993}}`) {
		t.Fatalf("raw envelope JSON semantics were not preserved: %s", encoded)
	}
}
