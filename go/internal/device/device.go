// Package device holds the Bluetooth OBD2 adapter profiles Car Monitor
// knows how to talk to. See DESIGN.md section 5.1: today there is exactly
// one hardcoded device, but every consumer depends on Profile rather than a
// literal MAC address so a second adapter is additive, not a rewrite.
package device

import "strings"

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
		MACAddress: "AA:BB:CC:DD:EE:FF",
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
