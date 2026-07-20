package com.carmonitor.app.carapp

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.testing.TestCarContext
import com.carmonitor.app.BluetoothPermissions
import com.carmonitor.app.CandidateDevice
import com.carmonitor.app.ObdDeviceLister
import com.carmonitor.app.ObdDeviceScanner
import com.carmonitor.app.R
import io.mockk.every
import io.mockk.mockkObject
import io.mockk.slot
import io.mockk.unmockkObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf

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
        mockkObject(ObdDeviceScanner)
    }

    @After
    fun tearDown() {
        // mockkObject() patches the singleton's bytecode process-wide —
        // leaving it mocked after this class's tests finish would leak
        // into any other test class in the same suite run that touches
        // the real object.
        unmockkObject(ObdDeviceLister)
        unmockkObject(ObdDeviceScanner)
    }

    // TestCarContext grants nothing by default — carContext.requestPermissions()'s
    // callback only fires once the (real) system permission flow completes,
    // which this harness doesn't simulate, so tests that need to observe
    // behavior past the permission check grant it directly via the shadow
    // instead, same as a device that already granted it in a prior session.
    private fun grantScanPermissions() {
        shadowOf(RuntimeEnvironment.getApplication()).grantPermissions(*BluetoothPermissions.forScan())
    }

    @Test
    fun `screen constructs without crashing`() {
        // Re-create screen after mocking setup
        screen = PairScannerScreen(testCarContext)
        assertNotNull("PairScannerScreen should construct successfully", screen)
    }

    @Test
    fun `always shows a Scan for Devices row first, even with no bonded devices`() {
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns emptyList()

        screen = PairScannerScreen(testCarContext)

        val template = screen.onGetTemplate() as ListTemplate
        val rows = template.singleList!!.items.map { it as Row }

        assertEquals("Should have only the scan row", 1, rows.size)
        assertEquals(
            testCarContext.getString(R.string.device_scan_button),
            rows[0].title.toString()
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
        val rows = template.singleList!!.items.map { it as Row }

        // Scan row, then the 2 bonded devices
        assertEquals("Should have the scan row plus 2 devices", 3, rows.size)

        assertTrue(
            "Second row should contain first device name",
            rows[1].title.toString().contains("Garage OBDLink")
        )
        assertTrue(
            "Second row should contain status",
            rows[1].title.toString().contains("Selected")
        )

        assertTrue(
            "Third row should contain second device name",
            rows[2].title.toString().contains("ELM327")
        )
        assertTrue(
            "Third row should contain status",
            rows[2].title.toString().contains("Paired")
        )
    }

    @Test
    fun `tapping Scan for Devices requests permissions then starts discovery`() {
        grantScanPermissions()
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns emptyList()
        every { ObdDeviceScanner.isLocationEnabled(any()) } returns true
        every { ObdDeviceScanner.startDiscovery(any(), any()) } returns null

        screen = PairScannerScreen(testCarContext)
        val template = screen.onGetTemplate() as ListTemplate
        val scanRow = template.singleList!!.items.map { it as Row }[0]

        scanRow.onClickDelegate!!.sendClick(object : androidx.car.app.OnDoneCallback {})

        io.mockk.verify { ObdDeviceScanner.startDiscovery(any(), any()) }
    }

    @Test
    fun `discovered non-bonded devices appear as rows and can be tapped to pair`() {
        grantScanPermissions()
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns emptyList()
        every { ObdDeviceScanner.isLocationEnabled(any()) } returns true

        val listenerSlot = slot<ObdDeviceScanner.Listener>()
        val fakeReceiver = object : android.content.BroadcastReceiver() {
            override fun onReceive(context: android.content.Context?, intent: android.content.Intent?) {}
        }
        every { ObdDeviceScanner.startDiscovery(any(), capture(listenerSlot)) } returns fakeReceiver
        every { ObdDeviceScanner.stopDiscovery(any(), any()) } returns Unit
        every { ObdDeviceScanner.pair(any()) } returns Unit

        screen = PairScannerScreen(testCarContext)
        val scanRow = (screen.onGetTemplate() as ListTemplate).singleList!!.items.map { it as Row }[0]
        scanRow.onClickDelegate!!.sendClick(object : androidx.car.app.OnDoneCallback {})

        val device = BluetoothAdapter.getDefaultAdapter().getRemoteDevice("11:22:33:44:55:66")
        shadowOf(device).setName("ELM327")
        listenerSlot.captured.onDeviceFound(device)

        val rowsAfterFound = (screen.onGetTemplate() as ListTemplate).singleList!!.items.map { it as Row }
        assertEquals("scan row + 1 discovered device", 2, rowsAfterFound.size)

        rowsAfterFound[1].onClickDelegate!!.sendClick(object : androidx.car.app.OnDoneCallback {})

        io.mockk.verify { ObdDeviceScanner.pair(device) }

        val rowsWhilePairing = (screen.onGetTemplate() as ListTemplate).singleList!!.items.map { it as Row }
        assertEquals(
            testCarContext.getString(R.string.device_scan_pairing),
            rowsWhilePairing[1].title.toString()
        )
    }

    @Test
    fun `bond state change to BONDED selects the device`() {
        grantScanPermissions()
        every { ObdDeviceLister.listCandidates(any(), any(), any()) } returns emptyList()
        io.mockk.coEvery { ObdDeviceLister.select(any(), any(), any(), any(), any()) } returns Unit
        every { ObdDeviceScanner.isLocationEnabled(any()) } returns true

        val listenerSlot = slot<ObdDeviceScanner.Listener>()
        val fakeReceiver = object : android.content.BroadcastReceiver() {
            override fun onReceive(context: android.content.Context?, intent: android.content.Intent?) {}
        }
        every { ObdDeviceScanner.startDiscovery(any(), capture(listenerSlot)) } returns fakeReceiver
        every { ObdDeviceScanner.stopDiscovery(any(), any()) } returns Unit
        every { ObdDeviceScanner.pair(any()) } returns Unit

        screen = PairScannerScreen(testCarContext)
        val scanRow = (screen.onGetTemplate() as ListTemplate).singleList!!.items.map { it as Row }[0]
        scanRow.onClickDelegate!!.sendClick(object : androidx.car.app.OnDoneCallback {})

        val device = BluetoothAdapter.getDefaultAdapter().getRemoteDevice("11:22:33:44:55:66")
        shadowOf(device).setName("ELM327")
        listenerSlot.captured.onDeviceFound(device)
        (screen.onGetTemplate() as ListTemplate).singleList!!.items.map { it as Row }[1]
            .onClickDelegate!!.sendClick(object : androidx.car.app.OnDoneCallback {})

        listenerSlot.captured.onBondStateChanged(device, BluetoothDevice.BOND_BONDED)

        io.mockk.coVerify { ObdDeviceLister.select(any(), any(), device.address, any(), any()) }
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
        unmockkObject(ObdDeviceLister)

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
