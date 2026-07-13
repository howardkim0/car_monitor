# Car Monitor — Design Doc

## 1. Summary

Car Monitor is an Android app whose job is to sit in the background, maintain
a Bluetooth connection to a car's OBD2 scanner (ELM327-compatible dongle),
continuously pull vehicle data (RPM, speed, coolant temp, DTCs, etc.), and
log/process it locally. There is no meaningful foreground UI beyond a status
screen and Android's required "this app is running" notification.

Core business logic (OBD2 protocol handling, data parsing, storage, device
and vehicle configuration) is written in **Go**. Android requires a JVM
entry point and owns Bluetooth I/O and process-lifecycle APIs, so a thin
Kotlin shell hosts a Foreground Service and hands raw bytes to Go.

For v1:
- Bluetooth device is hardcoded to one known MAC address (a classic SPP
  ELM327 dongle).
- Vehicle profile is hardcoded to a 2023 Subaru Forester (PIDs it supports,
  units, any make-specific quirks).

Both are implemented behind small interfaces so additional devices and
vehicles can be added later without restructuring the app.

## 2. Goals / Non-Goals

**Goals**
- Reliable background capture of OBD2 data with the phone screen off.
- Reconnect automatically if the Bluetooth link drops (dongle out of range,
  car turned off, phone Bluetooth toggled).
- Store readings locally in a simple, inspectable format.
- Keep the door open for more dongles / more cars without a rewrite.

**Non-goals (v1)**
- No cloud sync, no remote telemetry, no multi-device fleet management.
- No in-app Bluetooth pairing UI / device picker (MAC is hardcoded).
- No support for non-ELM327 protocols (e.g. proprietary CAN dongles).
- No iOS.

## 3. Why not "pure Go" on Android

Go cannot ship as a standalone Android app — APKs need a JVM entry point,
and Android's Bluetooth Classic (`BluetoothSocket`/RFCOMM, which is what
most ELM327 OBD2 dongles use) and Foreground Service lifecycle APIs are
Java/Kotlin-only. `tinygo-org/bluetooth` and similar pure-Go BLE stacks
don't target Android.

The practical split, and what this doc assumes:

- **Go module** (`go/`): all business logic — ELM327/OBD2 protocol framing,
  PID decoding, device registry, vehicle registry, storage. Compiled to an
  Android `.aar` via `gomobile bind`.
- **Kotlin shell** (`android/`): the smallest amount of Android glue
  possible — a `Foreground Service` that opens the classic Bluetooth socket,
  streams raw bytes into the Go library, and reads processed results back
  out. Also owns permissions, the persistent notification, boot-start, and
  a single status Activity (connected/disconnected, last readings).

Go stays the place where all the interesting logic and all the tests live;
Kotlin is intentionally kept dumb (I/O plumbing + Android ceremony).

## 4. Architecture

