package com.carmonitor.app

import android.app.Application
import android.util.Log
import mobile.Mobile

/**
 * Opens the app log for the whole process lifetime, before any Activity
 * or Service runs. Previously only ObdForegroundService.onCreate()
 * called Mobile.initAppLog(), which is never reached at all if the user
 * previously tapped Stop (resuming is always explicit, DESIGN.md
 * section 7) — any logging from an Activity opened in that state (e.g.
 * DeviceScanActivity) was silently dropped. See docs/defects.md.
 *
 * Never explicitly closed: the log should stay open for as long as
 * anything in the process might still log to it, not just while
 * ObdForegroundService happens to be running — Application.onTerminate()
 * is documented as unreliable on real devices anyway, so the OS
 * reclaiming the file handle on process death is the only close this
 * needs.
 */
class CarMonitorApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        try {
            Mobile.initAppLog(filesDir.absolutePath)
        } catch (e: Throwable) {
            // Best-effort, and deliberately catching Throwable (not just
            // Exception): app logging is an optional convenience, and no
            // failure initializing it — including an Error, e.g. a
            // corrupt/missing native library — should be able to crash
            // the whole app over what is, at worst, a logging feature
            // not working. Also what keeps this call safe under
            // Robolectric, which has no native library to load at all.
            Log.w(TAG, "Failed to init app log", e)
        }
    }

    private companion object {
        const val TAG = "CarMonitorApplication"
    }
}
