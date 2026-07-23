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

// rawLineSampleLimit bounds how many raw line contents are logged for
// diagnostics. The first rawLineSampleLimit lines (regardless of whether
// they parse successfully) are logged with their exact byte content quoted,
// to provide visibility into exactly what arrived from the adapter.
// Beyond this limit, no raw lines are logged, only aggregate statistics —
// keeping logs focused on first-time startup diagnostics.
const rawLineSampleLimit = 20

// statsLogEveryNLines fires a statistics log every statsLogEveryNLines
// lines received, reporting cumulative counts and decode percentage. This
// provides visibility into data reception vs. decoding rates over time
// without flooding the log with per-line noise.
const statsLogEveryNLines = 100

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
	// in the persistent app log for real-hardware verification.
	logf logfFunc

	// linesReceived and linesDecoded track raw reception vs. successful
	// parsing for content diagnostics. Feed() increments
	// linesReceived for every extracted line and linesDecoded whenever
	// parseLine succeeds, enabling real-time visibility into adapter
	// behavior: e.g. "received 300 lines but only decoded 240" reveals
	// a high noise/truncation rate. These fields are guarded by no mutex —
	// Feed() is always called sequentially from a single goroutine
	// (ObdForegroundService's readLoop), so no concurrency protection needed.
	linesReceived uint64
	linesDecoded  uint64

	// skipLeadingLF is set when a line-terminating '\r' was the last byte
	// available in a Feed call, so whether a paired '\n' follows it won't
	// be known until the next call. See Feed's CRLF handling.
	skipLeadingLF bool

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
// app log for real-hardware ELM327 verification.
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

// InitCommands returns the fixed ELM327 setup sequence sent once per
// connection, before any PID/discovery command — see DESIGN.md section 4
// step 5 for why. ATS1 and ATH0 are load-bearing: parseResponseBytes
// requires space-separated single-byte hex fields with no header/CAN-ID
// field, and a prior session (this app's or a different one's) can leave
// the adapter with either turned off/on the wrong way, since ELM327
// settings persist across Bluetooth reconnects.
func InitCommands() []string {
	return []string{"ATE0", "ATL0", "ATS1", "ATH0", "ATSP0"}
}

// Feed appends newly-read bytes from the Bluetooth socket and processes
// every complete (terminator-delimited) line. Partial lines are buffered
// until the remainder arrives in a later call.
//
// Some adapters send a line feed after the carriage return (despite
// ATL0), which has no content of its own but would otherwise surface as
// a spurious, permanently-undecodable extra line paired with every real
// one — capping the decode percentage Feed logs below at ~50% regardless
// of how well the adapter is actually responding. Feed swallows a '\n'
// immediately following '\r' so it's never counted as a received line;
// skipLeadingLF handles the case where the '\r' was the last byte of one
// call and the '\n' arrives at the start of the next.
//
// Content diagnostics: Feed tracks raw reception and successful parsing
// rates for visibility into adapter behavior. The first
// rawLineSampleLimit lines have their exact byte content logged, and
// every statsLogEveryNLines lines a cumulative log reports received vs.
// decoded counts and the decode percentage.
func (s *Session) Feed(data []byte) {
	// Guarded by len(data) > 0, not just checked inside: an empty/nil
	// call must leave skipLeadingLF exactly as it was, since it hasn't
	// actually seen whether the next byte is the paired '\n' yet —
	// clearing it unconditionally would let a stranded '\n' through as
	// its own line if Feed is ever called with no data in between.
	if len(data) > 0 {
		if s.skipLeadingLF && data[0] == '\n' {
			data = data[1:]
		}
		s.skipLeadingLF = false
	}

	s.pending = append(s.pending, data...)
	for {
		idx := bytes.IndexByte(s.pending, terminator)
		if idx == -1 {
			break
		}
		line := s.pending[:idx]
		remainder := s.pending[idx+1:]
		if len(remainder) == 0 {
			s.skipLeadingLF = true
		} else if remainder[0] == '\n' {
			remainder = remainder[1:]
		}
		s.pending = remainder

		// Track reception: every line extracted increments the count.
		s.linesReceived++

		// Log raw line content for the first rawLineSampleLimit lines,
		// regardless of whether it later parses successfully — provides
		// initial visibility into what the adapter is actually sending.
		if s.logf != nil && s.linesReceived <= uint64(rawLineSampleLimit) {
			s.logf("obd2: raw line %d: %q", s.linesReceived, string(line))
		}

		// stripPrompt before parsing, not before logging above: the raw
		// log should show exactly what was received, prompt included.
		parsedLine := stripPrompt(string(line))

		if s.tryHandleDiscoveryResponse(parsedLine) {
			// Discovery response was handled; skip the normal parsing path.
		} else {
			// Track decoding: increment whenever parseLine succeeds, regardless
			// of whether onReading is nil (the counter tracks "successfully decoded",
			// not "delivered to listener").
			if reading, ok := s.parseLine(parsedLine); ok {
				s.linesDecoded++
				if s.onReading != nil {
					s.onReading(reading)
				}
			}
		}

		// Log statistics every statsLogEveryNLines lines, after discovery
		// response handling and parsing have both had a chance to run.
		if s.logf != nil && s.linesReceived%uint64(statsLogEveryNLines) == 0 {
			s.logf("obd2: stats: %d lines received, %d decoded as readings (%.0f%%)",
				s.linesReceived, s.linesDecoded, float64(s.linesDecoded)/float64(s.linesReceived)*100)
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

// stripPrompt removes a leading ELM327 ready prompt ('>') from line, if
// present. The prompt has no terminator of its own — the adapter sends
// it immediately after a response's trailing '\r' with nothing
// following — so once Feed splits purely on the terminator, it lands
// glued onto the front of whatever line comes next (e.g.
// ">41 0C 1A F8" instead of an empty line followed by a clean
// "41 0C 1A F8"), corrupting an otherwise-valid response rather than
// just appearing harmlessly on its own. See docs/defects.md.
func stripPrompt(line string) string {
	return strings.TrimLeft(line, ">")
}

// parseResponseBytes splits a response line into raw hex bytes and
// confirms it's a positive response to a mode 01-09 request. Anything
// that isn't — a lone ELM327 prompt already stripped down to "" by
// stripPrompt, "SEARCHING...", "NO DATA", command echo, blank lines —
// fails this and is ignored by both callers below.
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
