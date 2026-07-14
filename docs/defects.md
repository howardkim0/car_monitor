# Past Defects

A log of real bugs found in this app: symptom reported, root cause, and
fix. Grouped by subsystem (not chronologically) so a new report can be
checked against past patterns in the same area — see `CLAUDE.md`'s
"Check past defects before investigating a new one."

This is a history doc, not a description of current behavior — for how a
system works *today*, read `DESIGN.md` (each entry below links back to
the section that documents the fix's current state). Add a new entry
here whenever a bug is found and fixed, per `CLAUDE.md`'s "every caught
bug gets a regression test" — the entry captures the story the test
alone can't (what the symptom looked like, why it happened), while
`DESIGN.md` stays timeless.

## OBD2 / ELM327 protocol

**Zero decoded readings despite a live, correctly-polling session**
(`84ed281`, `adf5c07`). Symptom: a full day's CSV had no data rows even
though the car was driving and the same dongle had worked moments
earlier in a different OBD2 app. Root cause: ELM327 adapter settings
(echo, linefeeds, spacing, headers, protocol) are RAM-resident and
persist across Bluetooth (dis)connects — a prior session (this app's or
another app's; headers-on is common, so multi-ECU responses can be
told apart) can leave the adapter in a state `parseResponseBytes`
can't read (e.g. a headered response like `7E8 04 41 0C 1A F8`, where
`7E8` doesn't fit the parser's single-byte-field assumption). That
failure was indistinguishable from ordinary ELM327 noise (skipped
silently, same as an echoed command or the `>` prompt), so polling
looked "live" — on schedule, zero decode errors — with zero readings
produced. Fix: `writeLoop` now sends a fixed AT init sequence once per
connection before requesting any PIDs (`obd2.InitCommands()`); a
follow-up fix (`adf5c07`) caught that the init loop wasn't flushing the
output stream the way the main polling loop already did, so the
commands were being written but not actually sent. See `DESIGN.md`
section 4 step 5 for the current design and why a full `ATZ` reset was
deliberately not used instead.

Follow-up (`b8c9f11`): since a decode failure at this layer is silent
by design (expected noise), the only way to diagnose a repeat of this
class of bug is seeing what the adapter actually sent — added raw
first-20-lines-per-session logging and a running received/decoded
count. See `DESIGN.md` section 12.

## Bluetooth device scanning / pairing

