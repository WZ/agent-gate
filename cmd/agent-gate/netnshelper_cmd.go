package main

import (
	"fmt"
	"os"

	"agent-gate/internal/launcher"
)

// maybeRunNetnsHelper intercepts argv[1] == "__netns-helper" before cobra
// parses anything. The hidden subcommand exec's its target; control never
// returns. Returns true if the helper ran (caller should exit).
func maybeRunNetnsHelper() bool {
	if len(os.Args) < 2 || os.Args[1] != "__netns-helper" {
		return false
	}
	if err := launcher.RunNetnsHelper(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// RunNetnsHelper exec's the target and never returns on success.
	return true
}
