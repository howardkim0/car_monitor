package vehicle

import "testing"

func TestDefaultProfile(t *testing.T) {
	p := Default()
	if p.Make != "Subaru" || p.Model != "Forester" || p.Year != 2023 {
		t.Fatalf("Default() = %+v, want the hardcoded 2023 Subaru Forester profile", p)
	}
	if len(p.PIDs) == 0 {
		t.Fatal("Default() profile has no PIDs")
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

	if _, err := pid.Decode([]byte{0x1A}); err == nil {
		t.Error("Decode with 1 byte should error, RPM needs 2")
	}
}

func TestDecodeSpeed(t *testing.T) {
	pid, ok := Default().PID(0x0D)
	if !ok {
		t.Fatal("speed PID not found")
	}

	got, err := pid.Decode([]byte{0x50})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if want := 80.0; got != want {
		t.Errorf("decodeSpeed(0x50) = %v, want %v", got, want)
	}

	if _, err := pid.Decode([]byte{}); err == nil {
		t.Error("Decode with 0 bytes should error, speed needs 1")
	}
}

func TestDecodeCoolantTemp(t *testing.T) {
	pid, ok := Default().PID(0x05)
	if !ok {
		t.Fatal("coolant temp PID not found")
	}

	got, err := pid.Decode([]byte{0x7B})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if want := 83.0; got != want {
		t.Errorf("decodeCoolantTemp(0x7B) = %v, want %v", got, want)
	}

	if _, err := pid.Decode([]byte{}); err == nil {
		t.Error("Decode with 0 bytes should error, coolant temp needs 1")
	}
}

func TestDecodeThrottlePosition(t *testing.T) {
	pid, ok := Default().PID(0x11)
	if !ok {
		t.Fatal("throttle position PID not found")
	}

	tests := []struct {
		data []byte
		want float64
	}{
		{data: []byte{0xFF}, want: 100.0},
		{data: []byte{0x00}, want: 0.0},
	}
	for _, tt := range tests {
		got, err := pid.Decode(tt.data)
		if err != nil {
			t.Fatalf("Decode(%v) returned error: %v", tt.data, err)
		}
		if got != tt.want {
			t.Errorf("decodeThrottlePosition(%v) = %v, want %v", tt.data, got, tt.want)
		}
	}

	if _, err := pid.Decode([]byte{}); err == nil {
		t.Error("Decode with 0 bytes should error, throttle position needs 1")
	}
}
