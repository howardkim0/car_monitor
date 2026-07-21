package com.carmonitor.app

import android.content.Intent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf

@RunWith(RobolectricTestRunner::class)
class BootReceiverTest {

    @Test
    fun `ACTION_BOOT_COMPLETED starts ObdForegroundService`() {
        val context = RuntimeEnvironment.getApplication()

        BootReceiver().onReceive(context, Intent(Intent.ACTION_BOOT_COMPLETED))

        val started = shadowOf(context).peekNextStartedService()
        assertNotNull("boot completion should start the foreground service", started)
        assertEquals(ObdForegroundService::class.java.name, started!!.component!!.className)
    }

    @Test
    fun `an unrelated action does not start anything`() {
        val context = RuntimeEnvironment.getApplication()

        BootReceiver().onReceive(context, Intent("some.other.action"))

        assertNull(
            "only ACTION_BOOT_COMPLETED should start the service",
            shadowOf(context).peekNextStartedService()
        )
    }
}
