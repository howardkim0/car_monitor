package com.carmonitor.app

import java.io.File
import java.io.RandomAccessFile

object LogViewer {
    const val TAIL_BYTES = 200 * 1024 // 200KB

    /**
     * Reads the last [maxBytes] of [file] as text, discarding a leading
     * partial line so the result starts cleanly at a line boundary.
     * Returns null if the file doesn't exist. An empty (0-byte) file
     * returns an empty string, not null.
     */
    fun readTail(file: File, maxBytes: Int = TAIL_BYTES): String? {
        if (!file.exists()) return null
        val length = file.length()
        val readFrom = maxOf(0L, length - maxBytes)
        RandomAccessFile(file, "r").use { raf ->
            raf.seek(readFrom)
            val bytes = ByteArray((length - readFrom).toInt())
            raf.readFully(bytes)
            var text = String(bytes, Charsets.UTF_8)
            // If we didn't start at byte 0, we likely landed mid-line —
            // drop everything up to and including the first newline so
            // the displayed text starts at a real line boundary, not a
            // truncated fragment of a log line.
            if (readFrom > 0) {
                val firstNewline = text.indexOf('\n')
                text = if (firstNewline >= 0) text.substring(firstNewline + 1) else ""
            }
            return text
        }
    }

    /** True if [file] is larger than [maxBytes] — i.e. readTail() truncated it. */
    fun isTruncated(file: File, maxBytes: Int = TAIL_BYTES): Boolean =
        file.exists() && file.length() > maxBytes
}
