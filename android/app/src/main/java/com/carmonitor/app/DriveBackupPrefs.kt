package com.carmonitor.app

import android.content.Context

/**
 * Persists the user-chosen Storage Access Framework backup folder (see
 * DESIGN.md section 7's Drive backup loop) across process death — the
 * folder Uri alone is meaningless without also holding the persisted
 * content:// permission grant, which contentResolver.takePersistableUriPermission
 * (called at selection time, in StatusActivity) makes durable
 * independently of this class.
 */
object DriveBackupPrefs {
    private const val PREFS_NAME = "car_monitor_prefs"
    private const val PREF_BACKUP_FOLDER_URI = "driveBackupFolderUri"

    fun getFolderUri(context: Context): String? =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE).getString(PREF_BACKUP_FOLDER_URI, null)

    fun setFolderUri(context: Context, uri: String) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(PREF_BACKUP_FOLDER_URI, uri)
            .apply()
    }
}
