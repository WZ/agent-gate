package agentdetect

import (
	"net/url"
	"strings"
)

// EnvBinding ties an env var to an agent and a default host.
type EnvBinding struct {
	Var         string
	Agent       string
	DefaultHost string
}

var KnownEnvVars = []EnvBinding{
	{Var: "ANTHROPIC_BASE_URL", Agent: "claude", DefaultHost: "api.anthropic.com"},
	{Var: "ANTHROPIC_API_URL", Agent: "claude", DefaultHost: "api.anthropic.com"},
	{Var: "OPENAI_BASE_URL", Agent: "codex", DefaultHost: "api.openai.com"},
	{Var: "OPENAI_API_URL", Agent: "codex", DefaultHost: "api.openai.com"},
}

// extractHost parses urlStr and returns the bare hostname (no port). Returns
// empty string for inputs we should treat as unsafe: missing scheme, empty
// host, control characters, or failed IDN normalization.
func extractHost(urlStr string, idnLookup func(string) (string, error)) string {
	if urlStr == "" {
		return ""
	}
	if hasControlChar(urlStr) {
		return ""
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	host = strings.ToLower(host)
	if hasControlChar(host) {
		return ""
	}
	normalized, err := idnLookup(host)
	if err != nil {
		return ""
	}
	if hasControlChar(normalized) {
		return ""
	}
	return normalized
}

func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
