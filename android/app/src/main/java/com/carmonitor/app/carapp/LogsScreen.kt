package com.carmonitor.app.carapp

import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.model.Action
import androidx.car.app.model.LongMessageTemplate
import androidx.car.app.model.ParkedOnlyOnClickListener
import androidx.car.app.model.Template
import com.carmonitor.app.LogViewer
import com.carmonitor.app.R
import java.io.File

/**
 * Shows a short tail of app.log on the car screen — LongMessageTemplate
 * (and the host it renders on) caps message length far below the
 * phone's scrollable LogViewerActivity, so this reads far less of the
 * file and trims further to a handful of lines. A "Refresh" action
 * re-reads, mirroring LogViewerActivity's existing Refresh button.
 */
class LogsScreen(carContext: CarContext) : Screen(carContext) {

    override fun onGetTemplate(): Template {
        val appLogFile = File(carContext.filesDir, "app.log")
        val tail = LogViewer.readTail(appLogFile, maxBytes = TAIL_MAX_BYTES)
        val message = if (tail.isNullOrBlank()) {
            carContext.getString(R.string.log_viewer_empty)
        } else {
            tail.lines().takeLast(TAIL_MAX_LINES).joinToString("\n")
        }

        // LongMessageTemplate requires its actions to be parked-only —
        // the host itself enforces that reading/refreshing a long text
        // block isn't something a driver does while moving.
        val refreshAction = Action.Builder()
            .setTitle(carContext.getString(R.string.log_viewer_refresh_button))
            .setOnClickListener(ParkedOnlyOnClickListener.create { invalidate() })
            .build()

        return LongMessageTemplate.Builder(message)
            .setTitle(carContext.getString(R.string.view_logs_button))
            .addAction(refreshAction)
            .build()
    }

    companion object {
        private const val TAIL_MAX_BYTES = 4000
        private const val TAIL_MAX_LINES = 15
    }
}
