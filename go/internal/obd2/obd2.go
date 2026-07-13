// Package obd2 frames ELM327 requests and parses ELM327 responses into
// decoded Readings, driven by an active vehicle.Profile. It has no
// knowledge of the underlying transport (Bluetooth socket, etc.) — see
// DESIGN.md section 4: the Android shell owns the socket and just feeds
// raw bytes in and pulls commands to send out.
package obd2

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

// logfFunc is the type of the optional diagnostic logger injected into
// Session. Matches fmt.Sprintf's signature so callers can pass any
// printf-style function (e.g. applog.Logger.Debugf).
type logfFunc func(format string, args ...any)

// terminator is the line ending ELM327 uses in both directions.
const terminator = '\r'

// discoveryTimeout bounds how long Session waits for "PIDs supported"
// bitmask responses (see below) before falling back to treating any
// still-unresolved range as fully supported, rather than going silent.
// A var, not a const, so tests can override it rather than sleeping for
// the real default.
var discoveryTimeout = 5 * time.Second

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
//
// It also drives a two-phase command lifecycle. Rather than
// unconditionally requesting every PID in the profile every cycle (fine
// for 4 PIDs, wasteful for dozens — see DESIGN.md section 5.2),
// Commands() initially returns only SAE J1979 "PIDs supported" discovery
// queries (Mode 01 PIDs 0x00/0x20/0x40/...). Once the ECU's bitmask
// responses (or discoveryTimeout) resolve which of the profile's PIDs it
// actually supports, Commands() switches over to the real, filtered
// per-PID request list. The caller (Kotlin's write loop) doesn't need to
// know any of this — it just keeps polling Commands() every cycle; Go
// owns the phase transition entirely.
//
// mu guards the discovery/command-cache state below because Commands()
// and Feed() are called from different concurrent goroutines in
// practice (ObdForegroundService.kt's writeLoop and readLoop run as
// sibling coroutines) — profile/onReading are set once at construction
// and never mutated, so they don't need it.
type Session struct {
	profile   vehicle.Profile
	onReading func(Reading)
	pending   []byte

	// logf is an optional diagnostic logger — nil by default so
	// NewSession callers (tests included) are unaffected. Set via
	// NewSessionWithLogger when the caller wants discovery/polling
	// diagnostics (e.g. mobile.go wires this to LogDebug so they land
	// in the persistent app log for real-hardware verification — see
	// DESIGN.md section 12).
	logf logfFunc

	mu               sync.Mutex
	discoveryStart   time.Time
	unresolvedRanges map[byte]bool // discovery-query PID -> still awaiting a response
	supported        map[byte]bool // profile PID code -> confirmed supported by the ECU
	commands         []string      // cached once discovery completes; nil until then
}

// NewSession creates a Session that decodes responses against profile,
// invoking onReading for every successfully decoded line. onReading may be
// nil if the caller only cares about Commands().
func NewSession(profile vehicle.Profile, onReading func(Reading)) *Session {
	return NewSessionWithLogger(profile, onReading, nil)
}

// NewSessionWithLogger is NewSession with an optional diagnostic logger.
// logf (which may be nil) is called with printf-style arguments at key
// discovery/polling lifecycle events — starting discovery, each range
// resolving, and the final transition to the per-PID command list. This
// is the hook mobile.go uses to route those messages into the persistent
// app log for real-hardware ELM327 verification (DESIGN.md section 12).
func NewSessionWithLogger(profile vehicle.Profile, onReading func(Reading), logf logfFunc) *Session {
	s := &Session{
		profile:          profile,
		onReading:        onReading,
		logf:             logf,
		discoveryStart:   time.Now(),
		unresolvedRanges: discoveryRanges(profile),
		supported:        make(map[byte]bool),
	}
	if logf != nil {
		logf("obd2: discovery starting: %d ranges to resolve (%v)",
			len(s.unresolvedRanges), discoveryRangeList(s.unresolvedRanges))
	}
	return s
}

// discoveryRangeList returns a sorted slice of range base values for
// logging — makes the log line deterministic instead of map-iteration-order.
func discoveryRangeList(ranges map[byte]bool) []string {
	bases := make([]byte, 0, len(ranges))
	for b := range ranges {
		bases = append(bases, b)
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })
	strs := make([]string, len(bases))
	for i, b := range bases {
		strs[i] = fmt.Sprintf("0x%02X", b)
	}
	return strs
}

// discoveryRanges returns the "PIDs supported" query PIDs (0x00, 0x20,
// 0x40, ...) needed to cover every PID code present in profile — derived
// from the profile rather than hardcoded, so it self-adjusts if the PID
// table changes.
func discoveryRanges(profile vehicle.Profile) map[byte]bool {
	var maxCode int
	for _, p := range profile.PIDs {
		if int(p.Code) > maxCode {
			maxCode = int(p.Code)
		}
	}
	// int, not byte: base ranges up to 0xE0 inclusive (8 possible ranges
	// in the 0x00-0xFF PID space), and 0xE0+0x20 == 0x100 overflows a
	// byte — using byte arithmetic here wraps back to 0x00 and never
	// satisfies "base <= maxCode", hanging this loop forever for any
	// profile PID code >= 0xE0.
	ranges := make(map[byte]bool)
	for base := 0; base <= maxCode; base += 0x20 {
		ranges[byte(base)] = true
	}
	return ranges
}

