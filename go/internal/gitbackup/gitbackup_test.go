package gitbackup

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

// fakeGitClient records calls and returns configurable errors for testing
// Syncer.SyncIfNeeded's trigger logic in isolation from real git/network.
type fakeGitClient struct {
	openOrCloneCalls   int
	commitAndPushCalls int
	openOrCloneErr     error
	commitAndPushErr   error
}

func (f *fakeGitClient) openOrClone(repoDir, remoteURL, keyPath string) (*git.Repository, error) {
	f.openOrCloneCalls++
	if f.openOrCloneErr != nil {
		return nil, f.openOrCloneErr
	}
	return &git.Repository{}, nil
}

func (f *fakeGitClient) commitAndPush(repo *git.Repository, keyPath, message string) (pushed bool, err error) {
	f.commitAndPushCalls++
	if f.commitAndPushErr != nil {
		return false, f.commitAndPushErr
	}
	return true, nil
}

func TestSyncIfNeededNoOpWhenNeitherConditionHolds(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")

	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now.Add(-2 * time.Minute), // 2 minutes ago (well under the 5-minute interval)
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	if err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}
	if fakeGit.openOrCloneCalls != 0 || fakeGit.commitAndPushCalls != 0 {
		t.Errorf("expected no git calls, got openOrClone=%d commitAndPush=%d", fakeGit.openOrCloneCalls, fakeGit.commitAndPushCalls)
	}
}

func TestSyncIfNeededFiresOnNewFile(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-03.csv"), "header\n")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now,
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	if err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}
	if fakeGit.openOrCloneCalls != 1 || fakeGit.commitAndPushCalls != 1 {
		t.Errorf("expected one sync, got openOrClone=%d commitAndPush=%d", fakeGit.openOrCloneCalls, fakeGit.commitAndPushCalls)
	}
	if syncer.lastFile != "readings-2006-01-03.csv" {
		t.Errorf("lastFile = %q, want readings-2006-01-03.csv", syncer.lastFile)
	}
}

func TestSyncIfNeededFiresOnIntervalElapsed(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")

	lastSyncedTime := time.Date(2006, 1, 2, 11, 0, 0, 0, time.UTC)
	currentTime := time.Date(2006, 1, 2, 11, 6, 0, 0, time.UTC) // 6 minutes later (just over the 5-minute interval)

	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: lastSyncedTime,
		clock:      func() time.Time { return currentTime },
		git:        fakeGit,
	}

	if err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}
	if fakeGit.openOrCloneCalls != 1 || fakeGit.commitAndPushCalls != 1 {
		t.Errorf("expected one sync, got openOrClone=%d commitAndPush=%d", fakeGit.openOrCloneCalls, fakeGit.commitAndPushCalls)
	}
	if syncer.lastSynced != currentTime {
		t.Errorf("lastSynced = %v, want %v", syncer.lastSynced, currentTime)
	}
}

func TestSyncIfNeededGitErrorDoesNotUpdateState(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-03.csv"), "header\n")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	lastSyncedTime := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)

	fakeGit := &fakeGitClient{commitAndPushErr: errors.New("push failed")}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: lastSyncedTime,
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "commit and push") {
		t.Fatalf("SyncIfNeeded err = %v, want it to contain \"commit and push\"", err)
	}
	if syncer.lastFile != "readings-2006-01-02.csv" || syncer.lastSynced != lastSyncedTime {
		t.Errorf("state changed after a failed sync: lastFile=%q lastSynced=%v, want unchanged", syncer.lastFile, syncer.lastSynced)
	}
}

func TestSyncIfNeededOpenOrCloneErrorDoesNotUpdateState(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-03.csv"), "header\n")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{openOrCloneErr: errors.New("clone failed")}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now.Add(-2 * time.Hour),
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "open or clone repo") {
		t.Fatalf("SyncIfNeeded err = %v, want it to contain \"open or clone repo\"", err)
	}
	if syncer.lastFile != "readings-2006-01-02.csv" {
		t.Errorf("lastFile changed after a failed sync, want unchanged")
	}
	if fakeGit.commitAndPushCalls != 0 {
		t.Errorf("commitAndPush should not be called when openOrClone fails")
	}
}

func TestSyncIfNeededCurrentReadingFileErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// readingsDir is a plain file, not a directory, so
	// currentReadingFile's os.ReadDir fails.
	readingsDir := filepath.Join(dir, "readings-as-file")
	mustWriteFile(t, readingsDir, "x")

	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir: filepath.Join(dir, "repo"),
		keyPath: "/fake/key",
		clock:   time.Now,
		git:     fakeGit,
	}

	err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "find current reading file") {
		t.Fatalf("SyncIfNeeded err = %v, want it to contain \"find current reading file\"", err)
	}
	if fakeGit.openOrCloneCalls != 0 {
		t.Errorf("openOrClone should not be called when currentReadingFile fails")
	}
}

func TestSyncIfNeededEmptyReadingsDir(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)

	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "", // no prior sync, and no reading files means currentFile is also ""
		lastSynced: now,
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	if err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}
	if fakeGit.openOrCloneCalls != 0 {
		t.Errorf("openOrCloneCalls = %d, want 0", fakeGit.openOrCloneCalls)
	}
}

func TestSyncIfNeededCopiesFilesIntoRepo(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	repoDir := filepath.Join(dir, "repo")
	appLogPath := filepath.Join(dir, "app.log")

	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")
	mustWriteFile(t, appLogPath, "log\n")

	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	syncer := &Syncer{
		repoDir:    repoDir,
		keyPath:    "/fake/key",
		lastFile:   "old.csv",
		lastSynced: now.Add(-2 * time.Hour),
		clock:      func() time.Time { return now },
		git:        &fakeGitClient{},
	}

	if err := syncer.SyncIfNeeded(readingsDir, appLogPath); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}

	logsDir := filepath.Join(repoDir, "logs")
	if _, err := os.Stat(filepath.Join(logsDir, "readings-2006-01-02.csv")); err != nil {
		t.Errorf("readings file not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logsDir, "app.log")); err != nil {
		t.Errorf("app.log not copied: %v", err)
	}
}

func TestSyncIfNeededCopyErrorDoesNotUpdateState(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-03.csv"), "header\n")

	// repoDir already exists as a plain file, so copyLogsToRepo's
	// MkdirAll(repoDir/logs) fails.
	repoDir := filepath.Join(dir, "repo")
	mustWriteFile(t, repoDir, "not a directory")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    repoDir,
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now.Add(-2 * time.Hour),
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "copy logs to repo") {
		t.Fatalf("SyncIfNeeded err = %v, want it to contain \"copy logs to repo\"", err)
	}
	if fakeGit.commitAndPushCalls != 0 {
		t.Errorf("commitAndPush should not be called when copying logs fails")
	}
}

func TestNewSyncerReturnsRealDefaults(t *testing.T) {
	syncer := NewSyncer("/test/repo", "/test/key")
	if syncer.clock == nil {
		t.Error("NewSyncer should set a default clock")
	}
	if syncer.git == nil {
		t.Error("NewSyncer should set a default gitClient")
	}
	if _, ok := syncer.git.(*realGitClient); !ok {
		t.Errorf("NewSyncer's default git client is %T, want *realGitClient", syncer.git)
	}
}

// --- SyncNow ---

func TestSyncNowBypassesGateAndAlwaysSyncs(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")

	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now, // no time has passed, and file hasn't changed
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	// SyncIfNeeded would be a no-op in this state.
	if err := syncer.SyncIfNeeded(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncIfNeeded: %v", err)
	}
	if fakeGit.openOrCloneCalls != 0 || fakeGit.commitAndPushCalls != 0 {
		t.Errorf("SyncIfNeeded should be a no-op, got openOrClone=%d commitAndPush=%d", fakeGit.openOrCloneCalls, fakeGit.commitAndPushCalls)
	}

	// But SyncNow should proceed regardless.
	fakeGit.openOrCloneCalls = 0
	fakeGit.commitAndPushCalls = 0
	if err := syncer.SyncNow(readingsDir, filepath.Join(dir, "app.log")); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if fakeGit.openOrCloneCalls != 1 || fakeGit.commitAndPushCalls != 1 {
		t.Errorf("SyncNow should bypass gate, got openOrClone=%d commitAndPush=%d", fakeGit.openOrCloneCalls, fakeGit.commitAndPushCalls)
	}
}

