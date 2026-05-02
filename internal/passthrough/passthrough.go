// Package passthrough is a file-backed list of hostnames where agent-gate
// will NOT attempt TLS interception. Traffic to these hosts is tunneled raw,
// so the proxy records the CONNECT (host + byte counts) but never sees
// plaintext. Use for cert-pinned upstreams (e.g. mcp-proxy.anthropic.com)
// where MITM would fail or for hosts the user has explicitly opted out of
// inspection.
package passthrough

import (
	"errors"
	"os"
	"strings"
	"sync"
)

// List is read-only at runtime: hosts are added by editing the file. The
// file is loaded once at startup; if you change it, restart agent-gate.
type List struct {
	mu    sync.RWMutex
	hosts map[string]struct{}
}

// Load reads path (if present). Missing file → empty list. Comments (`#`)
// and blank lines are ignored.
func Load(path string) (*List, error) {
	l := &List{hosts: map[string]struct{}{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		l.hosts[strings.ToLower(line)] = struct{}{}
	}
	return l, nil
}

// Contains reports whether host is in the list.
func (l *List) Contains(host string) bool {
	if l == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.hosts[strings.ToLower(host)]
	return ok
}