// Commands returns the ELM327 request lines to send this cycle: pending
// discovery queries while any "PIDs supported" range is still
// unresolved, or the real filtered per-PID requests once discovery has
// completed (by response or by discoveryTimeout elapsing). The caller
// polls these on a timer, appends the terminator, and writes them to the
// Bluetooth socket; Feed matches responses back up as they arrive.
func (s *Session) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.commands != nil {
		return s.commands
	}

	if len(s.unresolvedRanges) > 0 && time.Since(s.discoveryStart) < discoveryTimeout {
		return discoveryCommands(s.unresolvedRanges)
	}

	// Discovery is over, one way or another: apply the optimistic
	// fallback to any range that never got a response (matches the old
	// always-request-everything behavior instead of going silent if
	// discovery itself fails for some reason), then compute and cache
	// the real command list.
	timedOutRanges := len(s.unresolvedRanges)
	for base := range s.unresolvedRanges {
		// int, not byte, for the same overflow reason as discoveryRanges:
		// base+0x20 wraps for base == 0xE0.
		lo, hi := int(base), int(base)+0x20
		for _, p := range s.profile.PIDs {
			if int(p.Code) > lo && int(p.Code) <= hi {
				s.supported[p.Code] = true
			}
		}
	}
	s.unresolvedRanges = nil

	s.commands = buildCommands(s.profile, s.supported)
	if s.logf != nil {
		if timedOutRanges > 0 {
			// Some ranges never got a bitmask response — fell back to
			// assuming all PIDs in those ranges are supported.
			s.logf("obd2: discovery timed out after %.1fs with %d unresolved range(s); "+
				"falling back to full profile (%d commands)",
				time.Since(s.discoveryStart).Seconds(), timedOutRanges, len(s.commands))
		} else {
			s.logf("obd2: discovery complete via responses in %.1fs; "+
				"%d/%d profile PIDs confirmed supported (%d commands)",
				time.Since(s.discoveryStart).Seconds(),
				len(s.supported), len(s.profile.PIDs), len(s.commands))
		}
	}
	return s.commands
}

func discoveryCommands(ranges map[byte]bool) []string {
	bases := make([]byte, 0, len(ranges))
	for base := range ranges {
		bases = append(bases, base)
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })

	cmds := make([]string, 0, len(bases))
	for _, base := range bases {
		cmds = append(cmds, fmt.Sprintf("%02X%02X", byte(vehicle.ModeCurrentData), base))
	}
	return cmds
}

// buildCommands renders the ELM327 request lines ("010C", "010D", ...)
// for every PID in profile the ECU has confirmed supporting, in the
// profile's stable order.
func buildCommands(profile vehicle.Profile, supported map[byte]bool) []string {
	cmds := make([]string, 0, len(profile.PIDs))
	for _, p := range profile.PIDs {
		if !supported[p.Code] {
			continue
		}
		cmds = append(cmds, fmt.Sprintf("%02X%02X", byte(p.Mode), p.Code))
	}
	return cmds
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

		if s.tryHandleDiscoveryResponse(string(line)) {
			continue
		}
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

// tryHandleDiscoveryResponse checks whether line answers one of the
// still-pending "PIDs supported" queries; if so, it decodes the 32-bit
// bitmask (byte A bit 7 == PID base+1, ... byte D bit 0 == PID base+32,
// per the SAE J1979 convention), marks matching profile PIDs supported,
// resolves that range, and returns true so Feed skips the normal
// Reading-decode path — a bitmask isn't vehicle telemetry.
func (s *Session) tryHandleDiscoveryResponse(line string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.unresolvedRanges) == 0 {
		return false
	}

	raw, ok := parseResponseBytes(line)
	if !ok || len(raw) < 6 {
		return false
	}

	base := raw[1]
	if !s.unresolvedRanges[base] {
		return false
	}

	mask := uint32(raw[2])<<24 | uint32(raw[3])<<16 | uint32(raw[4])<<8 | uint32(raw[5])
	for k := 0; k < 32; k++ {
		if (mask>>(31-k))&1 != 1 {
			continue
		}
		// int, not byte: base+1+k overflows a byte for base == 0xE0,
		// k == 31 (would be PID code 0x100). That 32nd bit of the last
		// possible range has no corresponding PID — SAE only defines
		// codes 0x00-0xFF — so it's simply skipped rather than wrapped
		// into a bogus low PID code.
		code := int(base) + 1 + k
		if code <= 0xFF {
			s.supported[byte(code)] = true
		}
	}
	delete(s.unresolvedRanges, base)
	s.commands = nil // force Commands() to recompute now that a range resolved
	if s.logf != nil {
		s.logf("obd2: discovery range 0x%02X resolved: %d PIDs marked supported so far; %d range(s) still pending",
			base, len(s.supported), len(s.unresolvedRanges))
	}
	return true
}

// parseResponseBytes splits a response line into raw hex bytes and
// confirms it's a positive response to a mode 01-09 request. Anything
// that isn't — the ">" prompt, "SEARCHING...", "NO DATA", command echo,
// blank lines — fails this and is ignored by both callers below.
func parseResponseBytes(line string) ([]byte, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return nil, false
	}

	raw := make([]byte, 0, len(fields))
	for _, f := range fields {
		b, err := strconv.ParseUint(f, 16, 8)
		if err != nil {
			return nil, false
		}
		raw = append(raw, byte(b))
	}

	if raw[0] < 0x41 || raw[0] > 0x49 {
		return nil, false // not a positive response to a mode 01-09 request
	}
	return raw, true
}

// parseLine parses one ELM327 response line, e.g. "41 0C 1A F8", into a
// Reading.
func (s *Session) parseLine(line string) (Reading, bool) {
	raw, ok := parseResponseBytes(line)
	if !ok {
		return Reading{}, false
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
		Timestamp: time.Now().UTC(),
	}, true
}
