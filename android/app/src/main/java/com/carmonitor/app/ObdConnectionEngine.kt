package com.carmonitor.app

import android.bluetooth.BluetoothSocket
import android.os.SystemClock
import android.util.Log
import androidx.annotation.VisibleForTesting
import java.io.IOException
import java.io.OutputStream
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import mobile.Mobile

/**
 * Owns the connect → read/write/backoff loop for the Bluetooth link to
 * the OBD2 dongle (DESIGN.md section 4) — extracted out of
 * [ObdForegroundService] purely for testability: [mobile]/[clock] are
 * constructor-injected so `ObdConnectionEngineTest` can exercise the
 * whole state machine directly under `kotlinx-coroutines-test` virtual
 * time, with no Service, no Robolectric, and no real multi-minute
 * waits. Socket-opening (needs a real `Context`/`BluetoothManager`) and
 * anything that updates the persistent notification stay on
 * `ObdForegroundService`, reached back through [Callbacks].
 *
 * [logDebug]/[logError] default to `Mobile`'s own diagnostic logging
 * but are constructor-injectable too — not because they're part of
 * DESIGN.md section 3's `ObdMobile`/`ObdSession` seam (they're
 * deliberately excluded from it: pure side-effecting writes nothing
 * branches on), but because `mobile.Mobile`'s static initializer loads
 * a native library on first touch. Leaving these as bare
 * `Mobile.logDebug`/`logError` calls would make any test exercising
 * `readLoop`/`writeLoop` crash on the first log line, defeating the
 * whole point of this split.
 */
