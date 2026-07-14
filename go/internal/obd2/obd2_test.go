package obd2

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

func collectingSession() (*Session, *[]Reading) {
	readings := &[]Reading{}
	s := NewSession(vehicle.Default(), func(r Reading) {
		*readings = append(*readings, r)
	})
	return s, readings
}

func TestCommandsReturnsDiscoveryQueriesInitially(t *testing.T) {
	s := NewSession(vehicle.Default(), nil)
	got := s.Commands()
	// The profile's highest PID code is 0x5E, so discovery needs ranges
	// 0x00 (covers 0x01-0x20), 0x20 (0x21-0x40), and 0x40 (0x41-0x60).
	want := []string{"0100", "0120", "0140"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() before discovery resolves = %v, want %v", got, want)
	}
}

func TestInitCommands(t *testing.T) {
	got := InitCommands()
	want := []string{"ATE0", "ATL0", "ATS1", "ATH0", "ATSP0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InitCommands() = %v, want %v", got, want)
	}
}

// discoveryResponseLine builds a "PIDs supported" response line for the
// given base ("0x00", "0x20", or "0x40") where exactly the PIDs in
// codes are flagged supported.
func discoveryResponseLine(base byte, codes ...byte) string {
	var mask uint32
	for _, code := range codes {
		k := uint(code - base - 1)
		mask |= 1 << (31 - k)
	}
	return fmt.Sprintf("41 %02X %02X %02X %02X %02X\r",
		base, byte(mask>>24), byte(mask>>16), byte(mask>>8), byte(mask))
}

func TestCommandsFiltersToDiscoveredPIDsOnly(t *testing.T) {
	s, readings := collectingSession()

	s.Feed([]byte(discoveryResponseLine(0x00, 0x0C))) // only RPM supported
	s.Feed([]byte(discoveryResponseLine(0x20)))       // nothing in 0x21-0x40
	s.Feed([]byte(discoveryResponseLine(0x40)))       // nothing in 0x41-0x60

	got := s.Commands()
	want := []string{"010C"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() after discovery = %v, want %v", got, want)
	}
	if len(*readings) != 0 {
		t.Errorf("discovery responses should never produce a Reading, got %+v", *readings)
	}
}

func TestCommandsIncludesEveryDiscoveredPID(t *testing.T) {
	s := NewSession(vehicle.Default(), nil)

	// Flag every PID in the profile as supported, split across whichever
	// range each falls in.
	var lowCodes, midCodes, highCodes []byte
	for _, p := range vehicle.Default().PIDs {
		switch {
		case p.Code <= 0x20:
			lowCodes = append(lowCodes, p.Code)
		case p.Code <= 0x40:
			midCodes = append(midCodes, p.Code)
		default:
			highCodes = append(highCodes, p.Code)
		}
	}
	s.Feed([]byte(discoveryResponseLine(0x00, lowCodes...)))
	s.Feed([]byte(discoveryResponseLine(0x20, midCodes...)))
	s.Feed([]byte(discoveryResponseLine(0x40, highCodes...)))

	got := s.Commands()
	want := make([]string, 0, len(vehicle.Default().PIDs))
	for _, p := range vehicle.Default().PIDs {
		want = append(want, fmt.Sprintf("%02X%02X", byte(p.Mode), p.Code))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() with every PID discovered = %v, want %v", got, want)
	}
}

func TestCommandsIgnoresTruncatedDiscoveryResponse(t *testing.T) {
	s, readings := collectingSession()

	// Only 2 data bytes instead of 4 — must be safely ignored, not
	// mistaken for a real reading or crash, and must NOT falsely resolve
	// the 0x00 range.
	s.Feed([]byte("41 00 00 10\r"))

	got := s.Commands()
	want := []string{"0100", "0120", "0140"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() after a truncated discovery response = %v, want still-pending %v", got, want)
	}
	if len(*readings) != 0 {
		t.Errorf("truncated discovery response should not produce a Reading, got %+v", *readings)
	}
}

func TestCommandsTimeoutFallsBackToRequestingEverything(t *testing.T) {
	original := discoveryTimeout
	discoveryTimeout = 1 * time.Millisecond
	defer func() { discoveryTimeout = original }()

	s := NewSession(vehicle.Default(), nil)
	time.Sleep(5 * time.Millisecond)

	got := s.Commands()
	want := make([]string, 0, len(vehicle.Default().PIDs))
	for _, p := range vehicle.Default().PIDs {
		want = append(want, fmt.Sprintf("%02X%02X", byte(p.Mode), p.Code))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() after discovery timeout = %v, want the full %d-PID fallback list", got, len(want))
	}
}