```
┌─────────────────────────────── Android process ───────────────────────────────┐
│                                                                                  │
│  ┌───────────────┐        ┌────────────────────────────┐                       │
│  │ StatusActivity │◄──────►│  ObdForegroundService (Kotlin) │                   │
│  └───────────────┘        │  - BluetoothSocket (RFCOMM)     │                   │
│                            │  - persistent notification      │                  │
│                            │  - restarts on connection loss   │                 │
│                            └───────────────┬────────────────┘                  │
│                                             │ byte[] in / out (JNI, via gomobile)│
│                                             ▼                                   │
│                            ┌────────────────────────────────┐                   │
│                            │   mobile.aar  (Go, gomobile)    │                  │
│                            │  ┌───────────────────────────┐  │                  │
│                            │  │ internal/obd2  (ELM327 +   │  │                  │
│                            │  │   PID request/response)    │  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/device  (registry │  │                  │
│                            │  │   of known BT devices)     │  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/vehicle (registry │  │                  │
│                            │  │   of known cars / PID maps)│  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/storage (CSV,     │  │                  │
│                            │  │   UTC day-rotated readings)│  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/applog (size-     │  │                  │
│                            │  │   capped app/error log)    │  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/trend (trend      │  │                  │
│                            │  │   detection & anomalies)   │  │                  │
│                            │  ├───────────────────────────┤  │                  │
│                            │  │ internal/monitor (matches  │  │                  │
│                            │  │   readings to trend checks)│  │                  │
│                            │  └───────────────────────────┘  │                  │
│                            └────────────────────────────────┘                   │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Data flow

1. `ObdForegroundService` opens an RFCOMM socket to the hardcoded MAC using
   the standard SPP UUID (`00001101-0000-1000-8000-00805F9B34FB`).
2. Raw bytes read from the socket are pushed into the Go layer
   (`Session.Feed(data []byte)` in the gomobile binding).
3. Go's `internal/obd2` package frames ELM327 responses, matches them to
   outstanding PID requests, and decodes them into typed readings
   (`Reading{PID, Name, Value, Unit, Timestamp}`) using the active
   `vehicle.Profile`.
4. Decoded readings are appended to local storage (`internal/storage`, CSV,
   see section 6) and also handed back to Kotlin (via a callback interface)
   for the status screen to display. A failed append is not silently
   dropped — it goes to `internal/applog` (section 6) instead. A decode
   failure (a malformed or truncated response line) is not logged; it's
   treated as expected noise on a real ELM327 link and simply skipped.
5. `internal/obd2` decides *which* PIDs to request, based on the active
   `vehicle.Profile`'s PID list and a discovery step (section 5.2) — Kotlin
   never needs to know what a PID is. *How often* to poll is, for v1, a
   plain constant in `ObdForegroundService` (`Session`/`Commands()` carries
   no timing info); see section 12 for moving that into Go too.
6. Separately from that live per-reading path, `ObdForegroundService` also
   runs a periodic coroutine loop (`anomalyCheckLoop`, alongside the read/
   write loops — `ANOMALY_CHECK_INTERVAL_MS`, currently 60s, the same
   "Kotlin decides how often" precedent as step 5) that calls
   `Session.CheckAnomalies()`. That re-reads today's CSV log
   (`storage.LoadReadings`), hands it to `internal/monitor` to group into
   per-metric time series (pairing same-cycle PIDs like the two fuel trims,
   or the two O2 sensor voltages, by nearest timestamp — raw readings
   arrive as separate rows, not matched pairs), and runs every applicable
   `internal/trend` check. Only a metric whose severity level has *changed*
   since the last check is reported back to Kotlin, via a second callback
   interface (`AnomalyListener`) — so a persisting Warning stays silent
   instead of re-notifying every 60s, but an escalation, a de-escalation,
   or a recurrence after recovering to Normal each fire again. Kotlin turns
   that into a heads-up notification on a separate, higher-importance
   channel from the ongoing connection-status one.

## 5. Extensibility

### 5.1 Bluetooth devices

```go
// internal/device/device.go
type Profile struct {
    Name       string // human-readable, e.g. "OBDLink MX+"
    MACAddress string // "00:1D:A5:68:98:8A"
    Protocol   string // "spp" (classic RFCOMM) — only one supported today
}

var known = []Profile{
    {Name: "Garage OBDLink", MACAddress: "00:1D:A5:68:98:8A", Protocol: "spp"},
}