Three rounds of "the scan button doesn't work" reports against the
same feature, each round finding a real, distinct bug — worth reading
in order as an example of why re-review (not "no fresh log evidence,
close as unreproducible") is the right response to a repeat report.

**Round 1 — button gets stuck, no visible errors** (`ff999a8`).
Symptom: tapping "Pair Bluetooth OBD2 Scanners" appeared to hang, with
no way to stop it short of leaving the screen. Two real, independent
contributors: `startDiscovery()`'s boolean return value was never
checked, so a `false` return (adapter disabled, discovery already
running) left the button showing "Scanning…" forever with no scan
actually running; and a `SecurityException` from a denied permission
was only ever a log line, never a visible message — indistinguishable
from "no devices nearby." Same commit also made the scan button a
toggle (second tap calls `cancelDiscovery()` immediately, rather than
waiting out Android's own ~12s timeout) and added the `BLUETOOTH_SCAN`
manifest permission's `neverForLocation` flag, which turned out to be
this round's most likely primary cause (see Round 2) but wasn't
independently confirmed against the reporting device's Location
Services state at the time.

Caught during review of that same fix, before it shipped further:
a **double Toast** (`7f87ea2`) — the `SecurityException` catch block
and the subsequent `if (!started)` branch both showed a "could not
start" Toast, so a permission-denial failure showed the message twice.
Also a **dead string resource** (`device_scan_scanning`, `7f87ea2`)
left over once the toggle redesign superseded it, caught by grepping
source for the identifier.

**Round 2 — toggle works, but no discoverable devices ever show up**
(`42d21cc`). Root cause: `neverForLocation` (Round 1's fix) only
exempts **API 31+** from needing the system Location Services toggle
on for discovery to return results. This app's `minSdk` is 26, and on
API 26-30 there is no equivalent exemption at all —
`startDiscovery()` still returns `true` and `ACTION_DISCOVERY_FINISHED`
still fires on schedule, just with zero `ACTION_FOUND` broadcasts in
between, silently. Fix: `DeviceScanActivity.isLocationEnabled()` checks
the toggle directly before scanning on API < 31
(`LocationManager.isLocationEnabled()` on API 28+, the legacy
`Settings.Secure.LOCATION_MODE` field below that) and shows a direct
message if it's off. Also added live scan-status text ("Scanning… (N
found)" / "Scan finished — N found") so a genuinely-empty result reads
as confirmed zero, not "stuck" — see `DESIGN.md` sections 5.1 and 8.

**Round 3 — still "no devices," but the log evidence was stale**
(`93b8e5f`). A user-submitted `app.log` export was checked against the
Round 2 fix and found to contain *zero* lines postdating that fix's
release — every error in it predated the install, confirmed by
comparing log timestamps against the GitHub Actions run that published
that release. Not a new bug in the scan logic itself, but two real
gaps this exposed: `DeviceScanActivity` had no logging at all on its
non-error scan path, so even a same-session export couldn't have
confirmed whether a scan actually ran; and nothing in `app.log`
identified which commit produced it, making "is this actually the
fixed build?" a guessing game from timestamps alone. Fix: added
`Mobile.logDebug` calls for the location check result,
`startDiscovery()`'s return value, and each
`ACTION_FOUND`/`ACTION_DISCOVERY_FINISHED`; stamped every build with
`BuildConfig.GIT_COMMIT` (`git rev-parse` at build time), logged once
at app startup. See `DESIGN.md` section 6.2.

Building that same fix surfaced one more bug before it shipped: the
new log calls were added as direct, synchronous `Mobile.*` calls in
`onCreate()`/`startScan()`, which broke `./gradlew testDebugUnitTest`
with `UnsatisfiedLinkError` — Robolectric's plain JVM has no native
`libgojni.so` to load, so the first *synchronous* touch of the
gomobile-bound `Mobile` class during a test fails outright (existing
`Mobile.*` calls elsewhere in the codebase all happened to be
dispatched inside `scope.launch(Dispatchers.IO) { ... }` coroutines,
which avoided ever hitting this in a test run). Fixed by dispatching
the new calls the same way. See `DESIGN.md` section 6.2 for the
resulting rule ("every `Mobile.*` call from an Activity is dispatched
off the main thread").

## Git backup / SSH

**Every push failed: "cannot create known hosts callback"** (`dc02f6d`,
design in `3dfeab4`). Root cause: go-git's `HostKeyCallback` is `nil`
unless set explicitly, which falls back to reading `~/.ssh/known_hosts`
— a file that can never exist in this app's sandbox (no `$HOME`, no
`SSH_KNOWN_HOSTS`). Every push, automatic or manual, failed at the SSH
handshake regardless of network connectivity. Fix: pinned GitHub's own
published ed25519 host key (fetched from `https://api.github.com/meta`,
not transcribed from a docs page) via `ssh.FixedHostKey`.

**After that fix, pushes failed with "host key mismatch"** (`bd93fc6`).
Root cause: pinning the key alone wasn't sufficient — `HostKeyAlgorithms`
is a sibling field on the same `*ssh.PublicKeys` and, left unset,
`golang.org/x/crypto/ssh`'s own default algorithm list applies instead.
GitHub supports RSA, ECDSA, and ed25519 host keys, so without a stated
preference the negotiated key type wasn't guaranteed to be the ed25519
one this app pins — `FixedHostKey` then correctly (if confusingly)
rejected whatever different key type got negotiated. Fix: set
`HostKeyAlgorithms: ["ssh-ed25519"]` explicitly. Verified directly
against the real `github.com:22` before landing: the handshake fails
with exactly that error when unset, succeeds (reaching the next stage,
authentication) once set. See `DESIGN.md` section 7 for the current
SSH setup.

**A later "git push still not working" report was stale-APK evidence,
not a new bug** — the same class of issue as Bluetooth Round 3 above,
and the direct motivation for the `BuildConfig.GIT_COMMIT` stamp: two
log exports, taken minutes apart right after installing the latest
release, contained only error lines that predated that install (one
matching the `dc02f6d`-era symptom, one matching the `bd93fc6`-era
symptom — confirmed by bracketing both against the GitHub Actions
build-completion timestamps for each fix). No code change was needed
here beyond the diagnosability fix described in the Bluetooth Round 3
entry above.

**`mobile.go`'s reading-append path silently swallowed a failed
`store.Append`** (`f9d1930`, `_ = s.store.Append(r)`) — a storage
failure (e.g. disk full) would vanish with no trace. Fix: routed
through `internal/applog`'s `LogError` instead, alongside adding the
app/error log itself. See `DESIGN.md` section 6.2.

## Foreground service lifecycle ("Stop" unreliable)

**Tapping Stop appeared to work, then monitoring silently resumed**
(`7a75546`). Two independent real bugs: (1) `connectionLoop()`'s
`catch (e: Exception)` was also catching `CancellationException` — a
plain `Exception` subtype in Kotlin — and treating a requested stop as
just another failed connection attempt to retry; fixed by rethrowing
`CancellationException` explicitly before any broader catch. (2) A
Service stays alive as long as it's started *or* bound, and the
notification's Stop action goes straight to the service, bypassing
`StatusActivity` — so if the app happened to be open (bound) when Stop
was tapped, nothing unbound it and `stopSelf()` alone did nothing.
Fixed with a single teardown path (`stopServiceImmediately()`) that
closes the connection directly (unblocking any blocking
`connect()`/`read()` call already in flight, which cancellation alone
cannot interrupt) before updating state and stopping.

**The 5-minute no-connection timeout left a zombie service when the
app was open** (`3808042`) — the same "bound service ignores
`stopSelf()`" root cause as above, reached via a different trigger:
the timeout ran the same teardown from inside the service's own
coroutine, with no way to make an already-bound `StatusActivity`
unbind first. The notification vanished but the service stayed alive,
bound and idle, with the in-app status text frozen on stale "retrying"
text forever. Fixed by adding a distinct `ConnectionState.TimedOut`,
routed through the existing state-listener plumbing, so
`StatusActivity` unbinds itself in direct response to *any* terminal
state — not left as something incidental to which code path happened
to call `updateState()`. See `DESIGN.md` section 7 for the current
three-step teardown and the "resuming after a stop is always explicit"
rule this produced.

**Backfilled regression tests caught two more, smaller bugs** in the
same area while standing up the Android test suite (`1df576c`): a
failed `BluetoothSocket.connect()` wasn't closing the socket
afterward (leaking it on every failed connection attempt) — fixed by
extracting `connectSocket()` so the close-on-failure path is directly
testable; and a second `onStartCommand()` call arriving while a
`connectionLoop()` was still active (e.g. from a screen rotation
re-requesting permissions) would launch a second, concurrent
`connectionJob` rather than being a no-op — fixed by checking
`connectionJob?.isActive` before launching a new one.

## Android build / release

**CI-built debug APKs couldn't be installed over a previous install**
— every CI run used a fresh, ephemeral `debug.keystore` auto-generated
per-runner, so consecutive `latest` releases carried different
signatures and Android refused the in-place update (only a full
uninstall/reinstall worked). Fix: a persistent signing key, stored as
GitHub repo secrets and decoded to a runner-temp path at build time,
never committed. See `DESIGN.md` section 11's "Signing" subsection.
