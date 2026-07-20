package com.carmonitor.app

import java.io.File
import java.time.LocalDate
import java.time.ZoneOffset
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * Tests for DriveBackup.filesToCopy() — pure file-listing logic with no
 * Android framework dependency, so no Robolectric needed. DriveBackup.sync()
 * (the SAF-bound half) is verified against a real device/DHU instead, per
 * docs/dev-setup.md.
 */
class DriveBackupTest {

    @Rule
    @JvmField
    val tempDir = TemporaryFolder()

    @Test
    fun `currentReadingsFileName matches today's UTC date`() {
        val expected = "readings-${LocalDate.now(ZoneOffset.UTC)}.csv"
        assertEquals(expected, DriveBackup.currentReadingsFileName())
    }

    @Test
    fun `filesToCopy includes files not already backed up`() {
        val readingsDir = tempDir.newFolder("readings")
        File(readingsDir, "readings-2026-07-12.csv").writeText("a")
        File(readingsDir, "readings-2026-07-13.csv").writeText("b")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = setOf("readings-2026-07-12.csv"),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertEquals(setOf("readings-2026-07-13.csv"), result.map { it.name }.toSet())
    }

    @Test
    fun `filesToCopy always re-copies today's file even if already backed up`() {
        val readingsDir = tempDir.newFolder("readings")
        File(readingsDir, "readings-2026-07-14.csv").writeText("still growing")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = setOf("readings-2026-07-14.csv"),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertEquals(listOf("readings-2026-07-14.csv"), result.map { it.name })
    }

    @Test
    fun `filesToCopy skips already backed up files other than today's`() {
        val readingsDir = tempDir.newFolder("readings")
        File(readingsDir, "readings-2026-07-12.csv").writeText("a")
        File(readingsDir, "readings-2026-07-13.csv").writeText("b")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = setOf("readings-2026-07-12.csv", "readings-2026-07-13.csv"),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertTrue("nothing left to copy", result.isEmpty())
    }

    @Test
    fun `filesToCopy ignores non-readings files`() {
        val readingsDir = tempDir.newFolder("readings")
        File(readingsDir, "readings-2026-07-12.csv").writeText("a")
        File(readingsDir, "app.log").writeText("should never be included")
        File(readingsDir, "notes.txt").writeText("irrelevant")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = emptySet(),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertEquals(listOf("readings-2026-07-12.csv"), result.map { it.name })
    }

    @Test
    fun `filesToCopy returns empty list for an empty directory`() {
        val readingsDir = tempDir.newFolder("readings")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = emptySet(),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertTrue(result.isEmpty())
    }

    @Test
    fun `filesToCopy returns empty list when the readings directory doesn't exist yet`() {
        val readingsDir = File(tempDir.root, "readings-not-created")

        val result = DriveBackup.filesToCopy(
            readingsDir,
            alreadyBackedUp = emptySet(),
            todayFileName = "readings-2026-07-14.csv"
        )

        assertTrue(result.isEmpty())
    }
}
