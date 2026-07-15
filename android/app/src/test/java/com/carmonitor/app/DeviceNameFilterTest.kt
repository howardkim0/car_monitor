package com.carmonitor.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure string-matching logic — no Android framework dependencies, so
 * no Robolectric needed.
 */
class DeviceNameFilterTest {

    @Test
    fun `matches names containing OBD case-insensitively`() {
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("OBDLink MX+"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("obdlink mx+"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("OBDII Scanner"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("Vgate OBD2"))
    }

    @Test
    fun `matches names containing ELM case-insensitively`() {
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("ELM327"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("elm327 v1.5"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("ELM-327 Bluetooth"))
    }

    @Test
    fun `matches this repo's own hardcoded default device name`() {
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("Garage OBDLink"))
    }

    @Test
    fun `matches OBD or ELM anywhere in the name, not just as a prefix`() {
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("My Car OBD Reader"))
        assertTrue(DeviceNameFilter.looksLikeObd2Scanner("Bluetooth-ELM-Adapter"))
    }

    @Test
    fun `does not match unrelated device names`() {
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner("AirPods Pro"))
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner("Galaxy Buds2"))
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner("Pixel 8"))
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner("JBL Flip 6"))
    }

    @Test
    fun `does not match null or blank names`() {
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner(null))
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner(""))
        assertFalse(DeviceNameFilter.looksLikeObd2Scanner("   "))
    }
}
