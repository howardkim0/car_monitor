// Package trend implements trend detection algorithms for vehicle OBD2 readings
// to detect potential faults or degradation before they trigger a DTC.
package trend

import (
	"fmt"
	"math"
	"time"
)

// IssueLevel describes the severity of a detected anomaly.
type IssueLevel string

const (
	LevelNormal   IssueLevel = "NORMAL"
	LevelWarning  IssueLevel = "WARNING"
	LevelCritical IssueLevel = "CRITICAL"
)

// Anomaly holds the result of a trend check, detailing the metric,
// severity, explanation, and detection timestamp.
type Anomaly struct {
	Metric    string     `json:"metric"`
	Level     IssueLevel `json:"level"`
	Message   string     `json:"message"`
	Timestamp time.Time  `json:"timestamp"`
}

// CalculateSlope uses ordinary least squares (OLS) linear regression to calculate
// the slope (rate of change per second) of a series of value points.
// It also returns R-squared (coefficient of determination) to indicate trend strength.
func CalculateSlope(times []time.Time, values []float64) (slope float64, rSquared float64, err error) {
	n := len(times)
	if n != len(values) {
		return 0, 0, fmt.Errorf("length mismatch: times=%d, values=%d", len(times), len(values))
	}
	if n < 2 {
		return 0, 0, fmt.Errorf("need at least 2 points for slope calculation, got %d", n)
	}

	var sumX, sumY, sumXX, sumYY, sumXY float64
	t0 := times[0]

	for i := 0; i < n; i++ {
		x := times[i].Sub(t0).Seconds()
		y := values[i]

		sumX += x
		sumY += y
		sumXX += x * x
		sumYY += y * y
		sumXY += x * y
	}

	num := float64(n)*sumXY - sumX*sumY
	den := float64(n)*sumXX - sumX*sumX

	if den == 0 {
		return 0, 0, fmt.Errorf("all timestamps are identical, cannot calculate slope")
	}

	slope = num / den

	denR := (float64(n)*sumXX - sumX*sumX) * (float64(n)*sumYY - sumY*sumY)
	if denR <= 0 {
		rSquared = 1.0
	} else {
		r := num / math.Sqrt(denR)
		rSquared = r * r
	}

	return slope, rSquared, nil
}

// CalculatePearsonCorrelation computes the Pearson correlation coefficient between two slices.
func CalculatePearsonCorrelation(xs, ys []float64) (float64, error) {
	n := len(xs)
	if n != len(ys) {
		return 0, fmt.Errorf("length mismatch: xs=%d, ys=%d", len(xs), len(ys))
	}
	if n < 2 {
		return 0, fmt.Errorf("need at least 2 points for correlation, got %d", n)
	}

	var sumX, sumY, sumXX, sumYY, sumXY float64
	for i := 0; i < n; i++ {
		x := xs[i]
		y := ys[i]
		sumX += x
		sumY += y
		sumXX += x * x
		sumYY += y * y
		sumXY += x * y
	}

	num := float64(n)*sumXY - sumX*sumY
	den := math.Sqrt((float64(n)*sumXX - sumX*sumX) * (float64(n)*sumYY - sumY*sumY))
	if den == 0 {
		return 0, nil
	}

	return num / den, nil
}

// CalculateStdDev computes the standard deviation of a slice of values.
func CalculateStdDev(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0.0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)

	var varianceSum float64
	for _, v := range values {
		diff := v - mean
		varianceSum += diff * diff
	}

	return math.Sqrt(varianceSum / float64(n-1))
}

