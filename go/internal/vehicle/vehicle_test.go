package vehicle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultProfile(t *testing.T) {
	p := Default()
	if p.Make != "Subaru" || p.Model != "Forester" || p.Year != 2023 {
		t.Fatalf("Default() = %+v, want the hardcoded 2023 Subaru Forester profile", p)
	}
	if len(p.PIDs) != 32 {
		t.Fatalf("Default() profile has %d PIDs, want 32", len(p.PIDs))
	}
	if p.Trim != "" {
		t.Errorf("Default().Trim = %q, want empty (single-variant Forester profile)", p.Trim)
	}
}

// withTestRegistry temporarily swaps the package-level registry for the
// drill-down (Years/Makes/Models/Trims/Find) tests below, so they can
// exercise multi-entry dedup/sort/filter behavior without depending on
// how many real, hardware-verified profiles happen to exist today (see
// DESIGN.md section 5.2 on why that's deliberately just one so far).
func withTestRegistry(t *testing.T, profiles []Profile) {
	t.Helper()
	orig := registry
	registry = profiles
	t.Cleanup(func() { registry = orig })
}

func testRegistry() []Profile {
	return []Profile{
		{Make: "Subaru", Model: "Forester", Year: 2023, Trim: ""},
		{Make: "Subaru", Model: "Forester", Year: 2023, Trim: "Wilderness"},
		{Make: "Subaru", Model: "Outback", Year: 2023, Trim: ""},
		{Make: "Toyota", Model: "Camry", Year: 2022, Trim: ""},
	}
}

func TestYears(t *testing.T) {
	withTestRegistry(t, testRegistry())
	got := Years()
	want := []int{2023, 2022}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Years() = %v, want %v (descending, deduped across 2023's three entries)", got, want)
	}
}