func TestSyncNowErrorDoesNotUpdateState(t *testing.T) {
	dir := t.TempDir()
	readingsDir := filepath.Join(dir, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-03.csv"), "header\n")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{commitAndPushErr: errors.New("push failed")}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now.Add(-2 * time.Hour),
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	err := syncer.SyncNow(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "commit and push") {
		t.Fatalf("SyncNow err = %v, want it to contain \"commit and push\"", err)
	}
	if syncer.lastFile != "readings-2006-01-02.csv" || syncer.lastSynced != now.Add(-2*time.Hour) {
		t.Errorf("state changed after a failed SyncNow: lastFile=%q lastSynced=%v, want unchanged", syncer.lastFile, syncer.lastSynced)
	}
}

func TestSyncNowCurrentReadingFileErrorDoesNotUpdateState(t *testing.T) {
	dir := t.TempDir()
	// readingsDir is a plain file, not a directory, so currentReadingFile fails.
	readingsDir := filepath.Join(dir, "readings-as-file")
	mustWriteFile(t, readingsDir, "x")

	now := time.Date(2006, 1, 3, 12, 0, 0, 0, time.UTC)
	fakeGit := &fakeGitClient{}
	syncer := &Syncer{
		repoDir:    filepath.Join(dir, "repo"),
		keyPath:    "/fake/key",
		lastFile:   "readings-2006-01-02.csv",
		lastSynced: now.Add(-2 * time.Hour),
		clock:      func() time.Time { return now },
		git:        fakeGit,
	}

	err := syncer.SyncNow(readingsDir, filepath.Join(dir, "app.log"))
	if err == nil || !strings.Contains(err.Error(), "find current reading file") {
		t.Fatalf("SyncNow err = %v, want it to contain \"find current reading file\"", err)
	}
	if syncer.lastFile != "readings-2006-01-02.csv" || syncer.lastSynced != now.Add(-2*time.Hour) {
		t.Errorf("state changed after a failed SyncNow: lastFile=%q lastSynced=%v, want unchanged", syncer.lastFile, syncer.lastSynced)
	}
	if fakeGit.openOrCloneCalls != 0 {
		t.Errorf("openOrClone should not be called when currentReadingFile fails")
	}
}

func TestSyncNowRealGitClientFullCycle(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)

	// Override remoteURL so SyncNow uses the local bare repo instead of GitHub.
	oldRemoteURL := remoteURL
	remoteURL = bareDir
	t.Cleanup(func() { remoteURL = oldRemoteURL })

	repoDir := filepath.Join(root, "repo")
	readingsDir := filepath.Join(root, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")
	appLogPath := filepath.Join(root, "app.log")
	mustWriteFile(t, appLogPath, "log\n")

	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	syncer := &Syncer{
		repoDir:    repoDir,
		keyPath:    keyPath,
		lastFile:   "", // no prior sync, forcing doSync to get the current file
		lastSynced: now.Add(-10 * time.Hour),
		clock:      func() time.Time { return now },
		git:        &realGitClient{},
	}

	// First SyncNow creates and pushes.
	if err := syncer.SyncNow(readingsDir, appLogPath); err != nil {
		t.Fatalf("first SyncNow: %v", err)
	}
	if syncer.lastFile != "readings-2006-01-02.csv" {
		t.Errorf("lastFile = %q, want readings-2006-01-02.csv", syncer.lastFile)
	}
	if syncer.lastSynced != now {
		t.Errorf("lastSynced = %v, want %v", syncer.lastSynced, now)
	}

	// Verify state was updated.
	if syncer.lastFile == "" || syncer.lastSynced.Year() != 2006 {
		t.Fatalf("SyncNow didn't update state: lastFile=%q lastSynced=%v", syncer.lastFile, syncer.lastSynced)
	}

	// Verify the push worked by cloning again and checking for the files.
	repoDir2 := filepath.Join(root, "repo2")
	repo2, err := (&realGitClient{}).openOrClone(repoDir2, bareDir, keyPath)
	if err != nil {
		t.Fatalf("clone after SyncNow: %v", err)
	}
	if repo2 == nil {
		t.Fatal("clone returned nil repo")
	}
	if _, err := os.Stat(filepath.Join(repoDir2, "logs", "readings-2006-01-02.csv")); err != nil {
		t.Errorf("readings file not in cloned repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir2, "logs", "app.log")); err != nil {
		t.Errorf("app.log not in cloned repo: %v", err)
	}
}

func TestNetworkTimeoutEnforced(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)

	repoDir := filepath.Join(root, "repo")
	readingsDir := filepath.Join(root, "readings")
	mustMkdirAll(t, readingsDir)
	mustWriteFile(t, filepath.Join(readingsDir, "readings-2006-01-02.csv"), "header\n")
	appLogPath := filepath.Join(root, "app.log")

	// Save the original timeout and restore it at the end.
	oldTimeout := networkTimeout
	networkTimeout = 1 * time.Nanosecond
	t.Cleanup(func() { networkTimeout = oldTimeout })

	realGit := &realGitClient{}
	now := time.Date(2006, 1, 2, 12, 0, 0, 0, time.UTC)
	syncer := &Syncer{
		repoDir:    repoDir,
		keyPath:    keyPath,
		lastFile:   "",
		lastSynced: now.Add(-10 * time.Hour),
		clock:      func() time.Time { return now },
		git:        realGit,
	}

	// With an absurdly short timeout, even a local bare repo should timeout.
	err := syncer.SyncNow(readingsDir, appLogPath)
	if err == nil {
		t.Fatal("SyncNow should timeout with networkTimeout set to 1 nanosecond")
	}
	if !strings.Contains(err.Error(), "clone repo") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("err = %v, want it to contain \"clone repo\" or \"context deadline exceeded\"", err)
	}

	// State should not have been updated due to the error.
	if syncer.lastFile != "" || syncer.lastSynced != now.Add(-10*time.Hour) {
		t.Errorf("state should not have been updated on error")
	}
}

