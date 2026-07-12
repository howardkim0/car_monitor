// Package storage persists decoded Readings locally. See DESIGN.md section
// 6: v1 is a plain append-only JSON Lines file — trivial to inspect with
// `adb pull` + `jq`, and easy to swap for something else later since
// callers depend on the Store interface, not FileStore directly.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
)

// Store persists Readings.
type Store interface {
	Append(r obd2.Reading) error
	Close() error
}

// FileStore appends one JSON object per line to a local file, syncing after
// every write so a killed background process loses at most the reading
// currently in flight.
type FileStore struct {
	mu   sync.Mutex
	file *os.File
}

// OpenFileStore opens path for appending, creating it if it doesn't exist.
// Existing contents are preserved.
func OpenFileStore(path string) (*FileStore, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open reading log %q: %w", path, err)
	}
	return &FileStore{file: f}, nil
}

// Append writes r as one JSON line.
func (s *FileStore) Append(r obd2.Reading) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal reading: %w", err)
	}
	line = append(line, '\n')

	if _, err := s.file.Write(line); err != nil {
		return fmt.Errorf("write reading log: %w", err)
	}
	return s.file.Sync()
}

// Close flushes and closes the underlying file. Append must not be called
// after Close.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
