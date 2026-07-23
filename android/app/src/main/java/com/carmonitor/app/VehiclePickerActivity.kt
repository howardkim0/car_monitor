package com.carmonitor.app

import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.annotation.VisibleForTesting
import androidx.appcompat.app.AppCompatActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import mobile.Mobile

/**
 * "Select Vehicle" wizard (DESIGN.md section 5.3): a single Activity
 * managing four sequential drill-down steps — Model Year, Make, Model,
 * Trim/Engine — plus a confirmation screen, rather than four chained
 * Activities (this codebase has no Fragment/ViewPager usage anywhere,
 * section 3). Tapping a row selects it and immediately advances, the
 * same one-tap-does-it interaction as [DeviceScanActivity]'s device
 * rows. The Trim step is skipped entirely when a Make/Model/Year
 * combination has only one (untrimmed) profile.
 *
 * [vehicleMobile]'s Count/At calls are read directly on the main
 * thread, not IO-dispatched like [DeviceScanActivity]'s/[StatusActivity]'s
 * `Mobile.*` calls (DESIGN.md section 6.2): they're fast, local,
 * in-memory Go lookups with no I/O, and by the time a user reaches this
 * screen from the status screen's "Select Vehicle" button,
 * `StatusActivity.onCreate()` has already touched `Mobile` once (native
 * library already loaded) — unlike that blanket rule's concern
 * (blocking the UI thread on first-touch native library loading), there
 * is no sensible "render nothing, fill in the list later" flow for a
 * screen whose entire content *is* that list. [setSelectedVehicle] is
 * the one call here that does file I/O (persists the selection), so it
 * stays IO-dispatched, matching [DeviceScanActivity.selectDeviceAndFinish].
 */
class VehiclePickerActivity : AppCompatActivity() {

    private enum class Step { YEAR, MAKE, MODEL, TRIM, CONFIRM }

    @VisibleForTesting
    internal var vehicleMobile: VehicleMobile = RealVehicleMobile

    private lateinit var breadcrumbText: TextView
    private lateinit var titleText: TextView
    private lateinit var rowsContainer: LinearLayout
    private lateinit var primaryButton: Button
    private lateinit var secondaryButton: Button

    private val scope = CoroutineScope(Dispatchers.Main + Job())