// CheckCoolantTemp evaluates the engine coolant temperature based on the current value and trend.
func CheckCoolantTemp(times []time.Time, temps []float64, runTimeSec float64) Anomaly {
	n := len(temps)
	if n == 0 {
		return Anomaly{Metric: "Coolant Temperature", Level: LevelNormal, Message: "No data", Timestamp: time.Now().UTC()}
	}

	latestTemp := temps[n-1]
	now := time.Now().UTC()
	if n == len(times) && n > 0 {
		now = times[n-1]
	}

	// Single value checks
	if latestTemp > 110.0 {
		return Anomaly{
			Metric:    "Coolant Temperature",
			Level:     LevelCritical,
			Message:   fmt.Sprintf("Coolant temperature is critically high (overheating): %.1f°C", latestTemp),
			Timestamp: now,
		}
	}
	if latestTemp > 102.0 {
		return Anomaly{
			Metric:    "Coolant Temperature",
			Level:     LevelWarning,
			Message:   fmt.Sprintf("Coolant temperature is high: %.1f°C", latestTemp),
			Timestamp: now,
		}
	}
	if runTimeSec > 600.0 && latestTemp < 70.0 {
		return Anomaly{
			Metric:    "Coolant Temperature",
			Level:     LevelWarning,
			Message:   fmt.Sprintf("Coolant temperature is low after running: %.1f°C (thermostat may be stuck open)", latestTemp),
			Timestamp: now,
		}
	}

	// Trend check: rapid temperature rise
	if n >= 2 && len(times) == n {
		// Only look at the last 30 seconds of data for rapid spike
		windowSize := 0
		cutoffTime := times[n-1].Add(-30 * time.Second)
		for i := n - 1; i >= 0; i-- {
			if times[i].After(cutoffTime) || times[i].Equal(cutoffTime) {
				windowSize++
			} else {
				break
			}
		}

		if windowSize >= 2 {
			subTimes := times[n-windowSize:]
			subTemps := temps[n-windowSize:]
			slope, rSquared, err := CalculateSlope(subTimes, subTemps)
			// slope is °C per second. slope > 0.3 means a rise of > 9°C in 30s.
			if err == nil && slope > 0.3 && rSquared > 0.7 {
				return Anomaly{
					Metric:    "Coolant Temperature",
					Level:     LevelWarning,
					Message:   fmt.Sprintf("Coolant temperature is rising rapidly: +%.3f°C/s", slope),
					Timestamp: now,
				}
			}
		}
	}

	return Anomaly{
		Metric:    "Coolant Temperature",
		Level:     LevelNormal,
		Message:   fmt.Sprintf("Coolant temperature is normal: %.1f°C", latestTemp),
		Timestamp: now,
	}
}

// runningRPMThreshold gates every check below on the engine actually
// running: below this, the alternator isn't spinning fast enough to
// charge at all, so a low reading is expected, not a fault.
const runningRPMThreshold = 400.0

// voltageSettleSeconds bounds how long a low (or high) voltage snapshot
// is trusted as a real fault vs. ordinary post-crank alternator recovery
// lag. Real fleet data (see docs/defects.md) shows control module
// voltage sagging as low as 10.8V for 2-3 seconds after every auto
// idle-stop restart, before the alternator catches up — a single
// snapshot taken in that window looks identical to genuine alternator
// failure by threshold alone. Requiring the engine to have been
// continuously running for at least this long survives every observed
// restart transient (longest observed recovery was ~3s) while still
// catching a real failure within the next few poll cycles.
const voltageSettleSeconds = 8.0

