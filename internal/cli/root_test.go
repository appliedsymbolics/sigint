package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestDBInitInitializesLedgerOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "sigint.yaml")
	writeCLIConfig(t, configPath, `
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
	`)

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"db", "init", "--config", configPath})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute db init: %v", err)
	}

	var response map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", stdout.String(), err)
	}
	if response["status"] != "initialized" {
		t.Fatalf("expected initialized status, got %q", response["status"])
	}

	ledgerPath := filepath.Join(dir, "data", "ingest.sqlite")
	if response["ledger"] != ledgerPath {
		t.Fatalf("expected ledger path %q, got %q", ledgerPath, response["ledger"])
	}
	if _, err := os.Stat(ledgerPath); err != nil {
		t.Fatalf("expected sqlite ledger file: %v", err)
	}
}

func TestReplayRetentionAndDiagnosticsCommands(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "sigint.yaml")
	writeCLIConfig(t, configPath, `
server:
  host: 127.0.0.1
  port: 8920
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
storage:
  adapter: filesystem
  root: ./archive
replay:
  default_limit: 1
  max_limit: 1
	`)
	eventPath := filepath.Join(dir, "event.json")
	event := testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	})
	writeCLIJSON(t, eventPath, event)

	if _, err := executeRoot(t, "db", "init", "--config", configPath); err != nil {
		t.Fatalf("db init: %v", err)
	}
	if _, err := executeRoot(t, "ingest", "file", "--config", configPath, "--path", eventPath); err != nil {
		t.Fatalf("ingest file: %v", err)
	}

	replayOut, err := executeRoot(t, "events", "replay", "--config", configPath)
	if err != nil {
		t.Fatalf("events replay: %v", err)
	}
	var replay struct {
		Events []struct {
			Cursor  float64 `json:"cursor"`
			EventID string  `json:"event_id"`
		} `json:"events"`
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(replayOut.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay %q: %v", replayOut.String(), err)
	}
	if replay.Limit != 1 || len(replay.Events) != 1 || replay.Events[0].EventID != event["event_id"] {
		t.Fatalf("unexpected replay response: %+v", replay)
	}

	throughCursor := strconv.FormatInt(int64(replay.Events[0].Cursor), 10)
	retentionOut, err := executeRoot(t, "retention", "run", "--config", configPath, "--through-cursor", throughCursor)
	if err != nil {
		t.Fatalf("retention run: %v", err)
	}
	var retention struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(retentionOut.Bytes(), &retention); err != nil {
		t.Fatalf("decode retention %q: %v", retentionOut.String(), err)
	}
	if retention.Deleted != 1 {
		t.Fatalf("expected one deleted row, got %+v", retention)
	}

	diagnosticsOut, err := executeRoot(t, "diagnostics", "ready", "--config", configPath)
	if err != nil {
		t.Fatalf("diagnostics ready: %v", err)
	}
	var diagnostics map[string]any
	if err := json.Unmarshal(diagnosticsOut.Bytes(), &diagnostics); err != nil {
		t.Fatalf("decode diagnostics %q: %v", diagnosticsOut.String(), err)
	}
	if diagnostics["status"] != "ready" {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func writeCLIConfig(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func executeRoot(t *testing.T, args ...string) (bytes.Buffer, error) {
	t.Helper()
	cmd := NewRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return output, err
}
