package api_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/api"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestReplayEndpointDefaultCursorFiltersAndEmptyPage(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	first := testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	})
	second := testsupport.EventMap(t, map[string]any{
		"event_id":         "d762b514-5da6-45ca-bc1d-2c6bf6899ad5",
		"event_name":       "billing.invoice.created",
		"producer_service": "billing-service",
		"subject_type":     "invoice",
		"subject_id":       "INV-001",
		"aggregate_type":   "invoice",
		"aggregate_id":     "INV-001",
		"correlation_id":   "corr-002",
	})
	assertJSON(t, router, "POST", "/v1/events", first, http.StatusOK)
	assertJSON(t, router, "POST", "/v1/events", second, http.StatusOK)

	page := assertJSON(t, router, "GET", "/internal/v1/events/replay", nil, http.StatusOK)
	if page["limit"].(float64) != float64(events.DefaultReplayLimit) {
		t.Fatalf("unexpected default limit: %v", page["limit"])
	}
	allEvents := page["events"].([]any)
	if len(allEvents) != 2 {
		t.Fatalf("expected 2 replay events, got %d", len(allEvents))
	}
	if allEvents[0].(map[string]any)["event_id"] != first["event_id"] {
		t.Fatalf("unexpected first replay event: %+v", allEvents[0])
	}

	firstPage := assertJSON(t, router, "GET", "/internal/v1/events/replay?limit=1", nil, http.StatusOK)
	firstPageEvents := firstPage["events"].([]any)
	if len(firstPageEvents) != 1 || firstPageEvents[0].(map[string]any)["event_id"] != first["event_id"] {
		t.Fatalf("unexpected first cursor page: %+v", firstPageEvents)
	}
	nextCursor := jsonNumberString(t, firstPage["next_cursor"])
	secondPage := assertJSON(t, router, "GET", "/internal/v1/events/replay?after_cursor="+nextCursor+"&limit=10", nil, http.StatusOK)
	secondPageEvents := secondPage["events"].([]any)
	if len(secondPageEvents) != 1 || secondPageEvents[0].(map[string]any)["event_id"] != second["event_id"] {
		t.Fatalf("unexpected second cursor page: %+v", secondPageEvents)
	}

	values := url.Values{}
	values.Set("producer_service", "billing-service")
	values.Set("event_name", "billing.invoice.created")
	values.Set("subject_type", "invoice")
	values.Set("subject_id", "INV-001")
	values.Set("aggregate_type", "invoice")
	values.Set("aggregate_id", "INV-001")
	values.Set("correlation_id", "corr-002")
	filtered := assertJSON(t, router, "GET", "/internal/v1/events/replay?"+values.Encode(), nil, http.StatusOK)
	filteredEvents := filtered["events"].([]any)
	if len(filteredEvents) != 1 || filteredEvents[0].(map[string]any)["event_id"] != second["event_id"] {
		t.Fatalf("unexpected filtered replay events: %+v", filteredEvents)
	}

	empty := assertJSON(t, router, "GET", "/internal/v1/events/replay?after_cursor=999999&limit=10", nil, http.StatusOK)
	if len(empty["events"].([]any)) != 0 {
		t.Fatalf("expected empty replay page, got %+v", empty["events"])
	}
}

func TestReplayEndpointUsesConfiguredLimits(t *testing.T) {
	router, closeService := newRouterWithOptions(t, false, api.AuthOptions{}, api.ReplayOptions{DefaultLimit: 1, MaxLimit: 2})
	defer closeService()
	first := testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	})
	second := testsupport.EventMap(t, map[string]any{
		"event_id": "d762b514-5da6-45ca-bc1d-2c6bf6899ad5",
	})
	assertJSON(t, router, "POST", "/v1/events", first, http.StatusOK)
	assertJSON(t, router, "POST", "/v1/events", second, http.StatusOK)

	page := assertJSON(t, router, "GET", "/internal/v1/events/replay", nil, http.StatusOK)
	if page["limit"].(float64) != 1 {
		t.Fatalf("expected configured default limit, got %+v", page)
	}
	if len(page["events"].([]any)) != 1 {
		t.Fatalf("expected configured default to return one event, got %+v", page["events"])
	}
	if page["next_cursor"] == nil {
		t.Fatal("expected next cursor with configured default limit")
	}
	errorBody := assertJSON(t, router, "GET", "/internal/v1/events/replay?limit=3", nil, http.StatusUnprocessableEntity)
	if !strings.Contains(errorBody["detail"].(string), "2") {
		t.Fatalf("expected configured max limit in error, got %+v", errorBody)
	}
}

func TestReplayEndpointRejectsInvalidQueries(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()

	assertJSON(t, router, "GET", "/internal/v1/events/replay?after_cursor=not-a-number", nil, http.StatusUnprocessableEntity)
	assertJSON(t, router, "GET", "/internal/v1/events/replay?limit=not-a-number", nil, http.StatusUnprocessableEntity)
	assertJSON(t, router, "GET", "/internal/v1/events/replay?limit=1001", nil, http.StatusUnprocessableEntity)
}

func TestReplayEndpointMapsWindowExpiredToGone(t *testing.T) {
	runtime := api.NewRouter(api.Options{Service: replayExpiredService{}})

	body := assertJSON(t, runtime.Router, "GET", "/internal/v1/events/replay?after_cursor=1", nil, http.StatusGone)
	if body["code"] != "replay_window_expired" {
		t.Fatalf("unexpected error body: %+v", body)
	}
}

type replayExpiredService struct{}

func (replayExpiredService) LedgerReady(ctx context.Context) bool {
	return true
}

func (replayExpiredService) StorageReady() bool {
	return true
}

func (replayExpiredService) Ingest(ctx context.Context, envelope events.Envelope) (events.IngestResult, error) {
	return events.IngestResult{}, nil
}

func (replayExpiredService) GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error) {
	return nil, nil
}

func (replayExpiredService) ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error) {
	return events.ReplayPage{}, events.ReplayWindowExpiredError{Requested: events.ReplayCursor(1), Floor: events.ReplayCursor(2)}
}
