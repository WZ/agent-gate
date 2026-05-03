package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"agent-gate/internal/ca"
	"agent-gate/internal/launcher"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap config + CA + (Windows) WFP install",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := filepath.Dir(configPath)
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				return err
			}
			caDir := filepath.Join(configDir, "ca")
			if _, err := ca.Ensure(caDir); err != nil {
				return fmt.Errorf("ca: %w", err)
			}
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				if err := os.WriteFile(configPath, []byte(defaultConfigToml()), 0o600); err != nil {
					return fmt.Errorf("write default config: %w", err)
				}
			}
			if runtime.GOOS == "windows" {
				if err := launcher.InstallWFP(); err != nil {
					return fmt.Errorf("WFP install (run as admin): %w", err)
				}
			}
			fmt.Fprintln(os.Stderr, "agent-gate init: complete")
			return nil
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	return cmd
}

func defaultConfigToml() string {
	return `# agent-gate config — single-user audit gate for AI agents.
# All settings are local; nothing is ever sent off this machine.

[capture]
# "airtight" forces all subprocess egress through the proxy via a per-OS
# network jail. "permissive" only sets HTTPS_PROXY env vars (testing only).
default_mode = "airtight"

[ports]
# Loopback-only. Both refuse non-127.0.0.1 binds.
proxy = 8888       # TLS-MITM proxy
dashboard = 7878   # Local web UI for review

[storage]
# Where captured events are persisted (JSONL + SQLite index).
data_dir = "~/.local/share/agent-gate"
# How often to rotate JSONL: "daily" | "weekly" | "never"
rotate = "daily"
# Compress rotated files older than this duration.
gzip_after = "1d"

[allowlist]
# When true, the proxy returns 403 for any request whose host isn't in
# allowlist.txt. When false (default), unknown hosts pass but flag the event.
enforce = false

[rules]
# Disable specific built-in rules by ID. Use ` + "`agent-gate doctor`" + ` to list IDs.
disable = []
`
}
