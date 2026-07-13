# Plan: On-Device SSH Keypair

> Companion to `DESIGN.md` §12's SSH-keygen TODO, and a prerequisite
> for the git-backup plan (`docs/plan-git-backup.md`) — implement this
> one first. Saved per `CLAUDE.md`'s "Planning docs are saved to docs/".

## Goal

Generate an SSH keypair on-device (no `ssh-keygen` binary exists on
Android) and expose the public key through a "Copy SSH public key"
button on the main status screen, so the user can register it as a
GitHub deploy key for `car_monitor_logs.git`. Idempotent: once a key
exists, reuse it forever — regenerating would silently orphan any
deploy key already registered on GitHub.

## Design

**Dependency**: add `golang.org/x/crypto` (`go get
golang.org/x/crypto/ssh`) to `go/go.mod`. Confirmed available via the
module proxy; `ssh.MarshalPrivateKey(key crypto.PrivateKey, comment
string) (*pem.Block, error)` and `ssh.NewPublicKey` /
`ssh.MarshalAuthorizedKey` are the exact functions needed — both
verified present in the current `golang.org/x/crypto/ssh` package.
This same dependency is reused by the git-backup plan for SSH auth, so
adding it here is not throwaway.

**New package `go/internal/sshkey/sshkey.go`**
```go
// EnsureKey returns the OpenSSH authorized-keys-format public key line
// for this device, generating and persisting a new ed25519 keypair
// under dir on first call. Idempotent: if a keypair already exists on
// disk, it's read back and reused rather than regenerated.
func EnsureKey(dir string) (publicLine string, err error)

// PrivateKeyPath returns the path EnsureKey wrote (or will write) the
// private key to — used by the git-backup plan to load it for SSH
// auth. dir is the same directory passed to EnsureKey.
func PrivateKeyPath(dir string) string
```
- Files: `dir/id_ed25519` (private, `0o600`) and `dir/id_ed25519.pub`
  (public, `0o644`).
- Generation: `ed25519.GenerateKey(rand.Reader)` (stdlib `crypto/ed25519`).
- Encoding: `ssh.NewPublicKey(pub)` → `ssh.MarshalAuthorizedKey(sshPub)`
  for the `.pub` file/return value; `ssh.MarshalPrivateKey(priv,
  "car-monitor")` → `pem.EncodeToMemory(block)` for the private key file.
- `EnsureKey`: if `dir/id_ed25519.pub` exists, read and return its
  contents (trimmed) without touching the private key or regenerating.
  Otherwise generate, write both files (private key first, `0o600`,
  then public), and return the new public line.

**`go/mobile/mobile.go`** (or a new `go/mobile/sshkey.go`)
```go
// SSHPublicKey returns this device's SSH public key (see
// internal/sshkey), generating one under storageDir/ssh on first call.
func SSHPublicKey(storageDir string) (string, error)
```
Thin wrapper: `sshkey.EnsureKey(filepath.Join(storageDir, "ssh"))`.

**`StatusActivity.kt`**
- In `onCreate()`, launch a coroutine on `Dispatchers.IO` calling
  `Mobile.sshPublicKey(filesDir.absolutePath)` (disk I/O — keep off the
  main thread even though it's fast after the first call) and cache
  the result in a field.
- Add a `copySshKeyButton`. Disabled until the key finishes loading;
  on click, copy the cached public key to the clipboard
  (`ClipboardManager`) and show a `Toast`/`Snackbar` confirmation.
- On failure (e.g. disk full), log via `Mobile.logError(...)` and leave
  the button disabled with a `Toast` explaining the key isn't
  available — don't crash the Activity over this.

**`res/layout/activity_status.xml`** / **`res/values/strings.xml`**
- Add `copySshKeyButton` alongside the other status-screen buttons and
  a `copy_ssh_key_button` string, plus a short confirmation string
  (e.g. `ssh_key_copied`).

## Tests

`go/internal/sshkey/sshkey_test.go` (table-driven, matching this
repo's convention): fresh temp dir → `EnsureKey` generates a key;
assert the public line starts with `"ssh-ed25519 "`, the private key
file decodes as valid PEM and round-trips through
`ssh.ParsePrivateKey`, and file permissions are `0o600`/`0o644`
(`os.Stat(...).Mode()`, Unix-only check — this repo's CI and dev
sandbox are both Linux, matching `internal/applog`'s existing
Unix-permission assumptions). Second call on the same dir returns the
identical public line and does not rewrite the private key file (assert
via `os.Stat` mtime, or by writing then chmod-ing the private key
unwritable and confirming a second `EnsureKey` call still succeeds by
never touching it).

## DESIGN.md update (same change)

- §12: remove the "on-device SSH keypair" TODO bullet.
- Add a short new §6.3 ("SSH key for log backup") describing where the
  key lives (`filesDir/ssh/id_ed25519{,.pub}`), that it's ed25519 via
  `crypto/ed25519` + `golang.org/x/crypto/ssh`, generated once and
  reused, and surfaced via the status screen's "Copy SSH public key"
  button for registering as a GitHub deploy key.
- §9 repo layout: note the new `internal/sshkey` package.
