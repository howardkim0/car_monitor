# Instructions for Claude Code in this repo

## Read the design doc first, every time

Before making any change in this repo, read `DESIGN.md` in full. It defines
the architecture (Go core in `go/`, thin Kotlin/Android shell in `android/`),
the extensibility points for Bluetooth devices (`internal/device`) and
vehicle profiles (`internal/vehicle`), and the reasoning behind them. If a
change needs to diverge from what's there, update `DESIGN.md` in the same
change rather than letting the doc and the code drift apart.

## Planning docs are saved to docs/

When a non-trivial task goes through plan mode, save the resulting plan
into `docs/` (named `plan-<topic>.md`) once it's approved — not just left
in the ephemeral plan-mode location. Include research gathered while
planning (web lookups, spec cross-references, decisions made and why),
not only the final task breakdown — that reasoning is what's actually
hard to reconstruct later, and code/DESIGN.md diffs alone don't carry it.
If the task originated from a spec doc already in `docs/` (e.g.
`docs/prompt-<topic>.md`), the plan is its companion, not a replacement —
both stay.

## Delegate implementation to Haiku agents for token efficiency

Where a change is well-specified (the file(s), the exact diff, and the
intent are already clear — e.g. from a plan, a reviewed spec, or explicit
instructions), delegate the actual implementation to a Haiku-model
subagent rather than writing it inline. Reserve Sonnet/Opus-level
reasoning for planning, architecture decisions, two-persona review, and
ambiguous judgment calls; hand mechanical implementation to Haiku once
the "what" and "how" are already decided. Batch independent Haiku tasks
in parallel rather than running them serially. This keeps token spend
proportional to the actual difficulty of the decision being made, not the
size of the diff.

## DESIGN.md changes need an Architect pass first

Any edit to `DESIGN.md` — however small — gets an explicit Architect-persona review before it's committed. Run it as an actual separate pass (or a dedicated quick subagent) rather than a mental check while writing the diff. Check if the change fits the `go/`/`android/` split and the extensibility boundaries in §5, and ensure the doc matches the code. Leverage `agy` commands (e.g. `agy grep`, `agy find`) or Antigravity's codebase tools for rapid, token-efficient context discovery.

## Two-persona review before every commit

To minimize token usage and keep context size optimal, no code change is committed on a single pass. Perform a review from these two perspectives. For non-trivial changes, run these as separate passes or lightweight subagents (e.g. `self` or `research` subagents). Use `agy` codebase research tools (like `grep_search` or `view_file` with precise line ranges) inside these persona passes to quickly pull needed context without reading whole files:

1. **Architect** — Does this fit `DESIGN.md`'s architecture and boundaries? Check that new device/vehicle behavior goes through `device.Profile`/`vehicle.Profile` and lives in the correct package (`internal/obd2`, `internal/device`, `internal/vehicle`, `internal/storage`, `mobile`).
2. **Senior Engineer** — Is the logic correct (formulas, framing, concurrency, error handling)? Are edge cases covered by unit tests? Is it idiomatic Go, free of dead code?

## Token Efficiency Guidelines

- **Targeted File Reading**: Never view entire files if you only need a specific section. Use `view_file` with `StartLine` and `EndLine`.
- **Precise Search**: Prefer `grep_search` (with file globs) or `agy` CLI search commands to locate symbols/patterns instead of listing directories recursively or scanning files line by line.
- **Short Command Output**: Run terminal commands with output limits (e.g. `git log -n 5`, `go test -v ./... -run TestSpecific`) to prevent flooding the context.

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
