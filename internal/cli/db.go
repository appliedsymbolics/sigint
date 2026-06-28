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
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			handle, err := eventruntime.NewService(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer func() { _ = handle.Close() }()
			response := map[string]any{"status": "initialized", "ledger_adapter": cfg.Ledger.Adapter}
			if cfg.Ledger.Adapter == "sqlite" {
				response["ledger"] = cfg.Ledger.Path
			}
			return writeJSON(cmd, response)
		},
	}
	initCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config YAML.")
	_ = initCmd.MarkFlagRequired("config")
	cmd.AddCommand(initCmd)
	return cmd
}
