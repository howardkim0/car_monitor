package com.carmonitor.app

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * Tests for LogViewer.readTail() and isTruncated() — pure file I/O with no
 * Android framework dependencies, so no Robolectric needed.
 */
class LogViewerTest {

    @Rule
    @JvmField
    val tempDir = TemporaryFolder()

    @Test
    fun `readTail returns null for nonexistent file`() {
        val file = File(tempDir.root, "nonexistent.log")
        assertNull("readTail should return null for nonexistent file", LogViewer.readTail(file))
    }

    @Test
    fun `readTail returns full content when file is smaller than maxBytes`() {
        val file = File(tempDir.root, "app.log")
        val content = "Line 1\nLine 2\nLine 3\n"
        file.writeText(content)

        val result = LogViewer.readTail(file, maxBytes = 1000)
        assertEquals("Should return full content for small file", content, result)
    }

    @Test
    fun `readTail returns empty string for empty file`() {
        val file = File(tempDir.root, "app.log")
        file.writeText("")

        val result = LogViewer.readTail(file)
        assertEquals("Empty file should return empty string, not null", "", result)
    }

    @Test
    fun `readTail returns tail starting at line boundary when file is truncated`() {
        val file = File(tempDir.root, "app.log")
        val lines = (0..100).map { "Line $it with some text content to make it longer\n" }
        file.writeText(lines.joinToString(""))

        val result = LogViewer.readTail(file, maxBytes = 500)

        assertTrue("Result should not be null", result != null)
        result!!
        assertTrue("Result should contain complete trailing lines", result.isNotEmpty())
        // The last line written is always fully present — a truncated tail
        // only ever drops from the *front*, never the end of the file.
        assertTrue("Result should end with the file's last line", result.endsWith(lines.last()))
        // Every line in the result must be one of the original lines verbatim
        // (ignoring the trailing empty element from the final \n) — proof
        // no line was cut mid-way by the tail read.
        result.split('\n').dropLast(1).forEach { line ->
            assertTrue("'$line' should be a complete original line", lines.contains("$line\n"))
        }
    }

    @Test
    fun `readTail starts at line boundary, not mid-line`() {
        val file = File(tempDir.root, "app.log")
        // Three 11-byte lines (33 bytes total): "aaaaaaaaaa\n", "bbbbbbbbbb\n",
        // "cccccccccc\n". maxBytes=15 reads the last 15 bytes — byte 18
        // onward, landing 7 bytes into line2 ("bbb\n" + all of line3) — so
        // after discarding the partial "bbb" fragment up to its newline,
        // the complete, untouched last line should remain.
        val line1 = "a".repeat(10) + "\n"
        val line2 = "b".repeat(10) + "\n"
        val line3 = "c".repeat(10) + "\n"
        file.writeText(line1 + line2 + line3)

        val result = LogViewer.readTail(file, maxBytes = 15)

        assertEquals("Result should be exactly the complete last line, with the " +
            "preceding partial line discarded", line3, result)
    }

    @Test
    fun `isTruncated returns false for small file`() {
        val file = File(tempDir.root, "app.log")
        file.writeText("Small content\n")

        assertFalse("isTruncated should return false for small file", LogViewer.isTruncated(file, maxBytes = 1000))
    }

    @Test
    fun `isTruncated returns true for large file`() {
        val file = File(tempDir.root, "app.log")
        val maxBytes = 1000
        // Create content larger than maxBytes
        val largContent = "x".repeat(maxBytes + 100)
        file.writeText(largContent)

        assertTrue("isTruncated should return true for large file", LogViewer.isTruncated(file, maxBytes = maxBytes))
    }

    @Test
    fun `isTruncated returns false for nonexistent file`() {
        val file = File(tempDir.root, "nonexistent.log")

        assertFalse("isTruncated should return false for nonexistent file", LogViewer.isTruncated(file))
    }

    @Test
    fun `isTruncated returns false when file size equals maxBytes`() {
        val file = File(tempDir.root, "app.log")
        val maxBytes = 100
        val content = "x".repeat(maxBytes)
        file.writeText(content)

        assertFalse("isTruncated should return false when file size equals maxBytes", LogViewer.isTruncated(file, maxBytes = maxBytes))
    }
}
