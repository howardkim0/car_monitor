package com.carmonitor.app

import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.ScrollView
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import java.io.File

/**
 * Screen to display the tail of the app log (app.log), showing the most
 * recent debug/error messages. Uses the same coroutine pattern as
 * DeviceScanActivity — file I/O on Dispatchers.IO, UI updates on the
 * main thread.
 */
class LogViewerActivity : AppCompatActivity() {

    private lateinit var logText: TextView
    private lateinit var logScrollView: ScrollView
    private lateinit var truncationNotice: TextView
    private lateinit var refreshButton: Button

    private val scope = CoroutineScope(Dispatchers.Main + Job())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_log_viewer)

        logText = findViewById(R.id.logText)
        logScrollView = findViewById(R.id.logScrollView)
        truncationNotice = findViewById(R.id.truncationNotice)
        refreshButton = findViewById(R.id.refreshButton)
        refreshButton.setOnClickListener { loadLog() }

        loadLog()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun loadLog() {
        scope.launch(Dispatchers.IO) {
            val logFile = File(filesDir, "app.log")
            val logContent = LogViewer.readTail(logFile)
            val isTruncated = LogViewer.isTruncated(logFile)

            runOnUiThread {
                if (logContent == null) {
                    logText.text = getString(R.string.log_viewer_empty)
                    truncationNotice.visibility = View.GONE
                } else if (logContent.isBlank()) {
                    logText.text = getString(R.string.log_viewer_empty)
                    truncationNotice.visibility = View.GONE
                } else {
                    logText.text = logContent
                    truncationNotice.visibility = if (isTruncated) View.VISIBLE else View.GONE
                    // Scroll to bottom so most recent logs are visible
                    logScrollView.post {
                        logScrollView.fullScroll(View.FOCUS_DOWN)
                    }
                }
            }
        }
    }
}
