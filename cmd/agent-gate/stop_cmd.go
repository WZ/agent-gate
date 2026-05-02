package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"agent-gate/internal/config"
	"github.com/spf13/cobra"
)

func stopCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a running `agent-gate run` (reads lockfile, sends SIGTERM)",
		Long: `Reads the data-dir lockfile to find the active agent-gate run PID
and sends SIGTERM. Use this if Ctrl-C in the agent's TUI was caught by the
TUI itself and didn't propagate.

If the recorded PID is no longer running, the stale lockfile is removed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadFromFile(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			lockPath := filepath.Join(cfg.Storage.DataDir, "agent-gate.lock")
			data, err := os.ReadFile(lockPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Fprintln(os.Stderr, "agent-gate stop: no lockfile (nothing to stop)")
					return nil
				}
				return fmt.Errorf("read lockfile %s: %w", lockPath, err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid <= 0 {
				return fmt.Errorf("invalid lockfile contents at %s: %q", lockPath, data)
			}
			p, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("FindProcess %d: %w", pid, err)
			}
			if err := p.Signal(syscall.SIGTERM); err != nil {
				if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
					_ = os.Remove(lockPath)
					fmt.Fprintf(os.Stderr, "agent-gate stop: PID %d already gone; removed stale lockfile\n", pid)
					return nil
				}
				return fmt.Errorf("SIGTERM %d: %w", pid, err)
			}
			fmt.Fprintf(os.Stderr, "agent-gate stop: SIGTERM sent to PID %d\n", pid)
			return nil
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	return cmd
}
