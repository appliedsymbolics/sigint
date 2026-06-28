package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestStatusLabel(t *testing.T) {
	if got := statusLabel(http.StatusOK); got != "ok" {
		t.Fatalf("expected success label, got %q", got)
	}
	if got := statusLabel(http.StatusUnauthorized); got != "4xx" {
		t.Fatalf("expected client error label, got %q", got)
	}
	if got := statusLabel(http.StatusInternalServerError); got != "5xx" {
		t.Fatalf("expected server error label, got %q", got)
	}
}

func TestMethodLabel(t *testing.T) {
	cases := map[string]string{
		http.MethodGet:    "GET",
		http.MethodPost:   "POST",
		http.MethodPut:    "PUT",
		http.MethodPatch:  "PATCH",
		http.MethodDelete: "DELETE",
		http.MethodHead:   "OTHER",
	}
	for method, expected := range cases {
		if got := methodLabel(method); got != expected {
			t.Fatalf("expected %q for %s, got %q", expected, method, got)
		}
	}
}

func TestFormatRequestLogIncludesLabels(t *testing.T) {
	output := formatRequestLog(gin.LogFormatterParams{
		StatusCode:   http.StatusCreated,
		Latency:      120 * time.Millisecond,
		ClientIP:     "127.0.0.1",
		Method:       http.MethodPost,
		Path:         "/v1/events",
		TimeStamp:    time.Date(2026, time.June, 5, 12, 34, 56, 0, time.UTC),
		ErrorMessage: "db timeout",
	})

	if !strings.Contains(output, "ok POST 201") {
		t.Fatalf("expected status and method labels, got %q", output)
	}
	if !strings.Contains(output, "error: db timeout") {
		t.Fatalf("expected error suffix, got %q", output)
	}
}

func TestPrefixedWriterAddsPrefix(t *testing.T) {
	var buf bytes.Buffer
	writer := prefixedWriter{writer: &buf, prefix: "panic: "}
	if _, err := writer.Write([]byte("panic details\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "panic: panic details\n" {
		t.Fatalf("unexpected prefixed output: %q", got)
	}
}
