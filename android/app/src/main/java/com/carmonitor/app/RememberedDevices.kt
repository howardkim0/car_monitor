package com.carmonitor.app

import android.content.Context

/**
 * MAC addresses of devices the user has explicitly selected at least
 * once, even if their advertised name doesn't look like an OBD2
 * scanner (DeviceNameFilter). Most cheap ELM327 clones can't be
 * renamed, so once a user has confirmed — via "Show More" or
 * otherwise — that an oddly-named device really is their scanner, it
 * stays visible in both paired-devices listings from then on,
 * regardless of name.
 *
 * Kotlin-side SharedPreferences, not Go: this is UI-filter override
 * state, not device protocol data, so it doesn't belong in
 * internal/device alongside SaveSelected/LoadSelected (DESIGN.md
 * section 5.1).
 */
object RememberedDevices {
    private const val PREFS_NAME = "remembered_devices"
    private const val KEY_MACS = "macs"

    fun remember(context: Context, mac: String) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val current = prefs.getStringSet(KEY_MACS, emptySet()) ?: emptySet()
        // Never mutate the Set returned by getStringSet() in place —
        // build a fresh one, per SharedPreferences' documented contract.
        prefs.edit().putStringSet(KEY_MACS, current + mac.uppercase()).apply()
    }

    fun isRemembered(context: Context, mac: String): Boolean {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val current = prefs.getStringSet(KEY_MACS, emptySet()) ?: emptySet()
        return current.contains(mac.uppercase())
    }
}
