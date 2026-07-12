package com.carmonitor.app

import android.bluetooth.BluetoothSocket
import android.content.Intent
import io.mockk.every
import io.mockk.mockk
import io.mockk.verify
import java.io.IOException
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.android.controller.ServiceController
import org.robolectric.annotation.Config

/**
 * Regression tests for bugs actually found and fixed this session (see
 * DESIGN.md section 13 and CLAUDE.md's "every caught bug gets a
 * regression test") — not a coverage-chasing exercise.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ObdForegroundServiceTest {

    // Tracked so tearDown() can destroy() every service created via
    // newService(), which cancels connectionLoop()'s coroutine scope —
    // otherwise a real background Job (Dispatchers.IO, real delays) from
    // one test keeps running into the next, both wasting time and risking
    // cross-test interference within the same test JVM.
    private val controllers = mutableListOf<ServiceController<ObdForegroundService>>()

    private fun newService(): ObdForegroundService {
        val controller = Robolectric.buildService(ObdForegroundService::class.java).create()
        controllers.add(controller)
        return controller.get()
    }

    @After
    fun tearDown() {
        controllers.forEach { it.destroy() }
        controllers.clear()
    }

    // A failed connect() must close the socket rather than leak it — the
    // first bug caught in this session's review, before this was extracted
    // into connectSocket() to make it directly testable.
    @Test
    fun `connectSocket closes the socket when connect throws`() {
        val service = newService()
        val socket = mockk<BluetoothSocket>(relaxed = true)
        every { socket.connect() } throws IOException("connect failed")

        try {
            service.connectSocket(socket)
            fail("expected the IOException from connect() to propagate")
        } catch (e: IOException) {
            // expected — connectSocket() must still rethrow after closing
        }

        verify(exactly = 1) { socket.close() }
    }

    @Test
    fun `connectSocket does not close the socket on a successful connect`() {
        val service = newService()
        val socket = mockk<BluetoothSocket>(relaxed = true)
        every { socket.connect() } returns Unit

        service.connectSocket(socket)

        verify(exactly = 0) { socket.close() }
    }

    // A second onStartCommand() while a connectionLoop() is still active
    // must not launch a second concurrent Job — the rotation-race bug
    // (StatusActivity re-requesting permissions after a screen rotation
    // used to launch a duplicate Bluetooth session).
    @Test
    fun `onStartCommand does not launch a second connectionLoop while one is active`() {
        val service = newService()
        val startIntent = Intent(RuntimeEnvironment.getApplication(), ObdForegroundService::class.java)

        service.onStartCommand(startIntent, 0, 1)
        val firstJob = service.connectionJob
        assertNotNull("first onStartCommand should launch a connectionJob", firstJob)
        assertTrue("first job should still be active", firstJob!!.isActive)

        service.onStartCommand(startIntent, 0, 2)
        val secondJob = service.connectionJob

        assertSame("a second start command must not replace the still-active job", firstJob, secondJob)
    }

    // ACTION_STOP must cancel the active connectionJob synchronously,
    // rather than only requesting a stop via stopSelf() and leaving the
    // retry loop running — the "tap Stop, it retries anyway" bug.
    @Test
    fun `ACTION_STOP cancels the active connectionJob`() {
        val service = newService()
        val app = RuntimeEnvironment.getApplication()

        service.onStartCommand(Intent(app, ObdForegroundService::class.java), 0, 1)
        val job = service.connectionJob
        assertNotNull(job)
        assertTrue(job!!.isActive)

        val stopIntent = Intent(app, ObdForegroundService::class.java).setAction(ObdForegroundService.ACTION_STOP)
        service.onStartCommand(stopIntent, 0, 2)

        assertTrue("connectionJob must be cancelled by ACTION_STOP", job.isCancelled)
        assertFalse(job.isActive)
    }

    // ACTION_QUIT is deliberately NOT exercised through onStartCommand()
    // here: its branch ends in Process.killProcess(Process.myPid()),
    // which would kill the JVM this test suite itself runs in, not just
    // an app-under-test process — there's no Robolectric shadow to trust
    // here, and finding out the hard way isn't worth it. It shares
    // ACTION_STOP's exact stopServiceImmediately() call (see
    // ObdForegroundService.onStartCommand()), which the test above
    // already covers; the kill call itself is a one-line, directly
    // readable call to a standard Android API and is checked by manual
    // code review rather than an automated test.
}
