package com.carmonitor.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class RememberedDevicesTest {

    @Test
    fun `a device is not remembered until remember() is called`() {
        val context = RuntimeEnvironment.getApplication()
        assertFalse(RememberedDevices.isRemembered(context, "00:1D:A5:68:98:8A"))
    }

    @Test
    fun `remember() makes isRemembered() true for that MAC`() {
        val context = RuntimeEnvironment.getApplication()
        RememberedDevices.remember(context, "00:1D:A5:68:98:8A")
        assertTrue(RememberedDevices.isRemembered(context, "00:1D:A5:68:98:8A"))
    }

    @Test
    fun `MAC matching is case-insensitive`() {
        val context = RuntimeEnvironment.getApplication()
        RememberedDevices.remember(context, "00:1d:a5:68:98:8a")
        assertTrue(RememberedDevices.isRemembered(context, "00:1D:A5:68:98:8A"))
    }

    @Test
    fun `remembering one device does not affect another`() {
        val context = RuntimeEnvironment.getApplication()
        RememberedDevices.remember(context, "00:1D:A5:68:98:8A")
        assertFalse(RememberedDevices.isRemembered(context, "11:22:33:44:55:66"))
    }

    @Test
    fun `remembering multiple devices keeps all of them`() {
        val context = RuntimeEnvironment.getApplication()
        RememberedDevices.remember(context, "00:1D:A5:68:98:8A")
        RememberedDevices.remember(context, "11:22:33:44:55:66")

        assertTrue(RememberedDevices.isRemembered(context, "00:1D:A5:68:98:8A"))
        assertTrue(RememberedDevices.isRemembered(context, "11:22:33:44:55:66"))
    }

    @Test
    fun `remembering the same device twice does not throw`() {
        val context = RuntimeEnvironment.getApplication()
        RememberedDevices.remember(context, "00:1D:A5:68:98:8A")
        RememberedDevices.remember(context, "00:1D:A5:68:98:8A") // must not throw

        assertTrue(RememberedDevices.isRemembered(context, "00:1D:A5:68:98:8A"))
    }
}
