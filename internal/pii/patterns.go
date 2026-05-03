package pii

import "regexp"

// Pattern is one named regex with its kind metadata.
type Pattern struct {
	Code   string
	Tier   Tier
	Regexp *regexp.Regexp
}

// Patterns is the canonical free-text regex set. Each entry is run over
// any body via FindAll. Walker-only kinds (name, address, dob) are not
// listed here — they require key context and live in keys.go.
var Patterns = []Pattern{
	// Email: local@domain.tld with at least one dot in the domain.
	{Code: "email", Tier: TierIdentifying,
		Regexp: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},

	// JWT: header.payload.signature, anchored on the canonical eyJ prefix.
	{Code: "jwt", Tier: TierIdentifying,
		Regexp: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)},

	// UUID v4-shaped (also matches v1/v3/v5 — we don't differentiate).
	{Code: "uuid", Tier: TierIdentifying,
		Regexp: regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)},

	// IPv4 with octet validation so we don't match version strings.
	{Code: "ipv4", Tier: TierIdentifying,
		Regexp: regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)},

	// Credit card candidate: 13-19 digit clusters with optional spaces or dashes
	// between groups. Always Luhn-validated before emission (see FindAll
	// in pii.go).
	{Code: "credit_card", Tier: TierSensitive,
		Regexp: regexp.MustCompile(`\b(?:\d[\s\-]?){12,18}\d\b`)},

	// Phone (US/NANP-style). Requires at least one separator between the
	// area code and the prefix and between the prefix and the line number,
	// so bare 10-digit IDs do not flag. International prefix (+1, +44, ...)
	// is optional. The separator class accepts space, dash, dot.
	{Code: "phone", Tier: TierIdentifying,
		Regexp: regexp.MustCompile(`(?:\+\d{1,3}[\s\-.])?\(?\d{3}\)?[\s\-.]\d{3}[\s\-.]\d{4}\b`)},

	// SSN (US, dashed canonical form). Bare 9-digit strings are NOT matched
	// here — the key-context path handles those when the JSON field is
	// explicitly named ssn / social_security_number.
	{Code: "ssn", Tier: TierSensitive,
		Regexp: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
}
