package com.carmonitor.app.carapp

import android.content.Intent
import androidx.car.app.Screen
import androidx.car.app.Session

class CarMonitorSession : Session() {
    override fun onCreateScreen(intent: Intent): Screen = MainCarScreen(carContext)
}
