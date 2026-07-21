package com.carmonitor.app

import mobile.AnomalyListener
import mobile.Mobile
import mobile.ReadingListener
import mobile.Session

/**
 * Thin seam over the gomobile-bound `Mobile` static calls (DESIGN.md
 * section 3/4) — exists purely so `ObdConnectionEngine`'s connect/
 * backoff loop (and `ObdForegroundService`'s own `openConnection()`/
 * `buildNotification()`/git-backup loop) are testable under virtual
 * time. `mobile.Mobile` is a generated `static native`-method class,
 * not a Kotlin `object`, so it can't be mocked directly the way
 * `ObdDeviceLister`/`ObdDeviceScanner` are (section 11's "Shared logic,
 * not duplicated logic").
 */
interface ObdMobile {
    fun deviceMAC(storageDir: String): String
    fun selectedDeviceName(storageDir: String): String
    fun newSession(storageDir: String, listener: ReadingListener, anomalyListener: AnomalyListener): ObdSession
    fun initCommandCount(): Long
    fun initCommandAt(i: Long): String
    fun syncLogsIfNeeded(storageDir: String)
}

object RealObdMobile : ObdMobile {
    override fun deviceMAC(storageDir: String): String = Mobile.deviceMAC(storageDir)

    override fun selectedDeviceName(storageDir: String): String = Mobile.selectedDeviceName(storageDir)

    override fun newSession(
        storageDir: String,
        listener: ReadingListener,
        anomalyListener: AnomalyListener
    ): ObdSession = RealObdSession(Mobile.newSession(storageDir, listener, anomalyListener))

    override fun initCommandCount(): Long = Mobile.initCommandCount()

    override fun initCommandAt(i: Long): String = Mobile.initCommandAt(i)

    override fun syncLogsIfNeeded(storageDir: String) = Mobile.syncLogsIfNeeded(storageDir)
}

/** Per-connection seam over `mobile.Session` — the instance half of `ObdMobile`'s static half. */
interface ObdSession {
    fun feed(data: ByteArray)
    fun commandCount(): Long
    fun commandAt(i: Long): String
    fun checkAnomalies()
    fun close()
}

class RealObdSession(private val inner: Session) : ObdSession {
    override fun feed(data: ByteArray) = inner.feed(data)
    override fun commandCount(): Long = inner.commandCount()
    override fun commandAt(i: Long): String = inner.commandAt(i)
    override fun checkAnomalies() = inner.checkAnomalies()
    override fun close() = inner.close()
}
