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

    private val scope = CoroutineScope(Dispatchers.Main + Job())
    private val discoveredAddresses = mutableSetOf<String>()
    private var pairingDeviceAddress: String? = null
    private var isScanning = false

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
                    if (device.bondState != BluetoothDevice.BOND_BONDED && discoveredAddresses.add(device.address)) {
                        addDeviceRow(availableContainer, device, isPaired = false)
                    }
                }
                BluetoothAdapter.ACTION_DISCOVERY_FINISHED -> {
                    isScanning = false
                    scanButton.text = getString(R.string.device_scan_button)
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
        scanButton.setOnClickListener { startScan() }

        val filter = IntentFilter().apply {
            addAction(BluetoothDevice.ACTION_FOUND)
            addAction(BluetoothAdapter.ACTION_DISCOVERY_FINISHED)
            addAction(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
        }
        ContextCompat.registerReceiver(this, discoveryReceiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)

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

    private fun loadPairedDevices() {
        val adapter = (getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter
        pairedContainer.removeAllViews()
        try {
            adapter?.bondedDevices?.forEach { device -> addDeviceRow(pairedContainer, device, isPaired = true) }
        } catch (e: SecurityException) {
            Mobile.logError("Failed to list bonded devices: $e")
            statusText.text = getString(R.string.bluetooth_permission_needed)
        }
    }

    private fun startScan() {
        val adapter = (getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter ?: return
        if (isScanning) {
            try {
                adapter.cancelDiscovery()
            } catch (e: SecurityException) {
                Mobile.logError("Failed to cancel discovery: $e")
            }
            isScanning = false
            scanButton.text = getString(R.string.device_scan_button)
            return
        }
        availableContainer.removeAllViews()
        discoveredAddresses.clear()
        val started = try {
            adapter.startDiscovery()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to start discovery: $e")
            Toast.makeText(this, getString(R.string.device_scan_start_failed), Toast.LENGTH_SHORT).show()
            false
        }
        if (!started) {
            Toast.makeText(this, getString(R.string.device_scan_start_failed), Toast.LENGTH_SHORT).show()
            return
        }
        isScanning = true
        scanButton.text = getString(R.string.device_scan_stop_button)
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

    private fun deviceName(device: BluetoothDevice): String =
        try { device.name } catch (e: SecurityException) { null } ?: device.address
}

@Suppress("DEPRECATION")
private fun Intent.getBluetoothDeviceExtra(): BluetoothDevice? =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
    } else {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
    }
