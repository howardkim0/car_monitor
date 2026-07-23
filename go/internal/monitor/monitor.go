// Package monitor turns a day's decoded OBD2 readings into the trend
// results worth reasoning about — the glue between the flat, per-PID rows
// internal/storage persists and internal/trend's per-metric check
// functions, which each need a matched time series for one metric (or a
// matched pair of metrics), not a flat list of individual PID readings in
// arrival order.
//
// Deliberately has no I/O of its own — dir reading is internal/storage's
// job (see storage.LoadReadings) — so this stays as easy to unit test as
// internal/trend itself, and mobile.Session is the only thing that wires
// the two together.
package monitor

import (
	"time"

	"github.com/howardkim0/car_monitor/go/internal/obd2"
	"github.com/howardkim0/car_monitor/go/internal/trend"
)

// Metric names, matching vehicle.go's PID.Name fields exactly — see
// monitor_test.go's TestMetricNamesMatchVehicleProfile, which guards
// against these silently drifting apart from the PID list a future
// change to vehicle.go might rename.
const (
	coolantTemp            = "Coolant Temperature"
	controlModuleVoltage   = "Control Module Voltage"
	engineRPM              = "Engine RPM"
	runTimeSinceStart      = "Run Time Since Engine Start"
	shortTermFuelTrimBank1 = "Short Term Fuel Trim Bank 1"
	longTermFuelTrimBank1  = "Long Term Fuel Trim Bank 1"
	o2SensorBank1Sensor1   = "O2 Sensor Bank 1 Sensor 1"
	o2SensorBank1Sensor2   = "O2 Sensor Bank 1 Sensor 2"
)

// pairTolerance is how close in time two different-PID readings must be
// to count as belonging to the same poll cycle (e.g. a Short Term + Long
// Term Fuel Trim pair, or the two O2 sensor voltages) — two different
// PIDs are never read at exactly the same instant, so an exact-timestamp
// join would never match anything; a nearest-timestamp join needs a
// tolerance instead. Even at today's ~32-PID vehicle profile, a full
// poll cycle (COMMAND_INTERVAL_MS * PID count + POLL_CYCLE_MS, in
// ObdForegroundService) comfortably finishes within a couple of seconds,
// so 5s survives an occasional single missing/dropped response (the
// next cycle's reading is still well within tolerance) without being
// wide enough to accidentally pair values from genuinely different
// cycles together.
const pairTolerance = 5 * time.Second

// Evaluate groups readings by metric name and runs every trend check
// that has data for it, returning one trend.Anomaly per metric that was
// evaluated — at whatever level trend.Check* itself returned, including
// LevelNormal. Filtering for "worth telling a user about" is the
// caller's job (see mobile.Session.CheckAnomalies): a LevelNormal result
// still matters there, since it's what lets a caller notice a metric has
// recovered and reset its own "already told the user about this" state.
func Evaluate(readings []obd2.Reading) []trend.Anomaly {
	var anomalies []trend.Anomaly

	// Computed once and reused below rather than re-scanning readings per
	// check: rpm gates the catalytic converter check below, and
	// latestCoolant is just coolantTemps' last element (series() already
	// collects it in chronological order), so there's no need for a
	// second, separate latest() scan for it. Battery voltage does NOT
	// reuse this scalar — see the pairSeries call below and
	// trend.CheckBatteryVoltage's doc comment for why.
	rpm := latest(readings, engineRPM)

	coolantTimes, coolantTemps := series(readings, coolantTemp)
	var latestCoolant float64
	if n := len(coolantTemps); n > 0 {
		latestCoolant = coolantTemps[n-1]
		runTime := latest(readings, runTimeSinceStart)
		anomalies = append(anomalies, trend.CheckCoolantTemp(coolantTimes, coolantTemps, runTime))
	}

	// Paired by nearest timestamp, not each independently "latest" —
	// a vehicle with auto idle-stop drops RPM to 0 at every stop, so an
	// RPM value from a different instant than the voltage reading it
	// gates can make a normal restart's cranking sag look like alternator
	// failure. See docs/defects.md and trend.CheckBatteryVoltage.
	if times, volts, rpms := pairSeries(readings, controlModuleVoltage, engineRPM); len(volts) > 0 {
		anomalies = append(anomalies, trend.CheckBatteryVoltage(times, volts, rpms))
	}

	if times, stft, ltft := pairSeries(readings, shortTermFuelTrimBank1, longTermFuelTrimBank1); len(times) > 0 {
		anomalies = append(anomalies, trend.CheckFuelTrims(times, stft, ltft))
	}

	if times, s1, s2 := pairSeries(readings, o2SensorBank1Sensor1, o2SensorBank1Sensor2); len(times) > 0 {
		anomalies = append(anomalies, trend.CheckCatalyticConverter(times, s1, s2, rpm, latestCoolant))
	}

	return anomalies
}

// series extracts every reading named name, in the order they appear —
// already chronological, since callers pass an append-ordered slice
// (storage.LoadReadings reads the CSV back in the order it was written)
// — as parallel times/values slices.
func series(readings []obd2.Reading, name string) ([]time.Time, []float64) {
	var times []time.Time
	var values []float64
	for _, r := range readings {
		if r.Name == name {
			times = append(times, r.Timestamp)
			values = append(values, r.Value)
		}
	}
	return times, values
}

// latest returns the most recent value for name, or 0 if there isn't
// one. 0 is a safe default everywhere it's used: Evaluate only ever
// feeds it to gating comparisons in internal/trend (e.g. "rpm > 400"),
// never a value whose meaning changes specifically at 0.
func latest(readings []obd2.Reading, name string) float64 {
	for i := len(readings) - 1; i >= 0; i-- {
		if readings[i].Name == name {
			return readings[i].Value
		}
	}
	return 0
}

// pairSeries matches each aName reading to the closest-in-time bName
// reading, discarding any that don't have a match within pairTolerance.
// Both series are already sorted ascending (see series), which is what
// makes the single forward-only pass below correct: as target time
// increases monotonically, the nearest match in the other series can
// only ever move forward too, never backward.
//
// A single bName reading can be reused across more than one aName
// reading (e.g. if a bName response is dropped for one poll cycle, its
// aName partner just reuses the nearest surviving bName value instead of
// going unmatched) — deliberate, not a bug: rejecting reuse and forcing
// a strictly-advancing 1:1 assignment sounds more "correct" but isn't —
// it can make an early aName reading steal the genuinely-closest bName
// match away from a later one that needed it more, which is worse than
// occasional reuse of a still-recent, still-representative value.
func pairSeries(readings []obd2.Reading, aName, bName string) ([]time.Time, []float64, []float64) {
	aTimes, aVals := series(readings, aName)
	bTimes, bVals := series(readings, bName)

	var times []time.Time
	var as, bs []float64

	j := 0
	for i, at := range aTimes {
		if len(bTimes) == 0 {
			break
		}
		for j+1 < len(bTimes) && closer(bTimes[j+1], bTimes[j], at) {
			j++
		}
		if absDuration(bTimes[j].Sub(at)) <= pairTolerance {
			times = append(times, at)
			as = append(as, aVals[i])
			bs = append(bs, bVals[j])
		}
	}
	return times, as, bs
}

// closer reports whether candidate is at least as close to target as
// current is.
func closer(candidate, current, target time.Time) bool {
	return absDuration(candidate.Sub(target)) <= absDuration(current.Sub(target))
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
