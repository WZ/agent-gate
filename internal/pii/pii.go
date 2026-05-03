// Package pii detects personally-identifiable information in captured
// payload bodies. Unlike internal/secrets, which exists to MASK material
// before display, this package exists to FLAG material that should remain
// visible — auditors need to see the email or session ID that's leaking,
// not just know "something's redacted".
//
// Patterns here intentionally favor low false-positive rate over recall:
// only shapes that are almost certainly PII when they match are included.
// Names, addresses, dates, and other natural-language PII are out of scope
// (would require an NER model or a curated dictionary). Phone numbers,
// SSNs, and credit-card numbers are out of scope for v1 because their
// regexes are noisy without contextual signals or Luhn validation.
package pii

import "regexp"

// Pattern is one named PII regex.
type Pattern struct {
	Code   string
	Regexp *regexp.Regexp
}

// Match is one regex hit on a body.
type Match struct {
	Code       string
	Start, End int // byte offsets within the input
}

// Patterns is the canonical PII set used by the dashboard's payload
// highlighter. Order matters only when overlapping ranges are reported —
// the dashboard sorts by Start and skips overlaps.
var Patterns = []Pattern{
	// Email: local@domain.tld with at least one dot in the domain. Conservative
	// — accepts the common shapes without trying to cover RFC 5322 in full.
	{Code: "email", Regexp: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},

	// JWT: header.payload.signature, all base64url. Anchored on the canonical
	// `eyJ` prefix (base64 of `{"`) to avoid matching arbitrary three-segment
	// dotted tokens. Useful for catching unredacted bearer-style tokens that
	// did NOT have the literal "Bearer " prefix the secrets pattern requires.
	{Code: "jwt", Regexp: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)},

	// UUID v4-shaped (also matches v1/v3/v5 — we don't differentiate).
	{Code: "uuid", Regexp: regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)},

	// IPv4 with octet validation so we don't match version strings like 1.2.3.4
	// in package metadata. Each octet must be 0-255.
	{Code: "ipv4", Regexp: regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)},
}

// FindAll returns every match of any Pattern in body, sorted by Start
// (ties broken by longer match first). Overlapping matches are removed —
// the leftmost-longest wins. Output is suitable for span-wrapping during
// HTML emission.
func FindAll(body []byte) []Match {
	var matches []Match
	for _, p := range Patterns {
		for _, idx := range p.Regexp.FindAllIndex(body, -1) {
			matches = append(matches, Match{Code: p.Code, Start: idx[0], End: idx[1]})
		}
	}
	if len(matches) <= 1 {
		return matches
	}
	// Sort by (Start asc, End desc) so longer matches at the same start beat
	// shorter ones during overlap removal.
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
			continue // overlaps with previous accepted match
		}
		out = append(out, m)
		lastEnd = m.End
	}
	return out
}

// CountByCode tallies matches per Code, useful for the "PII: 2 email · 1 jwt"
// summary chip row above each payload pane.
func CountByCode(matches []Match) map[string]int {
	out := make(map[string]int, len(Patterns))
	for _, m := range matches {
		out[m.Code]++
	}
	return out
}
