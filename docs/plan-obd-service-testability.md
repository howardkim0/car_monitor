# Plan: Testable Connection/Backoff Loop for ObdForegroundService

> Saved per `CLAUDE.md`'s "Planning docs go in docs/".

## Context

A prior pass added `@VisibleForTesting internal` to several
`ObdForegroundService` helpers (`hasBluetoothPermission`,
`openConnection`, `buildNotification`, `connectSocket`, `writeCommand`)
and raised `android/` coverage from ~37% to ~45%. That hit a hard
ceiling: `connectionLoop`/`readLoop`/`writeLoop`/`anomalyCheckLoop`/the
backup loops remain almost entirely untested (10 of 124 measured lines,
~8%), for two compounding reasons —

1. They use real `delay()` calls (200ms command interval, 250ms poll
   cycle, 3s permission-poll, up to 30s backoff, 5min no-connection
   timeout) — testing the backoff cap or the timeout honestly would
   mean a test that really waits 5 minutes.
2. They call `mobile.Session`/`mobile.Mobile` (gomobile/JNI-backed)
   directly and synchronously. `mobile.Mobile` is `public abstract
   class ... static native` with a `static {}` block that
   `System.loadLibrary`s on first touch (confirmed via `javap` on the
   `.aar`'s `classes.jar`) — under Robolectric's plain JVM (no native
   lib), that throws `UnsatisfiedLinkError`/`NoClassDefFoundError`.
   Unlike the existing `mockkObject(ObdDeviceLister)` /
   `mockkObject(ObdDeviceScanner)` precedent (`PairScannerScreenTest.kt`),
   `Mobile` isn't a Kotlin `object`, so MockK can't safely intercept it
   without first triggering that same class-load failure.

Asked how other Bluetooth apps handle this: the two standard patterns
are (a) wrap the native/platform layer behind a small interface and
inject a fake in tests, and (b) make the dispatcher/clock injectable so
`kotlinx-coroutines-test`'s virtual time replaces real `delay()` waits.
This plan applies both, scoped to `ObdForegroundService` only.

**Estimated payoff** (measured against the current Kover XML report,
not guessed): the 124-line loop/backoff block is realistically
80–90%-coverable once extracted (pure logic, same shape as the Go code
that already hits 100%) — call it +90–105 covered lines. That moves
app-wide coverage from **~45% to roughly 53–56%**. PR 1–2 below (just
wrapping `Mobile` calls without extracting the loops) is worth maybe
+1–2 points on its own; essentially all the gain comes from PR 3.

## Decisions and alternatives considered

**1. Interface scope — narrow, excluding diagnostic logging.**
Introduce `ObdMobile` (wraps `deviceMAC`, `selectedDeviceName`,
`newSession`, `initCommandCount`/`initCommandAt`, `syncLogsIfNeeded`)
and `ObdSession` (wraps `feed`/`commandCount`/`commandAt`/
`checkAnomalies`/`close`), each with a `Real*` implementation
delegating to the real gomobile types.

Considered wrapping every `Mobile.*` call site in the file, including
the ~9 scattered `Mobile.logDebug`/`logError` diagnostic calls.
Rejected: nothing branches on those, no bug has ever been about wrong
log content per CLAUDE.md's "every caught bug gets a regression test,"
and wrapping them buys zero new tests for double the interface
surface. Those 9 sites stay uncovered — consistent with DESIGN.md
§10's non-goal of coverage parity.

One concrete, already-observed motivating case for `selectedDeviceName`
specifically: `buildNotification()`'s `Connecting`/`Connected` branches
call it, which is exactly why those two states (unlike `Disconnected`/
`PermissionMissing`/`TimedOut`/`Stopped`) have no test today.

**2. Extract `ObdConnectionEngine`, not a mutable dispatcher `var`.**
Considered exposing `@VisibleForTesting internal var dispatcher =
Dispatchers.IO` (same shape as the existing exposed fields). Rejected:
`scope = CoroutineScope(Dispatchers.IO + Job())` is a field initializer
that runs at construction, before any test has a hook to override a
`var` — Robolectric's `buildService(...).create()` gives no reliable
gap to land an override first. It also doesn't address
`SystemClock.elapsedRealtime()` calls inside `connectionLoop()`, which
need a matching virtual-time source or the timeout math is wrong
regardless of dispatcher.

Instead: extract a plain constructor-injected class,
`ObdConnectionEngine(callbacks: Callbacks, mobile: ObdMobile =
RealObdMobile, clock: () -> Long = SystemClock::elapsedRealtime)`,
owning `connectionLoop`/`readLoop`/`writeLoop`/`anomalyCheckLoop`/
`runSessionLoops`/`closeConnection` and the `@Volatile socket`/
`session` fields. No `dispatcher` parameter — whichever coroutine calls
`engine.connectionLoop()` supplies that; production code is unchanged
(`scope.launch { engine.connectionLoop() }`), tests use `runTest {
engine.connectionLoop() }` directly, no Service, no Robolectric.

This is **not** "more logic moving into Kotlin" (DESIGN.md §3's
Go-owns-logic framing) — no decision moves from Go to Kotlin; the
backoff schedule, timeout threshold, and polling cadence are unchanged.
Only *where in the file tree* already-existing Kotlin code lives
changes. State that explicitly in the class doc comment and the
DESIGN.md edit, since it's a plausible misreading during the
Architect-persona pass.

**Correctness trap to flag for whoever implements:** the injected
`clock: () -> Long` must return time in the same virtual-time domain
`delay()` advances in tests — i.e. `{ testScope.currentTime }`
(`kotlinx-coroutines-test`'s `TestScope.currentTime`), not
`System.currentTimeMillis()`. Using the wrong clock source makes a
timeout test either always-zero or silently never exercise the timeout
branch while still passing.

**What stays on `ObdForegroundService`:** `openConnection()`,
`connectSocket()`, `hasBluetoothPermission()`, `ConnectionHandles`
(its `session` field's type changes from `mobile.Session` to
`ObdSession`), `buildNotification()` — these need real `Context`/
`BluetoothManager`/`BluetoothSocket` and have no coroutine-timing
complexity; already directly tested, no reason to move them.
`reconnectNow()`/`stopServiceImmediately()` keep their current
semantics: the Service holds the active `ObdConnectionEngine`
(constructed alongside `connectionJob` in `onStartCommand()`) and
delegates (`reconnectNow()` → `activeEngine?.closeConnectionNow()`);
`connectionJob?.cancel()` stays a Service-level call, unchanged from
what the existing `ACTION_STOP`/`onDestroy` tests already assert.

**3. Scope — `ObdForegroundService` only, nothing else.**
`ObdDeviceLister`, `DeviceScanActivity`, `ObdDeviceScanner`,
`StatusActivity`, `CarMonitorApplication` all touch `Mobile`/`Session`
directly too, but none has this file's wall-clock coroutine-loop
problem — they're synchronous, one-shot calls, no `delay()`-driven
backoff/polling. Per DESIGN.md §10, `android/` tests target regression
coverage for bugs actually found, not proactive percentage-chasing,
and there's no cited bug motivating touching those files now. `ObdMobile`
is named/scoped generically (not nested inside the engine) specifically
so it's reusable later without a redesign — flagged as a
`docs/open-questions.md` entry (own GitHub issue, per that doc's rule)
rather than left implicit.

## New tests this unlocks

All in a new `ObdConnectionEngineTest.kt` — plain JUnit4 (no
`RobolectricTestRunner`: `BluetoothSocket`/`ObdMobile`/`ObdSession` are
all mocked via MockK, no real Android framework or `SystemClock`/
`Context` dependency left once `clock`/`callbacks` are injected).
Needs `kotlinx-coroutines-test` added as a `testImplementation` (not
currently a dependency).

1. Exponential backoff sequence `1, 2, 4, 8, 16, 30, 30, ...`, capped
   at 30s — verified in milliseconds of real test time via
   `runTest`/virtual time, not 63+ seconds of real waiting.
2. A session/decode failure (generic `Exception` mid-loop) falls
   through to backoff and continues, doesn't crash.
3. `SecurityException` is treated as `PermissionMissing` on
   `PERMISSION_POLL_INTERVAL_MS`, not exponential backoff — currently
   entirely unverified.
4. `NO_CONNECTION_TIMEOUT_MS` (5min) elapsing calls
   `onNoConnectionTimeout()` exactly once and `connectionLoop()`
   returns — via `advanceTimeBy(5.minutes)`.
5. Cancellation always propagates, never gets swallowed as a retryable
   failure — the exact "tap Stop, it retries anyway" bug class the
   file's own doc comment already worries about; the `catch (e:
   CancellationException) { throw e }` line has no test today.
6. `writeLoop` sends the full ELM327 init sequence once per connection,
   `COMMAND_INTERVAL_MS` apart.
7. `writeLoop` skips empty commands from `commandAt(i)` (no write, no
   `delay()`).
8. `anomalyCheckLoop` calls `checkAnomalies()` every
   `ANOMALY_CHECK_INTERVAL_MS` (e.g. 3 calls over 3 simulated minutes).

`ObdForegroundServiceTest.kt`'s existing 19 tests keep passing with
minimal changes. Two more become possible there once `ObdMobile` is
wired in: `buildNotification` for `Connecting`/`Connected`, and
`openConnection()`'s success path (today only its Bluetooth-disabled
failure path is tested).

## DESIGN.md update (required, done first)

This changes §4's data-flow narrative and touches §3's Kotlin/Go
boundary language, so it needs the full `CLAUDE.md` design-first
workflow (DESIGN.md edit → Architect-persona pass → commit/push alone
→ implement → two-persona review → commit), even though no externally
observable behavior changes.

- **§3**: one sentence that `ObdMobile`/`ObdSession` exist purely as a
  Kotlin-side testing seam, not a relocation of business logic —
  preempt the "Kotlin is getting smarter" misreading.
- **§4**: note that `ObdConnectionEngine` (not `ObdForegroundService`
  itself) owns the read/write/backoff loop; `ObdForegroundService`
  retains Android lifecycle/socket-opening and implements the engine's
  callback interface.
- **§7**: update the three-step teardown description to name which
  piece of `connectionJob?.cancel()`/`closeConnection()`/
  `stopServiceImmediately()` now lives where.
- **§10**: short addendum — virtual-time coroutine tests now exist for
  the connection/backoff state machine specifically; other `Mobile.*`
  call sites are unaffected and intentionally out of scope (point to
  the new `docs/open-questions.md` entry).

## Sequencing (four PRs, suite green throughout)

- **PR 0** — DESIGN.md edits above, Architect-persona pass, commit+push
  alone (pre-authorized per `CLAUDE.md`).
- **PR 1** — scaffolding, zero behavior change: add
  `kotlinx-coroutines-test` (`testImplementation`) to
  `android/app/build.gradle.kts`; add `ObdMobile`/`ObdSession`/
  `RealObdMobile`/`RealObdSession` (new file, not yet wired into
  production code) plus `FakeObdMobile`/`FakeObdSession` test doubles.
  Existing suite unaffected.
- **PR 2** — wire `ObdMobile` into `ObdForegroundService` without
  extracting the engine: replace `Mobile.*`/`session.*` calls in
  `openConnection()`/`buildNotification()` with a `@VisibleForTesting
  internal var mobile: ObdMobile = RealObdMobile` field (same pattern
  as the fields already exposed this way). Small, behavior-preserving
  diff; existing 19 tests unchanged; add the `buildNotification`
  Connecting/Connected and `openConnection` success-path tests.
- **PR 3** — the payoff: extract `ObdConnectionEngine` per the design
  above, `ObdForegroundService` implements `Callbacks` and delegates.
  Update `ObdForegroundServiceTest.kt` only where fields/methods moved
  (should be minimal — `connectionJob`'s field and semantics are
  unchanged). Add `ObdConnectionEngineTest.kt` with all 8 tests above.
  Needs the two-persona review most, since it touches concurrency/
  state-machine code directly.
- **PR 4 (optional/stretch, can slip independently)** — same treatment
  for `gitBackupLoop`/`driveBackupLoop` (lower ROI: mostly "try,
  log-on-failure, wait," little branching). Not required to close this
  out.

Run `./gradlew testDebugUnitTest` after each PR; no PR should leave the
suite red even transiently.

## Explicitly out of scope

- `ObdDeviceLister`/`DeviceScanActivity`/`ObdDeviceScanner`/
  `StatusActivity`/`CarMonitorApplication`'s own `Mobile` touches — see
  "Scope" above. Flag in `docs/open-questions.md` with a linked GitHub
  issue instead of silently leaving it undocumented.
- No DI framework introduced (still no Dagger/Hilt in this repo) —
  plain constructor injection with default arguments only.
- No timing-constant changes (`COMMAND_INTERVAL_MS`, `POLL_CYCLE_MS`,
  backoff/timeout values all stay exactly as-is) — this is a pure
  testability restructuring, not a behavior change.

## Critical files

- `android/app/src/main/java/com/carmonitor/app/ObdForegroundService.kt`
- `android/app/src/test/java/com/carmonitor/app/ObdForegroundServiceTest.kt`
- `android/app/src/main/java/com/carmonitor/app/ObdDeviceLister.kt`
  (reference pattern for the wrapper-object precedent; likely next
  consumer of `ObdMobile` per the open-question above)
- `android/app/build.gradle.kts` (add `kotlinx-coroutines-test`)
- `DESIGN.md` (§3, §4, §7, §10)
- `go/mobile/mobile.go` (source of truth for exact `Session`/`Mobile`
  signatures the new interfaces must mirror)
