package pii

import (
	"net/http"
	"reflect"
	"strings"
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

func TestFindNameViaJSONKey(t *testing.T) {
	cases := []struct {
		body string
		want []Match
	}{
		{
			body: `{"name":"Alice"}`,
			want: []Match{{Code: "name", Tier: TierIdentifying, Source: SourceKey, Start: 9, End: 14}},
		},
		{
			body: `{"first_name":"Bob","last_name":"Roe"}`,
			want: []Match{
				{Code: "name", Tier: TierIdentifying, Source: SourceKey, Start: 15, End: 18},
				{Code: "name", Tier: TierIdentifying, Source: SourceKey, Start: 33, End: 36},
			},
		},
		{
			body: `{"firstName":"Carol"}`,
			want: []Match{{Code: "name", Tier: TierIdentifying, Source: SourceKey, Start: 14, End: 19}},
		},
		{
			body: `{"user":{"name":"Dave"}}`,
			want: []Match{{Code: "name", Tier: TierIdentifying, Source: SourceKey, Start: 17, End: 21}},
		},
		{
			body: `{"name":""}`,
			want: nil,
		},
	}
	for _, tc := range cases {
		got := Find([]byte(tc.body), KindJSON)
		assertMatchesEqual(t, got, tc.want, tc.body)
	}
}

func TestFindAddressViaJSONKey(t *testing.T) {
	body := `{"street":"1 Main St","postal_code":"94105"}`
	got := Find([]byte(body), KindJSON)
	want := []Match{
		{Code: "address", Tier: TierIdentifying, Source: SourceKey, Start: 11, End: 20},
		{Code: "address", Tier: TierIdentifying, Source: SourceKey, Start: 37, End: 42},
	}
	assertMatchesEqual(t, got, want, body)
}

func assertMatchesEqual(t *testing.T, got, want []Match, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("matches len mismatch for %q:\n got: %+v\nwant: %+v", ctx, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("match[%d] for %q:\n got: %+v\nwant: %+v", i, ctx, got[i], want[i])
		}
	}
}

func TestRemoveOverlapsBreaksTiesBySensitiveTier(t *testing.T) {
	matches := []Match{
		{Code: "email", Tier: TierIdentifying, Source: SourceRegex, Start: 5, End: 22},
		{Code: "ssn", Tier: TierSensitive, Source: SourceRegex, Start: 5, End: 22},
	}
	got := removeOverlaps(matches)
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1: %+v", len(got), got)
	}
	if got[0].Code != "ssn" || got[0].Tier != TierSensitive {
		t.Errorf("expected ssn (sensitive) to win, got %+v", got[0])
	}
}

