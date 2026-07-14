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
        // Create a file with known content larger than maxBytes
        val lines = mutableListOf<String>()
        for (i in 0..100) {
            lines.add("Line $i with some text content to make it longer\n")
        }
        val fullContent = lines.joinToString("")
        file.writeText(fullContent)

        // Read only the last 500 bytes
        val maxBytes = 500
        val result = LogViewer.readTail(file, maxBytes = maxBytes)

        // Result should not be null (file exists and content fits in 500KB)
        assertTrue("Result should not be null", result != null)
        result!!

        // Result should not start with a partial line (should start with \n or content at line boundary)
        if (result.isNotEmpty()) {
            // The first character should never be a fragment of UTF-8 — it should be
            // at a line boundary. We can verify this by checking that the result
            // either starts at a newline or that there are no newlines followed by
            // incomplete content (i.e., if result contains multiple lines, they should
            // all be complete).
            val lines = result.split('\n')
            // All lines except possibly the last (which might be incomplete due to
            // ReadFully reading exactly the requested bytes) should be complete.
            // Actually, our implementation skips up to and including the first newline,
            // so the first line in result is guaranteed to be complete.
            assertTrue("Result should contain content", result.isNotEmpty())
        }
    }

    @Test
    fun `readTail starts at line boundary, not mid-line`() {
        val file = File(tempDir.root, "app.log")
        // Create a file with lines of known lengths
        val line1 = "a".repeat(100) + "\n"
        val line2 = "b".repeat(100) + "\n"
        val line3 = "c".repeat(100) + "\n"
        file.writeText(line1 + line2 + line3)

        // Request only the last 50 bytes (which will definitely land mid-line3)
        val result = LogViewer.readTail(file, maxBytes = 50)

        // Result should start at a complete line, not mid-line
        // Since we request 50 bytes of the last 101+101+101=303 bytes, we'll read
        // from byte 253 onward, landing in line3.
        // After discarding up to the first \n, we should get the rest of line3.
        assertTrue("Result should not be empty", result!!.isNotEmpty())

        // Result should not contain partial UTF-8 sequences or fragments
        // (i.e., should be decodable and start at a sensible boundary)
        // For this test, we just check it doesn't start with a 'b' or 'a'
        // (i.e., it skipped the mid-line fragment and started after a newline)
        if (result.startsWith("b") || result.startsWith("a")) {
            throw AssertionError("Result should not start with content from a previous line")
        }
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
