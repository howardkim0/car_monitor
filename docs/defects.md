# Past Defects

A log of real bugs found in this app: symptom reported, root cause, and
fix. Grouped by subsystem (not chronologically) so a new report can be
checked against past patterns in the same area — see `CLAUDE.md`'s
"Check docs/defects.md before investigating a new defect."

This is a history doc, not a description of current behavior — for how a
system works *today*, read `DESIGN.md` (each entry below links back to
the section that documents the fix's current state). Add a new entry
here whenever a bug is found and fixed, per `CLAUDE.md`'s "every caught
bug gets a regression test" — the entry captures the story the test
alone can't (what the symptom looked like, why it happened), while
`DESIGN.md` stays timeless.

## OBD2 / ELM327 protocol

**Zero decoded readings despite a live, correctly-polling session**
(`84ed281`, `adf5c07`). Symptom: a full day's CSV had no data rows even
though the car was driving and the same dongle had worked moments
earlier in a different OBD2 app. Root cause: ELM327 adapter settings
(echo, linefeeds, spacing, headers, protocol) are RAM-resident and
persist across Bluetooth (dis)connects — a prior session (this app's or
another app's; headers-on is common, so multi-ECU responses can be
told apart) can leave the adapter in a state `parseResponseBytes`
can't read (e.g. a headered response like `7E8 04 41 0C 1A F8`, where
`7E8` doesn't fit the parser's single-byte-field assumption). That
failure was indistinguishable from ordinary ELM327 noise (skipped
silently, same as an echoed command or the `>` prompt), so polling
looked "live" — on schedule, zero decode errors — with zero readings
produced. Fix: `writeLoop` now sends a fixed AT init sequence once per
connection before requesting any PIDs (`obd2.InitCommands()`); a
follow-up fix (`adf5c07`) caught that the init loop wasn't flushing the
output stream the way the main polling loop already did, so the
commands were being written but not actually sent. See `DESIGN.md`
section 4 step 5 for the current design and why a full `ATZ` reset was
deliberately not used instead.

Follow-up (`b8c9f11`): since a decode failure at this layer is silent
by design (expected noise), the only way to diagnose a repeat of this
class of bug is seeing what the adapter actually sent — added raw
first-20-lines-per-session logging and a running received/decoded
count. See `docs/open-questions.md`.

**A second, distinct zero-readings bug, caught by the diagnostics
above.** Symptom: "the scanner is sending data but the app is not
writing anything" — a fresh log showed active `readLoop`/`writeLoop`
traffic (bytes arriving on schedule, commands going out) alongside
`obd2: stats: 6100 lines received, 0 decoded as readings (0%)`. The
raw-line log (added in the follow-up above) showed why directly: every
response line after the very first was prefixed with a stray `>`
(e.g. `">41 20 90 1F B0 11 "` instead of `"41 20 90 1F B0 11 "`).
Root cause: `Feed` splits strictly on `\r`, but the ELM327 ready prompt
has no terminator of its own — the adapter emits it immediately after
a response's trailing `\r` with nothing following it, so it lands
glued onto the front of whatever line comes next once split. This
wasn't the harmless standalone-`>`-line case `parseResponseBytes`
already handled (see the entry above) — it corrupted an otherwise-valid
response's first hex field (`>41` isn't valid hex), failing every
single reading, every session, on every adapter, unconditionally (not
adapter-state-dependent like the first bug). Fix: `stripPrompt` removes
a leading `>` before parsing, applied after the raw-line diagnostic log
so that still shows the unstripped bytes actually received. See
`DESIGN.md` section 4 step 5.