    private var step: Step = Step.YEAR
    private var selectedYear: Long = 0
    private var selectedMake: String = ""
    private var selectedModel: String = ""
    private var selectedTrim: String = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_vehicle_picker)

        breadcrumbText = findViewById(R.id.vehiclePickerBreadcrumbText)
        titleText = findViewById(R.id.vehiclePickerTitleText)
        rowsContainer = findViewById(R.id.vehiclePickerRowsContainer)
        primaryButton = findViewById(R.id.vehiclePickerPrimaryButton)
        secondaryButton = findViewById(R.id.vehiclePickerSecondaryButton)

        onBackPressedDispatcher.addCallback(
            this,
            object : OnBackPressedCallback(true) {
                override fun handleOnBackPressed() {
                    val previous = previousStep()
                    if (previous == null) {
                        isEnabled = false
                        onBackPressedDispatcher.onBackPressed()
                    } else {
                        step = previous
                        renderStep()
                    }
                }
            }
        )

        renderStep()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun previousStep(): Step? = when (step) {
        Step.YEAR -> null
        Step.MAKE -> Step.YEAR
        Step.MODEL -> Step.MAKE
        Step.TRIM -> Step.MODEL
        Step.CONFIRM -> if (hasTrimChoice()) Step.TRIM else Step.MODEL
    }

    private fun hasTrimChoice(): Boolean =
        vehicleMobile.trimCount(selectedYear, selectedMake, selectedModel) > 0

    private fun renderStep() {
        rowsContainer.removeAllViews()
        primaryButton.visibility = android.view.View.GONE
        secondaryButton.visibility = android.view.View.GONE

        when (step) {
            Step.YEAR -> renderYearStep()
            Step.MAKE -> renderMakeStep()
            Step.MODEL -> renderModelStep()
            Step.TRIM -> renderTrimStep()
            Step.CONFIRM -> renderConfirmStep()
        }
    }

    private fun renderYearStep() {
        breadcrumbText.text = getString(R.string.vehicle_picker_year_breadcrumb)
        titleText.text = getString(R.string.vehicle_picker_year_title)
        val count = vehicleMobile.yearCount()
        for (i in 0 until count) {
            val year = vehicleMobile.yearAt(i)
            addSelectableRow(year.toString()) {
                selectedYear = year
                step = Step.MAKE
                renderStep()
            }
        }
    }

    private fun renderMakeStep() {
        breadcrumbText.text = selectedYear.toString()
        titleText.text = getString(R.string.vehicle_picker_make_title)
        val count = vehicleMobile.makeCount(selectedYear)
        for (i in 0 until count) {
            val make = vehicleMobile.makeAt(selectedYear, i)
            addSelectableRow(make) {
                selectedMake = make
                step = Step.MODEL
                renderStep()
            }
        }
    }

    private fun renderModelStep() {
        breadcrumbText.text = "$selectedYear › $selectedMake"
        titleText.text = getString(R.string.vehicle_picker_model_title)
        val count = vehicleMobile.modelCount(selectedYear, selectedMake)
        for (i in 0 until count) {
            val model = vehicleMobile.modelAt(selectedYear, selectedMake, i)
            addSelectableRow(model) {
                selectedModel = model
                selectedTrim = ""
                step = if (vehicleMobile.trimCount(selectedYear, selectedMake, model) > 0) Step.TRIM else Step.CONFIRM
                renderStep()
            }
        }
    }

    private fun renderTrimStep() {
        breadcrumbText.text = "$selectedYear › $selectedMake › $selectedModel"
        titleText.text = getString(R.string.vehicle_picker_trim_title)
        val count = vehicleMobile.trimCount(selectedYear, selectedMake, selectedModel)
        for (i in 0 until count) {
            val trim = vehicleMobile.trimAt(selectedYear, selectedMake, selectedModel, i)
            addSelectableRow(trim) {
                selectedTrim = trim
                step = Step.CONFIRM
                renderStep()
            }
        }
    }

    private fun renderConfirmStep() {
        breadcrumbText.text = getString(R.string.vehicle_picker_confirm_breadcrumb)
        titleText.text = buildString {
            append(selectedYear)
            append(' ')
            append(selectedMake)
            append(' ')
            append(selectedModel)
            if (selectedTrim.isNotEmpty()) {
                append(' ')
                append(selectedTrim)
            }
        }
        primaryButton.visibility = android.view.View.VISIBLE
        primaryButton.text = getString(R.string.vehicle_picker_confirm_button)
        primaryButton.setOnClickListener { confirmAndFinish() }
        secondaryButton.visibility = android.view.View.VISIBLE
        secondaryButton.text = getString(R.string.vehicle_picker_change_button)
        secondaryButton.setOnClickListener {
            step = Step.YEAR
            renderStep()
        }
    }

    private fun addSelectableRow(label: String, onSelect: () -> Unit) {
        val button = Button(this)
        button.layoutParams = LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT,
            LinearLayout.LayoutParams.WRAP_CONTENT
        ).apply { topMargin = 8 }
        button.text = label
        button.setOnClickListener { onSelect() }
        rowsContainer.addView(button)
    }

    // Off the main thread, matching DeviceScanActivity.selectDeviceAndFinish:
    // this call does file I/O (persisting the selection), unlike the plain
    // in-memory Count/At reads above.
    private fun confirmAndFinish() {
        val summary = titleText.text.toString()
        scope.launch(Dispatchers.IO) {
            try {
                vehicleMobile.setSelectedVehicle(filesDir.absolutePath, selectedYear, selectedMake, selectedModel, selectedTrim)
                runOnUiThread {
                    Toast.makeText(this@VehiclePickerActivity, getString(R.string.vehicle_selected, summary), Toast.LENGTH_SHORT).show()
                    setResult(RESULT_OK)
                    finish()
                }
            } catch (e: Exception) {
                Mobile.logError("Failed to select vehicle: $e")
                runOnUiThread {
                    Toast.makeText(this@VehiclePickerActivity, getString(R.string.vehicle_select_failed), Toast.LENGTH_SHORT).show()
                }
            }
        }
    }
}
