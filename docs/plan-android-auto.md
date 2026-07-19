# Plan: Android Auto Support

> Saved per `CLAUDE.md`'s "Planning docs go in docs/". Introduces a
> second UI surface (a car head unit screen) alongside the existing
> phone `StatusActivity`, via the AndroidX Car App Library
> (`androidx.car.app`).

## Goal

Expose 4 actions on the Android Auto screen, reusing the app's
existing button strings and color scheme rather than inventing new
UI text/colors:

1. **Start Scanning** (`R.string.start_scanning_button`)
2. **Pair Scanner** (`R.string.pair_devices_button`)
3. **Display Logs** (`R.string.view_logs_button`)
4. **Quit App** (`R.string.quit_app_button`) — with a confirmation
   step before it actually kills the process (added per user
   feedback: a single accidental tap while driving shouldn't kill
   the app).

## Why a car screen can't just reuse the existing Activities

Android Auto (phone-projection) renders only the Car App Library's
own templates (`ListTemplate`, `MessageTemplate`, etc.) on the head
unit — a `Screen` cannot host an arbitrary phone `Activity`. So
`DeviceScanActivity`'s full BLE-discovery/pairing flow and
`LogViewerActivity`'s scrollable log view can't be shown as-is; the
car screens need their own lighter equivalents that call into the
same underlying logic (`ObdForegroundService`, `LogViewer`,
`RememberedDevices`, `DeviceNameFilter`, `Mobile.*`) rather than
duplicating it.

