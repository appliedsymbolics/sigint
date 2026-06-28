package storage

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/appliedsymbolics/sigint/internal/events"
)

var unsafeSegment = regexp.MustCompile(`[^A-Za-z0-9._=-]+`)

type ConflictError struct {
	Message string
}

func (e ConflictError) Error() string {
	return e.Message
}

type Filesystem struct {
	root    string
	rawRoot string
}

func NewFilesystem(root string) *Filesystem {
	return &Filesystem{
		root:    root,
		rawRoot: filepath.Join(root, "raw"),
	}
}

func (f *Filesystem) IsReady() bool {
	if err := os.MkdirAll(f.rawRoot, 0o755); err != nil {
		return false
	}
	info, err := os.Stat(f.rawRoot)
	if err != nil || !info.IsDir() {
		return false
	}
	testFile, err := os.CreateTemp(f.rawRoot, ".ready-*.tmp")
	if err != nil {
		return false
	}
	name := testFile.Name()
	if err := testFile.Close(); err != nil {
		_ = os.Remove(name)
		return false
	}
	_ = os.Remove(name)
	return true
}

func (f *Filesystem) WriteRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	finalPath := f.pathFor(envelope)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return "", err
	}

	if existing, err := os.ReadFile(finalPath); err == nil {
		if string(existing) == rawEnvelopeJSON {
			return fileURI(finalPath)
		}
		return "", ConflictError{Message: "Storage object already exists with different content: " + finalPath}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(finalPath), "."+filepath.Base(finalPath)+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(rawEnvelopeJSON); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		return "", err
	}
	cleanup = false
	return fileURI(finalPath)
}

func (f *Filesystem) ConfirmRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	finalPath := f.pathFor(envelope)
	existing, err := os.ReadFile(finalPath)
	if err != nil {
		return "", err
	}
	if string(existing) != rawEnvelopeJSON {
		return "", ConflictError{Message: "Storage object already exists with different content: " + finalPath}
	}
	return fileURI(finalPath)
}

func (f *Filesystem) pathFor(envelope events.Envelope) string {
	return filepath.Join(
		f.rawRoot,
		"producer_service="+safeSegment(envelope.ProducerService),
		"event_date="+envelope.OccurredAt.Format("2006-01-02"),
		"event_name="+safeSegment(envelope.EventName),
		"event_id="+safeSegment(envelope.NormalizedEventID())+".json",
	)
}

func safeSegment(value string) string {
	safe := unsafeSegment.ReplaceAllString(strings.TrimSpace(value), "_")
	safe = strings.Trim(safe, "._")
	if safe == "" {
		return "unknown"
	}
	return safe
}

func fileURI(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String(), nil
}
