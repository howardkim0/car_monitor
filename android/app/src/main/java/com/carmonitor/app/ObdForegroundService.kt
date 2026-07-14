package com.carmonitor.app

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothSocket
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.os.Process
import android.os.SystemClock
import android.util.Log
import androidx.annotation.VisibleForTesting
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import mobile.AnomalyListener
import mobile.Mobile
import mobile.ReadingListener
import mobile.Session
import java.io.IOException
import java.util.UUID
import java.util.concurrent.CopyOnWriteArrayList

/**
 * Foreground service owning the Bluetooth link to the hardcoded OBD2 dongle.
 * See DESIGN.md sections 4 and 7: this is deliberately dumb I/O plumbing —
 * all protocol framing/decoding lives in the Go [Session]; this class only
 * moves bytes in and out of the socket and turns connection state into a
 * notification and callbacks for [StatusActivity].
 */
class ObdForegroundService : Service() {

    sealed class ConnectionState {
        data object Connecting : ConnectionState()
        data object Connected : ConnectionState()
        data class Disconnected(val retryInSeconds: Int) : ConnectionState()
        data object PermissionMissing : ConnectionState()
        data object TimedOut : ConnectionState()
        data object Stopped : ConnectionState()
    }

    interface StatusListener {
        fun onStateChanged(state: ConnectionState)
        fun onReading(name: String, value: Double, unit: String)
    }

    inner class LocalBinder : Binder() {
        fun getService(): ObdForegroundService = this@ObdForegroundService
    }

    private data class ConnectionHandles(val socket: BluetoothSocket, val session: Session)

    private val binder = LocalBinder()
    private val scope = CoroutineScope(Dispatchers.IO + Job())
    private val listeners = CopyOnWriteArrayList<StatusListener>()

    // @Volatile: read/written from both this service's own IO-dispatcher
    // coroutine (connectionLoop() and friends) and the main thread
    // (onStartCommand()'s ACTION_STOP/ACTION_QUIT path, which now tears
    // down the connection synchronously instead of only ever doing it via
    // onDestroy() — see stopServiceImmediately()).
    @Volatile
    private var socket: BluetoothSocket? = null
    @Volatile
    private var session: Session? = null
    @Volatile
    @VisibleForTesting
    internal var connectionJob: Job? = null

