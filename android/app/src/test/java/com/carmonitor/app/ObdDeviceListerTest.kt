package com.carmonitor.app

import android.content.Context
import io.mockk.every
import io.mockk.mockk
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.io.File

@RunWith(RobolectricTestRunner::class)
class ObdDeviceListerTest {

    @Test
    fun `filteringlogic correctly identifies OBD2 devices`() {
        // Test the filtering logic by verifying that DeviceNameFilter works
        // as expected — this is the core of what ObdDeviceLister does.
        assertEquals(
            "ELM327 should be recognized as OBD2",
            true,
            DeviceNameFilter.looksLikeObd2Scanner("ELM327")
        )
        assertEquals(
            "obd in name should be recognized",
            true,
            DeviceNameFilter.looksLikeObd2Scanner("My OBD Device")
        )
        assertEquals(
            "Non-OBD names should not be recognized",
            false,
            DeviceNameFilter.looksLikeObd2Scanner("My Phone")
        )
        assertEquals(
            "Null name should not be recognized",
            false,
            DeviceNameFilter.looksLikeObd2Scanner(null)
        )
    }

    @Test
    fun `CandidateDevice data class stores mac, name, and status correctly`() {
        // Test that CandidateDevice properly stores the device information
        // that ObdDeviceLister.listCandidates returns.
        val device = CandidateDevice(
            mac = "AA:BB:CC:DD:EE:FF",
            name = "ELM327",
            status = "Connected"
        )

        assertEquals("AA:BB:CC:DD:EE:FF", device.mac)
        assertEquals("ELM327", device.name)
        assertEquals("Connected", device.status)
    }
}
