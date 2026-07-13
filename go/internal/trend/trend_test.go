package trend

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestCalculateSlope(t *testing.T) {
	t0 := time.Now()

	// Error: length mismatch
	if _, _, err := CalculateSlope([]time.Time{t0}, []float64{1, 2}); err == nil {
		t.Error("CalculateSlope with mismatched lengths should have failed")
	}

	// Error: n < 2
	if _, _, err := CalculateSlope([]time.Time{t0}, []float64{1}); err == nil {
		t.Error("CalculateSlope with 1 point should have failed")
	}

	// Error: identical timestamps
	if _, _, err := CalculateSlope([]time.Time{t0, t0}, []float64{1, 2}); err == nil {
		t.Error("CalculateSlope with identical timestamps should have failed")
	}

	// Success: positive slope
	times := []time.Time{t0, t0.Add(1 * time.Second), t0.Add(2 * time.Second)}
	values := []float64{10, 12, 14}
	slope, r2, err := CalculateSlope(times, values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(slope-2.0) > 1e-9 {
		t.Errorf("expected slope 2.0, got %f", slope)
	}
	if math.Abs(r2-1.0) > 1e-9 {
		t.Errorf("expected R2 1.0, got %f", r2)
	}

	// Success: negative slope
	valuesNeg := []float64{10, 8, 6}
	slopeNeg, r2Neg, err := CalculateSlope(times, valuesNeg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(slopeNeg-(-2.0)) > 1e-9 {
		t.Errorf("expected slope -2.0, got %f", slopeNeg)
	}
	if math.Abs(r2Neg-1.0) > 1e-9 {
		t.Errorf("expected R2 1.0, got %f", r2Neg)
	}

	// Success: zero variance in values (flat line)
	valuesFlat := []float64{5, 5, 5}
	slopeFlat, r2Flat, err := CalculateSlope(times, valuesFlat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slopeFlat != 0 {
		t.Errorf("expected slope 0, got %f", slopeFlat)
	}
	if r2Flat != 1.0 {
		t.Errorf("expected R2 1.0 for flat line, got %f", r2Flat)
	}
}

func TestCalculatePearsonCorrelation(t *testing.T) {
	// Error: length mismatch
	if _, err := CalculatePearsonCorrelation([]float64{1}, []float64{1, 2}); err == nil {
		t.Error("CalculatePearsonCorrelation with mismatched lengths should fail")
	}

	// Error: too few points
	if _, err := CalculatePearsonCorrelation([]float64{1}, []float64{2}); err == nil {
		t.Error("CalculatePearsonCorrelation with < 2 points should fail")
	}

	// Success: division by zero (constant values)
	if r, err := CalculatePearsonCorrelation([]float64{1, 1}, []float64{2, 2}); err != nil || r != 0 {
		t.Errorf("expected (0, nil) for constant arrays, got (%f, %v)", r, err)
	}

	// Success: highly correlated
	xs := []float64{1, 2, 3, 4, 5}
	ys := []float64{2, 4, 6, 8, 10}
	r, err := CalculatePearsonCorrelation(xs, ys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(r-1.0) > 1e-9 {
		t.Errorf("expected correlation 1.0, got %f", r)
	}
}

func TestCalculateStdDev(t *testing.T) {
	// Less than 2 points
	if sd := CalculateStdDev([]float64{5}); sd != 0 {
		t.Errorf("expected std dev 0 for 1 point, got %f", sd)
	}

	// Normal calculation
	sd := CalculateStdDev([]float64{2, 4, 4, 4, 5, 5, 7, 9})
	// Mean = 5.0
	// Differences: -3, -1, -1, -1, 0, 0, 2, 4
	// Squared diffs: 9, 1, 1, 1, 0, 0, 4, 16
	// Variance sum = 32
	// Unbiased var = 32 / 7 = 4.571428
	// StdDev = sqrt(4.571428) = 2.138090
	expected := 2.138090
	if math.Abs(sd-expected) > 1e-5 {
		t.Errorf("expected std dev %f, got %f", expected, sd)
	}
}

func TestCheckCoolantTemp(t *testing.T) {
	t0 := time.Now()

	// Empty case
	if a := CheckCoolantTemp(nil, nil, 0); a.Level != LevelNormal || a.Message != "No data" {
		t.Errorf("unexpected empty case: %+v", a)
	}

	// Critical overheat
	if a := CheckCoolantTemp([]time.Time{t0}, []float64{112.5}, 100); a.Level != LevelCritical {
		t.Errorf("expected LevelCritical for 112.5°C, got %+v", a)
	}

	// Warning high
	if a := CheckCoolantTemp([]time.Time{t0}, []float64{104.0}, 100); a.Level != LevelWarning {
		t.Errorf("expected LevelWarning for 104°C, got %+v", a)
	}

	// Stuck thermostat warning
	if a := CheckCoolantTemp([]time.Time{t0}, []float64{65.0}, 700); a.Level != LevelWarning || a.Message == "" {
		t.Errorf("expected thermostat warning, got %+v", a)
	}

	// Normal
	if a := CheckCoolantTemp([]time.Time{t0}, []float64{90.0}, 100); a.Level != LevelNormal {
		t.Errorf("expected normal coolant, got %+v", a)
	}

	// Rapid temp rise
	times := []time.Time{
		t0,
		t0.Add(5 * time.Second),
		t0.Add(10 * time.Second),
		t0.Add(15 * time.Second),
		t0.Add(20 * time.Second),
	}
	temps := []float64{90.0, 92.0, 94.0, 96.0, 98.0} // +0.4°C/s
	if a := CheckCoolantTemp(times, temps, 100); a.Level != LevelWarning {
		t.Errorf("expected warning for rapid coolant temp rise, got %+v", a)
	}

	// No rapid temp rise if time is outside 30s window (or window size too small)
	timesOut := []time.Time{
		t0.Add(-50 * time.Second),
		t0.Add(-40 * time.Second),
		t0.Add(-30 * time.Second),
		t0.Add(-20 * time.Second),
		t0, // only t0 is inside last 30s
	}
	tempsOut := []float64{90.0, 92.0, 94.0, 96.0, 98.0}
	if a := CheckCoolantTemp(timesOut, tempsOut, 100); a.Level != LevelNormal {
		t.Errorf("expected normal (not enough data in 30s window), got %+v", a)
	}
}

func TestCheckBatteryVoltage(t *testing.T) {
	t0 := time.Now()

	// Empty case
	if a := CheckBatteryVoltage(nil, nil, 0); a.Level != LevelNormal || a.Message != "No data" {
		t.Errorf("unexpected empty case: %+v", a)
	}

	// Engine off (RPM = 0) should bypass running checks
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{11.8}, 0); a.Level != LevelNormal {
		t.Errorf("expected LevelNormal for resting battery voltage, got %+v", a)
	}

	// Engine running, critically low voltage
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{11.5}, 800); a.Level != LevelCritical {
		t.Errorf("expected LevelCritical for running low volt, got %+v", a)
	}

	// Engine running, low voltage warning
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{12.5}, 800); a.Level != LevelWarning {
		t.Errorf("expected LevelWarning for running low volt, got %+v", a)
	}

	// Engine running, critically high voltage
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{16.5}, 800); a.Level != LevelCritical {
		t.Errorf("expected LevelCritical for overcharging, got %+v", a)
	}

	// Engine running, high voltage warning
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{15.4}, 800); a.Level != LevelWarning {
		t.Errorf("expected LevelWarning for high volt, got %+v", a)
	}

	// Engine running, normal voltage
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{14.0}, 800); a.Level != LevelNormal {
		t.Errorf("expected LevelNormal, got %+v", a)
	}

	// Steady decay warning
	times := make([]time.Time, 11)
	voltages := make([]float64, 11)
	times[0] = t0
	voltages[0] = 14.0
	tStart := t0.Add(150 * time.Second)
	for i := 1; i < 11; i++ {
		times[i] = tStart.Add(time.Duration(i*10) * time.Second)
		voltages[i] = 14.5 - float64(i)*0.14 // drops to 13.1V at i=10 (delta = 1.4V over 90s)
	}
	if a := CheckBatteryVoltage(times, voltages, 800); a.Level != LevelWarning || !strings.Contains(a.Message, "steadily decaying") {
		t.Errorf("expected decay warning, got %+v", a)
	}

	// Decay warning bypassed if latest voltage is high (e.g. 13.5V still)
	for i := 1; i < 11; i++ {
		voltages[i] = 14.5 - float64(i)*0.09 // drops to 13.6V
	}
	if a := CheckBatteryVoltage(times, voltages, 800); a.Level != LevelNormal {
		t.Errorf("expected normal (latest volt still high enough), got %+v", a)
	}

	// Branch coverage: latestVolt < 13.2 but slope >= -0.008 (stable 13.1V)
	stableVolts := make([]float64, 11)
	for i := 0; i < 11; i++ {
		stableVolts[i] = 13.1
	}
	if a := CheckBatteryVoltage(times, stableVolts, 800); a.Level != LevelNormal {
		t.Errorf("expected normal for stable 13.1V, got %+v", a)
	}

	// Branch coverage: mismatched length between times and voltages (len(times) != len(voltages))
	if a := CheckBatteryVoltage([]time.Time{t0}, []float64{14.0, 14.1}, 800); a.Level != LevelNormal {
		t.Errorf("expected normal (mismatched length bypasses trend), got %+v", a)
	}

	// Branch coverage: windowSize < 5
	shortTimes := []time.Time{t0, t0.Add(5 * time.Second)}
	shortVolts := []float64{14.0, 13.9}
	if a := CheckBatteryVoltage(shortTimes, shortVolts, 800); a.Level != LevelNormal {
		t.Errorf("expected normal (windowSize < 5), got %+v", a)
	}

	// Branch coverage: CalculateSlope error (identical timestamps)
	dupTimes := []time.Time{t0, t0, t0, t0, t0}
	dupVolts := []float64{14.0, 14.0, 14.0, 14.0, 14.0}
	if a := CheckBatteryVoltage(dupTimes, dupVolts, 800); a.Level != LevelNormal {
		t.Errorf("expected normal (slope error), got %+v", a)
	}
}

