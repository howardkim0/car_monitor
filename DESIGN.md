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
│                            │  │ internal/storage (JSONL    │  │                  │
│                            │  │   append-only log on disk) │  │                  │
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
4. Decoded readings are appended to local storage (`internal/storage`) and
   also handed back to Kotlin (via a callback interface) for the status
   screen to display.
5. `internal/obd2` decides *which* PIDs to request, based on the active
   `vehicle.Profile`'s PID list — Kotlin never needs to know what a PID is.
   *How often* to poll is, for v1, a plain constant in
   `ObdForegroundService` (`Session`/`Commands()` carries no timing info);
   see section 12 for moving that into Go too.

## 5. Extensibility

### 5.1 Bluetooth devices

```go
// internal/device/device.go
type Profile struct {
    Name       string // human-readable, e.g. "OBDLink MX+"
    MACAddress string // "AA:BB:CC:DD:EE:FF"
    Protocol   string // "spp" (classic RFCOMM) — only one supported today
}

var known = []Profile{
    {Name: "Garage OBDLink", MACAddress: "AA:BB:CC:DD:EE:FF", Protocol: "spp"},
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
    Code    byte
    Name    string
    Mode    byte
    Decode  func(bytes []byte) float64
    Unit    string
}

type Profile struct {
    Make, Model string
    Year        int
    PIDs        []PID // subset of standard + any manufacturer-specific PIDs
}

var subaruForester2023 = Profile{
    Make: "Subaru", Model: "Forester", Year: 2023,
    PIDs: []PID{
        {Code: 0x0C, Name: "RPM", Mode: 0x01, Unit: "rpm", Decode: decodeRPM},
        {Code: 0x0D, Name: "Speed", Mode: 0x01, Unit: "km/h", Decode: decodeSpeed},
        {Code: 0x05, Name: "CoolantTemp", Mode: 0x01, Unit: "C", Decode: decodeTempC},
        // ... standard Mode 01 PIDs to start; Subaru-specific PIDs can be
        // added here once reverse-engineered / sourced.
    },
}

func Default() Profile { return subaruForester2023 }
```

Same pattern as devices: one hardcoded `Default()` today, but the rest of
the app only talks to `vehicle.Profile`. A second car means adding another
`Profile` value and a selection mechanism — no changes to `obd2` or
Kotlin. Longer-term this could move to a JSON/YAML file bundled as an
Android asset instead of a Go literal, so profiles can be edited without a
rebuild; not needed for v1.

### 5.3 Selecting device/vehicle without a rebuild

For v1, `device.Default()` and `vehicle.Default()` are simple hardcoded
functions — no config file, no UI. The interfaces above exist specifically
so that swapping this out later (env var, JSON asset, in-app picker) is a
localized change.

## 6. Storage

v1 uses an append-only JSON-lines file on local app storage
(`/data/data/<pkg>/files/readings.jsonl`), one `Reading` per line. No SQLite,
no cgo dependency, nothing to migrate — trivial to inspect with `adb pull`
and `jq`, trivial to replace with a real DB later if querying needs grow.

## 7. Background execution model

- **Foreground Service**, not `WorkManager` — this needs a persistent,
  long-lived Bluetooth socket, which is exactly the Foreground Service use
  case (`ConnectedDevice` / `dataSync` foreground service type).
- Started at app launch and (optionally) on `BOOT_COMPLETED`.
- Shows a persistent low-priority notification (required by Android for
  any foreground service) with current connection state.
- On socket error/disconnect: exponential backoff reconnect loop, capped
  (e.g. 30s max), rather than a tight retry loop draining the battery.
- User will need to exempt the app from battery optimization
  (`ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`) for reliable long-run
  background behavior — call this out in-app and in the README.

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
│   │   └── storage/          # JSONL append-only reading log
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

`android/car-monitor-debug.apk` is a debug-signed build committed directly
to `main`, kept up to date whenever `android/` or `mobile`'s exported
surface changes, so installing on a phone is just:

```sh
adb install -r android/car-monitor-debug.apk
```

This is a deliberate exception to normal practice — build outputs are
otherwise gitignored (`android/build/`, `android/app/build/`,
`android/app/libs/*.aar`) since they're regenerable from source. This one
file is tracked so anyone can sideload the app without a full SDK/NDK/Go
toolchain setup. Tradeoffs worth knowing:

- It's **debug-signed**, not release-signed — there's no release keystore
  in this repo (generating and storing a signing key is a separate,
  more sensitive decision than "commit the build output"). Fine for
  sideloading on your own device; not suitable for wider distribution
  without setting up real release signing first.
- Committing a ~14MB binary that gets replaced on every relevant change
  means `git log`/`git blame` on this path aren't meaningful, and the
  repo's `.git` history grows by roughly the APK's size on every update
  (git can't diff binaries). If that growth becomes a problem, moving to
  GitHub Releases (tagged, attached as a release asset, not in history)
  is the natural next step.

## 12. Open questions / future work

- Where does device/vehicle selection live once it's no longer hardcoded —
  a bundled JSON asset, or an in-app settings screen? (Interfaces in §5 are
  designed so this is additive.)
- DTC (fault code) reading/clearing is out of scope for v1 but fits the
  same PID-request pattern in `internal/obd2`.
- Long-term storage growth: JSONL is fine for early use; revisit if
  file size or query needs grow (SQLite via a pure-Go driver to avoid
  reintroducing cgo/NDK complexity for the app itself, e.g.
  `ncruces/go-sqlite3`).
- Polling cadence (section 4 step 5) currently lives as constants in
  `ObdForegroundService` rather than `internal/obd2`, since `Session`
  exposes no timing info today. Moving it into Go (e.g. an interval per
  `vehicle.Profile`, or per-`PID`) would let a future vehicle with
  different sampling needs express that without touching Kotlin.
