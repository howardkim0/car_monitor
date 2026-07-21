package com.carmonitor.app

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Plain JUnit4 — no RobolectricTestRunner, matching ObdConnectionEngineTest's
 * rationale: BackupLoops takes its ObdMobile and Drive-backup callbacks via
 * its constructor, so cadence/failure-handling is testable under
 * kotlinx-coroutines-test virtual time without a Service or real 5-minute
 * waits.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class BackupLoopsTest {

    @Test
    fun `gitBackupLoop calls syncLogsIfNeeded every BACKUP_CHECK_INTERVAL_MS`() = runTest {
        val mobile = FakeObdMobile()
        val backupLoops = BackupLoops(
            mobile = mobile,
            storageDir = "storageDir",
            getDriveFolderUri = { null },
            syncDrive = {}
        )

        val job = launch { backupLoops.gitBackupLoop() }
        runCurrent()
        assertEquals("no sync before the first interval elapses", 0, mobile.syncLogsCallCount)

        advanceTimeBy(5 * 60 * 1_000L) // BACKUP_CHECK_INTERVAL_MS
        runCurrent()
        assertEquals(1, mobile.syncLogsCallCount)

        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(2, mobile.syncLogsCallCount)

        job.cancel()
    }

    @Test
    fun `gitBackupLoop keeps looping after syncLogsIfNeeded throws`() = runTest {
        var callCount = 0
        val backupLoops = BackupLoops(
            mobile = object : ObdMobile by FakeObdMobile() {
                override fun syncLogsIfNeeded(storageDir: String) {
                    callCount++
                    if (callCount == 1) throw java.io.IOException("no cell signal")
                }
            },
            storageDir = "storageDir",
            getDriveFolderUri = { null },
            syncDrive = {}
        )

        val job = launch { backupLoops.gitBackupLoop() }
        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(1, callCount)

        // A failed attempt must not kill the loop — it retries next cycle.
        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(2, callCount)

        job.cancel()
    }

    @Test
    fun `driveBackupLoop is a no-op until a folder is configured`() = runTest {
        var folderUri: String? = null
        val syncCalls = mutableListOf<String>()
        val backupLoops = BackupLoops(
            mobile = FakeObdMobile(),
            storageDir = "storageDir",
            getDriveFolderUri = { folderUri },
            syncDrive = { uri -> syncCalls += uri }
        )

        val job = launch { backupLoops.driveBackupLoop() }
        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals("no folder configured yet — must not call syncDrive", emptyList<String>(), syncCalls)

        folderUri = "content://tree/1234"
        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(listOf("content://tree/1234"), syncCalls)

        job.cancel()
    }

    @Test
    fun `driveBackupLoop keeps looping after syncDrive throws`() = runTest {
        var syncCallCount = 0
        val backupLoops = BackupLoops(
            mobile = FakeObdMobile(),
            storageDir = "storageDir",
            getDriveFolderUri = { "content://tree/1234" },
            syncDrive = {
                syncCallCount++
                if (syncCallCount == 1) throw java.io.IOException("Drive app uninstalled")
            }
        )

        val job = launch { backupLoops.driveBackupLoop() }
        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(1, syncCallCount)

        advanceTimeBy(5 * 60 * 1_000L)
        runCurrent()
        assertEquals(2, syncCallCount)

        job.cancel()
    }
}
