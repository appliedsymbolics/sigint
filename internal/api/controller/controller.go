package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/appliedsymbolics/sigint/internal/api/service"
	"github.com/appliedsymbolics/sigint/internal/debugstream"
	"github.com/appliedsymbolics/sigint/internal/events"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	"github.com/gin-gonic/gin"
)

type Controller struct {
	service      service.IngestService
	bus          *debugstream.Bus
	replayLimits ReplayLimits
}

type ReplayLimits struct {
	DefaultLimit int
	MaxLimit     int
}

func New(service service.IngestService, bus *debugstream.Bus, replayLimits ReplayLimits) *Controller {
	return &Controller{service: service, bus: bus, replayLimits: normalizeReplayLimits(replayLimits)}
}

func normalizeReplayLimits(replayLimits ReplayLimits) ReplayLimits {
	if replayLimits.DefaultLimit == 0 {
		replayLimits.DefaultLimit = events.DefaultReplayLimit
	}
	if replayLimits.MaxLimit == 0 {
		replayLimits.MaxLimit = events.MaxReplayLimit
	}
	return replayLimits
}

// Healthz reports process liveness.
// @summary health check
// @description reports process liveness without checking configured backends
// @tags health
// @produce json
// @success 200 {object} map[string]string
// @router /healthz [get]
func (c *Controller) Healthz(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz reports backend readiness.
// @summary readiness check
// @description reports configured ledger and storage readiness
// @tags health
// @produce json
// @success 200 {object} map[string]string
// @failure 503 {object} map[string]string
// @router /readyz [get]
func (c *Controller) Readyz(ctx *gin.Context) {
	if !c.service.LedgerReady(ctx.Request.Context()) {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"detail": "ledger not ready"})
		return
	}
	if !c.service.StorageReady() {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"detail": "storage not ready"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// IngestEvent stores one event envelope.
// @summary ingest one event
// @description validates canonical hashes, writes the raw archive, and records ingest state
// @tags events
// @accept json
// @produce json
// @security BearerAuth
// @param event body events.Envelope true "Event envelope"
// @success 200 {object} ingestResponse
// @failure 401 {object} map[string]string
// @failure 409 {object} map[string]string
// @failure 422 {object} map[string]string
// @failure 500 {object} map[string]string
// @router /v1/events [post]
func (c *Controller) IngestEvent(ctx *gin.Context) {
	envelope, err := events.DecodeEnvelope(ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
		return
	}
	c.publishDebugEvent(envelope, "/v1/events", nil)

	result, err := c.service.Ingest(ctx.Request.Context(), envelope)
	if err != nil {
		c.writeIngestError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, responseFromResult(result))
}

// IngestBatch stores multiple event envelopes.
// @summary ingest a batch of events
// @description validates and ingests 1 to 1000 event envelopes, returning one result per event
// @tags events
// @accept json
// @produce json
// @security BearerAuth
// @param batch body events.BatchRequest true "Event batch"
// @success 200 {object} batchResponse
// @failure 401 {object} map[string]string
// @failure 422 {object} map[string]string
// @router /v1/events:batch [post]
func (c *Controller) IngestBatch(ctx *gin.Context) {
	rawEvents, err := events.DecodeBatchRaw(ctx.Request.Body)
	if err != nil {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
		return
	}

	results := make([]ingestResponse, 0, len(rawEvents))
	for index, rawEvent := range rawEvents {
		envelope, err := events.DecodeEnvelope(bytes.NewReader(rawEvent))
		if err != nil {
			results = append(results, batchFailureResponse(envelope, rawEvent, "failed", err))
			continue
		}

		batchIndex := index
		c.publishDebugEvent(envelope, "/v1/events:batch", &batchIndex)
		result, err := c.service.Ingest(ctx.Request.Context(), envelope)
		if err == nil {
			results = append(results, responseFromResult(result))
			continue
		}
		var conflict ingest.HashConflictError
		if errors.As(err, &conflict) {
			results = append(results, batchFailureResponse(envelope, rawEvent, "hash_conflict", err))
			continue
		}
		results = append(results, batchFailureResponse(envelope, rawEvent, "failed", err))
	}
	ctx.JSON(http.StatusOK, batchResponse{Results: results})
}

// GetEvent returns one event record.
// @summary get one event
// @description returns the ingest ledger record and raw envelope for one event id
// @tags events
// @produce json
// @param event_id path string true "Event ID"
// @success 200 {object} recordResponse
// @failure 404 {object} map[string]string
// @failure 500 {object} map[string]string
// @router /v1/events/{event_id} [get]
func (c *Controller) GetEvent(ctx *gin.Context) {
	record, err := c.service.GetEvent(ctx.Request.Context(), ctx.Param("event_id"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if record == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"detail": "event not found"})
		return
	}
	response, err := responseFromRecord(*record)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, response)
}

