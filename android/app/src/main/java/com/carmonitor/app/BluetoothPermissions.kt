package com.carmonitor.app

import android.Manifest
import android.os.Build

/**
 * Runtime Bluetooth permissions, factored out of StatusActivity so
 * DeviceScanActivity (and StatusActivity's own paired-devices dialog)
 * can request exactly what each needs — see DESIGN.md section 5.1/8.
 */
object BluetoothPermissions {
    /** Needed to connect to an already-paired device, or query bonded
     * devices/their names. */
    fun forConnect(): Array<String> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            arrayOf(Manifest.permission.BLUETOOTH_CONNECT)
        } else {
            arrayOf(Manifest.permission.ACCESS_FINE_LOCATION)
        }

    /** Additionally needed to discover new (unpaired) nearby devices. */
    fun forScan(): Array<String> =
        forConnect() + if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            arrayOf(Manifest.permission.BLUETOOTH_SCAN)
        } else {
            emptyArray()
        }
}
