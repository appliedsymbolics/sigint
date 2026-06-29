package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/api"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestHealthReadyAndDocs(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()

	assertJSON(t, router, "GET", "/healthz", nil, http.StatusOK)
	assertJSON(t, router, "GET", "/readyz", nil, http.StatusOK)
	assertStatus(t, router, "GET", "/", nil, http.StatusTemporaryRedirect)
	assertStatus(t, router, "GET", "/llms.txt", nil, http.StatusOK)
	assertStatus(t, router, "GET", "/v1/docs/swagger/index.html", nil, http.StatusOK)
	assertStatus(t, router, "GET", "/openapi.json", nil, http.StatusOK)
	assertStatus(t, router, "GET", "/docs", nil, http.StatusNotFound)
}

func TestLLMSText(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()

	response := assertStatus(t, router, "GET", "/llms.txt", nil, http.StatusOK)
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	body := response.Body.String()
	if !strings.HasPrefix(body, "# sigint") {
		t.Fatalf("unexpected llms.txt heading: %q", body)
	}
	if !strings.Contains(body, "/openapi.json") {
		t.Fatalf("llms.txt missing OpenAPI link: %q", body)
	}
}

func TestPostEventDuplicateConflictAndLookup(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	event := testsupport.EventMap(t, nil)

	first := assertJSON(t, router, "POST", "/v1/events", event, http.StatusOK)
	if first["status"] != "stored" {
		t.Fatalf("expected stored, got %v", first["status"])
	}
	second := assertJSON(t, router, "POST", "/v1/events", event, http.StatusOK)
	if second["status"] != "duplicate" {
		t.Fatalf("expected duplicate, got %v", second["status"])
	}
	fetched := assertJSON(t, router, "GET", "/v1/events/"+event["event_id"].(string), nil, http.StatusOK)
	raw := fetched["raw_envelope"].(map[string]any)
	if raw["event_id"] != event["event_id"] {
		t.Fatalf("unexpected raw envelope: %v", raw["event_id"])
	}

	changed := testsupport.EventMap(t, map[string]any{
		"event_id": event["event_id"],
		"payload":  map[string]any{"order_id": "ORD-001", "status": "changed"},
	})
	assertJSON(t, router, "POST", "/v1/events", changed, http.StatusConflict)
}

func TestInvalidPayloadHashReturns422(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	event := testsupport.EventMap(t, map[string]any{"payload_sha256": "0000000000000000000000000000000000000000000000000000000000000000"})

	assertJSON(t, router, "POST", "/v1/events", event, http.StatusUnprocessableEntity)
}

func TestBatchReturnsPerEventFailures(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	first := testsupport.EventMap(t, nil)
	second := testsupport.EventMap(t, map[string]any{"payload_sha256": "0000000000000000000000000000000000000000000000000000000000000000"})

	body := assertJSON(t, router, "POST", "/v1/events:batch", map[string]any{"events": []any{first, second}}, http.StatusOK)
	results := body["results"].([]any)
	if results[0].(map[string]any)["status"] != "stored" {
		t.Fatalf("unexpected first result: %v", results[0])
	}
	if results[1].(map[string]any)["status"] != "failed" {
		t.Fatalf("unexpected second result: %v", results[1])
	}
}

func TestBatchReturnsPerEventValidationFailures(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	first := testsupport.EventMap(t, nil)
	second := testsupport.EventMap(t, nil)
	second["payload_sha256"] = strings.ToUpper(second["payload_sha256"].(string))

	body := assertJSON(t, router, "POST", "/v1/events:batch", map[string]any{"events": []any{first, second}}, http.StatusOK)
	results := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected two results, got %+v", results)
	}
	if results[0].(map[string]any)["status"] != "stored" {
		t.Fatalf("unexpected first result: %v", results[0])
	}
	secondResult := results[1].(map[string]any)
	if secondResult["event_id"] != second["event_id"] {
		t.Fatalf("unexpected failed event_id: %+v", secondResult)
	}
	if secondResult["status"] != "failed" {
		t.Fatalf("unexpected second result: %v", secondResult)
	}
	if !strings.Contains(secondResult["message"].(string), "lowercase") {
		t.Fatalf("expected lowercase hash validation message, got %+v", secondResult)
	}
}

