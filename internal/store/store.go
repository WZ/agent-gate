package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"agent-gate/internal/types"
)

// Store is the public API: Append + read-back by id.
type Store struct {
	dir    string
	w      *JSONLWriter
	idx    *Index
	dbPath string

	mu sync.Mutex
}

func Open(dir string, now func() time.Time) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w, err := NewJSONLWriter(dir, now)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "events.db")
	idx, err := OpenIndex(dbPath)
	if err != nil {
		w.Close()
		return nil, err
	}
	return &Store{dir: dir, w: w, idx: idx, dbPath: dbPath}, nil
}

func (s *Store) Append(ev types.StoredEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	loc, err := s.w.Append(ev)
	if err != nil {
		return fmt.Errorf("jsonl append: %w", err)
	}
	if err := s.idx.Insert(ev, loc); err != nil {
		// JSONL is source of truth; index is rebuildable. Surface error but data is durable.
		return fmt.Errorf("index insert (jsonl line written): %w", err)
	}
	return nil
}

// Body returns a reader over the raw JSON line for event id.
// Caller must Close().
func (s *Store) Body(id string) (io.ReadCloser, error) {
	r, err := s.idx.QueryByID(id)
	if err != nil {
		return nil, err
	}
	return s.openSlice(r.JSONLPath, r.JSONLOffset, r.JSONLLength)
}

func (s *Store) openSlice(path string, offset, length int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, 0); err != nil {
		f.Close()
		return nil, err
	}
	return &limitedReadCloser{Reader: io.LimitReader(f, length), Closer: f}, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

// Clear truncates the index and removes all JSONL files. Caller must serialize
// against concurrent Appends (the writer's internal mutex handles its part).
// Used by the dashboard's "clear all" action.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.w.Truncate(); err != nil {
		return fmt.Errorf("truncate jsonl: %w", err)
	}
	if err := s.idx.Truncate(); err != nil {
		return fmt.Errorf("truncate index: %w", err)
	}
	return nil
}

func (s *Store) Index() *Index             { return s.idx }
func (s *Store) JSONLWriter() *JSONLWriter { return s.w }
func (s *Store) IndexPath() string         { return s.dbPath }

func (s *Store) Close() error {
	var first error
	if err := s.w.Close(); err != nil && first == nil {
		first = err
	}
	if err := s.idx.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
