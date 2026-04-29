package idgen

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

type Generator struct {
	mu  sync.Mutex
	src *ulid.MonotonicEntropy
}

func NewGenerator() *Generator {
	return &Generator{src: ulid.Monotonic(rand.Reader, 0)}
}

func (g *Generator) New() string {
	return g.NewAt(time.Now())
}

func (g *Generator) NewAt(t time.Time) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), g.src).String()
}