// --- currentReadingFile ---

func TestCurrentReadingFileEmpty(t *testing.T) {
	dir := t.TempDir()
	file, err := currentReadingFile(dir)
	if err != nil {
		t.Fatalf("currentReadingFile: %v", err)
	}
	if file != "" {
		t.Errorf("file = %q, want empty string", file)
	}
}

func TestCurrentReadingFileDirDoesNotExist(t *testing.T) {
	file, err := currentReadingFile(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("currentReadingFile: %v", err)
	}
	if file != "" {
		t.Errorf("file = %q, want empty string", file)
	}
}

func TestCurrentReadingFileReadDirError(t *testing.T) {
	// A path that exists but is a file, not a directory, makes
	// os.ReadDir fail with a non-NotExist error.
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	mustWriteFile(t, notADir, "x")

	_, err := currentReadingFile(notADir)
	if err == nil {
		t.Fatal("currentReadingFile should error when readingsDir is not a directory")
	}
}

func TestCurrentReadingFileIgnoresNonMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "notes.txt"), "x")

	file, err := currentReadingFile(dir)
	if err != nil {
		t.Fatalf("currentReadingFile: %v", err)
	}
	if file != "" {
		t.Errorf("file = %q, want empty string (no readings-*.csv present)", file)
	}
}

func TestCurrentReadingFileMultipleFilesReturnsNewest(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"readings-2006-01-01.csv",
		"readings-2006-01-20.csv",
		"readings-2006-01-02.csv",
		"notarealfile.txt",
	} {
		mustWriteFile(t, filepath.Join(dir, f), "header\n")
	}

	file, err := currentReadingFile(dir)
	if err != nil {
		t.Fatalf("currentReadingFile: %v", err)
	}
	if file != "readings-2006-01-20.csv" {
		t.Errorf("file = %q, want readings-2006-01-20.csv", file)
	}
}

// --- copyLogsToRepo / copyFile ---

