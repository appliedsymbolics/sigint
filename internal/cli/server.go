package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/appliedsymbolics/sigint/internal/api"
	"github.com/appliedsymbolics/sigint/internal/config"
)

const (
	backgroundReadinessTimeout     = 30 * time.Second
	backgroundReadinessPoll        = 200 * time.Millisecond
	backgroundReadinessHTTPTimeout = 6 * time.Second
)

func serverCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Server runtime commands.",
	}
	cmd.AddCommand(serverStartCommand())
	cmd.AddCommand(serverStatusCommand())
	cmd.AddCommand(serverStopCommand())
	return cmd
}

func serverStartCommand() *cobra.Command {
	var configPath, host, pidFile, logFile string
	var port int
	var background bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the sigint HTTP server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if host != "" {
				cfg.Server.Host = host
			}
			if port != 0 {
				cfg.Server.Port = port
			}
			if background {
				return startBackground(cmd, configPath, cfg.Server.Host, cfg.Server.Port, pidFile, logFile)
			}
			return runServer(cmd.Context(), cfg)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	cmd.Flags().StringVar(&host, "host", "", "Override configured host.")
	cmd.Flags().IntVar(&port, "port", 0, "Override configured port.")
	cmd.Flags().BoolVar(&background, "background", false, "Start server in the background.")
	cmd.Flags().StringVar(&pidFile, "pid-file", "sigint.pid", "PID file for background mode.")
	cmd.Flags().StringVar(&logFile, "log-file", "sigint.log", "Log file for background mode.")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func serverStatusCommand() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check server readiness.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := http.Client{Timeout: 5 * time.Second}
			response, err := client.Get(url)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode >= 400 {
				return fmt.Errorf("readiness check failed: %s", response.Status)
			}
			_, err = io.Copy(cmd.OutOrStdout(), response.Body)
			return err
		},
	}
	cmd.Flags().StringVar(&url, "url", "http://127.0.0.1:8920/readyz", "Readiness URL.")
	return cmd
}

func serverStopCommand() *cobra.Command {
	var pidFile string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a background server by PID file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				return fmt.Errorf("PID file not found: %s", pidFile)
			}
			pid, err := strconv.Atoi(string(bytes.TrimSpace(data)))
			if err != nil {
				return err
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := process.Signal(syscall.SIGTERM); err != nil {
				return err
			}
			_ = os.Remove(pidFile)
			return writeJSON(cmd, map[string]any{"status": "stopped", "pid": pid})
		},
	}
	cmd.Flags().StringVar(&pidFile, "pid-file", "sigint.pid", "PID file to stop.")
	return cmd
}

func runServer(parent context.Context, cfg config.App) error {
	service, closeService, err := serviceFromApp(parent, cfg)
	if err != nil {
		return err
	}
	defer closeService()

	runtime := api.NewRouter(api.Options{
		Service: service,
		Debug:   os.Getenv("SIGINT_ENV") == debugEnvValue,
		Auth: api.AuthOptions{
			ProducerToken: cfg.Auth.ProducerToken,
			InternalToken: cfg.Auth.InternalToken,
		},
		Replay: api.ReplayOptions{
			DefaultLimit: cfg.Replay.DefaultLimit,
			MaxLimit:     cfg.Replay.MaxLimit,
		},
	})
	server := &http.Server{
		Addr:    net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
		Handler: runtime.Router,
	}

	ctx, stop := signalContext(parent)
	defer stop()

	errCh := make(chan error, 1)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "sigint started on http://%s\n", listener.Addr().String())
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		if runtime.Bus != nil {
			runtime.Bus.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func startBackground(cmd *cobra.Command, configPath, host string, port int, pidFile, logFile string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil && filepath.Dir(pidFile) != "." {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil && filepath.Dir(logFile) != "." {
		return err
	}
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()

	child := exec.Command(exe, "server", "start", "--config", absConfig, "--host", host, "--port", strconv.Itoa(port))
	child.Stdout = log
	child.Stderr = log
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() {
		exited <- child.Wait()
	}()

	url := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	if err := waitForBackgroundReadiness(cmd.Context(), url+"/readyz", exited); err != nil {
		if !isBackgroundExitError(err) {
			terminateBackgroundProcess(child.Process, exited)
		}
		return fmt.Errorf("%w; see log file %s", err, logFile)
	}
	if err := pollBackgroundExit(exited); err != nil {
		return fmt.Errorf("%w; see log file %s", err, logFile)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644); err != nil {
		terminateBackgroundProcess(child.Process, exited)
		return err
	}
	return writeJSON(cmd, map[string]any{
		"status":   "started",
		"pid":      child.Process.Pid,
		"pid_file": pidFile,
		"log_file": logFile,
		"url":      url,
	})
}

func waitForBackgroundReadiness(parent context.Context, readinessURL string, exited <-chan error) error {
	ctx, cancel := context.WithTimeout(parent, backgroundReadinessTimeout)
	defer cancel()

	client := http.Client{Timeout: backgroundReadinessHTTPTimeout}
	ticker := time.NewTicker(backgroundReadinessPoll)
	defer ticker.Stop()

	for {
		if err := pollBackgroundExit(exited); err != nil {
			return err
		}
		if backgroundReady(ctx, &client, readinessURL) {
			return nil
		}
		if err := pollBackgroundExit(exited); err != nil {
			return err
		}

		select {
		case err := <-exited:
			return backgroundExitError{err: err}
		case <-ctx.Done():
			return fmt.Errorf("server did not become ready at %s within %s", readinessURL, backgroundReadinessTimeout)
		case <-ticker.C:
		}
	}
}

func backgroundReady(ctx context.Context, client *http.Client, readinessURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, readinessURL, nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode >= 200 && response.StatusCode < 300
}

func pollBackgroundExit(exited <-chan error) error {
	select {
	case err := <-exited:
		return backgroundExitError{err: err}
	default:
		return nil
	}
}

type backgroundExitError struct {
	err error
}

func (e backgroundExitError) Error() string {
	if e.err == nil {
		return "server exited before readiness"
	}
	return "server exited before readiness: " + e.err.Error()
}

func (e backgroundExitError) Unwrap() error {
	return e.err
}

func isBackgroundExitError(err error) bool {
	var exitErr backgroundExitError
	return errors.As(err, &exitErr)
}

func terminateBackgroundProcess(process *os.Process, exited <-chan error) {
	if process == nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
		return
	case <-time.After(5 * time.Second):
		_ = process.Kill()
	}
	select {
	case <-exited:
	default:
	}
}
