# Plan: Manual Log Export

> Companion to `DESIGN.md` §12's export TODO. Saved per `CLAUDE.md`'s
> "Planning docs are saved to docs/".

## Goal

A button on the status screen that lets the user get the on-device
logs (reading-log CSVs + app.log) off the phone without `adb`, using
Android's normal share sheet.

## Why this stays entirely in `android/`

Zipping files and firing a share `Intent` is Android plumbing, not
business logic — there's no decision here `internal/*` packages would
otherwise own (contrast with retention's count/keep policy, which is a
real business rule and belongs in Go). Keeping it in Kotlin avoids
adding a zip dependency to the Go side and avoids a gomobile round-trip
for a one-shot, UI-triggered action. This is a deliberate split
decision worth a one-line note in DESIGN.md §3 or §9 once implemented.

## Design

**New file `android/app/src/main/java/com/carmonitor/app/LogExporter.kt`**
```kotlin
object LogExporter {
    // Zips every readings-*.csv in readingsDir plus appLogFile (and
    // appLogFile + ".1" if present) into outputZip. Pure file I/O, no
    // Android framework dependency beyond java.io/java.util.zip, so
    // it's directly unit-testable without Robolectric.
    fun buildZip(readingsDir: File, appLogFile: File, outputZip: File)
}
```
- Uses `java.util.zip.ZipOutputStream`; skip missing files silently
  (e.g. no app.log.1 yet) rather than erroring.

**`StatusActivity.kt`**
- Add `exportButton` (wired like the existing `batteryOptimizationButton`).
- On click, in a coroutine on `Dispatchers.IO`:
  1. `LogExporter.buildZip(File(filesDir, "readings"), File(filesDir,
     "app.log"), File(cacheDir, "car_monitor_logs_<timestamp>.zip"))`
     — `cacheDir`, not `filesDir`, since this is a transient export
     artifact, not something to retain/prune.
  2. Get a `content://` URI via `FileProvider.getUriForFile(...)`.
  3. Back on the main thread: fire
     `Intent.createChooser(Intent(ACTION_SEND).apply { type =
     "application/zip"; putExtra(EXTRA_STREAM, uri);
     addFlags(FLAG_GRANT_READ_URI_PERMISSION) }, ...)`.
  4. Wrap the whole thing in try/catch, `Mobile.logError(...)` on
     failure (matching this file's existing error-handling style) plus
     a `Toast` telling the user the export failed — don't let a failed
     export crash the Activity.

**`AndroidManifest.xml`**
- Add a `<provider android:name="androidx.core.content.FileProvider"
  android:authorities="${applicationId}.fileprovider" android:exported="false"
  android:grantUriPermissions="true">` with a `<meta-data>` pointing at
  a new `res/xml/file_paths.xml`.
- `androidx.core:core-ktx` (already a dependency, check
  `app/build.gradle.kts`) provides `FileProvider`; if the project
  isn't already pulling in `androidx.core` at a version with
  `FileProvider`, add it explicitly.

**`res/xml/file_paths.xml`** (new)
```xml
<paths>
    <cache-path name="logs" path="." />
</paths>
```

**`res/layout/activity_status.xml`** / **`res/values/strings.xml`**
- Add an `exportButton` alongside `batteryOptimizationButton` and a
  `export_logs_button` string.

## Tests

`android/app/src/test/java/com/carmonitor/app/LogExporterTest.kt`
(JUnit, no Robolectric needed — `buildZip` touches no Android APIs):
write a couple of fake CSV files and an app.log to a temp dir, call
`buildZip`, then read the output zip back with
`java.util.zip.ZipInputStream` and assert the expected entry names and
contents are present. This is the "targets regressions/real behavior"
bar DESIGN.md §13 sets for `android/` tests — cheap to write and
directly exercises the one piece of this feature that isn't just
gluing together Android framework calls.

## DESIGN.md update (same change)

- §12: remove the "manual log export" TODO bullet.
- §3 or §9: one line noting `LogExporter` is deliberately Kotlin-only
  (see "Why this stays entirely in `android/`" above) as a concrete
  example of a feature that doesn't need to cross into Go.
