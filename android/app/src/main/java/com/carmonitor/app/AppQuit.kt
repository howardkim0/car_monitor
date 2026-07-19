package com.carmonitor.app

import android.content.Context
import android.os.Process

/**
 * Shared teardown for "Quit App" — StatusActivity's Quit button and the
 * Android Auto car screen's confirmed-quit action both call this rather
 * than duplicating the sequence. `kill` is injectable so a test can
 * assert the two steps before it without killing the test JVM (DESIGN.md
 * section 10's ACTION_QUIT carve-out still applies to the kill itself:
 * not unit-tested, checked by code review, and — because a Car App
 * Library host may hold this process via a live AIDL binding when it
 * runs — verified manually via the Desktop Head Unit, not automated).
 */
object AppQuit {
    fun quit(context: Context, kill: () -> Unit = { Process.killProcess(Process.myPid()) }) {
        MonitoringPrefs.setStoppedByUser(context, true)
        ObdForegroundService.quit(context)
        kill()
    }
}
