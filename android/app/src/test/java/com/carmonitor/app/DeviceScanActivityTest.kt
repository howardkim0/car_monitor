package com.carmonitor.app

import android.bluetooth.BluetoothDevice
import android.widget.Button
import android.widget.TextView
import androidx.core.content.ContextCompat
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config

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
}
