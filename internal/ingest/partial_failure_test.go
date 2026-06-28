package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestArchiveFirstRetryRepairsAfterLedgerInsertFailure(t *testing.T) {
	ctx := context.Background()
	ledger := newFakeLedger()
	ledger.insertErrors = []error{errors.New("postgres unavailable")}
	storage := newFakeArchiveStorage(true)
	service := ingest.NewService(ledger, storage, config.Default().Ingest)
	envelope := decode(t, testsupport.EventMap(t, nil))

	_, err := service.Ingest(ctx, envelope)
	var ledgerErr ingest.LedgerError
	if !errors.As(err, &ledgerErr) {
		t.Fatalf("expected ledger error, got %T %v", err, err)
	}
	if storage.writeCount != 1 {
		t.Fatalf("expected one archive attempt, got %d", storage.writeCount)
	}
	if _, ok := ledger.records[envelope.NormalizedEventID()]; ok {
		t.Fatal("ledger row should not exist after insert failure")
	}

	result, err := service.Ingest(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "stored" {
		t.Fatalf("expected stored repair, got %s", result.Status)
	}
	if storage.writeCount != 2 {
		t.Fatalf("expected retry to confirm archive object, got %d writes", storage.writeCount)
	}
	record := ledger.records[envelope.NormalizedEventID()]
	if record.Status != "stored" || record.StorageURI == nil {
		t.Fatalf("expected repaired stored ledger row, got %+v", record)
	}
}

func TestArchiveFirstRetryWithChangedContentAfterLedgerInsertFailureConflicts(t *testing.T) {
	ctx := context.Background()
	ledger := newFakeLedger()
	ledger.insertErrors = []error{errors.New("postgres unavailable")}
	storage := newFakeArchiveStorage(true)
	service := ingest.NewService(ledger, storage, config.Default().Ingest)
	envelope := decode(t, testsupport.EventMap(t, nil))

	_, err := service.Ingest(ctx, envelope)
	var ledgerErr ingest.LedgerError
	if !errors.As(err, &ledgerErr) {
		t.Fatalf("expected first ingest ledger error, got %T %v", err, err)
	}

	changed := decode(t, testsupport.EventMap(t, map[string]any{
		"event_id":         envelope.NormalizedEventID(),
		"producer_service": "different-service",
		"event_name":       "example.order.changed",
		"payload":          map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	_, err = service.Ingest(ctx, changed)
	var conflict ingest.HashConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected changed retry to conflict, got %T %v", err, err)
	}
	if _, ok := ledger.records[envelope.NormalizedEventID()]; ok {
		t.Fatal("conflicting retry should not create a ledger row")
	}
}

func TestExistingLedgerRowWithStorageFailureIsNotReplayableUntilRetry(t *testing.T) {
	ctx := context.Background()
	ledger := newFakeLedger()
	storage := newFakeArchiveStorage(true)
	storage.writeErrors = []error{errors.New("s3 unavailable")}
	service := ingest.NewService(ledger, storage, config.Default().Ingest)
	envelope := decode(t, testsupport.EventMap(t, nil))
	raw := canonicalText(t, envelope)
	ledger.records[envelope.NormalizedEventID()] = fakeRecordFromEnvelope(envelope, raw, "received")

	_, err := service.Ingest(ctx, envelope)
	var storageErr ingest.StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error, got %T %v", err, err)
	}
	record := ledger.records[envelope.NormalizedEventID()]
	if record.Status != "failed" {
		t.Fatalf("expected failed ledger row, got %s", record.Status)
	}
	if record.StorageURI != nil {
		t.Fatalf("event should not have storage URI after storage failure: %v", record.StorageURI)
	}
	page, err := service.ReplayEvents(ctx, events.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 0 {
		t.Fatalf("failed event should not be replayable: %+v", page.Events)
	}

	result, err := service.Ingest(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "stored" {
		t.Fatalf("expected stored retry, got %s", result.Status)
	}
	if storage.writeCount != 2 {
		t.Fatalf("expected retry to attempt archive again, got %d writes", storage.writeCount)
	}
	page, err = service.ReplayEvents(ctx, events.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].EventID != envelope.NormalizedEventID() {
		t.Fatalf("expected stored event to become replayable, got %+v", page.Events)
	}
}

func TestStorageContentConflictIsReturnedAsHashConflict(t *testing.T) {
	ctx := context.Background()
	ledger := newFakeLedger()
	storage := newFakeArchiveStorage(true)
	service := ingest.NewService(ledger, storage, config.Default().Ingest)
	envelope := decode(t, testsupport.EventMap(t, nil))
	raw := canonicalText(t, envelope)
	ledger.records[envelope.NormalizedEventID()] = fakeRecordFromEnvelope(envelope, raw, "received")
	storage.objects[envelope.NormalizedEventID()] = "{}"

	_, err := service.Ingest(ctx, envelope)
	var conflict ingest.HashConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected storage conflict to stay immutable conflict, got %T %v", err, err)
	}
	if ledger.records[envelope.NormalizedEventID()].Status != "received" {
		t.Fatalf("storage conflict should not mark ledger failed: %+v", ledger.records[envelope.NormalizedEventID()])
	}
}
