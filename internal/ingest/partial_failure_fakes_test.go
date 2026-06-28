package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/storage"
)

type fakeLedger struct {
	records      map[string]events.EventRecord
	insertErrors []error
	attempts     []string
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{records: map[string]events.EventRecord{}}
}

func (f *fakeLedger) Initialize(ctx context.Context) error {
	return nil
}

func (f *fakeLedger) IsReady(ctx context.Context) bool {
	return true
}

func (f *fakeLedger) GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error) {
	record, ok := f.records[eventID]
	if !ok {
		return nil, nil
	}
	return &record, nil
}

func (f *fakeLedger) InsertReceived(ctx context.Context, envelope events.Envelope, rawEnvelopeJSON string) (events.EventRecord, error) {
	if len(f.insertErrors) > 0 {
		err := f.insertErrors[0]
		f.insertErrors = f.insertErrors[1:]
		return events.EventRecord{}, err
	}
	eventID := envelope.NormalizedEventID()
	if existing, ok := f.records[eventID]; ok {
		return existing, nil
	}
	record := fakeRecordFromEnvelope(envelope, rawEnvelopeJSON, "received")
	f.records[eventID] = record
	return record, nil
}

func (f *fakeLedger) MarkStored(ctx context.Context, eventID string, storageURI string) (events.EventRecord, error) {
	record, ok := f.records[eventID]
	if !ok {
		return events.EventRecord{}, errors.New("unknown event")
	}
	now := time.Now().UTC()
	record.Status = "stored"
	record.StorageURI = &storageURI
	record.StoredAt = &now
	record.LastError = nil
	f.records[eventID] = record
	return record, nil
}

func (f *fakeLedger) MarkFailed(ctx context.Context, eventID string, message string) (events.EventRecord, error) {
	record, ok := f.records[eventID]
	if !ok {
		return events.EventRecord{}, errors.New("unknown event")
	}
	record.Status = "failed"
	record.LastError = &message
	record.StorageURI = nil
	record.StoredAt = nil
	f.records[eventID] = record
	return record, nil
}

func (f *fakeLedger) RecordAttempt(ctx context.Context, eventID string, status string, message *string) error {
	f.attempts = append(f.attempts, status)
	return nil
}

func (f *fakeLedger) ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error) {
	eventsOut := []events.ReplayEvent{}
	for _, record := range f.records {
		if record.Status != "stored" {
			continue
		}
		if record.Cursor == nil {
			cursor := events.ReplayCursor(len(eventsOut) + 1)
			record.Cursor = &cursor
		}
		replayEvent, err := events.ReplayEventFromRecord(record)
		if err != nil {
			return events.ReplayPage{}, err
		}
		eventsOut = append(eventsOut, replayEvent)
	}
	return events.ReplayPage{Events: eventsOut, Limit: query.Limit}, nil
}

type fakeArchiveStorage struct {
	archiveFirst bool
	objects      map[string]string
	writeErrors  []error
	writeCount   int
}

func newFakeArchiveStorage(archiveFirst bool) *fakeArchiveStorage {
	return &fakeArchiveStorage{archiveFirst: archiveFirst, objects: map[string]string{}}
}

func (f *fakeArchiveStorage) ArchiveFirst() bool {
	return f.archiveFirst
}

func (f *fakeArchiveStorage) IsReady() bool {
	return true
}

func (f *fakeArchiveStorage) WriteRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	f.writeCount++
	if len(f.writeErrors) > 0 {
		err := f.writeErrors[0]
		f.writeErrors = f.writeErrors[1:]
		return "", err
	}
	eventID := envelope.NormalizedEventID()
	if existing, ok := f.objects[eventID]; ok && existing != rawEnvelopeJSON {
		return "", storage.ConflictError{Message: "archive conflict"}
	}
	f.objects[eventID] = rawEnvelopeJSON
	return "mock://archive/" + eventID + ".json", nil
}

func fakeRecordFromEnvelope(envelope events.Envelope, rawEnvelopeJSON string, status string) events.EventRecord {
	cursor := events.ReplayCursor(1)
	return events.EventRecord{
		Cursor:          &cursor,
		EventID:         envelope.NormalizedEventID(),
		EventName:       envelope.EventName,
		EventVersion:    envelope.EventVersion,
		ProducerService: envelope.ProducerService,
		OccurredAt:      envelope.OccurredAt,
		ObservedAt:      envelope.ObservedAt,
		PartitionID:     envelope.PartitionID,
		SubjectType:     envelope.SubjectType,
		SubjectID:       envelope.SubjectID,
		PayloadSHA256:   envelope.PayloadSHA256,
		EventSHA256:     envelope.EventSHA256,
		Status:          status,
		ReceivedAt:      time.Now().UTC(),
		RawEnvelopeJSON: rawEnvelopeJSON,
	}
}

func canonicalText(t *testing.T, envelope events.Envelope) string {
	t.Helper()
	raw, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
