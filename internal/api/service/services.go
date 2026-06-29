package service

import (
	"context"

	"github.com/appliedsymbolics/sigint/internal/events"
)

type IngestService interface {
	LedgerReady(ctx context.Context) bool
	StorageReady() bool
	Ingest(ctx context.Context, envelope events.Envelope) (events.IngestResult, error)
	GetEvent(ctx context.Context, eventID string) (*events.EventRecord, error)
	ReplayEvents(ctx context.Context, query events.EventQuery) (events.ReplayPage, error)
}
