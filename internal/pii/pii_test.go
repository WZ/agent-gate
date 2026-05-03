package pii

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindAllEmail(t *testing.T) {
	matches := FindAll([]byte(`{"email":"alice@example.com","cc":"bob.smith+work@dops.io"}`))
	codes := codes(matches)
	assert.Equal(t, []string{"email", "email"}, codes)
}

func TestFindAllJWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NSJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	matches := FindAll([]byte(`{"token":"` + jwt + `"}`))
	assert.Len(t, matches, 1)
	assert.Equal(t, "jwt", matches[0].Code)
}

func TestFindAllUUID(t *testing.T) {
	matches := FindAll([]byte(`request_id=01F8MECHZX3TBDSZ7XR8RHRP01 trace=550e8400-e29b-41d4-a716-446655440000`))
	codes := codes(matches)
	assert.Equal(t, []string{"uuid"}, codes,
		"ULID should not match uuid; only the dashed canonical form does")
}

func TestFindAllIPv4ValidatesOctets(t *testing.T) {
	matches := FindAll([]byte(`server=10.0.0.1 ver=1.2.3.4 limit=999.999.999.999 also=192.168.1.255`))
	codes := codes(matches)
	// 10.0.0.1, 1.2.3.4, 192.168.1.255 all match (octets in 0-255).
	// 999.999.999.999 does not.
	assert.Equal(t, []string{"ipv4", "ipv4", "ipv4"}, codes)
}

func TestFindAllRemovesOverlaps(t *testing.T) {
	// An email shouldn't double-flag if a substring of it also matches another
	// pattern (none currently overlap, but the dedup logic should be robust).
	matches := FindAll([]byte(`alice@example.com`))
	assert.Len(t, matches, 1)
}

func TestFindAllSortedByStart(t *testing.T) {
	matches := FindAll([]byte(`ip=10.0.0.1 then email alice@example.com then 192.168.1.1`))
	require := assert.New(t)
	require.Len(matches, 3)
	for i := 1; i < len(matches); i++ {
		require.LessOrEqual(matches[i-1].Start, matches[i].Start)
	}
}

func TestFindAllReturnsEmptyForBenignBody(t *testing.T) {
	assert.Empty(t, FindAll([]byte(`{"model":"claude","max_tokens":1024}`)))
	assert.Empty(t, FindAll([]byte(`hello world`)))
	assert.Empty(t, FindAll(nil))
}

func TestCountByCode(t *testing.T) {
	matches := FindAll([]byte(`a@b.com c@d.io 10.0.0.1`))
	got := CountByCode(matches)
	assert.Equal(t, 2, got["email"])
	assert.Equal(t, 1, got["ipv4"])
}

func codes(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Code
	}
	return out
}

func TestMatchHasTierAndSource(t *testing.T) {
	matches := FindAll([]byte(`alice@example.com`))
	require.Len(t, matches, 1)
	assert.Equal(t, "email", matches[0].Code)
	assert.Equal(t, TierIdentifying, matches[0].Tier)
	assert.Equal(t, SourceRegex, matches[0].Source)
}

func TestDetectKindFromHeaders(t *testing.T) {
	cases := []struct {
		ct   string
		want ContentKind
	}{
		{"application/json", KindJSON},
		{"application/json; charset=utf-8", KindJSON},
		{"application/vnd.api+json", KindJSON},
		{"text/event-stream", KindSSE},
		{"text/plain", KindOther},
		{"", KindOther},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.ct != "" {
			h.Set("Content-Type", tc.ct)
		}
		assert.Equal(t, tc.want, DetectKind(h), "ct=%q", tc.ct)
	}
}

func TestFindAllCreditCardLuhnRequired(t *testing.T) {
	// Visa test number passes Luhn → flagged.
	matches := FindAll([]byte("paid with 4111111111111111 today"))
	require.Len(t, matches, 1)
	assert.Equal(t, "credit_card", matches[0].Code)
	assert.Equal(t, TierSensitive, matches[0].Tier)
	assert.Equal(t, SourceLuhn, matches[0].Source)
}

