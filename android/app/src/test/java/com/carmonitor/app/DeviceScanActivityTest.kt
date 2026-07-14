package com.carmonitor.app

import android.widget.Button
import android.widget.TextView
import org.junit.After
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
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
}
