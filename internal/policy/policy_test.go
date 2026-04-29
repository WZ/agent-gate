package policy

import (
	"path/filepath"
	"testing"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRule lets us write unit tests without depending on the built-in rule set.
type fakeRule struct {
	code     string
	severity Severity
	match    bool
	detail   string
}

func (r fakeRule) Code() string       { return r.code }
func (r fakeRule) Severity() Severity { return r.severity }
func (r fakeRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	return r.match, r.detail
}

func TestEngineRunsAllRulesAndCollectsFlags(t *testing.T) {
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	e := NewEngine(al, di,
		fakeRule{code: "rule-a", severity: SevHigh, match: true, detail: "fired"},
		fakeRule{code: "rule-b", severity: SevLow, match: false},
		fakeRule{code: "rule-c", severity: SevMedium, match: true, detail: "also fired"},
	)
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "evt-1"}})
	require.Len(t, flags, 2)
	assert.Equal(t, "rule-a", flags[0].Code)
	assert.Equal(t, "high", flags[0].Severity)
	assert.Equal(t, "rule-c", flags[1].Code)
}

func TestEngineSkipsDismissedFlags(t *testing.T) {
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	require.NoError(t, di.Add(dismissals.ScopeGlobalCode, "", "rule-a", "", "noisy"))

	e := NewEngine(al, di,
		fakeRule{code: "rule-a", severity: SevHigh, match: true, detail: "fired"},
		fakeRule{code: "rule-b", severity: SevHigh, match: true, detail: "fired"},
	)
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "evt-1"}})
	require.Len(t, flags, 1)
	assert.Equal(t, "rule-b", flags[0].Code)
}

func TestEngineRecoversFromRulePanic(t *testing.T) {
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)

	panicker := panicRule{code: "panicker"}
	e := NewEngine(al, di,
		panicker,
		fakeRule{code: "ok", severity: SevHigh, match: true, detail: "fine"},
	)
	flags := e.Evaluate(&types.ParsedEvent{})
	codes := flagCodes(flags)
	assert.Contains(t, codes, "ok")
	assert.Contains(t, codes, "rule_error")
}

type panicRule struct{ code string }

func (r panicRule) Code() string                                  { return r.code }
func (r panicRule) Severity() Severity                            { return SevHigh }
func (r panicRule) Evaluate(ev *types.ParsedEvent) (bool, string) { panic("oh no") }

func flagCodes(flags []types.Flag) []string {
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = f.Code
	}
	return out
}

func TestEngineHostScopedDismissalMatchesByHost(t *testing.T) {
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	require.NoError(t, di.Add(dismissals.ScopeHostCode, "", "noisy", "metrics.example.com", "internal"))

	e := NewEngine(al, di, fakeRule{code: "noisy", severity: SevHigh, match: true, detail: "fired"})

	// Different URL on the SAME host should be filtered out by the host-scoped dismissal.
	flags := e.Evaluate(&types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "any", URL: "https://metrics.example.com/v1/different/path"},
	})
	assert.Empty(t, flags, "host-scoped dismissal should match different paths on same host")

	// A different host on the same code should NOT be filtered.
	flags2 := e.Evaluate(&types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "any", URL: "https://other.example.com/x"},
	})
	assert.Len(t, flags2, 1)
}
