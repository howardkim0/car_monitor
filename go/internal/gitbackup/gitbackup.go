// Package gitbackup backs up on-device logs to a remote git repository,
// persisting state across Bluetooth reconnects so logs are synced on a
// file-rotation or 5-minute basis, whichever comes first.
package gitbackup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

var remoteURL = "git@github.com:howardkim0/car_monitor_logs.git"

const syncInterval = 5 * time.Minute

// gitClient abstracts git operations so they can be faked in tests.
type gitClient interface {
	// openOrClone ensures a local clone exists at repoDir, cloning from
	// remoteURL with the given private key if needed, or opening the
	// existing one. Returns an error if the clone or open fails.
	openOrClone(repoDir, remoteURL, keyPath string) (*git.Repository, error)

	// commitAndPush stages all changes, commits if there's anything to
	// commit (returning pushed=false if nothing changed), and pushes.
	// Returns pushed=false, err=nil for AlreadyUpToDate (expected,
	// not an error). Any real git error is returned.
	commitAndPush(repo *git.Repository, keyPath, message string) (pushed bool, err error)
}

// Syncer decides when to back up and performs the git operations. Not a
// Session method — like internal/applog's logger, it needs to persist
// state (lastFile, lastSynced) across Bluetooth reconnects, which
// recreate obd2.Session but not this.
type Syncer struct {
	repoDir    string
	keyPath    string
	lastFile   string
	lastSynced time.Time
	clock      func() time.Time // injected, defaults to time.Now
	git        gitClient        // injected, defaults to the real go-git-backed implementation
}

// NewSyncer returns a new Syncer with production defaults: real clock and
// real go-git implementation.
func NewSyncer(repoDir, keyPath string) *Syncer {
	return &Syncer{
		repoDir: repoDir,
		keyPath: keyPath,
		clock:   time.Now,
		git:     &realGitClient{},
	}
}

// SyncIfNeeded checks whether a new reading-log file has appeared
// (readingsDir's current-day filename differs from the last sync) or
// syncInterval has elapsed since the last successful sync, and if so,
// performs a sync via doSync.
//
// A no-op, returning nil, if neither condition holds. All git/network
// failures are returned as errors for the caller to log — never panics,
// never retried inline (the caller's own periodic trigger is the retry
// mechanism). Failures do not update lastFile/lastSynced, so the next
// check retries the same sync.
func (s *Syncer) SyncIfNeeded(readingsDir, appLogPath string) error {
	// Determine today's current reading-log filename.
	currentFile, err := currentReadingFile(readingsDir)
	if err != nil {
		return fmt.Errorf("find current reading file: %w", err)
	}

	now := s.clock()
	shouldSync := currentFile != s.lastFile || now.Sub(s.lastSynced) >= syncInterval

	if !shouldSync {
		return nil
	}

	return s.doSync(readingsDir, appLogPath)
}

// SyncNow performs the same clone/copy/commit/push sequence as SyncIfNeeded
// but without the gate check — it always attempts a sync, regardless of
// whether a new file has appeared or the interval has elapsed. Used for
// the manual "Git Push" button (DESIGN.md section 7) so a user can confirm
// backup is working without waiting for the next automatic check.
func (s *Syncer) SyncNow(readingsDir, appLogPath string) error {
	return s.doSync(readingsDir, appLogPath)
}

