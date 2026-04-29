package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/secrets"
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

// SecretInRequestRule scans the request body for any known credential pattern.
type SecretInRequestRule struct{}

func (SecretInRequestRule) Code() string       { return "secret_in_request" }
func (SecretInRequestRule) Severity() Severity { return SevHigh }
func (SecretInRequestRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	matches := secrets.FindAll(ev.RawFlow.ReqBody)
	if len(matches) == 0 {
		return false, ""
	}
	codes := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		codes[m.PatternCode] = struct{}{}
	}
	codeList := make([]string, 0, len(codes))
	for c := range codes {
		codeList = append(codeList, c)
	}
	sort.Strings(codeList)
	return true, "found " + strings.Join(codeList, ", ") + " in request body"
}

// EnvInToolResultRule fires when at least three KEY=VALUE-shaped lines appear in
// any tool_result. Catches accidental .env paste-back into a model prompt.
type EnvInToolResultRule struct{}

func (EnvInToolResultRule) Code() string       { return "env_in_tool_result" }
func (EnvInToolResultRule) Severity() Severity { return SevHigh }
func (EnvInToolResultRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	for i, tr := range ev.ToolResults {
		count := 0
		for _, line := range strings.Split(tr.Content, "\n") {
			if envLineRegexp.MatchString(strings.TrimSpace(line)) {
				count++
				if count >= 3 {
					return true, fmt.Sprintf("tool_result %d has %d+ KEY=VALUE lines (.env-shaped)", i, count)
				}
			}
		}
	}
	return false, ""
}

var envLineRegexp = regexp.MustCompile(`^[A-Z][A-Z0-9_]*=.+$`)
