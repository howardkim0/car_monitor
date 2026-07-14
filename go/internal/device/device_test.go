package device

import (
	"os"
	"path/filepath"
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

func TestLoadSelectedNoFileReturnsOKFalse(t *testing.T) {
	dir := t.TempDir()
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on dir with no selection should return ok=false, got ok=%v", ok)
	}
	if err != nil {
		t.Errorf("LoadSelected on dir with no selection should return err=nil, got %v", err)
	}
	if profile != (Profile{}) {
		t.Errorf("LoadSelected should return empty Profile, got %+v", profile)
	}
}

func TestSaveSelectedThenLoadSelectedRoundTrips(t *testing.T) {
	tests := []struct {
		name    string
		mac     string
		devName string
	}{
		{name: "simple mac and name", mac: "00:1D:A5:68:98:8A", devName: "Garage OBDLink"},
		{name: "different mac and name", mac: "AA:BB:CC:DD:EE:FF", devName: "Test Device"},
		{name: "name with spaces", mac: "11:22:33:44:55:66", devName: "My Bluetooth Scanner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := SaveSelected(dir, tt.mac, tt.devName); err != nil {
				t.Fatalf("SaveSelected: %v", err)
			}
			profile, ok, err := LoadSelected(dir)
			if !ok {
				t.Fatalf("LoadSelected after SaveSelected returned ok=false, want true")
			}
			if err != nil {
				t.Fatalf("LoadSelected after SaveSelected returned err=%v, want nil", err)
			}
			if profile.MACAddress != tt.mac {
				t.Errorf("LoadSelected returned MAC %q, want %q", profile.MACAddress, tt.mac)
			}
			if profile.Name != tt.devName {
				t.Errorf("LoadSelected returned Name %q, want %q", profile.Name, tt.devName)
			}
			if profile.Protocol != ProtocolSPP {
				t.Errorf("LoadSelected returned Protocol %q, want %q", profile.Protocol, ProtocolSPP)
			}
		})
	}
}

func TestLoadSelectedMalformedFileSingleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedDeviceFileName)
	if err := os.WriteFile(path, []byte("00:1D:A5:68:98:8A\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on malformed file should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on malformed file should return non-nil error, got nil")
	}
	if profile != (Profile{}) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedDeviceFileName)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on empty file should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on empty file should return non-nil error, got nil")
	}
	if profile != (Profile{}) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileEmptyFirstLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedDeviceFileName)
	if err := os.WriteFile(path, []byte("\nGarage OBDLink\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on file with empty first line should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on file with empty first line should return non-nil error, got nil")
	}
	if profile != (Profile{}) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedUnreadableDirectory(t *testing.T) {
	// Create a file where we expect a directory, so ReadFile fails for a reason
	// other than not-exist
	parent := t.TempDir()
	blockedPath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(blockedPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Trying to read a file inside blockedPath will fail because it's not a directory
	attemptPath := filepath.Join(blockedPath, selectedDeviceFileName)
	profile, ok, err := LoadSelected(blockedPath)
	if ok {
		t.Errorf("LoadSelected on blocked path should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on blocked path should return non-nil error, got nil (path was %q)", attemptPath)
	}
	if profile != (Profile{}) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestSelectedOrDefaultReturnsDefaultWhenNothingSaved(t *testing.T) {
	dir := t.TempDir()
	profile := SelectedOrDefault(dir)
	if profile != Default() {
		t.Errorf("SelectedOrDefault on fresh dir returned %+v, want Default() %+v", profile, Default())
	}
}

func TestSelectedOrDefaultReturnsSavedProfile(t *testing.T) {
	dir := t.TempDir()
	mac := "AA:BB:CC:DD:EE:FF"
	devName := "Test Device"
	if err := SaveSelected(dir, mac, devName); err != nil {
		t.Fatalf("SaveSelected: %v", err)
	}
	profile := SelectedOrDefault(dir)
	if profile.MACAddress != mac {
		t.Errorf("SelectedOrDefault returned MAC %q, want %q", profile.MACAddress, mac)
	}
	if profile.Name != devName {
		t.Errorf("SelectedOrDefault returned Name %q, want %q", profile.Name, devName)
	}
	if profile.Protocol != ProtocolSPP {
		t.Errorf("SelectedOrDefault returned Protocol %q, want %q", profile.Protocol, ProtocolSPP)
	}
}

func TestSelectedOrDefaultFallsBackOnMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedDeviceFileName)
	if err := os.WriteFile(path, []byte("malformed"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile := SelectedOrDefault(dir)
	if profile != Default() {
		t.Errorf("SelectedOrDefault on malformed file returned %+v, want Default() %+v", profile, Default())
	}
}

func TestSaveSelectedCreatesDirectoryIfNotExist(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "nested", "device", "config")
	mac := "00:1D:A5:68:98:8A"
	devName := "Garage OBDLink"
	if err := SaveSelected(dir, mac, devName); err != nil {
		t.Fatalf("SaveSelected with nested nonexistent path: %v", err)
	}
	// Verify the file was created
	path := filepath.Join(dir, selectedDeviceFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	expected := mac + "\n" + devName + "\n"
	if string(data) != expected {
		t.Errorf("SaveSelected file content = %q, want %q", string(data), expected)
	}
}

func TestSaveSelectedWriteFailureWhenDirectoryIsAFile(t *testing.T) {
	// Create a file where we want to write a directory structure;
	// SaveSelected should fail when trying to MkdirAll
	parent := t.TempDir()
	blocker := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := filepath.Join(blocker, "device", "config")
	if err := SaveSelected(dir, "AA:BB:CC:DD:EE:FF", "Test"); err == nil {
		t.Errorf("SaveSelected should fail when MkdirAll can't create directories, got nil")
	}
}

func TestSaveSelectedWriteFileFailure(t *testing.T) {
	// Create a directory, then make it read-only, which prevents WriteFile
	// from creating new files in it
	parent := t.TempDir()
	dir := filepath.Join(parent, "readonly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755) // restore for cleanup
	if err := SaveSelected(dir, "AA:BB:CC:DD:EE:FF", "Test"); err == nil {
		t.Errorf("SaveSelected should fail when WriteFile can't write, got nil")
	}
}
