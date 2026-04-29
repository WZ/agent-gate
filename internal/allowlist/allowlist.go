package allowlist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Allowlist is a file-backed set of hostnames the user has explicitly trusted.
// Safe for concurrent use.
type Allowlist struct {
	mu    sync.RWMutex
	path  string
	hosts map[string]struct{}
}

// Load reads the file (if it exists) and returns an Allowlist. Missing file => empty allowlist.
// Lines beginning with `#` are comments. Trailing `#...` on a line is ignored.
// Blank lines are skipped.
func Load(path string) (*Allowlist, error) {
	a := &Allowlist{path: path, hosts: map[string]struct{}{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
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
		a.hosts[strings.ToLower(line)] = struct{}{}
	}
	return a, nil
}

// Contains reports whether host (lowercased) is in the allowlist.
func (a *Allowlist) Contains(host string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.hosts[strings.ToLower(host)]
	return ok
}

// Add inserts host into the allowlist (in-memory + file). Returns nil on success
// even if host was already present (idempotent).
func (a *Allowlist) Add(host string) error {
	if !isPlainHostname(host) {
		return fmt.Errorf("allowlist: %q is not a plain hostname (no scheme, no port, no path)", host)
	}
	host = strings.ToLower(host)

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.hosts[host]; exists {
		return nil
	}
	a.hosts[host] = struct{}{}

	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.path), ".allowlist-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if existing, err := os.ReadFile(a.path); err == nil {
		if _, err := tmp.Write(existing); err != nil {
			tmp.Close()
			return err
		}
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			tmp.Write([]byte{'\n'})
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
	return os.Rename(tmpPath, a.path)
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
