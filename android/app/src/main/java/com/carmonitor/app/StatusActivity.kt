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
import android.provider.Settings
import android.widget.Button
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity

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

    private var boundService: ObdForegroundService? = null
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

        requestPermissions.launch(requiredPermissions())
    }

    override fun onStart() {
        super.onStart()
        bindService(Intent(this, ObdForegroundService::class.java), serviceConnection, Context.BIND_AUTO_CREATE)
        updateBatteryOptimizationButtonVisibility()
    }

    override fun onStop() {
        boundService?.removeStatusListener(this)
        unbindService(serviceConnection)
        boundService = null
        super.onStop()
    }

    override fun onStateChanged(state: ObdForegroundService.ConnectionState) {
        runOnUiThread {
            statusText.text = when (state) {
                is ObdForegroundService.ConnectionState.Connecting -> getString(R.string.status_connecting)
                is ObdForegroundService.ConnectionState.Connected -> getString(R.string.status_connected)
                is ObdForegroundService.ConnectionState.Disconnected ->
                    getString(R.string.status_disconnected, state.retryInSeconds)
                is ObdForegroundService.ConnectionState.PermissionMissing ->
                    getString(R.string.status_permission_missing)
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
}
