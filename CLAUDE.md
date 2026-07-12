# Instructions for Claude Code in this repo

## Read the design doc first, every time

Before making any change in this repo, read `DESIGN.md` in full. It defines
the architecture (Go core in `go/`, thin Kotlin/Android shell in `android/`),
the extensibility points for Bluetooth devices (`internal/device`) and
vehicle profiles (`internal/vehicle`), and the reasoning behind them. If a
change needs to diverge from what's there, update `DESIGN.md` in the same
change rather than letting the doc and the code drift apart.

## DESIGN.md changes need an Architect pass first

Any edit to `DESIGN.md` — however small — gets an explicit
Architect-persona review (the same persona from the three-persona review
below) before it's committed, even if the rest of the change is trivial
wording. Run it as an actual separate pass, not a mental check while
writing the diff: does the change fit the existing `go/`/`android/` split
and the extensibility boundaries in §5, does it contradict something
elsewhere in the doc, does it accurately describe what the code actually
does. `DESIGN.md` drifting from the code is exactly what "read the design
doc first, every time" above is trying to prevent — an unreviewed doc
change is how that drift starts.

## Three-persona review before every commit

No code change gets committed on the basis of a single pass. Review it from
these three perspectives, in order, before committing — don't collapse them
into one glance at the diff. For anything beyond a trivial change, run each
perspective as an actual separate pass (e.g. the `code-review` skill, or a
dedicated review subagent), not three bullet points ticked off in your head.

1. **Architect** — Does this fit `DESIGN.md`'s architecture? Does it respect
   the device/vehicle extensibility boundaries (new behavior goes through
   `device.Profile` / `vehicle.Profile`, never a hardcoded MAC or PID
   scattered into calling code)? Does it live in the right package
   (`internal/obd2`, `internal/device`, `internal/vehicle`,
   `internal/storage`, `mobile`) per the doc's data flow?
2. **Senior engineer** — Is the logic correct (decode formulas, ELM327
   framing, error handling, concurrency)? Are edge cases covered by tests
   (partial reads, unknown PIDs, malformed lines, closed stores)? Is it
   idiomatic Go, free of dead code and unneeded abstraction?
3. **UX designer** — For anything user-visible (notification text, status
   screen, error states): would a car owner, not an engineer, understand
   what the app is doing and why?

## Every caught bug gets a regression test

Whenever a bug is caught and fixed — whether it surfaced during the
three-persona review above, a pre-commit/CI failure, or a human noticing
something wrong and flagging it — add a unit test that reproduces it (i.e.
would have failed before the fix), not just the fix itself. This applies
no matter who or what caught it; "a human found it" or "the fix is
obviously correct" are not exemptions. For `go/`, follow the existing
table-driven, one-file-per-source-file convention. The test is what keeps
the bug fixed after the next refactor, not the fix alone.

## Tests and build are enforced, not optional

`githooks/pre-commit` (wired up via `git config core.hooksPath githooks`,
which `scripts/setup_ubuntu.sh` does automatically) runs `gofmt`, `go vet`,
`go test ./...` (with the coverage gate below), and `go build ./...` for
the `go/` module on every commit. Don't bypass it with `--no-verify` — fix
the failure instead. Any new package under `go/` needs unit tests before
it's committed, matching the existing packages' style (table-driven
tests, one test file per source file).

## Coverage is enforced, not just measured

`githooks/pre-commit` runs `go test -coverprofile=... ./...` and fails the
commit if total statement coverage drops below `MIN_COVERAGE` (100%, set
at the top of the script) — `go/` is small, pure business logic with no
framework I/O to mock, so 100% is a real, meaningful bar, not a number
inflated with contrived tests. A `.github/workflows/coverage.yml` re-runs
the same check on push/PR as a backstop (a bypassed local hook, or a
fresh clone without hooks configured) and emails on any regression. If a
change legitimately needs the floor lowered, change `MIN_COVERAGE` in
`githooks/pre-commit` in the same commit and say why, rather than working
around a failing gate.

The 100% gate is `go/`-only. `android/` has its own test setup now (JUnit +
Robolectric + MockK under `android/app/src/test`, run via
`./gradlew testDebugUnitTest`; Kover reports coverage via
`./gradlew koverHtmlReportDebug`) — see DESIGN.md section 13 for why it's
deliberately not held to the same 100% number: closing five small gaps in
pure Go functions and exhaustively simulating every Bluetooth/Service
framework interaction through Robolectric/MockK are not comparable-sized
tasks. `android/` tests target real regressions (bugs actually found), not
a percentage.

Full Android/APK compilation (`gomobile bind` + `gradlew assembleDebug`)
requires the Android SDK/NDK from `scripts/setup_ubuntu.sh` and is
deliberately not part of the pre-commit hook (too slow, and the toolchain
isn't guaranteed to be present on every machine). Run it manually per
`DESIGN.md` section 11 whenever a change touches the Android shell or the
`mobile` package's exported surface.
