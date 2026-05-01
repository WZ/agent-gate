package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-gate/internal/dashboard"
	"agent-gate/internal/idgen"
	"agent-gate/internal/proxy"
	rt "agent-gate/internal/runtime"
	"agent-gate/internal/types"
)

// runSupervised is the entry point of `agent-gate run`. It coordinates the
// proxy goroutine, dashboard goroutine, and the platform child spawn.
func runSupervised(ctx context.Context, opts Options) (int, error) {
	// 1. Load shared startup state.
	common, err := rt.LoadCommon(opts.ConfigPath)
	if err != nil {
		return 1, err
	}
	defer common.Close()

	// 2. Acquire lockfile (only one `agent-gate run` at a time).
	lockPath := filepath.Join(common.Cfg.Storage.DataDir, "agent-gate.lock")
	lock, err := acquireLockfile(lockPath)
	if err != nil {
		return 1, err
	}
	defer lock.release()

	// 3. CA trust check (warn-and-continue).
	if msg, ok := checkCATrusted(common.CA); !ok {
		fmt.Fprintln(os.Stderr, msg)
	}

	// 4. Decide capture mode and feasibility.
	captureMode := string(opts.Mode)
	if opts.Mode == Airtight {
		ok, reason := airtightFeasible()
		if !ok {
			if opts.AirtightFail {
				return 1, fmt.Errorf("airtight required but unavailable: %s", reason)
			}
			fmt.Fprintf(os.Stderr, "airtight unavailable (%s); falling back to permissive\n", reason)
			opts.Mode = Permissive
			captureMode = string(Permissive)
		}
	}

	// 5. Bind proxy listener and start proxy goroutine.
	proxyAddr := opts.ProxyAddr
	if proxyAddr == "" {
		proxyAddr = fmt.Sprintf("127.0.0.1:%d", common.Cfg.Ports.Proxy)
	}
	proxyLn, err := net.Listen("tcp", proxyAddr)
	if err != nil {
		return 1, fmt.Errorf("proxy listener bind %q: %w", proxyAddr, err)
	}

	flowCh := make(chan types.RawFlow, 64)
	supCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	pipelineDone := make(chan struct{})
	go func() {
		defer close(pipelineDone)
		rt.RunPipeline(supCtx, common, flowCh)
	}()

	proxyDone := make(chan error, 1)
	go func() {
		defer recoverPanic(opts.proxyHook, "proxy", cancel)
		proxyDone <- proxy.Run(proxy.Options{
			Listener:    proxyLn,
			CA:          common.CA,
			Out:         flowCh,
			IDGen:       idgen.NewGenerator(),
			CaptureMode: captureMode,
			Logger:      func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
		})
	}()

	// 6. Start dashboard goroutine. ADAPTED: dashboard.NewServer returns
	// http.Handler, so we wrap it in our own *http.Server for Shutdown support.
	dashAddr := opts.DashboardAddr
	if dashAddr == "" {
		dashAddr = fmt.Sprintf("127.0.0.1:%d", common.Cfg.Ports.Dashboard)
	}
	dashHandler := dashboard.NewServer(dashboard.Options{
		Addr:       dashAddr,
		Store:      common.Store,
		Allowlist:  common.Allowlist,
		Dismissals: common.Dismiss,
	})
	dashLn, err := net.Listen("tcp", dashAddr)
	if err != nil {
		proxyLn.Close()
		<-proxyDone
		return 1, fmt.Errorf("dashboard listener bind %q: %w", dashAddr, err)
	}
	dashHTTP := &http.Server{Handler: dashHandler, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		defer recoverPanic(opts.dashboardHook, "dashboard", func() {})
		if err := dashHTTP.Serve(dashLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
		}
	}()

	// 7. Build child env.
	childEnv := buildChildEnv(opts.Env, proxyAddr)

	// 8. Spawn child.
	var child *childHandle
	switch opts.Mode {
	case Airtight:
		child, err = spawnAirtight(supCtx, opts, childEnv)
	default:
		child, err = spawnPermissive(supCtx, opts, childEnv)
	}
	if err != nil {
		_ = dashHTTP.Close()
		proxyLn.Close()
		<-proxyDone
		return 1, fmt.Errorf("spawn child: %w", err)
	}

	fmt.Fprintf(os.Stderr, "agent-gate run: %s mode; proxy %s; dashboard %s\n", captureMode, proxyAddr, dashAddr)

	// 9. Signal handler.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-supCtx.Done():
		}
	}()

	// 10. Wait for child exit or ctx cancel.
	exitCode, waitErr := child.wait(supCtx)
	if errors.Is(waitErr, context.Canceled) {
		_ = child.kill()
		exitCode, _ = child.wait(context.Background())
	}

	// 11. Teardown.
	cancel()
	teardown(proxyLn, proxyDone, flowCh, pipelineDone, dashHTTP)

	return exitCode, nil
}

func teardown(proxyLn net.Listener, proxyDone <-chan error,
	flowCh chan types.RawFlow, pipelineDone <-chan struct{}, dashHTTP *http.Server) {

	_ = proxyLn.Close()
	waitWithTimeout(proxyDone, 2*time.Second)

	close(flowCh)
	waitClose(pipelineDone, 2*time.Second)

	shutdownCtx, c := context.WithTimeout(context.Background(), 1*time.Second)
	defer c()
	_ = dashHTTP.Shutdown(shutdownCtx)
}

func waitWithTimeout(ch <-chan error, d time.Duration) {
	select {
	case <-ch:
	case <-time.After(d):
	}
}

func waitClose(ch <-chan struct{}, d time.Duration) {
	select {
	case <-ch:
	case <-time.After(d):
	}
}

func recoverPanic(hook func(error), label string, cancel context.CancelFunc) {
	if r := recover(); r != nil {
		err := fmt.Errorf("%s panic: %v", label, r)
		fmt.Fprintln(os.Stderr, err)
		if hook != nil {
			hook(err)
		}
		cancel()
	}
}

func spawnPermissive(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	cmd := exec.CommandContext(ctx, opts.Cmd, opts.Args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &childHandle{cmd: cmd}, nil
}

func buildChildEnv(base []string, proxyAddr string) []string {
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		switch keyOf(kv) {
		case "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		if startsWith(kv, "AGENT_GATE_") {
			continue
		}
		out = append(out, kv)
	}
	proxyURL := "http://" + proxyAddr
	out = append(out,
		"HTTPS_PROXY="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"ALL_PROXY="+proxyURL,
		"NO_PROXY=",
	)
	return out
}

func keyOf(kv string) string {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i]
		}
	}
	return kv
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
