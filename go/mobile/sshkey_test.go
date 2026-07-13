package mobile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHPublicKeyGeneratesKeyUnderSshSubdir(t *testing.T) {
	storageDir := t.TempDir()

	pubKey, err := SSHPublicKey(storageDir)
	if err != nil {
		t.Fatalf("SSHPublicKey: %v", err)
	}

	// Verify the key was generated under storageDir/ssh.
	privPath := filepath.Join(storageDir, "ssh", "id_ed25519")
	pubPath := filepath.Join(storageDir, "ssh", "id_ed25519.pub")

	if _, err := os.Stat(privPath); err != nil {
		t.Errorf("private key not found at %q: %v", privPath, err)
	}
	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("public key not found at %q: %v", pubPath, err)
	}

	// Verify the returned key is in OpenSSH format.
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want to start with 'ssh-ed25519 '", pubKey)
	}
}

func TestSSHPublicKeyIsIdempotent(t *testing.T) {
	storageDir := t.TempDir()

	key1, err := SSHPublicKey(storageDir)
	if err != nil {
		t.Fatalf("first SSHPublicKey: %v", err)
	}

	key2, err := SSHPublicKey(storageDir)
	if err != nil {
		t.Fatalf("second SSHPublicKey: %v", err)
	}

	if key1 != key2 {
		t.Errorf("public keys differ: first=%q, second=%q", key1, key2)
	}
}
