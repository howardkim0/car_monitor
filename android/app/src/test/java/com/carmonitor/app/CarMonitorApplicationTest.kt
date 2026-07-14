package com.carmonitor.app

import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * Regression test for the app log being a silent no-op before
 * ObdForegroundService.onCreate() ran — which never happens at all if
 * the user previously tapped Stop (see docs/defects.md). Moving
 * Mobile.initAppLog() to CarMonitorApplication.onCreate() means it's
 * always ready before any Activity or Service exists.
 *
 * This only checks the manifest wiring, not that a log line actually
 * lands in app.log — that's untestable under Robolectric, which has no
 * native libgojni.so to load at all, so every Mobile.* call in this
 * codebase is fire-and-forget by design (DESIGN.md section 6.2). The
 * underlying init/no-op-before-init behavior is already covered at
 * 100% by go/mobile/applog_test.go; what's new here is only that
 * CarMonitorApplication is the thing calling it, before anything else
 * in the app runs.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CarMonitorApplicationTest {

    @Test
    fun `the manifest-registered Application is CarMonitorApplication`() {
        assertTrue(RuntimeEnvironment.getApplication() is CarMonitorApplication)
    }
}
