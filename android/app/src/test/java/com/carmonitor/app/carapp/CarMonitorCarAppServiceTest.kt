package com.carmonitor.app.carapp

import android.content.pm.PackageManager
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment

@RunWith(RobolectricTestRunner::class)
class CarMonitorCarAppServiceTest {

    @Test
    fun `manifest declares minCarApiLevel required by CarAppService`() {
        val context = RuntimeEnvironment.getApplication()
        val appInfo = context.packageManager.getApplicationInfo(
            context.packageName,
            PackageManager.GET_META_DATA
        )

        assertTrue(
            "androidx.car.app.minCarApiLevel meta-data missing from AndroidManifest.xml " +
                "-- CarAppService.getAppInfo() throws IllegalArgumentException without it, " +
                "crashing every screen render on a real Android Auto host/DHU",
            appInfo.metaData?.containsKey("androidx.car.app.minCarApiLevel") == true
        )
    }
}
