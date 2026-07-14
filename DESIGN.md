# Car Monitor — Design Doc

## 1. Summary

Car Monitor is an Android app that maintains a Bluetooth connection to a
car's OBD2 scanner (ELM327-compatible dongle), continuously pulls vehicle
data (RPM, speed, coolant temp, DTCs, etc.), and logs it locally. No
meaningful foreground UI beyond a status screen and Android's required
"this app is running" notification.

Core business logic (OBD2 protocol handling, parsing, storage, device and
vehicle configuration) is written in **Go**. Android requires a JVM entry
point and owns Bluetooth I/O and process-lifecycle APIs, so a thin Kotlin
shell hosts a Foreground Service and hands raw bytes to Go.

For v1:
- Bluetooth device defaults to one hardcoded MAC (a classic SPP ELM327
  dongle) unless the user pairs/selects another in-app (section 5.1) — no
  rebuild needed either way.
- Vehicle profile is hardcoded to a 2023 Subaru Forester, with no in-app
  override yet.

Both sit behind small interfaces so more devices/vehicles can be added
without restructuring the app.

## 2. Goals / Non-Goals

**Goals**
- Reliable background OBD2 capture with the screen off.
- Automatic reconnect on Bluetooth drop.
- Simple, inspectable local storage.
- Extensible to more dongles/cars without a rewrite.
- In-app Bluetooth device pairing/selection, no rebuild.

**Non-goals (v1)**
- No cloud sync, remote telemetry, or multi-device fleet management.
- No non-ELM327 protocols (e.g. proprietary CAN dongles).
- No iOS.

## 3. Why not "pure Go" on Android

Go can't ship as a standalone Android app — APKs need a JVM entry point,
and Bluetooth Classic (`BluetoothSocket`/RFCOMM, what most ELM327 dongles
use) and Foreground Service APIs are Java/Kotlin-only. Pure-Go BLE stacks
like `tinygo-org/bluetooth` don't target Android.

The split:
- **Go module** (`go/`): all business logic — ELM327/OBD2 protocol
  framing, PID decoding, device/vehicle registries, storage. Compiled to
  an Android `.aar` via `gomobile bind`.
- **Kotlin shell** (`android/`): the smallest glue possible — a
  `Foreground Service` that opens the Bluetooth socket and streams bytes
  to/from Go, plus permissions, the persistent notification, boot-start,
  and one status Activity. Its action buttons (battery-optimization
  exemption, export logs, view app logs, copy SSH public key, test
  alert, git push, pair/show Bluetooth devices, stop/start scanning,
  quit) are a single vertically-stacked column, not a grid — label
  lengths vary too much (a two-line "Copy SSH Public Key" next to a
  one-line "Quit App") to stay aligned in columns.

Go owns all interesting logic and tests; Kotlin is deliberately dumb I/O
plumbing plus Android ceremony. Framework-only concerns — zipping logs
for the share sheet (`LogExporter`), Bluetooth discovery UI — stay
Kotlin-only rather than round-tripping through Go.

## 4. Architecture

