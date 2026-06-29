package cli

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/appliedsymbolics/sigint/internal/config"
	"github.com/appliedsymbolics/sigint/internal/ingest"
	eventruntime "github.com/appliedsymbolics/sigint/internal/runtime"
)

func serviceFromConfig(ctx context.Context, configPath string) (*ingest.Service, func(), error) {
	handle, err := eventruntime.NewServiceFromConfig(ctx, configPath)
	if err != nil {
		return nil, func() {}, err
	}
	return handle.Service, func() { _ = handle.Close() }, nil
}

func serviceFromApp(ctx context.Context, cfg config.App) (*ingest.Service, func(), error) {
	handle, err := eventruntime.NewService(ctx, cfg)
	if err != nil {
		return nil, func() {}, err
	}
	return handle.Service, func() { _ = handle.Close() }, nil
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
