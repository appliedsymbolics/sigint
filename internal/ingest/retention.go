package ingest

import (
	"context"
	"errors"
	"strings"

	"github.com/appliedsymbolics/sigint/internal/events"
)

func (s *Service) RetainHotEvents(ctx context.Context, through events.ReplayCursor, limit int) (events.RetentionResult, error) {
	ledger, ok := s.ledger.(retentionLedger)
	if !ok {
		return events.RetentionResult{}, errors.New("ledger does not support retention")
	}
	storage, ok := s.storage.(archiveConfirmingStorage)
	if !ok {
		return events.RetentionResult{}, errors.New("storage does not support archive confirmation")
	}
	if limit <= 0 {
		limit = events.MaxReplayLimit
	}

	floor, err := ledger.GetReplayWatermark(ctx)
	if err != nil {
		return events.RetentionResult{}, err
	}
	candidates, err := ledger.RetentionCandidates(ctx, floor, through, limit)
	if err != nil {
		return events.RetentionResult{}, err
	}

	result := events.RetentionResult{RequestedThrough: through}
	confirmed := make([]events.ReplayCursor, 0, len(candidates))
	for _, record := range candidates {
		if record.Cursor == nil {
			result.Skipped++
			break
		}
		envelope, err := events.DecodeEnvelope(strings.NewReader(record.RawEnvelopeJSON))
		if err != nil {
			result.Skipped++
			break
		}
		if _, err := storage.ConfirmRawEvent(envelope, record.RawEnvelopeJSON); err != nil {
			result.Skipped++
			break
		}
		confirmed = append(confirmed, *record.Cursor)
		result.Confirmed++
		cursor := *record.Cursor
		result.RetainedThrough = &cursor
	}
	if len(confirmed) == 0 {
		return result, nil
	}
	deleted, err := ledger.RetainStoredEvents(ctx, confirmed, *result.RetainedThrough)
	if err != nil {
		return events.RetentionResult{}, err
	}
	result.Deleted = deleted
	return result, nil
}
