package com.carmonitor.app.carapp

import android.bluetooth.BluetoothDevice
import android.content.BroadcastReceiver
import android.content.pm.PackageManager
import androidx.car.app.CarContext
import androidx.car.app.CarToast
import androidx.car.app.Screen
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.model.Template
import androidx.core.content.ContextCompat
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.lifecycleScope
import com.carmonitor.app.BluetoothPermissions
import com.carmonitor.app.CandidateDevice
import com.carmonitor.app.DeviceNameFilter
import com.carmonitor.app.ObdDeviceLister
import com.carmonitor.app.ObdDeviceScanner
import com.carmonitor.app.R
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Lists already-bonded OBD2 devices for one-tap selection, plus a
 * "Scan for Devices" row that discovers and pairs non-bonded ones —
 * see DESIGN.md section 11 for the full reasoning (discovery is
 * buildable natively in a Screen; the real remaining risk is a
 * device's pairing confirmation, if it needs one, rendering on the
 * phone rather than the car display — accepted for this app's target
 * headless OBD2 hardware, backstopped by PAIRING_TIMEOUT_MS rather than
 * a silent hang). Deliberately not ParkedOnlyOnClickListener-gated,
 * unlike LogsScreen's Refresh action — an explicit trade-off, not an
 * oversight.
 *
 * `isConnected` is always passed as false to ObdDeviceLister — the car
 * screen has no StatusListener wired to ObdForegroundService (unlike
 * StatusActivity), so it can't distinguish "selected" from "currently
 * connected." Known display-only simplification, worth revisiting if
 * this proves confusing in practice.
 */
class PairScannerScreen(carContext: CarContext) : Screen(carContext) {

    private var discoveryReceiver: BroadcastReceiver? = null
    private var isScanning = false
    private val discoveredAddresses = mutableSetOf<String>()
    private val discoveredDevices = mutableListOf<BluetoothDevice>()
    private var pairingDevice: BluetoothDevice? = null
    private var pairingTimeoutJob: Job? = null

    init {
        lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onDestroy(owner: LifecycleOwner) {
                discoveryReceiver?.let { ObdDeviceScanner.stopDiscovery(carContext, it) }
            }
        })
    }

    override fun onGetTemplate(): Template {
        val candidates = ObdDeviceLister.listCandidates(carContext, carContext.filesDir, isConnected = false)

        val itemListBuilder = ItemList.Builder()
            .addItem(
                Row.Builder()
                    .setTitle(
                        carContext.getString(
                            if (isScanning) R.string.device_scan_stop_button else R.string.device_scan_button
                        )
                    )
                    .setOnClickListener { onScanToggle() }
                    .build()
            )

        candidates.forEach { candidate ->
            itemListBuilder.addItem(
                Row.Builder()
                    .setTitle("${candidate.name} — ${candidate.status}")
                    .setOnClickListener { onSelectBonded(candidate) }
                    .build()
            )
        }

        discoveredDevices.forEach { device ->
            val pairing = device.address == pairingDevice?.address
            itemListBuilder.addItem(
                Row.Builder()
                    .setTitle(
                        if (pairing) carContext.getString(R.string.device_scan_pairing) else deviceName(device)
                    )
                    .setOnClickListener { onDiscoveredDeviceTap(device) }
                    .build()
            )
        }

        return ListTemplate.Builder()
            .setTitle(carContext.getString(R.string.pair_devices_button))
            .setSingleList(itemListBuilder.build())
            .build()
    }

    private fun onScanToggle() {
        val receiver = discoveryReceiver
        if (receiver != null) {
            ObdDeviceScanner.stopDiscovery(carContext, receiver)
            discoveryReceiver = null
            isScanning = false
            invalidate()
            return
        }

        val missing = BluetoothPermissions.forScan().filter {
            ContextCompat.checkSelfPermission(carContext, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isEmpty()) {
            startScan()
        } else {
            carContext.requestPermissions(missing) { _, _ -> startScan() }
        }
    }

    private fun startScan() {
        if (!ObdDeviceScanner.isLocationEnabled(carContext)) {
            CarToast.makeText(carContext, carContext.getString(R.string.location_services_required), CarToast.LENGTH_LONG).show()
            return
        }

        discoveredAddresses.clear()
        discoveredDevices.clear()
        val receiver = ObdDeviceScanner.startDiscovery(
            carContext,
            object : ObdDeviceScanner.Listener {
                override fun onDeviceFound(device: BluetoothDevice) {
                    if (device.bondState == BluetoothDevice.BOND_BONDED) return
                    if (!discoveredAddresses.add(device.address)) return
                    val name = rawDeviceName(device)
                    if (DeviceNameFilter.looksLikeObd2Scanner(name)) {
                        discoveredDevices.add(device)
                        invalidate()
                    }
                }

                override fun onDiscoveryFinished() {
                    isScanning = false
                    discoveryReceiver = null
                    invalidate()
                }

                override fun onBondStateChanged(device: BluetoothDevice, bondState: Int) {
                    if (device.address != pairingDevice?.address) return
                    pairingTimeoutJob?.cancel()
                    pairingTimeoutJob = null
                    pairingDevice = null
                    when (bondState) {
                        BluetoothDevice.BOND_BONDED -> selectPairedDevice(device)
                        BluetoothDevice.BOND_NONE -> {
                            CarToast.makeText(carContext, carContext.getString(R.string.device_pairing_failed), CarToast.LENGTH_SHORT).show()
                            invalidate()
                        }
                    }
                }
            }
        )

        if (receiver == null) {
            CarToast.makeText(carContext, carContext.getString(R.string.device_scan_start_failed), CarToast.LENGTH_SHORT).show()
            return
        }
        discoveryReceiver = receiver
        isScanning = true
        invalidate()
    }

    private fun onDiscoveredDeviceTap(device: BluetoothDevice) {
        if (pairingDevice != null) return
        pairingDevice = device
        ObdDeviceScanner.pair(device)
        invalidate()
        pairingTimeoutJob = lifecycleScope.launch {
            delay(PAIRING_TIMEOUT_MS)
            pairingDevice = null
            CarToast.makeText(carContext, carContext.getString(R.string.device_pairing_timed_out), CarToast.LENGTH_LONG).show()
            invalidate()
        }
    }

    private fun selectPairedDevice(device: BluetoothDevice) {
        selectDevice(device.address, deviceName(device))
    }

    private fun onSelectBonded(candidate: CandidateDevice) {
        selectDevice(candidate.mac, candidate.name)
    }

    private fun selectDevice(mac: String, name: String) {
        lifecycleScope.launch {
            try {
                // No bound service to reconnect from the car screen — a
                // no-op onReconnect, matching that the selection still
                // applies on the next actual connection attempt either way.
                ObdDeviceLister.select(carContext, carContext.filesDir, mac, name) {}
                CarToast.makeText(carContext, carContext.getString(R.string.device_selected, name), CarToast.LENGTH_SHORT).show()
                screenManager.pop()
            } catch (e: Exception) {
                CarToast.makeText(carContext, carContext.getString(R.string.device_select_failed), CarToast.LENGTH_SHORT).show()
            }
        }
    }

    private fun rawDeviceName(device: BluetoothDevice): String? =
        try { device.name } catch (e: SecurityException) { null }

    private fun deviceName(device: BluetoothDevice): String = rawDeviceName(device) ?: device.address

    companion object {
        private const val PAIRING_TIMEOUT_MS = 20_000L
    }
}
