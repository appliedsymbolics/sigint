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
	guardPath := f.eventIDGuardPathFor(envelope)
	guardCreated, err := writeFileIfAbsent(guardPath, rawEnvelopeJSON)
	if err != nil {
		return "", err
	}

	finalPath := f.pathFor(envelope)
	if _, err := writeFileIfAbsent(finalPath, rawEnvelopeJSON); err != nil {
		if guardCreated {
			removeFileIfContentMatches(guardPath, rawEnvelopeJSON)
		}
		return "", err
	}
	return fileURI(finalPath)
}

func writeFileIfAbsent(finalPath string, content string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return false, err
	}
	if err := confirmExistingFile(finalPath, content); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(finalPath), "."+filepath.Base(finalPath)+".*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tmpName, finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, confirmExistingFile(finalPath, content)
		}
		return false, err
	}
	cleanup = false
	_ = os.Remove(tmpName)
	return true, nil
}

func confirmExistingFile(path string, content string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(existing) != content {
		return ConflictError{Message: "Storage object already exists with different content: " + path}
	}
	return nil
}

func removeFileIfContentMatches(path string, content string) {
	if err := confirmExistingFile(path, content); err == nil {
		_ = os.Remove(path)
	}
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

func (f *Filesystem) eventIDGuardPathFor(envelope events.Envelope) string {
	return filepath.Join(
		f.rawRoot,
		"event_id="+safeSegment(envelope.NormalizedEventID())+".json",
	)
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
