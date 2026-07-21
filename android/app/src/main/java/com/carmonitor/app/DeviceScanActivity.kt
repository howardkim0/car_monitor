package com.carmonitor.app

import android.Manifest
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.VisibleForTesting
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import mobile.Mobile

/**
 * "Pair Bluetooth OBD2 Scanners" screen (DESIGN.md section 5.1): lists
 * already-bonded devices for one-tap selection, and separately runs
 * discovery for nearby unpaired ones — tapping one triggers Android's
 * own system pairing dialog via createBond(), then selects it once
 * bonding completes. This app never implements pairing/PIN entry itself.
 */
class DeviceScanActivity : AppCompatActivity() {

    private lateinit var statusText: TextView
    private lateinit var scanButton: Button
    private lateinit var pairedContainer: LinearLayout
    private lateinit var availableContainer: LinearLayout
    private lateinit var showMoreButton: Button

    private val scope = CoroutineScope(Dispatchers.Main + Job())
    private val discoveredAddresses = mutableSetOf<String>()
    // Every device found this scan session, filtered or not — lets
    // "Show More" reveal ones already hidden by DeviceNameFilter
    // without needing to restart the scan.
    private val discoveredDevices = mutableListOf<BluetoothDevice>()
    private var pairingDeviceAddress: String? = null
    private var isScanning = false
    private var showAllDevices = false

    private val requestPermissions = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { results ->
        if (results.values.all { it }) {
            loadPairedDevices()
        } else {
            statusText.text = getString(R.string.bluetooth_permission_needed)
        }
    }

