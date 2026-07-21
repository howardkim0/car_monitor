package com.carmonitor.app

/** Test double for [ObdSession] — no native/JNI touch, fully in-memory. */
class FakeObdSession(
    var commands: List<String> = emptyList(),
) : ObdSession {

    val fedData = mutableListOf<ByteArray>()
    var feedThrows: Exception? = null
    var checkAnomaliesCallCount = 0
    var closeCallCount = 0

    override fun feed(data: ByteArray) {
        feedThrows?.let { throw it }
        fedData += data
    }

    override fun commandCount(): Long = commands.size.toLong()

    override fun commandAt(i: Long): String = commands.getOrElse(i.toInt()) { "" }

    override fun checkAnomalies() {
        checkAnomaliesCallCount++
    }

    override fun close() {
        closeCallCount++
    }
}
