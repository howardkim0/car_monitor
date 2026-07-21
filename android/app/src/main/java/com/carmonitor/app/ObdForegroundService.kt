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
import android.util.Log
import androidx.annotation.VisibleForTesting
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import mobile.AnomalyListener
import mobile.Mobile
import mobile.ReadingListener
import java.io.File
import java.io.IOException
import java.util.UUID
import java.util.concurrent.CopyOnWriteArrayList

/**
 * Foreground service owning the Bluetooth link to the hardcoded OBD2 dongle.
 * See DESIGN.md sections 4 and 7: this is deliberately dumb I/O plumbing —
 * all protocol framing/decoding lives in Go's Session type; this class only
 * opens the Bluetooth socket, handles Android lifecycle/permissions, and
 * turns connection state into a notification and callbacks for
 * [StatusActivity]. The actual connect/read/write/backoff loop is owned by
 * [ObdConnectionEngine] (constructed fresh in [onStartCommand]), which this
 * class drives as its [ObdConnectionEngine.Callbacks] — that split exists
 * purely for testability (DESIGN.md section 3/4). `Mobile`/`Session` calls
 * this class makes directly (`openConnection()`/`buildNotification()`) go
 * through [ObdMobile]/[ObdSession] rather than the gomobile-bound types
 * directly, so this behavior is testable without a native library loaded —
 * [mobile] defaults to [RealObdMobile] and is only ever swapped for a fake
 * in tests.
 */
class ObdForegroundService : Service(), ObdConnectionEngine.Callbacks {

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

    // Public, not internal: openConnection() below implements the public
    // ObdConnectionEngine.Callbacks interface, and Kotlin requires a
    // function's return type to be at least as visible as the function
    // itself (Callbacks' methods can't be `internal` — Kotlin disallows
    // non-public visibility on interface members).
    @VisibleForTesting
    data class ConnectionHandles(val socket: BluetoothSocket, val session: ObdSession)

    private val binder = LocalBinder()
    private val scope = CoroutineScope(Dispatchers.IO + Job())
    private val listeners = CopyOnWriteArrayList<StatusListener>()

    // Swappable only for tests (never reassigned in production) — see the
    // class doc comment above and DESIGN.md section 3.
    @VisibleForTesting
    internal var mobile: ObdMobile = RealObdMobile

    // The engine actually running connectionLoop() — constructed fresh
    // alongside connectionJob in onStartCommand(), read by
    // reconnectNow()/stopServiceImmediately()/onDestroy() to reach the
    // connection state that now lives on the engine, not this Service.
    // @Volatile: read/written from both this service's own IO-dispatcher
    // coroutine (via onStartCommand()) and the main thread
    // (onStartCommand()'s ACTION_STOP/ACTION_QUIT path, which now tears
    // down the connection synchronously instead of only ever doing it via
    // onDestroy() — see stopServiceImmediately()).
    @Volatile
    @VisibleForTesting
    internal var activeEngine: ObdConnectionEngine? = null
    @Volatile
    @VisibleForTesting
    internal var connectionJob: Job? = null

