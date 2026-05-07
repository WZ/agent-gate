package launcher

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
	lock, err := rt.AcquireLockfile(lockPath)
	if err != nil {
		return 1, err
	}
	defer lock.Release()

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

	upstreamRoots, err := loadUpstreamRoots(opts.UpstreamCAFile)
	if err != nil {
		return 1, err
	}
	if opts.UpstreamInsecureSkipVerify {
		fmt.Fprintln(os.Stderr, "⚠ upstream TLS verification DISABLED. Captures still happen, but upstream identity is NOT validated. Use only for testing self-hosted endpoints.")
	}

	enforce := common.Cfg.Allowlist.Enforce
	if opts.EnforceAllowlist != nil {
		enforce = *opts.EnforceAllowlist
	}
	hostGuard := func(host string) bool {
		if host == "" {
			return false
		}
		// Denylist always blocks. Allowlist enforcement is the second tier.
		if common.Denylist != nil && common.Denylist.Contains(host) {
			return true
		}
		if enforce && !common.Allowlist.Contains(host) {
			return true
		}
		return false
	}
	passthroughHost := func(host string) bool {
		// Denylist wins over passthrough: a blocked host should always 403,
		// even if the user also tagged it passthrough. Forcing MITM here
		// lets HostGuard (request-time) emit the synthetic 403 with body.
		if common.Denylist != nil && common.Denylist.Contains(host) {
			return false
		}
		return common.Passthrough != nil && common.Passthrough.Contains(host)
	}
	hijackHost := buildHijackHostPredicate(common, opts.HijackHosts)
	if enforce {
		fmt.Fprintln(os.Stderr, "allowlist enforcement ON: requests to non-allowlisted hosts will receive 403 from the proxy")
	}

	baseProxyRunConfig := proxyRunConfig{
		common:                     common,
		out:                        flowCh,
		captureMode:                captureMode,
		upstreamRoots:              upstreamRoots,
		upstreamInsecureSkipVerify: opts.UpstreamInsecureSkipVerify,
		hostGuard:                  hostGuard,
		passthroughHost:            passthroughHost,
		hijackHost:                 hijackHost,
		logger:                     func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	}

	proxyDone := make(chan error, 1)
	go func() {
		defer recoverPanic(opts.proxyHook, "proxy", cancel)
		proxyDone <- proxy.Run(proxyOptionsForListener(proxyRunConfigWithListener(baseProxyRunConfig, proxyLn)))
	}()

	// 5b. Allocate the netns-listener channel; on Linux, spawnAirtight will send
	// a netns-bound listener on it. On other platforms, no one sends; the goroutine
	// below sits on the select forever and exits on ctx cancellation.
	opts.nsListener = make(chan net.Listener, 1)
	go func() {
		select {
		case nsLn := <-opts.nsListener:
			if nsLn == nil {
				return
			}
			if err := proxy.Run(proxyOptionsForListener(proxyRunConfigWithListener(baseProxyRunConfig, nsLn))); err != nil && !errors.Is(err, net.ErrClosed) {
				fmt.Fprintf(os.Stderr, "ns proxy: %v\n", err)
			}
		case <-supCtx.Done():
			return
		}
	}()

	// 6. Start dashboard goroutine. ADAPTED: dashboard.NewServer returns
	// http.Handler, so we wrap it in our own *http.Server for Shutdown support.
	dashAddr := opts.DashboardAddr
	if dashAddr == "" {
		dashAddr = fmt.Sprintf("127.0.0.1:%d", common.Cfg.Ports.Dashboard)
	}
	// Try to bind the dashboard port. If it's already in use, probe it:
	// if a previous `agent-gate dashboard` (or another `agent-gate run`)
	// already owns the port, reuse it instead of failing the run. This
	// lets users keep a long-lived dashboard open between agent runs.
	// If the port is held by something that isn't us, surface the bind
	// error as before — we don't want to silently route around an
	// unrelated process listening on the audit port.
	dashLn, err := net.Listen("tcp", dashAddr)
	var dashHTTP *http.Server
	if err != nil {
		if isAgentGateDashboard(dashAddr) {
			fmt.Fprintf(os.Stderr,
				"dashboard already running at http://%s — reusing it (this run won't start a second one)\n",
				dashAddr)
		} else {
			cancel()
			_ = proxyLn.Close()
			<-proxyDone
			// flowCh intentionally not closed — see teardown comment.
			<-pipelineDone
			return 1, fmt.Errorf("dashboard listener bind %q: %w", dashAddr, err)
		}
	} else {
		dashHandler := dashboard.NewServer(dashboard.Options{
			Addr:        dashAddr,
			Store:       common.Store,
			Allowlist:   common.Allowlist,
			Denylist:    common.Denylist,
			Passthrough: common.Passthrough,
			Dismissals:  common.Dismiss,
		})
		dashHTTP = &http.Server{Handler: dashHandler, ReadHeaderTimeout: 30 * time.Second}
		go func() {
			defer recoverPanic(opts.dashboardHook, "dashboard", func() {})
			if err := dashHTTP.Serve(dashLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
			}
		}()
	}

	// 7. Carry resolved addresses back into opts so spawnAirtight/spawnPermissive
	// can read the actual port (e.g. for the sandbox profile).
	opts.ProxyAddr = proxyAddr
	opts.DashboardAddr = dashAddr

	// 8. Build child env.
	childEnv := buildChildEnv(opts.Env, proxyAddr)

	// 9. Spawn child.
	var child *childHandle
	switch opts.Mode {
	case Airtight:
		child, err = spawnAirtight(supCtx, opts, childEnv)
	default:
		child, err = spawnPermissive(supCtx, opts, childEnv)
	}
	if err != nil {
		cancel()
		if dashHTTP != nil {
			_ = dashHTTP.Close()
		}
		_ = proxyLn.Close()
		<-proxyDone
		// flowCh intentionally not closed — see teardown comment.
		<-pipelineDone
		return 1, fmt.Errorf("spawn child: %w", err)
	}

	fmt.Fprint(os.Stderr, runStatusLine(captureMode, proxyAddr, dashAddr))

	// 10. Signal handler.
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

	// 11. Wait for child exit or ctx cancel.
	exitCode, waitErr := child.wait(supCtx)
	if errors.Is(waitErr, context.Canceled) {
		_ = child.kill()
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		exitCode, _ = child.wait(killCtx)
		killCancel()
	}

	// 12. Teardown.
	cancel()
	teardown(proxyLn, proxyDone, flowCh, pipelineDone, dashHTTP)

	return exitCode, nil
}

