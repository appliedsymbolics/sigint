package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
)

func TestNewServiceFromConfigCreatesLocalService(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "sigint.yaml")
	writeRuntimeConfig(t, configPath, `
server:
  host: 127.0.0.1
  port: 8920
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
storage:
  adapter: filesystem
  root: ./archive
`)

	handle, err := NewServiceFromConfig(context.Background(), configPath)
	if err != nil {
		t.Fatalf("NewServiceFromConfig returned error: %v", err)
	}
	defer func() { _ = handle.Close() }()

	if handle.Service == nil {
		t.Fatal("expected service")
	}
	if !handle.Service.LedgerReady(context.Background()) {
		t.Fatal("expected initialized ledger to be ready")
	}
	if !handle.Service.StorageReady() {
		t.Fatal("expected filesystem storage to be ready")
	}
}

func TestInitializeLedgerCreatesSQLiteSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Ledger{
		Adapter: "sqlite",
		Path:    filepath.Join(t.TempDir(), "data", "ingest.sqlite"),
	}

	if err := InitializeLedger(ctx, cfg); err != nil {
		t.Fatalf("InitializeLedger returned error: %v", err)
	}

	eventLedger, closeLedger, err := newLedger(ctx, cfg)
	if err != nil {
		t.Fatalf("open initialized ledger: %v", err)
	}
	defer func() { _ = closeLedger() }()
	if !eventLedger.IsReady(ctx) {
		t.Fatal("expected initialized ledger schema to be ready")
	}
}

func TestNewServiceRejectsUnsupportedLedgerAdapter(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Ledger.Adapter = "memory"
	cfg.Storage.Adapter = "filesystem"
	cfg.Storage.Root = t.TempDir()

	_, err := NewService(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `unsupported ledger adapter "memory"`) {
		t.Fatalf("expected unsupported ledger error, got %q", err.Error())
	}
}

func TestNewServiceRejectsUnsupportedStorageAdapter(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Ledger.Adapter = "sqlite"
	cfg.Ledger.Path = filepath.Join(t.TempDir(), "ingest.sqlite")
	cfg.Storage.Adapter = "blackhole"

	_, err := NewService(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `unsupported storage adapter "blackhole"`) {
		t.Fatalf("expected unsupported storage error, got %q", err.Error())
	}
}

func writeRuntimeConfig(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