// doSync performs the actual sync logic: open/clone the repo, copy logs,
// commit and push. Only updates lastFile/lastSynced on success.
func (s *Syncer) doSync(readingsDir, appLogPath string) error {
	// Determine today's current reading-log filename to update state on success.
	currentFile, err := currentReadingFile(readingsDir)
	if err != nil {
		return fmt.Errorf("find current reading file: %w", err)
	}

	now := s.clock()

	// Open or clone the repository.
	repo, err := s.git.openOrClone(s.repoDir, remoteURL, s.keyPath)
	if err != nil {
		return fmt.Errorf("open or clone repo: %w", err)
	}

	// Copy reading logs and app log into the repo.
	if err := copyLogsToRepo(s.repoDir, readingsDir, appLogPath); err != nil {
		return fmt.Errorf("copy logs to repo: %w", err)
	}

	// Commit and push. No error if nothing changed.
	if _, err := s.git.commitAndPush(repo, s.keyPath, "Auto-backup: update logs"); err != nil {
		return fmt.Errorf("commit and push: %w", err)
	}

	// Only update state on successful sync.
	s.lastFile = currentFile
	s.lastSynced = now
	return nil
}

// currentReadingFile finds the lexicographically newest readings-*.csv
// in readingsDir, following the same convention as internal/storage.
func currentReadingFile(readingsDir string) (string, error) {
	entries, err := os.ReadDir(readingsDir)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read readings dir: %w", err)
	}
	// Dir doesn't exist yet, or is empty — return empty string; next sync
	// will add the first file when it appears.
	if os.IsNotExist(err) || len(entries) == 0 {
		return "", nil
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "readings-") && strings.HasSuffix(entry.Name(), ".csv") {
			files = append(files, entry.Name())
		}
	}
	if len(files) == 0 {
		return "", nil
	}

	// Sort and take the last (lexicographically newest).
	sort.Strings(files)
	return files[len(files)-1], nil
}

// copyLogsToRepo copies all readings-*.csv from readingsDir and appLogPath
// into s.repoDir/logs/, preserving filenames. Creates the logs/ subdirectory
// if it doesn't exist.
func copyLogsToRepo(repoDir, readingsDir, appLogPath string) error {
	logsDir := filepath.Join(repoDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	// Copy all reading logs.
	entries, err := os.ReadDir(readingsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read readings dir: %w", err)
	}
	if !os.IsNotExist(err) {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), "readings-") && strings.HasSuffix(entry.Name(), ".csv") {
				src := filepath.Join(readingsDir, entry.Name())
				dst := filepath.Join(logsDir, entry.Name())
				if err := copyFile(src, dst); err != nil {
					return fmt.Errorf("copy %s: %w", entry.Name(), err)
				}
			}
		}
	}

	// Copy app log.
	if _, err := os.Stat(appLogPath); err == nil {
		dst := filepath.Join(logsDir, "app.log")
		if err := copyFile(appLogPath, dst); err != nil {
			return fmt.Errorf("copy app.log: %w", err)
		}
	}

	return nil
}

// copyFile copies src to dst, creating dst if it doesn't exist.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// realGitClient wraps the actual go-git implementation.
type realGitClient struct{}

// authMethodFunc is the function used to create SSH auth methods; exposed as a
// package-level variable so tests can override it to use fake implementations.
var authMethodFunc = authMethodFromKey

// createRemoteFunc creates a named remote on repo; exposed as a
// package-level variable (same reasoning as authMethodFunc) so tests can
// force the "create remote for empty repo" error path in openOrClone,
// which go-git otherwise makes practically impossible to hit for real —
// CreateRemote only fails on a config-level problem (e.g. a duplicate
// remote name), and openOrClone only reaches it against a repo it just
// created itself, which can never already have an "origin" remote.
var createRemoteFunc = func(repo *git.Repository, cfg *config.RemoteConfig) (*git.Remote, error) {
	return repo.CreateRemote(cfg)
}

// plainInitFunc creates a new local repository; exposed as a package-level
// variable (same reasoning as createRemoteFunc) so tests can force the
// "init repo for empty remote" error path — by the time openOrClone
// reaches this call, PlainClone has already proven repoDir is a valid,
// writable target (that's how it discovered the remote was merely empty
// rather than failing on the local path), so a real PlainInit failure
// right after isn't practically reachable without this seam.
var plainInitFunc = git.PlainInit

