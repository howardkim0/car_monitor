package com.carmonitor.app.carapp

import android.content.pm.PackageManager
import androidx.car.app.validation.HostValidator
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
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

    // Robolectric's test manifest is debuggable by default, matching the
    // debug-build side of createHostValidator()'s branch — the release
    // (allowlist-restricted) branch isn't reachable without faking
    // ApplicationInfo.flags, so it's checked by direct code review instead.
    @Test
    fun `createHostValidator allows all hosts on a debuggable build`() {
        val service = Robolectric.buildService(CarMonitorCarAppService::class.java).create().get()

        assertSame(HostValidator.ALLOW_ALL_HOSTS_VALIDATOR, service.createHostValidator())
    }

    @Test
    fun `onCreateSession returns a CarMonitorSession`() {
        val service = Robolectric.buildService(CarMonitorCarAppService::class.java).create().get()

        assertTrue(service.onCreateSession() is CarMonitorSession)
    }
}
