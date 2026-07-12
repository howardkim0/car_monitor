# Instructions for Claude Code in this repo

## Read the design doc first, every time

Before making any change in this repo, read `DESIGN.md` in full. It defines
the architecture (Go core in `go/`, thin Kotlin/Android shell in `android/`),
the extensibility points for Bluetooth devices (`internal/device`) and
vehicle profiles (`internal/vehicle`), and the reasoning behind them. If a
change needs to diverge from what's there, update `DESIGN.md` in the same
change rather than letting the doc and the code drift apart.

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
`go test ./...`, and `go build ./...` for the `go/` module on every commit.
Don't bypass it with `--no-verify` — fix the failure instead. Any new
package under `go/` needs unit tests before it's committed, matching the
existing packages' style (table-driven tests, one test file per source
file).

Full Android/APK compilation (`gomobile bind` + `gradlew assembleDebug`)
requires the Android SDK/NDK from `scripts/setup_ubuntu.sh` and is
deliberately not part of the pre-commit hook (too slow, and the toolchain
isn't guaranteed to be present on every machine). Run it manually per
`DESIGN.md` section 11 whenever a change touches the Android shell or the
`mobile` package's exported surface.
