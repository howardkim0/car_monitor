# Plan: Local Log Retention Cap (30 files)

> Companion to `DESIGN.md` §12's retention TODO. Saved per `CLAUDE.md`'s
> "Planning docs are saved to docs/".

## Goal

Cap the number of on-device reading-log files at **30, by count, not by
age**. If the phone goes two months without a drive, the 30 files kept
can all be well over 30 days old — that's fine. The rule is purely
"keep the 30 newest `readings-*.csv` files, delete the rest," never a
calendar-day cutoff. This corrects the "30 days" framing in the current
DESIGN.md §12 bullet, which should be reworded once this lands.

Out of scope: `internal/applog`'s app.log is already size-capped
(§6.2) and untouched by this change.

## Design

`readings-YYYY-MM-DD.csv` filenames sort lexicographically in
chronological order, so no date parsing is needed — just glob and sort
the filenames.

**`go/internal/storage/storage.go`**
- Add `const MaxReadingLogFiles = 30`.
- Add a standalone function (not a `FileStore` method — it needs no
  open-file state, just a directory):
  ```go
  // PruneOldReadingLogs deletes readings-*.csv files in dir beyond the
  // keep most recent (by filename, which sorts chronologically).
  // Age is irrelevant — only count. keep <= 0 is treated as 0 (delete
  // everything matched). A glob or stat failure is a real error; a
  // per-file os.Remove failure is collected and returned but does not
  // stop pruning the rest (best-effort, matches this file's existing
  // "one bad file shouldn't wedge everything else" pattern).
  func PruneOldReadingLogs(dir string, keep int) error
  ```
- Implementation sketch: `filepath.Glob(filepath.Join(dir,
  "readings-*.csv"))`, `sort.Strings`, if `len(matches) > keep` remove
  `matches[:len(matches)-keep]` (the oldest), leave the newest `keep`.

**`go/mobile/mobile.go`**
- In `NewSession`, right after `storage.OpenFileStore(readingsDir)`
  succeeds, call `storage.PruneOldReadingLogs(readingsDir,
  storage.MaxReadingLogFiles)`. Log a failure via `LogError` (mirroring
  the existing `s.store.Append` error-handling pattern just below it)
  but don't fail session creation over it — pruning is cleanup, not a
  precondition for the app to work.

## Tests

`go/internal/storage/storage_test.go` (table-driven, matching this
file's convention): create a temp dir, write N empty files named
`readings-<date>.csv` for a mix of dates (including non-contiguous /
out-of-order-creation dates, and a couple of unrelated filenames that
must NOT be touched, e.g. `readings-invalid.csv`... actually that one
*does* match the glob and *should* be prunable — use something like
`notes.txt` or `readings.csv` without a date as the non-matching
control instead), call `PruneOldReadingLogs(dir, keep)`, assert exactly
the newest `keep` filenames remain via `os.ReadDir`. Cases: fewer files
than `keep` (no-op), exactly `keep` (no-op), more than `keep`
(prunes), `keep == 0`, dates spanning far more than 30 real days apart
(proves this is a count cap, not an age cap).

`go/mobile/mobile_test.go`: extend the existing fake-`Store` test setup
to confirm `NewSession` still succeeds when `PruneOldReadingLogs`
would/does encounter an error (use a real temp dir with a
read-only-permission trap file, or just verify via the real
`OpenFileStore` path plus a pre-seeded directory with >30 files that a
subsequent `NewSession` call prunes down to 30).

## DESIGN.md update (same change)

- §6.1: add a short paragraph noting reading logs are pruned to the 30
  most recent files on every `NewSession` call, by count not age.
- §12: remove the "cap local log retention" TODO bullet (superseded by
  §6.1 now describing the real behavior).
