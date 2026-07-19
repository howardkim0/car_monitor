package com.carmonitor.app.carapp

import androidx.car.app.model.LongMessageTemplate
import androidx.car.app.testing.TestCarContext
import com.carmonitor.app.R
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * Tests for LogsScreen — verifies template rendering (empty state,
 * truncated log tail), and that the Refresh action can be tapped
 * without throwing.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LogsScreenTest {

    private lateinit var testCarContext: TestCarContext

    @Before
    fun setup() {
        val context = RuntimeEnvironment.getApplication()
        testCarContext = TestCarContext.createCarContext(context)
    }

    @Test
    fun `when no app log file exists, displays empty message`() {
        val screen = LogsScreen(testCarContext)

        val template = screen.onGetTemplate() as LongMessageTemplate

        assertEquals(
            "should display empty log message when file doesn't exist",
            testCarContext.getString(R.string.log_viewer_empty),
            template.message.toString()
        )
    }

    @Test
    fun `when app log has many lines, displays only the last 15 lines`() {
        // Create fake app.log with 20 lines
        val appLogFile = File(testCarContext.filesDir, "app.log")
        appLogFile.parentFile?.mkdirs()
        val lines = (1..20).map { "line $it" }
        appLogFile.writeText(lines.joinToString("\n"))

        val screen = LogsScreen(testCarContext)

        val template = screen.onGetTemplate() as LongMessageTemplate
        val message = template.message.toString()

        // Should not contain the first line when truncated
        assertFalse(
            "message should not contain the first line when truncated",
            message.contains("line 1\n") || message.startsWith("line 1\n")
        )

        // Should contain lines from the end
        assertEquals("should contain the last line", true, message.contains("line 20"))
        assertEquals("should contain a line from the last 15", true, message.contains("line 15"))
    }

    @Test
    fun `title is View App Logs`() {
        val screen = LogsScreen(testCarContext)

        val template = screen.onGetTemplate() as LongMessageTemplate

        assertEquals(
            "title should be View App Logs string",
            testCarContext.getString(R.string.view_logs_button),
            template.title.toString()
        )
    }

    @Test
    fun `has exactly one Refresh action that can be tapped`() {
        val screen = LogsScreen(testCarContext)

        val template = screen.onGetTemplate() as LongMessageTemplate
        val actions = template.actions

        assertEquals("should have exactly one action", 1, actions.size)

        val action = actions[0]
        assertEquals(
            "action title should be Refresh",
            testCarContext.getString(R.string.log_viewer_refresh_button),
            action.title.toString()
        )

        val onClickDelegate = action.onClickDelegate
        assertNotNull("action should have an onClickDelegate", onClickDelegate)

        // Tapping the action should not throw — create an empty callback
        // (OnDoneCallback has default implementations for its methods)
        onClickDelegate?.sendClick(
            object : androidx.car.app.OnDoneCallback { }
        )
    }
}
