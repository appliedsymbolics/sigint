package ingest_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/ledger"
	"github.com/appliedsymbolics/sigint/internal/storage"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestRetainHotEventsDeletesOnlyArchiveConfirmedPrefixAndExpiresStaleCursors(t *testing.T) {
	ctx := context.Background()
	db, err := ledger.NewSQLite(filepath.Join(t.TempDir(), "ingest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := storage.NewFilesystem(t.TempDir())
	service := ingest.NewService(db, store, config.Default().Ingest)
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	firstEnvelope := decode(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	secondEnvelope := decode(t, testsupport.EventMap(t, map[string]any{
		"event_id": "d762b514-5da6-45ca-bc1d-2c6bf6899ad5",
	}))
	firstResult, err := service.Ingest(ctx, firstEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := service.Ingest(ctx, secondEnvelope)
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.ReplayEvents(ctx, events.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("expected 2 replay events, got %d", len(page.Events))
	}
	firstCursor := page.Events[0].Cursor
	secondCursor := page.Events[1].Cursor

	secondPath := filePathFromURI(t, secondResult.StorageURI)
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}

	result, err := service.RetainHotEvents(ctx, secondCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Confirmed != 1 || result.Deleted != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
	if result.RetainedThrough == nil || *result.RetainedThrough != firstCursor {
		t.Fatalf("unexpected retained cursor: %+v", result.RetainedThrough)
	}
	if _, err := os.Stat(filePathFromURI(t, firstResult.StorageURI)); err != nil {
		t.Fatalf("retention must not delete archive object: %v", err)
	}
	firstRecord, err := service.GetEvent(ctx, firstEnvelope.NormalizedEventID())
	if err != nil {
		t.Fatal(err)
	}
	if firstRecord != nil {
		t.Fatalf("expected first hot row to be deleted, got %+v", firstRecord)
	}
	secondRecord, err := service.GetEvent(ctx, secondEnvelope.NormalizedEventID())
	if err != nil {
		t.Fatal(err)
	}
	if secondRecord == nil {
		t.Fatal("second unconfirmed hot row should remain")
	}

	afterZero := events.ReplayCursor(0)
	_, err = service.ReplayEvents(ctx, events.EventQuery{AfterCursor: &afterZero, Limit: 10})
	var expired events.ReplayWindowExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("expected replay window expired, got %T %v", err, err)
	}
	_, err = service.ReplayEvents(ctx, events.EventQuery{Limit: 10})
	if !errors.As(err, &expired) {
		t.Fatalf("expected no-cursor replay window expired, got %T %v", err, err)
	}

	boundary, err := service.ReplayEvents(ctx, events.EventQuery{AfterCursor: &firstCursor, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(boundary.Events) != 1 || boundary.Events[0].EventID != secondEnvelope.NormalizedEventID() {
		t.Fatalf("boundary cursor should still replay remaining row, got %+v", boundary.Events)
	}
}

func filePathFromURI(t *testing.T, uri *string) string {
	t.Helper()
	if uri == nil {
		t.Fatal("storage URI is nil")
	}
	parsed, err := url.Parse(*uri)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Path
}
