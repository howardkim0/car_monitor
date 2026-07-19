package com.carmonitor.app.carapp

import android.content.pm.PackageManager
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.model.Action
import androidx.car.app.model.CarColor
import androidx.car.app.model.CarIcon
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.model.Template
import androidx.core.content.ContextCompat
import androidx.core.graphics.drawable.IconCompat
import com.carmonitor.app.BluetoothPermissions
import com.carmonitor.app.MonitoringPrefs
import com.carmonitor.app.ObdForegroundService
import com.carmonitor.app.R

/**
 * Root Android Auto screen: 4 actions reusing the phone screen's own
 * button strings and color scheme (DESIGN.md). "Quit App" pushes a
 * confirmation screen rather than acting immediately — a single
 * accidental tap while driving shouldn't kill the app.
 */
class MainCarScreen(carContext: CarContext) : Screen(carContext) {

    override fun onGetTemplate(): Template {
        val quitIcon = CarIcon.Builder(IconCompat.createWithResource(carContext, R.drawable.ic_quit))
            .setTint(CarColor.createCustom(0xFF3A3A3A.toInt(), 0xFF3A3A3A.toInt()))
            .build()

        val itemList = ItemList.Builder()
            .addItem(
                Row.Builder()
                    .setTitle(carContext.getString(R.string.start_scanning_button))
                    .setOnClickListener { onStartScanning() }
                    .build()
            )
            .addItem(
                Row.Builder()
                    .setTitle(carContext.getString(R.string.pair_devices_button))
                    .setOnClickListener { screenManager.push(PairScannerScreen(carContext)) }
                    .build()
            )
            .addItem(
                Row.Builder()
                    .setTitle(carContext.getString(R.string.view_logs_button))
                    .setOnClickListener { screenManager.push(LogsScreen(carContext)) }
                    .build()
            )
            .addItem(
                Row.Builder()
                    .setTitle(carContext.getString(R.string.quit_app_button))
                    .setImage(quitIcon)
                    .setOnClickListener { screenManager.push(QuitConfirmationScreen(carContext)) }
                    .build()
            )
            .build()

        return ListTemplate.Builder()
            .setTitle(carContext.getString(R.string.app_name))
            .setHeaderAction(Action.APP_ICON)
            .setSingleList(itemList)
            .build()
    }

    private fun onStartScanning() {
        val missing = BluetoothPermissions.forServiceStart().filter {
            ContextCompat.checkSelfPermission(carContext, it) != PackageManager.PERMISSION_GRANTED
        }
        if (missing.isEmpty()) {
            startMonitoring()
        } else {
            carContext.requestPermissions(missing) { _, _ -> startMonitoring() }
        }
    }

    private fun startMonitoring() {
        MonitoringPrefs.setStoppedByUser(carContext, false)
        ObdForegroundService.start(carContext)
    }
}
