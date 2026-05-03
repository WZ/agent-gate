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
}
