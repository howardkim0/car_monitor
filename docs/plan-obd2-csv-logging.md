# Expand PID coverage, CSV logging with day rotation, separate app log

> Resolved implementation plan for `docs/prompt-obd2-csv-logging.md`,
> approved 2026-07-12. Saved here per the standing convention in
> CLAUDE.md ("Planning docs are saved to docs/") — this is the record of
> *why* the implementation looks the way it does, including the PID-scope
> research, not just the final code/DESIGN.md diffs.

## Context

`docs/prompt-obd2-csv-logging.md` specs four related changes: go from 4
hardcoded PIDs to broad coverage, switch the reading log from JSONL to
CSV, rotate it daily (UTC) with resume-same-day semantics, and add a
separate size-capped app/error log. The doc resolves most open design
questions itself (PID discovery over static over-requesting; handle both
open-time and mid-session UTC-midnight rotation; app log lives in Go, one
kept prior file on rollover) — it only left the exact PID list's breadth
open.

I researched that: the 2023 Forester is a conventional CAN-bus gas
vehicle (FB25 N/A or FA24 turbo, both boxer engines with genuine Bank
1/Bank 2 O2 sensor pairs, no hybrid/diesel specifics apply), and pulled
the authoritative Mode 01 PID/formula table (Wikipedia's OBD-II PIDs
page, cross-checked) covering 0x00-0x60. Since the discovery mechanism
means a PID present in our static table but unsupported by the ECU is
simply never requested, over-including in the *table* costs nothing at
runtime — the only cost is implementation/test effort, which is exactly
what the "practical subset" choice is trading off. Landed on 28 PIDs
(4 existing + 24 new); full list below. User confirmed: use the practical
subset given this research holds up.

## 0. This plan is saved to `docs/` (new standing convention)

Per user request: this plan is saved as `docs/plan-obd2-csv-logging.md`
(a persistent, versioned companion to `docs/prompt-obd2-csv-logging.md`,
the original spec) rather than staying only in the ephemeral plan-mode
location. **New CLAUDE.md rule**: whenever a non-trivial planning-mode
plan is produced for this repo, save it into `docs/` (named
`plan-<topic>.md`) alongside any research notes gathered while planning,
so the reasoning behind decisions survives past the session that made
them — not just the final code/doc diffs.

**NA vs turbo, resolved**: targeting the NA FB25 2.5L variant specifically
(confirmed by user) doesn't change the PID list. SAE J1979 has no
distinct "boost pressure" PID — boost is inferred from Intake Manifold
Absolute Pressure (0x0B) exceeding Barometric Pressure (0x33), both
already included and applicable to both NA and turbo engines identically
(on NA, MAP simply never exceeds ambient). Fuel Rail Pressure (0x59) is
still relevant — the FB25 NA engine also uses direct injection in the
2023 model year, not only the turbo FA24. DESIGN.md §5.2 will note the
profile targets the NA variant specifically, since MAP's *behavior*
(though not the PID or formula) differs from a turbo engine.

## 1. PID discovery + expanded table (`go/internal/vehicle`, `go/internal/obd2`)

**New PID list** (existing 4 unchanged; Name/Unit/formula for the rest,
all sourced from the fetched SAE J1979 table):

| Code | Name | Formula | Unit |
|---|---|---|---|
| 0x04 | Calculated Engine Load | 100/255×A | % |
| 0x06/0x08 | Short Term Fuel Trim Bank 1/2 | 100/128×A−100 | % |
| 0x07/0x09 | Long Term Fuel Trim Bank 1/2 | 100/128×A−100 | % |
| 0x0A | Fuel Pressure | 3×A | kPa |
| 0x0B | Intake Manifold Pressure | A | kPa |
| 0x0E | Timing Advance | A/2−64 | ° before TDC |
| 0x0F | Intake Air Temperature | A−40 | °C |
| 0x10 | Mass Air Flow Rate | (256A+B)/100 | g/s |
| 0x14/0x15/0x16/0x17 | O2 Sensor B1S1/B1S2/B2S1/B2S2 | A/200 (voltage only — see note) | V |
| 0x1F | Run Time Since Start | 256A+B | s |
| 0x21 | Distance With MIL On | 256A+B | km |
| 0x2C | Commanded EGR | 100/255×A | % |
| 0x2F | Fuel Tank Level | 100/255×A | % |
| 0x30 | Warm-ups Since Codes Cleared | A | count |
| 0x31 | Distance Since Codes Cleared | 256A+B | km |
| 0x33 | Barometric Pressure | A | kPa |
| 0x42 | Control Module Voltage | (256A+B)/1000 | V |
| 0x43 | Absolute Load Value | 100/255×(256A+B) | % |
| 0x45 | Relative Throttle Position | 100/255×A | % |
| 0x46 | Ambient Air Temperature | A−40 | °C |
| 0x59 | Fuel Rail Pressure | 10×(256A+B) | kPa |
| 0x5C | Engine Oil Temperature | A−40 | °C |
| 0x5E | Engine Fuel Rate | (256A+B)/20 | L/h |

