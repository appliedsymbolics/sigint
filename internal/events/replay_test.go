package events_test

import (
	"testing"
	"time"

	"github.com/appliedsymbolics/sigint/internal/events"
)

func TestParseReplayCursor(t *testing.T) {
	cursor, err := events.ParseReplayCursor("42")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.String() != "42" {
		t.Fatalf("unexpected cursor string: %s", cursor.String())
	}

	if _, err := events.ParseReplayCursor("not-a-number"); err == nil {
		t.Fatal("expected invalid cursor error")
	}
	if _, err := events.ParseReplayCursor("-1"); err == nil {
		t.Fatal("expected negative cursor error")
	}
}

func TestNewEventQueryAppliesReplayLimits(t *testing.T) {
	query, err := events.NewEventQuery("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if query.Limit != events.DefaultReplayLimit {
		t.Fatalf("expected default limit %d, got %d", events.DefaultReplayLimit, query.Limit)
	}
	if query.AfterCursor != nil {
		t.Fatal("empty cursor should not set after_cursor")
	}

	query, err = events.NewEventQuery("100", events.MaxReplayLimit)
	if err != nil {
		t.Fatal(err)
	}
	if query.Limit != events.MaxReplayLimit {
		t.Fatalf("expected max limit %d, got %d", events.MaxReplayLimit, query.Limit)
	}
	if query.AfterCursor == nil || *query.AfterCursor != events.ReplayCursor(100) {
		t.Fatalf("unexpected after cursor: %v", query.AfterCursor)
	}

	if _, err := events.NewEventQuery("", events.MaxReplayLimit+1); err == nil {
		t.Fatal("expected over-limit error")
	}
}

func TestReplayEventFromRecord(t *testing.T) {
	cursor := events.ReplayCursor(7)
	storageURI := "file:///tmp/event.json"
	producerInstance := "instance-1"
	aggregateType := "order"
	aggregateID := "ORD-001"
	correlationID := "corr-001"
	occurredAt := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	receivedAt := occurredAt.Add(time.Second)
	storedAt := receivedAt.Add(time.Second)

	replayEvent, err := events.ReplayEventFromRecord(events.EventRecord{
		Cursor:           &cursor,
		EventID:          "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
		EventName:        "example.order.created",
		EventVersion:     "1.0",
		ProducerService:  "example-service",
		ProducerInstance: &producerInstance,
		OccurredAt:       occurredAt,
		AggregateType:    &aggregateType,
		AggregateID:      &aggregateID,
		CorrelationID:    &correlationID,
		PayloadSHA256:    "payload-hash",
		EventSHA256:      "event-hash",
		Status:           "stored",
		StorageURI:       &storageURI,
		ReceivedAt:       receivedAt,
		StoredAt:         &storedAt,
		RawEnvelopeJSON:  `{"event_id":"3ee6c93d-1f50-4e65-a867-f2f998be9ada"}` + "\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayEvent.Cursor != cursor {
		t.Fatalf("unexpected cursor: %v", replayEvent.Cursor)
	}
	if replayEvent.Status != "stored" {
		t.Fatalf("unexpected status: %s", replayEvent.Status)
	}
	if replayEvent.StorageURI == nil || *replayEvent.StorageURI != storageURI {
		t.Fatalf("unexpected storage URI: %v", replayEvent.StorageURI)
	}
	if replayEvent.RawEnvelopeJSON == "" {
		t.Fatal("raw envelope JSON should be included")
	}
	if replayEvent.AggregateID == nil || *replayEvent.AggregateID != aggregateID {
		t.Fatalf("unexpected aggregate id: %v", replayEvent.AggregateID)
	}
}

func TestReplayEventFromRecordRequiresCursor(t *testing.T) {
	if _, err := events.ReplayEventFromRecord(events.EventRecord{}); err == nil {
		t.Fatal("expected missing cursor error")
	}
}

func TestReplayWindowExpiredError(t *testing.T) {
	err := events.ReplayWindowExpiredError{
		Requested: events.ReplayCursor(10),
		Floor:     events.ReplayCursor(20),
	}
	if err.Error() != "replay_window_expired: after_cursor 10 is older than retained cursor floor 20" {
		t.Fatalf("unexpected error: %s", err.Error())
	}
}
