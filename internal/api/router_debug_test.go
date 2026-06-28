package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestDebugRoutesAreGatedAndHistoryReceivesEvents(t *testing.T) {
	disabled, closeDisabled := newRouter(t, false)
	defer closeDisabled()
	assertStatus(t, disabled, "GET", "/debug", nil, http.StatusNotFound)
	assertStatus(t, disabled, "DELETE", "/debug/history", nil, http.StatusNotFound)
	openapi := assertJSON(t, disabled, "GET", "/openapi.json", nil, http.StatusOK)
	paths := openapi["paths"].(map[string]any)
	if _, ok := paths["/debug"]; ok {
		t.Fatal("debug route should be omitted from OpenAPI when disabled")
	}
	if _, ok := paths["/debug/history"]; ok {
		t.Fatal("debug history route should be omitted from OpenAPI when disabled")
	}
	if _, ok := paths["/internal/v1/events/replay"]; !ok {
		t.Fatal("replay route should be present in OpenAPI")
	}

	enabled, closeEnabled := newRouter(t, true)
	defer closeEnabled()
	debugPage := assertStatus(t, enabled, "GET", "/debug", nil, http.StatusOK)
	if !strings.Contains(debugPage.Body.String(), "Clear events") {
		t.Fatal("debug page should include a clear events control")
	}
	if !strings.Contains(debugPage.Body.String(), "Order: newest first") {
		t.Fatal("debug page should include an event order control")
	}
	if !strings.Contains(debugPage.Body.String(), "event-webhook-delivery") {
		t.Fatal("debug page should include the webhook delivery CSS marker")
	}
	if !strings.Contains(debugPage.Body.String(), "Webhook delivery") {
		t.Fatal("debug page should include the webhook delivery badge label")
	}
	if !strings.Contains(debugPage.Body.String(), "webhook.delivery_attempted") {
		t.Fatal("debug page should classify by webhook delivery event name")
	}
	if !strings.Contains(debugPage.Body.String(), "webhook_delivery") {
		t.Fatal("debug page should classify by webhook delivery payload marker")
	}
	event := testsupport.EventMap(t, nil)
	assertJSON(t, enabled, "POST", "/v1/events", event, http.StatusOK)
	history := assertJSON(t, enabled, "GET", "/debug/history", nil, http.StatusOK)
	items := history[""].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one debug event, got %d", len(items))
	}
	assertStatus(t, enabled, "DELETE", "/debug/history", nil, http.StatusNoContent)
	history = assertJSON(t, enabled, "GET", "/debug/history", nil, http.StatusOK)
	items = history[""].([]any)
	if len(items) != 0 {
		t.Fatalf("expected no debug events after clear, got %d", len(items))
	}
}
