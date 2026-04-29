package redactor

import (
	"net/http"
	"sort"
	"strings"

	"agent-gate/internal/secrets"
)

// Redact replaces every secret-pattern match in s with a placeholder marker.
// The marker preserves enough context for a reviewer to see what kind of secret was here,
// without exposing the actual bytes.
func Redact(s string) string {
	body := []byte(s)
	matches := secrets.FindAll(body)
	if len(matches) == 0 {
		return s
	}

	// Sort matches by Start, then End to handle overlaps correctly
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start != matches[j].Start {
			return matches[i].Start < matches[j].Start
		}
		return matches[i].End < matches[j].End
	})

	var b strings.Builder
	last := 0
	for _, m := range matches {
		if m.Start < last {
			continue // overlap with previous; skip
		}
		b.WriteString(s[last:m.Start])
		// Use first 6 bytes of the match as the visible "type stub".
		end := m.Start + 6
		if end > m.End {
			end = m.End
		}
		b.WriteString("«REDACTED:")
		b.WriteString(m.PatternCode)
		b.WriteString(":")
		b.WriteString(s[m.Start:end])
		b.WriteString("•••»")
		last = m.End
	}
	b.WriteString(s[last:])
	return b.String()
}

// RedactHeaders returns a copy of h with sensitive header values masked.
// The original header is unchanged. Header names are matched case-insensitively
// using http.Header semantics.
func RedactHeaders(h http.Header) http.Header {
	masked := http.Header{}
	for k, v := range h {
		if isSensitiveHeader(k) {
			masked[k] = []string{"«REDACTED»"}
		} else {
			masked[k] = append([]string(nil), v...)
		}
	}
	return masked
}

var sensitiveHeaderNames = map[string]struct{}{
	"Authorization": {}, "Cookie": {}, "Set-Cookie": {},
	"X-Api-Key": {}, "X-Auth-Token": {}, "Proxy-Authorization": {},
}

func isSensitiveHeader(name string) bool {
	_, ok := sensitiveHeaderNames[http.CanonicalHeaderKey(name)]
	return ok
}
