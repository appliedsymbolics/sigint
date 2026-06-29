package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/api"
	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/ledger"
	"github.com/appliedsymbolics/sigint/internal/storage"
	"github.com/gin-gonic/gin"
)

func newRouter(t *testing.T, debug bool) (*gin.Engine, func()) {
	return newRouterWithAuth(t, debug, api.AuthOptions{})
}

func newRouterWithAuth(t *testing.T, debug bool, auth api.AuthOptions) (*gin.Engine, func()) {
	return newRouterWithOptions(t, debug, auth, api.ReplayOptions{})
}

func newRouterWithOptions(t *testing.T, debug bool, auth api.AuthOptions, replay api.ReplayOptions) (*gin.Engine, func()) {
	t.Helper()
	db, err := ledger.NewSQLite(t.TempDir() + "/ingest.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	service := ingest.NewService(db, storage.NewFilesystem(t.TempDir()), config.Default().Ingest)
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime := api.NewRouter(api.Options{Service: service, Debug: debug, Auth: auth, Replay: replay})
	return runtime.Router, func() {
		_ = db.Close()
		if runtime.Bus != nil {
			runtime.Bus.Close()
		}
	}
}

func assertStatus(t *testing.T, router http.Handler, method, path string, body any, expected int) *httptest.ResponseRecorder {
	return assertStatusWithHeaders(t, router, method, path, body, expected, nil)
}

func assertStatusWithHeaders(t *testing.T, router http.Handler, method, path string, body any, expected int, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	requestBody := encodeBody(t, body)
	req := httptest.NewRequest(method, path, requestBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != expected {
		t.Fatalf("%s %s expected %d got %d body=%s", method, path, expected, rec.Code, rec.Body.String())
	}
	return rec
}

func assertJSON(t *testing.T, router http.Handler, method, path string, body any, expected int) map[string]any {
	t.Helper()
	return assertJSONWithHeaders(t, router, method, path, body, expected, nil)
}

func assertJSONWithHeaders(t *testing.T, router http.Handler, method, path string, body any, expected int, headers map[string]string) map[string]any {
	t.Helper()
	rec := assertStatusWithHeaders(t, router, method, path, body, expected, headers)
	var decoded any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if object, ok := decoded.(map[string]any); ok {
		return object
	}
	return map[string]any{"": decoded}
}

func encodeBody(t *testing.T, body any) *bytes.Reader {
	t.Helper()
	if body == nil {
		return bytes.NewReader(nil)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}

func jsonNumberString(t *testing.T, value any) string {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number, got %T %v", value, value)
	}
	return strconv.FormatInt(int64(number), 10)
}
