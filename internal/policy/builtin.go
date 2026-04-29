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

// OversizedRequestRule fires when the request body exceeds Limit bytes.
type OversizedRequestRule struct{ Limit int64 }

func (OversizedRequestRule) Code() string       { return "oversized_request" }
func (OversizedRequestRule) Severity() Severity { return SevMedium }
func (r OversizedRequestRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	if int64(len(ev.RawFlow.ReqBody)) > r.Limit {
		return true, fmt.Sprintf("request body %d bytes exceeds limit %d", len(ev.RawFlow.ReqBody), r.Limit)
	}
	return false, ""
}

// OversizedResponseRule fires when the response body exceeds Limit bytes.
type OversizedResponseRule struct{ Limit int64 }

func (OversizedResponseRule) Code() string       { return "oversized_response" }
func (OversizedResponseRule) Severity() Severity { return SevLow }
func (r OversizedResponseRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	if int64(len(ev.RawFlow.RespBody)) > r.Limit {
		return true, fmt.Sprintf("response body %d bytes exceeds limit %d", len(ev.RawFlow.RespBody), r.Limit)
	}
	return false, ""
}

// UnknownMCPEndpointRule fires when a response is text/event-stream and the
// host is not in the known-MCP allowlist.
type UnknownMCPEndpointRule struct{ knownMCP map[string]struct{} }

func NewUnknownMCPEndpointRule(known map[string]struct{}) Rule {
	return UnknownMCPEndpointRule{knownMCP: known}
}

func (UnknownMCPEndpointRule) Code() string       { return "unknown_mcp_endpoint" }
func (UnknownMCPEndpointRule) Severity() Severity { return SevMedium }
func (r UnknownMCPEndpointRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	if !isSSEHeader(ev.RawFlow.RespHeaders) {
		return false, ""
	}
	host := hostFromURL(ev.RawFlow.URL)
	if _, ok := r.knownMCP[host]; ok {
		return false, ""
	}
	return true, "SSE response from " + host + " (not in known MCP list)"
}

func isSSEHeader(h map[string][]string) bool {
	for _, v := range h["Content-Type"] {
		if strings.Contains(strings.ToLower(v), "text/event-stream") {
			return true
		}
	}
	return false
}

// ParseErrorRule fires when the parser left an error annotation on the raw flow.
type ParseErrorRule struct{}

func (ParseErrorRule) Code() string       { return "parse_error" }
func (ParseErrorRule) Severity() Severity { return SevInfo }
func (ParseErrorRule) Evaluate(ev *types.ParsedEvent) (bool, string) {
	if ev.RawFlow.Err != "" {
		return true, ev.RawFlow.Err
	}
	return false, ""
}
