package com.carmonitor.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Restarts the foreground service after boot, per DESIGN.md section 7.
 * Registered for BOOT_COMPLETED only (see AndroidManifest.xml); the system
 * delivers this broadcast even to a non-exported receiver.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED) {
            ObdForegroundService.start(context)
        }
    }
}
