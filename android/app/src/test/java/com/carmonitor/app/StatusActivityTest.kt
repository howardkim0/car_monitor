package com.carmonitor.app

import android.app.NotificationManager
import android.content.Context
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config

/**
 * Regression tests for the bound-service-survives-stopSelf() bug: a
 * Service stays alive as long as it's started OR bound, so a stop request
 * that doesn't first unbind the client leaves the service running behind
 * a UI that already claims it stopped. See ObdForegroundServiceTest for
 * the service-side half of the same fix (ACTION_STOP cancelling
 * connectionJob synchronously).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class StatusActivityTest {

    private val controllers = mutableListOf<ActivityController<StatusActivity>>()

    private fun newActivity(): StatusActivity {
        val controller = Robolectric.buildActivity(StatusActivity::class.java).create().start()
        controllers.add(controller)
        return controller.get()
    }

    @After
    fun tearDown() {
        controllers.forEach { it.destroy() }
        controllers.clear()
    }

    @Test
    fun `a terminal Stopped state unbinds an activity that was bound`() {
        val activity = newActivity()
        // onStart() (called by .start() above) already attempted a real
        // bindService() — Robolectric auto-connects local binds by
        // default, so the activity should already be bound at this point.
        assertTrue("expected onStart() to have bound to the service", activity.isBound)

        activity.onStateChanged(ObdForegroundService.ConnectionState.Stopped)

        assertFalse("a terminal state must unbind, not just update UI text", activity.isBound)
        assertTrue(
            "expected an actual unbindService() call, not just the local flag flipping",
            shadowOf(activity.application).unboundServiceConnections.isNotEmpty()
        )
    }

    @Test
    fun `a terminal TimedOut state also unbinds`() {
        val activity = newActivity()
        assertTrue(activity.isBound)

        activity.onStateChanged(ObdForegroundService.ConnectionState.TimedOut)

        assertFalse(activity.isBound)
    }

    @Test
    fun `applyStoppedUi does not attempt a second unbind when already unbound`() {
        val activity = newActivity()
        activity.onStateChanged(ObdForegroundService.ConnectionState.Stopped)
        assertFalse(activity.isBound)

        // Must not throw (e.g. "Service not registered") from double-
        // unbinding — TimedOut/Stopped could plausibly race with a
        // user-tapped Stop in a real app.
        activity.onStateChanged(ObdForegroundService.ConnectionState.TimedOut)

        assertFalse(activity.isBound)
    }

    @Test
    fun `destroying the activity cancels the export coroutine scope`() {
        // Regression test: exportLogs() runs on a manually-created
        // CoroutineScope that touches the UI (Toast, startActivity) when
        // it completes. Before this fix, that scope was never cancelled,
        // so an export still in flight when the Activity is destroyed
        // (e.g. the user backs out mid-export) could reach into a dead
        // Activity. onDestroy() must cancel it.
        // Not added to `controllers` — destroyed directly below, and
        // tearDown() double-destroying it crashes Robolectric's fragment
        // teardown with an unrelated NPE.
        val controller = Robolectric.buildActivity(StatusActivity::class.java).create().start()
        val activity = controller.get()

        controller.destroy()

        assertFalse("expected the export coroutine scope to be cancelled on destroy", activity.exportScopeIsActive())
    }

    @Test
    fun `Test Alert button posts a notification even when stopped by user`() {
        // Regression test: the Test Alert button (per DESIGN.md section 4
        // step 6) must work independently of whether the service is running
        // or was explicitly stopped by the user. This proves the feature's
        // entire design decision holds: routing it through AnomalyNotifications
        // instead of the Service means it works regardless of service state.
        val app = RuntimeEnvironment.getApplication()

        // Set up the activity in "stopped by user" state so it doesn't bind
        // to or start ObdForegroundService
        val prefs = app.getSharedPreferences("car_monitor_prefs", Context.MODE_PRIVATE)
        prefs.edit().putBoolean("stoppedByUser", true).apply()

        val activity = newActivity()
        assertFalse("activity in stopped-by-user state should not be bound", activity.isBound)

        // Tap the Test Alert button
        activity.findViewById<android.widget.Button>(R.id.testAlertButton).performClick()

        // Assert a notification was posted on the anomaly channel
        val manager = app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val testAlertMetricName = app.getString(R.string.test_alert_metric_name)
        val wantId = 1000 + (testAlertMetricName.hashCode() and 0xFF)
        val posted = manager.activeNotifications
            .firstOrNull { it.id == wantId }
            ?.notification

        assertNotNull(
            "Test Alert button must post a notification even when stopped by user",
            posted
        )
        assertEquals(AnomalyNotifications.CHANNEL_ID, posted!!.channelId)
    }
}
