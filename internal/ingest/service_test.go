package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/ledger"
	"github.com/appliedsymbolics/sigint/internal/storage"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestIngestStoresDuplicateAndConflict(t *testing.T) {
	service, closeService := newService(t)
	defer closeService()
	envelope := decode(t, testsupport.EventMap(t, nil))

	first, err := service.Ingest(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "stored" {
		t.Fatalf("expected stored, got %s", first.Status)
	}

	second, err := service.Ingest(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "duplicate" {
		t.Fatalf("expected duplicate, got %s", second.Status)
	}

	changed := decode(t, testsupport.EventMap(t, map[string]any{
		"event_id": envelope.NormalizedEventID(),
		"payload":  map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	_, err = service.Ingest(context.Background(), changed)
	var conflict ingest.HashConflictError
	if !errorAs(err, &conflict) {
		t.Fatalf("expected conflict, got %T %v", err, err)
	}
}

func TestIngestRejectsInvalidPayloadHash(t *testing.T) {
	service, closeService := newService(t)
	defer closeService()
	envelope := decode(t, testsupport.EventMap(t, map[string]any{"payload_sha256": strings.Repeat("0", 64)}))

	_, err := service.Ingest(context.Background(), envelope)
	var validation ingest.HashValidationError
	if !errorAs(err, &validation) {
		t.Fatalf("expected hash validation, got %T %v", err, err)
	}
}

func TestReplayEventsReturnsStoredIngests(t *testing.T) {
	service, closeService := newService(t)
	defer closeService()
	envelope := decode(t, testsupport.EventMap(t, nil))

	result, err := service.Ingest(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "stored" {
		t.Fatalf("expected stored, got %s", result.Status)
	}

	page, err := service.ReplayEvents(context.Background(), events.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected 1 replay event, got %d", len(page.Events))
	}
	if page.Events[0].EventID != result.EventID {
		t.Fatalf("expected event_id %s, got %s", result.EventID, page.Events[0].EventID)
	}
	if page.Events[0].RawEnvelopeJSON == "" {
		t.Fatal("expected raw envelope JSON")
	}
}

func newService(t *testing.T) (*ingest.Service, func()) {
	t.Helper()
	db, err := ledger.NewSQLite(t.TempDir() + "/ingest.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	service := ingest.NewService(db, storage.NewFilesystem(t.TempDir()), config.Default().Ingest)
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service, func() { _ = db.Close() }
}

func TestIngestCollectorFixturesStoreDuplicateReplayAndConflict(t *testing.T) {
	service, closeService := newService(t)
	defer closeService()
	firstFixture := fixtureMap(t, "order-created-event.json")
	firstEnvelope := decode(t, firstFixture)
	secondEnvelope := decodeFixture(t, "inventory-adjusted-event.json")

	first, err := service.Ingest(context.Background(), firstEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "stored" {
		t.Fatalf("expected stored, got %s", first.Status)
	}

	duplicate, err := service.Ingest(context.Background(), firstEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Status != "duplicate" {
		t.Fatalf("expected duplicate, got %s", duplicate.Status)
	}

	if _, err := service.Ingest(context.Background(), secondEnvelope); err != nil {
		t.Fatal(err)
	}

	stored, err := service.GetEvent(context.Background(), firstEnvelope.NormalizedEventID())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.RawEnvelopeJSON == "" {
		t.Fatalf("expected raw stored fixture envelope, got %+v", stored)
	}

	page, err := service.ReplayEvents(context.Background(), events.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("expected 2 replay events, got %d", len(page.Events))
	}

	conflictFixture := copyFixtureMap(firstFixture)
	payload := copyFixtureMap(conflictFixture["payload"].(map[string]any))
	payload["state"] = "updated"
	conflictFixture["payload"] = payload
	refreshFixtureHashes(t, conflictFixture)
	_, err = service.Ingest(context.Background(), decode(t, conflictFixture))
	var conflict ingest.HashConflictError
	if !errorAs(err, &conflict) {
		t.Fatalf("expected conflict, got %T %v", err, err)
	}
}
func decode(t *testing.T, raw map[string]any) events.Envelope {
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

func decodeFixture(t *testing.T, name string) events.Envelope {
	t.Helper()
	return decode(t, fixtureMap(t, name))
}

func fixtureMap(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func copyFixtureMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func refreshFixtureHashes(t *testing.T, raw map[string]any) {
	t.Helper()
	payloadHash, err := events.SHA256JSON(raw["payload"])
	if err != nil {
		t.Fatal(err)
	}
	raw["payload_sha256"] = payloadHash
	eventHashInput := copyFixtureMap(raw)
	delete(eventHashInput, "event_sha256")
	eventHash, err := events.SHA256JSON(eventHashInput)
	if err != nil {
		t.Fatal(err)
	}
	raw["event_sha256"] = eventHash
}

func errorAs[T error](err error, target *T) bool {
	return errors.As(err, target)
}
