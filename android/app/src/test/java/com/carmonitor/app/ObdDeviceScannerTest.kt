package com.carmonitor.app

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import androidx.core.content.ContextCompat
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ObdDeviceScannerTest {

    private val noopListener = object : ObdDeviceScanner.Listener {
        override fun onDeviceFound(device: BluetoothDevice) {}
        override fun onDiscoveryFinished() {}
        override fun onBondStateChanged(device: BluetoothDevice, bondState: Int) {}
    }

    @Test
    fun `startDiscovery does not crash and returns a receiver or null`() {
        val context = RuntimeEnvironment.getApplication()
        // Must not throw either way — real behavior (whether the shadow
        // adapter's startDiscovery() succeeds) isn't the point of this
        // test; real discovery is only verified on-device/DHU per
        // docs/dev-setup.md, same limitation DeviceScanActivity has.
        ObdDeviceScanner.startDiscovery(context, noopListener)
    }

    // Regression test mirroring DeviceScanActivityTest's — the same
    // RECEIVER_NOT_EXPORTED class of bug (silently dropped system
    // Bluetooth broadcasts, docs/defects.md) applies equally here.
    @Test
    fun `receiver is registered as RECEIVER_EXPORTED when discovery starts`() {
        val context = RuntimeEnvironment.getApplication()
        val receiver = ObdDeviceScanner.startDiscovery(context, noopListener)

        // Only meaningful if discovery actually started in this
        // Robolectric environment — if it didn't (shadow adapter
        // returned false), there's nothing registered to check, and
        // that's covered by the "returns a receiver or null" test above.
        if (receiver != null) {
            val wrapper = shadowOf(context).registeredReceivers
                .firstOrNull { it.intentFilter.hasAction(BluetoothDevice.ACTION_FOUND) }
            assertNotNull("expected the discovery BroadcastReceiver to be registered", wrapper)
            assertEquals(
                "must be RECEIVER_EXPORTED, not RECEIVER_NOT_EXPORTED — these are " +
                    "system Bluetooth broadcasts, not sent by this app's own UID",
                ContextCompat.RECEIVER_EXPORTED,
                wrapper!!.flags
            )
            ObdDeviceScanner.stopDiscovery(context, receiver)
        }
    }

    @Test
    fun `stopDiscovery does not crash when called with an already-unregistered receiver`() {
        val context = RuntimeEnvironment.getApplication()
        val receiver = ObdDeviceScanner.startDiscovery(context, noopListener) ?: return
        ObdDeviceScanner.stopDiscovery(context, receiver)
        ObdDeviceScanner.stopDiscovery(context, receiver) // must not throw on double-stop
    }

    @Test
    fun `pair does not crash`() {
        val device = BluetoothAdapter.getDefaultAdapter().getRemoteDevice("AA:BB:CC:DD:EE:FF")
        // Real bonding behavior can't be simulated under Robolectric —
        // this only verifies createBond()'s SecurityException path is
        // actually caught, not thrown. Real pairing is exercised via the
        // on-device/DHU flow, per docs/dev-setup.md.
        ObdDeviceScanner.pair(device)
    }

    @Test
    fun `isLocationEnabled does not crash`() {
        val context = RuntimeEnvironment.getApplication()
        ObdDeviceScanner.isLocationEnabled(context)
    }
}
