package storage_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/storage"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestWriteRawEventUsesDeterministicPath(t *testing.T) {
	raw := testsupport.EventMap(t, map[string]any{"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada"})
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := events.DecodeEnvelope(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rawText, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	uri, err := storage.NewFilesystem(root).WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("expected file URI, got %s", uri)
	}
	path := filepath.Join(
		root,
		"raw",
		"producer_service=example-service",
		"event_date=2026-04-30",
		"event_name=example.order.created",
		"event_id=3ee6c93d-1f50-4e65-a867-f2f998be9ada.json",
	)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRawEventIsIdempotentForSameContentAndConflictsForDifferentContent(t *testing.T) {
	eventID := "3ee6c93d-1f50-4e65-a867-f2f998be9ada"
	envelope := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{"event_id": eventID}))
	rawText, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}

	store := storage.NewFilesystem(t.TempDir())
	firstURI, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	secondURI, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	if secondURI != firstURI {
		t.Fatalf("expected idempotent write to return same URI, got %s then %s", firstURI, secondURI)
	}

	changed := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": eventID,
		"payload":  map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	changedRawText, err := events.CanonicalJSONText(changed.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.WriteRawEvent(changed, changedRawText)
	var conflict storage.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected storage conflict, got %T %v", err, err)
	}
}

func TestConfirmRawEventUsesObjectContent(t *testing.T) {
	envelope := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewFilesystem(t.TempDir())
	writtenURI, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	confirmedURI, err := store.ConfirmRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	if confirmedURI != writtenURI {
		t.Fatalf("expected confirmed URI %s, got %s", writtenURI, confirmedURI)
	}
	_, err = store.ConfirmRawEvent(envelope, "{}\n")
	var conflict storage.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected storage conflict, got %T %v", err, err)
	}
}

func decodeEnvelope(t *testing.T, raw map[string]any) events.Envelope {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := events.DecodeEnvelope(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}
