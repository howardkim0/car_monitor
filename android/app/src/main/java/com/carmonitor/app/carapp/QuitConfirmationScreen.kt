package com.carmonitor.app.carapp

import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.model.Action
import androidx.car.app.model.CarColor
import androidx.car.app.model.MessageTemplate
import androidx.car.app.model.Template
import com.carmonitor.app.AppQuit
import com.carmonitor.app.R

class QuitConfirmationScreen(carContext: CarContext) : Screen(carContext) {

    override fun onGetTemplate(): Template {
        val quitColor = CarColor.createCustom(0xFF3A3A3A.toInt(), 0xFF3A3A3A.toInt())

        val quitAction = Action.Builder()
            .setTitle(carContext.getString(R.string.quit_app_button))
            .setBackgroundColor(quitColor)
            .setFlags(Action.FLAG_PRIMARY)
            .setOnClickListener { AppQuit.quit(carContext) }
            .build()

        val cancelAction = Action.Builder()
            .setTitle(carContext.getString(android.R.string.cancel))
            .setOnClickListener { screenManager.pop() }
            .build()

        return MessageTemplate.Builder(carContext.getString(R.string.quit_confirmation_message))
            .setTitle(carContext.getString(R.string.quit_app_button))
            .addAction(quitAction)
            .addAction(cancelAction)
            .build()
    }
}
