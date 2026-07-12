package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetAppLogger forces a clean nil starting state regardless of what
// other tests in this package did to the shared appLogger var — Go
// doesn't guarantee test execution order, so tests touching this global
// must not rely on it.
func resetAppLogger(t *testing.T) {
	t.Helper()
	appLogMu.Lock()
	appLogger = nil
	appLogMu.Unlock()
	t.Cleanup(func() {
		appLogMu.Lock()
		appLogger = nil
		appLogMu.Unlock()
	})
}

func TestLogErrorAndLogDebugAreNoOpsBeforeInit(t *testing.T) {
	resetAppLogger(t)

	// Must not panic with no logger initialized.
	LogError("should be dropped")
	LogDebug("should also be dropped")
}

func TestInitAppLogThenLogErrorPersists(t *testing.T) {
	resetAppLogger(t)
	dir := t.TempDir()

	if err := InitAppLog(dir); err != nil {
		t.Fatalf("InitAppLog: %v", err)
	}
	LogError("connection failed: %v placeholder")
	LogDebug("debug message")

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("reading app.log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ERROR") || !strings.Contains(content, "connection failed") {
		t.Errorf("app.log = %q, want an ERROR line with the message", content)
	}
	if !strings.Contains(content, "DEBUG") || !strings.Contains(content, "debug message") {
		t.Errorf("app.log = %q, want a DEBUG line with the message", content)
	}
}

func TestInitAppLogPropagatesOpenError(t *testing.T) {
	resetAppLogger(t)

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := InitAppLog(filepath.Join(blocker, "logs")); err == nil {
		t.Error("InitAppLog with an unmakeable dir should error, got nil")
	}
}

func TestCloseAppLogIsNoOpBeforeInit(t *testing.T) {
	resetAppLogger(t)

	if err := CloseAppLog(); err != nil {
		t.Errorf("CloseAppLog before InitAppLog = %v, want nil", err)
	}
}

func TestCloseAppLogThenLogErrorIsNoOp(t *testing.T) {
	resetAppLogger(t)
	dir := t.TempDir()

	if err := InitAppLog(dir); err != nil {
		t.Fatalf("InitAppLog: %v", err)
	}
	if err := CloseAppLog(); err != nil {
		t.Fatalf("CloseAppLog: %v", err)
	}

	// Must not panic, and must not reopen/write to the now-closed logger.
	LogError("dropped after close")
}
