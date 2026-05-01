package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"agent-gate/internal/launcher"
	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	var (
		configPath   string
		permissive   bool
		airtightFail bool
	)
	cmd := &cobra.Command{
		Use:                   "run [flags] -- <cmd> [args...]",
		Short:                 "Launch a command with airtight network capture",
		Long:                  "Spawns the target inside a per-platform network jail forcing all egress through the proxy.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := launcher.Airtight
			if permissive {
				mode = launcher.Permissive
			}
			opts := launcher.Options{
				Mode:         mode,
				AirtightFail: airtightFail,
				ConfigPath:   configPath,
				Cmd:          args[0],
				Args:         args[1:],
				Stdin:        os.Stdin,
				Stdout:       os.Stdout,
				Stderr:       os.Stderr,
			}
			exit, err := launcher.Run(context.Background(), opts)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			if exit != 0 {
				os.Exit(exit)
			}
			return nil
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	cmd.Flags().BoolVar(&permissive, "permissive", false, "Downgrade to env-only enforcement (HTTPS_PROXY)")
	cmd.Flags().BoolVar(&airtightFail, "airtight-fail", false, "Require airtight; abort if unsupported on this platform")
	return cmd
}
