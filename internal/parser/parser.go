package parser

import (
	"net/url"
	"strings"

	"agent-gate/internal/types"
)

// Parse decodes a RawFlow into a ParsedEvent. Always succeeds — never panics; never returns an
// error. If decoding fails partially, fields stay zero-valued. The caller can attach
// `parse_error` flags via the policy layer.
func Parse(flow types.RawFlow) types.ParsedEvent {
	return ParseFlow(flow)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}
