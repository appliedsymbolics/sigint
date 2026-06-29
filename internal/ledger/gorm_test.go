package ledger

import (
	"context"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestGORMSQLiteStoresAndReplaysEvent(t *testing.T) {
	ctx := context.Background()
	db, err := NewSQLite(t.TempDir() + "/ingest.sqlite")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer db.Close()
	if err := db.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !db.IsReady(ctx) {
		t.Fatal("expected sqlite ledger to be ready")
	}

	envelope := testsupport.SampleEnvelope()
	raw := testsupport.MustCanonicalEventJSON(t, envelope)
	record, err := db.InsertReceived(ctx, envelope, raw)
	if err != nil {
		t.Fatalf("insert received: %v", err)
	}
	if record.Status != "received" {
		t.Fatalf("status = %q, want received", record.Status)
	}
	if _, err := db.MarkStored(ctx, envelope.NormalizedEventID(), "file:///tmp/event.json"); err != nil {
		t.Fatalf("mark stored: %v", err)
	}

	page, err := db.ReplayEvents(ctx, testsupport.EmptyEventQuery())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("events len = %d, want 1", len(page.Events))
	}
	if page.Events[0].EventID != envelope.NormalizedEventID() {
		t.Fatalf("event_id = %q", page.Events[0].EventID)
	}
}
