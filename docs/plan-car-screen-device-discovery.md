# Allow pairing non-bonded devices from the Android Auto car screen

## Context

`PairScannerScreen` (the car screen's device picker) currently lists
already-*bonded* devices only, via `ObdDeviceLister.listCandidates()`.
This was a deliberate v1 choice (DESIGN.md section 11,
`docs/plan-android-auto.md`, and open GitHub issue #14), justified by
two stated reasons: (a) a `Screen` can't launch `DeviceScanActivity`
(the phone's full BLE discovery/pairing `Activity`), and (b) driver
distraction guidelines.

Research against the actually-pinned `androidx.car.app:app:1.7.0`
(decompiled and checked directly, not assumed) shows reason (a) is
real but **narrower than DESIGN.md currently implies**: a `Screen`
genuinely can't launch an external `Activity`, but BLE *discovery*
itself (scanning, listing results) isn't forbidden and doesn't require
one — `ListTemplate` + `Template.Builder.setLoading()` +
`Screen.invalidate()` (already used by `LogsScreen`'s Refresh action)
are enough to build a live-updating discovery list natively inside a
`Screen`. The genuine hard blocker is narrower: `BluetoothDevice.createBond()`'s
system pairing-confirmation UI, if a device needs one, is OS-level UI
that renders on the **phone**, not the car display — a driver who
can't glance at their phone could get stuck mid-pairing for such a
device.

**Decisions confirmed with the user:**
- Attempt pairing directly from the car screen (`createBond()`, same
  as the phone flow) and accept that risk, rather than restricting the
  car screen to discovery-only. In practice this is expected to work
  cleanly for the headless OBD2 dongles this app targets (the phone's
  own `DeviceScanActivity` already pairs them via plain `createBond()`
  with no PIN-entry UI of its own). A pairing attempt that doesn't
  complete gets an explicit timeout + failure message rather than
  hanging silently.
- No `ParkedOnlyOnClickListener` gating on the scan/pair actions —
  they stay tappable while driving, same as the rest of this screen
  today. This is a deliberate, explicit trade-off against the
  "driver distraction guidelines" reasoning DESIGN.md currently cites,
  and the DESIGN.md update must say so plainly rather than silently
  drop that reasoning.

## Design

**New shared component**: `ObdDeviceScanner` (flat `com.carmonitor.app`
package, alongside `ObdDeviceLister` — mirrors its naming and shared-object
precedent from DESIGN.md section 11's "shared logic, not duplicated logic").
A `Context`-parameterized, callback-based wrapper around the discovery +
pairing primitives `DeviceScanActivity` already uses inline
(`adapter.startDiscovery()`, a `BroadcastReceiver` for `ACTION_FOUND`/
`ACTION_DISCOVERY_FINISHED`/`ACTION_BOND_STATE_CHANGED` registered with
`ContextCompat.registerReceiver(..., RECEIVER_EXPORTED)` — see
`docs/defects.md` for why that flag matters — and `device.createBond()`),
so both an `Activity` `Context` and a `CarContext` (which extends
`ContextWrapper`) can use it identically:

```kotlin
object ObdDeviceScanner {
    interface Listener {
        fun onDeviceFound(device: BluetoothDevice)
        fun onDiscoveryFinished()
        fun onBondStateChanged(device: BluetoothDevice, bondState: Int)
    }
    fun startDiscovery(context: Context, listener: Listener): BroadcastReceiver
    fun stopDiscovery(context: Context, receiver: BroadcastReceiver)
    fun pair(context: Context, device: BluetoothDevice)
}
```

**Scope decision**: only `PairScannerScreen` consumes `ObdDeviceScanner`
for now. `DeviceScanActivity` keeps its existing, already-tested inline
implementation untouched — migrating it onto the shared object too
would be a reasonable future cleanup (worth a `docs/open-questions.md`
entry) but isn't required for this feature and adds regression risk to
working, tested code for no behavioral gain right now.

**`PairScannerScreen` changes**:
- A "Scan for Devices" row/action at the top of the template. Tapping
  it requests `BluetoothPermissions.forScan()` via
  `CarContext.requestPermissions(...)` (same pattern `MainCarScreen`
  already uses for its own permission-gated action), then calls
  `ObdDeviceScanner.startDiscovery(carContext, listener)`.
- While scanning: `setLoading(true)` on the template; each
  `onDeviceFound` callback appends the device (skip already-bonded,
  same `DeviceNameFilter.looksLikeObd2Scanner` filter
  `ObdDeviceLister`/`DeviceScanActivity` already apply) and calls
  `invalidate()` to re-render — mirroring `LogsScreen`'s Refresh→invalidate
  pattern.
- Tapping a discovered (unpaired) row calls
  `ObdDeviceScanner.pair(carContext, device)`, shows a "Pairing…"
  state, and starts a timeout (`lifecycleScope`, matching
  `ObdDeviceLister.select()`'s existing "car Screen's own
  `lifecycleScope`, cancelled automatically when the Screen is
  destroyed/popped" pattern) — e.g. 20s. `onBondStateChanged(BOND_BONDED)`
  before the timeout: persist via the same selection path
  `onSelect()` already uses, `CarToast` success, pop back.
  `BOND_NONE` or the timeout firing first: `CarToast` failure
  ("Pairing failed — try from the phone" for the timeout case
  specifically, since that's the scenario where a phone-side
  confirmation may be waiting), leave the row tappable again.
- Stop discovery (`ObdDeviceScanner.stopDiscovery`) when the `Screen`
  is destroyed/popped, mirroring `DeviceScanActivity.onDestroy()`'s
  cleanup — via a lifecycle observer on `screen.lifecycle`.
- Pre-31 Location Services check (`LocationManager.isLocationEnabled()`,
  same as `DeviceScanActivity`) before starting discovery; surface via
  `CarToast` if off, matching how permission/location gating already
  works elsewhere in `carapp/`.

**Files touched**: new `ObdDeviceScanner.kt` + `ObdDeviceScannerTest.kt`
(Robolectric, same style as `DeviceScanActivityTest.kt` — registration/
`RECEIVER_EXPORTED` regression coverage, dedup/filter logic; real
discovery/bond-state broadcasts can't be simulated under Robolectric,
same limitation `DeviceScanActivity` already has); modified
`PairScannerScreen.kt` + `PairScannerScreenTest.kt` (extend existing
`mockkObject(ObdDeviceLister)` pattern to also mock `ObdDeviceScanner`).
No `go/` changes.

**Process**: per `CLAUDE.md`'s design-first workflow — (1) update
DESIGN.md section 11's `PairScannerScreen` passage to describe the new
discovery+pairing capability, the narrower/corrected technical
rationale (discovery is buildable in a `Screen`; pairing's system-dialog
risk is the real remaining blocker), and the deliberate
no-parked-gating trade-off; (2) Architect-persona pass on that edit;
(3) commit + push the design doc (pre-authorized per that workflow);
(4) implement; (5) two-persona review + commit + push. Save this plan
to `docs/plan-car-screen-device-discovery.md` once approved. Once
implemented, close GitHub issue #14 and remove its
`docs/open-questions.md` entry (same pattern already used for issue
#15 earlier this session) — plus add a new open-questions.md entry for
"unify `DeviceScanActivity` onto the shared `ObdDeviceScanner`" as the
deferred cleanup noted above.

## Verification

- `./gradlew testDebugUnitTest` — new `ObdDeviceScannerTest` and
  updated `PairScannerScreenTest` pass; full suite stays green.
- Real discovery/pairing end-to-end can't be verified in this dev
  environment — no Bluetooth OBD2 dongle (or any unpaired Bluetooth
  device) is available here to actually discover/pair, same gap
  already noted for `obd2.InitCommands()` in `docs/open-questions.md`.
  What *is* verifiable here: DHU-based confirmation that the new "Scan
  for Devices" row renders, tapping it transitions to a loading state
  without crashing, and permission-request wiring fires correctly.
  Full real-hardware pairing verification is a known gap worth flagging
  back to the user rather than claiming false confidence in.
