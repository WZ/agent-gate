package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/ca"
	"agent-gate/internal/config"
	"agent-gate/internal/denylist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/parser"
	"agent-gate/internal/policy"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
)

// Common bundles the long-lived state shared by `agent-gate proxy` and
// `agent-gate run`. Built once at startup; closed once at shutdown.
type Common struct {
	Cfg       *config.Config
	ConfigDir string
	CA        *ca.CA
	Store     *store.Store
	Allowlist *allowlist.Allowlist
	Denylist  *denylist.Denylist
	Dismiss   *dismissals.Dismissals
	Engine    *policy.Engine
}

// LoadCommon performs all the I/O-heavy startup the proxy and the launcher
// share. Caller MUST call Close() on the returned Common before exit.
func LoadCommon(configPath string) (*Common, error) {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	configDir := filepath.Dir(configPath)
	caDir := filepath.Join(configDir, "ca")
	root, err := ca.Ensure(caDir)
	if err != nil {
		return nil, fmt.Errorf("ca: %w", err)
	}

	st, err := store.Open(cfg.Storage.DataDir, nowFunc)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	al, err := allowlist.Load(filepath.Join(configDir, "allowlist.txt"))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("load allowlist: %w", err)
	}
	if !al.Contains("api.anthropic.com") {
		if err := al.Add("api.anthropic.com"); err != nil {
			fmt.Fprintf(os.Stderr, "seed allowlist: %v\n", err)
		}
	}

	dl, err := denylist.Load(filepath.Join(configDir, "denylist.txt"))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("load denylist: %w", err)
	}

	di, err := dismissals.Load(filepath.Join(configDir, "dismissals.json"))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("load dismissals: %w", err)
	}

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

	return &Common{
		Cfg:       cfg,
		ConfigDir: configDir,
		CA:        root,
		Store:     st,
		Allowlist: al,
		Denylist:  dl,
		Dismiss:   di,
		Engine:    engine,
	}, nil
}

// Close shuts down the store. Safe to call on a nil *Common.
func (c *Common) Close() error {
	if c == nil || c.Store == nil {
		return nil
	}
	return c.Store.Close()
}

// RunPipeline reads RawFlows off `in`, parses, applies policy, and appends to
// the store. Returns when ctx is cancelled and `in` is closed and drained.
func RunPipeline(ctx context.Context, c *Common, in <-chan types.RawFlow) {
	for {
		select {
		case <-ctx.Done():
			// Drain remaining flows before returning so a fast Ctrl-C
			// doesn't lose in-flight events.
			for f := range in {
				c.persist(f)
			}
			return
		case f, ok := <-in:
			if !ok {
				return
			}
			c.persist(f)
		}
	}
}

func (c *Common) persist(f types.RawFlow) {
	ev := parser.Parse(f)
	flags := c.Engine.Evaluate(&ev)
	stored := types.StoredEvent{ParsedEvent: ev, Flags: flags}
	if err := c.Store.Append(stored); err != nil {
		fmt.Fprintf(os.Stderr, "store append: %v\n", err)
	}
}

// nowFunc is the timestamp source; overridable in tests via SetNow.
var nowFunc = defaultNow

func defaultNow() time.Time { return time.Now() }

// SetNow overrides the timestamp source for the runtime package. Tests only.
//
// Must be called BEFORE LoadCommon for the override to reach store.Open
// (which snapshots the function pointer at construction time).
//
// NOT safe for concurrent use; tests using SetNow must not run with
// t.Parallel(). Returns a restore closure to reset to the previous value.
func SetNow(fn func() time.Time) (restore func()) {
	prev := nowFunc
	nowFunc = fn
	return func() { nowFunc = prev }
}