func Default() Profile { return known[0] }
```

Today the hardcoded MAC is just `Default()`, wired to a build-time config
value (see 5.3). Adding a second device later means appending to `known`
and adding a selection mechanism (config value, or eventually a UI picker);
`internal/obd2` and the Kotlin service only ever depend on `device.Profile`,
never on a literal MAC string.

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

**Targets the NA FB25 2.5L Forester specifically, not the turbo FA24.**
That distinction doesn't change *which* PIDs apply — SAE J1979 has no
dedicated "boost" PID; boost is inferred from Intake Manifold Absolute
Pressure (0x0B) exceeding Barometric Pressure (0x33), both included and
valid for either engine — only how Intake Manifold Pressure *behaves* (on
this NA engine it never exceeds ambient).

**PID scope is a curated practical subset (32 PIDs today), not the full
SAE J1979 Mode 01 table (80+ PIDs across spec revisions).** The
discovery mechanism below means a PID listed here that this particular
ECU doesn't support is simply never requested — there's no runtime cost
to the list being broader than any one car needs, only the
implementation/test cost of adding an entry, which is what actually
bounds the list's size. Excluded and why: bit-encoded/enum PIDs (mode
status bytes, OBD standards, fuel type) don't fit the single-`float64`
`Decode` model and aren't "data" the way a temperature/pressure/speed
reading is; wideband/lambda O2 variants are redundant with the simpler
voltage-only O2 PIDs included; ethanol % is irrelevant to a
non-flex-fuel Forester. PIDs 0x14/0x15/0x16/0x17 (O2 sensor banks 1 and
2 — the boxer engine's genuine two banks, one per cylinder pair) each
return *two* values (sensor voltage, then short-term trim), but `Decode`
only supports one `float64` — only the voltage is decoded, since the
trim sub-field is redundant with the bank-level trim already captured
via PIDs 0x06-0x09.

**PID discovery, not static over-requesting.** `internal/obd2.Session`
doesn't request every PID in the profile from cycle one — with 32
PIDs (vs. the original 4), that would balloon a poll cycle to seconds
for PIDs that may not even be supported. Instead, `Commands()` initially
returns SAE "PIDs supported" bitmask queries (Mode 01 PIDs
`0x00`/`0x20`/`0x40`, derived from the profile's actual max PID code, not
hardcoded) and only switches to the real per-PID request list once the
ECU's bitmask responses resolve which profile PIDs it actually supports
— or after a 5-second timeout, which falls back to requesting
everything (matching the old always-request-everything behavior as a
safe default, rather than silently going dark, if discovery itself fails
for some reason). Kotlin's `writeLoop()` needs no changes for this — it
already just polls `Commands()` blindly every cycle; Go owns the entire
phase transition. Discovery only covers Mode 01 ("show current data") —
`discoveryCommands` hardcodes `vehicle.ModeCurrentData` — since that's
the only mode this app requests (section 12); a future mode (e.g. Mode 09
vehicle info) would need its own discovery/support-bitmask handling, not
an extension of this one.

Same extensibility pattern as devices otherwise: one hardcoded
`Default()` today, but the rest of the app only talks to
`vehicle.Profile`. A second car means adding another `Profile` value and
a selection mechanism — no changes to `obd2` or Kotlin. Longer-term this
could move to a JSON/YAML file bundled as an Android asset instead of a Go
literal, so profiles can be edited without a
rebuild; not needed for v1.

### 5.3 Selecting device/vehicle without a rebuild

For v1, `device.Default()` and `vehicle.Default()` are simple hardcoded
functions — no config file, no UI. The interfaces above exist specifically
so that swapping this out later (env var, JSON asset, in-app picker) is a
localized change.

## 6. Storage

### 6.1 Reading log

`internal/storage.FileStore` appends one CSV row per `Reading`
(`pid,name,value,unit,timestamp` — a header row once per file) to
`/data/data/<pkg>/files/readings/readings-YYYY-MM-DD.csv`, one file per
**UTC** calendar day — both the filename's date and every `timestamp`
value are UTC, deliberately with no local-timezone reference anywhere,
so a file's contents are unambiguous regardless of where the phone
travels. This is specifically so a future "give me Tuesday's drive"
analysis is just picking a file, not filtering timestamps out of a
single ever-growing log.

Rotation is checked on every `Append` call, keyed off the *reading's own*
timestamp rather than wall-clock-at-write-time: if it falls on a
different UTC day than the currently-open file, the old file is closed
(best-effort — a failed close doesn't block opening the new file) and
the new day's file is opened, writing the header only if it's empty
after opening — a post-open size check rather than a pre-open existence
check, so a file left at 0 bytes by a previously failed header write
gets the header retried on the next resume instead of being treated as
already-headered forever. This one code path handles both cases the day
boundary can be crossed: reopening the app hours or days later (resumes
today's file if one already exists for today, rather than duplicating
the header or losing yesterday's data), and a drive that spans UTC
midnight mid-session (rotates the moment a reading dated after midnight
is appended, not just at the next app restart).

No SQLite, no cgo dependency, nothing to migrate — trivial to inspect
with `adb pull` and any spreadsheet tool or `csv`-aware shell tool,
trivial to replace with a real DB later if querying needs grow.

`storage.LoadReadings` reads today's file back into memory — the one
other consumer of this format, used by trend/anomaly detection (section 4
step 6) rather than anything Kotlin calls directly. A row that fails to
parse is skipped rather than failing the whole read (skips forward past
CSV-syntax-level damage instead, e.g. a torn final line from an unclean
process kill mid-write); a file that can't be read at all — as opposed to
simply not existing yet — is a real error.

### 6.2 App/error log

Separate from the reading log deliberately: `internal/applog` is a
small, best-effort, plain-text log (not CSV — this is heterogeneous,
unstructured data, unlike the reading log's tabular data) for errors and
debug messages, at `/data/data/<pkg>/files/app.log`. It doesn't need the
reading log's day-based rotation (there's no "which day's errors" query
this needs to support) — instead it's a single file capped at
`applog.MaxSizeBytes` (10MB), and on exceeding that, the current file is
renamed aside to `app.log.1` (any existing `.1` is discarded first — one
kept prior file, not unbounded growth) and a fresh file started. If the
rename itself fails (e.g. transient I/O error), the current file was
never actually renamed away, so it's simply reopened at the same path —
logging keeps working (just without having rotated that time) rather
than going dark for the rest of the process over a rotation failure.

Reachable from both sides of the Go/Kotlin split, per this doc's
"Go owns business logic" split (section 3): `mobile.LogError`/
`mobile.LogDebug` are package-level (not tied to any one `Session` —
a `Session` is recreated on every Bluetooth reconnect, but the app log
must stay open across that churn) gomobile exports Kotlin calls into
`ObdForegroundService`'s existing `Log.w` sites, and Go's own error
paths write to the same log — notably `mobile.go`'s reading-append
path, which used to silently swallow a failed `store.Append` (`_ =
s.store.Append(r)`); it now routes that error through `LogError`
instead.

Every write here is best-effort by design: a logging failure must never
crash or block the app it's attached to, so both the Go side
(`internal/applog`) and the Kotlin call sites (`Mobile.initAppLog`/
`Mobile.closeAppLog`, wrapped in `catch (e: Throwable)` — not just
`Exception` — in `ObdForegroundService.onCreate()`/`onDestroy()`) treat
any failure here as something to log-and-continue past, never to
propagate. That `Throwable` (rather than `Exception`) catch is load-
bearing, not defensive-programming theater: gomobile's generated
`Mobile` class does native-library loading in its static initializer,
and a failure there surfaces as `UnsatisfiedLinkError`/
`ExceptionInInitializerError` — `Error` subtypes a plain
`catch (e: Exception)` does not catch, which would otherwise crash the
whole foreground service over what is, at worst, a logging feature not
working.

## 7. Background execution model

- **Foreground Service**, not `WorkManager` — this needs a persistent,
  long-lived Bluetooth socket, which is exactly the Foreground Service use
  case (`ConnectedDevice` / `dataSync` foreground service type).
- Started at app launch and (optionally) on `BOOT_COMPLETED`.
- Shows a persistent low-priority notification (required by Android for
  any foreground service) with current connection state.
- On socket error/disconnect: exponential backoff reconnect loop, capped
  (e.g. 30s max), rather than a tight retry loop draining the battery.
- If no connection is ever reached (or re-reached) within 5 minutes —
  counting time spent waiting on a missing Bluetooth permission too — the
  service stops itself (`ConnectionState.TimedOut`) via the same
  `stopServiceImmediately()` described below, rather than retrying
  indefinitely against a dongle that's never going to answer (car parked
  out of range, dongle unplugged, etc).
- User will need to exempt the app from battery optimization
  (`ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`) for reliable long-run
  background behavior — call this out in-app and in the README.
- **Stopping is synchronous and immediate, not just requested.** A "Stop
  monitoring" / "Start Scanning" toggle button on the status screen, and a
  "Stop" action on the persistent notification, both send `ACTION_STOP` to
  `ObdForegroundService`. `stopServiceImmediately()` — the single teardown
  path shared by a manual stop, Quit (next bullet), and the timeout above
  — does three things, in order, and all three matter:
  1. `connectionJob?.cancel()` — but cancellation alone cannot interrupt a
     blocking call already in flight (`BluetoothSocket.connect()`,
     `InputStream.read()`), so this only takes effect at the coroutine's
     next suspension point.
  2. `closeConnection()`, called directly rather than left to `onDestroy()`
     — closing the socket is what actually unblocks a call from (1)
     stuck mid-flight, from whichever thread it's stuck on.
  3. `updateState()` with the terminal state, *then*
     `stopForeground`+`stopSelf`.

  This exists because two earlier, real bugs made "Stop" unreliable:
  `connectionLoop()`'s `catch (e: Exception)` was catching
  `CancellationException` too (a plain `Exception` subtype in Kotlin) and
  treating a requested stop as just another failed attempt to retry —
  fixed by rethrowing it explicitly, before any broader catch clause. And
  a Service stays alive as long as it's started *or* bound, so a bound
  `StatusActivity` (app left open) meant `stopSelf()` alone did nothing
  and `onDestroy()` might never run — `StatusActivity` now unbinds itself
  in direct response to any terminal `ConnectionState` (`Stopped`,
  `TimedOut`) rather than that being incidental to who happened to call
  `updateState()`.

  Resuming after a stop is **always explicit** — tapping "Start Scanning"
  is the only way; reopening the app on its own does not resume
  monitoring (a fresh launch that was never stopped is unaffected, and
  still starts automatically as before).
- **Quit App**: a second action, on both the notification and the status
  screen, sends `ACTION_QUIT` — same teardown as Stop, then
  `Process.killProcess(Process.myPid())`. This takes the whole app process
  down, `StatusActivity` included, since everything runs in one process
  (no multi-process manifest config) — the standard way an Android app
  provides a true "exit," as opposed to Android's normal expectation that
  the OS manages process lifecycle.

## 8. Permissions

- `BLUETOOTH` and `BLUETOOTH_ADMIN` (`maxSdkVersion=30`) — the pre-API-31
  normal permissions any Bluetooth API call requires; superseded by
  `BLUETOOTH_CONNECT` on API 31+
- `BLUETOOTH_CONNECT` (Android 12+, API 31+)
- `BLUETOOTH_SCAN` (only needed if we ever add discovery; not required for
  connecting to a hardcoded, already-paired MAC, but harmless to declare
  ahead of the device-picker extensibility work) — note the Android shell
  must not call any SCAN-gated API (e.g. `BluetoothAdapter.cancelDiscovery()`)
  against a hardcoded MAC without also requesting this at runtime, or every
  connection attempt fails with `SecurityException` on API 31+
- `ACCESS_FINE_LOCATION` (still required by some OEMs for classic
  Bluetooth on API < 31)
- `FOREGROUND_SERVICE` and `FOREGROUND_SERVICE_CONNECTED_DEVICE` (API 34+)
- `POST_NOTIFICATIONS` (Android 13+, runtime-requested)
- `RECEIVE_BOOT_COMPLETED` (optional, only if auto-start on boot is enabled)
- `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` — pairs with the battery-optimization
  exemption prompt described in section 7

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
│   │   ├── trend/            # trend detection and anomaly checking
│   │   └── monitor/          # groups readings into per-metric series for trend
│   └── mobile/               # gomobile bind entry point (exported API)
├── android/                  # Android Studio / Gradle project
│   └── app/
│       └── src/main/java/.../ObdForegroundService.kt
└── scripts/
    └── setup_ubuntu.sh       # installs/maintains all local build prereqs
```

