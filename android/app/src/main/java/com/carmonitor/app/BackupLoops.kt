package com.carmonitor.app

import android.util.Log
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive

/**
 * The two periodic backup loops (DESIGN.md section 7) — independent of
 * the Bluetooth connection lifecycle, unlike [ObdConnectionEngine].
 * Extracted out of [ObdForegroundService] for the same reason as that
 * class: [getDriveFolderUri]/[syncDrive] abstract away the
 * `Context`-bound `DriveBackupPrefs`/`DriveBackup` calls so cadence and
 * failure handling are testable under `kotlinx-coroutines-test` virtual
 * time without a Service, Robolectric, or real 5-minute waits.
 */
class BackupLoops(
    private val mobile: ObdMobile = RealObdMobile,
    private val storageDir: String,
    private val getDriveFolderUri: () -> String?,
    private val syncDrive: (folderUri: String) -> Unit,
) {

    suspend fun gitBackupLoop() {
        while (currentCoroutineContext().isActive) {
            delay(BACKUP_CHECK_INTERVAL_MS)
            runCatching { mobile.syncLogsIfNeeded(storageDir) }
                .onFailure { Log.w(TAG, "git backup check failed", it) }
        }
    }

    /** A no-op until a folder is configured ([getDriveFolderUri] returns null). */
    suspend fun driveBackupLoop() {
        while (currentCoroutineContext().isActive) {
            delay(BACKUP_CHECK_INTERVAL_MS)
            val folderUri = getDriveFolderUri() ?: continue
            runCatching { syncDrive(folderUri) }
                .onFailure { Log.w(TAG, "Drive backup check failed", it) }
        }
    }

    companion object {
        private const val TAG = "BackupLoops"
        // Both loops share one cadence — Go's SyncIfNeeded decides whether
        // real git-backup work happens, so this can be coarse; there's no
        // reason for the two loops' intervals to diverge either.
        private const val BACKUP_CHECK_INTERVAL_MS = 5 * 60 * 1_000L
    }
}