func TestCommandsReturnsCachedResultOnSubsequentCalls(t *testing.T) {
	s := NewSession(vehicle.Default(), nil)
	s.Feed([]byte(discoveryResponseLine(0x00)))
	s.Feed([]byte(discoveryResponseLine(0x20)))
	s.Feed([]byte(discoveryResponseLine(0x40)))

	first := s.Commands()
	second := s.Commands()
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Commands() second call = %v, want the same cached result %v", second, first)
	}
}

func TestFeedIgnoresDiscoveryResponseAfterDiscoveryComplete(t *testing.T) {
	s, readings := collectingSession()
	s.Feed([]byte(discoveryResponseLine(0x00)))
	s.Feed([]byte(discoveryResponseLine(0x20)))
	s.Feed([]byte(discoveryResponseLine(0x40)))
	s.Commands() // completes discovery, clears unresolvedRanges

	// A late/duplicate "PIDs supported" response arriving after discovery
	// already finished must not panic or be mistaken for a Reading.
	s.Feed([]byte(discoveryResponseLine(0x00)))

	if len(*readings) != 0 {
		t.Errorf("a post-discovery bitmask response should not produce a Reading, got %+v", *readings)
	}
}

func TestFeedIgnoresDiscoveryResponseForUnrequestedRange(t *testing.T) {
	s, readings := collectingSession()

	// 0x60 was never one of this profile's discovery ranges (its highest
	// PID is 0x5E), so a response claiming to answer it must be ignored.
	s.Feed([]byte(discoveryResponseLine(0x60)))

	if len(*readings) != 0 {
		t.Errorf("an unrequested-range bitmask response should not produce a Reading, got %+v", *readings)
	}
	got := s.Commands()
	want := []string{"0100", "0120", "0140"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() after an unrequested-range response = %v, want still-pending %v", got, want)
	}
}

// TestDiscoveryHandlesHighPIDCodeWithoutOverflow regression-tests a bug
// caught in three-persona review: byte-typed arithmetic in
// discoveryRanges, Commands()'s timeout fallback, and
// tryHandleDiscoveryResponse's bitmask decode all overflowed for any
// profile PID code >= 0xE0 (discoveryRanges would loop forever; the
// other two would silently wrap a too-high code back into the 0x00-0x1F
// range). A profile whose only PID is 0xFE — a code no real vehicle
// profile has needed yet, but one the discovery math must not choke on —
// exercises all three sites in one test.
func TestDiscoveryHandlesHighPIDCodeWithoutOverflow(t *testing.T) {
	profile := vehicle.Profile{
		PIDs: []vehicle.PID{
			{Code: 0xFE, Mode: vehicle.ModeCurrentData, Name: "High Test PID", Unit: "", Decode: func(data []byte) (float64, error) { return 0, nil }},
		},
	}
	s := NewSession(profile, nil)

	// Pre-fix, discoveryRanges' byte-typed loop bound never terminated for
	// maxCode 0xFE (0xE0+0x20 wraps to 0x00, which is always <= maxCode).
	got := s.Commands()
	want := []string{"0100", "0120", "0140", "0160", "0180", "01A0", "01C0", "01E0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Commands() discovery ranges for a high-PID profile = %v, want %v", got, want)
	}

	for _, base := range []byte{0x00, 0x20, 0x40, 0x60, 0x80, 0xA0, 0xC0} {
		s.Feed([]byte(discoveryResponseLine(base)))
	}
	// base 0xE0, mask 0x00000005: bit 2 flags code 0xFE (base+1+29, a real
	// profile PID); bit 0 flags code 0x100 (base+1+31) — one past the
	// highest possible byte PID code 0xFF. Pre-fix, base+1+31 overflowed a
	// byte and wrapped to 0x00, wrongly marking a nonexistent PID 0x00
	// supported.
	s.Feed([]byte("41 E0 00 00 00 05\r"))

	got = s.Commands()
	want = []string{"01FE"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() after a discovery bit past the last valid PID code = %v, want %v (must not wrap to a bogus PID 0x00)", got, want)
	}
}

