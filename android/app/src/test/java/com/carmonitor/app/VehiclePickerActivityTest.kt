package com.carmonitor.app

import android.content.ComponentName
import android.widget.Button
import android.widget.LinearLayout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class VehiclePickerActivityTest {

    // Robolectric.buildActivity() constructs the Activity directly and
    // doesn't consult AndroidManifest.xml, so every other test in this file
    // would pass even if the Activity were never declared there -- which is
    // exactly what happened (StatusActivity's "Select Vehicle" button threw
    // ActivityNotFoundException on a real device, an explicit-Intent launch
    // Robolectric's buildActivity() never exercises). This is the only test
    // that actually resolves the manifest, the same gap-closing shape as
    // CarMonitorCarAppServiceTest's minCarApiLevel meta-data assertion.
    @Test
    fun `manifest declares VehiclePickerActivity`() {
        val context = RuntimeEnvironment.getApplication()

        val info = context.packageManager.getActivityInfo(
            ComponentName(context, VehiclePickerActivity::class.java),
            0
        )

        assertNotNull(
            "VehiclePickerActivity missing from AndroidManifest.xml -- " +
                "StatusActivity's \"Select Vehicle\" button launches it via an " +
                "explicit Intent, which throws ActivityNotFoundException without " +
                "this declaration",
            info
        )
    }

    private val controllers = mutableListOf<ActivityController<VehiclePickerActivity>>()

    // .start().resume(), not just .create(): OnBackPressedCallback (used
    // for the wizard's back-navigation) is Lifecycle-scoped and only
    // becomes active once the Activity reaches STARTED — a back-press
    // dispatched against a merely-CREATED Activity silently falls through
    // to the default (no-op-looking) behavior instead of reaching it.
    private fun newActivity(fake: VehicleMobile = FakeVehicleMobile()): VehiclePickerActivity {
        val controller = Robolectric.buildActivity(VehiclePickerActivity::class.java)
        controllers.add(controller)
        val activity = controller.get()
        activity.vehicleMobile = fake
        controller.create().start().resume()
        return activity
    }

    @After
    fun tearDown() {
        controllers.forEach { it.destroy() }
        controllers.clear()
    }

    private fun rows(activity: VehiclePickerActivity) =
        activity.findViewById<LinearLayout>(R.id.vehiclePickerRowsContainer)

    private fun rowLabels(activity: VehiclePickerActivity): List<String> {
        val container = rows(activity)
        return (0 until container.childCount).map { (container.getChildAt(it) as Button).text.toString() }
    }

    private fun clickRow(activity: VehiclePickerActivity, label: String) {
        val container = rows(activity)
        for (i in 0 until container.childCount) {
            val button = container.getChildAt(i) as Button
            if (button.text.toString() == label) {
                button.performClick()
                return
            }
        }
        throw AssertionError("no row labeled \"$label\" found (rows: ${rowLabels(activity)})")
    }

    private fun titleText(activity: VehiclePickerActivity): String =
        activity.findViewById<android.widget.TextView>(R.id.vehiclePickerTitleText).text.toString()

    private fun breadcrumbText(activity: VehiclePickerActivity): String =
        activity.findViewById<android.widget.TextView>(R.id.vehiclePickerBreadcrumbText).text.toString()

    @Test
    fun `activity creates without crashing and lists years first`() {
        val activity = newActivity()
        assertEquals(listOf("2023"), rowLabels(activity))
        assertEquals(activity.getString(R.string.vehicle_picker_year_title), titleText(activity))
    }

    @Test
    fun `selecting a year advances to the make step`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        assertEquals(listOf("Subaru"), rowLabels(activity))
        assertEquals("2023", breadcrumbText(activity))
        assertEquals(activity.getString(R.string.vehicle_picker_make_title), titleText(activity))
    }

    @Test
    fun `selecting a make advances to the model step`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        assertEquals(listOf("Forester", "Outback"), rowLabels(activity))
        assertEquals("2023 › Subaru", breadcrumbText(activity))
    }

    @Test
    fun `selecting a model with trims advances to the trim step`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Forester")
        assertEquals(listOf("Wilderness"), rowLabels(activity))
        assertEquals(activity.getString(R.string.vehicle_picker_trim_title), titleText(activity))
    }

    // Regression guard for DESIGN.md section 5.3's explicit "skip the Trim
    // step when there's only one, untrimmed variant" behavior.
    @Test
    fun `selecting a single-variant model skips straight to confirm`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Outback")

        assertEquals("2023 Subaru Outback", titleText(activity))
        assertEquals(
            "Confirm step should show its buttons instead of a Trim row list",
            android.view.View.VISIBLE,
            activity.findViewById<Button>(R.id.vehiclePickerPrimaryButton).visibility
        )
    }

    @Test
    fun `selecting a trim advances to confirm with the trim in the title`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Forester")
        clickRow(activity, "Wilderness")

        assertEquals("2023 Subaru Forester Wilderness", titleText(activity))
    }

    @Test
    fun `change vehicle button on confirm resets to the year step`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Outback")

        activity.findViewById<Button>(R.id.vehiclePickerSecondaryButton).performClick()

        assertEquals(listOf("2023"), rowLabels(activity))
        assertEquals(activity.getString(R.string.vehicle_picker_year_title), titleText(activity))
    }

    @Test
    fun `back navigation steps back one level at a time`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        assertEquals(activity.getString(R.string.vehicle_picker_model_title), titleText(activity))

        activity.onBackPressedDispatcher.onBackPressed()
        assertEquals(
            "back from Model should return to Make, not all the way to Year",
            activity.getString(R.string.vehicle_picker_make_title),
            titleText(activity)
        )

        activity.onBackPressedDispatcher.onBackPressed()
        assertEquals(
            "back from Make should return to Year",
            activity.getString(R.string.vehicle_picker_year_title),
            titleText(activity)
        )
    }

    // The Confirm step's "previous" target depends on whether a Trim step
    // was actually shown — a single-variant model (Outback) skipped it, so
    // back from Confirm must land on Model, not a Trim step never shown.
    @Test
    fun `back from confirm returns to model when the trim step was skipped`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Outback")

        activity.onBackPressedDispatcher.onBackPressed()

        assertEquals(activity.getString(R.string.vehicle_picker_model_title), titleText(activity))
    }

    @Test
    fun `back from confirm returns to trim when a trim was selected`() {
        val activity = newActivity()
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Forester")
        clickRow(activity, "Wilderness")

        activity.onBackPressedDispatcher.onBackPressed()

        assertEquals(activity.getString(R.string.vehicle_picker_trim_title), titleText(activity))
    }

    @Test
    fun `back from the first step exits the activity`() {
        val activity = newActivity()
        activity.onBackPressedDispatcher.onBackPressed()
        assertTrue("backing out of the first step should finish the activity", activity.isFinishing)
    }

    @Test
    fun `confirm button persists the selection with the right fields`() {
        val fake = FakeVehicleMobile()
        val activity = newActivity(fake)
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Outback")

        activity.findViewById<Button>(R.id.vehiclePickerPrimaryButton).performClick()

        awaitCondition { fake.setSelectedCalls.isNotEmpty() || activity.isFinishing }

        val call = fake.setSelectedCalls.singleOrNull()
        assertNotNull("expected setSelectedVehicle to have been called", call)
        assertEquals(2023L, call!!.year)
        assertEquals("Subaru", call.make)
        assertEquals("Outback", call.model)
        assertEquals("", call.trim)
    }

    @Test
    fun `confirm button includes the trim when one was selected`() {
        val fake = FakeVehicleMobile()
        val activity = newActivity(fake)
        clickRow(activity, "2023")
        clickRow(activity, "Subaru")
        clickRow(activity, "Forester")
        clickRow(activity, "Wilderness")

        activity.findViewById<Button>(R.id.vehiclePickerPrimaryButton).performClick()

        awaitCondition { fake.setSelectedCalls.isNotEmpty() || activity.isFinishing }

        assertEquals("Wilderness", fake.setSelectedCalls.single().trim)
    }

    // Polls the (paused-by-default under Robolectric) main looper alongside
    // a real background IO thread — confirmAndFinish() dispatches off the
    // main thread on purpose (matching DeviceScanActivity.selectDeviceAndFinish),
    // so a single idle() call right after performClick() can't be trusted to
    // see its effects yet.
    private fun awaitCondition(timeoutMs: Long = 2000, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            shadowOf(android.os.Looper.getMainLooper()).idle()
            if (condition()) return
            Thread.sleep(10)
        }
        throw AssertionError("condition not met within ${timeoutMs}ms")
    }
}
