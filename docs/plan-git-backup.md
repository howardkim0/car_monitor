# Plan: Automatic Git Backup of Logs

> Companion to `DESIGN.md` §12's git-backup TODO. Depends on
> `docs/plan-ssh-keygen.md` being implemented first (needs a private
> key to authenticate with). Saved per `CLAUDE.md`'s "Planning docs are
> saved to docs/".

## Goal

Push on-device logs to `git@github.com:howardkim0/car_monitor_logs.git`
automatically, triggered whenever a new log file appears (day
rotation) or every hour, whichever comes first. Best-effort: no
network, no deploy key registered yet, or a push failure must never
crash the app or interrupt OBD polling — log and retry at the next
trigger.

## Why Go, not Kotlin

Per `DESIGN.md` §3's "Go owns business logic" split: deciding *when* to
sync and *what* changed is business logic, and a pure-Go git client
(`github.com/go-git/go-git/v5`) avoids pulling a JVM-based git library
(JGit) into `android/` for what both the SSH-keygen plan and this one
already keep as Go concerns. `go-git` and `golang.org/x/crypto/ssh`
(added by the SSH-keygen plan) are both confirmed available on the
module proxy — verified `go doc` succeeds for
`go-git/v5.PlainInit`/`PlainClone` and
`go-git/v5/plumbing/transport/ssh.NewPublicKeys`.

## Design

**Dependency**: add `github.com/go-git/go-git/v5` (`go get`).

**New package `go/internal/gitbackup/gitbackup.go`**
```go
const remoteURL = "git@github.com:howardkim0/car_monitor_logs.git"
const syncInterval = 1 * time.Hour

// Syncer decides when to back up and performs the git operations.
// Not a Session method — like internal/applog's logger, it needs to
// persist state (last-synced filename, last-synced time) across
// Bluetooth reconnects, which recreate obd2.Session but not this.
type Syncer struct {
    repoDir    string // local clone, e.g. storageDir/backup-repo
    keyPath    string // from sshkey.PrivateKeyPath
    lastFile   string
    lastSynced time.Time
    clock      func() time.Time // injected, defaults to time.Now
    git        gitClient        // injected, defaults to the real go-git-backed implementation
}

func NewSyncer(repoDir, keyPath string) *Syncer

// SyncIfNeeded checks whether a new reading-log file has appeared
// (readingsDir's current-day filename differs from the last sync) or
// syncInterval has elapsed since the last successful sync, and if so,
// copies readings-*.csv + app.log into the local clone and pushes.
// A no-op, returning nil, if neither condition holds. All git/network
// failures are returned as errors for the caller to log — never panics,
// never retried inline (the caller's own periodic trigger is the retry
// mechanism).
func (s *Syncer) SyncIfNeeded(readingsDir, appLogPath string) error
```

**Testability seam** (this is the part worth getting right up front,
since `githooks/pre-commit` enforces 100% coverage on `go/` and this
package touches real git/network operations):
- `gitClient` is a small interface (`clone|open`, `addAndCommit`,
  `push`) that the real implementation wraps `go-git` calls behind.
- Unit tests for `SyncIfNeeded`'s *trigger logic* (new-file-detected /
  hourly-elapsed / neither) inject a fake `gitClient` and a fake
  `clock`, so that logic hits 100% coverage without any real git I/O.
- Unit tests for the *real* `gitClient` implementation (clone, commit,
  push) use `go-git`'s own `PlainInit(path, isBare: true)` to create a
  throwaway local bare repo as the "remote" in a temp dir, and point
  `CloneOptions.URL` at its local path instead of the real SSH remote.
  This exercises the actual clone/commit/push code paths for real,
  without needing network or a real SSH key — the same trick this repo
  already uses elsewhere for "test the real logic, not a mock of it"
  (e.g. `internal/obd2`'s tests feed real ELM327-formatted byte
  strings rather than mocking the parser). SSH-specific auth wiring
  (`ssh.NewPublicKeys`) is the one piece that's closer to
  framework-integration than unit-testable — keep it in a thin,
  clearly-isolated function so it's obviously not where the interesting
  logic lives, similar to how `internal/obd2`'s Bluetooth I/O boundary
  is Kotlin's job, not something `go/` unit tests reach past.

**`go/mobile/mobile.go`** (or `go/mobile/gitbackup.go`)
```go
// SyncLogsIfNeeded backs up logs to car_monitor_logs.git if a new log
// file has appeared or an hour has passed since the last sync. Safe to
// call frequently (e.g. every few minutes) — it's a no-op otherwise.
// storageDir is the app's private storage root, same as NewSession.
func SyncLogsIfNeeded(storageDir string) error
```
- Lazily creates and caches a package-level `*gitbackup.Syncer`
  (mirrors `applog.go`'s package-level logger pattern — this needs to
  persist across Bluetooth reconnects same as the app log does), keyed
  off `storageDir` (stable for the process lifetime, so no real cache
  invalidation logic needed). On error, `LogError(...)` and return —
  Kotlin's caller ignores the return value / just logs.

**`ObdForegroundService.kt`**
- New coroutine loop, started in `onCreate()` and cancelled in
  `onDestroy()` — **not** scoped to the Bluetooth connection lifecycle
  like `writeLoop`/`readLoop`/the anomaly-check loop, since backing up
  existing logs shouldn't require an active OBD connection:
  ```kotlin
  private fun gitBackupLoop() = scope.launch {
      while (isActive) {
          delay(GIT_BACKUP_CHECK_INTERVAL_MS)
          runCatching { Mobile.syncLogsIfNeeded(filesDir.absolutePath) }
              .onFailure { Log.w(TAG, "git backup check failed", it) }
      }
  }
  ```
- `GIT_BACKUP_CHECK_INTERVAL_MS` (e.g. 5 minutes) is just a check
  cadence — Go's `SyncIfNeeded` decides whether real work happens, so
  this can be coarse without affecting how promptly a new log gets
  backed up beyond that granularity.

**`AndroidManifest.xml`**
- Add `<uses-permission android:name="android.permission.INTERNET" />`
  — this is the app's first feature needing network access; update
  `DESIGN.md` §8's permission list in the same change.

## Tests

`go/internal/gitbackup/gitbackup_test.go`: table-driven trigger-logic
tests (fake clock + fake `gitClient`, per the seam above) covering
no-op/new-file/hourly-elapsed/both, plus a smaller set of tests against
a real local bare repo verifying a real clone → write → commit → push
round-trip lands the expected files and content, and that a second
sync with no changes doesn't create an empty commit.

`go/mobile/mobile_test.go`: `SyncLogsIfNeeded` wiring test using the
same fake-injection seam (may need a small test-only constructor or
build-tag seam in `mobile` to inject a fake `Syncer`, matching how
`newSessionWithStore` already exists purely so tests can inject a fake
`Store`).

## DESIGN.md update (same change)

- §12: remove the "automatic git backup" TODO bullet.
- §7: note that the git-backup loop is deliberately independent of the
  Foreground-Service Bluetooth-connection lifecycle (contrast with
  `writeLoop`/`readLoop`), and add one line acknowledging this is a
  `WorkManager`-shaped periodic task kept on the existing
  Foreground-Service's coroutine scope instead, for the same
  single-mechanism-not-two reason `RECEIVE_BOOT_COMPLETED` reuses the
  Service rather than introducing a second background-execution model.
- §8: add `INTERNET`.
- §9: note the new `internal/gitbackup` package.
