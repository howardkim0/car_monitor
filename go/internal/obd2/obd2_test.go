package obd2

import (
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

func TestCommands(t *testing.T) {
	s := NewSession(vehicle.Default(), nil)
	got := s.Commands()
	want := []string{"010C", "010D", "0105", "0111"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commands() = %v, want %v", got, want)
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

	// Known PID (0x0D, speed) but no data bytes — decodeSpeed errors on
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
