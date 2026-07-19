package com.carmonitor.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class MonitoringPrefsTest {

    @Test
    fun `defaults to not stopped by user`() {
        val context = RuntimeEnvironment.getApplication()
        assertFalse(MonitoringPrefs.isStoppedByUser(context))
    }

    @Test
    fun `setStoppedByUser persists the value`() {
        val context = RuntimeEnvironment.getApplication()

        MonitoringPrefs.setStoppedByUser(context, true)
        assertTrue(MonitoringPrefs.isStoppedByUser(context))

        MonitoringPrefs.setStoppedByUser(context, false)
        assertFalse(MonitoringPrefs.isStoppedByUser(context))
    }
}
