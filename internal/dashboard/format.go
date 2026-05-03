package dashboard

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"net/http"
	"strings"

	"agent-gate/internal/pii"
)

// formatBody applies content-aware pretty-printing to a captured request or
// response body. JSON gets indented. Other content types — including SSE,
// which is pretty-printed during highlight so the per-event collapse can
// own its layout — pass through unchanged.
//
// Formatting is best-effort. If parsing fails (truncated capture, redactor
// markers in unfortunate spots, or genuinely non-JSON content under a JSON
// content-type), we return the input unchanged rather than guess.
func formatBody(body string, headers http.Header) string {
	if body == "" {
		return body
	}
	if detectContentKind(headers) == kindJSON {
		return prettyJSON(body)
	}
	return body
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
			if class == "json-string" {
				emitStringWithPII(&out, tok)
			} else {
				out.WriteString(html.EscapeString(tok))
			}
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

// highlightEventStream walks an SSE stream, batches lines into events
// separated by blank-line boundaries (per the SSE spec), and emits one
// <details> per event. Each event's body holds its `data:` payload (with
// JSON highlighting) plus any `id:` / `retry:` fields. Lines before the
// first event boundary, or any block without a recognizable `event:` /
// `data:` field, render as raw escaped text outside any details element.
//
// Default state is open — same readability as before, with the affordance
// to collapse long streams (Anthropic Messages content_block_delta floods
// in particular).
func highlightEventStream(s string) template.HTML {
	var out strings.Builder
	out.Grow(len(s) * 2)

	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		eventName := ""
		dataPayload := ""
		var others []string
		hasField := false
		for _, raw := range block {
			line := strings.TrimRight(raw, "\r")
			if name, ok := strings.CutPrefix(line, "event: "); ok {
				eventName = name
				hasField = true
				continue
			}
			if payload, ok := strings.CutPrefix(line, "data: "); ok {
				dataPayload = payload
				hasField = true
				continue
			}
			if field, _, ok := strings.Cut(line, ": "); ok && isSSEField(field) {
				others = append(others, line)
				hasField = true
				continue
			}
			others = append(others, line)
		}
		if !hasField {
			// Nothing recognizable — emit verbatim, no details wrapper.
			for _, raw := range block {
				out.WriteString(html.EscapeString(strings.TrimRight(raw, "\r")))
				out.WriteByte('\n')
			}
			block = nil
			return
		}
		out.WriteString(`<details class="sse-block" open><summary><span class="sse-field">event:</span> `)
		if eventName == "" {
			out.WriteString(`<span class="sse-event-anonymous">message</span>`)
		} else {
			out.WriteString(html.EscapeString(eventName))
		}
		out.WriteString(`</summary>`)
		for _, line := range others {
			out.WriteByte('\n')
			if field, rest, ok := strings.Cut(line, ": "); ok && isSSEField(field) {
				out.WriteString(`<span class="sse-field">`)
				out.WriteString(html.EscapeString(field))
				out.WriteString(`:</span> `)
				out.WriteString(html.EscapeString(rest))
			} else {
				out.WriteString(html.EscapeString(line))
			}
		}
		if dataPayload != "" {
			out.WriteString("\n")
			out.WriteString(`<span class="sse-field">data:</span> `)
			// Pretty-print + highlight the payload here. Doing it inside
			// highlightEventStream keeps the SSE pipeline single-pass: each
			// data: line in the input is one JSON object, period.
			out.WriteString(string(highlightJSON(prettyJSON(dataPayload))))
		}
		out.WriteString(`</details>`)
		block = nil
	}

	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimRight(line, "\r") == "" {
			flush()
			continue
		}
		block = append(block, line)
	}
	flush()

	return template.HTML(strings.TrimRight(out.String(), "\n"))
}

func isSSEField(name string) bool {
	switch name {
	case "event", "id", "retry":
		return true
	}
	return false
}

// PIICount is one row in the summary chip strip above a payload pane.
type PIICount struct {
	Code  string // pii pattern identifier, e.g. "email"
	Label string // human-friendly label, e.g. "Email"
	Count int
}

var piiLabels = map[string]string{
	"email": "Email",
	"jwt":   "JWT",
	"uuid":  "UUID",
	"ipv4":  "IPv4",
}

// SummarizePII walks the body bytes, returning per-kind counts in canonical
// pii.Patterns order. Returns nil when no PII was detected — templates can
// guard with `{{ if .ReqPII }}` to hide the chip strip entirely.
func SummarizePII(body string) []PIICount {
	if body == "" {
		return nil
	}
	matches := pii.FindAll([]byte(body))
	if len(matches) == 0 {
		return nil
	}
	counts := pii.CountByCode(matches)
	out := make([]PIICount, 0, len(counts))
	for _, p := range pii.Patterns {
		if c := counts[p.Code]; c > 0 {
			label := piiLabels[p.Code]
			if label == "" {
				label = p.Code
			}
			out = append(out, PIICount{Code: p.Code, Label: label, Count: c})
		}
	}
	return out
}

// emitStringWithPII writes a JSON string token (including surrounding quotes)
// to out, wrapping any PII matches inside the literal in nested <span>
// elements. Tokens with no PII are emitted as a single HTML-escaped string.
//
// PII patterns never match quote characters, so the surrounding quotes pass
// through as plain content; we still html-escape the whole token before
// emission so embedded `<` / `>` / `&` cannot break out of the json-string
// wrapper.
func emitStringWithPII(out *strings.Builder, tok string) {
	matches := pii.FindAll([]byte(tok))
	if len(matches) == 0 {
		out.WriteString(html.EscapeString(tok))
		return
	}
	pos := 0
	for _, m := range matches {
		if m.Start > pos {
			out.WriteString(html.EscapeString(tok[pos:m.Start]))
		}
		out.WriteString(`<span class="pii pii-`)
		out.WriteString(m.Code)
		out.WriteString(`" title="`)
		out.WriteString(m.Code)
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(tok[m.Start:m.End]))
		out.WriteString(`</span>`)
		pos = m.End
	}
	if pos < len(tok) {
		out.WriteString(html.EscapeString(tok[pos:]))
	}
}
