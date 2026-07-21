package com.carmonitor.app

import android.Manifest
import android.app.Notification
import android.app.NotificationManager
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothSocket
import android.content.Context
import android.content.Intent
import io.mockk.every
import io.mockk.mockk
import io.mockk.verify
import java.io.IOException
import org.junit.After
import org.junit.Assert.assertEquals
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
import org.robolectric.Shadows.shadowOf
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

    // The anomaly channel must actually be created with HIGH importance —
    // if this silently regressed to the same LOW importance as the
    // ongoing status channel, anomaly notifications would stop reliably
    // interrupting a driver the way "coolant is overheating" needs to.
    @Test
    fun `onCreate creates the anomaly notification channel at HIGH importance`() {
        val service = newService()
        val manager = service.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        val channel = manager.getNotificationChannel(AnomalyNotifications.CHANNEL_ID)

        assertNotNull("anomaly notification channel should be created in onCreate()", channel)
        assertEquals(NotificationManager.IMPORTANCE_HIGH, channel!!.importance)
    }

    // Confirms the anomaly listener actually posts something a user would
    // see — the whole point of wiring internal/trend into the app at all
    // — rather than only logging or updating internal state.
    @Test
    fun `anomalyListener posts a notification naming the metric on the anomaly channel`() {
        val service = newService()
        val manager = service.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        service.anomalyListener.onAnomaly(
            "Coolant Temperature", "CRITICAL", "Coolant temperature is critically high: 112.5°C", 0L
        )

        val wantId = 1000 + ("Coolant Temperature".hashCode() and 0xFF)
        val posted: Notification? = manager.activeNotifications
            .firstOrNull { it.id == wantId }
            ?.notification

        assertNotNull("expected a notification posted for the Coolant Temperature anomaly", posted)
        assertEquals(AnomalyNotifications.CHANNEL_ID, posted!!.channelId)
    }

    @Test
    fun `writeCommand writes command with carriage return as ASCII bytes`() {
        val service = newService()
        val mockOutputStream = mockk<java.io.OutputStream>(relaxed = true)
        val slot = io.mockk.slot<ByteArray>()

        io.mockk.every { mockOutputStream.write(capture(slot)) } returns Unit

        service.writeCommand(mockOutputStream, "ATE0")

        io.mockk.verify(exactly = 1) { mockOutputStream.write(any<ByteArray>()) }
        val wantBytes = "ATE0\r".toByteArray(Charsets.US_ASCII)
        assertEquals("writeCommand should write command with carriage return",
            wantBytes.contentToString(), slot.captured.contentToString())

        // Every command write is flushed immediately — writeLoop's init
        // sequence and main polling loop both rely on writeCommand for
        // this rather than flushing separately at each call site (a prior
        // version of this fix only flushed in the main loop, silently
        // leaving the init sequence unflushed).
        io.mockk.verify(exactly = 1) { mockOutputStream.flush() }
    }

    @Test
    fun `reconnectNow does not throw when nothing is connected`() {
        val service = newService()
        service.reconnectNow() // must not throw
    }

    @Test
    @Config(sdk = [30])
    fun `hasBluetoothPermission is always true below API 31`() {
        val service = newService()
        assertTrue(service.hasBluetoothPermission())
    }

    @Test
    fun `hasBluetoothPermission reflects BLUETOOTH_CONNECT grant on API 31+`() {
        val service = newService()
        assertFalse(
            "should be false before the permission is granted",
            service.hasBluetoothPermission()
        )

        shadowOf(service.application).grantPermissions(Manifest.permission.BLUETOOTH_CONNECT)

        assertTrue(
            "should be true once BLUETOOTH_CONNECT is granted",
            service.hasBluetoothPermission()
        )
    }

    // openConnection() must fail fast — before ever touching Mobile.deviceMAC()
    // — when Bluetooth itself is off, rather than trying to open a socket
    // against a disabled adapter.
    @Test
    fun `openConnection throws when the Bluetooth adapter is disabled`() {
        val service = newService()
        val bluetoothManager = service.getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager
        shadowOf(bluetoothManager.adapter).setEnabled(false)

        try {
            service.openConnection()
            fail("expected an IOException when the adapter is disabled")
        } catch (e: IOException) {
            assertEquals("Bluetooth is disabled", e.message)
        }
    }

    @Test
    fun `addStatusListener immediately delivers the current state`() {
        val service = newService()
        var received: ObdForegroundService.ConnectionState? = null
        val listener = object : ObdForegroundService.StatusListener {
            override fun onStateChanged(state: ObdForegroundService.ConnectionState) {
                received = state
            }
            override fun onReading(name: String, value: Double, unit: String) {}
        }

        service.addStatusListener(listener)

        assertNotNull("a newly-added listener should be told the current state right away", received)
    }

    @Test
    fun `removeStatusListener stops further state updates`() {
        val service = newService()
        var callCount = 0
        val listener = object : ObdForegroundService.StatusListener {
            override fun onStateChanged(state: ObdForegroundService.ConnectionState) { callCount++ }
            override fun onReading(name: String, value: Double, unit: String) {}
        }
        service.addStatusListener(listener)
        val countAfterAdd = callCount

        service.removeStatusListener(listener)
        val app = RuntimeEnvironment.getApplication()
        service.onStartCommand(
            Intent(app, ObdForegroundService::class.java).setAction(ObdForegroundService.ACTION_STOP), 0, 1
        )

        assertEquals(
            "a removed listener must not hear about a later state change",
            countAfterAdd,
            callCount
        )
    }

    @Test
    fun `LocalBinder getService returns the owning service`() {
        val service = newService()
        val binder = service.onBind(Intent(RuntimeEnvironment.getApplication(), ObdForegroundService::class.java))
            as ObdForegroundService.LocalBinder

        assertSame(service, binder.getService())
    }

    @Test
    fun `onDestroy cancels an active connectionJob`() {
        val service = newService()
        val app = RuntimeEnvironment.getApplication()
        service.onStartCommand(Intent(app, ObdForegroundService::class.java), 0, 1)
        val job = service.connectionJob
        assertNotNull(job)
        assertTrue(job!!.isActive)

        service.onDestroy()

        // Not job.isCancelled: onDestroy() cancels via scope.cancel() (the
        // parent), not connectionJob.cancel() directly (unlike the
        // ACTION_STOP test above) — isCancelled can lag until the child
        // coroutine is actually scheduled to observe it, but isActive
        // flips immediately and is what actually matters here: the job is
        // no longer running.
        assertFalse("onDestroy must cancel the running connectionJob", job.isActive)
    }

    @Test
    fun `buildNotification uses the disconnected retry-countdown text`() {
        val service = newService()
        val notification = service.buildNotification(ObdForegroundService.ConnectionState.Disconnected(retryInSeconds = 7))

        assertEquals(
            service.getString(R.string.notification_disconnected, 7),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString()
        )
    }

    @Test
    fun `buildNotification uses the permission-missing text`() {
        val service = newService()
        val notification = service.buildNotification(ObdForegroundService.ConnectionState.PermissionMissing)

        assertEquals(
            service.getString(R.string.notification_permission_missing),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString()
        )
    }

    @Test
    fun `buildNotification uses the timed-out text`() {
        val service = newService()
        val notification = service.buildNotification(ObdForegroundService.ConnectionState.TimedOut)

        assertEquals(
            service.getString(R.string.notification_timed_out),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString()
        )
    }

    @Test
    fun `buildNotification uses the stopped text`() {
        val service = newService()
        val notification = service.buildNotification(ObdForegroundService.ConnectionState.Stopped)

        assertEquals(
            service.getString(R.string.notification_stopped),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString()
        )
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
