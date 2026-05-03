package pii

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