func runStatusLine(captureMode, proxyAddr, dashAddr string) string {
	return fmt.Sprintf("agent-gate run: %s mode; proxy %s; dashboard %s\n", captureMode, proxyAddr, dashboardURL(dashAddr))
}

func dashboardURL(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

func teardown(proxyLn net.Listener, proxyDone <-chan error,
	flowCh chan types.RawFlow, pipelineDone <-chan struct{}, dashHTTP *http.Server) {

	_ = proxyLn.Close()
	waitWithTimeout(proxyDone, 2*time.Second)

	// flowCh is intentionally NOT closed. proxy.Run returns as soon as the
	// listener errors on Accept, but goproxy's per-connection goroutines
	// (handleHttps + spawned response handlers) may still be in flight and
	// trying to write to flowCh. Closing here caused panics like
	// "panic: send on closed channel" in goproxy.FuncRespHandler.Handle on
	// Ctrl-C exits. The pipeline exits via supCtx cancellation (RunPipeline
	// drains buffered flows then returns); any handler goroutines that are
	// still blocked on flowCh are reaped when the supervisor returns.
	_ = flowCh
	waitClose(pipelineDone, 2*time.Second)

	if dashHTTP != nil {
		shutdownCtx, c := context.WithTimeout(context.Background(), 1*time.Second)
		defer c()
		_ = dashHTTP.Shutdown(shutdownCtx)
	}
}

// isAgentGateDashboard probes addr with a short HTTP GET and returns true
// when the response looks like an agent-gate dashboard. Used to decide
// whether a port-bind collision is a friendly one (reuse) or a genuine
// conflict (fail). Conservative: any non-2xx, missing signature, or
// network error returns false so we don't silently skip the dashboard
// when something else is listening on the port.
func isAgentGateDashboard(addr string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return strings.Contains(string(buf[:n]), "agent-gate")
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
	cmd.SysProcAttr = ttyAwareSysProcAttr(opts.Stdin)
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
		switch strings.ToUpper(keyOf(kv)) {
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

type proxyRunConfig struct {
	listener                   net.Listener
	common                     *rt.Common
	out                        chan<- types.RawFlow
	captureMode                string
	upstreamRoots              *x509.CertPool
	upstreamInsecureSkipVerify bool
	hostGuard                  func(string) bool
	passthroughHost            func(string) bool
	hijackHost                 func(string) bool
	logger                     func(string, ...any)
}

func proxyRunConfigWithListener(c proxyRunConfig, listener net.Listener) proxyRunConfig {
	c.listener = listener
	return c
}

func proxyOptionsForListener(c proxyRunConfig) proxy.Options {
	opts := proxy.Options{
		Listener:                   c.listener,
		Out:                        c.out,
		IDGen:                      idgen.NewGenerator(),
		CaptureMode:                c.captureMode,
		UpstreamRootCAs:            c.upstreamRoots,
		UpstreamInsecureSkipVerify: c.upstreamInsecureSkipVerify,
		HostGuard:                  c.hostGuard,
		PassthroughHost:            c.passthroughHost,
		HijackHost:                 c.hijackHost,
		Logger:                     c.logger,
	}
	if c.common != nil {
		opts.CA = c.common.CA
	}
	return opts
}

// buildHijackHostPredicate returns a closure that the proxy consults on every
// CONNECT to decide whether to hijack the conn for frame-aware capture. Empty
// hosts means no hijack at all. Denylisted hosts never get hijacked — they
// flow through the standard MITM path so HostGuard can synthesize the 403.
func buildHijackHostPredicate(common *rt.Common, hosts []string) func(string) bool {
	if len(hosts) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			set[h] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return func(host string) bool {
		if common != nil && common.Denylist != nil && common.Denylist.Contains(host) {
			return false
		}
		_, ok := set[strings.ToLower(host)]
		return ok
	}
}

func execArgv(exe string, args []string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, exe)
	out = append(out, args...)
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

// loadUpstreamRoots returns a cert pool to use for the proxy→upstream
// connection. If caFile is empty, returns nil — the proxy uses Go's
// default (system) trust store. If caFile is set, we start from the
// system pool (so api.anthropic.com still verifies normally) and
// additionally trust the PEM cert(s) in the file.
func loadUpstreamRoots(caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		return nil, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read upstream CA file %q: %w", caFile, err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("upstream CA file %q contains no valid PEM certificates", caFile)
	}
	return pool, nil
}
