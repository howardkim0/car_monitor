// Package sshkey generates and manages an on-device SSH keypair for
// authenticating log backups to a remote git repository. The keypair is
// idempotent: once generated and persisted, it is reused forever rather
// than regenerated, since the public key must be registered as a deploy
// key upstream (regenerating it would silently orphan any key already
// registered).
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// EnsureKey returns the OpenSSH authorized-keys-format public key line
// for this device, generating and persisting a new ed25519 keypair under
// dir on first call. Idempotent: if a keypair already exists on disk, it
// is read back and reused rather than regenerated.
//
// The private key is written to dir/id_ed25519 with mode 0o600; the
// public key is written to dir/id_ed25519.pub with mode 0o644. The
// private key is written first, so a partial failure (e.g., disk full
// after writing the private key but before the public one) never leaves
// the public key on disk without its matching private key.
func EnsureKey(dir string) (publicLine string, err error) {
	pubPath := filepath.Join(dir, "id_ed25519.pub")

	// Check if public key already exists; if so, read and return it
	// without touching anything else.
	if data, err := os.ReadFile(pubPath); err == nil {
		return string(data), nil
	}

	// Public key does not exist, so generate a new keypair.
	pubKey, privKey, err := generateKeypair()
	if err != nil {
		return "", fmt.Errorf("generate keypair: %w", err)
	}

	// Create the directory if it doesn't exist.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create ssh key dir %q: %w", dir, err)
	}

	// Encode the private key to PEM format.
	privBlock, err := marshalPrivateKeyFunc(privKey, "car-monitor")
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(privBlock)

	// Write the private key first (0o600).
	privPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return "", fmt.Errorf("write private key to %q: %w", privPath, err)
	}

	// Encode the public key to OpenSSH format.
	sshPubKey, err := newPublicKeyFunc(pubKey)
	if err != nil {
		return "", fmt.Errorf("create ssh public key: %w", err)
	}
	pubLine := string(ssh.MarshalAuthorizedKey(sshPubKey))

	// Write the public key (0o644).
	if err := os.WriteFile(pubPath, []byte(pubLine), 0o644); err != nil {
		return "", fmt.Errorf("write public key to %q: %w", pubPath, err)
	}

	return pubLine, nil
}

// PrivateKeyPath returns the path EnsureKey wrote (or will write) the
// private key to — used by the git-backup plan to load it for SSH
// authentication. dir is the same directory passed to EnsureKey.
func PrivateKeyPath(dir string) string {
	return filepath.Join(dir, "id_ed25519")
}

// randSource is the random source used for key generation; exposed as a
// package-level variable so tests can override it with a broken source to
// exercise error paths.
var randSource io.Reader = rand.Reader

// marshalPrivateKeyFunc is the function used to marshal private keys;
// exposed as a package-level variable so tests can override it to
// exercise error paths.
var marshalPrivateKeyFunc = ssh.MarshalPrivateKey

// newPublicKeyFunc is the function used to create SSH public keys;
// exposed as a package-level variable so tests can override it to
// exercise error paths.
var newPublicKeyFunc = ssh.NewPublicKey

// generateKeypair generates a new ed25519 public/private keypair using
// randSource for randomness.
func generateKeypair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(randSource)
	if err != nil {
		return nil, nil, fmt.Errorf("ed25519.GenerateKey: %w", err)
	}
	return pub, priv, nil
}
