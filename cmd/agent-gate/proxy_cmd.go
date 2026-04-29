package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/ca"
	"agent-gate/internal/config"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/idgen"
	"agent-gate/internal/parser"
	"agent-gate/internal/policy"
	"agent-gate/internal/proxy"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/spf13/cobra"
)

func proxyCmd() *cobra.Command {
	var (
		configPath              string
		captureMode             string
		addr                    string
		upstreamInsecureSkipVer bool
	)
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run the TLS-intercepting proxy (foreground)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProxy(configPath, captureMode, addr, upstreamInsecureSkipVer)
		},
	}
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&configPath, "config", filepath.Join(home, ".config", "agent-gate", "config.toml"), "Path to config.toml")
	cmd.Flags().StringVar(&captureMode, "capture-mode", "permissive", `"airtight" or "permissive"; recorded on every event`)
	cmd.Flags().StringVar(&addr, "addr", "", "Override proxy listen addr (default from config: 127.0.0.1:<port>)")
	cmd.Flags().BoolVar(&upstreamInsecureSkipVer, "upstream-insecure-skip-verify", false, `skip TLS verification on the proxy→upstream connection. Testing only — captures the data but you can't trust the upstream identity.`)
	return cmd
}

func runProxy(configPath, captureMode, addrOverride string, upstreamInsecure bool) error {
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

	// Load allowlist + dismissals (file-backed; missing files are fine).
	configDir := filepath.Dir(configPath)
	al, err := allowlist.Load(filepath.Join(configDir, "allowlist.txt"))
	if err != nil {
		return fmt.Errorf("load allowlist: %w", err)
	}
	// Default-trust the Anthropic API on first run so existing setups aren't noisy.
	if !al.Contains("api.anthropic.com") {
		if err := al.Add("api.anthropic.com"); err != nil {
			fmt.Fprintf(os.Stderr, "seed allowlist: %v\n", err)
		}
	}
	di, err := dismissals.Load(filepath.Join(configDir, "dismissals.json"))
	if err != nil {
		return fmt.Errorf("load dismissals: %w", err)
	}

	// Engine with the eight built-in rules.
	engine := policy.NewEngine(al, di,
		policy.NewHostNotAllowlistedRule(al),
		policy.PermissiveCaptureRule{},
		policy.SecretInRequestRule{},
		policy.EnvInToolResultRule{},
		policy.OversizedRequestRule{Limit: 5 << 20},
		policy.OversizedResponseRule{Limit: 5 << 20},
		policy.NewUnknownMCPEndpointRule(map[string]struct{}{}),
		policy.ParseErrorRule{},
	)

	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", cfg.Ports.Proxy)
	}
	if !isLoopbackAddr(addr) {
		return fmt.Errorf("refusing to bind non-loopback addr %q (security policy)", addr)
	}

	// Bind the listener up front so we can close it from the signal handler.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	flowCh := make(chan types.RawFlow, 64)

	// Pipeline goroutine: parse + evaluate + persist.
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
				flags := engine.Evaluate(&ev)
				stored := types.StoredEvent{ParsedEvent: ev, Flags: flags}
				if err := st.Append(stored); err != nil {
					fmt.Fprintf(os.Stderr, "store append: %v\n", err)
				}
			}
		}
	}()

	// Signal handling: close the listener (which makes proxy.Run return) and cancel ctx.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "shutting down...")
		ln.Close()
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "agent-gate proxy listening on %s (capture-mode=%s)\n", addr, captureMode)

	if upstreamInsecure {
		fmt.Fprintln(os.Stderr, "⚠ upstream TLS verification DISABLED. Captures still happen, but upstream identity is NOT validated. Use only for testing self-hosted endpoints.")
	}

	runErr := proxy.Run(proxy.Options{
		Listener:                   ln,
		CA:                         root,
		Out:                        flowCh,
		IDGen:                      idgen.NewGenerator(),
		CaptureMode:                captureMode,
		UpstreamInsecureSkipVerify: upstreamInsecure,
		Logger:                     func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	})
	close(flowCh)
	<-done

	// net.ErrClosed is the expected outcome of graceful shutdown via ln.Close.
	if runErr != nil && !errors.Is(runErr, net.ErrClosed) {
		return fmt.Errorf("proxy: %w", runErr)
	}
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