```
┌──────────────────────── Android process ─────────────────────────┐
│ ┌────────────────┐        ┌───────────────────────────────┐      │
│ │ StatusActivity │◄──────►│ ObdForegroundService (Kotlin) │      │
│ └────────────────┘        │ - BluetoothSocket (RFCOMM)    │      │
│                           │ - persistent notification     │      │
│                           │ - restarts on connection loss │      │
│                           └───────────────────────────────┘      │
│                                                                  │
│                            │ byte[] in / out (JNI, via gomobile) │
│                            ▼                                     │
│                           ┌──────────────────────────────────┐   │
│                           │ mobile.aar  (Go, gomobile)       │   │
│                           │ ┌──────────────────────────────┐ │   │
│                           │ │ internal/obd2  (ELM327 +     │ │   │
│                           │ │   PID request/response)      │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/device  (registry   │ │   │
│                           │ │   of known BT devices)       │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/vehicle (registry   │ │   │
│                           │ │   of known cars / PID maps)  │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/storage (CSV,       │ │   │
│                           │ │   UTC day-rotated readings)  │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/applog (size-       │ │   │
│                           │ │   capped app/error log)      │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/gitbackup (backs up │ │   │
│                           │ │   logs to remote git repo)   │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/sshkey (on-device   │ │   │
│                           │ │   SSH keypair generation)    │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/trend (trend        │ │   │
│                           │ │   detection & anomalies)     │ │   │
│                           │ ├──────────────────────────────┤ │   │
│                           │ │ internal/monitor (matches    │ │   │
│                           │ │   readings to trend checks)  │ │   │
│                           │ └──────────────────────────────┘ │   │
│                           └──────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

### Data flow

1. `ObdForegroundService` opens an RFCOMM socket to `Mobile.deviceMAC()`
   (the selected device, section 5.1, or the hardcoded fallback) using
   the standard SPP UUID (`00001101-0000-1000-8000-00805F9B34FB`).
2. Raw bytes read from the socket are pushed into Go via
   `Session.Feed(data []byte)`.
3. `internal/obd2` frames ELM327 responses, matches them to outstanding
   PID requests, and decodes typed readings (`Reading{PID, Name, Value,
   Unit, Timestamp}`) using the active `vehicle.Profile`.
4. Decoded readings are appended to storage (section 6.1) and handed
   back to Kotlin for display. A failed append goes to `internal/applog`
   (section 6.2) instead of being dropped silently; a decode failure
   (malformed/truncated response line) is expected noise on a real
   ELM327 link and is skipped without logging.
5. `internal/obd2` decides *which* PIDs to request (profile + discovery,
   section 5.2); Kotlin still decides polling cadence via a plain
   constant (`docs/open-questions.md`). Before requesting any PID, `writeLoop` sends a
   fixed ELM327 setup sequence once per connection —
   `obd2.InitCommands()` (`ATE0 ATL0 ATS1 ATH0 ATSP0`), exposed to
   Kotlin the same two-call way as `Commands()`/`CommandAt(i)`. Adapter
   settings (echo, headers, spacing, protocol) are RAM-resident and
   persist across Bluetooth (dis)connects, so a prior session — this
   app's or another OBD2 app's — can leave the adapter in a state
   `parseResponseBytes` can't read (it requires space-separated
   single-byte hex fields with no header/CAN-ID prefix). See
   `docs/defects.md` for the zero-readings incident this fixes.
   Deliberately no `ATZ` full reset instead: that costs a real ~1-2s
   reset on some clones every reconnect, including the frequent
   transient ones the backoff loop (section 7) already retries.
6. Independently, a periodic `anomalyCheckLoop` (`ANOMALY_CHECK_INTERVAL_MS`,
   60s) calls `Session.CheckAnomalies()`, which re-reads today's CSV
   (`storage.LoadReadings`), groups it into per-metric time series via
   `internal/monitor` (pairing same-cycle PIDs like the two fuel trims by
   nearest timestamp), and runs every applicable `internal/trend` check.
   Only a metric whose severity *changed* since the last check is
   reported back (via `AnomalyListener`), so a persisting Warning stays
   silent but an escalation, de-escalation, or recurrence each notify.
   Kotlin posts these as a heads-up notification on a separate,
   higher-importance channel from the ongoing connection-status one
   (`setAutoCancel(true)`, never `setOngoing`, so a swipe always
   dismisses it). Notification building lives in a standalone
   `AnomalyNotifications` object — used by both `ObdForegroundService`'s
   listener and `StatusActivity`'s "Test Alert" button, so there's one
   implementation, not two that can drift apart — which lets "Test
   Alert" post a sample notification without the Service running at
   all. That matters: routing it through the Service instead risks
   silently resuming a stopped connection (section 7's "resuming after a
   stop is always explicit" rule).

## 5. Extensibility

### 5.1 Bluetooth devices

```go
// internal/device/device.go
type Profile struct {
    Name       string   // human-readable, e.g. "OBDLink MX+"
    MACAddress string   // "00:1D:A5:68:98:8A"
    Protocol   Protocol // ProtocolSPP (classic RFCOMM) — only one supported today
}

var known = []Profile{
    {Name: "Garage OBDLink", MACAddress: "00:1D:A5:68:98:8A", Protocol: ProtocolSPP},
}

