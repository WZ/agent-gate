package policy

import (
	"fmt"
	"net/url"
	"strings"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/types"
)

type Severity string

const (
	SevHigh   Severity = "high"
	SevMedium Severity = "medium"
	SevLow    Severity = "low"
	SevInfo   Severity = "info"
)

type Rule interface {
	Code() string
	Severity() Severity
	Evaluate(*types.ParsedEvent) (matched bool, detail string)
}

type Engine struct {
	rules      []Rule
	allowlist  *allowlist.Allowlist
	dismissals *dismissals.Dismissals
}

func NewEngine(al *allowlist.Allowlist, di *dismissals.Dismissals, rules ...Rule) *Engine {
	return &Engine{rules: rules, allowlist: al, dismissals: di}
}

func (e *Engine) Allowlist() *allowlist.Allowlist { return e.allowlist }

// Evaluate runs every rule and returns the flags that fired, with dismissed
// flags filtered out. A rule that panics produces a `rule_error` flag and does
// not stop evaluation.
func (e *Engine) Evaluate(ev *types.ParsedEvent) []types.Flag {
	var flags []types.Flag
	for _, r := range e.rules {
		matched, detail := safeEvaluate(r, ev)
		if !matched {
			continue
		}
		code := r.Code()
		severity := string(r.Severity())
		if isRuleErrorDetail(detail) {
			code = "rule_error"
			severity = string(SevInfo)
		}
		host := hostFromURL(ev.RawFlow.URL)
		if e.dismissals != nil && e.dismissals.Has(ev.ID, code, host) {
			continue
		}
		flags = append(flags, types.Flag{Code: code, Severity: severity, Detail: detail})
	}
	return flags
}

func safeEvaluate(r Rule, ev *types.ParsedEvent) (matched bool, detail string) {
	defer func() {
		if rec := recover(); rec != nil {
			matched = true
			detail = fmt.Sprintf("rule %q panicked: %v", r.Code(), rec)
		}
	}()
	return r.Evaluate(ev)
}

func isRuleErrorDetail(d string) bool { return strings.HasPrefix(d, "rule \"") }

func hostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	h := u.Host
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return strings.ToLower(h)
}
