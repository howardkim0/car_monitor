package com.carmonitor.app

import android.bluetooth.BluetoothSocket
import kotlinx.coroutines.ExperimentalCoroutinesApi
import io.mockk.every
import io.mockk.mockk
import io.mockk.slot
import io.mockk.verify
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Plain JUnit4 — deliberately no RobolectricTestRunner. ObdConnectionEngine
 * takes its ObdMobile/ObdSession/clock dependencies via its constructor
 * (docs/plan-obd-service-testability.md), so its connect/backoff/retry
 * state machine runs entirely under kotlinx-coroutines-test's virtual
 * time here — no Service, no Android framework, no real multi-minute
 * waits for backoff/timeout assertions that would otherwise be
 * impractical to test honestly.
 *
 * readLoop is exercised only indirectly (test 2, via an InputStream that
 * returns -1 immediately) — it's deliberately not given its own test,
 * matching DESIGN.md section 10's "not a coverage-chasing exercise":
 * it's a thin read-feed-log loop with no branching logic of its own.
 * writeLoop/anomalyCheckLoop are called directly (bypassing
 * runSessionLoops/connectionLoop entirely) so their delay()-paced
 * behavior is testable without any concurrent-loop entanglement.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ObdConnectionEngineTest {

    private class RecordingOutputStream : OutputStream() {
        val writes = mutableListOf<ByteArray>()
        override fun write(b: Int) {
            writes += byteArrayOf(b.toByte())
        }
        override fun write(b: ByteArray, off: Int, len: Int) {
            writes += b.copyOfRange(off, off + len)
        }
        fun commandsSoFar(): List<String> = writes.map { String(it, Charsets.US_ASCII).trim() }
    }

    /** Immediately reports the stream as closed — never blocks. */
    private class ClosedInputStream : InputStream() {
        override fun read(): Int = -1
    }

    private fun fakeSocket(output: OutputStream = ByteArrayOutputStream()): BluetoothSocket {
        val socket = mockk<BluetoothSocket>(relaxed = true)
        every { socket.inputStream } returns ClosedInputStream()
        every { socket.outputStream } returns output
        return socket
    }

    @Test
    fun `connectionLoop backs off exponentially, capped at 30s`() = runTest {
        val callbacks = FakeConnectionEngineCallbacks().apply {
            openConnectionResult = { throw IOException("always fails") }
        }
        val engine = ObdConnectionEngine(callbacks, clock = { currentTime }, logDebug = {}, logError = {})
        val job = launch { engine.connectionLoop() }

        // Sum of the first 7 backoff delays (1+2+4+8+16+30+30=91s), with margin.
        advanceTimeBy(95_000)
        runCurrent()
        job.cancel()

        val disconnectedSeconds = callbacks.stateChanges
            .filterIsInstance<ObdForegroundService.ConnectionState.Disconnected>()
            .map { it.retryInSeconds }
        assertEquals(listOf(1, 2, 4, 8, 16, 30, 30), disconnectedSeconds.take(7))
    }

    @Test
    fun `a session failure after connecting falls through to backoff, not a crash`() = runTest {
        val socket = fakeSocket()
        val session = FakeObdSession()
        val callbacks = FakeConnectionEngineCallbacks().apply {
            openConnectionResult = { ObdForegroundService.ConnectionHandles(socket, session) }
        }
        val engine = ObdConnectionEngine(callbacks, mobile = FakeObdMobile(), clock = { currentTime }, logDebug = {}, logError = {})
        val job = launch { engine.connectionLoop() }

        advanceTimeBy(5_000)
        runCurrent()
        job.cancel()

        assertTrue(
            "expected a Connected state before the simulated session failure",
            callbacks.stateChanges.any { it is ObdForegroundService.ConnectionState.Connected }
        )
        assertTrue(
            "expected the loop to fall through to a Disconnected retry state, not crash",
            callbacks.stateChanges.any { it is ObdForegroundService.ConnectionState.Disconnected }
        )
    }

    @Test
    fun `SecurityException is treated as permission-missing, polling at a fixed interval not exponential backoff`() = runTest {
        val callbacks = FakeConnectionEngineCallbacks().apply {
            openConnectionResult = { throw SecurityException("Bluetooth permission denied") }
        }
        val engine = ObdConnectionEngine(callbacks, clock = { currentTime }, logDebug = {}, logError = {})
        val job = launch { engine.connectionLoop() }

        advanceTimeBy(10_000) // ~3 cycles at the 3s PERMISSION_POLL_INTERVAL_MS cadence
        runCurrent()
        job.cancel()

        val permissionMissingCount = callbacks.stateChanges.count {
            it is ObdForegroundService.ConnectionState.PermissionMissing
        }
        val disconnectedCount = callbacks.stateChanges.count {
            it is ObdForegroundService.ConnectionState.Disconnected
        }
        assertTrue("expected several PermissionMissing states at a fixed cadence", permissionMissingCount >= 3)
        assertEquals(
            "SecurityException must not trigger the exponential-backoff Disconnected path",
            0,
            disconnectedCount
        )
    }

    @Test
    fun `NO_CONNECTION_TIMEOUT_MS elapsing without ever connecting calls onNoConnectionTimeout once`() = runTest {
        val callbacks = FakeConnectionEngineCallbacks(permissionGranted = false)
        val engine = ObdConnectionEngine(callbacks, clock = { currentTime }, logDebug = {}, logError = {})
        val job = launch { engine.connectionLoop() }

        advanceTimeBy(5 * 60 * 1_000L + 1_000L) // NO_CONNECTION_TIMEOUT_MS (5min) plus margin
        runCurrent()

        assertEquals(1, callbacks.noConnectionTimeoutCallCount)
        assertTrue("connectionLoop should return on its own once the timeout fires", job.isCompleted)
    }

    // A bare `job.cancel()` while connectionLoop is parked in the outer
    // backoff delay() — outside the try block entirely — propagates
    // identically whether or not the try block's own
    // `catch (e: CancellationException) { throw e }` line exists, so it
    // wouldn't actually catch a regression there. The real risk that
    // line guards against is cancellation arriving *inside* the try —
    // e.g. while openConnection()'s real, blocking BluetoothSocket.connect()
    // call was already in flight, so coroutine cancellation couldn't
    // interrupt it and it returns normally anyway. This test simulates
    // exactly that race: openConnection() "succeeds" but cancel() was
    // already called from within it, as connectSocket()'s real blocking
    // call would leave no other way to notice a stop request mid-connect.
    @Test
    fun `cancellation racing a successful openConnection closes the connection and never reports Connected`() = runTest {
        val socket = mockk<BluetoothSocket>(relaxed = true)
        val session = FakeObdSession()
        lateinit var job: Job
        val callbacks = FakeConnectionEngineCallbacks().apply {
            openConnectionResult = {
                job.cancel()
                ObdForegroundService.ConnectionHandles(socket, session)
            }
        }
        val engine = ObdConnectionEngine(callbacks, mobile = FakeObdMobile(), clock = { currentTime }, logDebug = {}, logError = {})
        job = launch { engine.connectionLoop() }

        runCurrent()
        job.join()

        assertTrue("connectionLoop's job should complete as cancelled", job.isCancelled)
        assertEquals("the handles opened just before cancellation must still be closed", 1, session.closeCallCount)
        verify(exactly = 1) { socket.close() }
        assertTrue(
            "must never report Connected once cancellation was already requested",
            callbacks.stateChanges.none { it is ObdForegroundService.ConnectionState.Connected }
        )
    }

    @Test
    fun `writeLoop sends the full init sequence once per connection, COMMAND_INTERVAL_MS apart`() = runTest {
        val recordingOutput = RecordingOutputStream()
        val socket = fakeSocket(recordingOutput)
        val mobile = FakeObdMobile(initCommands = listOf("ATE0", "ATL0", "ATSP0"))
        val engine = ObdConnectionEngine(FakeConnectionEngineCallbacks(), mobile = mobile, clock = { currentTime }, logDebug = {}, logError = {})

        val job = launch { engine.writeLoop(socket, FakeObdSession()) }
        runCurrent()
        assertEquals(listOf("ATE0"), recordingOutput.commandsSoFar())

        advanceTimeBy(200) // COMMAND_INTERVAL_MS
        runCurrent()
        assertEquals(listOf("ATE0", "ATL0"), recordingOutput.commandsSoFar())

        advanceTimeBy(200)
        runCurrent()
        assertEquals(listOf("ATE0", "ATL0", "ATSP0"), recordingOutput.commandsSoFar())

        job.cancel()
    }

    @Test
    fun `writeLoop skips empty commands from commandAt without writing or delaying for them`() = runTest {
        val recordingOutput = RecordingOutputStream()
        val socket = fakeSocket(recordingOutput)
        val session = FakeObdSession(commands = listOf("0104", "", "0105"))
        val engine = ObdConnectionEngine(FakeConnectionEngineCallbacks(), mobile = FakeObdMobile(), clock = { currentTime }, logDebug = {}, logError = {})

        val job = launch { engine.writeLoop(socket, session) }
        runCurrent()
        assertEquals(listOf("0104"), recordingOutput.commandsSoFar())

        // Only one COMMAND_INTERVAL_MS elapses, yet both real commands are
        // already sent — proving the empty command in between was skipped
        // without writing anything or costing a delay cycle of its own.
        advanceTimeBy(200)
        runCurrent()
        assertEquals(listOf("0104", "0105"), recordingOutput.commandsSoFar())

        job.cancel()
    }

    @Test
    fun `anomalyCheckLoop calls checkAnomalies every ANOMALY_CHECK_INTERVAL_MS`() = runTest {
        val session = FakeObdSession()
        val engine = ObdConnectionEngine(FakeConnectionEngineCallbacks(), clock = { currentTime }, logDebug = {}, logError = {})

        val job = launch { engine.anomalyCheckLoop(session) }
        runCurrent()
        assertEquals(1, session.checkAnomaliesCallCount)

        advanceTimeBy(60_000) // ANOMALY_CHECK_INTERVAL_MS
        runCurrent()
        assertEquals(2, session.checkAnomaliesCallCount)

        advanceTimeBy(60_000)
        runCurrent()
        assertEquals(3, session.checkAnomaliesCallCount)

        job.cancel()
    }

    @Test
    fun `writeCommand writes command with carriage return as ASCII bytes`() {
        val engine = ObdConnectionEngine(FakeConnectionEngineCallbacks(), logDebug = {}, logError = {})
        val mockOutputStream = mockk<OutputStream>(relaxed = true)
        val slot = slot<ByteArray>()
        every { mockOutputStream.write(capture(slot)) } returns Unit

        engine.writeCommand(mockOutputStream, "ATE0")

        verify(exactly = 1) { mockOutputStream.write(any<ByteArray>()) }
        val wantBytes = "ATE0\r".toByteArray(Charsets.US_ASCII)
        assertEquals(wantBytes.contentToString(), slot.captured.contentToString())

        // Every command write is flushed immediately — writeLoop's init
        // sequence and main polling loop both rely on writeCommand for
        // this rather than flushing separately at each call site (a prior
        // version of this fix only flushed in the main loop, silently
        // leaving the init sequence unflushed).
        verify(exactly = 1) { mockOutputStream.flush() }
    }
}