func Default() Profile { return known[0] }
```

`known`/`Default()` are the factory fallback for a fresh install; the
device actually connected to is `SelectedOrDefault(dir)` — a persisted
user choice (`SaveSelected`/`LoadSelected`, a plain-text `mac\nname\n`
file, same philosophy as `internal/applog`, section 6.2) that takes
priority once one exists. `mobile.DeviceMAC(storageDir)` and
`mobile.SetSelectedDevice(storageDir, mac, name)` are the JNI wrappers —
`internal/obd2` never sees a literal MAC, only ever `device.Profile`.

Two Kotlin-only entry points on the status screen write a selection
(matching `LogExporter`'s precedent, section 3 — pairing UI is framework
ceremony, not business logic):
- **"Pair Bluetooth OBD2 Scanners"** (`DeviceScanActivity`) lists
  already-bonded devices for one-tap selection, and separately runs
  `BluetoothAdapter.startDiscovery()` for nearby unpaired ones — tapping
  one calls `BluetoothDevice.createBond()` (Android's own pairing
  dialog; this app never implements pairing itself) and selects it once
  bonding completes. The scan button toggles: a second tap calls
  `cancelDiscovery()` immediately rather than waiting out Android's own
  ~12s timeout. `startDiscovery()`'s boolean return is checked — it can
  return `false` (adapter disabled, discovery already running) without
  throwing — and a `SecurityException` from a denied permission (scan,
  list bonded devices, read a device's name) surfaces as a visible
  status message, not just a log line. Before scanning, `isLocationEnabled()` checks system
  Location Services directly on API < 31 (no `neverForLocation`
  exemption exists below API 31, section 8) and shows a message if it's
  off, rather than running a scan guaranteed to find nothing. Status
  text reports live progress — "Scanning… (N found)" / "Scan finished —
  N found" — so an empty result reads as confirmed zero, not "stuck."
  Every step is logged via `Mobile.logDebug` (section 6.2). See
  `docs/defects.md` for the three-round investigation behind this
  design.
- **"Show Paired Devices"** — a lightweight `AlertDialog` (no new
  Activity) listing every device the phone has ever paired with, each
  with a status: `Connected`, `Selected` (next attempt will use it), or
  plain `Paired`.

Both call `ObdForegroundService.reconnectNow()` after a selection
change — closes the current socket/session so `connectionLoop`'s
existing retry logic picks up the new `DeviceMAC()` on its next attempt;
a no-op if the service isn't running (the new selection just applies
whenever "Start Scanning" is next tapped).

### 5.2 Vehicles

```go
// internal/vehicle/vehicle.go
type PID struct {
    Code   byte
    Mode   Mode
    Name   string
    Unit   string
    Decode func(data []byte) (float64, error)
}

type Profile struct {
    Make, Model string
    Year        int
    PIDs        []PID
}

var subaruForester2023 = Profile{
    Make: "Subaru", Model: "Forester", Year: 2023,
    PIDs: []PID{
        {Code: 0x04, Mode: ModeCurrentData, Name: "Calculated Engine Load", Unit: "%", Decode: decodePercentOfByte},
        {Code: 0x05, Mode: ModeCurrentData, Name: "Coolant Temperature", Unit: "C", Decode: decodeByteMinus40},
        // ... 30 more standard SAE J1979 Mode 01 PIDs — see vehicle.go for
        // the full, current list; not duplicated here since it would just
        // drift out of sync with the code.
    },
}

