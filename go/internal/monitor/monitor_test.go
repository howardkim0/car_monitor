package monitor

import (
	"testing"
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
	"github.com/howardkim0/car_monitor/go/internal/trend"
	"github.com/howardkim0/car_monitor/go/internal/vehicle"
)

// TestMetricNamesMatchVehicleProfile guards against this package's
// hardcoded metric-name constants silently drifting away from
// vehicle.go's PID.Name fields — e.g. a future rename in vehicle.go
// would otherwise make Evaluate quietly stop finding that metric's data
// at all, with no error and no test failure anywhere else.
func TestMetricNamesMatchVehicleProfile(t *testing.T) {
	want := []string{
		coolantTemp, controlModuleVoltage, engineRPM, runTimeSinceStart,
		shortTermFuelTrimBank1, longTermFuelTrimBank1,
		o2SensorBank1Sensor1, o2SensorBank1Sensor2,
	}

	have := map[string]bool{}
	for _, pid := range vehicle.Default().PIDs {
		have[pid.Name] = true
	}

	for _, name := range want {
		if !have[name] {
			t.Errorf("metric name %q used by internal/monitor has no matching PID in vehicle.Default()", name)
		}
	}
}

func reading(name string, value float64, t time.Time) obd2.Reading {
	return obd2.Reading{Name: name, Value: value, Timestamp: t}
}

func TestSeries(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(coolantTemp, 90, t0),
		reading(engineRPM, 1500, t0),
		reading(coolantTemp, 91, t0.Add(time.Second)),
	}

	times, values := series(readings, coolantTemp)
	if len(times) != 2 || len(values) != 2 {
		t.Fatalf("series() = %d times, %d values, want 2 and 2", len(times), len(values))
	}
	if values[0] != 90 || values[1] != 91 {
		t.Errorf("series() values = %v, want [90 91]", values)
	}

	if times, values := series(readings, "Nonexistent Metric"); times != nil || values != nil {
		t.Errorf("series() for a metric with no data = %v, %v, want nil, nil", times, values)
	}
}

func TestLatest(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(engineRPM, 1500, t0),
		reading(engineRPM, 1800, t0.Add(time.Second)),
	}

	if got := latest(readings, engineRPM); got != 1800 {
		t.Errorf("latest() = %v, want 1800 (the most recent)", got)
	}
	if got := latest(readings, "Nonexistent Metric"); got != 0 {
		t.Errorf("latest() for a metric with no data = %v, want 0", got)
	}
	if got := latest(nil, engineRPM); got != 0 {
		t.Errorf("latest(nil, ...) = %v, want 0", got)
	}
}

