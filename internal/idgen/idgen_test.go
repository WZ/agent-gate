package idgen

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewIsMonotonic(t *testing.T) {
	g := NewGenerator()
	a := g.New()
	b := g.New()
	c := g.New()
	assert.True(t, a < b, "ULIDs should be lexicographically ordered: a=%s b=%s", a, b)
	assert.True(t, b < c, "ULIDs should be lexicographically ordered: b=%s c=%s", b, c)
}

func TestNewIsLength26(t *testing.T) {
	g := NewGenerator()
	id := g.New()
	assert.Len(t, id, 26)
}

func TestNewAtTimestampHonorsTime(t *testing.T) {
	g := NewGenerator()
	earlier := g.NewAt(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	later := g.NewAt(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	assert.True(t, earlier < later)
}
