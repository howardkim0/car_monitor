package com.carmonitor.app.carapp

import androidx.car.app.CarContext
import androidx.car.app.CarToast
import androidx.car.app.Screen
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.model.Template
import androidx.lifecycle.lifecycleScope
import com.carmonitor.app.CandidateDevice
import com.carmonitor.app.ObdDeviceLister
import com.carmonitor.app.R
import kotlinx.coroutines.launch

/**
 * Lists already-bonded OBD2 devices for the driver to switch to
 * mid-trip. Deliberately bonded-devices-only, not full BLE discovery —
 * see docs/plan-android-auto.md's "Pair Scanner is bonded-devices-only"
 * section for why (driver-distraction guidelines; Screens can't host
 * DeviceScanActivity's discovery Activity anyway).
 *
 * `isConnected` is always passed as false here — the car screen has no
 * StatusListener wired to ObdForegroundService (unlike StatusActivity),
 * so it can't distinguish "selected" from "currently connected." This
 * is a known display-only simplification: the selected device still
 * shows as "Selected," just not upgraded to "Connected" even if it
 * actually is. Worth revisiting if this proves confusing in practice.
 */
class PairScannerScreen(carContext: CarContext) : Screen(carContext) {

    override fun onGetTemplate(): Template {
        val candidates = ObdDeviceLister.listCandidates(carContext, carContext.filesDir, isConnected = false)

        val itemListBuilder = ItemList.Builder()
        if (candidates.isEmpty()) {
            itemListBuilder.setNoItemsMessage(carContext.getString(R.string.paired_devices_none))
        } else {
            candidates.forEach { candidate ->
                itemListBuilder.addItem(
                    Row.Builder()
                        .setTitle("${candidate.name} — ${candidate.status}")
                        .setOnClickListener { onSelect(candidate) }
                        .build()
                )
            }
        }

        return ListTemplate.Builder()
            .setTitle(carContext.getString(R.string.pair_devices_button))
            .setSingleList(itemListBuilder.build())
            .build()
    }

    private fun onSelect(candidate: CandidateDevice) {
        lifecycleScope.launch {
            try {
                // No bound service to reconnect from the car screen — a
                // no-op onReconnect, matching that the selection still
                // applies on the next actual connection attempt either way.
                ObdDeviceLister.select(carContext, carContext.filesDir, candidate.mac, candidate.name) {}
                CarToast.makeText(
                    carContext,
                    carContext.getString(R.string.device_selected, candidate.name),
                    CarToast.LENGTH_SHORT
                ).show()
                screenManager.pop()
            } catch (e: Exception) {
                CarToast.makeText(
                    carContext,
                    carContext.getString(R.string.device_select_failed),
                    CarToast.LENGTH_SHORT
                ).show()
            }
        }
    }
}
