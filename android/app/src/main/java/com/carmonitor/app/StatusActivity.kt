package com.carmonitor.app

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.IBinder
import android.os.PowerManager
import android.os.Process
import android.provider.Settings
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.VisibleForTesting
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.FileProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import mobile.Mobile
import java.util.Locale

/**
 * The single status screen DESIGN.md section 3 calls for: shows
 * connected/disconnected state and last readings. All it does beyond that
 * is request permissions and start/bind to [ObdForegroundService] — the
 * service keeps running independently of whether this Activity is visible.
 */
class StatusActivity : AppCompatActivity(), ObdForegroundService.StatusListener {

    private lateinit var statusText: TextView
    private lateinit var readingsText: TextView
    private lateinit var batteryOptimizationButton: Button
    private lateinit var exportButton: Button
    private lateinit var stopButton: Button
    private lateinit var quitButton: Button

    private var boundService: ObdForegroundService? = null

    private val scope = CoroutineScope(Dispatchers.Main + Job())

    @VisibleForTesting
    internal fun exportScopeIsActive(): Boolean = scope.isActive

    @VisibleForTesting
    internal var isBound = false

    // Backed by SharedPreferences, not savedInstanceState: resuming after
    // a stop must be explicit — tapping "Start Scanning" — even after the
    // app is fully closed and reopened, not just across a rotation.
    // savedInstanceState alone would only survive a config-change
    // recreation; a genuine relaunch after the process dies (task swiped
    // away, or the OS reclaiming memory) gets a null savedInstanceState
    // and would silently auto-resume monitoring, which is exactly the
    // implicit behavior this flag exists to prevent. In-memory cache of
    // the persisted value, loaded once in onCreate(); every mutation
    // writes through to prefs immediately.
    private var stoppedByUser = false

    private val latestReadings = linkedMapOf<String, Pair<Double, String>>()

