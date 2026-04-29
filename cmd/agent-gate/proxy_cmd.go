package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-gate/internal/ca"
	"agent-gate/internal/config"
	"agent-gate/internal/idgen"
	"agent-gate/internal/parser"
	"agent-gate/internal/proxy"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/spf13/cobra"
)

func proxyCmd() *cobra.Command {
	var (
		configPath  string
		captureMode string
		addr        string
	)
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run the TLS-intercepting proxy (foreground)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(configPath, captureMode, addr)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	cmd.Flags().StringVar(&captureMode, "capture-mode", "permissive", `"airtight" or "permissive"; recorded on every event`)
	cmd.Flags().StringVar(&addr, "addr", "", "Override proxy listen addr (default from config: 127.0.0.1:<port>)")
	return cmd
}

func runProxy(configPath, captureMode, addrOverride string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	caDir := filepath.Join(filepath.Dir(configPath), "ca")
	root, err := ca.Ensure(caDir)
	if err != nil {
		return fmt.Errorf("ca: %w", err)
	}

	st, err := store.Open(cfg.Storage.DataDir, time.Now)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	flowCh := make(chan types.RawFlow, 64)
	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", cfg.Ports.Proxy)
	}
	if !isLoopbackAddr(addr) {
		return fmt.Errorf("refusing to bind non-loopback addr %q (security policy)", addr)
	}

	// Pipeline goroutine: parse + persist.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case f, ok := <-flowCh:
				if !ok {
					return
				}
				ev := parser.Parse(f)
				stored := types.StoredEvent{ParsedEvent: ev}
				if err := st.Append(stored); err != nil {
					fmt.Fprintf(os.Stderr, "store append: %v\n", err)
				}
			}
		}
	}()

	// Signal handling for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "shutting down...")
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "agent-gate proxy listening on %s (capture-mode=%s)\n", addr, captureMode)

	if err := proxy.Run(proxy.Options{
		Addr:        addr,
		CA:          root,
		Out:         flowCh,
		IDGen:       idgen.NewGenerator(),
		CaptureMode: captureMode,
		Logger:      func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}); err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	close(flowCh)
	<-done
	return nil
}

func isLoopbackAddr(addr string) bool {
	// addr format "host:port"
	for _, prefix := range []string{"127.", "::1", "localhost:"} {
		if len(addr) >= len(prefix) && addr[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