// CheckBatteryVoltage evaluates control module voltage under charging
// conditions. rpms is each voltage reading's paired engine RPM value at
// (approximately) the same timestamp — not a single latest RPM value.
// A vehicle with auto idle-stop drops RPM to 0 at every stop, and RPM
// and voltage are never sampled at exactly the same instant; gating on
// an RPM value computed independently of which instant a given voltage
// reading came from is exactly how a normal restart's cranking sag gets
// mistaken for alternator failure. Callers should pair the two series by
// nearest timestamp (see monitor.pairSeries) before calling this.
func CheckBatteryVoltage(times []time.Time, voltages []float64, rpms []float64) Anomaly {
	n := len(voltages)
	if n == 0 || len(rpms) != n || len(times) != n {
		return Anomaly{Metric: "Control Module Voltage", Level: LevelNormal, Message: "No data or mismatched RPM pairing", Timestamp: time.Now().UTC()}
	}

	latestVolt := voltages[n-1]
	rpm := rpms[n-1]
	now := times[n-1]

	// Single value checks only apply when engine is running (RPM > 400)
	if rpm > runningRPMThreshold {
		// How long the engine has been continuously running as of now:
		// walk back to the last time RPM was at/below the running
		// threshold (a stop, whether ignition-off or auto idle-stop) —
		// or, if it's been running for the whole series, to its start.
		secondsRunning := now.Sub(times[0]).Seconds()
		for i := n - 2; i >= 0; i-- {
			if rpms[i] <= runningRPMThreshold {
				secondsRunning = now.Sub(times[i]).Seconds()
				break
			}
		}

		if secondsRunning >= voltageSettleSeconds {
			if latestVolt < 12.0 {
				return Anomaly{
					Metric:    "Control Module Voltage",
					Level:     LevelCritical,
					Message:   fmt.Sprintf("Battery voltage is critically low while running: %.2fV (alternator failed)", latestVolt),
					Timestamp: now,
				}
			}
			if latestVolt < 13.0 {
				return Anomaly{
					Metric:    "Control Module Voltage",
					Level:     LevelWarning,
					Message:   fmt.Sprintf("Battery voltage is low while running: %.2fV", latestVolt),
					Timestamp: now,
				}
			}
			if latestVolt > 16.0 {
				return Anomaly{
					Metric:    "Control Module Voltage",
					Level:     LevelCritical,
					Message:   fmt.Sprintf("Battery voltage is critically high: %.2fV (regulator failure)", latestVolt),
					Timestamp: now,
				}
			}
			if latestVolt > 15.2 {
				return Anomaly{
					Metric:    "Control Module Voltage",
					Level:     LevelWarning,
					Message:   fmt.Sprintf("Battery voltage is high: %.2fV", latestVolt),
					Timestamp: now,
				}
			}
		}

		// Trend check: voltage decay while engine is running
		if n >= 2 {
			// Look at last 2 minutes (120 seconds)
			windowSize := 0
			cutoffTime := times[n-1].Add(-120 * time.Second)
			for i := n - 1; i >= 0; i-- {
				if times[i].After(cutoffTime) || times[i].Equal(cutoffTime) {
					windowSize++
				} else {
					break
				}
			}

			if windowSize >= 5 {
				subTimes := times[n-windowSize:]
				subVolts := voltages[n-windowSize:]
				slope, rSquared, err := CalculateSlope(subTimes, subVolts)
				// slope < -0.008 means a drop of > 1V over 2 minutes (120s)
				if err == nil && slope < -0.008 && rSquared > 0.7 && latestVolt < 13.2 {
					return Anomaly{
						Metric:    "Control Module Voltage",
						Level:     LevelWarning,
						Message:   fmt.Sprintf("Battery voltage is steadily decaying: %.3fV/s", slope),
						Timestamp: now,
					}
				}
			}
		}
	}

	return Anomaly{
		Metric:    "Control Module Voltage",
		Level:     LevelNormal,
		Message:   fmt.Sprintf("Battery voltage is normal: %.2fV", latestVolt),
		Timestamp: now,
	}
}