func TestBatchMapsWrappedHashConflictErrors(t *testing.T) {
	runtime := api.NewRouter(api.Options{Service: wrappedHashConflictService{}})
	event := testsupport.EventMap(t, nil)

	body := assertJSON(t, runtime.Router, "POST", "/v1/events:batch", map[string]any{"events": []any{event}}, http.StatusOK)
	results := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %+v", results)
	}
	result := results[0].(map[string]any)
	if result["status"] != "hash_conflict" {
		t.Fatalf("expected hash_conflict result for wrapped error, got %+v", result)
	}
}

func TestProducerBearerAuthProtectsIngestRoutes(t *testing.T) {
	router, closeService := newRouterWithAuth(t, false, api.AuthOptions{ProducerToken: "producer-secret"})
	defer closeService()
	event := testsupport.EventMap(t, nil)

	missing := assertJSON(t, router, "POST", "/v1/events", event, http.StatusUnauthorized)
	if strings.Contains(missing["detail"].(string), "producer-secret") {
		t.Fatal("auth error leaked producer token")
	}
	assertJSONWithHeaders(t, router, "POST", "/v1/events", event, http.StatusUnauthorized, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	assertJSON(t, router, "POST", "/v1/events:batch", map[string]any{"events": []any{event}}, http.StatusUnauthorized)
	assertJSON(t, router, "GET", "/healthz", nil, http.StatusOK)

	stored := assertJSONWithHeaders(t, router, "POST", "/v1/events", event, http.StatusOK, map[string]string{
		"Authorization": "Bearer producer-secret",
	})
	if stored["status"] != "stored" {
		t.Fatalf("expected stored with valid token, got %+v", stored)
	}
}

func TestInternalBearerAuthProtectsReplayRoute(t *testing.T) {
	router, closeService := newRouterWithAuth(t, false, api.AuthOptions{InternalToken: "internal-secret"})
	defer closeService()
	event := testsupport.EventMap(t, nil)
	assertJSON(t, router, "POST", "/v1/events", event, http.StatusOK)

	missing := assertJSON(t, router, "GET", "/internal/v1/events/replay", nil, http.StatusUnauthorized)
	if strings.Contains(missing["detail"].(string), "internal-secret") {
		t.Fatal("auth error leaked internal token")
	}
	assertJSONWithHeaders(t, router, "GET", "/internal/v1/events/replay", nil, http.StatusUnauthorized, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	replay := assertJSONWithHeaders(t, router, "GET", "/internal/v1/events/replay", nil, http.StatusOK, map[string]string{
		"Authorization": "Bearer internal-secret",
	})
	if len(replay["events"].([]any)) != 1 {
		t.Fatalf("expected replay event with valid token, got %+v", replay)
	}
	assertJSON(t, router, "GET", "/healthz", nil, http.StatusOK)
}

func TestOpenAPIMarksAuthCapableRoutesWithBearerSecurity(t *testing.T) {
	router, closeService := newRouter(t, false)
	defer closeService()
	openapi := assertJSON(t, router, "GET", "/openapi.json", nil, http.StatusOK)
	paths := openapi["paths"].(map[string]any)
	for _, route := range []struct {
		path   string
		method string
	}{
		{path: "/v1/events", method: "post"},
		{path: "/v1/events:batch", method: "post"},
		{path: "/internal/v1/events/replay", method: "get"},
	} {
		pathItem := paths[route.path].(map[string]any)
		operation := pathItem[route.method].(map[string]any)
		if _, ok := operation["security"]; !ok {
			t.Fatalf("%s %s missing security requirement", route.method, route.path)
		}
	}
}

type wrappedHashConflictService struct{}

func (wrappedHashConflictService) LedgerReady(ctx context.Context) bool {
	return true
}

func (wrappedHashConflictService) StorageReady() bool {
	return true
}

func (wrappedHashConflictService) Ingest(ctx context.Context, envelope events.Envelope) (events.IngestResult, error) {
	return events.IngestResult{}, fmt.Errorf("wrapped: %w", ingest.HashConflictError{Message: "event hash conflict"})
}

func (wrappedHashConflictService) GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error) {
	return nil, nil
}

func (wrappedHashConflictService) ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error) {
	return events.ReplayPage{}, nil
}
