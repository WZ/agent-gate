package dashboard

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
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

// highlightBody returns the body string with content-aware syntax tokens
// wrapped in <span> elements for color styling. The returned template.HTML
// is HTML-safe — every byte from the input is run through html.EscapeString
// before being emitted.
//
// Content-types not understood here pass through with HTML escaping only.
func highlightBody(s string, headers http.Header) template.HTML {
	if s == "" {
		return ""
	}
	switch detectContentKind(headers) {
	case kindJSON:
		return highlightJSON(s)
	case kindEventStream:
		return highlightEventStream(s)
	default:
		return template.HTML(html.EscapeString(s))
	}
}

// highlightJSON tokenizes already-pretty-printed JSON and wraps each token
// in a <span> with a class. The walker is a simple state machine: it
// recognizes string literals (with backslash escapes), numbers, true/false/
// null literals, and structural punctuation. Anything it doesn't recognize
// (including the redactor's «REDACTED:...» markers when they appear OUTSIDE
// a string — which they should not, but we stay defensive) is HTML-escaped
// and emitted as plain text so rendering never breaks.
func highlightJSON(s string) template.HTML {
	var out strings.Builder
	out.Grow(len(s) * 2)
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			out.WriteByte(c)
			i++
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			out.WriteString(`<span class="json-punct">`)
			out.WriteByte(c)
			out.WriteString(`</span>`)
			i++
		case c == '"':
			start := i
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
			tok := s[start:i]
			class := "json-string"
			// peek past whitespace to check whether this string is an object
			// key (next non-whitespace char is `:`)
			for j := i; j < n; j++ {
				if s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r' {
					continue
				}
				if s[j] == ':' {
					class = "json-key"
				}
				break
			}
			out.WriteString(`<span class="`)
			out.WriteString(class)
			out.WriteString(`">`)
			out.WriteString(html.EscapeString(tok))
			out.WriteString(`</span>`)
		case c == '-' || (c >= '0' && c <= '9'):
			start := i
			i++
			for i < n {
				d := s[i]
				if d == '.' || d == 'e' || d == 'E' || d == '+' || d == '-' || (d >= '0' && d <= '9') {
					i++
					continue
				}
				break
			}
			out.WriteString(`<span class="json-number">`)
			out.WriteString(s[start:i])
			out.WriteString(`</span>`)
		case c == 't' && i+4 <= n && s[i:i+4] == "true":
			out.WriteString(`<span class="json-bool">true</span>`)
			i += 4
		case c == 'f' && i+5 <= n && s[i:i+5] == "false":
			out.WriteString(`<span class="json-bool">false</span>`)
			i += 5
		case c == 'n' && i+4 <= n && s[i:i+4] == "null":
			out.WriteString(`<span class="json-null">null</span>`)
			i += 4
		default:
			out.WriteString(html.EscapeString(string(c)))
			i++
		}
	}
	return template.HTML(out.String())
}

// highlightEventStream highlights only the JSON payload of each `data:` line.
// Other SSE fields (`event:`, `id:`, comment `:`) are HTML-escaped and emitted
// as plain text so rendering never breaks.
func highlightEventStream(s string) template.HTML {
	var out strings.Builder
	out.Grow(len(s) * 2)
	first := true
	for line := range strings.SplitSeq(s, "\n") {
		if !first {
			out.WriteByte('\n')
		}
		first = false
		trimmed := strings.TrimRight(line, "\r")
		if payload, ok := strings.CutPrefix(trimmed, "data: "); ok {
			out.WriteString(`<span class="sse-field">data:</span> `)
			out.WriteString(string(highlightJSON(payload)))
			continue
		}
		if field, rest, ok := strings.Cut(trimmed, ": "); ok && isSSEField(field) {
			out.WriteString(`<span class="sse-field">`)
			out.WriteString(html.EscapeString(field))
			out.WriteString(`:</span> `)
			out.WriteString(html.EscapeString(rest))
			continue
		}
		out.WriteString(html.EscapeString(trimmed))
	}
	return template.HTML(out.String())
}

func isSSEField(name string) bool {
	switch name {
	case "event", "id", "retry":
		return true
	}
	return false
}