func TestCopyLogsToRepoNonexistentReadingsDir(t *testing.T) {
	repoDir := t.TempDir()
	readingsDir := filepath.Join(t.TempDir(), "nonexistent", "readings")
	appLogPath := filepath.Join(t.TempDir(), "app.log")

	if err := copyLogsToRepo(repoDir, readingsDir, appLogPath); err != nil {
		t.Fatalf("copyLogsToRepo with nonexistent readings dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "logs")); err != nil {
		t.Errorf("logs dir not created: %v", err)
	}
}

func TestCopyLogsToRepoReadingsDirReadError(t *testing.T) {
	repoDir := t.TempDir()
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	mustWriteFile(t, notADir, "x")

	err := copyLogsToRepo(repoDir, notADir, filepath.Join(t.TempDir(), "app.log"))
	if err == nil {
		t.Fatal("copyLogsToRepo should error when readingsDir is not a directory")
	}
}

func TestCopyLogsToRepoMkdirAllFails(t *testing.T) {
	// repoDir is a plain file, so MkdirAll(repoDir/logs) fails.
	repoDir := filepath.Join(t.TempDir(), "repo-as-file")
	mustWriteFile(t, repoDir, "x")

	err := copyLogsToRepo(repoDir, t.TempDir(), filepath.Join(t.TempDir(), "app.log"))
	if err == nil {
		t.Fatal("copyLogsToRepo should error when repoDir/logs can't be created")
	}
}

func TestCopyLogsToRepoWithAppLogOnly(t *testing.T) {
	repoDir := t.TempDir()
	readingsDir := t.TempDir()
	appLogPath := filepath.Join(t.TempDir(), "app.log")
	mustWriteFile(t, appLogPath, "log content")

	if err := copyLogsToRepo(repoDir, readingsDir, appLogPath); err != nil {
		t.Fatalf("copyLogsToRepo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "logs", "app.log")); err != nil {
		t.Errorf("app.log not copied: %v", err)
	}
}

func TestCopyLogsToRepoReadingsFileCopyErrorPropagates(t *testing.T) {
	repoDir := t.TempDir()
	readingsDir := t.TempDir()
	readingsFile := filepath.Join(readingsDir, "readings-2006-01-02.csv")
	mustWriteFile(t, readingsFile, "data")
	// Make it unreadable so copyFile's os.ReadFile fails.
	if err := os.Chmod(readingsFile, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(readingsFile, 0o644) })

	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions don't block reads")
	}

	err := copyLogsToRepo(repoDir, readingsDir, filepath.Join(t.TempDir(), "app.log"))
	if err == nil {
		t.Fatal("copyLogsToRepo should propagate a copyFile error for an unreadable reading file")
	}
}

func TestCopyLogsToRepoAppLogCopyErrorPropagates(t *testing.T) {
	repoDir := t.TempDir()
	readingsDir := t.TempDir()
	appLogPath := filepath.Join(t.TempDir(), "app.log")
	mustWriteFile(t, appLogPath, "log")
	if err := os.Chmod(appLogPath, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(appLogPath, 0o644) })

	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions don't block reads")
	}

	err := copyLogsToRepo(repoDir, readingsDir, appLogPath)
	if err == nil {
		t.Fatal("copyLogsToRepo should propagate a copyFile error for an unreadable app.log")
	}
}

func TestCopyFileSourceNotExist(t *testing.T) {
	err := copyFile("/nonexistent/source", filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Error("copyFile with nonexistent source should error")
	}
}

func TestCopyFileWriteFails(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	mustWriteFile(t, src, "data")
	// dst's parent directory doesn't exist, so os.WriteFile fails.
	dst := filepath.Join(t.TempDir(), "missing-subdir", "dst")

	if err := copyFile(src, dst); err == nil {
		t.Error("copyFile should error when dst's directory doesn't exist")
	}
}

func TestCopyFileSuccess(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	mustWriteFile(t, src, "test content")

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "test content" {
		t.Errorf("dst content = %q, want %q", got, "test content")
	}
}

// --- authMethodFromKey ---

func TestAuthMethodFromKeyNonexistentFile(t *testing.T) {
	if _, err := authMethodFromKey("/nonexistent/path/to/key"); err == nil {
		t.Error("authMethodFromKey with nonexistent file should error")
	}
}

func TestAuthMethodFromKeyInvalidKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "bad_key")
	mustWriteFile(t, keyPath, "not a valid key")

	if _, err := authMethodFromKey(keyPath); err == nil {
		t.Error("authMethodFromKey with invalid key should error")
	}
}

