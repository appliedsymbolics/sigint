package storage_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestWriteRawEventConflictsForSameEventIDWithDifferentKeyFields(t *testing.T) {
	eventID := "3ee6c93d-1f50-4e65-a867-f2f998be9ada"
	envelope := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{"event_id": eventID}))
	rawText, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	store := storage.NewFilesystem(root)
	if _, err := store.WriteRawEvent(envelope, rawText); err != nil {
		t.Fatal(err)
	}

	changed := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{
		"event_id":         eventID,
		"producer_service": "different-service",
		"event_name":       "example.signal.changed",
		"payload":          map[string]any{"signal_id": "SIG-001", "status": "changed"},
	}))
	changedRawText, err := events.CanonicalJSONText(changed.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.WriteRawEvent(changed, changedRawText)
	var conflict storage.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected event_id guard conflict, got %T %v", err, err)
	}

	changedPath := filepath.Join(
		root,
		"raw",
		"producer_service=different-service",
		"event_date=2026-04-30",
		"event_name=example.signal.changed",
		"event_id="+eventID+".json",
	)
	if _, err := os.Stat(changedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conflicting write should not create alternate archive path, stat err: %v", err)
	}
}

func TestWriteRawEventPublishesAtomicallyForConcurrentDifferentContent(t *testing.T) {
	eventID := "3ee6c93d-1f50-4e65-a867-f2f998be9ada"
	first := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{"event_id": eventID}))
	firstRawText, err := events.CanonicalJSONText(first.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	second := decodeEnvelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": eventID,
		"payload":  map[string]any{"signal_id": "SIG-001", "status": "changed"},
	}))
	secondRawText, err := events.CanonicalJSONText(second.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}

	store := storage.NewFilesystem(t.TempDir())
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, rawText := range []string{firstRawText, secondRawText} {
		wg.Add(1)
		go func(rawText string) {
			defer wg.Done()
			<-start
			_, err := store.WriteRawEvent(first, rawText)
			results <- err
		}(rawText)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		default:
			var conflict storage.ConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("expected conflict or success, got %T %v", err, err)
			}
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got %d successes and %d conflicts", successes, conflicts)
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