func Default() Profile { return subaruForester2023 }
```

Targets the NA FB25 2.5L Forester specifically, not the turbo FA24 — SAE
J1979 has no dedicated "boost" PID either way (boost is inferred from
Intake Manifold Pressure vs. Barometric Pressure, both included), only
how that pressure *behaves* differs by engine.

**32 curated PIDs, not the full 80+ SAE J1979 Mode 01 table.** The
discovery mechanism below means an unsupported listed PID is simply
never requested, so there's no runtime cost to a broader list — only
implementation/test cost, which is what actually bounds it. Excluded:
bit-encoded/enum PIDs (don't fit the single-`float64` `Decode` model),
wideband O2 (redundant with the voltage-only PIDs included), ethanol%
(irrelevant to a non-flex-fuel Forester). The dual-bank O2 PIDs
(0x14-0x17) each return two values but `Decode` only supports one —
only voltage is decoded, since the trim sub-field duplicates the
bank-level trim already captured elsewhere.

**PID discovery, not static over-requesting.** `Commands()` starts by
returning SAE "PIDs supported" bitmask queries (derived from the
profile's max PID code) rather than all 32 PIDs from cycle one, and
switches to the real per-PID list once the ECU's bitmask resolves which
are supported — or after a 5s timeout, falling back to requesting
everything. Go owns the entire phase transition; Kotlin's `writeLoop()`
just keeps polling `Commands()` blindly. Only covers Mode 01 (the only
mode this app requests) — a future mode would need its own discovery
handling.

Same extensibility pattern as devices: one hardcoded `Default()` today,
but the rest of the app only talks to `vehicle.Profile`. A second car is
an additive `Profile` value plus a selection mechanism — no changes to
`obd2` or Kotlin. Could move to a bundled JSON/YAML asset later so
profiles are editable without a rebuild; not needed for v1.

### 5.3 Selecting device/vehicle without a rebuild

`device.Default()` is runtime-overridable via 5.1's persisted-selection
mechanism. `vehicle.Default()` is still a hardcoded function with no
config file or UI (`docs/open-questions.md`) — the interface exists so swapping it out
later (env var, JSON asset, extending the device-picker UI) is a
localized change.

## 6. Storage

### 6.1 Reading log

`internal/storage.FileStore` appends one CSV row per `Reading`
(`pid,name,value,unit,timestamp`) to
`/data/data/<pkg>/files/readings/readings-YYYY-MM-DD.csv`, one file per
**UTC** calendar day — deliberately no local-timezone reference anywhere,
so a file's contents stay unambiguous regardless of where the phone
travels, and a future "give me Tuesday's drive" query is just picking a
file. UTC matters concretely for a car: a drive can cross timezone
boundaries or a DST transition mid-session, and local timestamps would
make that shift look like a rotation event (or a clock going backward)
even though nothing about the drive changed. `applog`'s timestamps
(section 6.2) are UTC for the same reason, so both logs stay
correlatable regardless of trip location.

Rotation is checked on every `Append`, keyed off the *reading's own*
timestamp rather than wall-clock-at-write — handles both reopening the
app after a gap (resumes today's file if one exists) and a drive
spanning UTC midnight mid-session (rotates the moment a post-midnight
reading is appended). The header is written based on a post-open size
check rather than a pre-open existence check, so a 0-byte file from a
previously failed header write gets retried rather than treated as
already-headered.

No SQLite, no cgo — trivial to inspect with `adb pull` and any
CSV-aware tool, trivial to replace with a real DB later if needed.

`storage.LoadReadings` re-reads today's file for trend/anomaly detection
(section 4 step 6) — a row that fails to parse is skipped, not fatal;
a file that can't be read at all (as opposed to not existing yet) is a
real error.

On every new Bluetooth connection, `mobile.Session` prunes reading-log
files to the 30 most recent by filename count, not age — if the phone
sits unused for months, all 30 retained files can be well over 30
calendar days old. Best-effort: a failed prune doesn't block session
creation.

### 6.2 App/error log

`internal/applog` is a small, best-effort, plain-text log (not CSV —
heterogeneous data, unlike the reading log) for errors/debug messages at
`/data/data/<pkg>/files/app.log`, capped at `applog.MaxSizeBytes` (10MB).
On exceeding that, the current file is renamed to `app.log.1` (any
existing `.1` discarded first) and a fresh file started; if the rename
itself fails, the file is simply reopened at the same path rather than
going dark.

`mobile.LogError`/`mobile.LogDebug` are package-level gomobile exports
(not tied to any one `Session`, which is recreated on every reconnect,
but the app log must stay open across that churn), called from both
`ObdForegroundService`'s Kotlin log sites and Go's own error paths —
including `mobile.go`'s reading-append path (see `docs/defects.md` for
the swallowed-error bug this fixed).

Every write here is best-effort by design — a logging failure must
never crash or block the app. `Mobile.initAppLog` is called once from
`CarMonitorApplication.onCreate()` — before any Activity or Service
exists, not from `ObdForegroundService` (see `docs/defects.md` for the
silent-no-op-logging bug that traces to) — and is never explicitly
closed: the log must stay open for as long as anything in the process
might still log to it, not just while the Service happens to be
running, and `Application.onTerminate()` is unreliable on real devices
anyway. The call is wrapped in `catch (e: Throwable)`, not just
`Exception`: gomobile's `Mobile` class does native-library loading in
its static initializer, and a failure there surfaces as
`UnsatisfiedLinkError`/`ExceptionInInitializerError` — `Error`
subtypes a plain `Exception` catch would miss, crashing the whole app
over what is at worst a logging feature not working. That same catch
is also what keeps this call safe under Robolectric (`docs/dev-setup.md`),
which has no native library to load at all.

That same native-library-on-first-touch behavior is why every
`Mobile.*` call from an Activity is dispatched off the main thread —
via `scope.launch(Dispatchers.IO) { ... }` — rather than called inline.
Under Robolectric (plain-JVM unit tests, `docs/dev-setup.md`) there's no native
`libgojni.so` to load at all, so a synchronous `Mobile.*` call reached
during `onCreate()` throws `UnsatisfiedLinkError` and fails the test
outright. `StatusActivity` and `DeviceScanActivity` both follow this
rule — every `Mobile.*` call from an Activity is dispatched off the
main thread (see `docs/defects.md` for the regression this traces to).

Two build-identification diagnostics, both motivated by log evidence
that turned out to predate the fix it was meant to confirm (see
`docs/defects.md`):
- `BuildConfig.GIT_COMMIT` (`git rev-parse --short=12 HEAD` at build
  time, `"unknown"` if git isn't available) is logged once via
  `Mobile.logDebug` at app startup, so a log export can be matched to
  the exact build that produced it.
- `versionCode`/`versionName` are also build-time-derived, from `git
  rev-list --count HEAD` (total commit count): `versionName =
  "0.<count>"` (the `0.` prefix is deliberate — no stable release
  exists yet), `versionCode` set to the same integer. Needs CI's
  checkout to fetch full history (`fetch-depth: 0`) — unlike
  `GIT_COMMIT` above, a commit *count* is meaningless from a shallow
  checkout. Falls back to `1`/`"0.1"` if git is unavailable.
  `StatusActivity` shows this in a small label on the status screen —
  the version a driver would actually see and report, distinct from
  `GIT_COMMIT`'s exact-build-matching role.

**"View App Logs"** (`LogViewerActivity`) reads `app.log` directly for
in-app viewing, without needing `adb`, a file manager, or the
git-backup path (section 7) reachable at all. Kotlin-only, same
`LogExporter` precedent (section 3): `LogViewer.readTail()` is a small,
directly-unit-testable helper that reads only the last 200KB via
`RandomAccessFile` — not the full 10MB-capped file into one `TextView`
— discarding a leading partial line so the text starts at a clean line
boundary, with a truncation notice when the file is larger than that. A
Refresh action re-reads, since `app.log` grows live while monitoring
runs. Complements, not replaces, "Export Logs": the viewer is a quick
in-app glance; export is still how the full file (plus `app.log.1` and
the reading CSVs) actually leaves the device.

### 6.3 SSH key for log backup

On-device ed25519 keypair, generated once and persisted at
`/data/data/<pkg>/files/ssh/id_ed25519(.pub)` (modes 0o600/0o644), used
to authenticate log backups to a remote git repository (section 7's
git-backup loop). Idempotent — cached on first call to
`mobile.SSHPublicKey()` and reused forever, since regenerating would
orphan any deploy key already registered upstream.

Generated via `crypto/ed25519` + `golang.org/x/crypto/ssh` in
`internal/sshkey`, not shelled out (Android has no `ssh-keygen`
binary). Surfaced via a "Copy SSH Public Key" button, disabled until
the key loads from disk on a background coroutine, so the user can
register it as a GitHub deploy key without `adb`.

## 7. Background execution model

- **Foreground Service**, not `WorkManager` — needs a persistent,
  long-lived Bluetooth socket (`ConnectedDevice`/`dataSync` foreground
  service type).
- Started at app launch and (optionally) on `BOOT_COMPLETED`.
- Shows a persistent low-priority notification with current connection
  state, as Android requires for any foreground service.
- On socket error/disconnect: exponential backoff reconnect, capped
  (e.g. 30s max), not a tight battery-draining retry loop.
- No connection within 5 minutes — including time waiting on a missing
  Bluetooth permission — stops the service (`ConnectionState.TimedOut`)
  rather than retrying forever against a dongle that's never going to
  answer.
- Needs the battery-optimization exemption
  (`ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`) for reliable long-run
  background behavior.
- **Stopping is synchronous and immediate, not just requested.** A
  Stop/Start toggle on the status screen and a notification "Stop"
  action both send `ACTION_STOP`. `stopServiceImmediately()` — the
  single teardown path shared by manual stop, Quit, and the timeout
  above — does three things, in order:
  1. `connectionJob?.cancel()` — cancellation alone can't interrupt a
     blocking call already in flight (`connect()`, `read()`), so this
     only takes effect at the next suspension point.
  2. `closeConnection()`, called directly — unblocks a call from (1)
     stuck mid-flight, from whichever thread it's on.
  3. `updateState()` with the terminal state, then
     `stopForeground`+`stopSelf`.

  `StatusActivity` unbinds itself in direct response to any terminal
  `ConnectionState` (`Stopped`, `TimedOut`) — a Service stays alive as
  long as it's started *or* bound, so an unhandled bound Activity would
  keep it alive regardless of `stopSelf()`. See `docs/defects.md` for
  the two real bugs this three-step teardown fixed.

  Resuming after a stop is **always explicit** — tapping "Start
  Scanning" is the only way; reopening the app alone does not resume
  monitoring.
- **Quit App**: same teardown as Stop, then `Process.killProcess()` —
  takes the whole process down (everything runs in one process).
- **`reconnectNow()`**: used when switching the selected device (section
  5.1) rather than adding a fourth teardown path — deliberately lighter
  than `stopServiceImmediately()`: just closes the current
  socket/session so `connectionLoop`'s own retry logic reconnects with
  the new `DeviceMAC()`, without touching `connectionJob`, without a
  terminal `ConnectionState`, and without requiring "Start Scanning"
  afterward.
- **Git backup loop** runs independently of the Bluetooth lifecycle, in
  the Service's own coroutine scope (started once in `onCreate()`,
  cancelled in `onDestroy()`) rather than introducing `WorkManager` as a
  second background mechanism. Checked every 5 minutes
  (`GIT_BACKUP_CHECK_INTERVAL_MS`, matching `gitbackup.Syncer`'s own
  `syncInterval`). A failed push (e.g. no cell signal on a mountain
  drive) is caught, logged via `Mobile.logError`, and retried next
  cycle — never blocks anything; `lastSynced` only advances on success.
  Network calls (`PlainCloneContext`/`PushContext`) are timeout-bounded
  so a bad-signal attempt fails fast. A **"Git Push"** button
  (`Mobile.forceSyncLogs`, wrapping `Syncer.SyncNow` — which shares
  `SyncIfNeeded`'s clone/copy/commit/push logic but skips its gate
  check) triggers an immediate, ungated push. SSH host-key verification
  is **pinned to
  GitHub's own published ed25519 host key** (fetched from
  `https://api.github.com/meta`) via `ssh.FixedHostKey`, with
  `HostKeyAlgorithms` also set explicitly to `["ssh-ed25519"]` so
  that's actually the key type negotiated — GitHub also supports RSA
  and ECDSA host keys, and without a stated preference `FixedHostKey`
  can end up rejecting whichever of those gets negotiated instead. This
  fails closed if GitHub ever rotates the key, rather than silently
  accepting an unverified one. See `docs/defects.md` for the two-stage
  SSH handshake failure that led to pinning both the key and its
  algorithm.

