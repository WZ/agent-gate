package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if maybeRunNetnsHelper() {
		return
	}
	root := &cobra.Command{
		Use:   "agent-gate",
		Short: "Personal audit gate for Claude Code outbound traffic",
		Long: `agent-gate intercepts outbound HTTPS from Claude Code and persists
every request/response to a local JSONL log + SQLite index for later review.`,
		SilenceUsage: true,
	}
	root.AddCommand(versionCmd())
	root.AddCommand(proxyCmd())
	root.AddCommand(certCmd())
	root.AddCommand(tailCmd())
	root.AddCommand(dashboardCmd())
	root.AddCommand(runCmd())
	root.AddCommand(initCmd())
	root.AddCommand(uninstallCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