func TestAuthMethodFromKeyValidKey(t *testing.T) {
	keyPath := generateTestKey(t)
	auth, err := authMethodFromKey(keyPath)
	if err != nil {
		t.Errorf("authMethodFromKey with a valid key: %v", err)
	}

	publicKeys, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatalf("authMethodFromKey returned %T, want *gitssh.PublicKeys", auth)
	}
	if publicKeys.HostKeyCallback == nil {
		t.Error("authMethodFromKey must set HostKeyCallback — a nil callback falls back to " +
			"go-git's known_hosts-file lookup, which can never succeed in this app's sandbox")
	}
	wantAlgorithms := []string{"ssh-ed25519"}
	if !slices.Equal(publicKeys.HostKeyAlgorithms, wantAlgorithms) {
		t.Errorf("authMethodFromKey HostKeyAlgorithms = %v, want %v — without this, host key "+
			"negotiation isn't guaranteed to pick the ed25519 key FixedHostKey is pinned to, "+
			"and a real GitHub connection fails with \"host key mismatch\" (DESIGN.md section 7)",
			publicKeys.HostKeyAlgorithms, wantAlgorithms)
	}
}

func TestHostKeyCallbackForKeyValid(t *testing.T) {
	callback, err := hostKeyCallbackForKey(githubEd25519HostKey)
	if err != nil {
		t.Fatalf("hostKeyCallbackForKey(githubEd25519HostKey): %v", err)
	}
	if callback == nil {
		t.Error("expected a non-nil HostKeyCallback for a valid key")
	}
}

func TestHostKeyCallbackForKeyInvalid(t *testing.T) {
	_, err := hostKeyCallbackForKey("not a valid authorized-keys line")
	if err == nil {
		t.Error("hostKeyCallbackForKey with an invalid key line should error")
	}
}

func TestAuthMethodFromKeyPropagatesHostKeyParseError(t *testing.T) {
	old := pinnedHostKeyText
	pinnedHostKeyText = "not a valid authorized-keys line"
	t.Cleanup(func() { pinnedHostKeyText = old })

	keyPath := generateTestKey(t)
	if _, err := authMethodFromKey(keyPath); err == nil {
		t.Error("authMethodFromKey should propagate a hostKeyCallbackForKey parse error")
	}
}

// --- realGitClient: end-to-end against a local bare repo standing in for
// car_monitor_logs.git, so clone/commit/push are exercised for real
// without any network or a registered GitHub deploy key. ---

func TestRealGitClientFullSyncCycle(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)
	realGit := &realGitClient{}

	// First device, first-ever sync: bareDir is empty, so openOrClone must
	// take the PlainInit+CreateRemote fallback, not PlainClone.
	repoDirA := filepath.Join(root, "repoA")
	repoA, err := realGit.openOrClone(repoDirA, bareDir, keyPath)
	if err != nil {
		t.Fatalf("openOrClone (empty remote): %v", err)
	}
	mustMkdirAll(t, filepath.Join(repoDirA, "logs"))
	mustWriteFile(t, filepath.Join(repoDirA, "logs", "readings-2006-01-02.csv"), "header\n")

	pushed, err := realGit.commitAndPush(repoA, keyPath, "first backup")
	if err != nil {
		t.Fatalf("commitAndPush (first): %v", err)
	}
	if !pushed {
		t.Error("expected the first commit to be pushed")
	}

	// Same directory, no changes: IsClean() should short-circuit to
	// pushed=false, err=nil without hitting the network at all.
	pushed, err = realGit.commitAndPush(repoA, keyPath, "no-op backup")
	if err != nil {
		t.Fatalf("commitAndPush (no changes): %v", err)
	}
	if pushed {
		t.Error("expected pushed=false when nothing changed")
	}

	// Re-opening the same local repo dir should hit the PlainOpen
	// success path, not clone again.
	repoA2, err := realGit.openOrClone(repoDirA, bareDir, keyPath)
	if err != nil {
		t.Fatalf("openOrClone (reopen): %v", err)
	}
	if repoA2 == nil {
		t.Error("openOrClone should return a repo on reopen")
	}

	// A second device cloning fresh: bareDir now has a commit, so this
	// exercises PlainClone's real success path (not the empty fallback).
	repoDirB := filepath.Join(root, "repoB")
	repoB, err := realGit.openOrClone(repoDirB, bareDir, keyPath)
	if err != nil {
		t.Fatalf("openOrClone (non-empty remote clone): %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDirB, "logs", "readings-2006-01-02.csv")); err != nil {
		t.Errorf("cloned repo missing the file pushed by device A: %v", err)
	}

	// Device B pushes a second commit — a genuine second real push,
	// distinct from device A's first-ever push.
	mustWriteFile(t, filepath.Join(repoDirB, "logs", "readings-2006-01-03.csv"), "header\n")
	pushed, err = realGit.commitAndPush(repoB, keyPath, "second backup")
	if err != nil {
		t.Fatalf("commitAndPush (device B): %v", err)
	}
	if !pushed {
		t.Error("expected device B's commit to be pushed")
	}
}

