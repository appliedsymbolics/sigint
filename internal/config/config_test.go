package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/config"
)

func TestLoadResolvesRelativePathsFromConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
storage:
  adapter: filesystem
  root: ./data/event-lake
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ledger.Path != filepath.Join(dir, "data", "ingest.sqlite") {
		t.Fatalf("unexpected ledger path: %s", cfg.Ledger.Path)
	}
	if cfg.Storage.Root != filepath.Join(dir, "data", "event-lake") {
		t.Fatalf("unexpected storage root: %s", cfg.Storage.Root)
	}
}

func TestExamplesLoadWithoutChangingLocalConfigShape(t *testing.T) {
	for _, path := range []string{
		"../../examples/config.local.yaml",
		"../../examples/compose.config.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Ledger.Adapter != "sqlite" {
				t.Fatalf("unexpected ledger adapter: %s", cfg.Ledger.Adapter)
			}
			if cfg.Storage.Adapter != "filesystem" {
				t.Fatalf("unexpected storage adapter: %s", cfg.Storage.Adapter)
			}
		})
	}
}

func TestLoadProductionConfigExpandsEnvironmentPlaceholders(t *testing.T) {
	t.Setenv("SIGINT_POSTGRES_DSN", "postgres://sigint:secret@127.0.0.1:5432/sigint")
	t.Setenv("SIGINT_S3_BUCKET", "sigint-local")
	t.Setenv("SIGINT_AWS_REGION", "us-east-1")
	t.Setenv("SIGINT_S3_ENDPOINT", "http://127.0.0.1:4566")
	t.Setenv("SIGINT_PRODUCER_TOKEN", "producer-secret")
	t.Setenv("SIGINT_INTERNAL_TOKEN", "internal-secret")

	cfg, err := config.Load("../../examples/production.config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ledger.Adapter != "postgres" {
		t.Fatalf("unexpected ledger adapter: %s", cfg.Ledger.Adapter)
	}
	if cfg.Ledger.DSN == "" {
		t.Fatal("Postgres DSN was not expanded")
	}
	if cfg.Storage.Adapter != "s3" {
		t.Fatalf("unexpected storage adapter: %s", cfg.Storage.Adapter)
	}
	if cfg.Storage.Bucket != "sigint-local" {
		t.Fatalf("unexpected bucket: %s", cfg.Storage.Bucket)
	}
	if cfg.Storage.Endpoint != "http://127.0.0.1:4566" {
		t.Fatalf("unexpected endpoint: %s", cfg.Storage.Endpoint)
	}
	if !cfg.Storage.ForcePathStyle {
		t.Fatal("expected force_path_style for LocalStack-compatible config")
	}
	if cfg.Auth.ProducerToken != "producer-secret" {
		t.Fatal("producer token env was not resolved")
	}
	if cfg.Auth.InternalToken != "internal-secret" {
		t.Fatal("internal token env was not resolved")
	}
	if cfg.Replay.DefaultLimit != 100 || cfg.Replay.MaxLimit != 1000 {
		t.Fatalf("unexpected replay limits: %+v", cfg.Replay)
	}
}

func TestLoadRejectsUnknownConfigKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
  timeout: 30s
storage:
  adapter: filesystem
  root: ./event-lake
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(path)
	if err == nil {
		t.Fatal("expected unknown config key error")
	}
	if !strings.Contains(err.Error(), "field timeout not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLedgerUsesStrictLedgerKeysWithoutValidatingStorage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
storage:
  adapter: filesystem
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := config.LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Path != filepath.Join(dir, "ingest.sqlite") {
		t.Fatalf("unexpected ledger path: %s", ledger.Path)
	}

	unknownPath := filepath.Join(dir, "unknown.yaml")
	err = os.WriteFile(unknownPath, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
  timeout: 30s
storage:
  adapter: filesystem
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.LoadLedger(unknownPath)
	if err == nil {
		t.Fatal("expected unknown ledger key error")
	}
	if !strings.Contains(err.Error(), "field timeout not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAllowsIndependentLedgerAndStorageAdapters(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantLedger     string
		wantStorage    string
		wantLedgerPath bool
		wantStorageDir bool
	}{
		{
			name: "postgres ledger with filesystem storage",
			body: `
ledger:
  adapter: postgres
  dsn: postgres://sigint:secret@127.0.0.1:5432/sigint
storage:
  adapter: filesystem
  root: ./event-lake
`,
			wantLedger:     "postgres",
			wantStorage:    "filesystem",
			wantStorageDir: true,
		},
		{
			name: "sqlite ledger with s3 storage",
			body: `
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
storage:
  adapter: s3
  bucket: sigint-local
  region: us-east-1
`,
			wantLedger:     "sqlite",
			wantStorage:    "s3",
			wantLedgerPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			err := os.WriteFile(path, []byte(tt.body), 0o644)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Ledger.Adapter != tt.wantLedger {
				t.Fatalf("unexpected ledger adapter: %s", cfg.Ledger.Adapter)
			}
			if cfg.Storage.Adapter != tt.wantStorage {
				t.Fatalf("unexpected storage adapter: %s", cfg.Storage.Adapter)
			}
			if tt.wantLedgerPath && cfg.Ledger.Path != filepath.Join(dir, "ingest.sqlite") {
				t.Fatalf("unexpected ledger path: %s", cfg.Ledger.Path)
			}
			if tt.wantStorageDir && cfg.Storage.Root != filepath.Join(dir, "event-lake") {
				t.Fatalf("unexpected storage root: %s", cfg.Storage.Root)
			}
		})
	}
}

func TestLoadRejectsConfiguredMissingTokenEnv(t *testing.T) {
	_ = os.Unsetenv("SIGINT_TEST_MISSING_PRODUCER_TOKEN")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
storage:
  adapter: filesystem
  root: ./event-lake
auth:
  producer_token_env: SIGINT_TEST_MISSING_PRODUCER_TOKEN
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(path)
	if err == nil {
		t.Fatal("expected missing producer token env error")
	}
	if !strings.Contains(err.Error(), "auth.producer_token_env") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsUnsupportedRetentionHotWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
storage:
  adapter: filesystem
  root: ./event-lake
retention:
  hot_window: 168h
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(path)
	if err == nil {
		t.Fatal("expected unsupported hot window error")
	}
	if !strings.Contains(err.Error(), "retention.hot_window is not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigErrorsDoNotExposeTokenValues(t *testing.T) {
	t.Setenv("SIGINT_PRODUCER_TOKEN", "producer-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: postgres
storage:
  adapter: s3
  bucket: sigint-local
  region: us-east-1
auth:
  producer_token_env: SIGINT_PRODUCER_TOKEN
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "producer-secret") {
		t.Fatalf("config error leaked token value: %v", err)
	}
}

func TestFromEnvUsesSigintConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`
ledger:
  adapter: sqlite
  path: ./ingest.sqlite
storage:
  adapter: filesystem
  root: ./event-lake
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SIGINT_CONFIG", path)
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ledger.Adapter != "sqlite" {
		t.Fatalf("unexpected ledger adapter: %s", cfg.Ledger.Adapter)
	}
}
