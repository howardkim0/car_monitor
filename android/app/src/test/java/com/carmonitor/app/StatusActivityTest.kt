package com.carmonitor.app

import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
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
}
