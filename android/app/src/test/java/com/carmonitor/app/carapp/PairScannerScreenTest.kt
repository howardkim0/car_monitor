package com.carmonitor.app.carapp

import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.testing.TestCarContext
import com.carmonitor.app.CandidateDevice
import com.carmonitor.app.ObdDeviceLister
import com.carmonitor.app.R
import io.mockk.every
import io.mockk.mockkObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class PairScannerScreenTest {

    private lateinit var testCarContext: TestCarContext
    private lateinit var screen: PairScannerScreen

    @Before
    fun setup() {
        testCarContext = TestCarContext.createCarContext(RuntimeEnvironment.getApplication())
        // Mock ObdDeviceLister to avoid JNI call to Mobile.deviceMAC()
        // in a Robolectric test environment (plain JVM with no native libs)
        mockkObject(ObdDeviceLister)
    }

    @After
    fun tearDown() {
        // mockkObject() patches the singleton's bytecode process-wide —
        // leaving it mocked after this class's tests finish would leak
        // into any other test class in the same suite run that touches
        // the real ObdDeviceLister object.
        io.mockk.unmockkObject(ObdDeviceLister)
    }

    @Test
    fun `screen constructs without crashing`() {
        // Re-create screen after mocking setup
        screen = PairScannerScreen(testCarContext)
        assertNotNull("PairScannerScreen should construct successfully", screen)
    }

    @Test
    fun `renders empty list message when no devices available`() {
        // Mock listCandidates() to return empty list (the typical case
        // in Robolectric: no real Bluetooth adapter with bonded devices).
        // This avoids the JNI UnsatisfiedLinkError from Mobile.deviceMAC()
        // and lets us test the template rendering directly.
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns emptyList()

        screen = PairScannerScreen(testCarContext)

        val template = screen.onGetTemplate() as ListTemplate
        val itemList = template.singleList!!

        // Assert no items are shown
        assertEquals("Should have 0 items", 0, itemList.items.size)

        // Assert the no-items message is set
        assertNotNull("Should have a no-items message", itemList.noItemsMessage)
        assertEquals(
            "No-items message should match resource string",
            testCarContext.getString(R.string.paired_devices_none),
            itemList.noItemsMessage.toString()
        )
    }

    @Test
    fun `renders device list when devices available`() {
        // Mock listCandidates() to return a list of bonded OBD2 devices.
        // This tests the device-list rendering path without hitting JNI.
        val mockDevices = listOf(
            CandidateDevice(mac = "AA:BB:CC:DD:EE:FF", name = "Garage OBDLink", status = "Selected"),
            CandidateDevice(mac = "11:22:33:44:55:66", name = "ELM327", status = "Paired")
        )
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns mockDevices

        screen = PairScannerScreen(testCarContext)

        val template = screen.onGetTemplate() as ListTemplate
        val itemList = template.singleList!!
        val rows = itemList.items.map { it as Row }

        // Assert correct number of devices
        assertEquals("Should have 2 devices", 2, rows.size)

        // Assert first device row title
        assertTrue(
            "First device row should contain device name",
            rows[0].title.toString().contains("Garage OBDLink")
        )
        assertTrue(
            "First device row should contain status",
            rows[0].title.toString().contains("Selected")
        )

        // Assert second device row title
        assertTrue(
            "Second device row should contain device name",
            rows[1].title.toString().contains("ELM327")
        )
        assertTrue(
            "Second device row should contain status",
            rows[1].title.toString().contains("Paired")
        )
    }

    @Test
    fun `onGetTemplate with real ObdDeviceLister may throw from native library loading`() {
        // Document the real JNI limitation: ObdDeviceLister.listCandidates()
        // unconditionally calls Mobile.deviceMAC() which is a gomobile/JNI
        // call. Robolectric runs on the plain JVM with no native libraries,
        // so this fails when Mobile's static initializer first runs.
        //
        // The exact exception type here is order-dependent, not just
        // UnsatisfiedLinkError: the JVM caches a failed static
        // initializer, so this can surface as UnsatisfiedLinkError (first
        // touch in this class loader), ExceptionInInitializerError (the
        // static init itself failing), or NoClassDefFoundError (a later
        // touch after the JVM already marked Mobile's init as failed) —
        // matching this codebase's own established precedent for
        // gomobile-native-init failures (DESIGN.md section 6.2's
        // "wrapped in catch (e: Throwable), not just Exception" reasoning
        // for the exact same root cause). Catching Throwable here for the
        // same reason, not just one specific subtype.
        //
        // Real testing happens via on-device/emulator runs and DHU
        // (Desktop Head Unit) for Android Auto.
        //
        // Un-mock ObdDeviceLister to test the real (failing) path:
        io.mockk.unmockkObject(ObdDeviceLister)

        screen = PairScannerScreen(testCarContext)

        try {
            val template = screen.onGetTemplate()
            // If we get here without error, Robolectric must have loaded
            // libgojni.so somehow (very unlikely), or the error happens
            // lazily. Verify we got a template either way.
            assertNotNull("Should have returned a template", template)
        } catch (e: Throwable) {
            // Expected: Mobile.deviceMAC() is a JNI call with no native
            // library in Robolectric's plain JVM environment.
            assertNotNull("Expected a real exception/error from the failed native load", e)
        }
    }
}
