# Local development setup

Build prerequisites, build steps, and testing tooling — split out of
`DESIGN.md` since none of this is needed to understand the app's
architecture, only to actually build/test it locally. See `DESIGN.md`
section 10 ("Testing philosophy") for the testing *decisions* this repo
holds itself to (100% Go coverage, coverage parity as a non-goal,
regression tests for every bug) — this doc is the mechanics underneath
those decisions, not the reasoning.

## Local build prerequisites (Ubuntu)

All of the following are installed and kept up to date by
[`scripts/setup_ubuntu.sh`](../scripts/setup_ubuntu.sh) rather than
manual steps, so a fresh Ubuntu box (or CI runner) can be brought to a
working build environment with one command.

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
| Android Studio | Optional, full IDE tooling. **Not required** to build — Gradle CLI + `sdkmanager` suffice — but installed by default for convenience | latest stable, via JetBrains' official archive |

Notes:
- Everything installs under the invoking user's home directory
  (`~/Android/sdk`, `~/go`, `/opt/android-studio`), using its own pinned
  JDK/Go rather than the distro's.
- Idempotent: re-running skips anything already at the pinned version,
  patches `~/.bashrc`/`~/.profile` only once.
- `SKIP_ANDROID_STUDIO=1 ./scripts/setup_ubuntu.sh` skips the IDE for
  headless/CI boxes.

## Build steps (after running the setup script)

