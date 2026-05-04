// Package passthrough is a file-backed list of hostnames where agent-gate
// will NOT attempt TLS interception. Traffic to these hosts is tunneled raw,
// so the proxy records the CONNECT (host + byte counts) but never sees
// plaintext. Use for cert-pinned upstreams (e.g. mcp-proxy.anthropic.com)
// where MITM would fail or for hosts the user has explicitly opted out of
// inspection.
package passthrough

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// List is the file-backed passthrough host list. Hosts here have TLS
// interception skipped — the proxy tunnels TCP raw and the audit log only
// records the CONNECT host + byte counts.
type List struct {
	mu    sync.RWMutex
	path  string
	hosts map[string]struct{}
}

// Load reads path (if present). Missing file → empty list. Comments (`#`)
// and blank lines are ignored.
func Load(path string) (*List, error) {
	l := &List{path: path, hosts: map[string]struct{}{}}
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

// Add inserts host into the list (in-memory + file). Idempotent.
func (l *List) Add(host string) error {
	if !isPlainHostname(host) {
		return fmt.Errorf("passthrough: %q is not a plain hostname (no scheme, no port, no path)", host)
	}
	host = strings.ToLower(host)

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.hosts[host]; exists {
		return nil
	}
	l.hosts[host] = struct{}{}

	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".passthrough-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if existing, err := os.ReadFile(l.path); err == nil {
		if _, err := tmp.Write(existing); err != nil {
			tmp.Close()
			return err
		}
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			if _, err := tmp.Write([]byte{'\n'}); err != nil {
				tmp.Close()
				return err
			}
		}
	}
	if _, err := tmp.Write([]byte(host + "\n")); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, l.path)
}

// Remove deletes host from the passthrough list (in-memory + file). Idempotent.
// Atomic write via tempfile + rename.
func (l *List) Remove(host string) error {
	host = strings.ToLower(host)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.hosts[host]; !exists {
		return nil
	}
	delete(l.hosts, host)

	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".passthrough-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hosts := make([]string, 0, len(l.hosts))
	for h := range l.hosts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	for _, h := range hosts {
		if _, err := tmp.Write([]byte(h + "\n")); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, l.path)
}

func isPlainHostname(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "://") || strings.Contains(s, "/") || strings.ContainsRune(s, ':') {
		return false
	}
	if strings.ContainsAny(s, " \t#") {
		return false
	}
	return true
}
