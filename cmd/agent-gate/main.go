package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if maybeRunNetnsHelper() {
		return
	}
	if err := newRootCmd().Execute(); err != nil {
		if !errors.Is(err, errDoctorFailed) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-gate",
		Short: "Personal audit gate for Claude Code outbound traffic",
		Long: `agent-gate intercepts outbound HTTPS from Claude Code and persists
every request/response to a local JSONL log + SQLite index for later review.`,
		SilenceUsage: true,
	}

	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Getting started:"},
		&cobra.Group{ID: "daily", Title: "Daily use:"},
		&cobra.Group{ID: "maint", Title: "Maintenance:"},
		&cobra.Group{ID: "topics", Title: "Help topics:"},
	)

	withGroup := func(c *cobra.Command, group string) *cobra.Command {
		c.GroupID = group
		return c
	}

	root.AddCommand(withGroup(initCmd(), "start"))
	root.AddCommand(withGroup(doctorCmd(), "start"))

	root.AddCommand(withGroup(runCmd(), "daily"))
	root.AddCommand(withGroup(dashboardCmd(), "daily"))
	root.AddCommand(withGroup(proxyCmd(), "daily"))
	root.AddCommand(withGroup(tailCmd(), "daily"))
	root.AddCommand(withGroup(stopCmd(), "daily"))

	root.AddCommand(withGroup(certCmd(), "maint"))
	root.AddCommand(withGroup(uninstallCmd(), "maint"))
	root.AddCommand(withGroup(versionCmd(), "maint"))

	root.AddCommand(withGroup(helpTopicAllowlist(), "topics"))
	root.AddCommand(withGroup(helpTopicDenylist(), "topics"))
	root.AddCommand(withGroup(helpTopicPassthrough(), "topics"))

	return root
}