## 8. Permissions

- `BLUETOOTH` and `BLUETOOTH_ADMIN` (`maxSdkVersion=30`) — superseded by
  `BLUETOOTH_CONNECT` on API 31+.
- `BLUETOOTH_CONNECT` (Android 12+, API 31+).
- `BLUETOOTH_SCAN` (Android 12+, API 31+) — requested at runtime by
  `DeviceScanActivity` before `startDiscovery()`; not needed just to
  connect to an already-paired MAC. The Android shell must not call any
  SCAN-gated API (e.g. `BluetoothAdapter.cancelDiscovery()`) without
  also requesting this first, or the call fails with
  `SecurityException` on API 31+. Declared with
  `android:usesPermissionFlags="neverForLocation"` (this app never
  derives location from scan results) — without it, API 31+ also
  silently requires system Location Services on for discovery to
  return any results. API 31+ only (see `ACCESS_FINE_LOCATION` below
  for API < 31). See `docs/defects.md`.
- `ACCESS_FINE_LOCATION` (still required by some OEMs for classic
  Bluetooth on API < 31) — on API 26-30 there's no
  `neverForLocation`-equivalent exemption at all (`minSdk` is 26,
  `docs/dev-setup.md`); `DeviceScanActivity` checks
  `LocationManager.isLocationEnabled()` directly on these versions
  instead.
