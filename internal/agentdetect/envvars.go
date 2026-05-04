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
// host, control characters, non-ASCII (homograph defense), or failed IDN
// normalization.
//
// Why reject non-ASCII outright: agent-gate's threat model is "what your AI
// agent is reaching out to." All known AI-vendor hostnames are pure ASCII.
// A user-set ANTHROPIC_BASE_URL that contains any non-ASCII codepoint is
// either a typo, a typosquat, or a deliberate IDN homograph (e.g. Cyrillic а
// looking like Latin a). idna.Lookup.ToASCII would silently punycode the
// homograph into a different domain that the user never typed; we'd end up
// seeding `xn--pi-6kc.anthropic.com` into allowlist.txt and the user would
// think they trusted api.anthropic.com. Refuse instead.
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
	if hasNonASCII(host) {
		return ""
	}
	normalized, err := idnLookup(host)
	if err != nil {
		return ""
	}
	if hasControlChar(normalized) {
		return ""
	}
	if normalized != host {
		// idnLookup mutated the input. With the non-ASCII gate above, this
		// should be unreachable, but treat any silent mutation as suspicious.
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

func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return true
		}
	}
	return false
}
