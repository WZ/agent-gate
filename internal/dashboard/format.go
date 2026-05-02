package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// formatBody applies content-aware pretty-printing to a captured request or
// response body. JSON gets indented. SSE event-stream payloads get each
// `data:` line's JSON pretty-printed. Anything else passes through unchanged.
//
// Formatting is best-effort. If parsing fails (truncated capture, redactor
// markers in unfortunate spots, or genuinely non-JSON content under a JSON
// content-type), we return the input unchanged rather than guess.
func formatBody(body string, headers http.Header) string {
	if body == "" {
		return body
	}
	switch detectContentKind(headers) {
	case kindJSON:
		return prettyJSON(body)
	case kindEventStream:
		return prettyEventStream(body)
	default:
		return body
	}
}

type contentKind int

const (
	kindOther contentKind = iota
	kindJSON
	kindEventStream
)

func detectContentKind(h http.Header) contentKind {
	ct := strings.ToLower(h.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "application/json"),
		strings.Contains(ct, "+json"):
		return kindJSON
	case strings.Contains(ct, "text/event-stream"):
		return kindEventStream
	}
	return kindOther
}

// prettyJSON indents JSON with 2-space indentation. Returns the input
// unchanged if it does not parse — captured bodies can be truncated or
// contain redactor markers that break parsing in rare cases.
func prettyJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// prettyEventStream walks an SSE stream line-by-line and pretty-prints any
// JSON payload that follows a `data:` field. Comment lines (`:`...) and
// other SSE fields (`event:`, `id:`, `retry:`) pass through unchanged.
//
// Multi-line `data:` accumulation per the SSE spec is intentionally not
// implemented — Anthropic's stream uses one JSON object per `data:` line,
// which is the case we need to read clearly.
func prettyEventStream(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 64)
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if payload, ok := strings.CutPrefix(trimmed, "data: "); ok {
			pretty := prettyJSON(payload)
			if pretty != payload {
				out.WriteString("data: ")
				out.WriteString(pretty)
				out.WriteByte('\n')
				continue
			}
		}
		out.WriteString(trimmed)
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}
