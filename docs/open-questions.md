# Open questions / future work

Known gaps, accepted tradeoffs, and unfinished extensibility points —
split out of `DESIGN.md` since none of this describes current behavior,
only what might change. Each item below is also tracked as a GitHub
issue (linked) so it doesn't only live in a doc nobody re-reads; update
both together per `CLAUDE.md`.

- **DTC (fault code) reading/clearing is out of scope for v1** but fits
  the same PID-request pattern in `internal/obd2`. — [#3](../../../issues/3)
- **`obd2.InitCommands()` (`DESIGN.md` section 4 step 5) is unverified
  against real/clone hardware.** It sends five explicit `AT` commands
  instead of a full `ATZ` reset, on standard ELM327 semantics, but this
  dev environment has no Bluetooth device access. Some cheap clones are
  known to need a reset for `ATSP0` to fully re-trigger protocol
  auto-search; `ATZ` is the fallback if the zero-readings symptom
  recurs on real hardware. — [#4](../../../issues/4)
- **Long-term storage growth.** Day-rotated CSV (`DESIGN.md` section
  6.1) is fine for now; revisit with a pure-Go SQLite driver (e.g.
  `ncruces/go-sqlite3`, avoiding cgo/NDK complexity) if per-file size or
  cross-day queries grow. — [#5](../../../issues/5)
- **Polling cadence lives in Kotlin, not Go.** `COMMAND_INTERVAL_MS`/
  `POLL_CYCLE_MS` are constants in `ObdForegroundService` — *which*
  PIDs to request is decided in Go (`DESIGN.md` section 5.2's
  discovery), but *how often* isn't yet, since `Session` exposes no
  timing info. — [#6](../../../issues/6)
- **`COMMAND_INTERVAL_MS` (200ms) needs tuning against real hardware.**
  Raised from 50ms to be gentler on the adapter — at 200ms/command × 32
  PIDs plus 250ms `POLL_CYCLE_MS`, one full poll cycle is ~6.65s.
  Diagnostic logs (`writeLoop` cycle timing every ~9 cycles, `readLoop`
  bytes every 100 reads, Go's discovery-range resolution) land in
  `app.log` to verify this and tune if needed. These cover *timing*,
  not *content* — separately, `obd2.Session.Feed` also logs the raw
  content of each session's first 20 lines (quoted, so whitespace/header
  differences are visible) and a running received/decoded count every
  100 lines, plus each `InitCommands()` step as it's sent, so a future
  zero-readings session shows *what the adapter actually said* rather
  than just that nothing decoded. — [#7](../../../issues/7)
- **`mobile.Session.CommandCount()`/`CommandAt(i)` aren't one atomic
  snapshot.** Two separate JNI calls — `Commands()` changing mid-flight
  (discovery resolving) could in principle produce a stale index
  between them. Accepted as low-severity: self-heals on the next poll
  cycle, not worth the "return the whole list" JNI redesign it'd take
  to fix. — [#8](../../../issues/8)
- **`Session.CheckAnomalies`'s dedup state doesn't survive
  reconnects.** `lastLevel` is scoped to the `Session`, not persisted
  across Bluetooth reconnects, so an occasional duplicate notification
  around a reconnect is possible. Accepted — not worth the added
  package-level shared-state complexity for a cosmetic edge case.
  — [#9](../../../issues/9)
- **No "recovered to Normal" anomaly notification.** Only a metric
  moving to Warning/Critical notifies today. Deliberately out of scope
  for v1, worth revisiting. — [#10](../../../issues/10)
- **`internal/monitor`'s metric-name constants aren't
  compiler-linked to `vehicle.go`'s `PID.Name` fields** — matched by
  exact string equality instead. `TestMetricNamesMatchVehicleProfile`
  guards against silent drift, but a shared source of truth would
  remove the possibility structurally. — [#11](../../../issues/11)
- **Trend/anomaly detection re-reads the entire day's CSV on every
  check.** Each `internal/trend` check only looks at the last
  30s-5min of it. Acceptable for now (parsing is fast in absolute
  terms, and the check interval is deliberately coarse), but a
  full-day drive means paying that cost against a steadily growing
  file. If this becomes measurably expensive, the fix is incremental —
  track a byte offset and keep a small in-memory sliding window per
  metric, not re-architecting the check functions. — [#12](../../../issues/12)
- **`CarMonitorCarAppService`'s release `HostValidator` allowlist
  (`DESIGN.md` section 11) hasn't been checked against a real signed
  release build.** Verified only that `hosts_allowlist_sample` exists
  as a resource in the pinned `androidx.car.app:app:1.7.0` — re-verify
  this whenever that dependency version bumps, and confirm on a real
  signed build (not just the always-permissive debug path) before any
  release. — [#13](../../../issues/13)
- **`DeviceScanActivity` doesn't use the shared `ObdDeviceScanner`
  engine.** `PairScannerScreen` (`DESIGN.md` section 11) uses it for
  car-screen discovery/pairing, but `DeviceScanActivity` keeps its own
  original inline implementation — deliberate, to avoid regression risk
  to working, tested code when adding the car-screen feature. Unifying
  the two is a reasonable cleanup (one implementation instead of two
  that can drift), not required. — [#16](../../../issues/16)
- **Other `Mobile`/`Session` call sites share `ObdConnectionEngine`'s
  native-touch test ceiling.** `ObdMobile`/`ObdSession` (`DESIGN.md`
  section 3/4) exist so the engine's connect/backoff/retry loop is
  testable under virtual time, but `ObdDeviceLister`,
  `DeviceScanActivity`, `ObdDeviceScanner`, `StatusActivity`, and
  `CarMonitorApplication` all call `Mobile`/`Session` directly too and
  hit the same Robolectric `UnsatisfiedLinkError` ceiling — deliberately
  out of scope since none have a `delay()`-driven loop to test against
  virtual time, only synchronous one-shot calls with no cited bug.
  `ObdMobile` already exists as the seam to extend if one ever needs
  it. — [#18](../../../issues/18)
