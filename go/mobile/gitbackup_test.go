package mobile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/howardkim0/car_monitor/go/internal/gitbackup"
)

// resetGitSyncerForTest forces a clean nil starting state regardless of
// what other tests in this package did to the shared gitSyncer var.
func resetGitSyncerForTest(t *testing.T) {
	t.Helper()
	resetGitSyncer()
	t.Cleanup(func() {
		resetGitSyncer()
	})
}

// fakeGitLogSyncer lets tests deterministically exercise both of
// SyncLogsIfNeeded's branches without touching the real, hardcoded
// car_monitor_logs.git remote.
type fakeGitLogSyncer struct {
	err   error
	calls []struct{ readingsDir, appLogPath string }
}

func (f *fakeGitLogSyncer) SyncIfNeeded(readingsDir, appLogPath string) error {
	f.calls = append(f.calls, struct{ readingsDir, appLogPath string }{readingsDir, appLogPath})
	return f.err
}

func TestSyncLogsIfNeededSuccess(t *testing.T) {
	resetGitSyncerForTest(t)
	fake := &fakeGitLogSyncer{}
	setGitSyncerForTest(fake)

	storageDir := "/some/storage"
	if err := SyncLogsIfNeeded(storageDir); err != nil {
		t.Fatalf("SyncLogsIfNeeded: %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("SyncIfNeeded calls = %d, want 1", len(fake.calls))
	}
	wantReadingsDir := filepath.Join(storageDir, "readings")
	wantAppLogPath := filepath.Join(storageDir, "app.log")
	if fake.calls[0].readingsDir != wantReadingsDir || fake.calls[0].appLogPath != wantAppLogPath {
		t.Errorf("SyncIfNeeded called with (%q, %q), want (%q, %q)",
			fake.calls[0].readingsDir, fake.calls[0].appLogPath, wantReadingsDir, wantAppLogPath)
	}
}

func TestSyncLogsIfNeededLogsAndReturnsError(t *testing.T) {
	resetGitSyncerForTest(t)
	resetAppLogger(t)

	logDir := t.TempDir()
	if err := InitAppLog(logDir); err != nil {
		t.Fatalf("InitAppLog: %v", err)
	}

	wantErr := errors.New("push failed")
	setGitSyncerForTest(&fakeGitLogSyncer{err: wantErr})

	err := SyncLogsIfNeeded("/some/storage")
	if !errors.Is(err, wantErr) {
		t.Fatalf("SyncLogsIfNeeded err = %v, want %v", err, wantErr)
	}

	logData, err := os.ReadFile(filepath.Join(logDir, "app.log"))
	if err != nil {
		t.Fatalf("reading app.log: %v", err)
	}
	if !strings.Contains(string(logData), "git backup sync") {
		t.Errorf("app.log = %q, want it to contain \"git backup sync\"", logData)
	}
}

func TestCurrentOrCreateGitSyncerReusesInstanceAcrossCalls(t *testing.T) {
	resetGitSyncerForTest(t)

	dir := t.TempDir()
	first := currentOrCreateGitSyncer(dir)
	second := currentOrCreateGitSyncer(dir)

	if first != second {
		t.Error("expected the same *gitbackup.Syncer instance across calls, matching applog's package-level-logger pattern (state must survive Bluetooth-reconnect-driven Session recreation)")
	}
}

func TestCurrentOrCreateGitSyncerUsesRealSyncerByDefault(t *testing.T) {
	resetGitSyncerForTest(t)

	dir := t.TempDir()
	syncer := currentOrCreateGitSyncer(dir)

	if _, ok := syncer.(*gitbackup.Syncer); !ok {
		t.Errorf("currentOrCreateGitSyncer's default is %T, want *gitbackup.Syncer", syncer)
	}
}
