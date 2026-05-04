package dashboard

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"agent-gate/internal/pii"
	"agent-gate/internal/store"
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
// matches must be sorted by Start ascending and have offsets into s; the
// handler runs pii.Find once and passes the result here so detection isn't
// repeated per token.
//
// Content-types not understood here pass through with HTML escaping only.
func highlightBody(s string, headers http.Header, matches []pii.Match) template.HTML {
	if s == "" {
		return ""
	}
	switch detectContentKind(headers) {
	case kindJSON:
		return highlightJSON(s, matches)
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
//
// matches contain pre-computed PII byte ranges into s. Tokens that overlap
// a match get tier-colored span wrappers.
func highlightJSON(s string, matches []pii.Match) template.HTML {
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
				emitStringWithPII(&out, tok, start, matches)
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
			// Pretty-print + highlight the payload here. Per-payload
			// detection re-runs pii.Find on the pretty-printed bytes; the
			// SSE-level matches don't have the right offsets for indented
			// JSON. The chip-strip summary still uses SSE-level matches.
			pretty := prettyJSON(dataPayload)
			payloadMatches := pii.Find([]byte(pretty), pii.KindJSON)
			out.WriteString(string(highlightJSON(pretty, payloadMatches)))
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
	Tier  string // "sensitive" or "identifying"; drives chip color
	Count int
}

var piiLabels = map[string]string{
	"email":       "Email",
	"jwt":         "JWT",
	"uuid":        "UUID",
	"ipv4":        "IPv4",
	"name":        "Name",
	"address":     "Address",
	"dob":         "DOB",
	"phone":       "Phone",
	"ssn":         "SSN",
	"credit_card": "Credit card",
}

// SummarizePII tallies pre-computed matches by Code. Output is sorted
// sensitive-first (so the chip strip leads with the high-stakes signal),
// then alphabetical for stable display. Returns nil when matches is
// empty so templates can guard with `{{ if .ReqPII }}`.
func SummarizePII(matches []pii.Match) []PIICount {
	if len(matches) == 0 {
		return nil
	}
	type bucket struct {
		count int
		tier  pii.Tier
	}
	buckets := make(map[string]*bucket, len(matches))
	for _, m := range matches {
		b, ok := buckets[m.Code]
		if !ok {
			b = &bucket{tier: m.Tier}
			buckets[m.Code] = b
		}
		b.count++
	}
	out := make([]PIICount, 0, len(buckets))
	for code, b := range buckets {
		label := piiLabels[code]
		if label == "" {
			label = code
		}
		out = append(out, PIICount{Code: code, Label: label, Tier: string(b.tier), Count: b.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier == string(pii.TierSensitive)
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// HasSensitivePII reports whether any chip in a payload's summary is the
// sensitive tier. Used by the template to upgrade the summary strip's
// color when SSN / credit card / DOB are present.
func HasSensitivePII(rows []PIICount) bool {
	for _, r := range rows {
		if r.Tier == string(pii.TierSensitive) {
			return true
		}
	}
	return false
}

// loadPIIChipsForEvents reads the event_pii table for a batch of event ids
// and returns a per-event ordered chip list, summing across req+resp sides.
// Tier is derived from the static piiTierByCode map.
func loadPIIChipsForEvents(idx *store.Index, ids []string) (map[string][]PIICount, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT event_id, code, sum(count) FROM event_pii ` +
		`WHERE event_id IN (` + strings.Join(placeholders, ",") + `) ` +
		`GROUP BY event_id, code`
	rows, err := idx.Db().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	per := map[string]map[string]int{}
	for rows.Next() {
		var (
			eventID, code string
			n             int
		)
		if err := rows.Scan(&eventID, &code, &n); err != nil {
			return nil, err
		}
		if per[eventID] == nil {
			per[eventID] = map[string]int{}
		}
		per[eventID][code] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[string][]PIICount, len(per))
	for id, codeCounts := range per {
		chips := make([]PIICount, 0, len(codeCounts))
		for code, count := range codeCounts {
			label := piiLabels[code]
			if label == "" {
				label = code
			}
			tier := piiTierByCode(code)
			chips = append(chips, PIICount{Code: code, Label: label, Tier: tier, Count: count})
		}
		sort.Slice(chips, func(i, j int) bool {
			if chips[i].Tier != chips[j].Tier {
				return chips[i].Tier == "sensitive"
			}
			return chips[i].Code < chips[j].Code
		})
		out[id] = chips
	}
	return out, nil
}

func piiTierByCode(code string) string {
	switch code {
	case "ssn", "credit_card", "dob":
		return "sensitive"
	}
	return "identifying"
}

// emitStringWithPII writes a JSON string token (including surrounding
// quotes) to out, wrapping any pre-computed PII matches whose byte range
// falls within tokAbsStart..tokAbsStart+len(tok) in nested spans.
//
// matches must be sorted by Start ascending. We linear-scan and skip
// matches that don't overlap the current token. The surrounding quotes
// pass through as plain content; html.EscapeString runs over every byte
// before emission so embedded `<` / `>` / `&` can never break out of
// the json-string wrapper.
func emitStringWithPII(out *strings.Builder, tok string, tokAbsStart int, matches []pii.Match) {
	tokAbsEnd := tokAbsStart + len(tok)
	pos := 0
	for _, m := range matches {
		if m.Start >= tokAbsEnd {
			break
		}
		if m.End <= tokAbsStart {
			continue
		}
		localStart := m.Start - tokAbsStart
		localEnd := m.End - tokAbsStart
		if localStart < pos {
			continue
		}
		if localStart > pos {
			out.WriteString(html.EscapeString(tok[pos:localStart]))
		}
		out.WriteString(`<span class="pii pii-`)
		out.WriteString(string(m.Tier))
		out.WriteString(` pii-`)
		out.WriteString(m.Code)
		out.WriteString(`" title="`)
		out.WriteString(m.Code)
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(tok[localStart:localEnd]))
		out.WriteString(`</span>`)
		pos = localEnd
	}
	if pos < len(tok) {
		out.WriteString(html.EscapeString(tok[pos:]))
	}
}