// CheckFuelTrims evaluates fuel system balance using short-term and long-term trims.
func CheckFuelTrims(times []time.Time, stft, ltft []float64) Anomaly {
	n := len(stft)
	if n == 0 || len(ltft) != n {
		return Anomaly{Metric: "Fuel Trim", Level: LevelNormal, Message: "No data or mismatched trims", Timestamp: time.Now().UTC()}
	}

	latestST := stft[n-1]
	latestLT := ltft[n-1]
	totalTrim := latestST + latestLT
	now := time.Now().UTC()
	if n == len(times) && n > 0 {
		now = times[n-1]
	}

	// Single value checks
	if totalTrim > 25.0 {
		return Anomaly{
			Metric:    "Fuel Trim",
			Level:     LevelCritical,
			Message:   fmt.Sprintf("Total fuel trim is critically lean (adding fuel): %.1f%%", totalTrim),
			Timestamp: now,
		}
	}
	if totalTrim < -25.0 {
		return Anomaly{
			Metric:    "Fuel Trim",
			Level:     LevelCritical,
			Message:   fmt.Sprintf("Total fuel trim is critically rich (removing fuel): %.1f%%", totalTrim),
			Timestamp: now,
		}
	}
	if totalTrim > 15.0 {
		return Anomaly{
			Metric:    "Fuel Trim",
			Level:     LevelWarning,
			Message:   fmt.Sprintf("Total fuel trim is lean: %.1f%%", totalTrim),
			Timestamp: now,
		}
	}
	if totalTrim < -15.0 {
		return Anomaly{
			Metric:    "Fuel Trim",
			Level:     LevelWarning,
			Message:   fmt.Sprintf("Total fuel trim is rich: %.1f%%", totalTrim),
			Timestamp: now,
		}
	}

	// Trend check: long-term fuel trim drift over a 5-minute window
	if n >= 5 && len(times) == n {
		windowSize := 0
		cutoffTime := times[n-1].Add(-300 * time.Second)
		for i := n - 1; i >= 0; i-- {
			if times[i].After(cutoffTime) || times[i].Equal(cutoffTime) {
				windowSize++
			} else {
				break
			}
		}

		if windowSize >= 5 {
			subTimes := times[n-windowSize:]
			subLTFT := ltft[n-windowSize:]
			slope, rSquared, err := CalculateSlope(subTimes, subLTFT)
			// slope > 0.04 is +2.4% trim per minute.
			if err == nil && rSquared > 0.7 {
				if slope > 0.04 && latestLT > 10.0 {
					return Anomaly{
						Metric:    "Fuel Trim",
						Level:     LevelWarning,
						Message:   fmt.Sprintf("Long term fuel trim is drifting lean: +%.3f%%/s", slope),
						Timestamp: now,
					}
				}
				if slope < -0.04 && latestLT < -10.0 {
					return Anomaly{
						Metric:    "Fuel Trim",
						Level:     LevelWarning,
						Message:   fmt.Sprintf("Long term fuel trim is drifting rich: %.3f%%/s", slope),
						Timestamp: now,
					}
				}
			}
		}
	}

	return Anomaly{
		Metric:    "Fuel Trim",
		Level:     LevelNormal,
		Message:   fmt.Sprintf("Fuel trims are normal (Total: %.1f%%)", totalTrim),
		Timestamp: now,
	}
}

// CheckCatalyticConverter evaluates catalytic converter efficiency using upstream (Sensor 1)
// and downstream (Sensor 2) O2 sensor voltages over a 60-second window.
func CheckCatalyticConverter(times []time.Time, s1Voltages, s2Voltages []float64, rpm, coolantTemp float64) Anomaly {
	n := len(s1Voltages)
	if n == 0 || len(s2Voltages) != n || len(times) != n {
		return Anomaly{Metric: "Catalytic Converter", Level: LevelNormal, Message: "No data or mismatched O2 sensors", Timestamp: time.Now().UTC()}
	}

	now := times[n-1]

	// Must be warmed up and running under load
	if coolantTemp < 80.0 || rpm < 1000.0 {
		return Anomaly{
			Metric:    "Catalytic Converter",
			Level:     LevelNormal,
			Message:   "Vehicle not in conditions to test catalytic converter (needs warm coolant & load)",
			Timestamp: now,
		}
	}

	// Extract window of the last 60 seconds of data
	windowSize := 0
	cutoffTime := now.Add(-60 * time.Second)
	for i := n - 1; i >= 0; i-- {
		if times[i].After(cutoffTime) || times[i].Equal(cutoffTime) {
			windowSize++
		} else {
			break
		}
	}

	// We need a decent window of readings (e.g., at least 10) within the last 60s to compute reliable correlation.
	if windowSize >= 10 {
		subS1 := s1Voltages[n-windowSize:]
		subS2 := s2Voltages[n-windowSize:]

		corr, err := CalculatePearsonCorrelation(subS1, subS2)
		s2Std := CalculateStdDev(subS2)

		if err == nil && corr > 0.8 && s2Std > 0.15 {
			return Anomaly{
				Metric:    "Catalytic Converter",
				Level:     LevelWarning,
				Message:   fmt.Sprintf("Downstream O2 sensor highly correlates with upstream (degradation suspected): R=%.2f, StdDev=%.3fV", corr, s2Std),
				Timestamp: now,
			}
		}
	}

	return Anomaly{
		Metric:    "Catalytic Converter",
		Level:     LevelNormal,
		Message:   "Catalytic converter efficiency is normal",
		Timestamp: now,
	}
}