// networkTimeout bounds how long a single clone/push network operation
// may run — exposed as a var (same reasoning as authMethodFunc etc.)
// so tests can force it to expire immediately instead of waiting for a
// slow real timeout. 30s is generous for a small repo push over a weak
// connection while staying well under the 5-minute sync cadence, so a
// hung attempt (no cell service — the motivating case, DESIGN.md
// section 7) fails fast instead of occupying the sync loop.
var networkTimeout = 30 * time.Second

// worktreeStatusFunc computes a worktree's status; exposed as a
// package-level variable (same reasoning as plainInitFunc) so tests can
// force commitAndPush's "get status" error path. A real Status() failure
// (e.g. a read error against a corrupted on-disk index) is a genuine
// possibility on real hardware, but not one a permission trick or a
// corrupted HEAD file was found to reliably reproduce in a test.
var worktreeStatusFunc = func(wt *git.Worktree) (git.Status, error) {
	return wt.Status()
}

func (c *realGitClient) openOrClone(repoDir, remoteURL, keyPath string) (*git.Repository, error) {
	// Try to open existing repository.
	repo, err := git.PlainOpen(repoDir)
	if err == nil {
		return repo, nil
	}

	// Doesn't exist, clone it.
	authMethod, err := authMethodFunc(keyPath)
	if err != nil {
		return nil, fmt.Errorf("load SSH key: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	repo, err = git.PlainCloneContext(ctx, repoDir, false, &git.CloneOptions{
		URL:  remoteURL,
		Auth: authMethod,
	})
	if err == nil {
		return repo, nil
	}

	// car_monitor_logs.git starts out with zero commits — this device may
	// be the very first one to ever back up to it, and go-git's PlainClone
	// (unlike real `git clone`, which tolerates an empty remote) refuses to
	// clone an empty repository. Fall back to an empty local repo with
	// origin pointed at remoteURL; the commitAndPush below creates the
	// remote's first commit on push.
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		repo, err = plainInitFunc(repoDir, false)
		if err != nil {
			return nil, fmt.Errorf("init repo for empty remote: %w", err)
		}
		if _, err := createRemoteFunc(repo, &config.RemoteConfig{
			Name: "origin",
			URLs: []string{remoteURL},
		}); err != nil {
			return nil, fmt.Errorf("create remote for empty repo: %w", err)
		}
		return repo, nil
	}

	return nil, fmt.Errorf("clone repo: %w", err)
}

func (c *realGitClient) commitAndPush(repo *git.Repository, keyPath, message string) (pushed bool, err error) {
	wt, err := repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("get worktree: %w", err)
	}

	// Stage all changes.
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return false, fmt.Errorf("add files: %w", err)
	}

	// Check if there's anything to commit.
	status, err := worktreeStatusFunc(wt)
	if err != nil {
		return false, fmt.Errorf("get status: %w", err)
	}

	if status.IsClean() {
		return false, nil
	}

	// Commit.
	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "car-monitor",
			Email: "monitor@car",
			When:  time.Now(),
		},
	})
	if err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}

	// Push.
	authMethod, err := authMethodFunc(keyPath)
	if err != nil {
		return false, fmt.Errorf("load SSH key for push: %w", err)
	}

	// git.NoErrAlreadyUpToDate isn't handled specially here: it means the
	// remote already has what we're about to push, which can't happen on
	// this path — the status.IsClean() check above already returned early
	// for "nothing changed," so by the time we reach Push(), wt.Commit()
	// has just created a new commit the remote cannot already have.
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	if err := repo.PushContext(ctx, &git.PushOptions{Auth: authMethod}); err != nil {
		return false, fmt.Errorf("push: %w", err)
	}

	return true, nil
}

// authMethodFromKey creates an SSH auth method from a private key file.
func authMethodFromKey(keyPath string) (ssh.AuthMethod, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	auth, err := ssh.NewPublicKeys("git", keyData, "")
	if err != nil {
		return nil, fmt.Errorf("create public keys auth: %w", err)
	}

	return auth, nil
}