Excluded and why: bit-encoded/enum PIDs (0x01, 0x03, 0x1C, 0x51) don't
fit the single-`float64` `Decode` model and aren't "data" in the
temperature/pressure/speed sense this app logs; wideband/lambda O2
variants (0x24-0x2B, 0x34-0x3B) are redundant with the simpler 0x14-0x17
for this purpose; ethanol % (0x52) is irrelevant to a non-flex-fuel
Forester. **Note on 0x14-0x17**: these PIDs return *two* values (sensor
voltage + short-term trim) but `PID.Decode` only supports one
`float64` — decoding only the voltage, since the trim sub-field is
redundant with the bank-level trim already captured via 0x06-0x09.
Document this explicitly in a code comment and in DESIGN.md §5.2, not
silently.

**Discovery mechanism** (`go/internal/obd2/obd2.go`): `Session` gains a
two-phase command lifecycle instead of `Commands()` always returning a
static list built once at `NewSession()`:
- Compute the minimal discovery-query set from the profile's actual max
  PID code (`0x00, 0x20, 0x40` for our 28-PID table, whose max is 0x5E) —
  self-adjusting if the table ever changes, not a hardcoded list.
- `Commands()` returns pending discovery queries while any range is
  unresolved; once all ranges are resolved (or timed out), returns the
  real filtered command list, computed once and cached.
- `Feed()` intercepts response lines whose PID matches a pending
  discovery range *before* the normal `parseLine`/`onReading` path (a
  bitmask isn't a `Reading`), decodes the 32-bit big-endian bitmask
  (byte A bit 7 = PID `base+1` ... byte D bit 0 = PID `base+32`, per the
  SAE convention), and marks matching profile PIDs supported.
- **Timeout fallback**: track `time.Now()` at session start; after 5s,
  any still-unresolved range falls back to *optimistic* (treat all its
  profile PIDs as supported) rather than pessimistic — matches the old
  always-request-everything behavior as a safe default if discovery
  itself fails for some reason, rather than silently going dark.
- Refactor `parseLine`'s shared "split into hex bytes + mode-range check"
  logic into a small helper both the discovery-response path and the
  normal decode path use, avoiding duplicating that logic.
- Zero changes needed in Kotlin's `writeLoop()` — it already polls
  `Commands()` blindly every cycle; Go owns the phase transition
  entirely, consistent with "Kotlin stays dumb."

**Polling cadence**: with up to 28 commands/cycle instead of 4,
`COMMAND_INTERVAL_MS` (100ms today) would make a full cycle ~2.8s+.
Reducing to 50ms (still conservative headroom for real ELM327 response
times) roughly halves that. Flagged as unverified against real hardware
in the same way as all Bluetooth-dependent behavior this session — no
device access in this sandbox.

**Tests**: `vehicle_test.go` — one table-driven test covering all 28
PIDs' nominal decode, plus a loop asserting every PID's `Decode` errors
on too-short data. `obd2_test.go` — bitmask parsing (each byte position),
phase transition from discovery to real commands, the timeout-fallback
path, and that a discovery response never produces a `Reading` via
`onReading`.

## 2. CSV + day rotation (`go/internal/storage/storage.go`)

`OpenFileStore` changes from `(path string)` (one fixed JSONL file) to
`(dir string)`: creates `dir` (`os.MkdirAll`), computes today's UTC date,
opens/creates `readings-YYYY-MM-DD.csv` inside it. Writes the CSV header
(`pid,name,value,unit,timestamp` via `encoding/csv`, not hand-rolled
escaping) only when the file is new (size 0 after open — no separate
`os.Stat` race), not when resuming an existing same-day file.

`Append(r)`: before writing, compares `r.Timestamp`'s UTC date (not
wall-clock-at-write-time — the reading's own timestamp is what should
determine which day's file it belongs to) against the currently-open
file's date; if different, closes the current file and opens/creates the
new day's file (header-row logic as above) — this is the same code path
whether triggered at session-open or mid-session at a UTC-midnight
crossing, satisfying "handle both" without special-casing either. Keeps
the existing `file.Sync()`-after-every-write durability guarantee via
`csv.Writer.Flush()` + `Sync()`.

**UTC fix at the source**: `obd2.go`'s `parseLine` currently does
`Timestamp: time.Now()` (local zone). Change to `time.Now().UTC()` so
`Reading.Timestamp` is UTC everywhere it's used, including this rotation
decision — one fix instead of re-converting in multiple places.

**Tests**: no time-mocking needed — since rotation is keyed off
`r.Timestamp`, a test can `Append()` two readings dated on either side of
a UTC-midnight boundary directly and assert two separate correctly-named
files exist, the earlier one has exactly one header line, and reopening
mid-day resumes without a duplicate header.

## 3. App/error log (`go/internal/applog`, new package)

New package, plain text (not CSV — heterogeneous/unstructured data per
the doc): `Open(dir) (*Logger, error)`, `Errorf`/`Debugf(format, args...)`
writing one `RFC3339Nano-UTC LEVEL message` line per call, `Close()`.
Size cap as a named constant (`MaxSizeBytes = 10 * 1024 * 1024`, per
requirement 5); on exceeding it, renames the current file to `app.log.1`
(removing any prior `.1` first — one kept prior file, not unbounded) and
starts a fresh `app.log`. An unexported `openWithMaxSize(dir, maxSize)`
constructor (with `Open` calling it with the real constant) makes the
rotation boundary testable without writing 10MB in a test.

## 4. Wiring into `mobile` (new `go/mobile/applog.go` + changes to `mobile.go`)

- `NewSession(storageDir string, listener ReadingListener) (*Session, error)`
  — signature changes from a single JSONL file path to a directory root;
  internally opens the reading store at `storageDir/readings/` (day-rotated
  CSVs) via the new `storage.OpenFileStore(dir)`.
- New package-level, app-lifetime (not per-Session/per-connection) app
  log: `InitAppLog(dir string) error`, `LogError(message string)`,
  `LogDebug(message string)`, `CloseAppLog() error`. Package-level and
  independent of `Session` deliberately — `Session` is recreated on every
  Bluetooth reconnect (see `ObdForegroundService.openConnection()`), but
  the app log must stay open across reconnect churn, only capped by size.
- **Fixes the swallowed error** (`mobile.go:41`, `_ = s.store.Append(r)`):
  routes the error through `LogError` instead of discarding it. Change
  `Session.store`'s field type from the concrete `*storage.FileStore` to
  the existing `storage.Store` interface — enables injecting a
  fake-failing store in the regression test without filesystem tricks.
- **Regression test** (per CLAUDE.md, this is a bug being fixed while
  touching the file): a fake `Store` whose `Append` always errors,
  wired through `NewSession`-equivalent test setup, asserting the app
  log file actually contains the error line — proves the fix, not just
  that it compiles.

## 5. Kotlin wiring (`ObdForegroundService.kt`)

- `openConnection()`: `Mobile.newSession(storagePath, ...)` →
  `Mobile.newSession(filesDir.absolutePath, ...)` (no more manual
  `File(filesDir, "readings.jsonl")` join — Go organizes subpaths now).
- `onCreate()`: `Mobile.initAppLog(filesDir.absolutePath)`.
- `onDestroy()`: `Mobile.closeAppLog()`.
- All three existing `Log.w(TAG, ..., e)` call sites also call
  `Mobile.logError(...)` — keeps logcat for live dev visibility *and*
  persists to the new app log; doesn't remove the existing logging.

**`mobile`'s exported surface changes** (`NewSession`'s parameter
semantics, three new package-level functions) — note in the final
summary that `gomobile bind` + `gradlew assembleDebug` need a manual
rebuild per DESIGN.md §11, and actually do that rebuild before
committing (same as every prior Android-adjacent change this session),
refreshing `android/car-monitor-debug.apk`.

