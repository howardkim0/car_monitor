plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlinx.kover")
}

// Stamped into BuildConfig so a log export can be matched to the exact
// commit that produced the running build — this session diagnosed the same
// git-push SSH failure twice from log evidence before realizing the
// installed APK predated the fix (see DESIGN.md section 12). Reads HEAD
// directly rather than requiring full history, so it works against CI's
// shallow checkout too; falls back to "unknown" if git isn't available at
// all (e.g. building from a source archive with no .git directory).
fun gitCommitHash(): String =
    try {
        val process = ProcessBuilder("git", "rev-parse", "--short=12", "HEAD")
            .directory(rootDir)
            .redirectErrorStream(true)
            .start()
        val output = process.inputStream.bufferedReader().readText().trim()
        if (process.waitFor() == 0 && output.isNotEmpty()) output else "unknown"
    } catch (e: Exception) {
        "unknown"
    }

// versionCode/versionName double as a per-commit build number: the repo's
// total commit count, so every commit gets a distinct, automatically
// increasing version with no manual tagging step. Unlike gitCommitHash()
// above, this needs *full* history (git log, not just the checked-out
// commit) — CI's checkout must use fetch-depth: 0, or every CI build would
// report "1". Falls back to 1 if git isn't available at all, matching
// gitCommitHash()'s "unknown" fallback in spirit (always build something
// valid rather than fail the build over a version-numbering nicety).
fun gitCommitCount(): Int =
    try {
        val process = ProcessBuilder("git", "rev-list", "--count", "HEAD")
            .directory(rootDir)
            .redirectErrorStream(true)
            .start()
        val output = process.inputStream.bufferedReader().readText().trim()
        if (process.waitFor() == 0) output.toIntOrNull() ?: 1 else 1
    } catch (e: Exception) {
        1
    }

android {
    namespace = "com.carmonitor.app"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.carmonitor.app"
        minSdk = 26
        targetSdk = 34
        val commitCount = gitCommitCount()
        versionCode = commitCount
        versionName = "0.$commitCount"
        buildConfigField("String", "GIT_COMMIT", "\"${gitCommitHash()}\"")
    }

    buildFeatures {
        buildConfig = true
    }

    // Set only in CI (see .github/workflows/release-apk.yml): signs the debug
    // build with a persistent keystore instead of the ephemeral, per-runner
    // debug.keystore AGP auto-generates on a fresh machine. Without this,
    // every CI build carries a different signature and Android refuses to
    // install a new one over the last (it looks like a different app) —
    // installing an update requires uninstalling first. Absent locally, so
    // `./gradlew assembleDebug` on a dev machine is unaffected and keeps
    // using that machine's own debug.keystore. See DESIGN.md section 11.
    val ciKeystorePath = System.getenv("CM_RELEASE_KEYSTORE_PATH")

    if (ciKeystorePath != null) {
        signingConfigs {
            create("ci") {
                storeFile = file(ciKeystorePath)
                storePassword = System.getenv("CM_RELEASE_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("CM_RELEASE_KEY_ALIAS")
                keyPassword = System.getenv("CM_RELEASE_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
        debug {
            if (ciKeystorePath != null) {
                signingConfig = signingConfigs.getByName("ci")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            isReturnDefaultValues = true
        }
    }
}

dependencies {
    implementation(files("libs/mobile.aar"))
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.0")

    // See DESIGN.md section 13: Robolectric (Android framework on the
    // plain JVM, no emulator/device needed) plus MockK for BluetoothSocket
    // and similar collaborators Robolectric doesn't simulate.
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.robolectric:robolectric:4.13")
    testImplementation("io.mockk:mockk:1.13.11")
}
