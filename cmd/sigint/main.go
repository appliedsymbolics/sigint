package main

import (
	"fmt"
	"os"

	"github.com/appliedsymbolics/sigint/internal/cli"
)

// @title sigint API
// @version 0.1.0
// @description Generic events ingest service for durable event envelopes.
// @contact.name Applied Symbolics
// @host localhost:8920
// @BasePath /
// @schemes http https
// @securityDefinitions.apiKey BearerAuth
// @in header
// @name Authorization
// @description Optional bearer token for producer ingest and internal replay routes.
func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
