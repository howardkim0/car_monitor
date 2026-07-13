# Plan: Trend Detection for OBD2 Logs

> Resolved implementation plan for trend detection.
> Saved here per the standing convention in `CLAUDE.md` ("Planning docs are saved to docs/").

This document plans a trend detection system for `car_monitor` OBD2 logs based on the available PID columns, researches metrics indicating potential vehicle issues, and formalizes these checks into Go functions.

---

## 1. Research: OBD2 Metrics & Problem Indicators

Based on the available PIDs defined in [vehicle.go](file:///mnt/c/Users/a/dev/car_monitor/go/internal/vehicle/vehicle.go), we can monitor several critical automotive subsystems. Below is the research on key parameters, their normal ranges, and anomaly indicators.

### A. Engine Coolant Temperature (ECT) & Engine Oil Temperature (EOT)
- **Normal Range**: 
  - Coolant: 80°C to 100°C (176°F to 212°F) under normal operating load.
  - Oil: 90°C to 110°C (194°F to 230°F).
- **Single-Value Thresholds**:
  - **Coolant Warning**: > 102°C (215°F) indicates running hot.
  - **Coolant Critical (Overheating)**: > 110°C (230°F) is dangerous; risks cylinder head warping or head gasket failure.
  - **Thermostat Stuck Open**: Coolant stays < 70°C (158°F) even after 10+ minutes of engine operation (Run Time > 600s).
  - **Oil Critical**: > 125°C (257°F) indicates oil is breaking down.
- **Trend/Array Indicators**:
  - **Rapid Heat Spikes**: A rate of coolant temperature increase exceeding **0.3°C per second** (or > 9°C in 30 seconds) while driving or idling indicates a sudden coolant loss, radiator fan failure, or water pump failure.

### B. Control Module Voltage (Battery/Alternator)
- **Normal Range**:
  - Engine Running (RPM > 400): 13.5V to 15.0V (active charging).
  - Engine Off (RPM == 0): 12.2V to 12.8V (resting battery charge).
- **Single-Value Thresholds** (when engine is running):
  - **Charging System Warning**: < 13.0V indicates low alternator output.
  - **Charging System Critical**: < 12.0V means the alternator has failed, and the engine is running purely off battery power, which will soon deplete.
  - **Overcharging Warning**: > 15.2V.
  - **Overcharging Critical**: > 16.0V risks damaging sensitive vehicle ECUs.
- **Trend/Array Indicators**:
  - **Voltage Decay**: If voltage steadily declines over a 2-minute sliding window (slope is negative and decreases by > 1.0V), it indicates alternator decay or excessive electrical load exceeding charging capacity.

### C. Fuel Trims (Short-Term & Long-Term Fuel Trim)
- **Normal Range**: ±5% (closer to 0% means the ECU is making minimal corrections to match the target air-fuel ratio).
- **Single-Value Thresholds**:
  - **Total Fuel Trim (TFT)** = `STFT + LTFT`.
  - **Fuel Trim Warning**: `|TFT| > 15%` (ECU is adding or removing significant fuel to compensate).
  - **Fuel Trim Critical (Lean/Rich Anomaly)**: `|TFT| > 25%` (will trigger DTCs P0171/P0174 for Lean or P0172/P0175 for Rich soon).
- **Trend/Array Indicators**:
  - **Vacuum Leak Drift**: If LTFT shows a positive slope (gradually rising by > 5% over a 5-minute window) while engine load is low (idling), it indicates a vacuum leak (unmetered air entering the engine).

### D. Upstream & Downstream O2 Sensor Correlation
- **Normal Behavior**:
  - **Upstream (Sensor 1)**: Oscillates rapidly between 0.1V and 0.9V as the ECU adjusts the air-fuel mixture (closed loop).
  - **Downstream (Sensor 2)**: Stays relatively flat and stable (0.45V to 0.7V) because the catalytic converter consumes oxygen.
- **Trend/Array Indicators**:
  - **Catalytic Converter Failure (P0420)**: If the downstream sensor (Sensor 2) mimics the upstream sensor (Sensor 1), oscillating in sync, the catalytic converter is failing.
  - **Detection Logic**: Calculate the Pearson correlation coefficient ($R$) between Sensor 1 and Sensor 2 voltage arrays over a 60-second window. If $R > 0.8$ and the standard deviation of Sensor 2 is high (> 0.15V), the cat is failing.

---

## 2. API Design: `internal/trend` Package

We will create a new package `go/internal/trend` containing trend detection and linear regression helpers.

### Data Structures

```go
package trend

import "time"

type IssueLevel string

const (
	LevelNormal   IssueLevel = "NORMAL"
	LevelWarning  IssueLevel = "WARNING"
	LevelCritical IssueLevel = "CRITICAL"
)

type Anomaly struct {
	Metric      string     `json:"metric"`
	Level       IssueLevel `json:"level"`
	Message     string     `json:"message"`
	Timestamp   time.Time  `json:"timestamp"`
}
```

### Signature of Check Functions

We will write functions to check:
1. **Coolant Temperature**:
   `func CheckCoolantTemp(timestamps []time.Time, temps []float64, runTimeSec float64) Anomaly`
2. **Battery Voltage**:
   `func CheckBatteryVoltage(timestamps []time.Time, voltages []float64, rpm float64) Anomaly`
3. **Fuel Trims**:
   `func CheckFuelTrims(stft, ltft []float64) Anomaly`
4. **O2 Sensor Correlation**:
   `func CheckCatalyticConverter(s1Voltages, s2Voltages []float64, rpm, coolantTemp float64) Anomaly`

### Mathematical Utilities
- **`CalculateSlope(times []time.Time, values []float64) (float64, float64, error)`**: Uses ordinary least squares (OLS) linear regression to find the rate of change per second and $R^2$.
- **`CalculatePearsonCorrelation(xs, ys []float64) (float64, error)`**: Measures correlation between two variables.
- **`CalculateStdDev(values []float64) float64`**: Calculates standard deviation.

---

## 3. Implementation Steps

1. **Create Directory**: `go/internal/trend`
2. **Write Source File**: `go/internal/trend/trend.go` containing all structs, mathematical helpers, and check functions.
3. **Write Unit Tests**: `go/internal/trend/trend_test.go` covering 100% statement coverage (enforced by the pre-commit hook).
4. **Run Go Tests**: Ensure `go test ./...` passes.

---

## 4. Testing & Verification

To meet the 100% coverage requirement:
- Test linear regression slope with positive, negative, and zero slope inputs.
- Test coolant check with:
  - Normal temperatures.
  - Critical overheat (> 110°C).
  - Rapid temperature rise (positive slope > 0.3°C/s).
  - Thermostat failure (cold temperature after long runtime).
- Test battery check with:
  - Alternator working fine.
  - Alternator critical low voltage (< 12.0V).
  - Voltage decay trend (negative slope).
- Test fuel trim checks (normal, rich anomaly, lean anomaly).
- Test O2 sensor correlation checks (uncorrelated vs highly correlated/failed cat).