func TestPairSeries(t *testing.T) {
	t0 := time.Now()

	t.Run("perfectly interleaved", func(t *testing.T) {
		readings := []obd2.Reading{
			reading(shortTermFuelTrimBank1, 1, t0),
			reading(longTermFuelTrimBank1, 2, t0.Add(50*time.Millisecond)),
			reading(shortTermFuelTrimBank1, 3, t0.Add(2*time.Second)),
			reading(longTermFuelTrimBank1, 4, t0.Add(2050*time.Millisecond)),
		}
		times, as, bs := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 2 {
			t.Fatalf("got %d pairs, want 2: times=%v as=%v bs=%v", len(times), times, as, bs)
		}
		if as[0] != 1 || bs[0] != 2 || as[1] != 3 || bs[1] != 4 {
			t.Errorf("pairs = as=%v bs=%v, want as=[1 3] bs=[2 4]", as, bs)
		}
	})

	t.Run("a reading with no b at all is dropped", func(t *testing.T) {
		readings := []obd2.Reading{reading(shortTermFuelTrimBank1, 1, t0)}
		times, as, bs := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 0 || as != nil || bs != nil {
			t.Errorf("got times=%v as=%v bs=%v, want all empty (no b series at all)", times, as, bs)
		}
	})

	t.Run("b reading outside tolerance is dropped, not mismatched", func(t *testing.T) {
		readings := []obd2.Reading{
			reading(shortTermFuelTrimBank1, 1, t0),
			reading(longTermFuelTrimBank1, 2, t0.Add(10*time.Second)), // way outside pairTolerance
		}
		times, as, bs := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 0 {
			t.Errorf("got %d pairs, want 0 (b reading is outside pairTolerance): as=%v bs=%v", len(times), as, bs)
		}
	})

	t.Run("exactly at tolerance boundary still pairs", func(t *testing.T) {
		readings := []obd2.Reading{
			reading(shortTermFuelTrimBank1, 1, t0),
			reading(longTermFuelTrimBank1, 2, t0.Add(pairTolerance)),
		}
		times, _, _ := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 1 {
			t.Errorf("got %d pairs, want 1 (exactly at pairTolerance should still match)", len(times))
		}
	})

	t.Run("one dropped reading mid-series reuses the nearest surviving value", func(t *testing.T) {
		// Simulates a single missed/dropped LTFT response: rather than
		// going unmatched, the STFT reading from that cycle reuses the
		// nearest surviving LTFT value (still within pairTolerance) —
		// see pairSeries' doc comment on why reuse beats a strict,
		// no-reuse 1:1 assignment here.
		readings := []obd2.Reading{
			reading(shortTermFuelTrimBank1, 1, t0),
			reading(longTermFuelTrimBank1, 10, t0.Add(50*time.Millisecond)),
			reading(shortTermFuelTrimBank1, 2, t0.Add(2*time.Second)), // LTFT for this cycle missing
			reading(shortTermFuelTrimBank1, 3, t0.Add(4*time.Second)),
			reading(longTermFuelTrimBank1, 30, t0.Add(4050*time.Millisecond)),
		}
		times, as, bs := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 3 {
			t.Fatalf("got %d pairs, want 3: as=%v bs=%v", len(times), as, bs)
		}
		if as[0] != 1 || bs[0] != 10 || as[1] != 2 || bs[1] != 10 || as[2] != 3 || bs[2] != 30 {
			t.Errorf("pairs = as=%v bs=%v, want as=[1 2 3] bs=[10 10 30] (STFT#2 reuses LTFT#1, its nearest surviving match)", as, bs)
		}
	})

	t.Run("b series longer than a series", func(t *testing.T) {
		readings := []obd2.Reading{
			reading(shortTermFuelTrimBank1, 1, t0),
			reading(longTermFuelTrimBank1, 2, t0.Add(50*time.Millisecond)),
			reading(longTermFuelTrimBank1, 3, t0.Add(2050*time.Millisecond)),
		}
		times, as, bs := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1)
		if len(times) != 1 || as[0] != 1 || bs[0] != 2 {
			t.Errorf("got times=%v as=%v bs=%v, want exactly one pair (1, 2)", times, as, bs)
		}
	})
}

func TestEvaluateSkipsMetricsWithNoData(t *testing.T) {
	if got := Evaluate(nil); len(got) != 0 {
		t.Errorf("Evaluate(nil) = %v, want no anomalies evaluated at all", got)
	}
}

func TestEvaluateCoolantOverheat(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(coolantTemp, 112.5, t0),
		reading(runTimeSinceStart, 700, t0),
	}

	got := Evaluate(readings)
	found := false
	for _, a := range got {
		if a.Metric == "Coolant Temperature" {
			found = true
			if a.Level != trend.LevelCritical {
				t.Errorf("coolant anomaly level = %v, want LevelCritical", a.Level)
			}
		}
	}
	if !found {
		t.Errorf("Evaluate() = %+v, want a Coolant Temperature result", got)
	}
}

