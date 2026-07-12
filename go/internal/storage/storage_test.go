package storage

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
)

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %q: %v", path, err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parsing %q as CSV: %v", path, err)
	}
	return rows
}

func TestOpenFileStoreCreatesTodaysFileWithHeader(t *testing.T) {
	dir := t.TempDir()

	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer store.Close()

	wantPath := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")
	rows := readCSV(t, wantPath)
	if len(rows) != 1 || rows[0][0] != "pid" {
		t.Fatalf("got rows %v, want just the header row", rows)
	}
}

func TestAppendWritesCSVRows(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	now := time.Now().UTC()
	want := []obd2.Reading{
		{PID: 0x0C, Name: "Engine RPM", Value: 1726, Unit: "rpm", Timestamp: now},
		{PID: 0x0D, Name: "Vehicle Speed", Value: 80, Unit: "km/h", Timestamp: now},
	}
	for _, r := range want {
		if err := store.Append(r); err != nil {
			t.Fatalf("Append(%+v): %v", r, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "readings-"+now.Format(dateFormat)+".csv")
	rows := readCSV(t, path)
	if len(rows) != 3 { // header + 2 data rows
		t.Fatalf("got %d rows, want 3 (header + 2 readings): %v", len(rows), rows)
	}
	if rows[0][0] != "pid" || rows[0][4] != "timestamp" {
		t.Errorf("header row = %v, want pid,name,value,unit,timestamp", rows[0])
	}
	if rows[1][0] != "12" || rows[1][2] != "1726" {
		t.Errorf("first data row = %v, want pid=12 value=1726", rows[1])
	}
}

func TestAppendResumesSameDayFileWithoutDuplicatingHeader(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	first, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore (first): %v", err)
	}
	if err := first.Append(obd2.Reading{PID: 0x0C, Name: "Engine RPM", Timestamp: now}); err != nil {
		t.Fatalf("Append (first): %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close (first): %v", err)
	}

	second, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore (second): %v", err)
	}
	if err := second.Append(obd2.Reading{PID: 0x0D, Name: "Vehicle Speed", Timestamp: now}); err != nil {
		t.Fatalf("Append (second): %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}

	path := filepath.Join(dir, "readings-"+now.Format(dateFormat)+".csv")
	rows := readCSV(t, path)
	headers := 0
	for _, row := range rows {
		if row[0] == "pid" {
			headers++
		}
	}
	if headers != 1 {
		t.Errorf("got %d header rows after reopening the same day, want exactly 1: %v", headers, rows)
	}
	if len(rows) != 3 { // 1 header + 2 data rows total
		t.Errorf("got %d total rows, want 3 (1 header + 2 readings): %v", len(rows), rows)
	}
}

func TestAppendRotatesOnUTCDateChange(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer store.Close()

	day1 := time.Date(2026, 7, 11, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 12, 0, 1, 0, 0, time.UTC)

	if err := store.Append(obd2.Reading{PID: 0x0C, Name: "Engine RPM", Timestamp: day1}); err != nil {
		t.Fatalf("Append (day1): %v", err)
	}
	if err := store.Append(obd2.Reading{PID: 0x0D, Name: "Vehicle Speed", Timestamp: day2}); err != nil {
		t.Fatalf("Append (day2): %v", err)
	}

	day1Rows := readCSV(t, filepath.Join(dir, "readings-2026-07-11.csv"))
	if len(day1Rows) != 2 { // header + 1 reading
		t.Errorf("day1 file has %d rows, want 2 (header + 1 reading): %v", len(day1Rows), day1Rows)
	}

	day2Rows := readCSV(t, filepath.Join(dir, "readings-2026-07-12.csv"))
	if len(day2Rows) != 2 {
		t.Errorf("day2 file has %d rows, want 2 (header + 1 reading): %v", len(day2Rows), day2Rows)
	}
}

func TestOpenFileStoreErrorsWhenDirCannotBeCreated(t *testing.T) {
	// A regular file where a path component of dir needs to be a
	// directory makes os.MkdirAll fail regardless of permissions/root.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := OpenFileStore(filepath.Join(blocker, "readings")); err == nil {
		t.Error("OpenFileStore with an unmakeable dir should error, got nil")
	}
}

func TestOpenFileStoreErrorsWhenDatedFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	// A directory sitting at the exact path the code wants to open as a
	// file makes os.OpenFile fail regardless of permissions/root.
	blockedPath := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if _, err := OpenFileStore(dir); err == nil {
		t.Error("OpenFileStore with a directory blocking today's file should error, got nil")
	}
}

func TestAppendErrorsWhenRotatedFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir) // opens today's file
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer store.Close()

	// Block tomorrow's file (guaranteed different from whatever "today"
	// is when this test runs, so it can't collide with the file
	// OpenFileStore just created above) with a directory.
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	blockedPath := filepath.Join(dir, "readings-"+tomorrow.Format(dateFormat)+".csv")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	err = store.Append(obd2.Reading{
		PID:       0x0C,
		Name:      "Engine RPM",
		Timestamp: tomorrow,
	})
	if err == nil {
		t.Error("Append rotating into a directory-blocked path should error, got nil")
	}
}

func TestAppendAfterCloseErrors(t *testing.T) {
	dir := t.TempDir()

	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := store.Append(obd2.Reading{PID: 0x0C, Timestamp: time.Now().UTC()}); err == nil {
		t.Error("Append after Close should error, got nil")
	}
	// A second call after writeRow has reset the writer should also error
	// on the fresh writer trying to write to the still-closed file.
	if err := store.Append(obd2.Reading{PID: 0x0D, Timestamp: time.Now().UTC()}); err == nil {
		t.Error("second Append after Close should also error, got nil")
	}
}

func TestWriterResetOnWriteError(t *testing.T) {
	dir := t.TempDir()

	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()

	// Close the file to cause subsequent writes to fail.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// First Append: Write and Flush both fail on closed file.
	// This triggers error handling in doWriteRow which checks Flush error.
	firstErr := store.Append(obd2.Reading{PID: 0x0C, Timestamp: now})
	if firstErr == nil {
		t.Error("first Append after Close should error")
	}

	// Second Append: With a fresh writer (reset after the first error),
	// it also fails at Flush because the file is still closed.
	// The Write() call succeeds (buffering), but Flush() fails.
	secondErr := store.Append(obd2.Reading{PID: 0x0D, Timestamp: now})
	if secondErr == nil {
		t.Error("second Append after Close should error")
	}
}

func TestWriterResetPreservesFileStateForRetry(t *testing.T) {
	dir := t.TempDir()

	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	defer store.Close()

	// Write data successfully, exercising the doWriteRow success path
	// including Write, Flush, and Sync completion.
	now := time.Now().UTC()
	if err := store.Append(obd2.Reading{PID: 0x0C, Name: "Engine RPM", Value: 1726, Unit: "rpm", Timestamp: now}); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	// Verify the data was written
	path := filepath.Join(dir, "readings-"+now.Format(dateFormat)+".csv")
	rows := readCSV(t, path)
	if len(rows) != 2 { // header + 1 data row
		t.Fatalf("got %d rows after first Append, want 2", len(rows))
	}

	// Write another row to verify the writer remains functional after
	// the first successful write, ensuring Sync() completed properly
	// and the file stays open for subsequent appends.
	if err := store.Append(obd2.Reading{PID: 0x0D, Name: "Vehicle Speed", Value: 80, Unit: "km/h", Timestamp: now}); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	rows = readCSV(t, path)
	if len(rows) != 3 { // header + 2 data rows
		t.Fatalf("got %d rows after second Append, want 3", len(rows))
	}
}

func TestWriteErrorOnFieldLargerThanBufferedWriter(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// csv.Writer wraps a 4096-byte bufio.Writer. A field larger than that,
	// written to an empty (freshly reset) buffer, bypasses buffering
	// entirely and writes directly to the underlying (now-closed) file —
	// so Write() itself, not just Flush(), returns the error. This
	// exercises doWriteRow's Write()-error branch, which no other test
	// reaches (small rows only ever fail at Flush()).
	hugeName := strings.Repeat("x", 8192)
	if err := store.Append(obd2.Reading{PID: 0x0C, Name: hugeName, Timestamp: time.Now().UTC()}); err == nil {
		t.Error("Append with a field larger than the buffered writer against a closed file should error, got nil")
	}
}
