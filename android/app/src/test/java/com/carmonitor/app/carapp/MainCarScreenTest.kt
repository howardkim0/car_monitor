package com.carmonitor.app.carapp

import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.testing.TestCarContext
import com.carmonitor.app.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class MainCarScreenTest {

    private lateinit var testCarContext: TestCarContext
    private lateinit var screen: MainCarScreen

    @Before
    fun setup() {
        testCarContext = TestCarContext.createCarContext(RuntimeEnvironment.getApplication())
        screen = MainCarScreen(testCarContext)
    }

    @Test
    fun `renders ListTemplate with 4 rows in correct order`() {
        val template = screen.onGetTemplate() as ListTemplate
        val rows = template.singleList!!.items.map { it as Row }

        assertEquals(4, rows.size)
        assertEquals(
            testCarContext.getString(R.string.start_scanning_button),
            rows[0].title.toString()
        )
        assertEquals(
            testCarContext.getString(R.string.pair_devices_button),
            rows[1].title.toString()
        )
        assertEquals(
            testCarContext.getString(R.string.view_logs_button),
            rows[2].title.toString()
        )
        assertEquals(
            testCarContext.getString(R.string.quit_app_button),
            rows[3].title.toString()
        )
    }

    @Test
    fun `Quit row has tinted icon image`() {
        val template = screen.onGetTemplate() as ListTemplate
        val quitRow = template.singleList!!.items[3] as Row

        assertNotNull("Quit row should have an image", quitRow.image)
    }

    @Test
    fun `tapping Pair Scanner pushes PairScannerScreen`() {
        val template = screen.onGetTemplate() as ListTemplate
        val pairRow = template.singleList!!.items[1] as Row

        pairRow.onClickDelegate?.sendClick(object : androidx.car.app.OnDoneCallback {})

        val screenManager = testCarContext.getCarService(androidx.car.app.ScreenManager::class.java)
        assertTrue("Top screen should be PairScannerScreen", screenManager.top is PairScannerScreen)
    }

    @Test
    fun `tapping Display Logs pushes LogsScreen`() {
        val template = screen.onGetTemplate() as ListTemplate
        val logsRow = template.singleList!!.items[2] as Row

        logsRow.onClickDelegate?.sendClick(object : androidx.car.app.OnDoneCallback {})

        val screenManager = testCarContext.getCarService(androidx.car.app.ScreenManager::class.java)
        assertTrue("Top screen should be LogsScreen", screenManager.top is LogsScreen)
    }

    @Test
    fun `tapping Quit App pushes QuitConfirmationScreen`() {
        val template = screen.onGetTemplate() as ListTemplate
        val quitRow = template.singleList!!.items[3] as Row

        quitRow.onClickDelegate?.sendClick(object : androidx.car.app.OnDoneCallback {})

        val screenManager = testCarContext.getCarService(androidx.car.app.ScreenManager::class.java)
        assertTrue("Top screen should be QuitConfirmationScreen", screenManager.top is QuitConfirmationScreen)
    }

    @Test
    fun `tapping Start Scanning without permissions requests them`() {
        // Don't grant permissions — they should be requested
        val template = screen.onGetTemplate() as ListTemplate
        val startRow = template.singleList!!.items[0] as Row

        startRow.onClickDelegate?.sendClick(object : androidx.car.app.OnDoneCallback {})

        // Verify permissions were requested
        val permissionInfo = testCarContext.getLastPermissionRequestInfo()
        assertNotNull("Should have requested permissions", permissionInfo)
        assertTrue("Should include Bluetooth permission", permissionInfo!!.getPermissionsRequested().isNotEmpty())
    }
}
