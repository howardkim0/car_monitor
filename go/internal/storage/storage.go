// Package storage persists decoded Readings locally. See DESIGN.md section
// 6: readings are appended to a CSV file, one per UTC calendar day
// (`readings-YYYY-MM-DD.csv`), so a future "give me Tuesday's drive"
// analysis just means picking a file — trivial to inspect with `adb pull`
// and any spreadsheet tool, and easy to swap for something else later
// since callers depend on the Store interface, not FileStore directly.
package storage

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// MaxReadingLogFiles is the target number of reading log files to retain.
// Older files are deleted to keep only the newest this many files.
const MaxReadingLogFiles = 30

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

// LoadReadings reads back today's UTC-dated CSV reading log from dir, for
// callers that need the day's history rather than just live per-reading
// Append (see internal/monitor, which needs matched time-series per
// metric to run trend checks against). Mirrors OpenFileStore's "always
// today" semantics rather than taking a date, since there's no caller
// yet that needs anything but the current day.
//
// A missing file (today's first Append hasn't happened yet) returns a
// nil slice, not an error — same "not an error" treatment OpenFileStore
// gives a file it's about to create.
//
// A row that fails to parse (wrong column count, or a value/timestamp
// that won't parse) is skipped rather than failing the whole read. A read
// that fails after the header was already read successfully is treated
// as a torn final line (e.g. from an unclean process kill mid-write) —
// it stops there and returns everything read so far, rather than
// discarding otherwise-good data over damage to the last line. A read
// that fails before ever getting past the header is treated as a real
// error instead: os.Open succeeds even when path is a directory (or
// other non-regular file) on Unix, so that class of problem only
// surfaces once something actually tries to read from it.
func LoadReadings(dir string) ([]obd2.Reading, error) {
	path := filepath.Join(dir, fmt.Sprintf("readings-%s.csv", time.Now().UTC().Format(dateFormat)))

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open reading log %q: %w", path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	var readings []obd2.Reading
	skippedHeader := false
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if !skippedHeader {
				return nil, fmt.Errorf("read reading log %q: %w", path, err)
			}
			break
		}
		if !skippedHeader {
			skippedHeader = true
			continue
		}
		if r, ok := parseRow(row); ok {
			readings = append(readings, r)
		}
	}
	return readings, nil
}

// PruneOldReadingLogs deletes readings-*.csv files in dir beyond the
// keep most recent (by filename, which sorts chronologically).
// Age is irrelevant — only count. keep <= 0 is treated as 0 (delete
// everything matched). A glob or stat failure is a real error; a
// per-file os.Remove failure is collected and returned but does not
// stop pruning the rest (best-effort, matches this file's existing
// "one bad file shouldn't wedge everything else" pattern).
func PruneOldReadingLogs(dir string, keep int) error {
	if keep < 0 {
		keep = 0
	}

	matches, err := filepath.Glob(filepath.Join(dir, "readings-*.csv"))
	if err != nil {
		return fmt.Errorf("glob reading logs: %w", err)
	}

	if len(matches) <= keep {
		return nil
	}

	sort.Strings(matches)
	toRemove := matches[:len(matches)-keep]

	var removeErr error
	for _, path := range toRemove {
		if err := os.Remove(path); err != nil {
			if removeErr == nil {
				removeErr = fmt.Errorf("remove reading log %q: %w", path, err)
			}
		}
	}

	return removeErr
}

func parseRow(row []string) (obd2.Reading, bool) {
	if len(row) != 5 {
		return obd2.Reading{}, false
	}

	pid, err := strconv.ParseUint(row[0], 10, 8)
	if err != nil {
		return obd2.Reading{}, false
	}
	value, err := strconv.ParseFloat(row[2], 64)
	if err != nil {
		return obd2.Reading{}, false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, row[4])
	if err != nil {
		return obd2.Reading{}, false
	}

	return obd2.Reading{
		PID:       byte(pid),
		Name:      row[1],
		Value:     value,
		Unit:      row[3],
		Timestamp: timestamp,
	}, true
}
