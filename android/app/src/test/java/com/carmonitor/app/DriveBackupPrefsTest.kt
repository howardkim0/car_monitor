package com.carmonitor.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class DriveBackupPrefsTest {

    @Test
    fun `defaults to no folder configured`() {
        val context = RuntimeEnvironment.getApplication()
        assertNull(DriveBackupPrefs.getFolderUri(context))
    }

    @Test
    fun `setFolderUri persists the value`() {
        val context = RuntimeEnvironment.getApplication()

        DriveBackupPrefs.setFolderUri(context, "content://com.android.externalstorage.documents/tree/primary%3ABackups")
        assertEquals(
            "content://com.android.externalstorage.documents/tree/primary%3ABackups",
            DriveBackupPrefs.getFolderUri(context)
        )
    }

    @Test
    fun `setFolderUri overwrites a previously chosen folder`() {
        val context = RuntimeEnvironment.getApplication()

        DriveBackupPrefs.setFolderUri(context, "content://first")
        DriveBackupPrefs.setFolderUri(context, "content://second")

        assertEquals("content://second", DriveBackupPrefs.getFolderUri(context))
    }
}