    @Volatile
    private var latestState: ConnectionState = ConnectionState.Disconnected(retryInSeconds = 0)

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onCreate() {
        super.onCreate()
        try {
            Mobile.initAppLog(filesDir.absolutePath)
        } catch (e: Throwable) {
            // Best-effort, and deliberately catching Throwable (not just
            // Exception): app logging is an optional convenience, and no
            // failure initializing it — including an Error, e.g. a
            // corrupt/missing native library — should be able to crash
            // the whole foreground service and stop monitoring the car
            // over what is, at worst, a logging feature not working.
            Log.w(TAG, "Failed to initialize app log", e)
        }
        scope.launch { gitBackupLoop() }
        createNotificationChannel()
        createAnomalyNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification(latestState))
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopServiceImmediately(ConnectionState.Stopped)
                return START_NOT_STICKY
            }
            ACTION_QUIT -> {
                stopServiceImmediately(ConnectionState.Stopped)
                // Same process as the Activity (no multi-process manifest
                // config) — this is the standard way an Android app
                // provides a true "Quit" that takes the whole app down,
                // not just this service.
                Process.killProcess(Process.myPid())
                return START_NOT_STICKY
            }
        }

        // onStartCommand is always called on the main thread, one call at a
        // time, so checking-and-launching here is inherently race-free —
        // unlike checking `session == null`, which stays true for the
        // whole (multi-second) connect() call, so a second start request
        // arriving mid-connect (e.g. StatusActivity re-requesting
        // permissions after a screen rotation) would otherwise launch a
        // second concurrent connectionLoop().
        if (connectionJob?.isActive != true) {
            connectionJob = scope.launch { connectionLoop() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        closeConnection()
        try {
            Mobile.closeAppLog()
        } catch (e: Throwable) {
            // See onCreate()'s matching catch (Throwable, not Exception).
            Log.w(TAG, "Failed to close app log", e)
        }
        super.onDestroy()
    }

    fun addStatusListener(listener: StatusListener) {
        listeners.add(listener)
        listener.onStateChanged(latestState)
    }

    fun removeStatusListener(listener: StatusListener) {
        listeners.remove(listener)
    }

    /**
     * Connect, run the read/write loops until the socket fails, then
     * reconnect with exponential backoff (capped) per DESIGN.md section 7.
     * Never lets a real failure escape and kill the service — except that
     * if NO_CONNECTION_TIMEOUT_MS elapses without ever reaching Connected
     * (permission wait time counts too), the service stops itself rather
     * than retrying forever against a dongle that's never going to answer.
     *
     * `catch (e: CancellationException) { throw e }` below matters more
     * than it looks: CancellationException is a plain Exception subtype in
     * Kotlin, so a bare `catch (e: Exception)` silently swallows a
     * requested stop and treats it as just another failed attempt to
     * retry — which is exactly the "tap Stop, it retries anyway" bug this
     * change fixes. Cancellation must always propagate, never be treated
     * as a retryable failure.
     */
    private suspend fun connectionLoop() {
        var backoffMs = INITIAL_BACKOFF_MS
        // elapsedRealtime(), not wall-clock time: immune to the user (or
        // NTP) changing the system clock mid-wait.
        var lastConnectedAt = SystemClock.elapsedRealtime()

        while (scope.isActive) {
            if (!hasBluetoothPermission()) {
                if (SystemClock.elapsedRealtime() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                    stopSelfDueToNoConnection()
                    return
                }
                updateState(ConnectionState.PermissionMissing)
                delay(PERMISSION_POLL_INTERVAL_MS)
                continue
            }

            updateState(ConnectionState.Connecting)
            var permissionMissing = false
            try {
                val handles = openConnection()

                if (!scope.isActive) {
                    // Stop/Quit was requested while connect() — a raw
                    // blocking call coroutine cancellation cannot
                    // interrupt — was already in flight. Don't report
                    // Connected after the user already asked to stop;
                    // close what we just opened and get out.
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
                lastConnectedAt = SystemClock.elapsedRealtime()
                updateState(ConnectionState.Connected)
                runSessionLoops(handles.socket, handles.session)
            } catch (e: CancellationException) {
                throw e
            } catch (e: SecurityException) {
                Log.w(TAG, "Bluetooth permission missing", e)
                Mobile.logError("Bluetooth permission missing: $e")
                permissionMissing = true
            } catch (e: Exception) {
                // Socket/session failure of any kind (IOException from a
                // dropped connection, or an error from the Go layer) — fall
                // through to the backoff-and-retry below rather than
                // crashing the service.
                Log.w(TAG, "Connection attempt failed, will retry", e)
                Mobile.logError("Connection attempt failed, will retry: $e")
            } finally {
                closeConnection()
            }

            if (permissionMissing) {
                if (SystemClock.elapsedRealtime() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                    stopSelfDueToNoConnection()
                    return
                }
                updateState(ConnectionState.PermissionMissing)
                delay(PERMISSION_POLL_INTERVAL_MS)
                continue
            }

            if (SystemClock.elapsedRealtime() - lastConnectedAt >= NO_CONNECTION_TIMEOUT_MS) {
                stopSelfDueToNoConnection()
                return
            }

            val retrySeconds = (backoffMs / 1000).toInt()
            updateState(ConnectionState.Disconnected(retrySeconds))
            delay(backoffMs)
            backoffMs = (backoffMs * 2).coerceAtMost(MAX_BACKOFF_MS)
        }
    }

    private fun stopSelfDueToNoConnection() {
        val message = "No Bluetooth connection in ${NO_CONNECTION_TIMEOUT_MS / 1000}s, stopping"
        Log.w(TAG, message)
        Mobile.logError(message)
        stopServiceImmediately(ConnectionState.TimedOut)
    }

    /**
     * The one place that actually tears the service down — used by a
     * user-requested stop, Quit, and the no-connection timeout alike.
     * Deliberately does NOT rely on onDestroy() to do this work: a Service
     * stays alive as long as it's started OR bound, so if StatusActivity
     * happens to be bound (app open) when this is requested, onDestroy()
     * might not run for an arbitrarily long time — and simply calling
     * stopSelf() wouldn't touch the running connectionLoop() at all.
     *
     * cancel() alone isn't enough either: it can't interrupt a raw
     * blocking call already in flight (BluetoothSocket.connect(),
     * InputStream.read()), so closeConnection() is called here too —
     * closing the socket directly unblocks any such call from wherever
     * it's stuck, which is what actually makes "stop" mean *now* rather
     * than "whenever the coroutine next happens to check."
     *
     * Called from both the main thread (onStartCommand) and this
     * service's own IO-dispatcher coroutine (the timeout path) — see the
     * @Volatile fields above for why that's safe.
     */
    private fun stopServiceImmediately(finalState: ConnectionState) {
        connectionJob?.cancel()
        closeConnection()
        updateState(finalState)
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun hasBluetoothPermission(): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return ContextCompat.checkSelfPermission(
                this, Manifest.permission.BLUETOOTH_CONNECT
            ) == PackageManager.PERMISSION_GRANTED
        }
        return true
    }

    private fun openConnection(): ConnectionHandles {
        val bluetoothManager = getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager
        val adapter: BluetoothAdapter = bluetoothManager.adapter
            ?: throw IOException("No Bluetooth adapter on this device")
        if (!adapter.isEnabled) {
            throw IOException("Bluetooth is disabled")
        }

        // No adapter.cancelDiscovery() here: this app never calls
        // startDiscovery() (DESIGN.md's non-goals rule out a device
        // picker/scan UI for v1), and cancelDiscovery() requires the
        // BLUETOOTH_SCAN runtime permission on API 31+ that nothing else in
        // the app requests — calling it would turn "not scanning" into a
        // hard SecurityException that blocks every connection attempt.
        val device = adapter.getRemoteDevice(Mobile.deviceMAC())
        val newSocket = device.createRfcommSocketToServiceRecord(SPP_UUID)
        connectSocket(newSocket)

        val newSession = try {
            Mobile.newSession(filesDir.absolutePath, sessionListener, anomalyListener)
        } catch (e: Exception) {
            newSocket.close()
            throw e
        }

        return ConnectionHandles(newSocket, newSession)
    }

    /** Extracted from openConnection() so the socket-leak-on-failure fix is directly unit-testable. */
    @VisibleForTesting
    internal fun connectSocket(socket: BluetoothSocket) {
        try {
            socket.connect() // blocks until connected or throws IOException
        } catch (e: Exception) {
            socket.close() // connect() failed: don't leak the socket/fd
            throw e
        }
    }

    private val sessionListener = ReadingListener { _, name, unit, value, _ ->
        listeners.forEach { it.onReading(name, value, unit) }
    }

    /**
     * Posts a heads-up notification for a trend anomaly Go's Session.CheckAnomalies
     * found — see anomalyCheckLoop for how often that runs. A separate, higher-
     * importance channel from the ongoing connection-status one (CHANNEL_ID):
     * "coolant is overheating" deserves to interrupt in a way "still connected"
     * shouldn't. Not ongoing/persistent, and each metric gets its own notification
     * ID (so e.g. a coolant alert and a battery alert coexist instead of one
     * overwriting the other) rather than reusing NOTIFICATION_ID.
     */
    @VisibleForTesting
    internal val anomalyListener = AnomalyListener { metric, level, message, _ ->
        AnomalyNotifications.post(this, metric, level, message)
    }

    /** Suspends until the reader or writer loop throws (i.e. the socket died). */
    private suspend fun runSessionLoops(socket: BluetoothSocket, session: Session) = coroutineScope {
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
    private suspend fun gitBackupLoop() {
        while (currentCoroutineContext().isActive) {
            delay(GIT_BACKUP_CHECK_INTERVAL_MS)
            runCatching { Mobile.syncLogsIfNeeded(filesDir.absolutePath) }
                .onFailure { Log.w(TAG, "git backup check failed", it) }
        }
    }

    private suspend fun anomalyCheckLoop(session: Session) {
        while (currentCoroutineContext().isActive) {
            session.checkAnomalies()
            delay(ANOMALY_CHECK_INTERVAL_MS)
        }
    }

    private suspend fun readLoop(socket: BluetoothSocket, session: Session) {
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
                Mobile.logDebug(msg)
            }
        }
    }

    @VisibleForTesting
    internal fun writeCommand(output: java.io.OutputStream, command: String) {
        output.write((command + "\r").toByteArray(Charsets.US_ASCII))
    }

    private suspend fun writeLoop(socket: BluetoothSocket, session: Session) {
        val output = socket.outputStream
        var cycleCount = 0L
        val loopStartMs = SystemClock.elapsedRealtime()
        // Log once at the start of each session so the app log shows the
        // active polling constants — lets us verify DESIGN.md §12's
        // COMMAND_INTERVAL_MS/POLL_CYCLE_MS assumptions against real hardware
        // without needing to rebuild.
        val startMsg = "writeLoop starting: COMMAND_INTERVAL_MS=$COMMAND_INTERVAL_MS " +
            "POLL_CYCLE_MS=$POLL_CYCLE_MS commandCount=${session.commandCount()}"
        Log.d(TAG, startMsg)
        Mobile.logDebug(startMsg)
        // ELM327 setup, once per connection, before any PID/discovery command
        // — see DESIGN.md section 4 step 5 for why this exists.
        for (i in 0 until Mobile.initCommandCount()) {
            writeCommand(output, Mobile.initCommandAt(i))
            delay(COMMAND_INTERVAL_MS)
        }
        while (currentCoroutineContext().isActive) {
            val cycleStart = SystemClock.elapsedRealtime()
            val commandCount = session.commandCount()
            for (i in 0L until commandCount) {
                val command = session.commandAt(i)
                if (command.isEmpty()) continue
                writeCommand(output, command)
                output.flush()
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
                val elapsedSec = (SystemClock.elapsedRealtime() - loopStartMs) / 1000.0
                val cycleDurationMs = SystemClock.elapsedRealtime() - cycleStart
                val msg = "writeLoop cycle=$cycleCount elapsed=%.1fs commandCount=$commandCount " +
                    "cycleDurationMs=${cycleDurationMs}ms"
                Log.d(TAG, msg.format(elapsedSec))
                Mobile.logDebug(msg.format(elapsedSec))
            }
        }
    }

    private fun closeConnection() {
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

    private fun updateState(state: ConnectionState) {
        latestState = state
        listeners.forEach { it.onStateChanged(state) }
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.notify(NOTIFICATION_ID, buildNotification(state))
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.notification_channel_name),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = getString(R.string.notification_channel_description)
        }
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.createNotificationChannel(channel)
    }

    private fun createAnomalyNotificationChannel() {
        AnomalyNotifications.ensureChannel(this)
    }

    private fun buildNotification(state: ConnectionState): Notification {
        val contentIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, StatusActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = PendingIntent.getService(
            this,
            0,
            Intent(this, ObdForegroundService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE
        )
        val quitIntent = PendingIntent.getService(
            this,
            0,
            Intent(this, ObdForegroundService::class.java).setAction(ACTION_QUIT),
            PendingIntent.FLAG_IMMUTABLE
        )
        val text = when (state) {
            is ConnectionState.Connecting -> getString(R.string.notification_connecting, Mobile.deviceMAC())
            is ConnectionState.Connected -> getString(R.string.notification_connected, Mobile.deviceMAC())
            is ConnectionState.Disconnected -> getString(R.string.notification_disconnected, state.retryInSeconds)
            is ConnectionState.PermissionMissing -> getString(R.string.notification_permission_missing)
            is ConnectionState.TimedOut -> getString(R.string.notification_timed_out)
            is ConnectionState.Stopped -> getString(R.string.notification_stopped)
        }
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentIntent(contentIntent)
            .addAction(R.drawable.ic_stop, getString(R.string.notification_stop_action), stopIntent)
            .addAction(R.drawable.ic_quit, getString(R.string.notification_quit_action), quitIntent)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    companion object {
        private const val TAG = "ObdForegroundService"
        private const val CHANNEL_ID = "obd2_status"
        private const val NOTIFICATION_ID = 1
        private val SPP_UUID: UUID = UUID.fromString("00001101-0000-1000-8000-00805F9B34FB")
        private const val INITIAL_BACKOFF_MS = 1_000L
        private const val MAX_BACKOFF_MS = 30_000L
        // 200ms, not the original 50ms (which itself was reduced from 100ms
        // alongside PID expansion — see DESIGN.md section 5.2 and §12).
        // 200ms is gentler on the ELM327 adapter while still cycling all 32
        // PIDs in ~7s (200ms * 32 + POLL_CYCLE_MS). Unverified against real
        // hardware — the diagnostic logs added to writeLoop/readLoop (see
        // above) will show actual cycle duration on a real device, letting
        // us tune this value based on real ELM327 behavior.
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
        // Git backup check cadence. Go's SyncIfNeeded decides whether real
        // work happens, so this can be coarse; 5 minutes is fine.
        private const val GIT_BACKUP_CHECK_INTERVAL_MS = 5 * 60 * 1_000L
        @VisibleForTesting
        internal const val ACTION_STOP = "com.carmonitor.app.action.STOP"
        @VisibleForTesting
        internal const val ACTION_QUIT = "com.carmonitor.app.action.QUIT"

        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ObdForegroundService::class.java))
        }

        /** Fully stops monitoring — see stopServiceImmediately(). Tap "Start Scanning" to resume. */
        fun stop(context: Context) {
            context.startService(Intent(context, ObdForegroundService::class.java).setAction(ACTION_STOP))
        }

        /** Stops monitoring and kills the whole app process — see onStartCommand(). */
        fun quit(context: Context) {
            context.startService(Intent(context, ObdForegroundService::class.java).setAction(ACTION_QUIT))
        }
    }
}
