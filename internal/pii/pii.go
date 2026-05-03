// Package pii detects personally-identifiable information in captured
// payload bodies. Unlike internal/secrets, which exists to MASK material
// before display, this package exists to FLAG material that should remain
// visible — auditors need to see the email or session ID that's leaking,
// not just know "something's redacted".
package pii

import (
	"net/http"
	"strings"
)

// ContentKind drives detection strategy. JSON gets a key-context-aware
// token walk; SSE recurses on each data: payload as JSON; everything else
// is plain regex over the whole body.
type ContentKind int

const (
	KindOther ContentKind = iota
	KindJSON
	KindSSE
)

// Tier classifies the audit severity of a kind. Sensitive items
// (SSN, credit card, date-of-birth) receive a stronger inline color and
// sort first in the chip strip; identifying items (everything else) stay
// in the warning amber palette.
type Tier string

const (
	TierSensitive   Tier = "sensitive"
	TierIdentifying Tier = "identifying"
)

// Source records why a Match was emitted. Useful for tests and future
// debugging UI; not surfaced to end users in v1.
type Source string

const (
	SourceRegex Source = "regex"
	SourceKey   Source = "key"
	SourceLuhn  Source = "luhn"
)

// Match is one PII hit on a body.
type Match struct {
	Code       string
	Tier       Tier
	Source     Source
	Start, End int
}

// DetectKind mirrors the dashboard's existing content-type detection,
// exposed so capture-time and render-time callers don't depend on the
// dashboard package.
func DetectKind(h http.Header) ContentKind {
	ct := strings.ToLower(h.Get("Content-Type"))
	switch {
	case strings.Contains(ct, "application/json"),
		strings.Contains(ct, "+json"):
		return KindJSON
	case strings.Contains(ct, "text/event-stream"):
		return KindSSE
	}
	return KindOther
}

// Find returns every PII match in body, sorted by Start (ties: longer
// first; same-position ties broken by tier — sensitive wins). Overlapping
// ranges are removed leftmost-longest.
//
// Dispatch by content kind:
//   - KindJSON: walker owns both key-context and free-text regex.
//   - KindSSE:  body-wide regex catches matches in event:/id: lines;
//     each data: payload is recursed as KindJSON so key-context fires
//     inside streamed deltas.
//   - KindOther: plain regex over the whole body via FindAll.
func Find(body []byte, kind ContentKind) []Match {
	switch kind {
	case KindJSON:
		return removeOverlaps(findInJSON(body))
	case KindSSE:
		return findInSSE(body)
	default:
		return FindAll(body)
	}
}

// findInSSE walks an SSE stream and runs JSON detection on each data:
// payload. Free-text regex also runs over the whole body so matches in
// event:/id: lines (which aren't JSON) still surface.
func findInSSE(body []byte) []Match {
	all := FindAll(body)
	for line, off := range sseDataLines(body) {
		payload := []byte(line)
		for _, m := range findInJSON(payload) {
			all = append(all, Match{
				Code: m.Code, Tier: m.Tier, Source: m.Source,
				Start: off + m.Start, End: off + m.End,
			})
		}
	}
	return removeOverlaps(all)
}

// sseDataLines yields the JSON payload of each `data: {...}` line and
// its byte offset in body.
func sseDataLines(body []byte) func(yield func(line string, off int) bool) {
	return func(yield func(line string, off int) bool) {
		s := string(body)
		offset := 0
		for {
			i := strings.Index(s, "data: ")
			if i < 0 {
				return
			}
			payloadStart := i + len("data: ")
			rest := s[payloadStart:]
			end := strings.IndexAny(rest, "\n\r")
			if end < 0 {
				end = len(rest)
			}
			payload := rest[:end]
			if !yield(payload, offset+payloadStart) {
				return
			}
			advance := payloadStart + end
			s = s[advance:]
			offset += advance
		}
	}
}

// FindAll returns every regex match of any pattern in body. Preserved as
// a thin alias for non-dashboard callers (e.g., a future policy rule that
// wants raw byte scanning without content-type awareness).
//
// Credit-card candidates are Luhn-validated before emission; failed
// candidates are silently dropped. Their Source is SourceLuhn rather
// than SourceRegex to mark that an extra check passed.
func FindAll(body []byte) []Match {
	var matches []Match
	for _, p := range Patterns {
		for _, idx := range p.Regexp.FindAllIndex(body, -1) {
			if p.Code == "credit_card" {
				digits := stripNonDigits(string(body[idx[0]:idx[1]]))
				if !Luhn(digits) {
					continue
				}
				matches = append(matches, Match{
					Code:   p.Code,
					Tier:   p.Tier,
					Source: SourceLuhn,
					Start:  idx[0],
					End:    idx[1],
				})
				continue
			}
			matches = append(matches, Match{
				Code:   p.Code,
				Tier:   p.Tier,
				Source: SourceRegex,
				Start:  idx[0],
				End:    idx[1],
			})
		}
	}
	return removeOverlaps(matches)
}

// CountByCode tallies matches per Code. Used by the chip-strip summary.
func CountByCode(matches []Match) map[string]int {
	out := make(map[string]int, len(Patterns))
	for _, m := range matches {
		out[m.Code]++
	}
	return out
}

// removeOverlaps sorts matches by (Start asc, End desc) and skips entries
// that overlap an earlier accepted match. Pure helper — kept here so both
// FindAll and Find share the same invariant.
func removeOverlaps(matches []Match) []Match {
	if len(matches) <= 1 {
		return matches
	}
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0; j-- {
			a, b := matches[j-1], matches[j]
			if a.Start < b.Start || (a.Start == b.Start && a.End >= b.End) {
				break
			}
			matches[j-1], matches[j] = b, a
		}
	}
	out := matches[:0]
	lastEnd := -1
	for _, m := range matches {
		if m.Start < lastEnd {
			continue
		}
		out = append(out, m)
		lastEnd = m.End
	}
	return out
}
