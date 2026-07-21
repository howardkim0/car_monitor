package com.carmonitor.app

import java.io.IOException

/**
 * Test double for [ObdConnectionEngine.Callbacks] — records every state
 * change and lets each test script what `hasBluetoothPermission()`/
 * `openConnection()` do, without any real Context/BluetoothManager.
 */
class FakeConnectionEngineCallbacks(
    var permissionGranted: Boolean = true,
) : ObdConnectionEngine.Callbacks {

    val stateChanges = mutableListOf<ObdForegroundService.ConnectionState>()
    var noConnectionTimeoutCallCount = 0

    /** Defaults to always failing — most tests only care about the failure/backoff path. */
    var openConnectionResult: () -> ObdForegroundService.ConnectionHandles = {
        throw IOException("no fake connection configured")
    }

    override fun hasBluetoothPermission(): Boolean = permissionGranted

    override fun openConnection(): ObdForegroundService.ConnectionHandles = openConnectionResult()

    override fun onStateChanged(state: ObdForegroundService.ConnectionState) {
        stateChanges += state
    }

    override fun onNoConnectionTimeout() {
        noConnectionTimeoutCallCount++
    }
}
