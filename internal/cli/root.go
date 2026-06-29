package cli

import "github.com/spf13/cobra"

const debugEnvValue = "debug"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "sigint",
		Short: "Generic events ingest service for immutable fact streams.",
	}
	root.AddCommand(dbCommand())
	root.AddCommand(serverCommand())
	root.AddCommand(ingestCommand())
	root.AddCommand(eventsCommand())
	root.AddCommand(retentionCommand())
	root.AddCommand(diagnosticsCommand())
	return root
}
