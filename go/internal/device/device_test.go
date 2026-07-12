package device

import (
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	got := Default()
	if got.MACAddress != "00:1D:A5:68:98:8A" {
		t.Errorf("Default().MACAddress = %q, want the hardcoded garage adapter MAC", got.MACAddress)
	}
	if got.Protocol != ProtocolSPP {
		t.Errorf("Default().Protocol = %q, want %q", got.Protocol, ProtocolSPP)
	}
}

func TestByMAC(t *testing.T) {
	tests := []struct {
		name   string
		mac    string
		wantOK bool
	}{
		{name: "exact match", mac: "00:1D:A5:68:98:8A", wantOK: true},
		{name: "case insensitive match", mac: "00:1d:a5:68:98:8a", wantOK: true},
		{name: "unknown mac", mac: "11:22:33:44:55:66", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, ok := ByMAC(tt.mac)
			if ok != tt.wantOK {
				t.Fatalf("ByMAC(%q) ok = %v, want %v", tt.mac, ok, tt.wantOK)
			}
			if ok && !strings.EqualFold(profile.MACAddress, tt.mac) {
				t.Errorf("ByMAC(%q) returned profile with MAC %q", tt.mac, profile.MACAddress)
			}
		})
	}
}
