package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %q: %v", path, err)
	}
	return string(data)
}

func TestOpenCreatesDirAndFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs") // dir doesn't exist yet
	l, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Errorf("app log file not created: %v", err)
	}
}

func TestErrorfAndDebugfWriteFormattedLines(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	l.Errorf("append failed: %v", "disk full")
	l.Debugf("connected to %s", "AA:BB:CC:DD:EE:FF")

	content := readFile(t, l.currentPath())
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), content)
	}
	if !strings.Contains(lines[0], "ERROR") || !strings.Contains(lines[0], "append failed: disk full") {
		t.Errorf("first line = %q, want it to contain ERROR and the formatted message", lines[0])
	}
	if !strings.Contains(lines[1], "DEBUG") || !strings.Contains(lines[1], "connected to AA:BB:CC:DD:EE:FF") {
		t.Errorf("second line = %q, want it to contain DEBUG and the formatted message", lines[1])
	}
}

func TestRotationKeepsExactlyOnePriorFile(t *testing.T) {
	dir := t.TempDir()
	l, err := openWithMaxSize(dir, 10) // tiny cap so 1-2 lines already exceed it
	if err != nil {
		t.Fatalf("openWithMaxSize: %v", err)
	}
	defer l.Close()

	l.Errorf("first")  // written to the original file, which is now > 10 bytes
	l.Errorf("second") // rotateIfNeeded sees the cap exceeded: "first" -> prior, fresh file gets "second"
	l.Errorf("third")  // rotates again: "second" -> prior (replacing "first"), fresh file gets "third"

	prior := readFile(t, l.priorPath())
	if !strings.Contains(prior, "second") || strings.Contains(prior, "first") || strings.Contains(prior, "third") {
		t.Errorf("prior file = %q, want it to contain only \"second\" (one kept prior file, not unbounded)", prior)
	}

	current := readFile(t, l.currentPath())
	if !strings.Contains(current, "third") || strings.Contains(current, "second") {
		t.Errorf("current file = %q, want it to contain only \"third\"", current)
	}
}

func TestRotationFailureDegradesWithoutPanicking(t *testing.T) {
	dir := t.TempDir()
	l, err := openWithMaxSize(dir, 10)
	if err != nil {
		t.Fatalf("openWithMaxSize: %v", err)
	}
	defer l.Close()

	l.Errorf("first") // pushes the file past the 10-byte cap

	// Sabotage the next rotation attempt: a non-empty directory at
	// priorPath means os.Remove(priorPath) fails (Remove refuses
	// non-empty dirs), leaving it in place so the subsequent
	// os.Rename(current, priorPath) also fails (can't rename onto an
	// existing non-empty directory).
	if err := os.Mkdir(l.priorPath(), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.priorPath(), "keepme"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Must not panic even though rotation will fail internally.
	l.Errorf("second")

	// Rotation failed before reopening, so the original file (with
	// "first") was never renamed away and still exists at currentPath.
	current := readFile(t, l.currentPath())
	if !strings.Contains(current, "first") {
		t.Errorf("current file = %q, want the pre-rotation content to still be there since rename failed", current)
	}

	// The real regression: pre-fix, a failed rename left l.file pointing
	// at the handle rotateIfNeeded had already closed, so "second" (and
	// every write after it) would silently vanish — Errorf swallows
	// WriteString/Sync errors — leaving logging permanently dead for the
	// rest of the process. Reopening the same path on rename failure
	// means "second" actually lands, and logging keeps working afterward.
	if !strings.Contains(current, "second") {
		t.Errorf("current file = %q, want \"second\" to have been written too — logging must survive a failed rotation, not go dark", current)
	}
	l.Errorf("third")
	current = readFile(t, l.currentPath())
	if !strings.Contains(current, "third") {
		t.Errorf("current file = %q, want \"third\" to have been written too — logging must keep working after a failed rotation, not just once more", current)
	}
}

func TestCloseThenWriteDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Best-effort: writing (and its internal rotation check) after Close
	// must not panic, even though the underlying file is gone.
	l.Errorf("should not panic")
}

func TestOpenErrorsWhenDirCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Open(filepath.Join(blocker, "logs")); err == nil {
		t.Error("Open with an unmakeable dir should error, got nil")
	}
}

func TestOpenErrorsWhenFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	// A directory sitting at the exact path the code wants to open as a
	// file makes os.OpenFile fail regardless of permissions/root.
	if err := os.Mkdir(filepath.Join(dir, fileName), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if _, err := Open(dir); err == nil {
		t.Error("Open with a directory blocking the log file should error, got nil")
	}
}
