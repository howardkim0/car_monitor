package com.carmonitor.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class UpdateDismissalPrefsTest {

    @Test
    fun `defaults to nothing dismissed`() {
        val context = RuntimeEnvironment.getApplication()
        assertFalse(UpdateDismissalPrefs.isDismissed(context, 112))
    }

    @Test
    fun `setDismissed persists the versionCode`() {
        val context = RuntimeEnvironment.getApplication()

        UpdateDismissalPrefs.setDismissed(context, 112)

        assertTrue(UpdateDismissalPrefs.isDismissed(context, 112))
    }

    @Test
    fun `dismissing one versionCode does not suppress a newer one`() {
        val context = RuntimeEnvironment.getApplication()

        UpdateDismissalPrefs.setDismissed(context, 112)

        assertFalse(
            "a later, higher versionCode must not be treated as already dismissed",
            UpdateDismissalPrefs.isDismissed(context, 113)
        )
    }
}
