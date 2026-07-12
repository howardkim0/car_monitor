package mobile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
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

// fakeStore lets tests exercise the Append-error path in
// newSessionWithStore without needing a real filesystem failure.
type fakeStore struct {
	appendErr error
	appended  []obd2.Reading
}

func (f *fakeStore) Append(r obd2.Reading) error {
	f.appended = append(f.appended, r)
	return f.appendErr
}

func (f *fakeStore) Close() error { return nil }

func TestSessionFeedNotifiesListenerAndPersists(t *testing.T) {
	dir := t.TempDir()
	listener := &fakeListener{}

	session, err := NewSession(dir, listener)
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

	readingsPath := filepath.Join(dir, "readings", "readings-"+time.Now().UTC().Format("2006-01-02")+".csv")
	data, err := os.ReadFile(readingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("reading log is empty, want the fed reading to have been persisted")
	}
}

func TestNewSessionPropagatesStorageError(t *testing.T) {
	// A regular file where a path component needs to be a directory makes
	// storage.OpenFileStore's os.MkdirAll fail; NewSession must surface
	// that rather than panicking or returning a half-initialized Session.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := NewSession(blocker, nil); err == nil {
		t.Error("NewSession with an unwritable storage path should error, got nil")
	}
}

func TestNewSessionWithStoreLogsAppendErrorsInsteadOfSwallowingThem(t *testing.T) {
	resetAppLogger(t)
	logDir := t.TempDir()
	if err := InitAppLog(logDir); err != nil {
		t.Fatalf("InitAppLog: %v", err)
	}
	defer CloseAppLog()

	store := &fakeStore{appendErr: errors.New("disk full")}
	session := newSessionWithStore(store, nil)

	session.Feed([]byte("41 0C 1A F8\r"))

	if len(store.appended) != 1 {
		t.Fatalf("store.Append called %d times, want 1", len(store.appended))
	}

	data, err := os.ReadFile(filepath.Join(logDir, "app.log"))
	if err != nil {
		t.Fatalf("reading app.log: %v", err)
	}
	if got := string(data); !strings.Contains(got, "append reading") || !strings.Contains(got, "disk full") {
		t.Errorf("app.log = %q, want it to record the swallowed Append error", got)
	}
}

func TestSessionCommands(t *testing.T) {
	dir := t.TempDir()
	session, err := NewSession(dir, nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	// Discovery queries are returned first — see internal/obd2's
	// two-phase Commands(); this profile's highest PID (0x5E) needs
	// ranges 0x00, 0x20, 0x40.
	if got := session.CommandCount(); got != 3 {
		t.Fatalf("CommandCount() before discovery = %d, want 3 (discovery queries)", got)
	}
	if got := session.CommandAt(0); got != "0100" {
		t.Errorf("CommandAt(0) = %q, want %q", got, "0100")
	}
	if got := session.CommandAt(-1); got != "" {
		t.Errorf("CommandAt(-1) = %q, want empty string, not a panic", got)
	}
	if got := session.CommandAt(session.CommandCount()); got != "" {
		t.Errorf("CommandAt(CommandCount()) = %q, want empty string, not a panic", got)
	}
}

func TestDeviceMAC(t *testing.T) {
	if got := DeviceMAC(); got != "00:1D:A5:68:98:8A" {
		t.Errorf("DeviceMAC() = %q, want the hardcoded garage adapter MAC", got)
	}
}
