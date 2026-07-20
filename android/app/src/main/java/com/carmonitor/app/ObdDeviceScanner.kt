package com.carmonitor.app

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.location.LocationManager
import android.os.Build
import androidx.core.content.ContextCompat
import mobile.Mobile

/**
 * Context-parameterized BLE discovery + pairing engine — the same raw
 * primitives DeviceScanActivity uses inline, factored out so a car
 * Screen (CarContext, which extends ContextWrapper same as an Activity)
 * can drive them identically. Deliberately does no OBD2-specific
 * filtering/dedup itself — that stays the caller's job (see
 * PairScannerScreen), same as DeviceScanActivity applies it in its own
 * receiver handler today. Used by PairScannerScreen; DeviceScanActivity
 * keeps its own existing, already-tested inline implementation for now
 * — see DESIGN.md section 11 for why unifying the two isn't done yet.
 */
object ObdDeviceScanner {

    interface Listener {
        fun onDeviceFound(device: BluetoothDevice)
        fun onDiscoveryFinished()
        fun onBondStateChanged(device: BluetoothDevice, bondState: Int)
    }

    /**
     * Registers a receiver for ACTION_FOUND/ACTION_DISCOVERY_FINISHED/
     * ACTION_BOND_STATE_CHANGED and starts discovery. Returns the
     * receiver (pass to stopDiscovery() when done) or null if discovery
     * couldn't be started (adapter unavailable/disabled, permission
     * denied) — nothing is left registered in that case.
     */
    fun startDiscovery(context: Context, listener: Listener): BroadcastReceiver? {
        val adapter = (context.getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter
            ?: return null

        val receiver = object : BroadcastReceiver() {
            override fun onReceive(receiverContext: Context, intent: Intent) {
                when (intent.action) {
                    BluetoothDevice.ACTION_FOUND -> {
                        val device = intent.getBluetoothDeviceExtra() ?: return
                        listener.onDeviceFound(device)
                    }
                    BluetoothAdapter.ACTION_DISCOVERY_FINISHED -> listener.onDiscoveryFinished()
                    BluetoothDevice.ACTION_BOND_STATE_CHANGED -> {
                        val device = intent.getBluetoothDeviceExtra() ?: return
                        val bondState = intent.getIntExtra(BluetoothDevice.EXTRA_BOND_STATE, BluetoothDevice.BOND_NONE)
                        listener.onBondStateChanged(device, bondState)
                    }
                }
            }
        }
        val filter = IntentFilter().apply {
            addAction(BluetoothDevice.ACTION_FOUND)
            addAction(BluetoothAdapter.ACTION_DISCOVERY_FINISHED)
            addAction(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
        }
        // RECEIVER_EXPORTED, not RECEIVER_NOT_EXPORTED: these three
        // actions are sent by the Bluetooth stack, a privileged system
        // process that doesn't run under this app's own UID — see
        // DeviceScanActivity and docs/defects.md for the regression this
        // guards against.
        ContextCompat.registerReceiver(context, receiver, filter, ContextCompat.RECEIVER_EXPORTED)

        val started = try {
            adapter.startDiscovery()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to start discovery: $e")
            false
        }
        if (!started) {
            context.unregisterReceiver(receiver)
            return null
        }
        return receiver
    }

    fun stopDiscovery(context: Context, receiver: BroadcastReceiver) {
        try {
            (context.getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter?.cancelDiscovery()
        } catch (e: SecurityException) {
            // Permission revoked mid-flow; nothing to clean up either way.
        }
        try {
            context.unregisterReceiver(receiver)
        } catch (e: IllegalArgumentException) {
            // Already unregistered — fine.
        }
    }

    /** This app never implements pairing/PIN entry itself — createBond() triggers Android's own system pairing dialog. */
    fun pair(device: BluetoothDevice) {
        try {
            device.createBond()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to create bond: $e")
        }
    }

    /**
     * On API < 31, classic Bluetooth discovery silently returns zero
     * ACTION_FOUND broadcasts if Location Services is off — no
     * exception, startDiscovery() still returns true. API 31+ is exempt
     * via the BLUETOOTH_SCAN manifest declaration's neverForLocation
     * flag (DESIGN.md section 8). Mirrors DeviceScanActivity's own check.
     */
    fun isLocationEnabled(context: Context): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return true
        }
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            val locationManager = context.getSystemService(Context.LOCATION_SERVICE) as? LocationManager
            locationManager?.isLocationEnabled ?: true
        } else {
            @Suppress("DEPRECATION")
            val mode = android.provider.Settings.Secure.getInt(
                context.contentResolver,
                android.provider.Settings.Secure.LOCATION_MODE,
                android.provider.Settings.Secure.LOCATION_MODE_OFF
            )
            mode != android.provider.Settings.Secure.LOCATION_MODE_OFF
        }
    }
}

@Suppress("DEPRECATION")
private fun Intent.getBluetoothDeviceExtra(): BluetoothDevice? =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
    } else {
        getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
    }
