package com.carmonitor.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat

/**
 * Posting logic for anomaly/alert notifications, factored out of
 * ObdForegroundService so it works without the Service running — see
 * DESIGN.md section 4 step 6 for why (StatusActivity's Test Alert button
 * needs this, and routing it through the Service would risk silently
 * resuming monitoring after an explicit Stop).
 */
object AnomalyNotifications {
    const val CHANNEL_ID = "obd2_anomaly"
    // Offset from the persistent status notification's ID (1) so an
    // anomaly notification never collides with it.
    private const val NOTIFICATION_ID_BASE = 1000

    fun ensureChannel(context: Context) {
        val channel = NotificationChannel(
            CHANNEL_ID,
            context.getString(R.string.notification_anomaly_channel_name),
            NotificationManager.IMPORTANCE_HIGH
        ).apply {
            description = context.getString(R.string.notification_anomaly_channel_description)
        }
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.createNotificationChannel(channel)
    }

    fun post(context: Context, metric: String, level: String, message: String) {
        val notification: Notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setContentTitle(context.getString(R.string.notification_anomaly_title, metric))
            .setContentText(message)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentIntent(
                PendingIntent.getActivity(
                    context,
                    0,
                    Intent(context, StatusActivity::class.java),
                    PendingIntent.FLAG_IMMUTABLE
                )
            )
            .setPriority(
                if (level == "CRITICAL") NotificationCompat.PRIORITY_MAX else NotificationCompat.PRIORITY_DEFAULT
            )
            .setAutoCancel(true)
            .build()
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.notify(NOTIFICATION_ID_BASE + (metric.hashCode() and 0xFF), notification)
    }
}