- `FOREGROUND_SERVICE` and `FOREGROUND_SERVICE_CONNECTED_DEVICE` (API 34+).
- `POST_NOTIFICATIONS` (Android 13+, runtime-requested).
- `RECEIVE_BOOT_COMPLETED` (optional, only if auto-start on boot is enabled).
- `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` — pairs with section 7's
  exemption prompt.
- `INTERNET` — for git backup to `car_monitor_logs.git`.

## 9. Repo layout

```
car_monitor/
├── DESIGN.md
├── go/                       # all business logic, plain Go module
│   ├── go.mod
│   ├── internal/
│   │   ├── obd2/             # ELM327 framing, PID request/response loop
│   │   ├── device/           # known Bluetooth device profiles
│   │   ├── vehicle/          # known vehicle profiles + PID maps
│   │   ├── storage/          # CSV, UTC day-rotated reading log
│   │   ├── applog/           # size-capped app/error log
│   │   ├── gitbackup/        # git backup of logs to remote repo
│   │   ├── sshkey/           # on-device SSH keypair generation and persistence
│   │   ├── trend/            # trend detection and anomaly checking
│   │   └── monitor/          # groups readings into per-metric series for trend
│   └── mobile/               # gomobile bind entry point (exported API)
├── android/                  # Android Studio / Gradle project
│   └── app/
│       └── src/main/java/.../ObdForegroundService.kt
├── docs/
│   ├── dev-setup.md          # local build prerequisites, build steps, testing tooling
│   ├── defects.md            # log of past bugs: symptom, root cause, fix
│   └── open-questions.md     # known gaps/future work, mirrored as GitHub issues
└── scripts/
    └── setup_ubuntu.sh       # installs/maintains all local build prereqs
```

