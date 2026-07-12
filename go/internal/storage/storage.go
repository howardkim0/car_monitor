// Package storage persists decoded Readings locally. See DESIGN.md section
// 6: readings are appended to a CSV file, one per UTC calendar day
// (`readings-YYYY-MM-DD.csv`), so a future "give me Tuesday's drive"
// analysis just means picking a file — trivial to inspect with `adb pull`
// and any spreadsheet tool, and easy to swap for something else later
// since callers depend on the Store interface, not FileStore directly.
package storage

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
)

// Store persists Readings.
type Store interface {
	Append(r obd2.Reading) error
	Close() error
}

// dateFormat is the UTC calendar-day format used both in filenames and
// to decide when to rotate — see DESIGN.md section 6.
const dateFormat = "2006-01-02"

var csvHeader = []string{"pid", "name", "value", "unit", "timestamp"}

// FileStore appends one CSV row per Reading to a local file, syncing
// after every write so a killed background process loses at most the
// reading currently in flight. The file rotates to a new one whenever
// the reading being appended falls on a different UTC calendar day than
// the currently-open file — checked on every Append, so this holds
// whether the rotation happens at session-open time or mid-session
// (e.g. a drive spanning UTC midnight). Resuming an existing same-day
// file (app restart, reconnect) appends without rewriting its header.
type FileStore struct {
	mu          sync.Mutex
	dir         string
	file        *os.File
	writer      *csv.Writer
	currentDate string
}

// OpenFileStore opens (creating if necessary) today's UTC-dated CSV file
// inside dir, creating dir itself if it doesn't exist.
func OpenFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create reading log dir %q: %w", dir, err)
	}

	s := &FileStore{dir: dir}
	if err := s.openForDate(time.Now().UTC().Format(dateFormat)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) pathForDate(date string) string {
	return filepath.Join(s.dir, fmt.Sprintf("readings-%s.csv", date))
}

// openForDate opens (creating if necessary) the CSV file for date,
// writing the header row only if the file didn't already exist — so
// resuming an existing same-day file never duplicates it.
func (s *FileStore) openForDate(date string) error {
	path := s.pathForDate(date)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open reading log %q: %w", path, err)
	}

	// A post-open size check (not a separate pre-open os.Stat) both avoids
	// a TOCTOU race and means a file left empty by a previously failed
	// header write (see writeRow above) gets the header retried on the
	// next resume, instead of being permanently treated as "not new".
	info, statErr := f.Stat()
	isNew := statErr == nil && info.Size() == 0

	s.file = f
	s.writer = csv.NewWriter(f)
	s.currentDate = date

	if isNew {
		_ = s.writeRow(csvHeader)
	}
	return nil
}

// writeRow writes one CSV row and flushes/syncs it to disk immediately —
// shared by the header row (in openForDate) and data rows (in Append) so
// there's one error-handling path for both, not two. On any failure, a
// fresh csv.Writer replaces s.writer: csv.Writer wraps a bufio.Writer with
// sticky-error semantics — once a write or flush fails, every subsequent
// call on the same Writer returns that same stale error forever, even
// after the underlying condition (e.g. transient disk-full) clears. A
// fresh Writer over the same *os.File lets the next Append retry cleanly
// instead of this file being wedged for the rest of the UTC day.
func (s *FileStore) writeRow(row []string) error {
	err := s.doWriteRow(row)
	if err != nil {
		s.writer = csv.NewWriter(s.file)
	}
	return err
}

func (s *FileStore) doWriteRow(row []string) error {
	if err := s.writer.Write(row); err != nil {
		return fmt.Errorf("write reading log row: %w", err)
	}
	s.writer.Flush()
	if err := s.writer.Error(); err != nil {
		return fmt.Errorf("flush reading log row: %w", err)
	}
	return s.file.Sync()
}

// Append writes r as one CSV row, rotating to a new UTC-dated file first
// if r's timestamp falls on a different day than the currently-open file.
func (s *FileStore) Append(r obd2.Reading) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	date := r.Timestamp.UTC().Format(dateFormat)
	if date != s.currentDate {
		// Best-effort: writing today's data matters more than a clean
		// close of yesterday's file, and failing to close it shouldn't
		// block starting the new one.
		_ = s.file.Close()
		if err := s.openForDate(date); err != nil {
			return err
		}
	}

	row := []string{
		strconv.Itoa(int(r.PID)),
		r.Name,
		strconv.FormatFloat(r.Value, 'f', -1, 64),
		r.Unit,
		r.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	return s.writeRow(row)
}

// Close flushes and closes the underlying file. Append must not be called
// after Close.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
