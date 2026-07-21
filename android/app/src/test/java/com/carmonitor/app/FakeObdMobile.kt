package com.carmonitor.app

import mobile.AnomalyListener
import mobile.ReadingListener

/**
 * Test double for [ObdMobile] — no native/JNI touch, fully in-memory.
 * Lets `ObdConnectionEngineTest` (and any `ObdForegroundServiceTest`
 * case that needs `openConnection()`'s success path or
 * `buildNotification()`'s device-name branches) run without ever
 * loading `mobile.Mobile`'s native library.
 */
class FakeObdMobile(
    var deviceMac: String = "AA:BB:CC:DD:EE:FF",
    var selectedName: String = "Fake Device",
    var initCommands: List<String> = emptyList(),
) : ObdMobile {

    var newSessionResult: () -> ObdSession = { FakeObdSession() }
    var newSessionThrows: Exception? = null
    val newSessionCalls = mutableListOf<String>()
    var syncLogsCallCount = 0

    override fun deviceMAC(storageDir: String): String = deviceMac

    override fun selectedDeviceName(storageDir: String): String = selectedName

    override fun newSession(
        storageDir: String,
        listener: ReadingListener,
        anomalyListener: AnomalyListener
    ): ObdSession {
        newSessionCalls += storageDir
        newSessionThrows?.let { throw it }
        return newSessionResult()
    }

    override fun initCommandCount(): Long = initCommands.size.toLong()

    override fun initCommandAt(i: Long): String = initCommands.getOrElse(i.toInt()) { "" }

    override fun syncLogsIfNeeded(storageDir: String) {
        syncLogsCallCount++
    }
}