func TestEvaluateBatteryLowVoltageUsesPairedRPM(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(engineRPM, 800, t0.Add(-10*time.Second)),
		reading(controlModuleVoltage, 14.0, t0.Add(-10*time.Second)),
		reading(engineRPM, 800, t0),
		reading(controlModuleVoltage, 11.5, t0),
	}

	got := Evaluate(readings)
	var batteryAnomaly *trend.Anomaly
	for i := range got {
		if got[i].Metric == "Control Module Voltage" {
			batteryAnomaly = &got[i]
		}
	}
	if batteryAnomaly == nil {
		t.Fatalf("Evaluate() = %+v, want a Control Module Voltage result", got)
	}
	if batteryAnomaly.Level != trend.LevelCritical {
		t.Errorf("battery anomaly level = %v, want LevelCritical (rpm=800 > 400 for 10s, volts=11.5 < 12.0)", batteryAnomaly.Level)
	}
}

// TestEvaluateBatteryVoltageIgnoresRestartTransient guards against the
// false "alternator failed" alert real fleet data exposed: this
// vehicle's auto idle-stop drops RPM to 0 at every stop, and control
// module voltage sags well below 12V for a couple of seconds after each
// restart while the alternator catches up. Before the fix, Evaluate()
// paired whichever RPM reading happened to be independently "latest" in
// the whole day's data with whichever voltage reading was independently
// "latest" — not matched by timestamp — so a mismatched pairing exactly
// like this one fired a false CRITICAL. See docs/defects.md.
func TestEvaluateBatteryVoltageIgnoresRestartTransient(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(engineRPM, 0, t0), // stopped (idle-stop)
		reading(controlModuleVoltage, 14.0, t0),
		reading(engineRPM, 900, t0.Add(3*time.Second)),              // just restarted
		reading(controlModuleVoltage, 10.82, t0.Add(3*time.Second)), // cranking sag, still recovering
	}

	got := Evaluate(readings)
	for _, a := range got {
		if a.Metric == "Control Module Voltage" && a.Level != trend.LevelNormal {
			t.Errorf("battery anomaly level = %v, want LevelNormal (only 3s since restart, below settle window): %+v", a.Level, a)
		}
	}
}

func TestEvaluateFuelTrimsPairedAcrossReadings(t *testing.T) {
	t0 := time.Now()
	readings := []obd2.Reading{
		reading(shortTermFuelTrimBank1, 10.0, t0),
		reading(longTermFuelTrimBank1, 16.0, t0.Add(50*time.Millisecond)),
	}

	got := Evaluate(readings)
	found := false
	for _, a := range got {
		if a.Metric == "Fuel Trim" {
			found = true
			if a.Level != trend.LevelCritical {
				t.Errorf("fuel trim anomaly level = %v, want LevelCritical (total=26%%)", a.Level)
			}
		}
	}
	if !found {
		t.Errorf("Evaluate() = %+v, want a Fuel Trim result", got)
	}
}

func TestEvaluateCatalyticConverterUsesLatestRPMAndCoolant(t *testing.T) {
	t0 := time.Now()
	var readings []obd2.Reading
	readings = append(readings, reading(engineRPM, 1200, t0))
	readings = append(readings, reading(coolantTemp, 90, t0))

	// 10 closely-correlated pairs, ~1s apart, well within pairTolerance
	// and within the 60s window CheckCatalyticConverter itself applies.
	s1 := []float64{0.1, 0.8, 0.2, 0.9, 0.1, 0.8, 0.2, 0.9, 0.1, 0.8}
	s2 := []float64{0.15, 0.75, 0.25, 0.85, 0.15, 0.75, 0.25, 0.85, 0.15, 0.75}
	for i := 0; i < 10; i++ {
		ts := t0.Add(time.Duration(i) * time.Second)
		readings = append(readings, reading(o2SensorBank1Sensor1, s1[i], ts))
		readings = append(readings, reading(o2SensorBank1Sensor2, s2[i], ts.Add(50*time.Millisecond)))
	}

	got := Evaluate(readings)
	found := false
	for _, a := range got {
		if a.Metric == "Catalytic Converter" {
			found = true
			if a.Level != trend.LevelWarning {
				t.Errorf("catalytic converter anomaly level = %v, want LevelWarning", a.Level)
			}
		}
	}
	if !found {
		t.Errorf("Evaluate() = %+v, want a Catalytic Converter result", got)
	}
}
