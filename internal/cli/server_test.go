package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const cliTestChildEnv = "SIGINT_CLI_TEST_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(cliTestChildEnv) == "1" {
		cmd := NewRootCommand()
		cmd.SetArgs(os.Args[1:])
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestServerStartBackgroundWritesPIDAfterReadiness(t *testing.T) {
	t.Setenv(cliTestChildEnv, "1")

	dir := t.TempDir()
	port := availableTCPPort(t)
	configPath := filepath.Join(dir, "sigint.yaml")
	writeCLIConfig(t, configPath, fmt.Sprintf(`
server:
  host: 127.0.0.1
  port: %d
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
storage:
  adapter: filesystem
  root: ./archive
replay:
  default_limit: 1
  max_limit: 1
	`, port))
	pidFile := filepath.Join(dir, "sigint.pid")
	logFile := filepath.Join(dir, "sigint.log")

	output, err := executeRoot(
		t,
		"server", "start",
		"--config", configPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--background",
		"--pid-file", pidFile,
		"--log-file", logFile,
	)
	if err != nil {
		t.Fatalf("server start: %v\nlog:\n%s", err, readOptionalFile(logFile))
	}

	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_, _ = executeRoot(t, "server", "stop", "--pid-file", pidFile)
		}
	})

	var response struct {
		Status  string `json:"status"`
		PID     int    `json:"pid"`
		PIDFile string `json:"pid_file"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("decode start response %q: %v", output.String(), err)
	}
	if response.Status != "started" || response.PID <= 0 || response.PIDFile != pidFile {
		t.Fatalf("unexpected start response: %+v", response)
	}
	if response.URL != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("unexpected start URL: %q", response.URL)
	}

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if strings.TrimSpace(string(pidData)) != strconv.Itoa(response.PID) {
		t.Fatalf("expected pid file to contain %d, got %q", response.PID, string(pidData))
	}
	if _, err := executeRoot(t, "server", "status", "--url", response.URL+"/readyz"); err != nil {
		t.Fatalf("server status after background start: %v\nlog:\n%s", err, readOptionalFile(logFile))
	}

	if _, err := executeRoot(t, "server", "stop", "--pid-file", pidFile); err != nil {
		t.Fatalf("server stop: %v", err)
	}
	stopped = true
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected pid file to be removed, got %v", err)
	}
}

func TestServerStartBackgroundDoesNotWritePIDWhenChildFails(t *testing.T) {
	t.Setenv(cliTestChildEnv, "1")

	dir := t.TempDir()
	blocker := newPortBlocker(t)
	defer blocker.Close()

	port := blocker.Listener.Addr().(*net.TCPAddr).Port
	configPath := filepath.Join(dir, "sigint.yaml")
	writeCLIConfig(t, configPath, fmt.Sprintf(`
server:
  host: 127.0.0.1
  port: %d
ledger:
  adapter: sqlite
  path: ./data/ingest.sqlite
storage:
  adapter: filesystem
  root: ./archive
replay:
  default_limit: 1
  max_limit: 1
	`, port))
	pidFile := filepath.Join(dir, "sigint.pid")
	logFile := filepath.Join(dir, "sigint.log")

	_, err := executeRoot(
		t,
		"server", "start",
		"--config", configPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--background",
		"--pid-file", pidFile,
		"--log-file", logFile,
	)
	if err == nil {
		_, _ = executeRoot(t, "server", "stop", "--pid-file", pidFile)
		t.Fatal("expected background start to fail")
	}
	if !strings.Contains(err.Error(), "server exited before readiness") {
		t.Fatalf("expected early exit error, got %v\nlog:\n%s", err, readOptionalFile(logFile))
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected pid file to be absent after failed start, got %v", err)
	}
}

func availableTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func newPortBlocker(t *testing.T) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on blocked port: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "occupied", http.StatusServiceUnavailable)
	}))
	server.Listener = listener
	server.Start()
	return server
}

func readOptionalFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