This is phone-projected Android Auto only, not Android Automotive OS
(car-native) — `CarAppService`/`Session`/`Screen` still run in the
phone's own process, so nothing about Bluetooth I/O, the foreground
service, or storage changes; the car screen is purely an additional
UI surface, same architecture as `DeviceScanActivity`/
`LogViewerActivity` today (DESIGN.md §3: "framework-only concerns...
stay Kotlin-only").

## Design decisions and why

**`ListTemplate`, not `GridTemplate`, for the 4-action root menu.**
`GridTemplate`'s `GridItem` requires an image for every item — 3 new
icon assets for no benefit on a menu this small. `ListTemplate` rows
need no image, and still support a tinted icon on individual rows via
`CarIcon.Builder(icon).setTint(CarColor)` — so nothing is lost.

**Color scheme: reuse what's actually there, don't invent new
colors.** The phone screen today only tints two buttons — Stop
(`#B3261E`, red) and Quit (`#3A3A3A`, dark gray); every other button,
including "Start Scanning"'s toggle state, "Pair Bluetooth OBD2
Scanners", and "View App Logs", uses the plain default Material
button color. The honest carryover is: **Quit App's row gets
`#3A3A3A`** (reusing the already-existing `ic_quit.xml` notification
icon, tinted via `CarColor.createCustom(0xFF3A3A3A.toInt(),
0xFF3A3A3A.toInt())`); the other 3 rows stay untinted, matching that
they have no special color on the phone either.

**Quit App requires a confirmation screen.** Tapping "Quit App"
pushes a `QuitConfirmationScreen` (`MessageTemplate`, title asking to
confirm, two actions: "Quit" — `FLAG_PRIMARY`, tinted `#3A3A3A`,
calls `AppQuit.quit(carContext)` — and "Cancel" — pops back to
`MainCarScreen`). This is the standard Car App Library confirmation
pattern (a second `Screen`, not a native `AlertDialog`, which
`Screen`s can't show) and mirrors why Stop/Start already needs an
explicit tap on the phone (DESIGN.md §7: "resuming after a stop is
always explicit").

**Pair Scanner is bonded-devices-only, not full discovery.** Full BLE
discovery + pairing (`DeviceScanActivity`'s flow: scan → wait → tap
unpaired device → confirm system pairing dialog) is a multi-step,
attention-heavy flow that both fights Android's Driver Distraction
Guidelines for in-car UI and can't be hosted by a `Screen` anyway.
`PairScannerScreen` lists only already-bonded devices (same filter
`StatusActivity.showPairedDevicesDialog()` already applies) — good
enough to switch dongles mid-trip without exposing a discovery flow
behind the wheel. Full discovery-on-car-screen is filed as an open
question (see `docs/open-questions.md` update below), not built.

**Display Logs shows a short tail, not the full file.**
`LongMessageTemplate` (added for exactly this — a longer block of
scrollable text with a title + actions) is the right fit, still with
a host-enforced length cap tighter than the phone's scrollable
`LogViewerActivity`. Uses the existing `LogViewer.readTail()` with a
much smaller `maxBytes` (~4000, versus the phone's 200KB), further
trimmed to roughly the last 15 lines. One "Refresh" action re-reads,
mirroring `LogViewerActivity`'s existing Refresh button.

**`HostValidator`: permissive for debug, the library's own allowlist
for release.** `androidx.car.app:app` ships a bundled "known Android
Auto/Automotive hosts" resource for exactly this — no hand-maintained
signature list:
```kotlin
override fun createHostValidator(): HostValidator =
    if (carContext.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0) {
        HostValidator.ALLOW_ALL_HOSTS_VALIDATOR
    } else {
        HostValidator.Builder(carContext)
            .addAllowedHosts(androidx.car.app.R.array.hosts_allowlist_sample)
            .build()
    }
```
Verify the exact resource name against whatever version gets pinned.

**Manifest category: `androidx.car.app.category.IOT`.** Not
navigation/parking/charging — this is a connected-device monitoring
app, which is what the IOT category is for.

**Permissions must be requestable from the car screen itself.**
`StatusActivity.startScanning()` always calls
`requestPermissions.launch(requiredPermissions())` before binding —
`ActivityResultContracts` isn't available to a `Screen`. On a fresh
install used from the car before the phone app has ever been opened,
Bluetooth/notification permissions may not exist yet, and
`ObdForegroundService.start()` alone gives the driver no way to grant
them. `MainCarScreen`'s "Start Scanning" row must check permission
state first and, if anything is missing, call the Car App Library's
own `CarContext.requestPermissions(permissions) { approved, rejected
-> ... }` (the car-screen equivalent of
`ActivityResultContracts.RequestMultiplePermissions`) before calling
`ObdForegroundService.start(carContext)`. `StatusActivity`'s private
`requiredPermissions()` (combines `BluetoothPermissions.forConnect()`
+ conditional `POST_NOTIFICATIONS`) gets extracted into
`BluetoothPermissions.forServiceStart()` so both surfaces request the
exact same set — same "one implementation" reasoning as the 3 shared
objects below, and reuses the existing `BluetoothPermissions` object
(already factored out for this purpose per its own doc comment)
rather than adding a 4th new shared object. Whether granted or
rejected, `MainCarScreen` calls `ObdForegroundService.start(carContext)`
afterward regardless — matching `StatusActivity`'s existing behavior
(`requestPermissions`'s callback in `StatusActivity.kt` starts the
service either way; the service itself polls its own permission state
and surfaces `ConnectionState.PermissionMissing` rather than refusing
to start). The car screen doesn't need a different policy here, just
the same one reached through `CarContext.requestPermissions()`
instead of `ActivityResultContracts`.

## Extraction step (do this first, before any car code)

`StatusActivity`'s relevant logic is currently private on an
`AppCompatActivity` — unreachable from a `Screen`. Pull it into 3
shared objects, same "one implementation, not two that can drift"
pattern DESIGN.md §4 step 6 already uses for `AnomalyNotifications`:

- **`MonitoringPrefs`** (new object) — `PREFS_NAME`/
  `PREF_STOPPED_BY_USER` and `isStoppedByUser(context)`/
  `setStoppedByUser(context, value)`, replacing `StatusActivity`'s
  private `loadStoppedByUser()`/`saveStoppedByUser()`.
- **`AppQuit`** (new object) — `quit(context: Context, kill: () ->
  Unit = { Process.killProcess(Process.myPid()) })`:
  `MonitoringPrefs.setStoppedByUser(context, true)`,
  `ObdForegroundService.quit(context)`, then `kill()`. The kill step
  must be an injectable parameter, not a hardcoded call — calling
  `quit()` as a single function that always ends in
  `Process.killProcess()` would kill the test JVM, making the "two
  steps before it are testable" claim from the original draft false
  as stated. With `kill` injected, `AppQuitTest` passes a no-op lambda
  and asserts `MonitoringPrefs`/`ObdForegroundService.quit()` were
  called *and* that `kill` itself was invoked exactly once — real
  callers (`StatusActivity`, `MainCarScreen`'s confirmation flow) just
  use the default. Also note (per the Architect review): a Car App
  Library host may hold this process via a live AIDL binding when
  `Process.killProcess()` runs — same category of not-worth-automating
  risk as the existing `ACTION_QUIT` carve-out (DESIGN.md §10), so
  this path gets verified manually via DHU, not by an automated test,
  same as today.
- **`ObdDeviceLister`** (new object, *no coroutine scope of its
  own*) — `listCandidates(context, filesDir): List<CandidateDevice>`
  (data class: mac, name, status) extracts `showPairedDevicesDialog()`'s
  bonded-device fetch + `DeviceNameFilter`/`RememberedDevices` filter +
  status labeling, **including its existing `try/catch
  (SecurityException)` → empty list** (today's lines 464–474) carried
  over verbatim — a revoked-permission throw here must not escape
  into a `Screen.onGetTemplate()` and crash template rendering; the
  empty-list path renders via `ItemList.setNoItemsMessage(...)` same
  as a genuinely-empty bonded list. `select(context, filesDir, mac,
  name, onReconnect: () -> Unit)` extracts `selectDevice()`'s body as
  a **suspend function**, not a fire-and-forget launch — the object
  itself owns no scope, so it can't leak into a destroyed caller.
  Callers launch it on their *own* lifecycle-scoped coroutine:
  `StatusActivity` keeps using its existing `scope` (cancelled in
  `onDestroy()`); `PairScannerScreen` (a `LifecycleOwner`) launches on
  its own `lifecycleScope`, which the Car App Library cancels when the
  `Screen` is destroyed/popped — mirroring exactly why
  `StatusActivity.onDestroy()` cancels its own `scope` today (see that
  method's comment), just via the `Screen`-native mechanism instead of
  a hand-rolled one.

`StatusActivity` is edited to delegate to these 3 objects instead of
its private methods — mechanical, low-risk, and lands as its own step
with `StatusActivityTest` re-run to confirm no behavior change (it
already asserts on `stoppedByUser`/bind behavior).

## New files

**Package structure** (per Architect review): the flat
`com.carmonitor.app` package already holds ~10 files; adding all 8 new
ones there would roughly double it, and 6 of them are Car App
Library-specific — a cohesive unit unlike anything else in the
package. The 3 shared objects stay flat (alongside
`AnomalyNotifications`/`RememberedDevices`, which they match in
shape); the 6 Car App Library classes go in a new `carapp`
subpackage:

- `android/app/src/main/java/com/carmonitor/app/MonitoringPrefs.kt`
- `android/app/src/main/java/com/carmonitor/app/AppQuit.kt`
- `android/app/src/main/java/com/carmonitor/app/ObdDeviceLister.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/CarMonitorCarAppService.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/CarMonitorSession.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/MainCarScreen.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/QuitConfirmationScreen.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/PairScannerScreen.kt`
- `android/app/src/main/java/com/carmonitor/app/carapp/LogsScreen.kt`
- `android/app/src/main/res/xml/automotive_app_desc.xml`
- Matching test files under `android/app/src/test/java/com/carmonitor/app/`
  (flat, mirroring main-source layout) and
  `android/app/src/test/java/com/carmonitor/app/carapp/`:
  `MonitoringPrefsTest.kt`, `AppQuitTest.kt`, `ObdDeviceListerTest.kt`,
  `MainCarScreenTest.kt`, `QuitConfirmationScreenTest.kt`,
  `PairScannerScreenTest.kt`, `LogsScreenTest.kt`

The manifest's `android:name=".CarMonitorCarAppService"` shorthand
needs the full package
(`.carapp.CarMonitorCarAppService`) once it moves.

## Edited files

- `android/app/build.gradle.kts` — add, following the existing
  bare-string-literal dependency convention (no version catalog in
  this project):
  ```kotlin
  implementation("androidx.car.app:app:1.8.0")
  testImplementation("androidx.car.app:app-testing:1.8.0")
  ```
  (Verified against Google's Maven index at planning time — latest
  stable, non-alpha/beta/rc release of both artifacts.)
- `android/app/src/main/AndroidManifest.xml`:
  ```xml
  <meta-data
      android:name="com.google.android.gms.car.application"
      android:resource="@xml/automotive_app_desc" />

  <service
      android:name=".carapp.CarMonitorCarAppService"
      android:exported="true"
      android:foregroundServiceType="connectedDevice">
      <intent-filter>
          <action android:name="androidx.car.app.CarAppService" />
          <category android:name="androidx.car.app.category.IOT" />
      </intent-filter>
  </service>
  ```
- `android/app/src/main/java/com/carmonitor/app/BluetoothPermissions.kt` —
  add `forServiceStart()`, extracting `StatusActivity`'s private
  `requiredPermissions()` (currently `BluetoothPermissions.forConnect()`
  + conditional `POST_NOTIFICATIONS`) so `MainCarScreen`'s permission
  check requests the identical set.
- `android/app/src/main/java/com/carmonitor/app/StatusActivity.kt` —
  delegate to `MonitoringPrefs`/`AppQuit`/`ObdDeviceLister`/
  `BluetoothPermissions.forServiceStart()`.
- `DESIGN.md` — new section describing the car entry point: the
  `CarAppService`/`Session`/`Screen` classes, why `ObdForegroundService`/
  Bluetooth/`Mobile.*` are untouched (car screens are just another UI
  surface in the same process), why the shared objects exist, and the
  bonded-devices-only / quit-confirmation scope decisions and why.
  Needs an Architect-persona pass per `CLAUDE.md` before committing.
- `docs/dev-setup.md` — new "Testing on Android Auto" subsection:
  Desktop Head Unit (DHU) setup, since this is Android Auto
  phone-projection, not Android Automotive OS (the Studio car
  emulator targets the wrong thing). Install via SDK Manager → SDK
  Tools → "Android Auto Desktop Head Unit emulator"; enable Android
  Auto's head-unit-server/developer mode on the connected phone;
  `adb forward tcp:5277 tcp:5277`; run `desktop-head-unit`.
- `docs/open-questions.md` — new entries, each mirrored as a filed
  GitHub issue per existing convention:
  - Tightening `HostValidator` before any real release (currently
    debug-permissive/release-allowlist as designed above, but the
    allowlist resource name should be verified against the pinned
    library version, not assumed).
  - Whether bonded-devices-only pairing on the car screen should ever
    grow to include full discovery.

## Implementation order

1. Extract `MonitoringPrefs`, `AppQuit` (injectable `kill`),
   `ObdDeviceLister` (suspend `select()`, no owned scope),
   `BluetoothPermissions.forServiceStart()`; edit `StatusActivity` to
   use all 4; re-run `StatusActivityTest`.
2. Add the `androidx.car.app` dependencies; add the manifest
   service/meta-data/`automotive_app_desc.xml`.
3. `carapp/CarMonitorCarAppService` + `carapp/CarMonitorSession` (thin
   — no logic to speak of).
4. `carapp/MainCarScreen` (the 4-row root list, including the
   `CarContext.requestPermissions()` check before "Start Scanning")
   + `carapp/QuitConfirmationScreen`.
5. `carapp/PairScannerScreen` (using `lifecycleScope` for
   `ObdDeviceLister.select()`).
6. `carapp/LogsScreen`.
7. Tests for every new class (table above).
8. `DESIGN.md` (Architect-reviewed), `docs/dev-setup.md`,
   `docs/open-questions.md` + linked GitHub issues.

## Tests

Robolectric + `androidx.car.app:app-testing`'s `TestCarContext`/
`ScreenController` (verify exact current method names against the
resolved artifact once the dependency is added — the harness API has
shifted across library versions, so budget spike time here before
trusting the estimate), MockK for `ObdForegroundService`/`Mobile.*`
collaborators, matching existing `StatusActivityTest` conventions.
Each new `Screen` test asserts: the rendered template's row
titles/order match the string resources (not literal strings, so a
copy change can't silently drift the test out of sync), and that
tapping each row invokes the expected mocked side effect.
`QuitConfirmationScreenTest` specifically asserts "Cancel" calls
`screenManager.pop()` without calling `AppQuit.quit()`, and "Quit"
calls `AppQuit.quit()` with a mocked `kill` lambda (never the real
`Process.killProcess`) and asserts it was invoked exactly once.
`ObdDeviceListerTest` covers the `SecurityException` → empty-list
path explicitly, mirroring `showPairedDevicesDialog()`'s existing
behavior. `AppQuitTest` asserts step order (prefs set, then service
quit, then `kill()`) using a mocked `kill`.
