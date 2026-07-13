// Package mobile is the gomobile bind entry point Kotlin calls into (see
// DESIGN.md sections 4 and 11). Its exported surface is intentionally
// narrow and gomobile-friendly: no exported []string or slice-of-struct
// returns, one callback interface, plain constructor + methods.
package mobile

import (
	"fmt"
	"path/filepath"

	"github.com/howardkim0/car_monitor/go/internal/device"
	"github.com/howardkim0/car_monitor/go/internal/monitor"
	"github.com/howardkim0/car_monitor/go/internal/obd2"
	"github.com/howardkim0/car_monitor/go/internal/storage"
	"github.com/howardkim0/car_monitor/go/internal/trend"
	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

// ReadingListener is implemented on the Kotlin side; gomobile bind turns
// this into a Java interface Session calls back into for every decoded
// reading.
type ReadingListener interface {
	OnReading(pid int, name, unit string, value float64, unixMillis int64)
}

// AnomalyListener is implemented on the Kotlin side; gomobile bind turns
// this into a Java interface CheckAnomalies calls back into whenever a
// trend check's result has changed since the last check — see
// Session.CheckAnomalies. level is one of trend.IssueLevel's string
// values ("WARNING", "CRITICAL"); LevelNormal is never reported here,
// only used internally to reset state once a metric recovers.
type AnomalyListener interface {
	OnAnomaly(metric, level, message string, unixMillis int64)
}

// Session is the single entry point the Android shell talks to: feed it
// raw bytes off the Bluetooth socket, it decodes them against the
// hardcoded device/vehicle profiles, persists them, and calls back into
// listener.
type Session struct {
	inner           *obd2.Session
	store           storage.Store
	readingsDir     string
	anomalyListener AnomalyListener
	lastLevel       map[string]trend.IssueLevel
}

// NewSession opens (or creates/resumes) the day-rotated CSV reading log
// under storageDir/readings (see DESIGN.md section 6) and wires up
// decoding against the default vehicle profile. storageDir is the app's
// private storage root (Android's filesDir) — this package organizes its
// own subpaths within it, so callers no longer build a specific file
// path themselves. listener may be nil if the caller only wants readings
// persisted, not delivered live; anomalyListener may be nil if the
// caller doesn't want CheckAnomalies results delivered at all (in which
// case CheckAnomalies is a cheap no-op).
func NewSession(storageDir string, listener ReadingListener, anomalyListener AnomalyListener) (*Session, error) {
	readingsDir := filepath.Join(storageDir, "readings")
	store, err := storage.OpenFileStore(readingsDir)
	if err != nil {
		return nil, err
	}
	return newSessionWithStore(store, readingsDir, listener, anomalyListener), nil
}

// newSessionWithStore holds the actual reading-pipeline wiring, factored
// out of NewSession so a test can inject a fake Store — e.g. to exercise
// the Append-error path below without needing a real filesystem failure.
func newSessionWithStore(store storage.Store, readingsDir string, listener ReadingListener, anomalyListener AnomalyListener) *Session {
	s := &Session{
		store:           store,
		readingsDir:     readingsDir,
		anomalyListener: anomalyListener,
		lastLevel:       make(map[string]trend.IssueLevel),
	}
	// firstReading tracks whether we've logged the "pipeline is alive"
	// diagnostic for this session — reset on every Bluetooth reconnect
	// (a new Session is created each time) so the log appears once per
	// connection, not once per app lifetime. Answers DESIGN.md §12's
	// question about real-hardware round-trip timing.
	var firstReading bool
	s.inner = obd2.NewSessionWithLogger(vehicle.Default(), func(r obd2.Reading) {
		if !firstReading {
			firstReading = true
			LogDebug(fmt.Sprintf("obd2: first reading received — pipeline alive: pid=0x%02X name=%q value=%.2f %s",
				r.PID, r.Name, r.Value, r.Unit))
		}
		if err := s.store.Append(r); err != nil {
			LogError(fmt.Sprintf("append reading (pid 0x%02X): %v", r.PID, err))
		}
		if listener != nil {
			listener.OnReading(int(r.PID), r.Name, r.Unit, r.Value, r.Timestamp.UnixMilli())
		}
	}, func(format string, args ...any) { LogDebug(fmt.Sprintf(format, args...)) })
	return s
}

// CheckAnomalies re-reads today's persisted log (see internal/storage's
// LoadReadings) and runs it through internal/monitor's trend checks,
// reporting to anomalyListener only the metrics whose level has changed
// since the last call — e.g. Normal→Warning fires once, but repeated
// calls stay silent while a condition persists, and only fire again if
// it escalates (Warning→Critical), de-escalates but is still abnormal
// (Critical→Warning), or recurs after recovering to Normal in between.
// A no-op if anomalyListener is nil — nothing would consume the result.
//
// The dedup state above is scoped to this Session, not persisted across
// Bluetooth reconnects (a new Session is created on each one — see
// ObdForegroundService.openConnection in the Kotlin shell): an
// occasional duplicate notification around a reconnect is an acceptable
// tradeoff for not needing this state to survive Session recreation.
//
// How often to call this is the caller's decision, same as *how often to
// poll* is for PID requests — see DESIGN.md section 4 step 5's
// precedent; ObdForegroundService currently does both on coroutine-loop
// timers of their own.
func (s *Session) CheckAnomalies() {
	if s.anomalyListener == nil {
		return
	}

	readings, err := storage.LoadReadings(s.readingsDir)
	if err != nil {
		LogError(fmt.Sprintf("check anomalies: load readings: %v", err))
		return
	}

	for _, a := range monitor.Evaluate(readings) {
		if s.lastLevel[a.Metric] == a.Level {
			continue
		}
		s.lastLevel[a.Metric] = a.Level
		if a.Level != trend.LevelNormal {
			s.anomalyListener.OnAnomaly(a.Metric, string(a.Level), a.Message, a.Timestamp.UnixMilli())
		}
	}
}

// Feed pushes newly-read bytes from the Bluetooth socket into the session.
func (s *Session) Feed(data []byte) {
	s.inner.Feed(data)
}

// CommandCount returns how many commands the session currently wants
// sent this cycle — either pending PID-discovery queries or, once
// discovery has resolved, the real per-PID request list (see
// internal/obd2's two-phase Commands()). gomobile bind can't return a
// []string cleanly, so the Android shell polls CommandCount/CommandAt on
// a timer to build its request loop instead of consuming Commands()
// directly.
//
// CommandCount and CommandAt are two separate JNI calls, not one atomic
// snapshot — since Commands() can now change between them (discovery
// resolving mid-poll), Kotlin could in principle see a count from before
// a transition and an index from after. Not fixed: the real fix is a
// single "return the whole list" call, not worth adding across the JNI
// boundary for a mismatch that self-heals on the very next poll cycle.
// See DESIGN.md section 12.
func (s *Session) CommandCount() int {
	return len(s.inner.Commands())
}

// CommandAt returns the i'th command (e.g. "0104" or, during discovery,
// "0100"). The caller appends a carriage return and writes it to the
// Bluetooth socket. Returns "" if i is out of range rather than
// panicking across the gomobile/JNI boundary.
func (s *Session) CommandAt(i int) string {
	cmds := s.inner.Commands()
	if i < 0 || i >= len(cmds) {
		return ""
	}
	return cmds[i]
}

// Close flushes and closes the local reading log.
func (s *Session) Close() error {
	return s.store.Close()
}

// DeviceMAC returns the hardcoded Bluetooth MAC address the Android shell
// should connect to.
func DeviceMAC() string {
	return device.Default().MACAddress
}
