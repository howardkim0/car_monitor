package com.carmonitor.app

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Tests for AppUpdater's fetch/parse/compare control flow, with
 * fetch/download/readApkInfo faked so no real network or PackageManager
 * I/O runs — see DESIGN.md section 12. Runs under Robolectric (rather
 * than plain JUnit, unlike LogExporterTest) because org.json.JSONObject
 * is part of the Android platform stub jar and needs Robolectric's real
 * shadow implementation to actually parse, not just avoid throwing.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AppUpdaterTest {

    private fun releaseJson(assetName: String?, downloadUrl: String?): String {
        val assetsJson = if (assetName == null) {
            "[]"
        } else {
            """[{"name": "$assetName", "browser_download_url": "$downloadUrl"}]"""
        }
        return """{"tag_name": "latest", "assets": $assetsJson}"""
    }

    @Test
    fun `findAssetDownloadUrl returns the url for a matching asset name`() {
        val json = releaseJson(AppUpdater.ASSET_NAME, "https://example.com/car-monitor-debug.apk")
        assertEquals("https://example.com/car-monitor-debug.apk", AppUpdater.findAssetDownloadUrl(json))
    }

    @Test
    fun `findAssetDownloadUrl returns null when the asset name is not present`() {
        val json = releaseJson("some-other-file.txt", "https://example.com/some-other-file.txt")
        assertNull(AppUpdater.findAssetDownloadUrl(json))
    }

    @Test
    fun `findAssetDownloadUrl returns null when there are no assets at all`() {
        assertNull(AppUpdater.findAssetDownloadUrl(releaseJson(null, null)))
    }

    @Test
    fun `findAssetDownloadUrl returns null for malformed JSON`() {
        assertNull(AppUpdater.findAssetDownloadUrl("not json at all"))
    }

    @Test
    fun `isNewer is true only when downloaded versionCode is strictly greater`() {
        assertTrue(AppUpdater.isNewer(112, 111))
        assertTrue(!AppUpdater.isNewer(111, 111))
        assertTrue(!AppUpdater.isNewer(110, 111))
    }

    @Test
    fun `checkForUpdate returns UpdateAvailable when the downloaded versionCode is newer`() {
        val destination = File.createTempFile("test-update", ".apk")
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = destination,
            fetch = { releaseJson(AppUpdater.ASSET_NAME, "https://example.com/app.apk") },
            download = { _, _ -> },
            readApkInfo = { AppUpdater.ApkInfo("com.carmonitor.app", 112) },
        )
        val updateAvailable = result as? AppUpdater.Result.UpdateAvailable
        assertEquals(112, updateAvailable?.downloadedVersionCode)
        assertEquals(destination, updateAvailable?.apkFile)
    }

    @Test
    fun `checkForUpdate returns UpToDate when downloaded versionCode is not newer`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { releaseJson(AppUpdater.ASSET_NAME, "https://example.com/app.apk") },
            download = { _, _ -> },
            readApkInfo = { AppUpdater.ApkInfo("com.carmonitor.app", 111) },
        )
        assertEquals(AppUpdater.Result.UpToDate, result)
    }

    @Test
    fun `checkForUpdate returns Failed when the asset is missing from the release`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { releaseJson(null, null) },
            download = { _, _ -> },
            readApkInfo = { AppUpdater.ApkInfo("com.carmonitor.app", 112) },
        )
        assertTrue(result is AppUpdater.Result.Failed)
    }

    @Test
    fun `checkForUpdate returns Failed when readApkInfo cannot read the download`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { releaseJson(AppUpdater.ASSET_NAME, "https://example.com/app.apk") },
            download = { _, _ -> },
            readApkInfo = { null },
        )
        assertTrue(result is AppUpdater.Result.Failed)
    }

    @Test
    fun `checkForUpdate returns Failed when the downloaded package name does not match`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { releaseJson(AppUpdater.ASSET_NAME, "https://example.com/app.apk") },
            download = { _, _ -> },
            readApkInfo = { AppUpdater.ApkInfo("com.evil.app", 999) },
        )
        assertTrue(result is AppUpdater.Result.Failed)
    }

    @Test
    fun `checkForUpdate returns Failed when fetch throws`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { throw java.io.IOException("network down") },
            download = { _, _ -> },
            readApkInfo = { AppUpdater.ApkInfo("com.carmonitor.app", 112) },
        )
        assertTrue(result is AppUpdater.Result.Failed)
    }

    @Test
    fun `checkForUpdate returns Failed when download throws`() {
        val result = AppUpdater.checkForUpdate(
            expectedPackageName = "com.carmonitor.app",
            installedVersionCode = 111,
            destination = File.createTempFile("test-update", ".apk"),
            fetch = { releaseJson(AppUpdater.ASSET_NAME, "https://example.com/app.apk") },
            download = { _, _ -> throw java.io.IOException("download failed") },
            readApkInfo = { AppUpdater.ApkInfo("com.carmonitor.app", 112) },
        )
        assertTrue(result is AppUpdater.Result.Failed)
    }
}
