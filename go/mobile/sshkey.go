package mobile

import (
	"path/filepath"

	"github.com/howardkim0/car_monitor/go/internal/sshkey"
)

// SSHPublicKey returns this device's SSH public key (see internal/sshkey),
// generating one under storageDir/ssh on first call. Subsequent calls
// retrieve and return the existing key without regeneration.
func SSHPublicKey(storageDir string) (string, error) {
	return sshkey.EnsureKey(filepath.Join(storageDir, "ssh"))
}
