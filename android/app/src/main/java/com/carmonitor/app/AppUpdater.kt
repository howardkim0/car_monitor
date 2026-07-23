package com.carmonitor.app

import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/**
 * Checks GitHub Releases for a newer debug-signed build than the one
 * currently running and reports whether it's worth installing. The
 * control flow (asset lookup, version comparison, the package-name
 * sanity guard) takes fetch/download/readApkInfo as plain function
 * parameters so it's directly unit-testable with fakes standing in for
 * real network/PackageManager I/O — the same seam-injection precedent
 * as ObdConnectionEngine's injected clock. See DESIGN.md section 12.
 */
object AppUpdater {
    const val RELEASE_METADATA_URL =
        "https://api.github.com/repos/howardkim0/car_monitor/releases/tags/latest"
    const val ASSET_NAME = "car-monitor-debug.apk"

    data class ApkInfo(val packageName: String, val versionCode: Int)

    sealed class Result {
        object UpToDate : Result()
        data class UpdateAvailable(val apkFile: File, val downloadedVersionCode: Int) : Result()
        data class Failed(val reason: String) : Result()
    }

    fun checkForUpdate(
        expectedPackageName: String,
        installedVersionCode: Int,
        destination: File,
        fetch: (String) -> String,
        download: (String, File) -> Unit,
        readApkInfo: (File) -> ApkInfo?,
    ): Result {
        val releaseJson = try {
            fetch(RELEASE_METADATA_URL)
        } catch (e: Exception) {
            return Result.Failed("fetch failed: $e")
        }

        val assetUrl = findAssetDownloadUrl(releaseJson)
            ?: return Result.Failed("asset '$ASSET_NAME' not found in release")

        try {
            download(assetUrl, destination)
        } catch (e: Exception) {
            return Result.Failed("download failed: $e")
        }

        val info = readApkInfo(destination)
            ?: return Result.Failed("could not read downloaded APK's manifest")

        if (info.packageName != expectedPackageName) {
            return Result.Failed("package name mismatch: ${info.packageName}")
        }

        return if (isNewer(info.versionCode, installedVersionCode)) {
            Result.UpdateAvailable(destination, info.versionCode)
        } else {
            Result.UpToDate
        }
    }

    /** Pure JSON parsing — no I/O, directly unit-testable. */
    fun findAssetDownloadUrl(releaseJson: String, assetName: String = ASSET_NAME): String? =
        try {
            val assets = JSONObject(releaseJson).getJSONArray("assets")
            (0 until assets.length())
                .asSequence()
                .map { assets.getJSONObject(it) }
                .firstOrNull { it.optString("name") == assetName }
                ?.optString("browser_download_url")
                ?.takeIf { it.isNotBlank() }
        } catch (e: Exception) {
            null
        }

    /** Pure comparison — no I/O, directly unit-testable. */
    fun isNewer(downloadedVersionCode: Int, installedVersionCode: Int): Boolean =
        downloadedVersionCode > installedVersionCode

    /**
     * Real network I/O — not unit-tested; reviewed by hand, same carve-out
     * class as AppQuit's Process.killProcess() call.
     */
    fun defaultFetch(url: String): String {
        val connection = URL(url).openConnection() as HttpURLConnection
        connection.setRequestProperty("Accept", "application/vnd.github+json")
        connection.setRequestProperty("User-Agent", "car_monitor-app")
        connection.connectTimeout = 15_000
        connection.readTimeout = 15_000
        return try {
            connection.inputStream.bufferedReader().use { it.readText() }
        } finally {
            connection.disconnect()
        }
    }

    /**
     * Real network I/O — not unit-tested; reviewed by hand. GitHub's asset
     * URL redirects (HTTPS to HTTPS, objects.githubusercontent.com), which
     * HttpURLConnection follows automatically.
     */
    fun defaultDownload(url: String, destination: File) {
        val connection = URL(url).openConnection() as HttpURLConnection
        connection.instanceFollowRedirects = true
        connection.connectTimeout = 15_000
        connection.readTimeout = 30_000
        try {
            connection.inputStream.use { input ->
                destination.outputStream().use { output -> input.copyTo(output) }
            }
        } finally {
            connection.disconnect()
        }
    }
}
