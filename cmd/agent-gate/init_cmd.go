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
	return `[capture]
default_mode = "airtight"

[ports]
proxy = 8888
dashboard = 7878

[storage]
data_dir = "~/.local/share/agent-gate"
rotate = "daily"
gzip_after = "1d"

[allowlist]
file = "~/.config/agent-gate/allowlist.txt"

[rules]
disable = []
`
}
