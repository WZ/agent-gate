package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These vars are populated at link time by goreleaser via -ldflags
// (-X main.version, main.commit, main.date). When building from source
// without ldflags they fall back to the dev defaults below.
var (
	version = "0.0.1-dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("agent-gate %s (commit %s, built %s)\n", version, commit, date)
			return nil
		},
	}
}
