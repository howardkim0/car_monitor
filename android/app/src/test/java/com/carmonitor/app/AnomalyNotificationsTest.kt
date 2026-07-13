package com.carmonitor.app

import android.app.NotificationManager
import android.content.Context
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AnomalyNotificationsTest {

    private fun getContext(): Context = RuntimeEnvironment.getApplication()

    @Test
    fun `ensureChannel creates a channel with IMPORTANCE_HIGH and the right name description`() {
        val context = getContext()
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        AnomalyNotifications.ensureChannel(context)

        val channel = manager.getNotificationChannel(AnomalyNotifications.CHANNEL_ID)
        assertNotNull("channel should be created", channel)
        assertEquals(NotificationManager.IMPORTANCE_HIGH, channel!!.importance)
        assertEquals(
            context.getString(R.string.notification_anomaly_channel_name),
            channel.name
        )
        assertEquals(
            context.getString(R.string.notification_anomaly_channel_description),
            channel.description
        )
    }

    @Test
    fun `post with level CRITICAL results in PRIORITY_MAX notification`() {
        val context = getContext()
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        AnomalyNotifications.ensureChannel(context)
        AnomalyNotifications.post(context, "Test Metric", "CRITICAL", "Test message")

        val wantId = 1000 + ("Test Metric".hashCode() and 0xFF)
        val posted = manager.activeNotifications
            .firstOrNull { it.id == wantId }
            ?.notification

        assertNotNull("notification should be posted", posted)
        assertEquals(android.app.Notification.PRIORITY_MAX, posted!!.priority)
    }

    @Test
    fun `post with level WARNING results in PRIORITY_DEFAULT notification`() {
        val context = getContext()
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        AnomalyNotifications.ensureChannel(context)
        AnomalyNotifications.post(context, "Test Metric", "WARNING", "Test message")

        val wantId = 1000 + ("Test Metric".hashCode() and 0xFF)
        val posted = manager.activeNotifications
            .firstOrNull { it.id == wantId }
            ?.notification

        assertNotNull("notification should be posted", posted)
        assertEquals(android.app.Notification.PRIORITY_DEFAULT, posted!!.priority)
    }

    @Test
    fun `post sets autoCancel true and uses the CHANNEL_ID channel`() {
        val context = getContext()
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        AnomalyNotifications.ensureChannel(context)
        AnomalyNotifications.post(context, "Test Metric", "WARNING", "Test message")

        val wantId = 1000 + ("Test Metric".hashCode() and 0xFF)
        val posted = manager.activeNotifications
            .firstOrNull { it.id == wantId }
            ?.notification

        assertNotNull("notification should be posted", posted)
        assertEquals(AnomalyNotifications.CHANNEL_ID, posted!!.channelId)
        assertEquals(android.app.Notification.FLAG_AUTO_CANCEL, posted.flags and android.app.Notification.FLAG_AUTO_CANCEL)
    }

    @Test
    fun `calling ensureChannel twice does not throw`() {
        val context = getContext()

        AnomalyNotifications.ensureChannel(context)
        AnomalyNotifications.ensureChannel(context)

        // If we get here without throwing, the test passes
    }
}
