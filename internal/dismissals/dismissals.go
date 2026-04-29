package dismissals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Scope string

const (
	ScopeEvent      Scope = "event"
	ScopeHostCode   Scope = "host_code"
	ScopeGlobalCode Scope = "global_code"
)

type Entry struct {
	Scope     Scope     `json:"scope"`
	EventID   string    `json:"event_id,omitempty"`
	Code      string    `json:"code"`
	Host      string    `json:"host,omitempty"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// Dismissals is a file-backed audit-log-of-the-audit-log of dismissals and trust actions.
// Safe for concurrent use within a single process; cross-process not guaranteed (last writer wins).
type Dismissals struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
}

func Load(path string) (*Dismissals, error) {
	d := &Dismissals{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(data, &d.entries); err != nil {
		return nil, fmt.Errorf("dismissals: parse %s: %w", path, err)
	}
	return d, nil
}

func (d *Dismissals) Add(scope Scope, eventID, code, host, reason string) error {
	switch scope {
	case ScopeEvent, ScopeHostCode, ScopeGlobalCode:
	default:
		return fmt.Errorf("dismissals: unknown scope %q", scope)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = append(d.entries, Entry{
		Scope:     scope,
		EventID:   eventID,
		Code:      code,
		Host:      host,
		Reason:    reason,
		Timestamp: time.Now().UTC(),
	})
	return d.persistLocked()
}

func (d *Dismissals) Has(eventID, code, host string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, e := range d.entries {
		switch e.Scope {
		case ScopeEvent:
			if e.EventID == eventID && e.Code == code {
				return true
			}
		case ScopeHostCode:
			if e.Host == host && e.Code == code {
				return true
			}
		case ScopeGlobalCode:
			if e.Code == code {
				return true
			}
		}
	}
	return false
}

func (d *Dismissals) Entries() []Entry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Entry, len(d.entries))
	copy(out, d.entries)
	return out
}

func (d *Dismissals) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".dismissals-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
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