// ReplayEvents returns stored events after an optional cursor.
// @summary replay stored events
// @description replays stored events in ingest order using an exclusive cursor
// @tags internal
// @produce json
// @security BearerAuth
// @param after_cursor query string false "Exclusive replay cursor"
// @param limit query int false "Maximum events to return"
// @param producer_service query string false "Producer service filter"
// @param event_name query string false "Event name filter"
// @param subject_type query string false "Subject type filter"
// @param subject_id query string false "Subject id filter"
// @param aggregate_type query string false "Aggregate type filter"
// @param aggregate_id query string false "Aggregate id filter"
// @param correlation_id query string false "Correlation id filter"
// @success 200 {object} events.ReplayPage
// @failure 401 {object} map[string]string
// @failure 410 {object} map[string]string
// @failure 422 {object} map[string]string
// @failure 500 {object} map[string]string
// @router /internal/v1/events/replay [get]
func (c *Controller) ReplayEvents(ctx *gin.Context) {
	limit, err := parseOptionalInt(ctx.Query("limit"))
	if err != nil {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "limit must be an integer"})
		return
	}
	query, err := events.NewEventQueryWithLimits(ctx.Query("after_cursor"), limit, c.replayLimits.DefaultLimit, c.replayLimits.MaxLimit)
	if err != nil {
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
		return
	}
	query.ProducerService = queryParamPtr(ctx, "producer_service")
	query.EventName = queryParamPtr(ctx, "event_name")
	query.SubjectType = queryParamPtr(ctx, "subject_type")
	query.SubjectID = queryParamPtr(ctx, "subject_id")
	query.AggregateType = queryParamPtr(ctx, "aggregate_type")
	query.AggregateID = queryParamPtr(ctx, "aggregate_id")
	query.CorrelationID = queryParamPtr(ctx, "correlation_id")

	page, err := c.service.ReplayEvents(ctx.Request.Context(), query)
	if err != nil {
		c.writeReplayError(ctx, err)
		return
	}
	if page.Events == nil {
		page.Events = []events.ReplayEvent{}
	}
	ctx.JSON(http.StatusOK, page)
}

func (c *Controller) DebugPage(ctx *gin.Context) {
	if c.bus == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"detail": "debug stream disabled"})
		return
	}
	ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(debugPageHTML))
}

func (c *Controller) DebugHistory(ctx *gin.Context) {
	if c.bus == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"detail": "debug stream disabled"})
		return
	}
	ctx.JSON(http.StatusOK, c.bus.History())
}

func (c *Controller) DebugClearHistory(ctx *gin.Context) {
	if c.bus == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"detail": "debug stream disabled"})
		return
	}
	c.bus.ClearHistory()
	ctx.Status(http.StatusNoContent)
}

func (c *Controller) DebugStream(ctx *gin.Context) {
	if c.bus == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"detail": "debug stream disabled"})
		return
	}
	ctx.Header("Content-Type", "application/x-ndjson")
	subscriber := c.bus.Subscribe()
	defer c.bus.Unsubscribe(subscriber)

	ctx.Stream(func(w io.Writer) bool {
		select {
		case line, ok := <-subscriber:
			if !ok {
				return false
			}
			_, _ = w.Write([]byte(line))
			return true
		case <-ctx.Request.Context().Done():
			return false
		case <-time.After(100 * time.Millisecond):
			return !c.bus.IsClosed()
		}
	})
}

func (c *Controller) writeIngestError(ctx *gin.Context, err error) {
	var conflict ingest.HashConflictError
	var hashValidation ingest.HashValidationError
	var storage ingest.StorageError
	var ledger ingest.LedgerError
	switch {
	case errors.As(err, &conflict):
		ctx.JSON(http.StatusConflict, gin.H{"detail": err.Error()})
	case errors.As(err, &hashValidation):
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{"detail": err.Error()})
	case errors.As(err, &storage):
		ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
	case errors.As(err, &ledger):
		ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
	}
}

func (c *Controller) writeReplayError(ctx *gin.Context, err error) {
	var expired events.ReplayWindowExpiredError
	if errors.As(err, &expired) {
		ctx.JSON(http.StatusGone, gin.H{"code": "replay_window_expired", "detail": err.Error()})
		return
	}
	ctx.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
}

func parseOptionalInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func queryParamPtr(ctx *gin.Context, name string) *string {
	value := strings.TrimSpace(ctx.Query(name))
	if value == "" {
		return nil
	}
	return &value
}

func batchFailureResponse(envelope events.Envelope, rawEvent []byte, status string, err error) ingestResponse {
	message := err.Error()
	receivedAt := envelope.ReceivedFallback()
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	return ingestResponse{
		EventID:    batchFailureEventID(envelope, rawEvent),
		Status:     status,
		ReceivedAt: receivedAt,
		Message:    &message,
	}
}

func batchFailureEventID(envelope events.Envelope, rawEvent []byte) string {
	eventID := envelope.NormalizedEventID()
	if eventID != "00000000-0000-0000-0000-000000000000" {
		return eventID
	}
	var probe struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(rawEvent, &probe); err == nil && strings.TrimSpace(probe.EventID) != "" {
		return probe.EventID
	}
	return eventID
}

func (c *Controller) publishDebugEvent(envelope events.Envelope, endpoint string, batchIndex *int) {
	if c.bus == nil {
		return
	}
	source := map[string]any{"endpoint": endpoint}
	if batchIndex != nil {
		source["batch_index"] = *batchIndex
	}
	c.bus.Publish(map[string]any{
		"debug_version":    "1.0",
		"streamed_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"source":           source,
		"event_id":         envelope.NormalizedEventID(),
		"event_name":       envelope.EventName,
		"producer_service": envelope.ProducerService,
		"event":            envelope.CanonicalEvent(),
	})
}
