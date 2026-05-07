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
		configPath       string
		permissive       bool
		airtightFail     bool
		upstreamCAFile   string
		upstreamInsecure bool
		hijackHosts      []string
	)
	var enforceAllowlist *bool
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
			// Honor --enforce-allowlist / --no-enforce-allowlist as a tri-state.
			if cmd.Flags().Changed("enforce-allowlist") {
				v, _ := cmd.Flags().GetBool("enforce-allowlist")
				enforceAllowlist = &v
			}
			opts := launcher.Options{
				Mode:                       mode,
				AirtightFail:               airtightFail,
				ConfigPath:                 configPath,
				Cmd:                        args[0],
				Args:                       args[1:],
				Stdin:                      os.Stdin,
				Stdout:                     os.Stdout,
				Stderr:                     os.Stderr,
				UpstreamCAFile:             upstreamCAFile,
				UpstreamInsecureSkipVerify: upstreamInsecure,
				EnforceAllowlist:           enforceAllowlist,
				HijackHosts:                hijackHosts,
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
	cmd.Flags().StringVar(&upstreamCAFile, "upstream-ca", "", "PEM file with extra root CA(s) to trust for proxy→upstream TLS (use for self-signed ANTHROPIC_BASE_URL)")
	cmd.Flags().BoolVar(&upstreamInsecure, "upstream-insecure-skip-verify", false, "Skip upstream cert verification entirely (testing only — captures still happen)")
	cmd.Flags().Bool("enforce-allowlist", false, "Make the proxy return 403 for hosts not in the allowlist; overrides [allowlist].enforce in config.toml")
	cmd.Flags().StringSliceVar(&hijackHosts, "hijack-host", nil, "Take ownership of the CONNECT for this host so the proxy can frame-decode the WebSocket session inside; repeatable. Example: --hijack-host chatgpt.com")
	return cmd
}
