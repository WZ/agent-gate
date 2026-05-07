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
		modeStr          string
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
			mode, airtightFail, err := parseRunMode(modeStr)
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&modeStr, "mode", "airtight", `How to force the target's egress through the proxy.
DEFAULT: airtight (recommended for most users). Choose one of:

  airtight        Spawn the target inside the per-OS network jail (macOS sandbox-exec
                  / Linux netns). Every byte of egress is physically routed through
                  the proxy; tools that ignore HTTPS_PROXY get kernel-level deny.
                  Falls back to permissive with a stderr warning if the platform
                  doesn't support the jail (e.g. hardened Linux, Windows today).

  airtight-strict Same as airtight, but refuse to fall back. Abort with a non-zero
                  exit if the jail is unavailable. Use in CI where partial capture
                  is worse than failing the run.

  permissive      Skip the jail entirely; just set HTTPS_PROXY env vars on the
                  child. A well-behaved agent honors them; a misbehaving one CAN
                  bypass capture. Use only when you trust the agent or the platform
                  cannot provide a jail.`)
	cmd.Flags().StringVar(&upstreamCAFile, "upstream-ca", "", "PEM file with extra root CA(s) to trust for proxy→upstream TLS (use for self-signed ANTHROPIC_BASE_URL)")
	cmd.Flags().BoolVar(&upstreamInsecure, "upstream-insecure-skip-verify", false, "Skip upstream cert verification entirely (testing only — captures still happen)")
	cmd.Flags().Bool("enforce-allowlist", false, "Make the proxy return 403 for hosts not in the allowlist; overrides [allowlist].enforce in config.toml")
	cmd.Flags().StringSliceVar(&hijackHosts, "hijack-host", nil, "Capture WebSocket message bodies for HOST. claude / codex / aider are captured by default and do NOT need this flag. Reach for it only if you're auditing a custom or internal agent that talks to your own WebSocket backend. Repeatable.")
	markAdvanced(cmd.Flags(), "upstream-ca", "upstream-insecure-skip-verify", "hijack-host")
	cmd.SetUsageFunc(renderUsage)
	return cmd
}

// parseRunMode maps the user-visible --mode value onto the launcher's
// (Mode, AirtightFail) pair. Three values:
//
//	airtight         → Mode=Airtight, AirtightFail=false  (default; auto-fallback on)
//	airtight-strict  → Mode=Airtight, AirtightFail=true   (no fallback)
//	permissive       → Mode=Permissive, AirtightFail=false
func parseRunMode(s string) (launcher.Mode, bool, error) {
	switch s {
	case "airtight":
		return launcher.Airtight, false, nil
	case "airtight-strict":
		return launcher.Airtight, true, nil
	case "permissive":
		return launcher.Permissive, false, nil
	default:
		return "", false, fmt.Errorf("--mode must be airtight, airtight-strict, or permissive (got %q)", s)
	}
}