func TestCheckFuelTrims(t *testing.T) {
	t0 := time.Now()

	// Empty case
	if a := CheckFuelTrims(nil, nil, nil); a.Level != LevelNormal || a.Message != "No data or mismatched trims" {
		t.Errorf("unexpected empty case: %+v", a)
	}

	// Mismatched length case
	if a := CheckFuelTrims(nil, []float64{1.0}, []float64{1.0, 2.0}); a.Level != LevelNormal || a.Message != "No data or mismatched trims" {
		t.Errorf("unexpected mismatched length case: %+v", a)
	}

	// Normal case
	if a := CheckFuelTrims([]time.Time{t0}, []float64{1.5}, []float64{2.0}); a.Level != LevelNormal {
		t.Errorf("expected normal fuel trim, got %+v", a)
	}

	// Critically lean
	if a := CheckFuelTrims([]time.Time{t0}, []float64{10.0}, []float64{16.0}); a.Level != LevelCritical {
		t.Errorf("expected LevelCritical (TFT=26), got %+v", a)
	}

	// Critically rich
	if a := CheckFuelTrims([]time.Time{t0}, []float64{-10.0}, []float64{-16.0}); a.Level != LevelCritical {
		t.Errorf("expected LevelCritical (TFT=-26), got %+v", a)
	}

	// Lean warning
	if a := CheckFuelTrims([]time.Time{t0}, []float64{5.0}, []float64{11.0}); a.Level != LevelWarning {
		t.Errorf("expected LevelWarning (TFT=16), got %+v", a)
	}

	// Rich warning
	if a := CheckFuelTrims([]time.Time{t0}, []float64{-5.0}, []float64{-11.0}); a.Level != LevelWarning {
		t.Errorf("expected LevelWarning (TFT=-16), got %+v", a)
	}

	// Long-term drift lean (keeping totalTrim < 15.0 to trigger trend check instead of single value warning)
	times := make([]time.Time, 11)
	stft := make([]float64, 11)
	ltft := make([]float64, 11)
	times[0] = t0
	stft[0] = 0.0
	ltft[0] = 5.0
	tTrimStart := t0.Add(350 * time.Second)
	for i := 1; i < 11; i++ {
		times[i] = tTrimStart.Add(time.Duration(i*30) * time.Second)
		stft[i] = -11.0                // negative trim offsets positive ltft
		ltft[i] = 5.0 + float64(i)*2.0 // drifts to 25% (latest totalTrim = 14.0%)
	}
	if a := CheckFuelTrims(times, stft, ltft); a.Level != LevelWarning || a.Message == "" {
		t.Errorf("expected drift lean warning, got %+v", a)
	}

	// Long-term drift rich (keeping totalTrim > -15.0 to trigger trend check instead of single value warning)
	for i := 1; i < 11; i++ {
		stft[i] = 11.0                  // positive trim offsets negative ltft
		ltft[i] = -5.0 - float64(i)*2.0 // drifts to -25% (latest totalTrim = -14.0%)
	}
	if a := CheckFuelTrims(times, stft, ltft); a.Level != LevelWarning || a.Message == "" {
		t.Errorf("expected drift rich warning, got %+v", a)
	}

	// Branch coverage: len(times) != len(stft)
	if a := CheckFuelTrims(nil, []float64{1, 2, 3, 4, 5}, []float64{1, 2, 3, 4, 5}); a.Level != LevelNormal {
		t.Errorf("expected normal (mismatched times skips trend check), got %+v", a)
	}

	// Branch coverage: windowSize < 5
	shortTimes := []time.Time{t0, t0.Add(30 * time.Second)}
	shortST := []float64{0.0, 0.0}
	shortLT := []float64{5.0, 6.0}
	if a := CheckFuelTrims(shortTimes, shortST, shortLT); a.Level != LevelNormal {
		t.Errorf("expected normal (windowSize < 5), got %+v", a)
	}

	// Branch coverage: CalculateSlope error (identical timestamps)
	dupTimes := []time.Time{t0, t0, t0, t0, t0}
	dupST := []float64{0, 0, 0, 0, 0}
	dupLT := []float64{5, 6, 7, 8, 9}
	if a := CheckFuelTrims(dupTimes, dupST, dupLT); a.Level != LevelNormal {
		t.Errorf("expected normal (slope error), got %+v", a)
	}
}

