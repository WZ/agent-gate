package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-gate/internal/config"
	"agent-gate/internal/store"
	"github.com/spf13/cobra"
)

func tailCmd() *cobra.Command {
	var (
		configPath string
		interval   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Tail captured events as they arrive (polling)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTail(configPath, interval)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	cmd.Flags().DurationVar(&interval, "interval", 1*time.Second, "Poll interval")
	return cmd
}

func runTail(configPath string, interval time.Duration) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Storage.DataDir, time.Now)
	if err != nil {
		return err
	}
	defer st.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)

	since := time.Now().Add(-1 * time.Minute) // start from last minute
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-sig:
			return nil
		case <-t.C:
			rows, err := st.Index().Query(store.QueryFilter{Since: since, Limit: 100})
			if err != nil {
				fmt.Fprintf(os.Stderr, "query: %v\n", err)
				continue
			}
			// rows are newest-first; print oldest-first.
			for i := len(rows) - 1; i >= 0; i-- {
				r := rows[i]
				fmt.Printf("%s  %-6s %s://%s%s  %d  model=%s tokens=%d/%d session=%s\n",
					r.StartedAt.Format(time.RFC3339), r.Method, "https", r.Host, r.Path,
					r.Status, r.Model, r.InputTokens, r.OutputTokens, r.SessionID)
				if r.StartedAt.After(since) {
					since = r.StartedAt
				}
			}
		}
	}
}
