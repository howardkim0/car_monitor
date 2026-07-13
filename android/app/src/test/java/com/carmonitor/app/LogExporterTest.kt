package com.carmonitor.app

import java.io.File
import java.util.zip.ZipInputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * Tests for LogExporter.buildZip() — pure file I/O with no Android
 * framework dependencies, so no Robolectric needed.
 */
class LogExporterTest {

    @Rule
    @JvmField
    val tempDir = TemporaryFolder()

    @Test
    fun `buildZip includes all readings CSV files and app logs`() {
        // Create fake CSV files
        val readingsDir = tempDir.newFolder("readings")
        val csv1 = File(readingsDir, "readings-2026-07-12.csv")
        csv1.writeText("pid,name,value,unit,timestamp\n")
        csv1.appendText("0x04,Calculated Engine Load,42.5,%,2026-07-12T12:00:00Z\n")

        val csv2 = File(readingsDir, "readings-2026-07-13.csv")
        csv2.writeText("pid,name,value,unit,timestamp\n")
        csv2.appendText("0x05,Coolant Temperature,85.0,C,2026-07-13T12:00:00Z\n")

        // Create fake app log file
        val appLog = File(tempDir.root, "app.log")
        appLog.writeText("[2026-07-12T12:00:00] INFO: Started\n")
        appLog.appendText("[2026-07-12T12:01:00] DEBUG: Connected\n")

        // Create fake rotated log
        val appLog1 = File(tempDir.root, "app.log.1")
        appLog1.writeText("[2026-07-11T23:59:00] INFO: Previous session\n")

        // Create output zip
        val outputZip = File(tempDir.root, "test.zip")

        // Call buildZip
        LogExporter.buildZip(readingsDir, appLog, outputZip)

        // Verify zip exists and contains expected entries
        assertTrue("Output zip should exist", outputZip.exists())

        val entries = mutableSetOf<String>()
        ZipInputStream(outputZip.inputStream()).use { zip ->
            var entry = zip.nextEntry
            while (entry != null) {
                entries.add(entry.name)
                entry = zip.nextEntry
            }
        }

        assertEquals(
            "Zip should contain readings CSVs and app logs",
            setOf("readings-2026-07-12.csv", "readings-2026-07-13.csv", "app.log", "app.log.1"),
            entries
        )
    }

    @Test
    fun `buildZip skips missing files silently`() {
        val readingsDir = tempDir.newFolder("readings")
        val csv = File(readingsDir, "readings-2026-07-12.csv")
        csv.writeText("pid,name,value,unit,timestamp\n")

        // App log doesn't exist, and neither does its .1 sibling
        val appLog = File(tempDir.root, "app.log")
        val outputZip = File(tempDir.root, "test.zip")

        // Should not throw
        LogExporter.buildZip(readingsDir, appLog, outputZip)

        val entries = mutableSetOf<String>()
        ZipInputStream(outputZip.inputStream()).use { zip ->
            var entry = zip.nextEntry
            while (entry != null) {
                entries.add(entry.name)
                entry = zip.nextEntry
            }
        }

        // Only the CSV should be present; missing app.log and app.log.1 are silently skipped
        assertEquals(
            "Zip should contain only the CSV when app logs are missing",
            setOf("readings-2026-07-12.csv"),
            entries
        )
    }

    @Test
    fun `buildZip with only app log (no CSVs) creates valid zip`() {
        val readingsDir = tempDir.newFolder("readings")
        val appLog = File(tempDir.root, "app.log")
        appLog.writeText("[2026-07-12T12:00:00] INFO: Started\n")

        val outputZip = File(tempDir.root, "test.zip")

        LogExporter.buildZip(readingsDir, appLog, outputZip)

        val entries = mutableSetOf<String>()
        ZipInputStream(outputZip.inputStream()).use { zip ->
            var entry = zip.nextEntry
            while (entry != null) {
                entries.add(entry.name)
                entry = zip.nextEntry
            }
        }

        assertEquals(
            "Zip should contain only app.log",
            setOf("app.log"),
            entries
        )
    }

    @Test
    fun `buildZip preserves file contents`() {
        val readingsDir = tempDir.newFolder("readings")
        val csv = File(readingsDir, "readings-2026-07-12.csv")
        val csvContent = "pid,name,value,unit,timestamp\n0x04,Load,42.5,%,2026-07-12T12:00:00Z\n"
        csv.writeText(csvContent)

        val appLog = File(tempDir.root, "app.log")
        val appLogContent = "[2026-07-12T12:00:00] INFO: Started\n"
        appLog.writeText(appLogContent)

        val outputZip = File(tempDir.root, "test.zip")
        LogExporter.buildZip(readingsDir, appLog, outputZip)

        // Read and verify contents
        ZipInputStream(outputZip.inputStream()).use { zip ->
            var entry = zip.nextEntry
            while (entry != null) {
                val content = zip.readBytes().decodeToString()
                when (entry.name) {
                    "readings-2026-07-12.csv" -> assertEquals(csvContent, content)
                    "app.log" -> assertEquals(appLogContent, content)
                }
                entry = zip.nextEntry
            }
        }
    }
}