func TestOpenOrCloneRealCloneFailure(t *testing.T) {
	realGit := &realGitClient{}
	keyPath := generateTestKey(t)

	// Points at a path that isn't a git repo at all — PlainOpen fails,
	// and the "clone" from it fails too (not with ErrEmptyRemoteRepository,
	// so the generic clone-failure branch is what's exercised).
	notARepo := t.TempDir()
	_, err := realGit.openOrClone(filepath.Join(t.TempDir(), "dst"), notARepo, keyPath)
	if err == nil {
		t.Fatal("openOrClone should fail cloning from a non-repository path")
	}
	if !strings.Contains(err.Error(), "clone repo") {
		t.Errorf("err = %v, want it to contain \"clone repo\"", err)
	}
}

func TestOpenOrCloneAuthLoadFailure(t *testing.T) {
	realGit := &realGitClient{}
	_, err := realGit.openOrClone(filepath.Join(t.TempDir(), "dst"), "git@github.com:fake/repo.git", "/nonexistent/key")
	if err == nil || !strings.Contains(err.Error(), "load SSH key") {
		t.Fatalf("err = %v, want it to contain \"load SSH key\"", err)
	}
}

func TestOpenOrClonePlainInitFailsOnEmptyRemote(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)

	// By the time openOrClone's empty-remote fallback calls PlainInit,
	// PlainClone has already proven repoDir is a valid, writable target
	// (that's how it got far enough to discover the remote was merely
	// empty) — so forcing a real PlainInit failure at that exact point
	// isn't practically reachable without this seam.
	old := plainInitFunc
	plainInitFunc = func(path string, isBare bool) (*git.Repository, error) {
		return nil, errors.New("mock init failure")
	}
	t.Cleanup(func() { plainInitFunc = old })

	realGit := &realGitClient{}
	_, err := realGit.openOrClone(filepath.Join(root, "repo"), bareDir, keyPath)
	if err == nil || !strings.Contains(err.Error(), "init repo for empty remote") {
		t.Fatalf("err = %v, want it to contain \"init repo for empty remote\"", err)
	}
}

func TestOpenOrCloneCreateRemoteFails(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)

	old := createRemoteFunc
	createRemoteFunc = func(repo *git.Repository, cfg *config.RemoteConfig) (*git.Remote, error) {
		return nil, errors.New("mock create remote failure")
	}
	t.Cleanup(func() { createRemoteFunc = old })

	realGit := &realGitClient{}
	repoDir := filepath.Join(root, "repo")
	_, err := realGit.openOrClone(repoDir, bareDir, keyPath)
	if err == nil || !strings.Contains(err.Error(), "create remote for empty repo") {
		t.Fatalf("err = %v, want it to contain \"create remote for empty repo\"", err)
	}
}

func TestCommitAndPushWorktreeErrorOnBareRepo(t *testing.T) {
	bareDir := t.TempDir()
	repo, err := git.PlainInit(bareDir, true)
	if err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	realGit := &realGitClient{}
	_, err = realGit.commitAndPush(repo, generateTestKey(t), "msg")
	if err == nil || !strings.Contains(err.Error(), "get worktree") {
		t.Fatalf("err = %v, want it to contain \"get worktree\"", err)
	}
}

func TestCommitAndPushAddFailsOnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions don't block reads")
	}
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	filePath := filepath.Join(repoDir, "a.txt")
	mustWriteFile(t, filePath, "data")
	if err := os.Chmod(filePath, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(filePath, 0o644) })

	realGit := &realGitClient{}
	pushed, err := realGit.commitAndPush(repo, generateTestKey(t), "msg")
	if err == nil || !strings.Contains(err.Error(), "add files") {
		t.Fatalf("err = %v, want it to contain \"add files\"", err)
	}
	if pushed {
		t.Error("pushed should be false on error")
	}
}

