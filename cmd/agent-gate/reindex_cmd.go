package main

import (
	"context"
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

func reindexCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the PII count index from the JSONL log",
		Long: `Walks every event in the audit log and recomputes its PII
counts. Useful after upgrading agent-gate to a release that
adds new PII detectors, or after manually deleting event_pii.

This command is foreground; cancel with Ctrl-C.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReindex(configPath)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config",
		filepath.Join(home, ".config", "agent-gate", "config.toml"),
		"Path to config.toml")
	return cmd
}

func runReindex(configPath string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Storage.DataDir, time.Now)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "cancelling...")
		cancel()
	}()

	var eventCount int
	if err := st.Index().Db().QueryRow(`SELECT count(*) FROM events`).Scan(&eventCount); err != nil {
		return fmt.Errorf("count events: %w", err)
	}
	fmt.Fprintf(os.Stderr, "reindex: scanning %d events...\n", eventCount)

	if err := st.ReindexPII(ctx); err != nil {
		return fmt.Errorf("reindex: %w", err)
	}

	fmt.Fprintf(os.Stdout, "reindex complete: indexed %d events\n", eventCount)
	return nil
}