## 6. DESIGN.md updates (Architect-reviewed before commit, per CLAUDE.md)

- §4 (architecture/data flow): CSV format, day rotation, the discovery
  step, the app log package, in the diagram and the numbered data-flow
  list.
- §5.2 (vehicle PIDs): the practical-subset decision and why (with the
  research grounding — boxer 2-bank O2 layout, modern CAN-bus, no
  hybrid/diesel), the discovery mechanism, the O2-sensor
  voltage-only-not-trim simplification, the full PID table.
- §6 (storage): rewrite "JSON-lines" → CSV with day rotation (exact
  filename format `readings-YYYY-MM-DD.csv`, UTC), plus a new subsection
  for the app log (path, size cap, rollover behavior).
- Note the `COMMAND_INTERVAL_MS` change and that it's unverified against
  real hardware, same caveat pattern as existing Bluetooth-dependent
  notes in the doc.

## Process (per CLAUDE.md, unchanged from established session practice)

Three-persona review (Architect / Senior engineer / UX designer — this
touches extensibility boundaries, file format, and log layout, not a
trivial diff) as actual separate passes before committing, on top of the
dedicated Architect pass for the DESIGN.md diff itself. Regression tests
for the swallowed-error fix. Don't bypass `githooks/pre-commit` — new
code needs to actually reach the existing 100% coverage floor, not just
compile. Rebuild `mobile.aar` + the debug APK. Commits split by concern
similar to this session's established pattern (PID/discovery, CSV+
rotation, app log package + mobile wiring + Kotlin wiring together since
they're one coherent cross-language change, DESIGN.md).

## Verification

- `bash githooks/pre-commit` passes at 100% coverage with the expanded
  test suite.
- Manually trace the discovery→steady-state transition and the
  rotation-on-append logic against the final code, same rigor as every
  other Go-side change verified this session (these ARE runnable/testable
  locally, unlike the Bluetooth/Android runtime behavior).
- `./gradlew assembleDebug` still builds; rebuild and commit
  `car-monitor-debug.apk`.
- Update the published UX mockup artifact if the status screen's visible
  behavior changes (it shouldn't, materially — this is a backend/logging
  change, not a UI change) — confirm during implementation whether
  anything user-visible actually shifted before deciding to republish.
