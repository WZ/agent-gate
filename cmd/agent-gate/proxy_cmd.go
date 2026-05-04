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

	"agent-gate/internal/idgen"
	"agent-gate/internal/proxy"
	"agent-gate/internal/runtime"
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
	rt, err := runtime.LoadCommon(configPath)
	if err != nil {
		return err
	}
	defer rt.Close()

	addr := addrOverride
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", rt.Cfg.Ports.Proxy)
	}
	if !isLoopbackAddr(addr) {
		return fmt.Errorf("refusing to bind non-loopback addr %q (security policy)", addr)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	flowCh := make(chan types.RawFlow, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pipelineDone := make(chan struct{})
	go func() {
		defer close(pipelineDone)
		runtime.RunPipeline(ctx, rt, flowCh)
	}()

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
		CA:                         rt.CA,
		Out:                        flowCh,
		IDGen:                      idgen.NewGenerator(),
		CaptureMode:                captureMode,
		UpstreamInsecureSkipVerify: upstreamInsecure,
		PassthroughHost: func(host string) bool {
			return rt.Passthrough != nil && rt.Passthrough.Contains(host)
		},
		Logger: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	})
	// flowCh is intentionally NOT closed: in-flight goproxy goroutines may
	// outlive proxy.Run (which returns as soon as the listener closes) and
	// still write to flowCh. Closing here caused "send on closed channel"
	// panics on Ctrl-C. The pipeline exits via ctx cancellation instead.
	<-pipelineDone

	if runErr != nil && !errors.Is(runErr, net.ErrClosed) {
		return fmt.Errorf("proxy: %w", runErr)
	}
	return nil
}

func isLoopbackAddr(addr string) bool {
	for _, prefix := range []string{"127.", "::1", "localhost:"} {
		if len(addr) >= len(prefix) && addr[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