class ObdConnectionEngine(
    private val callbacks: Callbacks,
    private val mobile: ObdMobile = RealObdMobile,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
    // Lambda literals, not bare `Mobile::logDebug`/`Mobile::logError`
    // method references: a method reference to a class with a failing
    // static initializer (mobile.Mobile's native lib load) gets resolved
    // — and so throws — at every construction site that doesn't override
    // this default, not deferred until actually called. A lambda literal
    // only touches Mobile when its body actually runs.
    private val logDebug: (String) -> Unit = { Mobile.logDebug(it) },
    private val logError: (String) -> Unit = { Mobile.logError(it) },
) {

    /** Reached back into `ObdForegroundService` for what still needs a real Android `Context`. */
    interface Callbacks {
        fun hasBluetoothPermission(): Boolean
        fun openConnection(): ObdForegroundService.ConnectionHandles
        fun onStateChanged(state: ObdForegroundService.ConnectionState)
        fun onNoConnectionTimeout()
    }

    @Volatile
    private var socket: BluetoothSocket? = null
    @Volatile
    private var session: ObdSession? = null

    /**
     * Connect, run the read/write loops until the socket fails, then
     * reconnect with exponential backoff (capped) per DESIGN.md section 7.
     * Never lets a real failure escape and kill the caller — except that
     * if NO_CONNECTION_TIMEOUT_MS elapses without ever reaching Connected
     * (permission wait time counts too), [Callbacks.onNoConnectionTimeout]
     * fires instead of retrying forever against a dongle that's never
     * going to answer.
     *
     * `catch (e: CancellationException) { throw e }` below matters more
     * than it looks: CancellationException is a plain Exception subtype in
     * Kotlin, so a bare `catch (e: Exception)` silently swallows a
     * requested stop and treats it as just another failed attempt to
     * retry — which is exactly the "tap Stop, it retries anyway" bug this
     * change fixes. Cancellation must always propagate, never be treated
     * as a retryable failure.
     */
    suspend fun connectionLoop() {
        var backoffMs = INITIAL_BACKOFF_MS
        // clock() (elapsedRealtime in production), not wall-clock time:
        // immune to the user (or NTP) changing the system clock mid-wait.
        var lastConnectedAt = clock()

        while (currentCoroutineContext().isActive) {
            if (!callbacks.hasBluetoothPermission()) {
                if (clock() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                    handleNoConnectionTimeout()
                    return
                }
                callbacks.onStateChanged(ObdForegroundService.ConnectionState.PermissionMissing)
                delay(PERMISSION_POLL_INTERVAL_MS)
                continue
            }

            callbacks.onStateChanged(ObdForegroundService.ConnectionState.Connecting)
            var permissionMissing = false
            try {
                val handles = callbacks.openConnection()

                if (!currentCoroutineContext().isActive) {
                    // Stop/Quit was requested while openConnection() — a raw
                    // blocking call coroutine cancellation cannot interrupt
                    // — was already in flight. Don't report Connected after
                    // the caller already asked to stop; close what we just
                    // opened and get out.
                    try {
                        handles.session.close()
                    } catch (e: Exception) {
                        // Best-effort; the service is stopping regardless.
                    }
                    handles.socket.close()
                    return
                }

                socket = handles.socket
                session = handles.session
                backoffMs = INITIAL_BACKOFF_MS
                lastConnectedAt = clock()
                callbacks.onStateChanged(ObdForegroundService.ConnectionState.Connected)
                runSessionLoops(handles.socket, handles.session)
            } catch (e: CancellationException) {
                throw e
            } catch (e: SecurityException) {
                Log.w(TAG, "Bluetooth permission missing", e)
                logError("Bluetooth permission missing: $e")
                permissionMissing = true
            } catch (e: Exception) {
                // Socket/session failure of any kind (IOException from a
                // dropped connection, or an error from the Go layer) — fall
                // through to the backoff-and-retry below rather than
                // crashing the caller.
                Log.w(TAG, "Connection attempt failed, will retry", e)
                logError("Connection attempt failed, will retry: $e")
            } finally {
                closeConnectionNow()
            }

            if (permissionMissing) {
                if (clock() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                    handleNoConnectionTimeout()
                    return
                }
                callbacks.onStateChanged(ObdForegroundService.ConnectionState.PermissionMissing)
                delay(PERMISSION_POLL_INTERVAL_MS)
                continue
            }

            if (clock() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                handleNoConnectionTimeout()
                return
            }

            val retrySeconds = (backoffMs / 1000).toInt()
            callbacks.onStateChanged(ObdForegroundService.ConnectionState.Disconnected(retrySeconds))
            delay(backoffMs)
            backoffMs = (backoffMs * 2).coerceAtMost(MAX_BACKOFF_MS)
        }
    }

    private fun handleNoConnectionTimeout() {
        val message = "No Bluetooth connection in ${NO_CONNECTION_TIMEOUT_MS / 1000}s, stopping"
        Log.w(TAG, message)
        logError(message)
        callbacks.onNoConnectionTimeout()
    }

    /**
     * Closes the current connection (if any) — best-effort, never throws.
     * Used both internally (between connection attempts) and externally
     * by `ObdForegroundService.reconnectNow()`/`stopServiceImmediately()`
     * (DESIGN.md section 7), which reach the currently-active engine
     * instance to unblock a raw blocking call (`connect()`, `read()`)
     * stuck mid-flight from wherever it's stuck.
     */
    fun closeConnectionNow() {
        try {
            session?.close()
        } catch (e: Exception) {
            // Best-effort close; the connection is going away regardless.
        }
        session = null

        try {
            socket?.close()
        } catch (e: IOException) {
            // Best-effort close.
        }
        socket = null
    }

    /**
     * Suspends until the reader or writer loop throws (i.e. the socket
     * died). No dispatcher specified on these launches — all three
     * inherit whatever dispatcher connectionLoop() itself is running on
     * (Dispatchers.IO in production, via ObdForegroundService's scope;
     * a virtual-time test dispatcher in ObdConnectionEngineTest, letting
     * writeLoop/anomalyCheckLoop's delay()-paced tests run instantly).
     */
    private suspend fun runSessionLoops(socket: BluetoothSocket, session: ObdSession) = coroutineScope {
        launch { readLoop(socket, session) }
        launch { writeLoop(socket, session) }
        launch { anomalyCheckLoop(session) }
    }

    /**
     * Periodically asks Go to re-check today's logged readings for trend
     * anomalies (coolant temp, battery voltage, fuel trims, catalytic
     * converter — see internal/trend and internal/monitor) and notifies on
     * whatever's new. How often to check is this loop's call, same as
     * writeLoop's POLL_CYCLE_MS is for polling — see DESIGN.md section 4
     * step 5. A full day's worth of readings gets re-read from disk on every
     * check (see storage.LoadReadings); ANOMALY_CHECK_INTERVAL_MS is
     * intentionally much coarser than the polling cycle so that cost stays
     * bounded rather than paid multiple times a second.
     */
    @VisibleForTesting
    internal suspend fun anomalyCheckLoop(session: ObdSession) {
        while (currentCoroutineContext().isActive) {
            session.checkAnomalies()
            delay(ANOMALY_CHECK_INTERVAL_MS)
        }
    }

    private suspend fun readLoop(socket: BluetoothSocket, session: ObdSession) {
        val input = socket.inputStream
        val buffer = ByteArray(1024)
        var readCount = 0L
        while (currentCoroutineContext().isActive) {
            val read = input.read(buffer) // blocking read, safe on Dispatchers.IO
            if (read < 0) throw IOException("Bluetooth input stream closed")
            session.feed(buffer.copyOf(read))
            readCount++
            // Log the very first read (confirms the socket is delivering
            // data end-to-end) and then every READ_DIAGNOSTIC_EVERY_N reads,
            // noting bytes received so we can gauge throughput vs. command
            // cadence on real hardware.
            if (readCount == 1L || readCount % READ_DIAGNOSTIC_EVERY_N == 0L) {
                val msg = "readLoop read=$readCount bytes=$read"
                Log.d(TAG, msg)
                logDebug(msg)
            }
        }
    }

    @VisibleForTesting
    internal fun writeCommand(output: OutputStream, command: String) {
        output.write((command + "\r").toByteArray(Charsets.US_ASCII))
        output.flush()
    }

    @VisibleForTesting
    internal suspend fun writeLoop(socket: BluetoothSocket, session: ObdSession) {
        val output = socket.outputStream
        var cycleCount = 0L
        val loopStartMs = clock()
        // Log once at the start of each session so the app log shows the
        // active polling constants — lets us verify DESIGN.md §12's
        // COMMAND_INTERVAL_MS/POLL_CYCLE_MS assumptions against real hardware
        // without needing to rebuild.
        val startMsg = "writeLoop starting: COMMAND_INTERVAL_MS=$COMMAND_INTERVAL_MS " +
            "POLL_CYCLE_MS=$POLL_CYCLE_MS commandCount=${session.commandCount()}"
        Log.d(TAG, startMsg)
        logDebug(startMsg)
        // ELM327 setup, once per connection, before any PID/discovery command
        // — see DESIGN.md section 4 step 5 for why this exists.
        for (i in 0 until mobile.initCommandCount()) {
            val initCommand = mobile.initCommandAt(i)
            writeCommand(output, initCommand)
            val initMsg = "writeLoop: sent init command $initCommand"
            Log.d(TAG, initMsg)
            logDebug(initMsg)
            delay(COMMAND_INTERVAL_MS)
        }
        val initDoneMsg = "writeLoop: init sequence complete (${mobile.initCommandCount()} commands sent)"
        Log.d(TAG, initDoneMsg)
        logDebug(initDoneMsg)
        while (currentCoroutineContext().isActive) {
            val cycleStart = clock()
            val commandCount = session.commandCount()
            for (i in 0L until commandCount) {
                val command = session.commandAt(i)
                if (command.isEmpty()) continue
                writeCommand(output, command)
                delay(COMMAND_INTERVAL_MS)
            }
            delay(POLL_CYCLE_MS)
            cycleCount++
            // Log every POLL_DIAGNOSTIC_EVERY_N_CYCLES cycles so we get
            // real-hardware data on: actual cycle cadence vs. the constants
            // above, how many commands are being sent (may be fewer after
            // discovery filters PIDs), and whether cycles are running longer
            // than expected (which would suggest the ELM327 is slowing us).
            if (cycleCount % POLL_DIAGNOSTIC_EVERY_N_CYCLES == 0L) {
                val elapsedSec = (clock() - loopStartMs) / 1000.0
                val cycleDurationMs = clock() - cycleStart
                val msg = "writeLoop cycle=$cycleCount elapsed=%.1fs commandCount=$commandCount " +
                    "cycleDurationMs=${cycleDurationMs}ms"
                Log.d(TAG, msg.format(elapsedSec))
                logDebug(msg.format(elapsedSec))
            }
        }
    }

    companion object {
        private const val TAG = "ObdConnectionEngine"
        private const val INITIAL_BACKOFF_MS = 1_000L
        private const val MAX_BACKOFF_MS = 30_000L
        // 200ms, not the original 50ms (which itself was reduced from 100ms
        // alongside PID expansion — see DESIGN.md section 5.2 and §12).
        // 200ms is gentler on the ELM327 adapter while still cycling all 32
        // PIDs in ~7s (200ms * 32 + POLL_CYCLE_MS). Unverified against real
        // hardware — the diagnostic logs above show actual cycle duration on
        // a real device, letting us tune this value based on real ELM327
        // behavior.
        private const val COMMAND_INTERVAL_MS = 200L
        private const val POLL_CYCLE_MS = 250L
        // Deliberately much coarser than POLL_CYCLE_MS: each check re-reads
        // and re-parses the whole day's CSV log from disk (see
        // storage.LoadReadings), and every trend.Check* function already
        // only cares about the last 30s-5min of data anyway — there's
        // nothing to gain from checking more often than this, only more
        // disk I/O paid for it as the day's log grows.
        private const val ANOMALY_CHECK_INTERVAL_MS = 60_000L
        private const val PERMISSION_POLL_INTERVAL_MS = 3_000L
        private const val NO_CONNECTION_TIMEOUT_MS = 5 * 60 * 1_000L
        // Emit a writeLoop timing log every N cycles; at 200ms/command and
        // 32 commands + 250ms POLL_CYCLE_MS overhead, one cycle is ~6.65s,
        // so N=9 gives a diagnostic line approximately every minute.
        private const val POLL_DIAGNOSTIC_EVERY_N_CYCLES = 9L
        // Emit a readLoop bytes-received log every N reads. ELM327 responses
        // are short (< 20 bytes each), so this is roughly every ~100 command
        // responses — a few minutes of real driving at 200ms/command.
        private const val READ_DIAGNOSTIC_EVERY_N = 100L
    }
}
