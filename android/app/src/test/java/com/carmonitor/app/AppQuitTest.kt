package com.carmonitor.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf

@RunWith(RobolectricTestRunner::class)
class AppQuitTest {

    @Test
    fun `quit sets stoppedByUser, stops the service, then invokes kill`() {
        val context = RuntimeEnvironment.getApplication()
        var killed = false

        AppQuit.quit(context) { killed = true }

        assertTrue("expected stoppedByUser to be persisted", MonitoringPrefs.isStoppedByUser(context))
        val startedIntent = shadowOf(context).nextStartedService
        assertEquals(ObdForegroundService.ACTION_QUIT, startedIntent?.action)
        assertTrue("expected the injected kill callback to run", killed)
    }
}