`go/` (including `mobile/`) and `android/` are both implemented, matching
this layout.

## 10. Testing philosophy

Build prerequisites, build steps, and testing tooling (Robolectric,
MockK, coverage commands) live in [`docs/dev-setup.md`](docs/dev-setup.md)
— none of that is needed to understand the app's architecture, only to
build/test it locally. Two decisions belong here instead, since they're
relevant to every commit, not just local setup:

**Coverage parity between `go/` and `android/` is a deliberate
non-goal.** `go/` reaching enforced 100% is straightforward (pure logic,
no framework I/O to mock); doing the same for `android/` would mean
exhaustively simulating every Bluetooth/Service-lifecycle interaction
through Robolectric/MockK — a materially larger, more speculative
undertaking. `android/` tests target regression coverage for bugs
actually found, not a percentage. CI reports Android coverage (Kover) as
a build artifact; it isn't gated.

**Regression tests exist for bugs actually found during development**,
per `CLAUDE.md`'s "every caught bug gets a regression test" — see
`docs/defects.md` for what each bug was and why. `ACTION_QUIT` is
deliberately not exercised through an automated test — its handler ends
in `Process.killProcess(Process.myPid())`, which would kill the test JVM
itself, and there's no Robolectric shadow worth trusting there. It
shares `ACTION_STOP`'s exact `stopServiceImmediately()` call, which that
test already covers; the kill call itself is one line, checked by
direct code review.

