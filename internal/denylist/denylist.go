package denylist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Denylist is a file-backed set of hostnames the user has explicitly forbidden.
// When a host appears here, the proxy returns 403 to the agent regardless of
// allowlist enforce mode. Block always wins.
type Denylist struct {
	mu    sync.RWMutex
	path  string
	hosts map[string]struct{}
}

// Load reads the file (if it exists) and returns a Denylist. Missing file =>
// empty denylist. Comment lines (starting with `#`) and trailing `#...` are
// ignored. Blank lines are skipped.
func Load(path string) (*Denylist, error) {
	d := &Denylist{path: path, hosts: map[string]struct{}{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
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
		d.hosts[strings.ToLower(line)] = struct{}{}
	}
	return d, nil
}

// Contains reports whether host (lowercased) is in the denylist.
func (d *Denylist) Contains(host string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.hosts[strings.ToLower(host)]
	return ok
}

// Add inserts host into the denylist (in-memory + file). Idempotent.
func (d *Denylist) Add(host string) error {
	if !isPlainHostname(host) {
		return fmt.Errorf("denylist: %q is not a plain hostname (no scheme, no port, no path)", host)
	}
	host = strings.ToLower(host)

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.hosts[host]; exists {
		return nil
	}
	d.hosts[host] = struct{}{}

	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".denylist-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if existing, err := os.ReadFile(d.path); err == nil {
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
	return os.Rename(tmpPath, d.path)
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
