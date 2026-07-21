package com.carmonitor.app

import io.mockk.mockk
import mobile.AnomalyListener
import mobile.ReadingListener
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Sanity tests for the [FakeObdMobile]/[FakeObdSession] test doubles
 * themselves — not yet wired into production code (see
 * docs/plan-obd-service-testability.md's PR 1/PR 2 split). Confirms the
 * fakes behave as configured before `ObdConnectionEngineTest` and
 * `ObdForegroundServiceTest` start relying on them.
 */
class FakeObdMobileTest {

    @Test
    fun `deviceMAC and selectedDeviceName return the configured values`() {
        val mobile = FakeObdMobile(deviceMac = "11:22:33:44:55:66", selectedName = "Garage OBDLink")

        assertEquals("11:22:33:44:55:66", mobile.deviceMAC("storageDir"))
        assertEquals("Garage OBDLink", mobile.selectedDeviceName("storageDir"))
    }

    @Test
    fun `newSession records the call and returns the configured session`() {
        val session = FakeObdSession()
        val mobile = FakeObdMobile().apply { newSessionResult = { session } }
        val listener = mockk<ReadingListener>()
        val anomalyListener = mockk<AnomalyListener>()

        val result = mobile.newSession("storageDir", listener, anomalyListener)

        assertSame(session, result)
        assertEquals(listOf("storageDir"), mobile.newSessionCalls)
    }

    @Test
    fun `newSession rethrows the configured exception`() {
        val mobile = FakeObdMobile().apply { newSessionThrows = IllegalStateException("boom") }

        try {
            mobile.newSession("storageDir", mockk(), mockk())
            org.junit.Assert.fail("expected newSession to throw")
        } catch (e: IllegalStateException) {
            assertEquals("boom", e.message)
        }
    }

    @Test
    fun `initCommandCount and initCommandAt reflect the configured list`() {
        val mobile = FakeObdMobile(initCommands = listOf("ATE0", "ATL0", "ATSP0"))

        assertEquals(3L, mobile.initCommandCount())
        assertEquals("ATE0", mobile.initCommandAt(0))
        assertEquals("ATSP0", mobile.initCommandAt(2))
    }

    @Test
    fun `initCommandAt returns empty string out of range, matching Mobile's own convention`() {
        val mobile = FakeObdMobile(initCommands = listOf("ATE0"))

        assertEquals("", mobile.initCommandAt(5))
    }

    @Test
    fun `syncLogsIfNeeded counts calls`() {
        val mobile = FakeObdMobile()

        mobile.syncLogsIfNeeded("storageDir")
        mobile.syncLogsIfNeeded("storageDir")

        assertEquals(2, mobile.syncLogsCallCount)
    }

    @Test
    fun `FakeObdSession feed records data and can be configured to throw`() {
        val session = FakeObdSession()
        session.feed(byteArrayOf(1, 2, 3))

        assertEquals(1, session.fedData.size)
        assertTrue(session.fedData[0].contentEquals(byteArrayOf(1, 2, 3)))

        session.feedThrows = IllegalStateException("socket dead")
        try {
            session.feed(byteArrayOf(4))
            org.junit.Assert.fail("expected feed to throw")
        } catch (e: IllegalStateException) {
            assertEquals("socket dead", e.message)
        }
    }

    @Test
    fun `FakeObdSession commandCount and commandAt reflect the configured list`() {
        val session = FakeObdSession(commands = listOf("0104", "0105"))

        assertEquals(2L, session.commandCount())
        assertEquals("0104", session.commandAt(0))
        assertEquals("", session.commandAt(9))
    }

    @Test
    fun `FakeObdSession checkAnomalies and close count calls`() {
        val session = FakeObdSession()

        session.checkAnomalies()
        session.checkAnomalies()
        session.close()

        assertEquals(2, session.checkAnomaliesCallCount)
        assertEquals(1, session.closeCallCount)
    }
}
