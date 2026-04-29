package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"agent-gate/internal/types"
)

// Location records where a JSON line lives on disk.
type Location struct {
	Path   string
	Offset int64
	Length int64
}

// JSONLWriter appends StoredEvents to a daily-rotated JSONL file.
// File mode is 0o600. The Now() clock is injectable for tests.
type JSONLWriter struct {
	dir string
	now func() time.Time

	mu      sync.Mutex
	current *os.File
	curDate string
	curSize int64
}

// NewJSONLWriter creates the directory if needed and returns a writer.
func NewJSONLWriter(dir string, now func() time.Time) (*JSONLWriter, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &JSONLWriter{dir: dir, now: now}, nil
}

// Append marshals ev to JSON, writes a single line ending in '\n', returns Location.
func (w *JSONLWriter) Append(ev types.StoredEvent) (Location, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := w.now().UTC()
	date := t.Format("2006-01-02")
	if err := w.ensureFile(date); err != nil {
		return Location{}, err
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return Location{}, err
	}
	data = append(data, '\n')

	off := w.curSize
	n, err := w.current.Write(data)
	if err != nil {
		return Location{}, err
	}
	w.curSize += int64(n)

	return Location{
		Path:   w.current.Name(),
		Offset: off,
		Length: int64(n),
	}, nil
}

// Close flushes and closes the current file.
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		err := w.current.Close()
		w.current = nil
		return err
	}
	return nil
}

func (w *JSONLWriter) ensureFile(date string) error {
	if w.current != nil && w.curDate == date {
		return nil
	}
	if w.current != nil {
		if err := w.current.Close(); err != nil {
			return err
		}
		w.current = nil
	}
	path := filepath.Join(w.dir, fmt.Sprintf("%s.jsonl", date))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.current = f
	w.curDate = date
	w.curSize = st.Size()
	return nil
}
