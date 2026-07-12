package storage

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
)

func TestAppendWritesJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.jsonl")

	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	want := []obd2.Reading{
		{PID: 0x0C, Name: "Engine RPM", Value: 1726, Unit: "rpm", Timestamp: time.Now()},
		{PID: 0x0D, Name: "Vehicle Speed", Value: 80, Unit: "km/h", Timestamp: time.Now()},
	}
	for _, r := range want {
		if err := store.Append(r); err != nil {
			t.Fatalf("Append(%+v): %v", r, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("re-opening reading log: %v", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning reading log: %v", err)
	}

	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
}

func TestAppendIsActuallyAppendOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.jsonl")

	first, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("OpenFileStore (first): %v", err)
	}
	if err := first.Append(obd2.Reading{PID: 0x0C, Name: "Engine RPM"}); err != nil {
		t.Fatalf("Append (first): %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close (first): %v", err)
	}

	second, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("OpenFileStore (second): %v", err)
	}
	if err := second.Append(obd2.Reading{PID: 0x0D, Name: "Vehicle Speed"}); err != nil {
		t.Fatalf("Append (second): %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("got %d lines after two separate opens, want 2 (reopening must not truncate)", lines)
	}
}

func TestAppendAfterCloseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.jsonl")

	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := store.Append(obd2.Reading{PID: 0x0C}); err == nil {
		t.Error("Append after Close should error, got nil")
	}
}
