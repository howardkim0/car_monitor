package com.carmonitor.app

import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.documentfile.provider.DocumentFile
import java.io.File
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

/**
 * Copies readings-*.csv files (never app.log/app.log.1) into a
 * user-chosen Storage Access Framework folder — see DESIGN.md section
 * 7's Drive backup loop. filesToCopy() is pure file-listing logic with
 * no Android framework dependency, so directly unit-testable like
 * LogExporter; sync() is the SAF-bound half that actually writes,
 * verified against a real device/DHU per docs/dev-setup.md rather than
 * Robolectric.
 */
object DriveBackup {
    private const val TAG = "DriveBackup"
    private val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd")

    /** Matches internal/storage.FileStore's readings-YYYY-MM-DD.csv naming — UTC, same reasoning as DESIGN.md section 6.1. */
    fun currentReadingsFileName(): String = "readings-${LocalDate.now(ZoneOffset.UTC).format(DATE_FORMAT)}.csv"

    /**
     * Readings files that still need copying: anything not already in
     * alreadyBackedUp, plus todayFileName every time — that file keeps
     * growing through the day, unlike already-rotated (immutable) files,
     * which only need to be copied once.
     */
    fun filesToCopy(readingsDir: File, alreadyBackedUp: Set<String>, todayFileName: String): List<File> {
        val readingsFiles = readingsDir.listFiles() ?: return emptyList()
        return readingsFiles.filter { file ->
            file.isFile &&
                file.name.startsWith("readings-") &&
                file.name.endsWith(".csv") &&
                (file.name == todayFileName || file.name !in alreadyBackedUp)
        }
    }

    /**
     * Best-effort: a revoked grant (Drive app uninstalled, permission
     * pulled in Android settings) or any other I/O failure is caught and
     * logged, never thrown — matches git-backup's "log and retry next
     * cycle" resilience (DESIGN.md section 7).
     */
    fun sync(context: Context, readingsDir: File, folderUriString: String) {
        try {
            val folder = DocumentFile.fromTreeUri(context, Uri.parse(folderUriString)) ?: return
            val existingNames = folder.listFiles().mapNotNull { it.name }.toSet()

            for (file in filesToCopy(readingsDir, existingNames, currentReadingsFileName())) {
                val target = folder.findFile(file.name)?.takeIf { it.isFile }
                    ?: folder.createFile("text/csv", file.name)
                    ?: continue
                context.contentResolver.openOutputStream(target.uri, "w")?.use { output ->
                    file.inputStream().use { input -> input.copyTo(output) }
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Drive backup sync failed", e)
        }
    }
}
