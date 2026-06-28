package cli

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/events"
)

func ingestCommand() *cobra.Command {
	var configPath, eventPath string
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest commands.",
	}
	fileCmd := &cobra.Command{
		Use:   "file",
		Short: "Ingest one event envelope from a JSON file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, closeService, err := serviceFromConfig(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			defer closeService()
			data, err := os.ReadFile(eventPath)
			if err != nil {
				return err
			}
			envelope, err := events.DecodeEnvelope(bytes.NewReader(data))
			if err != nil {
				return err
			}
			result, err := service.Ingest(cmd.Context(), envelope)
			if err != nil {
				return err
			}
			return writeJSON(cmd, result)
		},
	}
	fileCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	fileCmd.Flags().StringVarP(&eventPath, "path", "p", "", "Path to event JSON.")
	_ = fileCmd.MarkFlagRequired("config")
	_ = fileCmd.MarkFlagRequired("path")
	cmd.AddCommand(fileCmd)
	return cmd
}

func eventsCommand() *cobra.Command {
	var configPath, eventID string
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Event lookup commands.",
	}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Fetch an event record from the ingest ledger.",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, closeService, err := serviceFromConfig(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			defer closeService()
			record, err := service.GetEvent(cmd.Context(), eventID)
			if err != nil {
				return err
			}
			if record == nil {
				return fmt.Errorf("event not found: %s", eventID)
			}
			return writeJSON(cmd, map[string]any{
				"event_id":         record.EventID,
				"event_name":       record.EventName,
				"event_version":    record.EventVersion,
				"producer_service": record.ProducerService,
				"status":           record.Status,
				"storage_uri":      record.StorageURI,
				"received_at":      record.ReceivedAt,
				"stored_at":        record.StoredAt,
			})
		},
	}
	getCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	getCmd.Flags().StringVar(&eventID, "event-id", "", "Event ID.")
	_ = getCmd.MarkFlagRequired("config")
	_ = getCmd.MarkFlagRequired("event-id")
	cmd.AddCommand(getCmd)
	cmd.AddCommand(eventsReplayCommand(&configPath))
	return cmd
}

func eventsReplayCommand(configPath *string) *cobra.Command {
	var afterCursor, producerService, eventName, subjectType, subjectID, aggregateType, aggregateID, correlationID string
	var limit int
	replayCmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay stored events from the ingest ledger.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			service, closeService, err := serviceFromApp(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer closeService()
			query, err := events.NewEventQueryWithLimits(afterCursor, limit, cfg.Replay.DefaultLimit, cfg.Replay.MaxLimit)
			if err != nil {
				return err
			}
			query.ProducerService = optionalString(producerService)
			query.EventName = optionalString(eventName)
			query.SubjectType = optionalString(subjectType)
			query.SubjectID = optionalString(subjectID)
			query.AggregateType = optionalString(aggregateType)
			query.AggregateID = optionalString(aggregateID)
			query.CorrelationID = optionalString(correlationID)
			page, err := service.ReplayEvents(cmd.Context(), query)
			if err != nil {
				return err
			}
			if page.Events == nil {
				page.Events = []events.ReplayEvent{}
			}
			return writeJSON(cmd, page)
		},
	}
	replayCmd.Flags().StringVarP(configPath, "config", "c", "", "Path to config YAML.")
	replayCmd.Flags().StringVar(&afterCursor, "after-cursor", "", "Exclusive replay cursor.")
	replayCmd.Flags().IntVar(&limit, "limit", 0, "Maximum replay events to return.")
	replayCmd.Flags().StringVar(&producerService, "producer-service", "", "Filter by producer service.")
	replayCmd.Flags().StringVar(&eventName, "event-name", "", "Filter by event name.")
	replayCmd.Flags().StringVar(&subjectType, "subject-type", "", "Filter by subject type.")
	replayCmd.Flags().StringVar(&subjectID, "subject-id", "", "Filter by subject id.")
	replayCmd.Flags().StringVar(&aggregateType, "aggregate-type", "", "Filter by aggregate type.")
	replayCmd.Flags().StringVar(&aggregateID, "aggregate-id", "", "Filter by aggregate id.")
	replayCmd.Flags().StringVar(&correlationID, "correlation-id", "", "Filter by correlation id.")
	_ = replayCmd.MarkFlagRequired("config")
	return replayCmd
}
