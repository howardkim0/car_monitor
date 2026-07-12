// Package mobile is the gomobile bind entry point Kotlin calls into (see
// DESIGN.md sections 4 and 11). Its exported surface is intentionally
// narrow and gomobile-friendly: no exported []string or slice-of-struct
// returns, one callback interface, plain constructor + methods.
package mobile

import (
	"github.com/howardkim0/car_monitor/go/internal/device"
	"github.com/howardkim0/car_monitor/go/internal/obd2"
	"github.com/howardkim0/car_monitor/go/internal/storage"
	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

// ReadingListener is implemented on the Kotlin side; gomobile bind turns
// this into a Java interface Session calls back into for every decoded
// reading.
type ReadingListener interface {
	OnReading(pid int, name, unit string, value float64, unixMillis int64)
}

// Session is the single entry point the Android shell talks to: feed it
// raw bytes off the Bluetooth socket, it decodes them against the
// hardcoded device/vehicle profiles, persists them, and calls back into
// listener.
type Session struct {
	inner *obd2.Session
	store *storage.FileStore
}

// NewSession opens (or creates) the JSONL reading log at storagePath and
// wires up decoding against the default vehicle profile. listener may be
// nil if the caller only wants readings persisted, not delivered live.
func NewSession(storagePath string, listener ReadingListener) (*Session, error) {
	store, err := storage.OpenFileStore(storagePath)
	if err != nil {
		return nil, err
	}

	s := &Session{store: store}
	s.inner = obd2.NewSession(vehicle.Default(), func(r obd2.Reading) {
		_ = s.store.Append(r)
		if listener != nil {
			listener.OnReading(int(r.PID), r.Name, r.Unit, r.Value, r.Timestamp.UnixMilli())
		}
	})
	return s, nil
}

// Feed pushes newly-read bytes from the Bluetooth socket into the session.
func (s *Session) Feed(data []byte) {
	s.inner.Feed(data)
}

// CommandCount returns how many PID request commands the active vehicle
// profile has. gomobile bind can't return a []string cleanly, so the
// Android shell polls CommandCount/CommandAt on a timer to build its
// request loop instead of consuming Commands() directly.
func (s *Session) CommandCount() int {
	return len(s.inner.Commands())
}

// CommandAt returns the i'th PID request command (e.g. "010C"). The caller
// appends a carriage return and writes it to the Bluetooth socket. Returns
// "" if i is out of range rather than panicking across the gomobile/JNI
// boundary.
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
