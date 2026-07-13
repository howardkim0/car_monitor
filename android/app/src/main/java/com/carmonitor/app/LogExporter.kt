package com.carmonitor.app

import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

/**
 * Zips reading log CSVs and the app log for user export via Android's
 * share sheet. Pure file I/O with no Android framework dependencies
 * (beyond java.io/java.util.zip), so directly unit-testable.
 */
object LogExporter {
    fun buildZip(readingsDir: File, appLogFile: File, outputZip: File) {
        ZipOutputStream(outputZip.outputStream()).use { zip ->
            // Zip every readings-*.csv file in readingsDir
            readingsDir.listFiles()?.forEach { file ->
                if (file.isFile && file.name.startsWith("readings-") && file.name.endsWith(".csv")) {
                    addFileToZip(zip, file, file.name)
                }
            }

            // Zip the app.log file if it exists
            if (appLogFile.exists()) {
                addFileToZip(zip, appLogFile, appLogFile.name)
            }

            // Zip the app.log.1 file if it exists (rotated log)
            val appLog1 = File(appLogFile.parentFile, "${appLogFile.name}.1")
            if (appLog1.exists()) {
                addFileToZip(zip, appLog1, appLog1.name)
            }
        }
    }

    private fun addFileToZip(zip: ZipOutputStream, file: File, entryName: String) {
        ZipEntry(entryName).apply {
            zip.putNextEntry(this)
            file.inputStream().use { input ->
                input.copyTo(zip)
            }
            zip.closeEntry()
        }
    }
}