func TestCommitAndPushStatusError(t *testing.T) {
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	mustWriteFile(t, filepath.Join(repoDir, "a.txt"), "data")

	old := worktreeStatusFunc
	worktreeStatusFunc = func(wt *git.Worktree) (git.Status, error) {
		return nil, errors.New("mock status failure")
	}
	t.Cleanup(func() { worktreeStatusFunc = old })

	realGit := &realGitClient{}
	pushed, err := realGit.commitAndPush(repo, generateTestKey(t), "msg")
	if err == nil || !strings.Contains(err.Error(), "get status") {
		t.Fatalf("err = %v, want it to contain \"get status\"", err)
	}
	if pushed {
		t.Error("pushed should be false on error")
	}
}

func TestCommitAndPushCommitFailsWhenObjectsUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions don't block writes")
	}
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	mustWriteFile(t, filepath.Join(repoDir, "a.txt"), "data")

	// Pre-stage the file while .git/objects is still writable, so the
	// blob object is already on disk — commitAndPush's own AddWithOptions
	// then has nothing new to write and succeeds even once objects
	// becomes read-only below, isolating the failure to wt.Commit's tree
	// and commit objects, which are genuinely new.
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatalf("pre-stage AddWithOptions: %v", err)
	}

	objectsDir := filepath.Join(repoDir, ".git", "objects")
	if err := os.Chmod(objectsDir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(objectsDir, 0o755) })

	realGit := &realGitClient{}
	pushed, err := realGit.commitAndPush(repo, generateTestKey(t), "msg")
	if err == nil || !strings.Contains(err.Error(), "commit:") {
		t.Fatalf("err = %v, want it to contain \"commit:\"", err)
	}
	if pushed {
		t.Error("pushed should be false on error")
	}
}

func TestCommitAndPushPushFailsWithoutRemote(t *testing.T) {
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	mustWriteFile(t, filepath.Join(repoDir, "a.txt"), "data")

	realGit := &realGitClient{}
	pushed, err := realGit.commitAndPush(repo, generateTestKey(t), "msg")
	if err == nil {
		t.Fatal("commitAndPush should fail pushing a repo with no configured remote")
	}
	if pushed {
		t.Error("pushed should be false on a push failure")
	}
	if !strings.Contains(err.Error(), "push") {
		t.Errorf("err = %v, want it to contain \"push\"", err)
	}
}

func TestCommitAndPushAuthLoadFailureDuringPush(t *testing.T) {
	root := t.TempDir()
	bareDir := filepath.Join(root, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	keyPath := generateTestKey(t)
	realGit := &realGitClient{}
	repoDir := filepath.Join(root, "repo")
	repo, err := realGit.openOrClone(repoDir, bareDir, keyPath)
	if err != nil {
		t.Fatalf("openOrClone: %v", err)
	}
	mustMkdirAll(t, filepath.Join(repoDir, "logs"))
	mustWriteFile(t, filepath.Join(repoDir, "logs", "a.csv"), "data")

	old := authMethodFunc
	authMethodFunc = func(keyPath string) (gitssh.AuthMethod, error) {
		return nil, errors.New("mock auth error")
	}
	t.Cleanup(func() { authMethodFunc = old })

	pushed, err := realGit.commitAndPush(repo, keyPath, "msg")
	if err == nil || !strings.Contains(err.Error(), "load SSH key for push") {
		t.Fatalf("err = %v, want it to contain \"load SSH key for push\"", err)
	}
	if pushed {
		t.Error("pushed should be false on error")
	}
}

// --- test helpers ---

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// generateTestKey writes a throwaway, validly-encoded ed25519 SSH private
// key to a temp file and returns its path. It authenticates nothing real
// (never registered anywhere) — its only job is to parse successfully so
// authMethodFromKey/ssh.NewPublicKeys succeed, since go-git's local
// filesystem transport (used against the bare repos in these tests)
// never actually uses the Auth method, just requires one to be
// constructible.
func generateTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile key: %v", err)
	}
	return keyPath
}
