package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/config"
	"agent-gate/internal/dashboard"
	"agent-gate/internal/denylist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/passthrough"
	"agent-gate/internal/store"
	"github.com/spf13/cobra"
)

func dashboardCmd() *cobra.Command {
	var (
		configPath string
		addr       string
	)
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Run the local web dashboard (foreground)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDashboard(configPath, addr)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	cmd.Flags().StringVar(&addr, "addr", "", "Override dashboard listen addr (default from config: 127.0.0.1:<port>)")
	return cmd
}

func runDashboard(configPath, addrOverride string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Storage.DataDir, time.Now)
	if err != nil {
		return err
	}
	defer st.Close()

	// First-launch (or post-upgrade) auto-reindex: if event_pii is behind
	// the events table, or if a schema migration added derived SQLite
	// columns, kick off a synchronous reindex from JSONL truth. This is
	// bounded — roughly 50ms × event count — so even a 5000-event corpus
	// takes a few seconds. Background reindexing with SSE-driven progress
	// is the upgrade path documented in the spec.
	if ran, err := st.MaybeReindex(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: reindex failed: %v\n", err)
	} else if ran {
		fmt.Fprintln(os.Stderr, "reindex complete")
	}

	configDir := filepath.Dir(configPath)
	al, err := allowlist.Load(filepath.Join(configDir, "allowlist.txt"))
	if err != nil {
		return err
	}
	dl, err := denylist.Load(filepath.Join(configDir, "denylist.txt"))
	if err != nil {
		return err
	}
	pt, err := passthrough.Load(filepath.Join(configDir, "passthrough.txt"))
	if err != nil {
		return err
	}
	di, err := dismissals.Load(filepath.Join(configDir, "dismissals.json"))
	if err != nil {
		return err
	}

	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", cfg.Ports.Dashboard)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "shutting down...")
		ln.Close()
	}()

	fmt.Fprintf(os.Stderr, "agent-gate dashboard listening on http://%s\n", addr)
	return dashboard.Run(dashboard.Options{
		Listener: ln, Addr: addr,
		Store: st, Allowlist: al, Denylist: dl, Passthrough: pt, Dismissals: di,
	})
}