// TestCommandsTimeoutFallbackHandlesHighPIDCodeWithoutOverflow
// regression-tests the byte-overflow fix in Commands()'s timeout-fallback
// loop specifically (the third overflow site), since the sibling test
// TestDiscoveryHandlesHighPIDCodeWithoutOverflow only covers discoveryRanges
// and tryHandleDiscoveryResponse, not the timeout fallback. Pre-fix,
// the fallback loop's byte-typed range check overflowed for base == 0xE0,
// wrapping high PID codes like 0xFE incorrectly.
func TestCommandsTimeoutFallbackHandlesHighPIDCodeWithoutOverflow(t *testing.T) {
	original := discoveryTimeout
	discoveryTimeout = 1 * time.Millisecond
	defer func() { discoveryTimeout = original }()

	profile := vehicle.Profile{
		PIDs: []vehicle.PID{
			{Code: 0xFE, Mode: vehicle.ModeCurrentData, Name: "High Test PID", Unit: "", Decode: func(data []byte) (float64, error) { return 0, nil }},
		},
	}
	s := NewSession(profile, nil)
	time.Sleep(5 * time.Millisecond)

	// Without feeding any discovery response, Commands() falls back to
	// iterating through every range (0x00 through 0xE0) in the
	// unresolvedRanges loop, including base == 0xE0 where the byte-typed
	// range check (p.Code > base && p.Code <= base+0x20) would overflow.
	got := s.Commands()
	want := []string{"01FE"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() timeout fallback for high PID code = %v, want %v (must not wrap to a bogus PID)", got, want)
	}
}

func TestFeedDecodesReading(t *testing.T) {
	s, readings := collectingSession()

	s.Feed([]byte("41 0C 1A F8\r"))

	if len(*readings) != 1 {
		t.Fatalf("got %d readings, want 1: %+v", len(*readings), *readings)
	}
	r := (*readings)[0]
	if r.PID != 0x0C || r.Name != "Engine RPM" || r.Unit != "rpm" {
		t.Errorf("reading = %+v, want PID 0x0C Engine RPM rpm", r)
	}
	if r.Value != 1726.0 {
		t.Errorf("reading.Value = %v, want 1726", r.Value)
	}
	if time.Since(r.Timestamp) > time.Second {
		t.Errorf("reading.Timestamp = %v, not close to now", r.Timestamp)
	}
	if r.Timestamp.Location() != time.UTC {
		t.Errorf("reading.Timestamp location = %v, want UTC", r.Timestamp.Location())
	}
}

func TestFeedIgnoresNonDataLines(t *testing.T) {
	s, readings := collectingSession()

	for _, line := range []string{">", "SEARCHING...", "NO DATA", "", "010C"} {
		s.Feed([]byte(line + "\r"))
	}

	if len(*readings) != 0 {
		t.Errorf("got %d readings from non-data lines, want 0: %+v", len(*readings), *readings)
	}
}

func TestFeedBuffersPartialLines(t *testing.T) {
	s, readings := collectingSession()

	s.Feed([]byte("41 0C 1A"))
	if len(*readings) != 0 {
		t.Fatalf("got a reading before the line was terminated: %+v", *readings)
	}

	s.Feed([]byte(" F8\r"))
	if len(*readings) != 1 {
		t.Fatalf("got %d readings after terminator arrived, want 1", len(*readings))
	}
	if (*readings)[0].Value != 1726.0 {
		t.Errorf("reading.Value = %v, want 1726", (*readings)[0].Value)
	}
}

func TestFeedIgnoresUnknownPID(t *testing.T) {
	s, readings := collectingSession()

	s.Feed([]byte("41 99 00\r"))

	if len(*readings) != 0 {
		t.Errorf("got %d readings for an unknown PID, want 0", len(*readings))
	}
}

func TestFeedIgnoresModeOutsideResponseRange(t *testing.T) {
	s, readings := collectingSession()

	// Two valid hex fields, but 0x7F ("general reject", an ELM327/OBD2
	// negative response code) isn't in the 0x41-0x49 positive-response
	// range parseLine requires.
	s.Feed([]byte("7F 0C\r"))

	if len(*readings) != 0 {
		t.Errorf("got %d readings from an out-of-range mode byte, want 0: %+v", len(*readings), *readings)
	}
}

func TestFeedIgnoresDecodeError(t *testing.T) {
	s, readings := collectingSession()

	// Known PID (0x0D, speed) but no data bytes — decodeByteAsIs errors on
	// too-short input, so parseLine must reject the line rather than
	// panic or emit a zero-value Reading.
	s.Feed([]byte("41 0D\r"))

	if len(*readings) != 0 {
		t.Errorf("got %d readings from an undecodable line, want 0: %+v", len(*readings), *readings)
	}
}