    @Volatile
    private var latestState: ConnectionState = ConnectionState.Disconnected(retryInSeconds = 0)

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onCreate() {
        super.onCreate()
        // App log is opened once for the whole process, in
        // CarMonitorApplication.onCreate() — not here. It used to be
        // opened here, but that meant it was never opened at all if the
        // user previously tapped Stop (this service never starts unless
        // "Start Scanning" is tapped, DESIGN.md section 7), silently
        // dropping any logging from an Activity used in that state. See
        // docs/defects.md.
        scope.launch { gitBackupLoop() }
        scope.launch { driveBackupLoop() }
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
        // unlike checking whether a connection is already open, which stays
        // true for the whole (multi-second) connect() call, so a second
        // start request arriving mid-connect (e.g. StatusActivity
        // re-requesting permissions after a screen rotation) would
        // otherwise launch a second concurrent connectionLoop().
        if (connectionJob?.isActive != true) {
            val engine = ObdConnectionEngine(callbacks = this, mobile = mobile)
            activeEngine = engine
            connectionJob = scope.launch { engine.connectionLoop() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        activeEngine?.closeConnectionNow()
        // App log is intentionally not closed here — it must stay open
        // for as long as anything in the process might still log to it
        // (e.g. DeviceScanActivity after tapping Stop), not just while
        // this service happens to be running. See CarMonitorApplication.
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
     * Closes the current connection (if any) so connectionLoop's own retry
     * logic immediately attempts a fresh connection using the (possibly
     * just-changed) selected device — see DESIGN.md section 7. Deliberately
     * lighter than stopServiceImmediately(): no connectionJob cancellation,
     * no terminal ConnectionState, no "Start Scanning" required afterward.
     * A no-op if the service isn't currently connecting/connected (e.g.
     * stopped) — the new selection simply becomes what's used whenever the
     * user next taps Start Scanning.
     */
    fun reconnectNow() {
        activeEngine?.closeConnectionNow()
    }

    // ObdConnectionEngine.Callbacks — everything the connect/read/write/
    // backoff loop (ObdConnectionEngine) needs from this Service. See the
    // class doc comment and DESIGN.md section 4.

    override fun onStateChanged(state: ConnectionState) = updateState(state)

    /**
     * The no-connection timeout (NO_CONNECTION_TIMEOUT_MS, checked by the
     * engine) stops the service rather than retrying forever against a
     * dongle that's never going to answer — see stopServiceImmediately().
     */
    override fun onNoConnectionTimeout() {
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
     * InputStream.read()), so closeConnectionNow() is called here too —
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
        activeEngine?.closeConnectionNow()
        updateState(finalState)
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    @VisibleForTesting
    override fun hasBluetoothPermission(): Boolean { // public: see ConnectionHandles' comment above
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return ContextCompat.checkSelfPermission(
                this, Manifest.permission.BLUETOOTH_CONNECT
            ) == PackageManager.PERMISSION_GRANTED
        }
        return true
    }

    @VisibleForTesting
    override fun openConnection(): ConnectionHandles { // public: see ConnectionHandles' comment above
        val bluetoothManager = getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager
        val adapter: BluetoothAdapter = bluetoothManager.adapter
            ?: throw IOException("No Bluetooth adapter on this device")
        if (!adapter.isEnabled) {
            throw IOException("Bluetooth is disabled")
        }

        // No adapter.cancelDiscovery() here: DeviceScanActivity (section 5.1)
        // is responsible for cancelling its own discovery when it finishes or
        // the user navigates away, so by the time a connection attempt reaches
        // here no discovery should be active. Calling cancelDiscovery()
        // speculatively would still risk a SecurityException if BLUETOOTH_SCAN
        // was denied (it's only requested by DeviceScanActivity, not
        // unconditionally at every connection attempt), for no benefit if
        // nothing is actually scanning.
        val device = adapter.getRemoteDevice(mobile.deviceMAC(filesDir.absolutePath))
        val newSocket = device.createRfcommSocketToServiceRecord(SPP_UUID)
        connectSocket(newSocket)

        val newSession = try {
            mobile.newSession(filesDir.absolutePath, sessionListener, anomalyListener)
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
     * found — see ObdConnectionEngine.anomalyCheckLoop for how often that
     * runs. A separate, higher-
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

    // Git backup check cadence is GIT_BACKUP_CHECK_INTERVAL_MS; Go's
    // SyncIfNeeded decides whether real work happens, so this can be coarse.
    private suspend fun gitBackupLoop() {
        while (currentCoroutineContext().isActive) {
            delay(GIT_BACKUP_CHECK_INTERVAL_MS)
            runCatching { Mobile.syncLogsIfNeeded(filesDir.absolutePath) }
                .onFailure { Log.w(TAG, "git backup check failed", it) }
        }
    }

    /**
     * A no-op until a folder is configured (DriveBackupPrefs.getFolderUri()
     * returns null) — see DESIGN.md section 7. Reuses gitBackupLoop's
     * cadence rather than introducing a second interval constant; there's
     * no reason for these to diverge yet.
     */
    private suspend fun driveBackupLoop() {
        while (currentCoroutineContext().isActive) {
            delay(GIT_BACKUP_CHECK_INTERVAL_MS)
            val folderUri = DriveBackupPrefs.getFolderUri(this@ObdForegroundService) ?: continue
            runCatching { DriveBackup.sync(this@ObdForegroundService, File(filesDir, "readings"), folderUri) }
                .onFailure { Log.w(TAG, "Drive backup check failed", it) }
        }
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

    @VisibleForTesting
    internal fun buildNotification(state: ConnectionState): Notification {
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
            is ConnectionState.Connecting -> getString(R.string.notification_connecting, mobile.selectedDeviceName(filesDir.absolutePath))
            is ConnectionState.Connected -> getString(R.string.notification_connected, mobile.selectedDeviceName(filesDir.absolutePath))
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
