package mobile

import (
	"os"
	"path/filepath"
	"testing"
)

type fakeListener struct {
	calls []struct {
		pid        int
		name, unit string
		value      float64
	}
}

func (f *fakeListener) OnReading(pid int, name, unit string, value float64, unixMillis int64) {
	f.calls = append(f.calls, struct {
		pid        int
		name, unit string
		value      float64
	}{pid, name, unit, value})
}

func TestSessionFeedNotifiesListenerAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.jsonl")
	listener := &fakeListener{}

	session, err := NewSession(path, listener)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	session.Feed([]byte("41 0C 1A F8\r"))

	if len(listener.calls) != 1 {
		t.Fatalf("got %d listener calls, want 1", len(listener.calls))
	}
	got := listener.calls[0]
	if got.pid != 0x0C || got.name != "Engine RPM" || got.unit != "rpm" || got.value != 1726.0 {
		t.Errorf("listener call = %+v, want PID 0x0C Engine RPM rpm 1726", got)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("reading log is empty, want the fed reading to have been persisted")
	}
}

func TestSessionCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.jsonl")
	session, err := NewSession(path, nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	if got := session.CommandCount(); got == 0 {
		t.Fatal("CommandCount() = 0, want at least one PID command")
	}
	if got := session.CommandAt(0); got != "010C" {
		t.Errorf("CommandAt(0) = %q, want %q", got, "010C")
	}
	if got := session.CommandAt(-1); got != "" {
		t.Errorf("CommandAt(-1) = %q, want empty string, not a panic", got)
	}
	if got := session.CommandAt(session.CommandCount()); got != "" {
		t.Errorf("CommandAt(CommandCount()) = %q, want empty string, not a panic", got)
	}
}

func TestDeviceMAC(t *testing.T) {
	if got := DeviceMAC(); got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("DeviceMAC() = %q, want the hardcoded garage adapter MAC", got)
	}
}
