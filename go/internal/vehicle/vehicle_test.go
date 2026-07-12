package vehicle

import "testing"

func TestDefaultProfile(t *testing.T) {
	p := Default()
	if p.Make != "Subaru" || p.Model != "Forester" || p.Year != 2023 {
		t.Fatalf("Default() = %+v, want the hardcoded 2023 Subaru Forester profile", p)
	}
	if len(p.PIDs) != 32 {
		t.Fatalf("Default() profile has %d PIDs, want 32", len(p.PIDs))
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
