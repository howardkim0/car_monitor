package com.carmonitor.app

import android.content.Context

/**
 * The last update versionCode the user explicitly dismissed via "Check
 * for Updates"' available-update dialog — shared between the manual
 * button and the on-launch automatic check (DESIGN.md section 12) so a
 * dismissed build doesn't keep re-prompting on every app open. Storing
 * the versionCode itself (not just a boolean) means dismissing one
 * update doesn't suppress a later, newer one.
 */
object UpdateDismissalPrefs {
    private const val PREFS_NAME = "car_monitor_prefs"
    private const val PREF_DISMISSED_VERSION_CODE = "dismissedUpdateVersionCode"

    fun isDismissed(context: Context, versionCode: Int): Boolean =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getInt(PREF_DISMISSED_VERSION_CODE, -1) == versionCode

    fun setDismissed(context: Context, versionCode: Int) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putInt(PREF_DISMISSED_VERSION_CODE, versionCode)
            .apply()
    }
}
