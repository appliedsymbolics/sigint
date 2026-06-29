package ingest_test

import (
	"context"
	"os"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/appliedsymbolics/sigint/internal/ledger"
	"github.com/appliedsymbolics/sigint/internal/storage"
	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestPostgresIntegrationIngestDuplicateAndHashConflict(t *testing.T) {
	dsn := os.Getenv("SIGINT_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SIGINT_POSTGRES_TEST_DSN is not set")
	}

	ctx := context.Background()
	db, err := ledger.NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	service := ingest.NewService(db, storage.NewFilesystem(t.TempDir()), config.Default().Ingest)
	if err := service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	envelope := decode(t, testsupport.EventMap(t, nil))
	first, err := service.Ingest(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "stored" {
		t.Fatalf("expected stored, got %s", first.Status)
	}

	duplicate, err := service.Ingest(ctx, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Status != "duplicate" {
		t.Fatalf("expected duplicate, got %s", duplicate.Status)
	}

	changed := decode(t, testsupport.EventMap(t, map[string]any{
		"event_id": envelope.NormalizedEventID(),
		"payload":  map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	_, err = service.Ingest(ctx, changed)
	var conflict ingest.HashConflictError
	if !errorAs(err, &conflict) {
		t.Fatalf("expected conflict, got %T %v", err, err)
	}
}
