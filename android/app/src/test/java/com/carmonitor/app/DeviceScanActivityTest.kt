package com.carmonitor.app

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.content.Intent
import android.widget.Button
import android.widget.TextView
import androidx.core.content.ContextCompat
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config
import org.robolectric.shadows.ShadowToast

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class DeviceScanActivityTest {

    private val controllers = mutableListOf<ActivityController<DeviceScanActivity>>()

    private fun newActivity(): DeviceScanActivity {
        val controller = Robolectric.buildActivity(DeviceScanActivity::class.java).create()
        controllers.add(controller)
        return controller.get()
    }

    @After
    fun tearDown() {
        controllers.forEach { it.destroy() }
        controllers.clear()
    }

    @Test
    fun `activity creates without crashing and finds its views`() {
        val activity = newActivity()
        assertNotNull(activity.findViewById<Button>(R.id.scanButton))
        assertNotNull(activity.findViewById<TextView>(R.id.deviceScanStatusText))
    }

    @Test
    fun `scan button starts enabled and stays enabled`() {
        val activity = newActivity()
        val button = activity.findViewById<Button>(R.id.scanButton)
        assertTrue("scan button should never be disabled — it's a toggle now", button.isEnabled)
    }

    @Test
    fun `scan button click does not crash when Bluetooth adapter is unavailable`() {
        val activity = newActivity()
        val button = activity.findViewById<Button>(R.id.scanButton)
        button.performClick() // must not throw
    }

    @Test
    fun `Show More button exists`() {
        val activity = newActivity()
        assertNotNull(activity.findViewById<Button>(R.id.showMoreButton))
    }

    @Test
    fun `Show More button click does not crash with nothing discovered yet`() {
        val activity = newActivity()
        val button = activity.findViewById<Button>(R.id.showMoreButton)
        button.performClick() // must not throw
    }

    // Regression test: the discovery receiver was registered as
    // RECEIVER_NOT_EXPORTED, which silently drops broadcasts from
    // privileged system processes like the Bluetooth stack —
    // ACTION_FOUND/ACTION_DISCOVERY_FINISHED/ACTION_BOND_STATE_CHANGED
    // never reached this receiver, ever, regardless of how many
    // devices were actually discoverable nearby (see docs/defects.md).
    // Safe to export: all three are AOSP protected broadcasts only the
    // system can send.
    @Test
    fun `discovery receiver is registered as RECEIVER_EXPORTED`() {
        val activity = newActivity()

        val wrapper = shadowOf(activity.application).registeredReceivers
            .firstOrNull { it.intentFilter.hasAction(BluetoothDevice.ACTION_FOUND) }

        assertNotNull("expected the discovery BroadcastReceiver to be registered", wrapper)
        assertEquals(
            "must be RECEIVER_EXPORTED, not RECEIVER_NOT_EXPORTED — these are " +
                "system Bluetooth broadcasts, not sent by this app's own UID",
            ContextCompat.RECEIVER_EXPORTED,
            wrapper!!.flags
        )
    }

    @Test
    @Config(sdk = [34])
    fun `isLocationEnabled is always true on API 31+`() {
        val activity = newActivity()
        assertTrue(activity.isLocationEnabled())
    }

    @Test
    @Config(sdk = [29])
    fun `isLocationEnabled reflects LocationManager state on API 28-30`() {
        val activity = newActivity()
        val locationManager = activity.getSystemService(android.location.LocationManager::class.java)
        shadowOf(locationManager).setLocationEnabled(false)

        assertFalse(activity.isLocationEnabled())

        shadowOf(locationManager).setLocationEnabled(true)

        assertTrue(activity.isLocationEnabled())
    }

    private fun obdLookingDevice(address: String = "AA:BB:CC:DD:EE:FF"): BluetoothDevice {
        val device = BluetoothAdapter.getDefaultAdapter().getRemoteDevice(address)
        shadowOf(device).setName("ELM327")
        return device
    }

    // Robolectric's registered-receiver dispatch runs on the (paused-by-
    // default) main looper rather than synchronously inline with
    // sendBroadcast() — without draining it here, the receiver's UI-updating
    // code hasn't run yet by the time a test's assertions execute.
    private fun DeviceScanActivity.sendAndIdle(intent: Intent) {
        sendBroadcast(intent)
        shadowOf(android.os.Looper.getMainLooper()).idle()
    }

    @Test
    fun `ACTION_FOUND adds a non-bonded OBD-looking device as a row`() {
        val activity = newActivity()
        val device = obdLookingDevice()
        shadowOf(device).setBondState(BluetoothDevice.BOND_NONE)

        activity.sendAndIdle(
            Intent(BluetoothDevice.ACTION_FOUND).putExtra(BluetoothDevice.EXTRA_DEVICE, device)
        )

        val availableContainer = activity.findViewById<android.widget.LinearLayout>(R.id.availableDevicesContainer)
        assertEquals(1, availableContainer.childCount)
    }

    @Test
    fun `ACTION_FOUND ignores a device that is already bonded`() {
        val activity = newActivity()
        val device = obdLookingDevice()
        shadowOf(device).setBondState(BluetoothDevice.BOND_BONDED)

        activity.sendAndIdle(
            Intent(BluetoothDevice.ACTION_FOUND).putExtra(BluetoothDevice.EXTRA_DEVICE, device)
        )

        val availableContainer = activity.findViewById<android.widget.LinearLayout>(R.id.availableDevicesContainer)
        assertEquals(
            "an already-bonded device belongs in the paired list, not the available-to-pair list",
            0,
            availableContainer.childCount
        )
    }

    @Test
    fun `ACTION_FOUND does not add the same device address twice`() {
        val activity = newActivity()
        val device = obdLookingDevice()
        shadowOf(device).setBondState(BluetoothDevice.BOND_NONE)
        val foundIntent = Intent(BluetoothDevice.ACTION_FOUND).putExtra(BluetoothDevice.EXTRA_DEVICE, device)

        activity.sendAndIdle(foundIntent)
        activity.sendAndIdle(foundIntent)

        val availableContainer = activity.findViewById<android.widget.LinearLayout>(R.id.availableDevicesContainer)
        assertEquals(1, availableContainer.childCount)
    }

    @Test
    fun `ACTION_DISCOVERY_FINISHED resets the scan button and status text`() {
        val activity = newActivity()
        val scanButton = activity.findViewById<Button>(R.id.scanButton)
        scanButton.performClick() // start scanning, if the shadow adapter allows it

        activity.sendAndIdle(Intent(BluetoothAdapter.ACTION_DISCOVERY_FINISHED))

        assertEquals(activity.getString(R.string.device_scan_button), scanButton.text.toString())
    }

    @Test
    fun `ACTION_BOND_STATE_CHANGED to BOND_NONE shows a pairing-failed toast`() {
        val activity = newActivity()
        val device = obdLookingDevice("11:22:33:44:55:66")
        shadowOf(device).setBondState(BluetoothDevice.BOND_NONE)
        // Make this device the one currently being paired by discovering and tapping it.
        activity.sendAndIdle(Intent(BluetoothDevice.ACTION_FOUND).putExtra(BluetoothDevice.EXTRA_DEVICE, device))
        val availableContainer = activity.findViewById<android.widget.LinearLayout>(R.id.availableDevicesContainer)
        (availableContainer.getChildAt(0) as Button).performClick()

        activity.sendAndIdle(
            Intent(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
                .putExtra(BluetoothDevice.EXTRA_DEVICE, device)
                .putExtra(BluetoothDevice.EXTRA_BOND_STATE, BluetoothDevice.BOND_NONE)
        )

        assertEquals(
            activity.getString(R.string.device_pairing_failed),
            ShadowToast.getTextOfLatestToast()
        )
    }
}