func TestFindAllCreditCardSkipsLuhnFailure(t *testing.T) {
	// Single digit changed → fails Luhn → not flagged.
	matches := FindAll([]byte("paid with 4111111111111112 today"))
	for _, m := range matches {
		if m.Code == "credit_card" {
			t.Fatalf("unexpected credit_card match: %+v", m)
		}
	}
}

func TestFindAllCreditCardWithFormattingPunctuation(t *testing.T) {
	// "4111 1111 1111 1111" should be detected (spaces stripped before Luhn).
	matches := FindAll([]byte("card 4111 1111 1111 1111 expires 2030"))
	require.Len(t, matches, 1)
	assert.Equal(t, "credit_card", matches[0].Code)
}

func TestFindAllPhoneRegexRequiresSeparators(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"call (415) 555-1234 today", true},
		{"+1-415-555-1234", true},
		{"415.555.1234", true},
		{"415 555 1234", true},
		{"4155551234", false}, // bare digits — no flag
		{"order 4155551234567 dispatched", false},
	}
	for _, tc := range cases {
		matches := FindAll([]byte(tc.in))
		got := false
		for _, m := range matches {
			if m.Code == "phone" {
				got = true
				break
			}
		}
		if got != tc.want {
			t.Errorf("FindAll(%q) phone match = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFindAllSSNRegexRequiresDashes(t *testing.T) {
	matches := FindAll([]byte("ssn: 123-45-6789 on file"))
	require.Len(t, matches, 1)
	assert.Equal(t, "ssn", matches[0].Code)
	assert.Equal(t, TierSensitive, matches[0].Tier)

	// Bare 9-digit strings do not flag from free-text regex.
	matches = FindAll([]byte("id 123456789 dispatched"))
	for _, m := range matches {
		if m.Code == "ssn" {
			t.Fatalf("unexpected ssn match: %+v", m)
		}
	}
}

func TestSensitiveKeysLookup(t *testing.T) {
	cases := []struct {
		key      string
		wantCode string
		wantTier Tier
	}{
		{"name", "name", TierIdentifying},
		{"Name", "name", TierIdentifying},
		{"NAME", "name", TierIdentifying},
		{"first_name", "name", TierIdentifying},
		{"firstName", "name", TierIdentifying},
		{"family_name", "name", TierIdentifying},
		{"surname", "name", TierIdentifying},
		{"address", "address", TierIdentifying},
		{"postal_code", "address", TierIdentifying},
		{"zip", "address", TierIdentifying},
		{"dob", "dob", TierSensitive},
		{"date_of_birth", "dob", TierSensitive},
		{"birthday", "dob", TierSensitive},
		{"phone", "phone", TierIdentifying},
		{"mobile", "phone", TierIdentifying},
		{"tel", "phone", TierIdentifying},
		{"ssn", "ssn", TierSensitive},
		{"social_security_number", "ssn", TierSensitive},
		{"credit_card", "credit_card", TierSensitive},
		{"card_number", "credit_card", TierSensitive},
		{"pan", "credit_card", TierSensitive},
	}
	for _, tc := range cases {
		got, ok := sensitiveKeyLookup(tc.key)
		if !ok {
			t.Errorf("sensitiveKeyLookup(%q): not found, want %s/%s", tc.key, tc.wantCode, tc.wantTier)
			continue
		}
		if got.Code != tc.wantCode || got.Tier != tc.wantTier {
			t.Errorf("sensitiveKeyLookup(%q) = %+v, want code=%s tier=%s", tc.key, got, tc.wantCode, tc.wantTier)
		}
	}
}

func TestSensitiveKeysLookupNonSensitive(t *testing.T) {
	for _, key := range []string{"model", "max_tokens", "messages", "id", "type"} {
		if _, ok := sensitiveKeyLookup(key); ok {
			t.Errorf("sensitiveKeyLookup(%q) should not match", key)
		}
	}
}