**Decode percentage stuck around 50% on an otherwise-healthy session**
(discovered from a real `car_monitor_logs` export on versionName 0.111,
well after the two bugs above were fixed). Symptom: readings were
landing in the CSV correctly, but `obd2: stats` logs never climbed
past ~45-50%, even over a long, stable session with no adapter
disconnects. The raw first-20-lines log showed why: every real response
line was immediately followed by a separate blank line (e.g. `"41 00
B6 3F A8 13 "` then `""`), consistently, for the whole session. Root
cause: `InitCommands()` sends `ATL0` ("line feeds off"), but this
particular adapter terminates responses with `\r\n` regardless — and
`Feed` split strictly on `\r`, so the stranded `\n` became its own
zero-content "line" once the next `\r` arrived. That line can never
parse as a `Reading` (`parseResponseBytes` requires ≥2 fields), so it
was correctly excluded from `linesDecoded` but still counted in
`linesReceived`, capping the ratio at ~50% by construction of the stat
— not because half of requested PIDs were actually going unanswered.
Fix: `Feed` now swallows a `\n` immediately following the terminating
`\r` before counting a line, via `skipLeadingLF` (also handles the case
where the two bytes arrive split across separate Bluetooth reads/`Feed`
calls). A genuine blank response (bare `\r\r`, no line feed) is still
counted, since only a `\n` directly paired with the preceding `\r` is
adapter noise. Senior-engineer review caught that `skipLeadingLF` must
*not* be cleared by an empty/nil intervening `Feed` call that hasn't
actually seen the deciding byte yet — an unconditional reset there would
let the `\n` back through as its own phantom line the next time `Feed`
ran with real data. See `DESIGN.md` section 4 step 5.

## Bluetooth device scanning / pairing