func TestFindSSEDispatchesEachDataPayloadAsJSON(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"name":"Alice"}`,
		``,
		`event: content_block_delta`,
		`data: {"delta":{"text":"contact alice@example.com"}}`,
	}, "\n")

	got := Find([]byte(body), KindSSE)

	codes := map[string]int{}
	for _, m := range got {
		codes[m.Code]++
	}

	if codes["name"] != 1 {
		t.Errorf("expected exactly 1 name match, got %d (matches: %+v)", codes["name"], got)
	}
	if codes["email"] != 1 {
		t.Errorf("expected exactly 1 email match, got %d (matches: %+v)", codes["email"], got)
	}
}

func TestFindSSEEmailInEventLineIsAlsoCaught(t *testing.T) {
	body := "event: contact-alice@example.com\ndata: {\"k\":\"v\"}"
	got := Find([]byte(body), KindSSE)
	count := 0
	for _, m := range got {
		if m.Code == "email" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 email match anywhere in SSE body, got %d (matches: %+v)", count, got)
	}
}

func TestFindJSONRunsRegexInsideStringValues(t *testing.T) {
	body := `{"contact":"alice@example.com from 192.168.1.42"}`
	got := Find([]byte(body), KindJSON)
	wantCodes := []string{"email", "ipv4"}
	gotCodes := []string{}
	for _, m := range got {
		gotCodes = append(gotCodes, m.Code)
	}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("got codes %v, want %v\nmatches: %+v", gotCodes, wantCodes, got)
	}
}

func TestFindJSONDoesNotRunRegexInsideKeys(t *testing.T) {
	body := `{"alice@example.com":"value"}`
	got := Find([]byte(body), KindJSON)
	for _, m := range got {
		if m.Code == "email" {
			t.Fatalf("unexpected email match in key: %+v", m)
		}
	}
}

func TestFindCreditCardKeyContextStillRequiresLuhn(t *testing.T) {
	cases := []struct {
		body string
		want []Match
	}{
		{
			body: `{"card_number":"4111111111111111"}`,
			want: []Match{{Code: "credit_card", Tier: TierSensitive, Source: SourceKey, Start: 16, End: 32}},
		},
		{body: `{"card_number":"tok_abc123"}`, want: nil},
		{body: `{"card_number":"4111111111111112"}`, want: nil},
		{
			body: `{"pan":"4111 1111 1111 1111"}`,
			want: []Match{{Code: "credit_card", Tier: TierSensitive, Source: SourceKey, Start: 8, End: 27}},
		},
	}
	for _, tc := range cases {
		got := Find([]byte(tc.body), KindJSON)
		assertMatchesEqual(t, got, tc.want, tc.body)
	}
}

func TestFindCreditCardOnlyOneMatchPerNumber(t *testing.T) {
	body := `{"card_number":"4111111111111111"}`
	matches := Find([]byte(body), KindJSON)
	count := 0
	for _, m := range matches {
		if m.Code == "credit_card" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d credit_card matches, want 1: %+v", count, matches)
	}
}

func TestFindSSNViaJSONKey(t *testing.T) {
	cases := []struct {
		body string
		want []Match
	}{
		{
			body: `{"ssn":"123-45-6789"}`,
			want: []Match{{Code: "ssn", Tier: TierSensitive, Source: SourceKey, Start: 8, End: 19}},
		},
		{
			body: `{"ssn":"123456789"}`,
			want: []Match{{Code: "ssn", Tier: TierSensitive, Source: SourceKey, Start: 8, End: 17}},
		},
		{
			body: `{"social_security_number":"123-45-6789"}`,
			want: []Match{{Code: "ssn", Tier: TierSensitive, Source: SourceKey, Start: 27, End: 38}},
		},
		{body: `{"ssn":"n/a"}`, want: nil},
		{body: `{"ssn":"1234"}`, want: nil},
		{body: `{"ssn":"1234567890"}`, want: nil},
	}
	for _, tc := range cases {
		got := Find([]byte(tc.body), KindJSON)
		assertMatchesEqual(t, got, tc.want, tc.body)
	}
}

func TestFindPhoneViaJSONKeyAcceptsBareDigits(t *testing.T) {
	cases := []struct {
		body string
		want []Match
	}{
		{
			body: `{"phone":"4155551234"}`,
			want: []Match{{Code: "phone", Tier: TierIdentifying, Source: SourceKey, Start: 10, End: 20}},
		},
		{
			body: `{"mobile":"+1 415 555 1234"}`,
			want: []Match{{Code: "phone", Tier: TierIdentifying, Source: SourceKey, Start: 11, End: 26}},
		},
		{body: `{"phone":"see notes"}`, want: nil},
		{body: `{"phone":"123456"}`, want: nil},
	}
	for _, tc := range cases {
		got := Find([]byte(tc.body), KindJSON)
		assertMatchesEqual(t, got, tc.want, tc.body)
	}
}

func TestFindDOBRequiresDateShape(t *testing.T) {
	cases := []struct {
		body string
		want []Match
	}{
		{
			body: `{"dob":"1990-05-12"}`,
			want: []Match{{Code: "dob", Tier: TierSensitive, Source: SourceKey, Start: 8, End: 18}},
		},
		{
			body: `{"birthday":"05/12/1990"}`,
			want: []Match{{Code: "dob", Tier: TierSensitive, Source: SourceKey, Start: 13, End: 23}},
		},
		{
			body: `{"date_of_birth":"5/12/90"}`,
			want: []Match{{Code: "dob", Tier: TierSensitive, Source: SourceKey, Start: 18, End: 25}},
		},
		{body: `{"dob":"n/a"}`, want: nil},
		{body: `{"dob":""}`, want: nil},
		{body: `{"dob":"unknown"}`, want: nil},
	}
	for _, tc := range cases {
		got := Find([]byte(tc.body), KindJSON)
		assertMatchesEqual(t, got, tc.want, tc.body)
	}
}
