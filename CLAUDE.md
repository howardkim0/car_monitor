# Instructions for Claude Code in this repo

## Read DESIGN.md first, every time

Before making any change, read `DESIGN.md` in full — it defines the
architecture (Go core in `go/`, thin Kotlin shell in `android/`) and the
`internal/device`/`internal/vehicle` extensibility points. Update it in
the same change if code diverges from what it says.

## DESIGN.md is timeless

It describes the app as it exists now, not a changelog of any session,
PR, or round of edits — avoid phrasing like "this session" or "just
added." Use present tense for durable facts (e.g. "a prune pass caps
reading logs at 30 files"). History worth keeping goes in commit
messages or `docs/plan-*.md`, not DESIGN.md. Fix drifted language
wherever you touch a section, even if that wasn't the point of the change.

## Planning docs go in docs/

After plan mode approves a non-trivial task, save the plan to
`docs/plan-<topic>.md` — include the reasoning (research, alternatives
considered), not just the task breakdown, since that's what's hardest to
reconstruct later. If it originated from `docs/prompt-<topic>.md`, the
plan is a companion, not a replacement.

## Delegate well-specified work to Haiku

If the file(s), diff, and intent are already clear — from a plan, spec,
or explicit instruction — delegate implementation to a Haiku subagent
instead of writing it inline. Reserve Sonnet/Opus for planning,
architecture decisions, review, and judgment calls. Batch independent
Haiku tasks in parallel.

## DESIGN.md edits need an Architect pass first

Any DESIGN.md edit, however small, gets an explicit Architect-persona
review (a separate pass or subagent, not a mental check) before
committing: does it fit the `go/`/`android/` split and §5's
extensibility boundaries, and does the doc match the code?

## Two-persona review before every commit

Review non-trivial code changes from two perspectives, as separate
passes:

1. **Architect** — fits `DESIGN.md`'s boundaries? New device/vehicle
   behavior goes through `device.Profile`/`vehicle.Profile` and lives in
   the right package (`internal/obd2`, `internal/device`,
   `internal/vehicle`, `internal/storage`, `mobile`)?
2. **Senior Engineer** — correct logic, concurrency, error handling?
   Edge cases tested? Idiomatic, no dead code?

## Every caught bug gets a regression test

Whenever a bug is found and fixed — by the review above, CI, or a
human — add a test that would have failed before the fix, not just the
fix. No exceptions for "a human found it" or "the fix is obviously
correct." Go tests: table-driven, one file per source file.

## Tests and build are enforced

`githooks/pre-commit` runs `gofmt`, `go vet`, `go test ./...` (100%
coverage gate), and `go build ./...` for `go/` on every commit. Don't
bypass with `--no-verify` — fix the failure. New `go/` packages need
tests in the same style before committing.

## Coverage: 100% for go/, not android/

The pre-commit hook fails below `MIN_COVERAGE` (100%, top of the
script) — `go/` is pure logic with no framework I/O to mock, so this is
a real bar. `.github/workflows/coverage.yml` backstops it on push/PR.
Lowering the floor requires changing `MIN_COVERAGE` in the same commit
and saying why.

`android/` (JUnit + Robolectric + MockK, `./gradlew testDebugUnitTest`,
Kover for coverage) isn't held to 100% — see DESIGN.md §13 — and targets
real regressions, not a percentage. Full Android/APK compilation is
deliberately not part of the pre-commit hook; run it manually per
DESIGN.md §11 when touching `android/` or `mobile`'s exported surface.
