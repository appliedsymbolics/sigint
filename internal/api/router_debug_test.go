package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/api"
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
	if strings.Contains(debugPage.Body.String(), "event-webhook-delivery") {
		t.Fatal("debug page should not include webhook-specific CSS markers")
	}
	if strings.Contains(debugPage.Body.String(), "Webhook delivery") {
		t.Fatal("debug page should not include webhook-specific badge labels")
	}
	for _, marker := range []string{"webhook.delivery_attempted", "webhook_delivery"} {
		if strings.Contains(debugPage.Body.String(), marker) {
			t.Fatalf("debug page should not include webhook-specific classifier %q", marker)
		}
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

func TestDebugRoutesUseInternalBearerTokenWhenConfigured(t *testing.T) {
	router, closeService := newRouterWithAuth(t, true, api.AuthOptions{
		ProducerToken: "producer-secret",
		InternalToken: "internal-secret",
	})
	defer closeService()

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: "GET", path: "/debug"},
		{method: "GET", path: "/debug/history"},
		{method: "DELETE", path: "/debug/history"},
		{method: "GET", path: "/debug/stream"},
	} {
		assertStatus(t, router, route.method, route.path, nil, http.StatusUnauthorized)
		assertStatusWithHeaders(t, router, route.method, route.path, nil, http.StatusUnauthorized, map[string]string{
			"Authorization": "Bearer producer-secret",
		})
	}

	assertStatusWithHeaders(t, router, "GET", "/debug", nil, http.StatusOK, map[string]string{
		"Authorization": "Bearer internal-secret",
	})
	assertJSONWithHeaders(t, router, "GET", "/debug/history", nil, http.StatusOK, map[string]string{
		"Authorization": "Bearer internal-secret",
	})
	assertStatusWithHeaders(t, router, "DELETE", "/debug/history", nil, http.StatusNoContent, map[string]string{
		"Authorization": "Bearer internal-secret",
	})
}

func TestDebugRoutesFallBackToProducerBearerToken(t *testing.T) {
	router, closeService := newRouterWithAuth(t, true, api.AuthOptions{ProducerToken: "producer-secret"})
	defer closeService()

	assertStatus(t, router, "GET", "/debug", nil, http.StatusUnauthorized)
	assertStatusWithHeaders(t, router, "GET", "/debug", nil, http.StatusOK, map[string]string{
		"Authorization": "Bearer producer-secret",
	})
}
