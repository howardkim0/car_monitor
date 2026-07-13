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

func TestLoadReadingsReturnsNilWhenTodaysFileDoesNotExist(t *testing.T) {
	dir := t.TempDir()

	readings, err := LoadReadings(dir)
	if err != nil {
		t.Fatalf("LoadReadings: %v", err)
	}
	if readings != nil {
		t.Errorf("LoadReadings with no file = %v, want nil", readings)
	}
}

func TestLoadReadingsRoundTripsAppendedRows(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	today := time.Now().UTC()
	ts1 := time.Date(today.Year(), today.Month(), today.Day(), 10, 30, 0, 123000000, time.UTC)
	ts2 := ts1.Add(1 * time.Second)
	want := []obd2.Reading{
		{PID: 0x0C, Name: "Engine RPM", Value: 1726, Unit: "rpm", Timestamp: ts1},
		{PID: 0x05, Name: "Coolant Temperature", Value: 90.5, Unit: "C", Timestamp: ts2},
	}
	for _, r := range want {
		if err := store.Append(r); err != nil {
			t.Fatalf("Append(%+v): %v", r, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := LoadReadings(dir)
	if err != nil {
		t.Fatalf("LoadReadings: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("LoadReadings returned %d readings, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].PID != want[i].PID || got[i].Name != want[i].Name || got[i].Value != want[i].Value ||
			got[i].Unit != want[i].Unit || !got[i].Timestamp.Equal(want[i].Timestamp) {
			t.Errorf("reading %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadReadingsSkipsMalformedRowsButKeepsGoodOnes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")

	raw := "pid,name,value,unit,timestamp\n" +
		"12,Engine RPM,1726,rpm,2026-07-12T10:30:00Z\n" + // good
		"not-a-pid,Engine RPM,1726,rpm,2026-07-12T10:30:00Z\n" + // bad pid
		"12,Engine RPM,not-a-number,rpm,2026-07-12T10:30:00Z\n" + // bad value
		"12,Engine RPM,1726,rpm,not-a-timestamp\n" + // bad timestamp
		"12,Engine RPM,1726,rpm\n" + // wrong column count
		"13,Vehicle Speed,80,km/h,2026-07-12T10:30:01Z\n" // good
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadReadings(dir)
	if err != nil {
		t.Fatalf("LoadReadings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d readings, want 2 (only the well-formed rows): %+v", len(got), got)
	}
	if got[0].PID != 0x0C || got[1].PID != 0x0D {
		t.Errorf("got PIDs %#x, %#x, want 0x0C, 0x0D", got[0].PID, got[1].PID)
	}
}

func TestLoadReadingsStopsAtTornFinalLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")

	// An unterminated quote makes encoding/csv return a parse error on
	// that line — simulating a write that got cut off mid-row by an
	// unclean process kill, after the header and one good row.
	raw := "pid,name,value,unit,timestamp\n" +
		"12,Engine RPM,1726,rpm,2026-07-12T10:30:00Z\n" +
		"13,\"Vehicle Speed,80,km/h,2026-07-12T10:30:01Z\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadReadings(dir)
	if err != nil {
		t.Fatalf("LoadReadings: %v", err)
	}
	if len(got) != 1 || got[0].PID != 0x0C {
		t.Fatalf("got %+v, want just the one good row before the torn line", got)
	}
}

func TestLoadReadingsErrorsWhenFileExistsButCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	// A directory sitting at the exact path LoadReadings wants to open as
	// a file makes os.Open succeed (opening a directory read-only doesn't
	// fail on Unix) but the first Read() fail before the header is ever
	// reached — the "error before skippedHeader" branch.
	blockedPath := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if _, err := LoadReadings(dir); err == nil {
		t.Error("LoadReadings with a directory blocking today's file should error, got nil")
	}
}

func TestLoadReadingsErrorsWhenFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readings-"+time.Now().UTC().Format(dateFormat)+".csv")
	if err := os.WriteFile(path, []byte("pid,name,value,unit,timestamp\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Remove read permission so os.Open itself fails with something other
	// than ErrNotExist — the file genuinely exists.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(path, 0o644) // restore so t.TempDir() cleanup can remove it

	if _, err := LoadReadings(dir); err == nil {
		t.Error("LoadReadings against an unreadable file should error, got nil")
	}
}

func TestPruneOldReadingLogs(t *testing.T) {
	tests := []struct {
		name        string
		createFiles []string
		keep        int
		wantRemain  []string
		expectError bool
	}{
		{
			name:        "fewer files than keep",
			createFiles: []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv"},
			keep:        5,
			wantRemain:  []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv"},
			expectError: false,
		},
		{
			name:        "exactly keep files",
			createFiles: []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv", "readings-2026-07-12.csv"},
			keep:        3,
			wantRemain:  []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv", "readings-2026-07-12.csv"},
			expectError: false,
		},
		{
			name:        "more files than keep, removes oldest",
			createFiles: []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv", "readings-2026-07-12.csv", "readings-2026-07-13.csv", "readings-2026-07-14.csv"},
			keep:        3,
			wantRemain:  []string{"readings-2026-07-12.csv", "readings-2026-07-13.csv", "readings-2026-07-14.csv"},
			expectError: false,
		},
		{
			name:        "keep equals zero",
			createFiles: []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv"},
			keep:        0,
			wantRemain:  []string{},
			expectError: false,
		},
		{
			name:        "keep is negative",
			createFiles: []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv"},
			keep:        -5,
			wantRemain:  []string{},
			expectError: false,
		},
		{
			name:        "dates spanning far more than 30 days",
			createFiles: []string{"readings-2026-05-10.csv", "readings-2026-05-20.csv", "readings-2026-06-15.csv", "readings-2026-07-10.csv", "readings-2026-07-12.csv"},
			keep:        2,
			wantRemain:  []string{"readings-2026-07-10.csv", "readings-2026-07-12.csv"},
			expectError: false,
		},
		{
			name:        "ignores non-matching files",
			createFiles: []string{"readings-2026-07-10.csv", "notes.txt", "readings-2026-07-11.csv", "app.log"},
			keep:        1,
			wantRemain:  []string{"notes.txt", "app.log", "readings-2026-07-11.csv"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create the files
			for _, filename := range tt.createFiles {
				path := filepath.Join(dir, filename)
				if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			// Call PruneOldReadingLogs
			err := PruneOldReadingLogs(dir, tt.keep)
			if (err != nil) != tt.expectError {
				t.Errorf("PruneOldReadingLogs error = %v, expectError = %v", err, tt.expectError)
			}

			// Check remaining files
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}

			var remaining []string
			for _, entry := range entries {
				remaining = append(remaining, entry.Name())
			}

			if len(remaining) != len(tt.wantRemain) {
				t.Errorf("got %d files remaining, want %d: %v", len(remaining), len(tt.wantRemain), remaining)
				return
			}

			for _, want := range tt.wantRemain {
				found := false
				for _, got := range remaining {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want file %q in remaining files, not found: %v", want, remaining)
				}
			}
		})
	}
}

func TestPruneOldReadingLogsGlobError(t *testing.T) {
	// filepath.Glob returns an error if the pattern is invalid (e.g.,
	// unclosed bracket). We can trigger this by using a dir path that,
	// when joined with "readings-*.csv", creates an invalid pattern.
	invalidDir := "/tmp/[invalid"
	if err := PruneOldReadingLogs(invalidDir, 30); err == nil {
		t.Error("PruneOldReadingLogs with invalid glob pattern should error, got nil")
	}
}

func TestPruneOldReadingLogsRemoveError(t *testing.T) {
	dir := t.TempDir()

	// Create files and then make the directory read-only so Remove fails
	for _, filename := range []string{"readings-2026-07-10.csv", "readings-2026-07-11.csv"} {
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// Make directory read-only to cause Remove to fail
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir() cleanup can remove it

	// PruneOldReadingLogs should return an error (best-effort)
	err := PruneOldReadingLogs(dir, 1)
	if err == nil {
		t.Error("PruneOldReadingLogs with read-only dir should error, got nil")
	}
}