func TestCheckCatalyticConverter(t *testing.T) {
	t0 := time.Now()

	// Empty case
	if a := CheckCatalyticConverter(nil, nil, nil, 1200, 90.0); a.Level != LevelNormal {
		t.Errorf("expected normal for empty O2 arrays, got %+v", a)
	}

	// Mismatched lengths
	if a := CheckCatalyticConverter([]time.Time{t0}, []float64{1}, []float64{1, 2}, 1200, 90.0); a.Level != LevelNormal || a.Message != "No data or mismatched O2 sensors" {
		t.Errorf("expected mismatched trims warning/message, got %+v", a)
	}

	// Low RPM / low temp
	if a := CheckCatalyticConverter([]time.Time{t0, t0.Add(5 * time.Second)}, []float64{0.5, 0.6}, []float64{0.5, 0.6}, 800, 70.0); a.Level != LevelNormal {
		t.Errorf("expected normal for low RPM/temp, got %+v", a)
	}

	// Too few points in 60s window (windowSize < 10)
	shortTimes := make([]time.Time, 5)
	shortS1 := []float64{0.5, 0.6, 0.5, 0.6, 0.5}
	shortS2 := []float64{0.5, 0.6, 0.5, 0.6, 0.5}
	for i := 0; i < 5; i++ {
		shortTimes[i] = t0.Add(time.Duration(i*5) * time.Second)
	}
	if a := CheckCatalyticConverter(shortTimes, shortS1, shortS2, 1200, 90.0); a.Level != LevelNormal {
		t.Errorf("expected normal for too few points in window, got %+v", a)
	}

	// Degradation warning: highly correlated upstream/downstream and high variance on downstream.
	// We also include an older timestamp to trigger the else-break branch in O2 windowing loop.
	times := make([]time.Time, 11)
	times[0] = t0
	tCatStart := t0.Add(100 * time.Second)
	for i := 1; i < 11; i++ {
		times[i] = tCatStart.Add(time.Duration(i*5) * time.Second) // 105s to 150s (delta = 45s, well within 60s)
	}
	s1 := []float64{0.0, 0.1, 0.8, 0.2, 0.9, 0.1, 0.8, 0.2, 0.9, 0.1, 0.8}
	s2 := []float64{0.0, 0.15, 0.75, 0.25, 0.85, 0.15, 0.75, 0.25, 0.85, 0.15, 0.75} // closely mimics s1 (skipping index 0 which is older)
	if a := CheckCatalyticConverter(times, s1, s2, 1200, 90.0); a.Level != LevelWarning {
		t.Errorf("expected warning for degraded cat, got %+v", a)
	}

	// Normal case: uncorrelated and stable downstream
	s2Stable := []float64{0.0, 0.5, 0.51, 0.49, 0.5, 0.51, 0.49, 0.5, 0.51, 0.49, 0.5}
	if a := CheckCatalyticConverter(times, s1, s2Stable, 1200, 90.0); a.Level != LevelNormal {
		t.Errorf("expected normal for stable downstream, got %+v", a)
	}
}