Three rounds of "the scan button doesn't work" reports against the
same feature, each round finding a real, distinct bug — worth reading
in order as an example of why re-review (not "no fresh log evidence,
close as unreproducible") is the right response to a repeat report.

**Round 1 — button gets stuck, no visible errors** (`ff999a8`).
Symptom: tapping "Pair Bluetooth OBD2 Scanners" appeared to hang, with
no way to stop it short of leaving the screen. Two real, independent
contributors: `startDiscovery()`'s boolean return value was never
checked, so a `false` return (adapter disabled, discovery already
running) left the button showing "Scanning…" forever with no scan
actually running; and a `SecurityException` from a denied permission
was only ever a log line, never a visible message — indistinguishable
from "no devices nearby." Same commit also made the scan button a
toggle (second tap calls `cancelDiscovery()` immediately, rather than
waiting out Android's own ~12s timeout) and added the `BLUETOOTH_SCAN`
manifest permission's `neverForLocation` flag, which turned out to be
this round's most likely primary cause (see Round 2) but wasn't
independently confirmed against the reporting device's Location
Services state at the time.

Caught during review of that same fix, before it shipped further:
a **double Toast** (`7f87ea2`) — the `SecurityException` catch block
and the subsequent `if (!started)` branch both showed a "could not
start" Toast, so a permission-denial failure showed the message twice.
Also a **dead string resource** (`device_scan_scanning`, `7f87ea2`)
left over once the toggle redesign superseded it, caught by grepping
source for the identifier.

**Round 2 — toggle works, but no discoverable devices ever show up**
(`42d21cc`). Root cause: `neverForLocation` (Round 1's fix) only
exempts **API 31+** from needing the system Location Services toggle
on for discovery to return results. This app's `minSdk` is 26, and on
API 26-30 there is no equivalent exemption at all —
`startDiscovery()` still returns `true` and `ACTION_DISCOVERY_FINISHED`
still fires on schedule, just with zero `ACTION_FOUND` broadcasts in
between, silently. Fix: `DeviceScanActivity.isLocationEnabled()` checks
the toggle directly before scanning on API < 31
(`LocationManager.isLocationEnabled()` on API 28+, the legacy
`Settings.Secure.LOCATION_MODE` field below that) and shows a direct
message if it's off. Also added live scan-status text ("Scanning… (N
found)" / "Scan finished — N found") so a genuinely-empty result reads
as confirmed zero, not "stuck" — see `DESIGN.md` sections 5.1 and 8.

**Round 3 — still "no devices," but the log evidence was stale**
(`93b8e5f`). A user-submitted `app.log` export was checked against the
Round 2 fix and found to contain *zero* lines postdating that fix's
release — every error in it predated the install, confirmed by
comparing log timestamps against the GitHub Actions run that published
that release. Not a new bug in the scan logic itself, but two real
gaps this exposed: `DeviceScanActivity` had no logging at all on its
non-error scan path, so even a same-session export couldn't have
confirmed whether a scan actually ran; and nothing in `app.log`
identified which commit produced it, making "is this actually the
fixed build?" a guessing game from timestamps alone. Fix: added
`Mobile.logDebug` calls for the location check result,
`startDiscovery()`'s return value, and each
`ACTION_FOUND`/`ACTION_DISCOVERY_FINISHED`; stamped every build with
`BuildConfig.GIT_COMMIT` (`git rev-parse` at build time), logged once
at app startup. See `DESIGN.md` section 6.2.

Building that same fix surfaced one more bug before it shipped: the
new log calls were added as direct, synchronous `Mobile.*` calls in
`onCreate()`/`startScan()`, which broke `./gradlew testDebugUnitTest`
with `UnsatisfiedLinkError` — Robolectric's plain JVM has no native
`libgojni.so` to load, so the first *synchronous* touch of the
gomobile-bound `Mobile` class during a test fails outright (existing
`Mobile.*` calls elsewhere in the codebase all happened to be
dispatched inside `scope.launch(Dispatchers.IO) { ... }` coroutines,
which avoided ever hitting this in a test run). Fixed by dispatching
the new calls the same way. See `DESIGN.md` section 6.2 for the
resulting rule ("every `Mobile.*` call from an Activity is dispatched
off the main thread").

**Round 4 — genuinely zero devices found, with multiple devices in
pairing mode 10cm from the phone.** The Round 3 diagnostics finally
paid off: a fresh log showed `startDiscovery()` returning `true`,
`locationEnabled=true`, and then `Scan stopped by user: found=0` after
27 seconds — no `ACTION_FOUND` lines at all, not even one. Every prior
round had addressed a real but secondary issue (a stuck button, a
silent permission failure, two different Location-Services gaps)
without ever questioning whether the receiver could receive broadcasts
at all — until a report with devices confirmed physically present and
discoverable ruled out "nothing nearby" as an explanation. Root cause:
the discovery `BroadcastReceiver` was registered with
`RECEIVER_NOT_EXPORTED` (added when `DeviceScanActivity` was first
built, `d03c192`, and never revisited since). Per Android's own
documentation, `RECEIVER_NOT_EXPORTED` silently drops broadcasts from
"highly privileged apps, such as Bluetooth and telephony, that are
part of the Android framework but don't run under the system's unique
process ID" — exactly `ACTION_FOUND`/`ACTION_DISCOVERY_FINISHED`/
`ACTION_BOND_STATE_CHANGED`, all sent by the Bluetooth stack. This had
silently broken discovery from the very first `DeviceScanActivity`
commit; none of Rounds 1-3's fixes could have surfaced it, since each
found a real, independently-reproducible symptom of its own. Fix:
`RECEIVER_EXPORTED` instead — safe, since all three actions are
AOSP `<protected-broadcast>` entries only the system can ever send, so
no third-party app can spoof them by exporting the receiver. See
`DESIGN.md` section 5.1.

## Storage / app log

**`mobile.go`'s reading-append path silently swallowed a failed
`store.Append`** (`f9d1930`, `_ = s.store.Append(r)`) — a storage
failure (e.g. disk full) would vanish with no trace. Fix: routed
through `internal/applog`'s `LogError` instead, alongside adding the
app/error log itself. See `DESIGN.md` section 6.2.

**Logging from `DeviceScanActivity` (and anything else run before
monitoring started) was a silent no-op.** Found via a background
investigation task, then confirmed directly against the source before
fixing. Root cause: `Mobile.initAppLog()` was only ever called from
`ObdForegroundService.onCreate()` — and `LogDebug`/`LogError` are
genuine no-ops on the Go side until that's run
(`TestLogErrorAndLogDebugAreNoOpsBeforeInit`). Critically, that
`onCreate()` is never reached at all if the user previously tapped
Stop: resuming is always explicit (`DESIGN.md` section 7), so
`ObdForegroundService.start()` is simply never called on that launch.
A user who opened "Pair Bluetooth OBD2 Scanners" in that state got no
log trail for it whatsoever — not a brief startup race, but the entire
session. Fix: moved `Mobile.initAppLog()` to a new
`CarMonitorApplication.onCreate()`, which Android guarantees runs
before any Activity or Service in the process; stopped explicitly
closing the log from `ObdForegroundService.onDestroy()` too, since it
must stay open for as long as anything in the process might still log
to it, not just while that Service happens to be running. See
`DESIGN.md` section 6.2.

## Git backup / SSH

**Every push failed: "cannot create known hosts callback"** (`dc02f6d`,
design in `3dfeab4`). Root cause: go-git's `HostKeyCallback` is `nil`
unless set explicitly, which falls back to reading `~/.ssh/known_hosts`
— a file that can never exist in this app's sandbox (no `$HOME`, no
`SSH_KNOWN_HOSTS`). Every push, automatic or manual, failed at the SSH
handshake regardless of network connectivity. Fix: pinned GitHub's own
published ed25519 host key (fetched from `https://api.github.com/meta`,
not transcribed from a docs page) via `ssh.FixedHostKey`.

**After that fix, pushes failed with "host key mismatch"** (`bd93fc6`).
Root cause: pinning the key alone wasn't sufficient — `HostKeyAlgorithms`
is a sibling field on the same `*ssh.PublicKeys` and, left unset,
`golang.org/x/crypto/ssh`'s own default algorithm list applies instead.
GitHub supports RSA, ECDSA, and ed25519 host keys, so without a stated
preference the negotiated key type wasn't guaranteed to be the ed25519
one this app pins — `FixedHostKey` then correctly (if confusingly)
rejected whatever different key type got negotiated. Fix: set
`HostKeyAlgorithms: ["ssh-ed25519"]` explicitly. Verified directly
against the real `github.com:22` before landing: the handshake fails
with exactly that error when unset, succeeds (reaching the next stage,
authentication) once set. See `DESIGN.md` section 7 for the current
SSH setup.

**A later "git push still not working" report was stale-APK evidence,
not a new bug** — the same class of issue as Bluetooth Round 3 above,
and the direct motivation for the `BuildConfig.GIT_COMMIT` stamp: two
log exports, taken minutes apart right after installing the latest
release, contained only error lines that predated that install (one
matching the `dc02f6d`-era symptom, one matching the `bd93fc6`-era
symptom — confirmed by bracketing both against the GitHub Actions
build-completion timestamps for each fix). No code change was needed
here beyond the diagnosability fix described in the Bluetooth Round 3
entry above.

## Foreground service lifecycle ("Stop" unreliable)

**Tapping Stop appeared to work, then monitoring silently resumed**
(`7a75546`). Two independent real bugs: (1) `connectionLoop()`'s
`catch (e: Exception)` was also catching `CancellationException` — a
plain `Exception` subtype in Kotlin — and treating a requested stop as
just another failed connection attempt to retry; fixed by rethrowing
`CancellationException` explicitly before any broader catch. (2) A
Service stays alive as long as it's started *or* bound, and the
notification's Stop action goes straight to the service, bypassing
`StatusActivity` — so if the app happened to be open (bound) when Stop
was tapped, nothing unbound it and `stopSelf()` alone did nothing.
Fixed with a single teardown path (`stopServiceImmediately()`) that
closes the connection directly (unblocking any blocking
`connect()`/`read()` call already in flight, which cancellation alone
cannot interrupt) before updating state and stopping.

**The 5-minute no-connection timeout left a zombie service when the
app was open** (`3808042`) — the same "bound service ignores
`stopSelf()`" root cause as above, reached via a different trigger:
the timeout ran the same teardown from inside the service's own
coroutine, with no way to make an already-bound `StatusActivity`
unbind first. The notification vanished but the service stayed alive,
bound and idle, with the in-app status text frozen on stale "retrying"
text forever. Fixed by adding a distinct `ConnectionState.TimedOut`,
routed through the existing state-listener plumbing, so
`StatusActivity` unbinds itself in direct response to *any* terminal
state — not left as something incidental to which code path happened
to call `updateState()`. See `DESIGN.md` section 7 for the current
three-step teardown and the "resuming after a stop is always explicit"
rule this produced.

**Backfilled regression tests caught two more, smaller bugs** in the
same area while standing up the Android test suite (`1df576c`): a
failed `BluetoothSocket.connect()` wasn't closing the socket
afterward (leaking it on every failed connection attempt) — fixed by
extracting `connectSocket()` so the close-on-failure path is directly
testable; and a second `onStartCommand()` call arriving while a
`connectionLoop()` was still active (e.g. from a screen rotation
re-requesting permissions) would launch a second, concurrent
`connectionJob` rather than being a no-op — fixed by checking
`connectionJob?.isActive` before launching a new one.

## Status screen UI

**The version label (and the last few buttons before it) were
invisible off-screen** (`1ebb44c`). Symptom: "the version number
doesn't show on the app," reported right after the version-label
feature shipped. Root cause: the status screen's button column was a
plain `LinearLayout` with no `ScrollView` — `statusText` +
`readingsText` + 10 action buttons overflows the visible area on any
real phone screen, with no way to scroll to whatever fell past the
fold. The version label, appended last, was simply the first symptom
to get reported; `Quit`/`Stop` and other trailing buttons were equally
unreachable on smaller screens, just less likely to be needed in a
quick glance. Fix: wrapped the whole screen in a `ScrollView`;
`readingsText` no longer needs `layout_weight="1"` to compete with the
buttons for space, which also stops it from being silently clipped if
the reading list itself runs long. See `DESIGN.md` section 3.

## Android build / release

**CI-built debug APKs couldn't be installed over a previous install**
— every CI run used a fresh, ephemeral `debug.keystore` auto-generated
per-runner, so consecutive `latest` releases carried different
signatures and Android refused the in-place update (only a full
uninstall/reinstall worked). Fix: a persistent signing key, stored as
GitHub repo secrets and decoded to a runner-temp path at build time,
never committed. See `docs/dev-setup.md`'s "Signing" subsection.

**`libgojni.so` wasn't 16KB-page aligned**, surfaced as an install-time
warning ("LOAD segment not aligned") on a real device rather than a
crash — noticed while installing a build for Drive-backup verification
(see the Android Auto section above for the DHU-testing context this
followed). `readelf -lW libgojni.so` showed every `LOAD` segment's
alignment column as `0x1000` (4KB) across all four ABIs. Root cause:
the pinned NDK (26.1.10909125, `docs/dev-setup.md`) predates NDK r27's
switch to defaulting `ld`'s max page size to 16KB — Android is moving
toward requiring 16KB-aligned native libraries on ARM64 hardware, and
without this, `gomobile bind`'s cgo-linked output keeps the legacy 4KB
default regardless of target API level. Fix: `CGO_LDFLAGS="-Wl,-z,max-page-size=16384"`
set before every `gomobile bind` invocation — local dev steps
(`docs/dev-setup.md`) and both CI workflows that build `mobile.aar`
(`coverage.yml`, `release-apk.yml`) now set it. Verified with
`readelf -lW <path> | grep LOAD` showing `0x4000` on all four ABIs,
surviving Gradle's `stripDebugDebugSymbols` step, confirmed on a real
device with no alignment warning at install time. A future NDK bump to
r27+ would make this flag redundant but isn't required for the fix.

## Android Auto

**Every screen crashed on a real Android Auto host with "Car Monitor
has encountered an unexpected error"**, verified end-to-end for the
first time against the Desktop Head Unit (see `docs/dev-setup.md`'s
"Testing on Android Auto") once DHU could actually reach a phone (see
that doc's DHU setup gotchas). `adb logcat` showed the real cause:
`CarApp.H.Tem: Error: ... java.lang.IllegalArgumentException: Min API
level not declared in manifest (androidx.car.app.minCarApiLevel)`, from
`AppInfo.retrieveMinCarAppApiLevel` via `CarAppService.getAppInfo()`.
`AndroidManifest.xml` had the Car App Library's discovery meta-data
(`com.google.android.gms.car.application`) but never added the
separate `androidx.car.app.minCarApiLevel` meta-data the library also
requires at the `<application>` level — nothing in Robolectric's
`ScreenController`/`TestCarContext` harness exercises
`CarAppService.getAppInfo()` (existing tests call
`Screen.onGetTemplate()` directly, per `DESIGN.md` section 11's
"Testing note"), so this shipped invisibly until a real host tried to
negotiate API levels. Fix: add `<meta-data
android:name="androidx.car.app.minCarApiLevel" android:value="1" />`
next to the existing car-application meta-data. Regression test:
`CarMonitorCarAppServiceTest` asserts the manifest's parsed
`ApplicationInfo.metaData` contains that key — Robolectric parses the
real manifest, so this catches the gap without needing a real host.

## Trend / anomaly detection

**False "alternator failed" CRITICAL alert risk from a normal auto
idle-stop restart**, found by replaying real fleet data
(`car_monitor_logs`, see `CLAUDE.md`) at full timestamp resolution
rather than reading it in aggregate. Symptom: `internal/monitor`'s
`Evaluate` computed a single "latest RPM" value (independently of which
voltage reading it would gate) and passed it to
`trend.CheckBatteryVoltage`, which itself used only its single "latest
voltage" sample. This vehicle's auto idle-stop drops RPM to 0 at every
stop and restarts the engine at every light; control module voltage was
observed sagging as low as 10.82V for 2-3 seconds after every one of
several real restarts before the alternator caught up — numerically
identical to the "alternator failed" CRITICAL threshold (<12.0V while
running). Root cause had two layers: (1) RPM and voltage are read at
different instants within a poll cycle, so an independently-"latest"
RPM value could reflect a different moment than the voltage sample it
gated; (2) even *correct* nearest-timestamp pairing wasn't sufficient by
itself — replaying the data with proper pairing still left 3 false-
positive instances, because the low voltage really was the genuine
reading at that instant (real post-crank alternator recovery lag, not
stale data). Fix: `CheckBatteryVoltage` now takes a `rpms` series paired
by nearest timestamp to `voltages` (via `monitor.pairSeries`, the same
join already used for fuel trims and O2 sensors) *and* requires the
engine to have been continuously running for at least
`voltageSettleSeconds` (8s, comfortably past the ~3s longest recovery
observed) before trusting a single-value voltage threshold — derived by
scanning the paired RPM series backward for the last time it was at or
below the running threshold. The windowed decay/trend check is
unaffected (a multi-second transient can't satisfy its 5-sample/120s/R²
requirements). See `DESIGN.md` section 4 step 6 for the current
anomaly-check flow. Regression tests:
`TestEvaluateBatteryVoltageIgnoresRestartTransient` (monitor,
end-to-end) and the settle-window cases in `TestCheckBatteryVoltage`
(trend, unit-level).

Note: `CheckCatalyticConverter`'s RPM/coolant gate has the same
independently-"latest" pattern as the original bug, but wasn't fixed
here — real fleet data shows this vehicle's ECU doesn't report PID
`0x05` (Coolant Temperature) or `0x14` (O2 Sensor Bank 1 Sensor 1) as
supported at all (confirmed by decoding the raw discovery bitmask
response), so that check never runs in practice and the gap is
currently unreachable.
