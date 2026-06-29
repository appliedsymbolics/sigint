package cli

import (
	"github.com/spf13/cobra"

	"github.com/appliedsymbolics/sigint/internal/events"
)

func retentionCommand() *cobra.Command {
	var configPath, throughCursor string
	var limit int
	cmd := &cobra.Command{
		Use:   "retention",
		Short: "Hot retention commands.",
	}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run explicit hot retention through an archive-confirmed cursor.",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, closeService, err := serviceFromConfig(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			defer closeService()
			cursor, err := events.ParseReplayCursor(throughCursor)
			if err != nil {
				return err
			}
			result, err := service.RetainHotEvents(cmd.Context(), cursor, limit)
			if err != nil {
				return err
			}
			return writeJSON(cmd, result)
		},
	}
	runCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	runCmd.Flags().StringVar(&throughCursor, "through-cursor", "", "Inclusive replay cursor to retain through.")
	runCmd.Flags().IntVar(&limit, "limit", 0, "Maximum candidate rows to inspect.")
	_ = runCmd.MarkFlagRequired("config")
	_ = runCmd.MarkFlagRequired("through-cursor")
	cmd.AddCommand(runCmd)
	return cmd
}

func diagnosticsCommand() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Backend diagnostics commands.",
	}
	readyCmd := &cobra.Command{
		Use:   "ready",
		Short: "Report configured backend readiness.",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, closeService, err := serviceFromConfig(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			defer closeService()
			ledgerReady := service.LedgerReady(cmd.Context())
			storageReady := service.StorageReady()
			status := "not_ready"
			if ledgerReady && storageReady {
				status = "ready"
			}
			return writeJSON(cmd, map[string]any{
				"status":        status,
				"ledger_ready":  ledgerReady,
				"storage_ready": storageReady,
			})
		},
	}
	readyCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	_ = readyCmd.MarkFlagRequired("config")
	cmd.AddCommand(readyCmd)
	return cmd
}