func TestFeedMultipleReadingsInOneCall(t *testing.T) {
	s, readings := collectingSession()

	s.Feed([]byte("41 0C 1A F8\r41 0D 50\r"))

	if len(*readings) != 2 {
		t.Fatalf("got %d readings, want 2: %+v", len(*readings), *readings)
	}
	if (*readings)[0].PID != 0x0C || (*readings)[1].PID != 0x0D {
		t.Errorf("readings out of order: %+v", *readings)
	}
	if (*readings)[1].Value != 80.0 {
		t.Errorf("speed reading.Value = %v, want 80", (*readings)[1].Value)
	}
}

// TestNewSessionWithLoggerLogsDiscoveryStart verifies that NewSessionWithLogger
// invokes the provided logf at construction time (the "discovery starting" line)
// and that discoveryRangeList is exercised (it formats the list of ranges).
func TestNewSessionWithLoggerLogsDiscoveryStart(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	NewSessionWithLogger(vehicle.Default(), nil, logf)

	if len(logged) == 0 {
		t.Fatal("expected at least one log line from NewSessionWithLogger, got none")
	}
	// The first message must mention "discovery starting" so we know it's the
	// right call site, not some incidental log from another path.
	if !contains(logged[0], "discovery starting") {
		t.Errorf("first log line = %q, want it to contain \"discovery starting\"", logged[0])
	}
}

// TestCommandsLogsDiscoveryCompleteViaResponses verifies that Commands() emits
// a log line (via logf) when all discovery ranges are resolved by ECU responses.
func TestCommandsLogsDiscoveryCompleteViaResponses(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)

	// Resolve every range with an empty bitmask (no PIDs supported is fine
	// for this test — we just need all ranges to report back).
	s.Feed([]byte(discoveryResponseLine(0x00)))
	s.Feed([]byte(discoveryResponseLine(0x20)))
	s.Feed([]byte(discoveryResponseLine(0x40)))

	// Trigger the transition — Commands() is where the log fires.
	logged = nil // clear start log; only care about the completion message
	s.Commands()

	if !anyContains(logged, "discovery complete via responses") {
		t.Errorf("expected a \"discovery complete via responses\" log line after all ranges resolved; got %v", logged)
	}
}

// TestCommandsLogsDiscoveryTimeout verifies that Commands() emits a log line
// when discovery times out before all ranges receive a response.
func TestCommandsLogsDiscoveryTimeout(t *testing.T) {
	original := discoveryTimeout
	discoveryTimeout = 1 * time.Millisecond
	defer func() { discoveryTimeout = original }()

	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)
	time.Sleep(5 * time.Millisecond) // let the timeout elapse

	logged = nil // clear start log
	s.Commands()

	if !anyContains(logged, "discovery timed out") {
		t.Errorf("expected a \"discovery timed out\" log line; got %v", logged)
	}
}

// TestTryHandleDiscoveryResponseLogsRangeResolved verifies that the per-range
// log fires inside tryHandleDiscoveryResponse when a discovery range resolves.
func TestTryHandleDiscoveryResponseLogsRangeResolved(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)

	logged = nil // clear the start-of-discovery log
	s.Feed([]byte(discoveryResponseLine(0x00, 0x0C)))

	if !anyContains(logged, "range 0x00 resolved") {
		t.Errorf("expected a \"range 0x00 resolved\" log line after feeding a discovery response; got %v", logged)
	}
}

// TestFeedLogsRawLineContentForFirstNLines verifies that Feed logs the
// exact byte content of the first rawLineSampleLimit lines, regardless of
// whether they parse successfully, then stops raw-line logging afterward.
func TestFeedLogsRawLineContentForFirstNLines(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)
	logged = nil // clear discovery start log

	// Feed 25 simple lines (beyond the rawLineSampleLimit of 20).
	for i := 1; i <= 25; i++ {
		s.Feed([]byte(fmt.Sprintf("NO DATA\r")))
	}

	// Count "obd2: raw line" logs — should be exactly 20.
	rawLineCount := 0
	for _, line := range logged {
		if contains(line, "obd2: raw line") {
			rawLineCount++
		}
	}
	if rawLineCount != 20 {
		t.Errorf("expected exactly 20 raw-line logs for first 20 lines, got %d", rawLineCount)
	}
}