func TestMakes(t *testing.T) {
	withTestRegistry(t, testRegistry())
	if got, want := Makes(2023), []string{"Subaru"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Makes(2023) = %v, want %v (deduped across Forester's two trims)", got, want)
	}
	if got := Makes(1999); got != nil {
		t.Errorf("Makes(1999) = %v, want nil for a year with no profiles", got)
	}
}

func TestModels(t *testing.T) {
	withTestRegistry(t, testRegistry())
	got := Models(2023, "Subaru")
	want := []string{"Forester", "Outback"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Models(2023, Subaru) = %v, want %v (alphabetical, deduped across Forester's two trims)", got, want)
	}
	if got := Models(2023, "Honda"); got != nil {
		t.Errorf("Models(2023, Honda) = %v, want nil for a make with no profiles that year", got)
	}
}

func TestTrims(t *testing.T) {
	withTestRegistry(t, testRegistry())
	if got, want := Trims(2023, "Subaru", "Forester"), []string{"Wilderness"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Trims(2023, Subaru, Forester) = %v, want %v", got, want)
	}
	if got := Trims(2023, "Subaru", "Outback"); got != nil {
		t.Errorf("Trims(single-variant Outback) = %v, want nil (empty), not [\"\"], so the picker can skip its Trim step", got)
	}
}

func TestFind(t *testing.T) {
	withTestRegistry(t, testRegistry())

	if p, ok := Find(2023, "Subaru", "Forester", ""); !ok || p.Trim != "" {
		t.Errorf("Find(2023, Subaru, Forester, \"\") = %+v, %v, want the untrimmed profile", p, ok)
	}
	if p, ok := Find(2023, "Subaru", "Forester", "Wilderness"); !ok || p.Trim != "Wilderness" {
		t.Errorf("Find(2023, Subaru, Forester, Wilderness) = %+v, %v, want the Wilderness profile", p, ok)
	}
	if _, ok := Find(2023, "Subaru", "Forester", "Touring"); ok {
		t.Error("Find with an unknown trim should return ok=false")
	}
	if _, ok := Find(1999, "Subaru", "Forester", ""); ok {
		t.Error("Find with an unknown year should return ok=false")
	}
}

// isEmptyProfile reports whether p is the zero Profile, by identifying
// field rather than a struct-equality comparison — Profile.PIDs is a
// slice, which Go doesn't allow comparing with == / !=.
func isEmptyProfile(p Profile) bool {
	return p.Make == "" && p.Model == "" && p.Year == 0 && p.Trim == ""
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
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile, got %+v", profile)
	}
}

func TestSaveSelectedThenLoadSelectedRoundTrips(t *testing.T) {
	withTestRegistry(t, testRegistry())

	tests := []struct {
		name string
		p    Profile
	}{
		{name: "untrimmed profile", p: Profile{Make: "Subaru", Model: "Forester", Year: 2023, Trim: ""}},
		{name: "trimmed profile", p: Profile{Make: "Subaru", Model: "Forester", Year: 2023, Trim: "Wilderness"}},
		{name: "different make/model/year", p: Profile{Make: "Toyota", Model: "Camry", Year: 2022, Trim: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := SaveSelected(dir, tt.p); err != nil {
				t.Fatalf("SaveSelected: %v", err)
			}
			profile, ok, err := LoadSelected(dir)
			if !ok {
				t.Fatalf("LoadSelected after SaveSelected returned ok=false, want true")
			}
			if err != nil {
				t.Fatalf("LoadSelected after SaveSelected returned err=%v, want nil", err)
			}
			if profile.Make != tt.p.Make || profile.Model != tt.p.Model || profile.Year != tt.p.Year || profile.Trim != tt.p.Trim {
				t.Errorf("LoadSelected returned %+v, want %+v", profile, tt.p)
			}
		})
	}
}

// TestLoadSelectedStaleSelectionFallsBackWithoutError guards the case
// DESIGN.md section 5.3 calls out explicitly: a previously-saved
// selection that no longer matches any registry entry (e.g. a future
// app update removes that Profile) must be treated the same as "never
// selected," not surfaced as an error for something the user didn't do
// wrong.
func TestLoadSelectedStaleSelectionFallsBackWithoutError(t *testing.T) {
	withTestRegistry(t, testRegistry())
	dir := t.TempDir()
	if err := SaveSelected(dir, Profile{Make: "Ford", Model: "Fusion", Year: 2015, Trim: ""}); err != nil {
		t.Fatalf("SaveSelected: %v", err)
	}

	withTestRegistry(t, []Profile{{Make: "Subaru", Model: "Forester", Year: 2023}})

	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected for a selection no longer in registry should return ok=false, got ok=%v", ok)
	}
	if err != nil {
		t.Errorf("LoadSelected for a selection no longer in registry should return err=nil, got %v", err)
	}
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile when stale, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileTooFewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedVehicleFileName)
	if err := os.WriteFile(path, []byte("2023\nSubaru\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on malformed file should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on malformed file should return non-nil error, got nil")
	}
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedVehicleFileName)
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
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileEmptyModelLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedVehicleFileName)
	if err := os.WriteFile(path, []byte("2023\nSubaru\n\n\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected on file with empty model line should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on file with empty model line should return non-nil error, got nil")
	}
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedMalformedFileInvalidYear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedVehicleFileName)
	if err := os.WriteFile(path, []byte("not-a-year\nSubaru\nForester\n\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(dir)
	if ok {
		t.Errorf("LoadSelected with a non-numeric year should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected with a non-numeric year should return non-nil error, got nil")
	}
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestLoadSelectedUnreadableDirectory(t *testing.T) {
	// Create a file where we expect a directory, so ReadFile fails for a
	// reason other than not-exist.
	parent := t.TempDir()
	blockedPath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(blockedPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile, ok, err := LoadSelected(blockedPath)
	if ok {
		t.Errorf("LoadSelected on blocked path should return ok=false, got ok=%v", ok)
	}
	if err == nil {
		t.Errorf("LoadSelected on blocked path should return non-nil error, got nil")
	}
	if !isEmptyProfile(profile) {
		t.Errorf("LoadSelected should return empty Profile on error, got %+v", profile)
	}
}

func TestSelectedOrDefaultReturnsDefaultWhenNothingSaved(t *testing.T) {
	dir := t.TempDir()
	profile := SelectedOrDefault(dir)
	if profile.Make != Default().Make || profile.Model != Default().Model || profile.Year != Default().Year {
		t.Errorf("SelectedOrDefault on fresh dir returned %+v, want Default() %+v", profile, Default())
	}
}

func TestSelectedOrDefaultReturnsSavedProfile(t *testing.T) {
	withTestRegistry(t, testRegistry())
	dir := t.TempDir()
	want := Profile{Make: "Toyota", Model: "Camry", Year: 2022, Trim: ""}
	if err := SaveSelected(dir, want); err != nil {
		t.Fatalf("SaveSelected: %v", err)
	}
	profile := SelectedOrDefault(dir)
	if profile.Make != want.Make || profile.Model != want.Model || profile.Year != want.Year {
		t.Errorf("SelectedOrDefault returned %+v, want %+v", profile, want)
	}
}

func TestSelectedOrDefaultFallsBackOnMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, selectedVehicleFileName)
	if err := os.WriteFile(path, []byte("malformed"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	profile := SelectedOrDefault(dir)
	if profile.Make != Default().Make || profile.Model != Default().Model || profile.Year != Default().Year {
		t.Errorf("SelectedOrDefault on malformed file returned %+v, want Default() %+v", profile, Default())
	}
}

func TestSaveSelectedCreatesDirectoryIfNotExist(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "nested", "vehicle", "config")
	p := Profile{Make: "Subaru", Model: "Forester", Year: 2023, Trim: ""}
	if err := SaveSelected(dir, p); err != nil {
		t.Fatalf("SaveSelected with nested nonexistent path: %v", err)
	}
	path := filepath.Join(dir, selectedVehicleFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if want := "2023\nSubaru\nForester\n\n"; string(data) != want {
		t.Errorf("SaveSelected file content = %q, want %q", string(data), want)
	}
}

func TestSaveSelectedWriteFailureWhenDirectoryIsAFile(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := filepath.Join(blocker, "vehicle", "config")
	p := Profile{Make: "Subaru", Model: "Forester", Year: 2023}
	if err := SaveSelected(dir, p); err == nil {
		t.Errorf("SaveSelected should fail when MkdirAll can't create directories, got nil")
	}
}

func TestSaveSelectedWriteFileFailure(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "readonly")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755) // restore for cleanup
	p := Profile{Make: "Subaru", Model: "Forester", Year: 2023}
	if err := SaveSelected(dir, p); err == nil {
		t.Errorf("SaveSelected should fail when WriteFile can't write, got nil")
	}
}

func TestProfilePIDLookup(t *testing.T) {
	p := Default()

	if _, ok := p.PID(0x0C); !ok {
		t.Error("PID(0x0C) (RPM) not found in default profile")
	}
	if _, ok := p.PID(0xFF); ok {
		t.Error("PID(0xFF) should not be found in default profile")
	}
}

// TestAllPIDsErrorOnEmptyData covers the "too-short data" branch of
// every PID's Decode, in one pass, rather than one near-duplicate test
// function per PID — zero bytes is always insufficient regardless of
// whether a given formula needs 1 or 2 data bytes.
func TestAllPIDsErrorOnEmptyData(t *testing.T) {
	for _, pid := range Default().PIDs {
		t.Run(pid.Name, func(t *testing.T) {
			if _, err := pid.Decode(nil); err == nil {
				t.Errorf("%s (0x%02X): Decode(nil) should error, got nil", pid.Name, pid.Code)
			}
		})
	}
}

func TestDecodeRPM(t *testing.T) {
	pid, ok := Default().PID(0x0C)
	if !ok {
		t.Fatal("RPM PID not found")
	}
	got, err := pid.Decode([]byte{0x1A, 0xF8})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if want := 1726.0; got != want {
		t.Errorf("decodeRPM(0x1AF8) = %v, want %v", got, want)
	}
}

func TestDecodeByteMinus40(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want float64
	}{
		{"coolant temp", []byte{0x7B}, 83.0},
		{"intake air temp", []byte{0x5A}, 50.0},
		{"ambient air temp", []byte{0x28}, 0.0},
		{"engine oil temp", []byte{0x00}, -40.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeByteMinus40(tt.data)
			if err != nil {
				t.Fatalf("decodeByteMinus40(%v): %v", tt.data, err)
			}
			if got != tt.want {
				t.Errorf("decodeByteMinus40(%v) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestDecodePercentOfByte(t *testing.T) {
	tests := []struct {
		data []byte
		want float64
	}{
		{[]byte{0xFF}, 100.0},
		{[]byte{0x00}, 0.0},
	}
	for _, tt := range tests {
		got, err := decodePercentOfByte(tt.data)
		if err != nil {
			t.Fatalf("decodePercentOfByte(%v): %v", tt.data, err)
		}
		if got != tt.want {
			t.Errorf("decodePercentOfByte(%v) = %v, want %v", tt.data, got, tt.want)
		}
	}
}

func TestDecodeFuelTrim(t *testing.T) {
	tests := []struct {
		data []byte
		want float64
	}{
		{[]byte{0x80}, 0.0},      // midpoint (128*100/128-100)
		{[]byte{0xFF}, 99.21875}, // max (255*100/128-100)
		{[]byte{0x00}, -100.0},   // min
	}
	for _, tt := range tests {
		got, err := decodeFuelTrim(tt.data)
		if err != nil {
			t.Fatalf("decodeFuelTrim(%v): %v", tt.data, err)
		}
		if got != tt.want {
			t.Errorf("decodeFuelTrim(%v) = %v, want %v", tt.data, got, tt.want)
		}
	}
}

func TestDecodeByteAsIs(t *testing.T) {
	got, err := decodeByteAsIs([]byte{0x64})
	if err != nil {
		t.Fatalf("decodeByteAsIs: %v", err)
	}
	if want := 100.0; got != want {
		t.Errorf("decodeByteAsIs([0x64]) = %v, want %v", got, want)
	}
}

func TestDecodeTwoByteInt(t *testing.T) {
	got, err := decodeTwoByteInt([]byte{0x00, 0x3C})
	if err != nil {
		t.Fatalf("decodeTwoByteInt: %v", err)
	}
	if want := 60.0; got != want {
		t.Errorf("decodeTwoByteInt([0x00,0x3C]) = %v, want %v", got, want)
	}
}

func TestDecodeO2SensorVoltage(t *testing.T) {
	// Real responses carry 2 data bytes (voltage, trim); only the first
	// is decoded, and a 1-byte input must still work (defensive, in case
	// an adapter ever trims the trailing byte).
	got, err := decodeO2SensorVoltage([]byte{0x64, 0xFF})
	if err != nil {
		t.Fatalf("decodeO2SensorVoltage: %v", err)
	}
	if want := 0.5; got != want {
		t.Errorf("decodeO2SensorVoltage([0x64,0xFF]) = %v, want %v", got, want)
	}
}

func TestDecodeFuelPressure(t *testing.T) {
	got, err := decodeFuelPressure([]byte{0x0A})
	if err != nil {
		t.Fatalf("decodeFuelPressure: %v", err)
	}
	if want := 30.0; got != want {
		t.Errorf("decodeFuelPressure([0x0A]) = %v, want %v", got, want)
	}
}

func TestDecodeTimingAdvance(t *testing.T) {
	got, err := decodeTimingAdvance([]byte{0x80})
	if err != nil {
		t.Fatalf("decodeTimingAdvance: %v", err)
	}
	if want := 0.0; got != want {
		t.Errorf("decodeTimingAdvance([0x80]) = %v, want %v", got, want)
	}
}

func TestDecodeMassAirFlow(t *testing.T) {
	got, err := decodeMassAirFlow([]byte{0x01, 0x2C})
	if err != nil {
		t.Fatalf("decodeMassAirFlow: %v", err)
	}
	if want := 3.0; got != want {
		t.Errorf("decodeMassAirFlow([0x01,0x2C]) = %v, want %v", got, want)
	}
}

func TestDecodeControlModuleVoltage(t *testing.T) {
	got, err := decodeControlModuleVoltage([]byte{0x2E, 0xE0})
	if err != nil {
		t.Fatalf("decodeControlModuleVoltage: %v", err)
	}
	if want := 12.0; got != want {
		t.Errorf("decodeControlModuleVoltage([0x2E,0xE0]) = %v, want %v", got, want)
	}
}

func TestDecodeAbsoluteLoad(t *testing.T) {
	got, err := decodeAbsoluteLoad([]byte{0x00, 0xFF})
	if err != nil {
		t.Fatalf("decodeAbsoluteLoad: %v", err)
	}
	if want := 100.0; got != want {
		t.Errorf("decodeAbsoluteLoad([0x00,0xFF]) = %v, want %v", got, want)
	}
}

func TestDecodeFuelRailPressure(t *testing.T) {
	got, err := decodeFuelRailPressure([]byte{0x00, 0x0A})
	if err != nil {
		t.Fatalf("decodeFuelRailPressure: %v", err)
	}
	if want := 100.0; got != want {
		t.Errorf("decodeFuelRailPressure([0x00,0x0A]) = %v, want %v", got, want)
	}
}

func TestDecodeEngineFuelRate(t *testing.T) {
	got, err := decodeEngineFuelRate([]byte{0x00, 0x28})
	if err != nil {
		t.Fatalf("decodeEngineFuelRate: %v", err)
	}
	if want := 2.0; got != want {
		t.Errorf("decodeEngineFuelRate([0x00,0x28]) = %v, want %v", got, want)
	}
}
