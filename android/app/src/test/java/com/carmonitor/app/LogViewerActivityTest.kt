package com.carmonitor.app

import android.widget.Button
import android.widget.ScrollView
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
class LogViewerActivityTest {

    private val controllers = mutableListOf<ActivityController<LogViewerActivity>>()

    private fun newActivity(): LogViewerActivity {
        val controller = Robolectric.buildActivity(LogViewerActivity::class.java).create()
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
        assertNotNull("Should find logText view", activity.findViewById<TextView>(R.id.logText))
        assertNotNull("Should find refreshButton view", activity.findViewById<Button>(R.id.refreshButton))
        assertNotNull("Should find logScrollView", activity.findViewById<ScrollView>(R.id.logScrollView))
        assertNotNull("Should find truncationNotice view", activity.findViewById<TextView>(R.id.truncationNotice))
    }

    @Test
    fun `refresh button click does not crash when app log does not exist`() {
        val activity = newActivity()
        val button = activity.findViewById<Button>(R.id.refreshButton)
        button.performClick() // must not throw, even though app.log doesn't exist in Robolectric's filesDir
    }
}
