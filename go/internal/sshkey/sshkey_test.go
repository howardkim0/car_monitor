package sshkey

import (
	"crypto"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestEnsureKeyGeneratesNewKeyInFreshDir(t *testing.T) {
	dir := t.TempDir()

	pubLine, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}

	// Check that public key line starts with "ssh-ed25519 ".
	if !strings.HasPrefix(pubLine, "ssh-ed25519 ") {
		t.Errorf("public key line = %q, want to start with 'ssh-ed25519 '", pubLine)
	}

	// Check that both files exist.
	privPath := filepath.Join(dir, "id_ed25519")
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if _, err := os.Stat(privPath); err != nil {
		t.Errorf("private key file %q: %v", privPath, err)
	}
	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("public key file %q: %v", pubPath, err)
	}
}

func TestEnsureKeyReturnsIdempotentPublicKey(t *testing.T) {
	dir := t.TempDir()

	pubLine1, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("first EnsureKey: %v", err)
	}

	pubLine2, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("second EnsureKey: %v", err)
	}

	if pubLine1 != pubLine2 {
		t.Errorf("public key lines differ: first=%q, second=%q", pubLine1, pubLine2)
	}
}

func TestEnsureKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()

	_, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}

	privPath := filepath.Join(dir, "id_ed25519")
	privInfo, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if privMode := privInfo.Mode().Perm(); privMode != 0o600 {
		t.Errorf("private key file mode = %#o, want 0o600", privMode)
	}

	pubPath := filepath.Join(dir, "id_ed25519.pub")
	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("stat public key: %v", err)
	}
	if pubMode := pubInfo.Mode().Perm(); pubMode != 0o644 {
		t.Errorf("public key file mode = %#o, want 0o644", pubMode)
	}
}

func TestEnsureKeyPrivateKeyParses(t *testing.T) {
	dir := t.TempDir()

	_, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}

	privPath := filepath.Join(dir, "id_ed25519")
	privData, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(privData)
	if err != nil {
		t.Errorf("ssh.ParsePrivateKey: %v", err)
	}

	// Verify it's usable as a signer (has a PublicKey method).
	if signer.PublicKey() == nil {
		t.Error("parsed signer has no public key")
	}
}

func TestEnsureKeyDoesNotRewritePrivateKey(t *testing.T) {
	dir := t.TempDir()

	_, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("first EnsureKey: %v", err)
	}

	privPath := filepath.Join(dir, "id_ed25519")

	// Get the original mtime.
	info1, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key (first): %v", err)
	}
	mtime1 := info1.ModTime()

	// Sleep a tiny bit to ensure mtime would differ if the file was
	// rewritten, then call EnsureKey again.
	time.Sleep(10 * time.Millisecond)

	_, err = EnsureKey(dir)
	if err != nil {
		t.Fatalf("second EnsureKey: %v", err)
	}

	// Verify mtime is unchanged.
	info2, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key (second): %v", err)
	}
	mtime2 := info2.ModTime()

	if !mtime1.Equal(mtime2) {
		t.Errorf("private key mtime changed: %v → %v (file was rewritten)", mtime1, mtime2)
	}
}

func TestPrivateKeyPath(t *testing.T) {
	dir := "/some/path"
	want := "/some/path/id_ed25519"
	got := PrivateKeyPath(dir)
	if got != want {
		t.Errorf("PrivateKeyPath(%q) = %q, want %q", dir, got, want)
	}
}

func TestEnsureKeyErrorsWhenDirCannotBeCreated(t *testing.T) {
	// Create a file where a directory component needs to be.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Try to create a keypair under a path that would require the blocker
	// to become a directory.
	if _, err := EnsureKey(filepath.Join(blocker, "ssh")); err == nil {
		t.Error("EnsureKey with an unmakeable dir should error, got nil")
	}
}

func TestEnsureKeyErrorsWhenPrivateKeyWriteFails(t *testing.T) {
	dir := t.TempDir()

	// Create a directory where the private key file needs to be written.
	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.Mkdir(privPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	// Try to generate a keypair; the write to privPath should fail.
	if _, err := EnsureKey(dir); err == nil {
		t.Error("EnsureKey with private key path blocked by directory should error, got nil")
	}
}

func TestEnsureKeyErrorsWhenPublicKeyWriteFails(t *testing.T) {
	dir := t.TempDir()

	// Write the private key manually so EnsureKey won't regenerate it.
	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("WriteFile private key: %v", err)
	}

	// Create a directory where the public key file needs to be written.
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if err := os.Mkdir(pubPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	// Try to generate a keypair; the write to pubPath should fail.
	if _, err := EnsureKey(dir); err == nil {
		t.Error("EnsureKey with public key path blocked by directory should error, got nil")
	}
}

// brokenReader is an io.Reader that always returns an error.
type brokenReader struct{}

func (br brokenReader) Read(p []byte) (int, error) {
	return 0, errors.New("broken random source")
}

func TestEnsureKeyErrorsWhenRandSourceFails(t *testing.T) {
	dir := t.TempDir()

	// Save the original randSource and restore it after the test.
	oldRandSource := randSource
	defer func() { randSource = oldRandSource }()

	// Override randSource with a broken reader.
	randSource = brokenReader{}

	// EnsureKey should error when trying to generate a keypair.
	_, err := EnsureKey(dir)
	if err == nil {
		t.Error("EnsureKey with broken random source should error, got nil")
	}

	// Verify that no files were created.
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if _, err := os.Stat(pubPath); err == nil {
		t.Error("public key file should not exist after failed key generation")
	}
}

// brokenMarshalPrivateKey is a function that always returns an error.
func brokenMarshalPrivateKey(key crypto.PrivateKey, comment string) (*pem.Block, error) {
	return nil, errors.New("broken marshal private key")
}

func TestEnsureKeyErrorsWhenMarshalPrivateKeyFails(t *testing.T) {
	dir := t.TempDir()

	// Save the original marshalPrivateKeyFunc and restore it after the test.
	oldMarshalFunc := marshalPrivateKeyFunc
	defer func() { marshalPrivateKeyFunc = oldMarshalFunc }()

	// Override marshalPrivateKeyFunc with a broken version.
	marshalPrivateKeyFunc = brokenMarshalPrivateKey

	// EnsureKey should error when trying to marshal the private key.
	_, err := EnsureKey(dir)
	if err == nil {
		t.Error("EnsureKey with broken marshal private key should error, got nil")
	}

	// Verify that no files were created.
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if _, err := os.Stat(pubPath); err == nil {
		t.Error("public key file should not exist after failed private key marshaling")
	}
}

// brokenNewPublicKey is a function that always returns an error.
func brokenNewPublicKey(key interface{}) (ssh.PublicKey, error) {
	return nil, errors.New("broken new public key")
}

func TestEnsureKeyErrorsWhenNewPublicKeyFails(t *testing.T) {
	dir := t.TempDir()

	// Save the original newPublicKeyFunc and restore it after the test.
	oldNewPubFunc := newPublicKeyFunc
	defer func() { newPublicKeyFunc = oldNewPubFunc }()

	// Override newPublicKeyFunc with a broken version.
	newPublicKeyFunc = brokenNewPublicKey

	// EnsureKey should error when trying to create the public key.
	_, err := EnsureKey(dir)
	if err == nil {
		t.Error("EnsureKey with broken new public key should error, got nil")
	}

	// Verify that no public key file was created (private key file might exist).
	pubPath := filepath.Join(dir, "id_ed25519.pub")
	if _, err := os.Stat(pubPath); err == nil {
		t.Error("public key file should not exist after failed public key creation")
	}
}
