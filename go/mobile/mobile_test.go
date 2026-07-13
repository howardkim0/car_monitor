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

type fakeAnomalyListener struct {
	calls []struct {
		metric, level, message string
	}
}

func (f *fakeAnomalyListener) OnAnomaly(metric, level, message string, unixMillis int64) {
	f.calls = append(f.calls, struct {
		metric, level, message string
	}{metric, level, message})
}

// writeReadingsCSV writes today's reading log directly, bypassing
// Session.Feed/Append, so CheckAnomalies tests can set up whatever
// reading history they need in one line instead of a full ELM327 wire
// exchange per row.
func writeReadingsCSV(t *testing.T, readingsDir string, rows [][]string) {
	t.Helper()
	path := filepath.Join(readingsDir, "readings-"+time.Now().UTC().Format("2006-01-02")+".csv")
	var sb strings.Builder
	sb.WriteString("pid,name,value,unit,timestamp\n")
	for _, row := range rows {
		sb.WriteString(strings.Join(row, ","))
		sb.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestSessionFeedNotifiesListenerAndPersists(t *testing.T) {
	dir := t.TempDir()
	listener := &fakeListener{}

	session, err := NewSession(dir, listener, nil)
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

	if _, err := NewSession(blocker, nil, nil); err == nil {
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
	session := newSessionWithStore(store, t.TempDir(), nil, nil)

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
	session, err := NewSession(dir, nil, nil)
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

func TestCheckAnomaliesIsNoOpWithoutListener(t *testing.T) {
	// Must not panic and must not attempt to read anything (the readings
	// dir here doesn't even exist) — nothing would consume the result.
	session := newSessionWithStore(&fakeStore{}, filepath.Join(t.TempDir(), "nonexistent"), nil, nil)
	session.CheckAnomalies()
}

func TestCheckAnomaliesFiresOnceThenStaysSilentWhileUnchanged(t *testing.T) {
	dir := t.TempDir()
	listener := &fakeAnomalyListener{}
	session := newSessionWithStore(&fakeStore{}, dir, nil, listener)

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "112.5", "C", time.Now().UTC().Format(time.RFC3339Nano)},
	})

	session.CheckAnomalies()
	if len(listener.calls) != 1 {
		t.Fatalf("got %d calls after first check, want 1", len(listener.calls))
	}
	if listener.calls[0].metric != "Coolant Temperature" || listener.calls[0].level != "CRITICAL" {
		t.Errorf("call = %+v, want Coolant Temperature CRITICAL", listener.calls[0])
	}

	session.CheckAnomalies() // same data, same level — should stay silent
	if len(listener.calls) != 1 {
		t.Errorf("got %d calls after a second, unchanged check, want still 1", len(listener.calls))
	}
}

func TestCheckAnomaliesFiresAgainOnEscalation(t *testing.T) {
	dir := t.TempDir()
	listener := &fakeAnomalyListener{}
	session := newSessionWithStore(&fakeStore{}, dir, nil, listener)

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "104.0", "C", time.Now().UTC().Format(time.RFC3339Nano)}, // Warning
	})
	session.CheckAnomalies()

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "112.5", "C", time.Now().UTC().Format(time.RFC3339Nano)}, // Critical
	})
	session.CheckAnomalies()

	if len(listener.calls) != 2 {
		t.Fatalf("got %d calls, want 2 (Warning, then Critical on escalation)", len(listener.calls))
	}
	if listener.calls[0].level != "WARNING" || listener.calls[1].level != "CRITICAL" {
		t.Errorf("levels = [%s %s], want [WARNING CRITICAL]", listener.calls[0].level, listener.calls[1].level)
	}
}

func TestCheckAnomaliesRefiresAfterRecoveryAndRecurrence(t *testing.T) {
	dir := t.TempDir()
	listener := &fakeAnomalyListener{}
	session := newSessionWithStore(&fakeStore{}, dir, nil, listener)

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "112.5", "C", time.Now().UTC().Format(time.RFC3339Nano)}, // Critical
	})
	session.CheckAnomalies()

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "90.0", "C", time.Now().UTC().Format(time.RFC3339Nano)}, // Normal
	})
	session.CheckAnomalies() // recovers silently — no new call

	writeReadingsCSV(t, dir, [][]string{
		{"5", "Coolant Temperature", "112.5", "C", time.Now().UTC().Format(time.RFC3339Nano)}, // Critical again
	})
	session.CheckAnomalies()

	if len(listener.calls) != 2 {
		t.Fatalf("got %d calls, want 2 (both Critical occurrences; the recovery in between is silent)", len(listener.calls))
	}
}

func TestCheckAnomaliesLogsLoadReadingsErrors(t *testing.T) {
	resetAppLogger(t)
	logDir := t.TempDir()
	if err := InitAppLog(logDir); err != nil {
		t.Fatalf("InitAppLog: %v", err)
	}
	defer CloseAppLog()

	dir := t.TempDir()
	listener := &fakeAnomalyListener{}
	session := newSessionWithStore(&fakeStore{}, dir, nil, listener)

	// A directory sitting at the exact path LoadReadings wants to open as
	// a file makes it fail with something other than "doesn't exist".
	blockedPath := filepath.Join(dir, "readings-"+time.Now().UTC().Format("2006-01-02")+".csv")
	if err := os.Mkdir(blockedPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	session.CheckAnomalies()

	if len(listener.calls) != 0 {
		t.Errorf("got %d anomaly calls despite a load error, want 0", len(listener.calls))
	}
	data, err := os.ReadFile(filepath.Join(logDir, "app.log"))
	if err != nil {
		t.Fatalf("reading app.log: %v", err)
	}
	if !strings.Contains(string(data), "check anomalies") {
		t.Errorf("app.log = %q, want it to record the LoadReadings error", string(data))
	}
}

func TestDeviceMAC(t *testing.T) {
	if got := DeviceMAC(); got != "00:1D:A5:68:98:8A" {
		t.Errorf("DeviceMAC() = %q, want the hardcoded garage adapter MAC", got)
	}
}