`go/` (including `mobile/`) and `android/` are both implemented, matching
this layout.

## 10. Local build prerequisites (Ubuntu)

All of the following are installed and kept up to date by
[`scripts/setup_ubuntu.sh`](scripts/setup_ubuntu.sh) rather than manual
steps, so a fresh Ubuntu box (or CI runner) can be brought to a working
build environment with one command.

| Tool | Why it's needed | Version pinned by script |
|---|---|---|
| Go | All app logic; also builds `gomobile`/`gobind` | 1.26.5 |
| OpenJDK 17 | Required by the Android Gradle Plugin | 17 (Temurin) |
| Android `cmdline-tools` + `sdkmanager` | Pulls SDK platform, build-tools, NDK | latest `cmdline-tools` |
| Android SDK Platform | Compile target | `android-34` |
| Android Build-Tools | `aapt`/`d8`/etc. | `34.0.0` |
| Android NDK | `gomobile bind` cross-compiles Go with cgo enabled (JNI bridge), which needs NDK's clang toolchains | `26.1.10909125` |
| `platform-tools` (adb) | Deploying/debugging on a device | latest |
| `gomobile` / `gobind` | Builds the Go code into an Android `.aar` | `golang.org/x/mobile@latest` |
| Android Studio | Optional, for editing/debugging the Kotlin shell with full tooling (layout preview, profiler, device manager). **Not required** to build — Gradle CLI + `sdkmanager` are sufficient — but installed by default for convenience | latest stable, via JetBrains' official archive |

