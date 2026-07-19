package com.carmonitor.app

import android.content.Context

/**
 * Whether the user has explicitly stopped monitoring — shared between
 * StatusActivity (the phone screen) and the Android Auto car screen so
 * both surfaces agree on app state. See DESIGN.md section 7: resuming
 * after a stop is always explicit, backed by SharedPreferences (not
 * savedInstanceState) so a genuine process relaunch doesn't silently
 * auto-resume.
 */
object MonitoringPrefs {
    private const val PREFS_NAME = "car_monitor_prefs"
    private const val PREF_STOPPED_BY_USER = "stoppedByUser"

    fun isStoppedByUser(context: Context): Boolean =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE).getBoolean(PREF_STOPPED_BY_USER, false)

    fun setStoppedByUser(context: Context, value: Boolean) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putBoolean(PREF_STOPPED_BY_USER, value)
            .apply()
    }
}
