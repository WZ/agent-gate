package dashboard

import "testing"

func TestFlagLabelForKnownCodes(t *testing.T) {
	cases := map[string]string{
		"host_not_allowlisted": "Host not allowlisted",
		"secret_in_request":    "Secret in request",
		"env_in_tool_result":   "Env in tool result",
		"oversized_request":    "Oversized request",
		"oversized_response":   "Oversized response",
		"unknown_mcp_endpoint": "Unknown MCP endpoint",
		"permissive_capture":   "Permissive capture",
		"parse_error":          "Parse error",
	}
	for code, want := range cases {
		if got := flagLabelFor(code); got != want {
			t.Errorf("flagLabelFor(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestFlagLabelForUnknownCodeReturnsRaw(t *testing.T) {
	// Future rule codes must still render — fall through to the raw identifier
	// so no flag ever shows up blank in the dashboard.
	if got := flagLabelFor("future_rule_code"); got != "future_rule_code" {
		t.Errorf("flagLabelFor(unknown) = %q, want raw passthrough", got)
	}
	if got := flagLabelFor(""); got != "" {
		t.Errorf("flagLabelFor(\"\") = %q, want empty passthrough", got)
	}
}
