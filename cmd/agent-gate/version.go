package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// version, commit, and date are populated at link time by goreleaser via
// -ldflags (-X main.version, main.commit, main.date) for tagged releases.
// For a plain `go build` from a git checkout, buildInfo() falls back to
// runtime/debug.ReadBuildInfo, which Go 1.18+ embeds automatically
// (vcs.revision, vcs.time, vcs.modified). This means a local install
// reports a real commit hash and commit timestamp without any Makefile
// gymnastics, while goreleaser's tagged builds keep their explicit values.
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
			v, c, d := buildInfo()
			fmt.Printf("agent-gate %s (commit %s, built %s)\n", v, c, d)
			return nil
		},
	}
}

// buildInfo returns the effective version, commit, and date for the running
// binary. ldflags-set values (from goreleaser) win when present; otherwise
// vcs metadata embedded by `go build` from a git checkout fills the gap, and
// a "-dirty" suffix is appended to the version when the working tree had
// uncommitted changes at build time.
func buildInfo() (v, c, d string) {
	v, c, d = version, commit, date
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "unknown" || c == "" {
				c = shortCommit(s.Value)
			}
		case "vcs.time":
			if d == "unknown" || d == "" {
				d = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = true
			}
		}
	}
	if dirty && !strings.HasSuffix(v, "-dirty") {
		v = v + "-dirty"
	}
	return v, c, d
}

func shortCommit(rev string) string {
	if len(rev) >= 7 {
		return rev[:7]
	}
	return rev
}
