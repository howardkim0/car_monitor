package mobile

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/howardkim0/car_monitor/go/internal/gitbackup"
	"github.com/howardkim0/car_monitor/go/internal/sshkey"
)

// gitLogSyncer is the subset of *gitbackup.Syncer this file depends on,
// factored out purely so tests can inject a fake — the real Syncer talks
// to the hardcoded car_monitor_logs.git remote over SSH, which has no
// reachable success path in a unit test (same reasoning as
// newSessionWithStore's fake Store below).
type gitLogSyncer interface {
	SyncIfNeeded(readingsDir, appLogPath string) error
	SyncNow(readingsDir, appLogPath string) error
}

// gitSyncer is package-level and independent of any Session deliberately:
// a Session is recreated on every Bluetooth reconnect (see
// ObdForegroundService.openConnection()), but git backup state must
// persist across that churn so it syncs on a log-rotation or hourly
// cadence, not on every connection.
var (
	gitSyncerMu sync.Mutex
	gitSyncer   gitLogSyncer
)

// SyncLogsIfNeeded backs up logs to car_monitor_logs.git if a new log
// file has appeared or the sync interval has elapsed since the last sync.
// Safe to call frequently (e.g. every few minutes) — it's a no-op otherwise.
// storageDir is the app's private storage root, same as NewSession.
func SyncLogsIfNeeded(storageDir string) error {
	syncer := currentOrCreateGitSyncer(storageDir)

	if err := syncer.SyncIfNeeded(
		filepath.Join(storageDir, "readings"),
		filepath.Join(storageDir, "app.log"),
	); err != nil {
		LogError(fmt.Sprintf("git backup sync: %v", err))
		return err
	}

	return nil
}

// ForceSyncLogs performs an immediate, ungated push to car_monitor_logs.git,
// regardless of whether the normal sync conditions (new file or interval elapsed)
// hold. Used by the "Git Push" button on the status screen so a user can
// confirm backup is working without waiting for the next automatic check.
// storageDir is the app's private storage root, same as NewSession.
func ForceSyncLogs(storageDir string) error {
	syncer := currentOrCreateGitSyncer(storageDir)

	if err := syncer.SyncNow(
		filepath.Join(storageDir, "readings"),
		filepath.Join(storageDir, "app.log"),
	); err != nil {
		LogError(fmt.Sprintf("git backup force sync: %v", err))
		return err
	}

	return nil
}

// currentOrCreateGitSyncer never fails (gitbackup.NewSyncer does no I/O —
// it just stores paths and creation of the repo/key is deferred to
// SyncIfNeeded), so unlike NewSession's storage.OpenFileStore this has no
// error to plumb through.
func currentOrCreateGitSyncer(storageDir string) gitLogSyncer {
	gitSyncerMu.Lock()
	defer gitSyncerMu.Unlock()

	if gitSyncer == nil {
		gitSyncer = gitbackup.NewSyncer(
			filepath.Join(storageDir, "backup-repo"),
			sshkey.PrivateKeyPath(filepath.Join(storageDir, "ssh")),
		)
	}

	return gitSyncer
}

// resetGitSyncer is a test-only function to reset the package-level
// syncer between tests to avoid cross-test pollution.
func resetGitSyncer() {
	gitSyncerMu.Lock()
	defer gitSyncerMu.Unlock()
	gitSyncer = nil
}

// setGitSyncerForTest installs a fake gitLogSyncer, bypassing
// currentOrCreateGitSyncer's lazy real-Syncer construction — the same
// injection idea as newSessionWithStore's fake Store, needed because the
// real Syncer's only remote is the hardcoded car_monitor_logs.git SSH URL.
func setGitSyncerForTest(fake gitLogSyncer) {
	gitSyncerMu.Lock()
	defer gitSyncerMu.Unlock()
	gitSyncer = fake
}