    private val discoveryReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.action) {
                BluetoothDevice.ACTION_FOUND -> {
                    val device = intent.getBluetoothDeviceExtra() ?: return
                    val looksLikeObd2 = DeviceNameFilter.looksLikeObd2Scanner(rawDeviceName(device))
                    logDebug(
                        "ACTION_FOUND: bondState=${device.bondState} " +
                            "alreadySeen=${discoveredAddresses.contains(device.address)} " +
                            "looksLikeObd2=$looksLikeObd2"
                    )
                    if (device.bondState != BluetoothDevice.BOND_BONDED && discoveredAddresses.add(device.address)) {
                        discoveredDevices.add(device)
                        if (looksLikeObd2 || showAllDevices) {
                            addDeviceRow(availableContainer, device, isPaired = false)
                            statusText.text = getString(R.string.device_scan_found_count, availableContainer.childCount)
                        }
                    }
                }
                BluetoothAdapter.ACTION_DISCOVERY_FINISHED -> {
                    logDebug("ACTION_DISCOVERY_FINISHED: found=${availableContainer.childCount}")
                    isScanning = false
                    scanButton.text = getString(R.string.device_scan_button)
                    statusText.text = getString(R.string.device_scan_finished_count, availableContainer.childCount)
                }
                BluetoothDevice.ACTION_BOND_STATE_CHANGED -> {
                    val device = intent.getBluetoothDeviceExtra() ?: return
                    if (device.address == pairingDeviceAddress) {
                        when (intent.getIntExtra(BluetoothDevice.EXTRA_BOND_STATE, BluetoothDevice.BOND_NONE)) {
                            BluetoothDevice.BOND_BONDED -> {
                                pairingDeviceAddress = null
                                selectDeviceAndFinish(device)
                            }
                            BluetoothDevice.BOND_NONE -> {
                                pairingDeviceAddress = null
                                Toast.makeText(this@DeviceScanActivity, getString(R.string.device_pairing_failed), Toast.LENGTH_SHORT).show()
                            }
                        }
                    }
                }
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_device_scan)

        statusText = findViewById(R.id.deviceScanStatusText)
        scanButton = findViewById(R.id.scanButton)
        pairedContainer = findViewById(R.id.pairedDevicesContainer)
        availableContainer = findViewById(R.id.availableDevicesContainer)
        showMoreButton = findViewById(R.id.showMoreButton)
        scanButton.setOnClickListener { startScan() }
        showMoreButton.setOnClickListener { showAllDiscoveredDevices() }

        val filter = IntentFilter().apply {
            addAction(BluetoothDevice.ACTION_FOUND)
            addAction(BluetoothAdapter.ACTION_DISCOVERY_FINISHED)
            addAction(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
        }
        // RECEIVER_EXPORTED, not RECEIVER_NOT_EXPORTED: these three
        // actions are sent by the Bluetooth stack, a privileged system
        // process that doesn't run under this app's own UID.
        // RECEIVER_NOT_EXPORTED silently drops broadcasts from
        // processes like that (see DESIGN.md section 5.1 and
        // docs/defects.md) — startDiscovery() still returns true and
        // nothing errors, the receiver just never hears about any
        // device found, ever, regardless of how many are actually in
        // range. Safe to export: all three are AOSP protected
        // broadcasts (<protected-broadcast> in the platform manifest),
        // so no third-party app can spoof them.
        ContextCompat.registerReceiver(this, discoveryReceiver, filter, ContextCompat.RECEIVER_EXPORTED)

        requestPermissions.launch(BluetoothPermissions.forScan())
    }

    override fun onDestroy() {
        scope.cancel()
        try {
            (getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter?.cancelDiscovery()
        } catch (e: SecurityException) {
            // Permission revoked mid-flow; nothing to clean up either way.
        }
        unregisterReceiver(discoveryReceiver)
        super.onDestroy()
    }

    // Off the main thread: the gomobile-bound Mobile class loads a native
    // library on first touch, which both belongs off the UI/BroadcastReceiver
    // thread and (in test builds under Robolectric, which has no native lib
    // to load) must not run synchronously or every activity/receiver test
    // exercising these call sites fails with UnsatisfiedLinkError.
    private fun logDebug(message: String) {
        scope.launch(Dispatchers.IO) { Mobile.logDebug(message) }
    }

    private fun loadPairedDevices() {
        val adapter = (getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter
        pairedContainer.removeAllViews()
        try {
            adapter?.bondedDevices
                ?.filter { device ->
                    DeviceNameFilter.looksLikeObd2Scanner(rawDeviceName(device)) ||
                        RememberedDevices.isRemembered(this, device.address)
                }
                ?.forEach { device -> addDeviceRow(pairedContainer, device, isPaired = true) }
        } catch (e: SecurityException) {
            Mobile.logError("Failed to list bonded devices: $e")
            statusText.text = getString(R.string.bluetooth_permission_needed)
        }
    }

    /**
     * On API < 31, classic Bluetooth discovery silently returns zero
     * ACTION_FOUND broadcasts if the system Location Services toggle is
     * off — no exception, startDiscovery() still returns true. API 31+ is
     * exempt via the BLUETOOTH_SCAN manifest declaration's
     * neverForLocation flag (see DESIGN.md section 8), so this only needs
     * to check on older versions.
     */
    @VisibleForTesting
    internal fun isLocationEnabled(): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return true
        }
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            val locationManager = getSystemService(Context.LOCATION_SERVICE) as? android.location.LocationManager
            locationManager?.isLocationEnabled ?: true
        } else {
            @Suppress("DEPRECATION")
            val mode = android.provider.Settings.Secure.getInt(
                contentResolver,
                android.provider.Settings.Secure.LOCATION_MODE,
                android.provider.Settings.Secure.LOCATION_MODE_OFF
            )
            mode != android.provider.Settings.Secure.LOCATION_MODE_OFF
        }
    }

    private fun startScan() {
        val adapter = (getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter ?: return
        if (isScanning) {
            logDebug("Scan stopped by user: found=${availableContainer.childCount}")
            try {
                adapter.cancelDiscovery()
            } catch (e: SecurityException) {
                Mobile.logError("Failed to cancel discovery: $e")
            }
            isScanning = false
            scanButton.text = getString(R.string.device_scan_button)
            statusText.text = getString(R.string.device_scan_finished_count, availableContainer.childCount)
            return
        }
        val locationEnabled = isLocationEnabled()
        logDebug("Scan requested: sdkInt=${Build.VERSION.SDK_INT} locationEnabled=$locationEnabled")
        if (!locationEnabled) {
            statusText.text = getString(R.string.location_services_required)
            return
        }
        availableContainer.removeAllViews()
        discoveredAddresses.clear()
        discoveredDevices.clear()
        showAllDevices = false
        val started = try {
            adapter.startDiscovery()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to start discovery: $e")
            false
        }
        logDebug("adapter.startDiscovery() returned $started")
        if (!started) {
            // Single Toast for both failure modes — a denied permission
            // (caught above, mapped to false) and startDiscovery() itself
            // returning false (adapter disabled, discovery already
            // running) are both "couldn't start," no need to tell them apart.
            Toast.makeText(this, getString(R.string.device_scan_start_failed), Toast.LENGTH_SHORT).show()
            return
        }
        isScanning = true
        scanButton.text = getString(R.string.device_scan_stop_button)
        statusText.text = getString(R.string.device_scan_found_count, 0)
    }

    // Most cheap ELM327 clones can't be renamed, so the name filter
    // needs a real bypass (DESIGN.md section 5.1): reveal every device
    // found so far this session (discoveredDevices already has them,
    // filtered or not) and stop filtering new ACTION_FOUND results too.
    private fun showAllDiscoveredDevices() {
        showAllDevices = true
        availableContainer.removeAllViews()
        discoveredDevices.forEach { device -> addDeviceRow(availableContainer, device, isPaired = false) }
        statusText.text = getString(
            if (isScanning) R.string.device_scan_found_count else R.string.device_scan_finished_count,
            availableContainer.childCount
        )
    }

    private fun addDeviceRow(container: LinearLayout, device: BluetoothDevice, isPaired: Boolean) {
        val button = Button(this)
        button.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { topMargin = 8 }
        val name = deviceName(device)
        button.text = "$name (${device.address})"
        button.setOnClickListener {
            if (isPaired) {
                selectDeviceAndFinish(device)
            } else {
                pairDevice(device, button)
            }
        }
        container.addView(button)
    }

    private fun pairDevice(device: BluetoothDevice, button: Button) {
        pairingDeviceAddress = device.address
        button.isEnabled = false
        button.text = getString(R.string.device_scan_pairing)
        try {
            device.createBond()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to create bond: $e")
            pairingDeviceAddress = null
            button.isEnabled = true
            button.text = deviceName(device)
            Toast.makeText(this, getString(R.string.device_pairing_failed), Toast.LENGTH_SHORT).show()
        }
    }

    private fun selectDeviceAndFinish(device: BluetoothDevice) {
        val name = deviceName(device)
        // Selecting any device — filter-matching or only reached via
        // "Show More" — is a strong signal it's the driver's OBD2
        // scanner, so it stays visible in both paired-devices listings
        // from now on regardless of name (DESIGN.md section 5.1).
        RememberedDevices.remember(this, device.address)
        scope.launch(Dispatchers.IO) {
            try {
                Mobile.setSelectedDevice(filesDir.absolutePath, device.address, name)
                runOnUiThread {
                    Toast.makeText(this@DeviceScanActivity, getString(R.string.device_selected, name), Toast.LENGTH_SHORT).show()
                    setResult(RESULT_OK)
                    finish()
                }
            } catch (e: Exception) {
                Mobile.logError("Failed to select device: $e")
                runOnUiThread {
                    Toast.makeText(this@DeviceScanActivity, getString(R.string.device_select_failed), Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    private fun rawDeviceName(device: BluetoothDevice): String? =
        try { device.name } catch (e: SecurityException) { null }

    private fun deviceName(device: BluetoothDevice): String =
        rawDeviceName(device) ?: device.address
}

@Suppress("DEPRECATION")
private fun Intent.getBluetoothDeviceExtra(): BluetoothDevice? =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
    } else {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
    }
