package cli

import (
	"github.com/spf13/cobra"

	"github.com/appliedsymbolics/sigint/internal/config"
	eventruntime "github.com/appliedsymbolics/sigint/internal/runtime"
)

func dbCommand() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database/ledger commands.",
	}
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the configured ingest ledger.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ledgerConfig, err := config.LoadLedger(configPath)
			if err != nil {
				return err
			}
			if err := eventruntime.InitializeLedger(cmd.Context(), ledgerConfig); err != nil {
				return err
			}
			response := map[string]any{"status": "initialized", "ledger_adapter": ledgerConfig.Adapter}
			if ledgerConfig.Adapter == "sqlite" {
				response["ledger"] = ledgerConfig.Path
			}
			return writeJSON(cmd, response)
		},
	}
	initCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	_ = initCmd.MarkFlagRequired("config")
	cmd.AddCommand(initCmd)
	return cmd
}
