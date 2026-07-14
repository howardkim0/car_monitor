// Package device holds the Bluetooth OBD2 adapter profiles Car Monitor
// knows how to talk to. See DESIGN.md section 5.1: today there is exactly
// one hardcoded device, but every consumer depends on Profile rather than a
// literal MAC address so a second adapter is additive, not a rewrite.
package device

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Protocol identifies how the app talks to a paired Bluetooth device.
type Protocol string

// ProtocolSPP is classic Bluetooth RFCOMM using the Serial Port Profile,
// which is what virtually all ELM327-compatible OBD2 dongles use.
const ProtocolSPP Protocol = "spp"

// Profile describes one Bluetooth OBD2 adapter.
type Profile struct {
	Name       string
	MACAddress string
	Protocol   Protocol
}

// known lists every Bluetooth device profile the app is aware of. Adding
// support for a new adapter means appending here; internal/obd2 and the
// Android shell only ever depend on Profile, never on a literal MAC.
var known = []Profile{
	{
		Name:       "Garage OBDLink",
		MACAddress: "00:1D:A5:68:98:8A",
		Protocol:   ProtocolSPP,
	},
}

// Default returns the Bluetooth device profile the app should connect to.
func Default() Profile {
	return known[0]
}

// ByMAC looks up a known profile by MAC address, case-insensitively. It
// returns false if no known profile matches.
func ByMAC(mac string) (Profile, bool) {
	for _, p := range known {
		if strings.EqualFold(p.MACAddress, mac) {
			return p, true
		}
	}
	return Profile{}, false
}

const selectedDeviceFileName = "selected_device.txt"

// SaveSelected persists mac and name as the user's chosen device,
// overriding Default() until changed again — see DESIGN.md section 5.1.
// Written as "mac\nname\n", matching internal/applog's/internal/sshkey's
// plain-text-over-JSON philosophy for small, simple, human-inspectable
// on-device state.
func SaveSelected(dir, mac, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create device selection dir %q: %w", dir, err)
	}
	data := mac + "\n" + name + "\n"
	path := filepath.Join(dir, selectedDeviceFileName)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("write device selection %q: %w", path, err)
	}
	return nil
}

// LoadSelected reads back a persisted selection under dir, if one
// exists. ok is false (not an error) if SaveSelected has never been
// called there — a fresh install has no override yet.
func LoadSelected(dir string) (profile Profile, ok bool, err error) {
	path := filepath.Join(dir, selectedDeviceFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("read device selection %q: %w", path, err)
	}
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 || lines[0] == "" || lines[1] == "" {
		return Profile{}, false, fmt.Errorf("device selection %q is malformed", path)
	}
	return Profile{MACAddress: lines[0], Name: lines[1], Protocol: ProtocolSPP}, true, nil
}

// SelectedOrDefault returns LoadSelected's result if a valid selection
// exists under dir, else Default(). A read/parse error is treated the
// same as "no selection" — best-effort, falls back rather than
// blocking every connection attempt over a corrupted preference file.
func SelectedOrDefault(dir string) Profile {
	profile, ok, err := LoadSelected(dir)
	if err != nil || !ok {
		return Default()
	}
	return profile
}