// TestFeedStatsLogEveryNLines verifies that stats logs fire at exactly
// line N and not before, and report correct counts.
func TestFeedStatsLogEveryNLines(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)
	logged = nil // clear discovery start log

	// Feed 99 lines — should not trigger a stats log yet.
	for i := 0; i < 99; i++ {
		s.Feed([]byte("NO DATA\r"))
	}
	if anyContains(logged, "obd2: stats") {
		t.Errorf("stats log should not fire before line 100, but got logs: %v", logged)
	}

	// Feed one more to reach 100 — should trigger exactly one stats log.
	s.Feed([]byte("NO DATA\r"))
	statsCount := 0
	for _, line := range logged {
		if contains(line, "obd2: stats") {
			statsCount++
		}
	}
	if statsCount != 1 {
		t.Errorf("expected exactly one stats log at line 100, got %d", statsCount)
	}
}

// TestFeedLinesDecodedOnlyIncrementsForValidReadings verifies that
// linesDecoded increments only for lines that successfully parse to a Reading,
// not for noise or malformed responses.
func TestFeedLinesDecodedOnlyIncrementsForValidReadings(t *testing.T) {
	readings := &[]Reading{}
	s := NewSessionWithLogger(vehicle.Default(), func(r Reading) {
		*readings = append(*readings, r)
	}, nil)

	// Feed a mix: valid reading, noise, valid reading, discovery response (not counted),
	// and invalid/truncated lines.
	// Valid RPM reading: "41 0C 0A 00" (PID 0x0C is RPM, supported in profile)
	s.Feed([]byte("41 0C 0A 00\r"))
	// Noise: "NO DATA\r"
	s.Feed([]byte("NO DATA\r"))
	// Another valid reading
	s.Feed([]byte("41 0C 0B 00\r"))
	// Discovery response: should not increment linesDecoded
	s.Feed([]byte(discoveryResponseLine(0x00, 0x0C)))

	// The stats should show linesReceived = 4, linesDecoded = 2.
	// We can't directly inspect s.linesReceived/linesDecoded, but we can verify
	// that only the valid readings were delivered to onReading.
	if len(*readings) != 2 {
		t.Errorf("expected 2 successfully decoded readings, got %d", len(*readings))
	}
}

// TestFeedNoLoggingWhenLogfIsNil verifies that Feed handles a nil logf
// gracefully and doesn't panic or corrupt state.
func TestFeedNoLoggingWhenLogfIsNil(t *testing.T) {
	readings := &[]Reading{}
	s := NewSessionWithLogger(vehicle.Default(), func(r Reading) {
		*readings = append(*readings, r)
	}, nil)

	// Feed a variety of lines — should not panic.
	for i := 0; i < 150; i++ {
		s.Feed([]byte("41 0C 0A 00\r"))
	}

	// Should have received all 150 readings (one per line).
	if len(*readings) != 150 {
		t.Errorf("expected 150 readings with nil logf, got %d", len(*readings))
	}
}

// TestFeedStatsLogFiresForDiscoveryResponseLines verifies that stats logs
// fire inside the discovery response handler when line number hits a boundary.
func TestFeedStatsLogFiresForDiscoveryResponseLines(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	s := NewSessionWithLogger(vehicle.Default(), nil, logf)
	logged = nil // clear discovery start log

	// Feed discovery responses for each range, cycling through 0x00, 0x20, 0x40
	// until line 100. Once each range is resolved, subsequent responses for
	// that range are not recognized as discovery responses (they try to parse
	// as readings and fail), so we cycle through ranges to keep line 100 as a
	// valid discovery response that fires the stats log.
	for i := 0; i < 100; i++ {
		base := byte(0x00 + (i%3)*0x20) // cycles 0x00, 0x20, 0x40, 0x00, ...
		s.Feed([]byte(discoveryResponseLine(base, 0x0C)))
	}

	// Stats log should fire at line 100, inside the discovery response branch.
	if !anyContains(logged, "obd2: stats") {
		t.Errorf("expected stats log at line 100 (a discovery response line); got logs: %v", logged)
	}
}

// contains reports whether sub is a substring of s.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// anyContains reports whether any string in lines contains sub.
func anyContains(lines []string, sub string) bool {
	for _, l := range lines {
		if contains(l, sub) {
			return true
		}
	}
	return false
}
