package com.carmonitor.app

import android.Manifest
import android.os.Build
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.junit.Assert.assertArrayEquals

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class BluetoothPermissionsTest {

    @Test
    fun `forConnect returns BLUETOOTH_CONNECT on SDK 31+`() {
        val permissions = BluetoothPermissions.forConnect()
        assertArrayEquals(arrayOf(Manifest.permission.BLUETOOTH_CONNECT), permissions)
    }

    @Test
    fun `forScan returns both BLUETOOTH_CONNECT and BLUETOOTH_SCAN on SDK 31+`() {
        val permissions = BluetoothPermissions.forScan()
        val expected = arrayOf(Manifest.permission.BLUETOOTH_CONNECT, Manifest.permission.BLUETOOTH_SCAN)
        assertArrayEquals(expected, permissions)
    }
}