    private val serviceConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            val service = (binder as? ObdForegroundService.LocalBinder)?.getService() ?: return
            boundService = service
            service.addStatusListener(this@StatusActivity)
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            boundService = null
        }
    }

    private val requestPermissions = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) {
        // Whether or not everything was granted, start the service — it
        // polls its own permission state and shows "permission missing" in
        // the notification/status text rather than crashing.
        ObdForegroundService.start(this)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_status)

        statusText = findViewById(R.id.statusText)
        readingsText = findViewById(R.id.readingsText)
        batteryOptimizationButton = findViewById(R.id.batteryOptimizationButton)
        batteryOptimizationButton.setOnClickListener { requestBatteryOptimizationExemption() }
        exportButton = findViewById(R.id.exportButton)
        exportButton.setOnClickListener { exportLogs() }
        stopButton = findViewById(R.id.stopButton)
        stopButton.setOnClickListener { if (stoppedByUser) startScanning() else stopMonitoring() }
        quitButton = findViewById(R.id.quitButton)
        quitButton.setOnClickListener { quitApp() }

        stoppedByUser = loadStoppedByUser()
        if (stoppedByUser) {
            statusText.text = getString(R.string.status_stopped)
            stopButton.text = getString(R.string.start_scanning_button)
        } else {
            requestPermissions.launch(requiredPermissions())
        }
    }

    override fun onStart() {
        super.onStart()
        if (!stoppedByUser) {
            isBound = bindService(Intent(this, ObdForegroundService::class.java), serviceConnection, Context.BIND_AUTO_CREATE)
        }
        updateBatteryOptimizationButtonVisibility()
    }

    override fun onStop() {
        boundService?.removeStatusListener(this)
        if (isBound) {
            unbindService(serviceConnection)
            isBound = false
        }
        boundService = null
        super.onStop()
    }

    override fun onDestroy() {
        // exportLogs() launches on this scope and touches the UI
        // (Toast, startActivity) when it finishes — cancel it here so a
        // still-running export doesn't reach into a destroyed Activity
        // (e.g. after the user backs out mid-export).
        scope.cancel()
        super.onDestroy()
    }

    override fun onStateChanged(state: ObdForegroundService.ConnectionState) {
        runOnUiThread {
            when (state) {
                is ObdForegroundService.ConnectionState.Connecting ->
                    statusText.text = getString(R.string.status_connecting)
                is ObdForegroundService.ConnectionState.Connected ->
                    statusText.text = getString(R.string.status_connected)
                is ObdForegroundService.ConnectionState.Disconnected ->
                    statusText.text = getString(R.string.status_disconnected, state.retryInSeconds)
                is ObdForegroundService.ConnectionState.PermissionMissing ->
                    statusText.text = getString(R.string.status_permission_missing)
                is ObdForegroundService.ConnectionState.TimedOut ->
                    applyStoppedUi(getString(R.string.status_timed_out))
                is ObdForegroundService.ConnectionState.Stopped ->
                    applyStoppedUi(getString(R.string.status_stopped))
            }
        }
    }

    override fun onReading(name: String, value: Double, unit: String) {
        latestReadings[name] = value to unit
        runOnUiThread {
            readingsText.text = latestReadings.entries.joinToString("\n") { (readingName, valueAndUnit) ->
                val (readingValue, readingUnit) = valueAndUnit
                getString(R.string.reading_row_format, readingName, formatValue(readingValue), readingUnit)
            }
        }
    }

    private fun stopMonitoring() {
        applyStoppedUi(getString(R.string.status_stopped))
        ObdForegroundService.stop(this)
    }

    /**
     * Shared by a user-tapped Stop and the service reaching a terminal
     * state on its own (TimedOut): unbind (if bound) and flip the toggle
     * button to "Start Scanning". A Service stays alive as long as it's
     * started OR bound, so stopSelf() alone does nothing while this
     * Activity still holds a live bind — this is the only thing that can
     * actually release it.
     */
    private fun applyStoppedUi(message: String) {
        boundService?.removeStatusListener(this)
        boundService = null
        if (isBound) {
            unbindService(serviceConnection)
            isBound = false
        }
        stoppedByUser = true
        saveStoppedByUser(true)
        statusText.text = message
        stopButton.text = getString(R.string.start_scanning_button)
    }

    /** The explicit-human-input counterpart to applyStoppedUi() — undoes it. */
    private fun startScanning() {
        stoppedByUser = false
        saveStoppedByUser(false)
        stopButton.text = getString(R.string.stop_button)
        statusText.text = getString(R.string.status_connecting)
        requestPermissions.launch(requiredPermissions())
        isBound = bindService(Intent(this, ObdForegroundService::class.java), serviceConnection, Context.BIND_AUTO_CREATE)
    }

    private fun quitApp() {
        // Quitting counts as an explicit stop too — reopening the app
        // afterward should require "Start Scanning" like any other stop,
        // not silently resume just because the process happens to be gone.
        saveStoppedByUser(true)
        // Best-effort: ask the service to tear its connection down first
        // (see ObdForegroundService.stopServiceImmediately()) before the
        // hard process kill below, which takes the service down anyway —
        // same process, no multi-process manifest config.
        ObdForegroundService.quit(this)
        Process.killProcess(Process.myPid())
    }

    private fun loadStoppedByUser(): Boolean =
        getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE).getBoolean(PREF_STOPPED_BY_USER, false)

    private fun saveStoppedByUser(value: Boolean) {
        getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putBoolean(PREF_STOPPED_BY_USER, value)
            .apply()
    }

    private fun formatValue(value: Double): String =
        if (value == value.toLong().toDouble()) value.toLong().toString() else "%.1f".format(value)

    private fun requiredPermissions(): Array<String> = buildList {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            add(Manifest.permission.BLUETOOTH_CONNECT)
        } else {
            add(Manifest.permission.ACCESS_FINE_LOCATION)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            add(Manifest.permission.POST_NOTIFICATIONS)
        }
    }.toTypedArray()

    private fun updateBatteryOptimizationButtonVisibility() {
        val powerManager = getSystemService(Context.POWER_SERVICE) as PowerManager
        batteryOptimizationButton.visibility =
            if (powerManager.isIgnoringBatteryOptimizations(packageName)) {
                android.view.View.GONE
            } else {
                android.view.View.VISIBLE
            }
    }

    private fun requestBatteryOptimizationExemption() {
        val intent = Intent(
            Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
            Uri.parse("package:$packageName")
        )
        startActivity(intent)
    }

    private fun exportLogs() {
        scope.launch(Dispatchers.IO) {
            try {
                val timestamp = SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(Date())
                val readingsDir = File(filesDir, "readings")
                val appLogFile = File(filesDir, "app.log")
                val outputZip = File(cacheDir, "car_monitor_logs_$timestamp.zip")

                LogExporter.buildZip(readingsDir, appLogFile, outputZip)

                val uri = FileProvider.getUriForFile(
                    this@StatusActivity,
                    "${packageName}.fileprovider",
                    outputZip
                )

                runOnUiThread {
                    try {
                        val shareIntent = Intent(Intent.ACTION_SEND).apply {
                            type = "application/zip"
                            putExtra(Intent.EXTRA_STREAM, uri)
                            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                        }
                        startActivity(Intent.createChooser(shareIntent, getString(R.string.export_logs_chooser_title)))
                    } catch (e: Exception) {
                        Mobile.logError("Failed to show share sheet: $e")
                        Toast.makeText(
                            this@StatusActivity,
                            getString(R.string.export_logs_failed),
                            Toast.LENGTH_SHORT
                        ).show()
                    }
                }
            } catch (e: Exception) {
                Mobile.logError("Log export failed: $e")
                runOnUiThread {
                    Toast.makeText(
                        this@StatusActivity,
                        getString(R.string.export_logs_failed),
                        Toast.LENGTH_SHORT
                    ).show()
                }
            }
        }
    }

    companion object {
        private const val PREFS_NAME = "car_monitor_prefs"
        private const val PREF_STOPPED_BY_USER = "stoppedByUser"
    }
}