```sh
# 1. Build the Go core into an Android AAR
cd go/mobile
export CGO_LDFLAGS="-Wl,-z,max-page-size=16384"
gomobile bind -androidapi 26 -o ../../android/app/libs/mobile.aar -target=android ./...

# 2. Build the Android app
cd ../../android
./gradlew assembleDebug

# 3. Install on a connected/USB-debugging device
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

`gofmt`, `go vet`, `go test ./...`, and `go build ./...` for `go/` run
automatically on every commit via `githooks/pre-commit` (see
`CLAUDE.md`). The `gomobile bind`/`gradlew` steps aren't part of that
hook — they need the Android SDK/NDK and are slow — so run them manually
whenever a change touches `android/` or `mobile`'s exported surface.

`-androidapi 26` matches `build.gradle.kts`'s `minSdk`; without it,
`gomobile bind` defaults to API 16, which NDK 26 no longer supports —
the bind step fails immediately with "unsupported API version 16 (not
in 21..34)".

`CGO_LDFLAGS="-Wl,-z,max-page-size=16384"` forces `libgojni.so`'s ELF
`LOAD` segments onto 16KB boundaries — the pinned NDK (26.1.10909125)
predates NDK r27's switch to 16KB alignment by default, so without this
flag every ABI's `.so` links at the legacy 4KB page size. This matters
for real ARM64 hardware moving to a 16KB kernel page size (Google Play
requires 16KB-aligned native libraries for apps targeting newer API
levels) — without it, `libgojni.so` either fails to load or triggers an
alignment warning at install time (`LOAD segment not aligned`). Verify
with `readelf -lW <path-to-libgojni.so> | grep LOAD` — the last column
should read `0x4000`, not `0x1000`. See `docs/defects.md`.

Measured with the SDK/NDK/`gomobile` already installed: `gomobile bind`
~10s; `gradlew assembleDebug` ~1.5min on a clean checkout, ~10s warm.

### Pre-built APK

A debug-signed APK is published to this repo's [GitHub
Releases](../../../releases) page under a single rolling `latest`
release/tag, built by `.github/workflows/release-apk.yml` on every push
to `main` touching `android/**` or `go/**`:

```sh
gh release download latest -p 'car-monitor-debug.apk' -R howardkim0/car_monitor
adb install -r car-monitor-debug.apk
```

(or download from the release page and `adb install -r`/tap it
on-device). The workflow deletes and recreates `latest` every run, so it
always reflects current `main` — no version history beyond `git log`.

Build outputs are gitignored (regenerable from source) rather than
committed, so `.git` doesn't grow by the APK's size on every change.

**Signing.** The build stays the `debug` build type, but
`android/app/build.gradle.kts` gives it a stable signing key when four
`CM_RELEASE_*` env vars are present (`release-apk.yml` sets these from
repo secrets — `RELEASE_KEYSTORE_BASE64`, `RELEASE_KEYSTORE_PASSWORD`,
`RELEASE_KEY_ALIAS`, `RELEASE_KEY_PASSWORD` — decoding the keystore to a
`RUNNER_TEMP` path, never into the repo). Without those secrets, it
falls back to CI's ephemeral per-runner `debug.keystore`. This matters
because Android refuses to install an APK over an existing app unless
signatures match, and a fresh ephemeral keystore per CI runner would
otherwise mean every `latest` release needs a manual uninstall to
update. A local `./gradlew assembleDebug` is unaffected either way (uses
that machine's own persistent debug keystore). See `docs/defects.md`.

One migration note: a phone with a build installed *before* the CI
secrets were configured needs one manual uninstall; every release after
that shares the same key and installs as a normal update. The keystore
itself is GitHub-secrets-only, never committed (`.gitignore`'s
`*.keystore`/`*.jks` rules) — leaking it would let anyone produce an APK
Android treats as a legitimate update to this app.

## Testing tooling

**`go/`**: table-driven `testing` package tests, one file per source
file. `githooks/pre-commit` enforces both passing tests and a 100%
statement coverage floor (see `CLAUDE.md`). `.github/workflows/coverage.yml`
re-runs the same check on push/PR and emails on any regression below
100% — a safety net for a bypassed local hook or fresh clone, not the
primary gate.

**`android/`**: JUnit4 + [Robolectric](http://robolectric.org/) (Android
framework on the plain JVM — no emulator/KVM needed) +
[MockK](https://mockk.io/) for collaborators Robolectric doesn't
simulate well (`BluetoothSocket`). Tests live in
`android/app/src/test/java/com/carmonitor/app/`, run via `./gradlew
testDebugUnitTest`. Coroutines run against real `Dispatchers.IO` rather
than `kotlinx-coroutines-test`'s virtual time — no injectable-dispatcher
seam exists yet; revisit if that stops being cheap enough.
`ObdForegroundService.connectionJob`/`connectSocket()`/`ACTION_STOP`/
`ACTION_QUIT` and `StatusActivity.isBound` are `internal` +
`@VisibleForTesting` rather than `private` so regression tests can
observe them directly.

Note for anyone touching `Mobile.*` call sites: under Robolectric
there's no native `libgojni.so` to load, so a synchronous `Mobile.*`
call reached during `onCreate()` throws `UnsatisfiedLinkError` and fails
the test outright — see `DESIGN.md` section 6.2 for the
dispatch-off-the-main-thread rule this requires. `carapp/` tests that
need to exercise `ObdDeviceLister` mock it with MockK's `mockkObject`
(paired with `unmockkObject` in `@After` — it patches the singleton
process-wide, and leaving it mocked leaks into any other test class in
the same suite run) rather than fighting the same native-load
constraint a second time.

### Testing on Android Auto

The `carapp/` classes (`DESIGN.md` section 11) render on a real Android
Auto host, not just under Robolectric — verify end-to-end behavior with
the **Desktop Head Unit (DHU)**, Google's Android Auto
(phone-projection) simulator. This targets phone-projected Android
Auto specifically, not Android Automotive OS — the Studio "Automotive"
system image/AVD is for that other, car-native target and won't run
this app's `CarAppService` at all.

```sh
# Install once, either via Android Studio's SDK Manager
# (SDK Manager → SDK Tools → "Android Auto Desktop Head Unit emulator")
# or the command line:
sdkmanager "extras;google;auto"   # → $ANDROID_HOME/extras/google/auto/desktop-head-unit

adb forward tcp:5277 tcp:5277
desktop-head-unit
```

Requires a connected device/emulator with the Android Auto app
installed and its "head unit server"/developer mode enabled (Android
Auto app → Settings → tap the version number to unlock developer
settings → "Start head unit server"). A debug build's `HostValidator`
is permissive (`ALLOW_ALL_HOSTS_VALIDATOR`), so no extra host
allowlisting is needed for local DHU testing.

**Known gotchas getting DHU actually running (Linux):**

- **Missing `libc++.so.1`/`libc++abi.so.1`.** The prebuilt
  `desktop-head-unit` binary needs the host (not Android-target) build
  of libc++, which isn't a system package on a fresh box but *is*
  already bundled with the NDK the setup script installs:
  ```sh
  export LD_LIBRARY_PATH="$ANDROID_HOME/ndk/<version>/toolchains/llvm/prebuilt/linux-x86_64/lib/x86_64-unknown-linux-gnu:$ANDROID_HOME/extras/google/auto"
  ```
- **Needs `DISPLAY` set.** Fails with `SDL_CreateWindowRenderer failed`
  if `DISPLAY` isn't exported to match a running X server.
- **Reads stdin as an interactive command loop.** If stdin isn't a real
  terminal (e.g. launched from a script or backgrounded non-interactively),
  it hits EOF immediately and treats that as "quit" — exits instantly
  with code 0 and no error, easy to mistake for a connection failure.
  Keep stdin open on a FIFO in that situation:
  ```sh
  mkfifo dhu_stdin.fifo && exec 3<>dhu_stdin.fifo
  desktop-head-unit <&3 &
  ```
- **"Start head unit server" toggling on doesn't guarantee it's
  listening.** A first connection attempt can get `Connection refused`
  on port 5277 (visible in `adb logcat` as `adbd: failed to connect to
  socket 'tcp:5277'`) even though the developer-settings toggle looks
  enabled. Toggling it off and back on immediately before connecting
  fixes this.
- **A physically connected USB cable competes with DHU.** The phone's
  Android Auto app auto-detects the USB data connection itself and
  attempts a *separate*, real AOAP projection handoff over that cable,
  which times out after ~5s against a dev machine (`adb logcat` shows
  `GH.ConnLoggerV2` events ending in `FRAMER_READ_END_OF_STREAM_NO_DATA`
  → `USB_ISSUE_PROJECTION_NOT_STARTED`). This doesn't block DHU's own
  TCP-over-adb session, but it's confusing log noise — prefer wireless
  adb (below) so the cable isn't plugged in at all during a DHU session.

**Wireless adb**, so DHU testing doesn't need a cable connected:

```sh
# On the phone: Settings → Developer options → Wireless debugging →
# "Pair device with pairing code" shows an IP:port + 6-digit code.
adb pair <pairing-ip>:<pairing-port> <code>

# Then use the *main* Wireless debugging screen's IP:port (different
# from the pairing screen's) to connect:
adb connect <ip>:<port>
```

`adb forward` is per-transport, so target the wireless connection
explicitly if a USB session was ever forwarded too:
```sh
adb -s <ip>:<port> forward tcp:5277 tcp:5277
```
