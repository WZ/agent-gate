package policy

import (
	"agent-gate/internal/allowlist"
	"agent-gate/internal/types"
)

// hostNotAllowlistedRule fires on every event whose host is not in the allowlist.
type hostNotAllowlistedRule struct{ allowlist *allowlist.Allowlist }

// NewHostNotAllowlistedRule is the public constructor; the type is unexported
// so consumers go through this for clarity.
func NewHostNotAllowlistedRule(al *allowlist.Allowlist) Rule {
	return hostNotAllowlistedRule{allowlist: al}
}

func (hostNotAllowlistedRule) Code() string       { return "host_not_allowlisted" }
func (hostNotAllowlistedRule) Severity() Severity { return SevHigh }
func (r hostNotAllowlistedRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	host := hostFromURL(ev.RawFlow.URL)
	if host == "" {
		return false, ""
	}
	if r.allowlist != nil && r.allowlist.Contains(host) {
		return false, ""
	}
	return true, "host " + host + " is not in the allowlist"
}

// PermissiveCaptureRule emits an info flag on every event captured under
// permissive mode, so reviewers know the trust posture.
type PermissiveCaptureRule struct{}

func (PermissiveCaptureRule) Code() string       { return "permissive_capture" }
func (PermissiveCaptureRule) Severity() Severity { return SevInfo }
func (PermissiveCaptureRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	if ev.RawFlow.CaptureMode == "permissive" {
		return true, "captured under env-only enforcement; airtight mode is recommended"
	}
	return false, ""
}
