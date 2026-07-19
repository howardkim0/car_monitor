package com.carmonitor.app

import android.bluetooth.BluetoothManager
import android.content.Context
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import mobile.Mobile

data class CandidateDevice(val mac: String, val name: String, val status: String)

/**
 * Bonded-OBD2-device listing + selection, shared by StatusActivity's
 * "Show Paired Devices" dialog and the Android Auto car screen's
 * PairScannerScreen — one implementation, not two that can drift
 * (DESIGN.md section 4 step 6's AnomalyNotifications precedent).
 * Deliberately bonded-devices-only: active BLE discovery/pairing
 * (DeviceScanActivity) stays phone-only.
 */
object ObdDeviceLister {

    /**
     * Bonded devices that look like OBD2 scanners (DeviceNameFilter) or
     * have been explicitly remembered before (RememberedDevices),
     * labeled with their current status. A SecurityException (Bluetooth
     * permission revoked between check and use) returns an empty list
     * rather than throwing — callers show their own empty-state UI.
     */
    fun listCandidates(context: Context, filesDir: File, isConnected: Boolean): List<CandidateDevice> {
        val adapter = (context.getSystemService(Context.BLUETOOTH_SERVICE) as BluetoothManager).adapter
        val bonded = try {
            adapter?.bondedDevices
                ?.filter { device ->
                    val name = try { device.name } catch (e: SecurityException) { null }
                    DeviceNameFilter.looksLikeObd2Scanner(name) || RememberedDevices.isRemembered(context, device.address)
                }
                ?: emptyList()
        } catch (e: SecurityException) {
            Mobile.logError("Failed to list bonded devices: $e")
            emptyList()
        }

        val selectedMac = Mobile.deviceMAC(filesDir.absolutePath)
        return bonded.map { device ->
            val name = try { device.name } catch (e: SecurityException) { null } ?: device.address
            val status = when {
                device.address.equals(selectedMac, ignoreCase = true) && isConnected ->
                    context.getString(R.string.device_status_connected)
                device.address.equals(selectedMac, ignoreCase = true) ->
                    context.getString(R.string.device_status_selected)
                else -> context.getString(R.string.device_status_paired)
            }
            CandidateDevice(mac = device.address, name = name, status = status)
        }
    }

    /**
     * Persists [mac] as remembered and selected, then invokes
     * [onReconnect] (typically a bound service's reconnectNow(), a
     * no-op if nothing is bound/running). Suspends on Dispatchers.IO
     * for the Mobile.* call — callers launch this on their own
     * lifecycle-scoped coroutine (StatusActivity's existing scope,
     * cancelled in onDestroy(); a car Screen's own lifecycleScope,
     * cancelled when the Screen is destroyed/popped) rather than this
     * object owning one itself. Rethrows on failure (after logging) so
     * each caller can show its own UI-appropriate error feedback.
     */
    suspend fun select(context: Context, filesDir: File, mac: String, name: String, onReconnect: () -> Unit) {
        RememberedDevices.remember(context, mac)
        withContext(Dispatchers.IO) {
            try {
                Mobile.setSelectedDevice(filesDir.absolutePath, mac, name)
            } catch (e: Exception) {
                Mobile.logError("Failed to select device: $e")
                throw e
            }
        }
        onReconnect()
    }
}
