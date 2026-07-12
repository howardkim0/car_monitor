# Task: expand OBD2 PID coverage, switch reading log to CSV, add daily log rotation, and separate app/error logging

Repo: car_monitor (Go core in `go/`, thin Kotlin/Android shell in `android/`).
Read `DESIGN.md` in full before touching anything, and `CLAUDE.md` for repo
process rules (three-persona review, regression-test-per-bug, coverage
floor, `githooks/pre-commit` — do not bypass it). Any edit to `DESIGN.md`
needs its own explicit Architect-persona pass per CLAUDE.md, in addition to
the three-persona pass on the code.

### Current state (so you don't have to re-derive it)

- `go/internal/vehicle/vehicle.go`: `subaruForester2023` profile hardcodes
  exactly 4 PIDs — RPM (0x0C), Speed (0x0D), Coolant Temp (0x05), Throttle
  Position (0x11) — each with a `Decode func([]byte) (float64, error)`.
- `go/internal/obd2/obd2.go`: `Session.Commands()` builds one ELM327 request
  string per PID in the active profile (`buildCommands`); `Feed` parses
  response lines and calls `onReading` for anything it can match to a known
  PID + decode successfully. Unsupported/unmatched PIDs (`NO DATA`, unknown
  PID code) are silently dropped already — safe to over-request.
- `go/internal/storage/storage.go`: `FileStore` appends one JSON object per
  line (JSONL) to a single fixed path, syncing every write. No rotation, no
  daily boundary logic.
- `go/mobile/mobile.go`: `NewSession(storagePath, listener)` opens the store
  and wires `obd2.Session` → `storage.FileStore` → Kotlin callback. Note
  line 41, `_ = s.store.Append(r)`, silently swallows storage errors — worth
  fixing while touching this file, with a regression test.
- Polling cadence lives in `android/app/.../ObdForegroundService.kt`
  (`COMMAND_INTERVAL_MS = 100L` between commands, `POLL_CYCLE_MS = 250L`
  between full cycles) — with only 4 PIDs, a full cycle is ~650ms.
- Android error/debug logging today is plain `Log.e`/`Log.d` (logcat only,
  nothing persisted) in `ObdForegroundService.kt` / `StatusActivity.kt`.

### Requirements

1. **Capture every available PID**, not just the current 4. Extend
   `vehicle.Profile`/`subaruForester2023` (or the PID-decode layer more
   generally) to cover the standard SAE J1979 Mode 01 PID set, each with a
   correct decode formula and unit.
   - **Open design decision, resolve and document in `DESIGN.md` §5.2**:
     "every PID available" could mean (a) the full static SAE table,
     requested unconditionally every cycle (simplest, but with ~40+ PIDs at
     100ms/command the poll cycle balloons to multiple seconds — you may
     need to revisit `COMMAND_INTERVAL_MS`/`POLL_CYCLE_MS`), or (b) a
     supported-PID discovery step at session start (Mode 01 PIDs
     0x00/0x20/0x40/0x60/... return bitmasks of what the connected ECU
     actually supports) so only genuinely-available PIDs get requested
     going forward. Recommend (b) for a real car — it avoids wasting every
     cycle on PIDs that will only ever return `NO DATA` — but pick one,
     justify it in `DESIGN.md`, and don't leave it as an implicit choice.
   - Add unit tests for every new decode formula (table-driven, per
     `obd2_test.go`/`vehicle_test.go` convention), including malformed/
     short-data cases.

2. **Switch the reading log from JSONL to CSV.** Same 5 fields as today
   (`pid,name,value,unit,timestamp`), header row once per file. Update
   `DESIGN.md` §6 (currently says "JSON-lines") and the architecture
   diagram in §4 accordingly.

3. **Rotate the reading log to a new file per UTC calendar day**, and
   **resume appending to today's file** if the app restarts (or the
   service reconnects) later the same UTC day — don't truncate or start a
   fresh file on every session, only on a UTC day boundary. This needs to
   work both (a) at session/store open time, and (b) mid-session if a run
   spans UTC midnight — decide and document whether you handle both;
   recommend handling both, since "new file per day" otherwise silently
   fails to hold for any drive that crosses UTC midnight.
   - This day-based rotation is specifically to make future time-based
     analysis of the reading data easier (e.g. "give me Tuesday's drive").
     It does **not** apply to the app/error log — see requirement 5.

4. **Timestamps inside the reading log are UTC, and the filename is dated
   in UTC too** (e.g. `readings-2026-07-12.csv`). Do **not** include a
   local timezone reference in the filename — UTC everywhere, both in the
   file contents and the rotation boundary, keeps this unambiguous. Pick a
   concrete, exact filename format and note it in `DESIGN.md`.

5. **Store app logs (errors/debug) separately from the reading data log,
   as a single file bounded by a size cap** (e.g. 10MB, make it a named
   constant) rather than day-based rotation — app logs don't need the
   time-based-analysis boundary the reading log does (see requirement 3),
   they just need to not grow unbounded. Decide and document the rollover
   behavior once the cap is hit (e.g. rename the current file aside and
   start a new one, keeping one prior file, vs. simple truncate-and-restart
   — a single kept prior file is usually enough to debug "what just
   happened" without unbounded growth). Decide where this lives
   architecturally: given `DESIGN.md`'s "Go owns all business logic,
   Kotlin stays dumb," a new small logging surface in Go (e.g.
   `internal/applog`, or extending `internal/storage`) that both Go's own
   error paths (e.g. the swallowed `store.Append` error above, decode
   failures) and Kotlin (via a new `mobile` export) can write to is more
   consistent with the existing split than ad hoc file-writing on the
   Kotlin side. Plain text or JSONL is fine for this — it's heterogeneous,
   unstructured data, unlike the tabular reading log, so don't force it
   into CSV.

### Process / acceptance checklist

- Follow the existing `go/` conventions: table-driven tests, one test file
  per source file, no bypassing `githooks/pre-commit` (`gofmt`, `go vet`,
  `go test ./...` with coverage floor, `go build ./...`).
- Any bug you catch while doing this (including the swallowed error at
  `mobile.go:41` if you touch it) needs a regression test that fails
  before the fix, per `CLAUDE.md`.
- Run the three-persona review (Architect / Senior engineer / UX designer)
  as actual separate passes before committing — this change touches
  extensibility boundaries (`vehicle.Profile`), file format, and
  user-visible log layout, so it's not a trivial diff.
- If `mobile`'s exported surface changes (new methods for app logging,
  changed constructor signature, etc.), note in your summary that
  `gomobile bind` + `gradlew assembleDebug` need a manual rebuild per
  `DESIGN.md` §11 — this isn't part of the pre-commit hook.
- Update `DESIGN.md` §4 (data flow / architecture diagram), §6 (storage),
  and §5.2 (vehicle PIDs) to match whatever you actually build — don't let
  the doc and code drift, per `CLAUDE.md`'s primary instruction.
