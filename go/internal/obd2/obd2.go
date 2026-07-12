// Package obd2 frames ELM327 requests and parses ELM327 responses into
// decoded Readings, driven by an active vehicle.Profile. It has no
// knowledge of the underlying transport (Bluetooth socket, etc.) — see
// DESIGN.md section 4: the Android shell owns the socket and just feeds
// raw bytes in and pulls commands to send out.
package obd2

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

// terminator is the line ending ELM327 uses in both directions.
const terminator = '\r'

// Reading is one decoded value read from the vehicle at a point in time.
type Reading struct {
	PID       byte      `json:"pid"`
	Name      string    `json:"name"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

// Session tracks the active vehicle profile and buffers partial input
// while decoding a stream of ELM327 response lines into Readings.
type Session struct {
	profile   vehicle.Profile
	commands  []string
	pending   []byte
	onReading func(Reading)
}

// NewSession creates a Session that decodes responses against profile,
// invoking onReading for every successfully decoded line. onReading may be
// nil if the caller only cares about Commands().
func NewSession(profile vehicle.Profile, onReading func(Reading)) *Session {
	return &Session{
		profile:   profile,
		commands:  buildCommands(profile),
		onReading: onReading,
	}
}

// buildCommands renders the ELM327 request lines ("010C", "010D", ...) for
// every PID in profile, in a stable order.
func buildCommands(profile vehicle.Profile) []string {
	cmds := make([]string, 0, len(profile.PIDs))
	for _, p := range profile.PIDs {
		cmds = append(cmds, fmt.Sprintf("%02X%02X", byte(p.Mode), p.Code))
	}
	return cmds
}

// Commands returns the ELM327 request lines for every PID the active
// vehicle profile supports. The caller polls these on a timer, appends the
// terminator, and writes them to the Bluetooth socket; Feed matches
// responses back up as they arrive.
func (s *Session) Commands() []string {
	return s.commands
}

// Feed appends newly-read bytes from the Bluetooth socket and processes
// every complete (terminator-delimited) line. Partial lines are buffered
// until the remainder arrives in a later call.
func (s *Session) Feed(data []byte) {
	s.pending = append(s.pending, data...)
	for {
		idx := bytes.IndexByte(s.pending, terminator)
		if idx == -1 {
			break
		}
		line := s.pending[:idx]
		s.pending = s.pending[idx+1:]
		if reading, ok := s.parseLine(string(line)); ok && s.onReading != nil {
			s.onReading(reading)
		}
	}
	if len(s.pending) == 0 {
		// Drop the reference to the (possibly large, reallocated) backing
		// array once every line has been consumed, instead of letting a
		// long-running session's buffer only ever grow via append.
		s.pending = nil
	}
}

// parseLine parses one ELM327 response line, e.g. "41 0C 1A F8", into a
// Reading. Anything that isn't a clean, space-separated hex byte response
// with a recognized PID — the ">" prompt, "SEARCHING...", "NO DATA",
// command echo, blank lines — is ignored.
func (s *Session) parseLine(line string) (Reading, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Reading{}, false
	}

	raw := make([]byte, 0, len(fields))
	for _, f := range fields {
		b, err := strconv.ParseUint(f, 16, 8)
		if err != nil {
			return Reading{}, false
		}
		raw = append(raw, byte(b))
	}

	mode := raw[0]
	if mode < 0x41 || mode > 0x49 {
		return Reading{}, false // not a positive response to a mode 01-09 request
	}

	pidCode := raw[1]
	pid, ok := s.profile.PID(pidCode)
	if !ok {
		return Reading{}, false
	}

	value, err := pid.Decode(raw[2:])
	if err != nil {
		return Reading{}, false
	}

	return Reading{
		PID:       pidCode,
		Name:      pid.Name,
		Value:     value,
		Unit:      pid.Unit,
		Timestamp: time.Now(),
	}, true
}