Notes:
- Everything is installed under the invoking user's home directory
  (`~/Android/sdk`, `~/go`, `/opt/android-studio` for the Studio IDE) so the
  script doesn't need to touch system Python/Java if the distro ships its
  own — it uses its own pinned JDK/Go instead of relying on `apt`'s version.
- The script is idempotent: re-running it skips anything already installed
  at the pinned version and only patches `~/.bashrc`/`~/.profile` once.
- Android Studio install can be skipped with `SKIP_ANDROID_STUDIO=1
  ./scripts/setup_ubuntu.sh` for headless/CI boxes that only need the CLI
  toolchain.

## 11. Build steps (after running the setup script)

```sh
# 1. Build the Go core into an Android AAR
cd go/mobile
gomobile bind -androidapi 26 -o ../../android/app/libs/mobile.aar -target=android ./...

# 2. Build the Android app
cd ../../android
./gradlew assembleDebug

# 3. Install on a connected/USB-debugging device
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

`gofmt`, `go vet`, `go test ./...`, and `go build ./...` for the `go/`
module run automatically on every commit via `githooks/pre-commit` (see
`CLAUDE.md`). The `gomobile bind` / `gradlew` steps above are not part of
that hook — they need the Android SDK/NDK and are slow — so run them
manually whenever a change touches `android/` or `mobile`'s exported
surface.

`-androidapi 26` matches `android/app/build.gradle.kts`'s `minSdk`.
Without it, `gomobile bind` defaults to API 16, which NDK 26 (the version
`scripts/setup_ubuntu.sh` installs) no longer supports — the bind step
fails immediately with "unsupported API version 16 (not in 21..34)".

Measured on a machine with the SDK/NDK/`gomobile` already installed:
`gomobile bind` takes ~10s; `gradlew assembleDebug` takes ~1.5min on a
clean checkout (downloading the Gradle distribution + AGP + dependencies)
and ~10s on a warm rebuild.

### Pre-built APK

A debug-signed APK is published to this repo's [GitHub
Releases](../../releases) page under a single rolling `latest` release/tag,
built by `.github/workflows/release-apk.yml` on every push to `main` that
touches `android/**` or `go/**`. Installing on a phone is:

```sh
gh release download latest -p 'car-monitor-debug.apk' -R howardkim0/car_monitor
adb install -r car-monitor-debug.apk
```

(or just download `car-monitor-debug.apk` from the release page in a
browser and `adb install -r` it — or just tap it on-device once "install
unknown apps" is allowed for the browser/Files app, no `adb` needed). The
workflow deletes and recreates the `latest` release each run, so it always
reflects the current `main` HEAD — there's no version history beyond what
`git log` on `main` already gives you.

Build outputs are otherwise gitignored (`android/build/`,
`android/app/build/`, `android/app/libs/*.aar`, `android/*.apk`) since
they're regenerable from source; the APK used to be a tracked exception to
that (committed straight to `main`), but that meant `git log`/`git blame`
on the path weren't meaningful and `.git` grew by roughly the APK's size on
every relevant change (git can't diff binaries) — publishing to Releases
instead gets the same "always have a ready-to-sideload build" property
without either cost.

**Signing.** The build stays the `debug` build type (`android:debuggable`
is still `true` — this is not a hardened, Play-Store-style release build,
and `isMinifyEnabled` is off), but `android/app/build.gradle.kts` gives it
a stable signing key when four `CM_RELEASE_*` environment variables are
present, which `release-apk.yml` sets from repo secrets
(`RELEASE_KEYSTORE_BASE64`, `RELEASE_KEYSTORE_PASSWORD`, `RELEASE_KEY_ALIAS`,
`RELEASE_KEY_PASSWORD`) — decoding the keystore to a `RUNNER_TEMP` path,
never into the repo. Without those secrets configured, the build silently
falls back to whatever ephemeral `debug.keystore` that CI run's fresh VM
auto-generates, same as before this existed.

The problem this solves: Android refuses to install an APK over an
existing app unless the signatures match, and a fresh debug.keystore gets
auto-generated on every CI runner (a new VM each run) — so without a
persistent key, every `latest` release used to carry a different
signature, and updating meant uninstalling the old one first. A locally
built `./gradlew assembleDebug` is unaffected either way (still signed
with that machine's own persistent `~/.android/debug.keystore`); only CI
builds needed this.

One migration note: a phone that already has a build installed from
*before* the CI secrets were configured needs one manual uninstall — after
that, every future `latest` release shares the same key and installs as a
normal in-place update. Losing the keystore, or its passwords, means
generating a new one and going through that same one-time uninstall again
on every device; leaking it would let anyone produce an APK Android treats
as a legitimate update to this app, so it's kept out of the repo entirely
(GitHub secrets only, never committed — see `.gitignore`'s `*.keystore`/
`*.jks` rules).

## 12. Open questions / future work

- Where does device/vehicle selection live once it's no longer hardcoded —
  a bundled JSON asset, or an in-app settings screen? (Interfaces in §5 are
  designed so this is additive.)
- DTC (fault code) reading/clearing is out of scope for v1 but fits the
  same PID-request pattern in `internal/obd2`.
- Long-term storage growth: day-rotated CSV (section 6.1) is fine for
  early use; revisit if per-file size or cross-day query needs grow
  (SQLite via a pure-Go driver to avoid reintroducing cgo/NDK complexity
  for the app itself, e.g. `ncruces/go-sqlite3`).
- Polling cadence (section 4 step 5) still lives as constants
  (`COMMAND_INTERVAL_MS`/`POLL_CYCLE_MS`) in `ObdForegroundService`
  rather than `internal/obd2` — *which* PIDs to request is now decided
  in Go (section 5.2's discovery mechanism), but *how often* isn't yet,
  since `Session` still exposes no timing info. Moving that into Go too
  (e.g. an interval per `vehicle.Profile`, or per-`PID`) would let a
  future vehicle with different sampling needs express that without
  touching Kotlin.
- `COMMAND_INTERVAL_MS` was reduced from 100ms to 50ms alongside the PID
  expansion (32 PIDs now vs. the original 4, so a full cycle at the old
  interval would run several seconds) — unverified against real ELM327
  hardware, since this dev environment has no Bluetooth device access;
  revisit if real-world testing shows it's too aggressive.
- `mobile.Session.CommandCount()`/`CommandAt(i)` are two separate JNI
  calls, not one atomic snapshot. Now that `Commands()` can change
  mid-flight (discovery resolving between the two calls), Kotlin's
  `writeLoop()` could in principle see a `CommandAt` index that's gone
  stale between the count and the fetch. Accepted as a known, low-severity
  gap rather than fixed: the JNI boundary makes a single "return the whole
  list" call the real fix, which isn't worth doing for a mismatch that
  self-heals on the very next poll cycle (at most, one cycle skips or
  double-requests a PID).
- Trend/anomaly detection (section 4 step 6) re-reads and re-parses
  *today's entire* CSV log from disk on every check, even though every
  `internal/trend` check function only ever looks at the last 30s-5min of
  it — acceptable for now (CSV parsing tens of thousands of simple rows
  is still fast in absolute terms, and `ANOMALY_CHECK_INTERVAL_MS` is
  deliberately coarse, once a minute), but a full-day drive means paying
  that cost against a steadily growing file for the rest of the day.
  If this shows up as measurably expensive on a real device, the fix is
  incremental — track a byte offset and only read new rows since the last
  check, keeping a small in-memory sliding window per metric — not
  re-architecting the check functions themselves.
- `Session.CheckAnomalies`'s per-metric dedup state (`lastLevel`) is
  scoped to the `Session`, not persisted across Bluetooth reconnects (a
  new `Session` is created on each one), so an occasional duplicate
  notification around a reconnect is possible. Accepted for the same
  reason as the `CommandCount`/`CommandAt` gap above: the state would
  need to move to a package-level variable (like `internal/applog`'s
  logger) to survive `Session` recreation, which isn't worth the added
  shared-mutable-state complexity for an edge case this cosmetic.
- Only a metric moving to Warning/Critical ever notifies; there's no
  "this recovered to Normal" notification. Deliberately out of scope for
  v1 (the ask was "notify if an anomaly is found," not "and when it
  isn't anymore") but worth revisiting — a driver who got a low-battery
  alert earlier in a drive might reasonably want to know it's resolved.
- `internal/monitor`'s metric-name constants (e.g. `"Coolant
  Temperature"`) are matched against `vehicle.go`'s `PID.Name` fields by
  exact string equality, with no compiler-enforced link between the two —
  `monitor_test.go`'s `TestMetricNamesMatchVehicleProfile` guards against
  a silent rename in one drifting away from the other, but a shared
  source of truth (e.g. named constants `vehicle` exports and `monitor`
  imports) would remove the possibility structurally instead of relying
  on a test to catch it.

## 13. Testing

**`go/`**: table-driven `testing` package tests, one file per source file
(`obd2_test.go` next to `obd2.go`, etc.) — the existing, only convention.
`githooks/pre-commit` enforces both that these pass and a 100% statement
coverage floor (`go test -coverprofile=...` + `go tool cover -func=...`,
see `CLAUDE.md`'s "Coverage is enforced" section for exactly how). A
GitHub Actions workflow (`.github/workflows/coverage.yml`) re-runs the
same check on push/PR and emails on any regression below 100% — a safety
net for a bypassed local hook or a fresh clone, not the primary gate.

**`android/`**: JUnit4 + [Robolectric](http://robolectric.org/) (runs
Android framework code on the plain JVM — no emulator/device needed,
which matters given this project's dev environment has no working KVM
access) + [MockK](https://mockk.io/) for collaborators that Robolectric
doesn't simulate well (`BluetoothSocket`). Test sources live in
`android/app/src/test/java/com/carmonitor/app/`, run via
`./gradlew testDebugUnitTest`. Dependencies are `testImplementation` in
`app/build.gradle.kts`; `testOptions { unitTests { isIncludeAndroidResources = true } }`
lets Robolectric resolve `R.string.*`/`R.layout.*` in tests. Coroutines
are exercised against the real `Dispatchers.IO` rather than
`kotlinx-coroutines-test`'s virtual time — `ObdForegroundService`'s `scope`
has no injectable-dispatcher seam today, and adding one purely for test
determinism (accepting a few real seconds of wall-clock time per test
instead) wasn't judged worth the production-code change for what this
suite currently needs; revisit if that stops being true.
`ObdForegroundService.connectionJob`/`connectSocket()`/`ACTION_STOP`/`ACTION_QUIT`
and `StatusActivity.isBound` are `internal` + `@VisibleForTesting` rather
than `private`, specifically so these regression tests can observe them
directly instead of reverse-engineering state through side effects.

**Coverage parity between `go/` and `android/` is a deliberate
non-goal for now.** `go/` reaching a real, enforced 100% is
straightforward — it's business logic with no framework I/O to mock. Doing
the same for `android/` would mean exhaustively simulating every
Bluetooth/Service-lifecycle framework interaction and failure mode through
Robolectric/MockK, which is a materially larger and more speculative
undertaking than closing five small gaps in pure Go functions. `android/`
tests focus on regression coverage for bugs actually found (see below) —
real, meaningful branches — over chasing a percentage. CI reports Android
coverage (Kover) as a build artifact; it isn't gated. Revisit this once
the Android test suite has enough real breadth that a numeric floor would
mean something.

**Regression tests, backfilled for bugs fixed this session** (per
`CLAUDE.md`'s "every caught bug gets a regression test," applied
retroactively where it still tests current behavior — not for behavior
this same round of changes deleted, like the old auto-resume-on-reopen):
`connectSocket()` (extracted from `openConnection()`) closing the socket
on a failed `connect()`; `onStartCommand()` refusing to launch a second
concurrent `connectionLoop()`; `ACTION_STOP` actually cancelling the
active `connectionJob` synchronously rather than only requesting a stop
that the still-running loop could outlive (the root cause of "tap Stop,
it retries anyway"); and both `TimedOut` and `Stopped` states unbinding
`StatusActivity` from the service. `ACTION_QUIT` is deliberately *not*
exercised through an automated test — its handler ends in
`Process.killProcess(Process.myPid())`, which would kill the test JVM
itself, not just an app-under-test process, and there's no Robolectric
shadow worth trusting there. It shares `ACTION_STOP`'s exact
`stopServiceImmediately()` call, which the `ACTION_STOP` test already
covers; the kill call itself is one line, checked by direct code review.
