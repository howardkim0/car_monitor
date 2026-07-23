// Package vehicle holds per-car OBD2 PID definitions and decode formulas.
// See DESIGN.md section 5.2: internal/obd2 only ever depends on Profile,
// so adding a second car to registry is additive.
package vehicle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Mode is an OBD2 service/mode identifier.
type Mode byte

// ModeCurrentData is Mode 01, "show current data" — the only mode this app
// requests in v1. DTC read/clear (modes 03/04) is future work.
const ModeCurrentData Mode = 0x01

// PID describes one OBD2 parameter this app knows how to request and
// decode for a given vehicle.
type PID struct {
	Code   byte
	Mode   Mode
	Name   string
	Unit   string
	Decode func(data []byte) (float64, error)
}

// Profile describes one vehicle: the PIDs it supports and how to decode
// their responses.
type Profile struct {
	Make  string
	Model string
	Year  int
	Trim  string // "" when this Make/Model/Year has only one variant
	PIDs  []PID
}

// PID finds a PID definition by code within this profile.
func (p Profile) PID(code byte) (PID, bool) {
	for _, pid := range p.PIDs {
		if pid.Code == code {
			return pid, true
		}
	}
	return PID{}, false
}

// subaruForester2023 targets the naturally-aspirated FB25 2.5L variant
// specifically (not the turbocharged FA24) — see DESIGN.md section 5.2.
// That distinction doesn't change which PIDs apply (SAE J1979 has no
// dedicated "boost" PID; boost is inferred from Intake Manifold Absolute
// Pressure exceeding Barometric Pressure, both included below and valid
// for either engine), only how Intake Manifold Pressure *behaves* — on
// this NA engine it never exceeds ambient.
//
// This list is a deliberately curated practical subset of the full SAE
// J1979 Mode 01 PID table, not an exhaustive one — see DESIGN.md section
// 5.2 for the full reasoning. `internal/obd2`'s discovery step (SAE
// "PIDs supported" bitmask queries) means a PID listed here that this
// particular ECU doesn't actually support is simply never requested, so
// there's no runtime cost to the list being broader than any one car
// needs — the cost is purely the implementation/test effort of adding
// entries, which is what bounds this list's size.
var subaruForester2023 = Profile{
	Make:  "Subaru",
	Model: "Forester",
	Year:  2023,
	PIDs: []PID{
		{Code: 0x04, Mode: ModeCurrentData, Name: "Calculated Engine Load", Unit: "%", Decode: decodePercentOfByte},
		{Code: 0x05, Mode: ModeCurrentData, Name: "Coolant Temperature", Unit: "C", Decode: decodeByteMinus40},
		{Code: 0x06, Mode: ModeCurrentData, Name: "Short Term Fuel Trim Bank 1", Unit: "%", Decode: decodeFuelTrim},
		{Code: 0x07, Mode: ModeCurrentData, Name: "Long Term Fuel Trim Bank 1", Unit: "%", Decode: decodeFuelTrim},
		{Code: 0x08, Mode: ModeCurrentData, Name: "Short Term Fuel Trim Bank 2", Unit: "%", Decode: decodeFuelTrim},
		{Code: 0x09, Mode: ModeCurrentData, Name: "Long Term Fuel Trim Bank 2", Unit: "%", Decode: decodeFuelTrim},
		{Code: 0x0A, Mode: ModeCurrentData, Name: "Fuel Pressure", Unit: "kPa", Decode: decodeFuelPressure},
		{Code: 0x0B, Mode: ModeCurrentData, Name: "Intake Manifold Pressure", Unit: "kPa", Decode: decodeByteAsIs},
		{Code: 0x0C, Mode: ModeCurrentData, Name: "Engine RPM", Unit: "rpm", Decode: decodeRPM},
		{Code: 0x0D, Mode: ModeCurrentData, Name: "Vehicle Speed", Unit: "km/h", Decode: decodeByteAsIs},
		{Code: 0x0E, Mode: ModeCurrentData, Name: "Timing Advance", Unit: "deg", Decode: decodeTimingAdvance},
		{Code: 0x0F, Mode: ModeCurrentData, Name: "Intake Air Temperature", Unit: "C", Decode: decodeByteMinus40},
		{Code: 0x10, Mode: ModeCurrentData, Name: "Mass Air Flow Rate", Unit: "g/s", Decode: decodeMassAirFlow},
		{Code: 0x11, Mode: ModeCurrentData, Name: "Throttle Position", Unit: "%", Decode: decodePercentOfByte},
		// O2 Sensor PIDs 0x14-0x17 each return 2 data bytes (sensor
		// voltage, then short-term fuel trim); only the voltage is
		// decoded here — the trim sub-field is redundant with the
		// bank-level trim already captured via 0x06-0x09 above. See
		// decodeO2SensorVoltage and DESIGN.md section 5.2.
		{Code: 0x14, Mode: ModeCurrentData, Name: "O2 Sensor Bank 1 Sensor 1", Unit: "V", Decode: decodeO2SensorVoltage},
		{Code: 0x15, Mode: ModeCurrentData, Name: "O2 Sensor Bank 1 Sensor 2", Unit: "V", Decode: decodeO2SensorVoltage},
		{Code: 0x16, Mode: ModeCurrentData, Name: "O2 Sensor Bank 2 Sensor 1", Unit: "V", Decode: decodeO2SensorVoltage},
		{Code: 0x17, Mode: ModeCurrentData, Name: "O2 Sensor Bank 2 Sensor 2", Unit: "V", Decode: decodeO2SensorVoltage},
		{Code: 0x1F, Mode: ModeCurrentData, Name: "Run Time Since Engine Start", Unit: "s", Decode: decodeTwoByteInt},
		{Code: 0x21, Mode: ModeCurrentData, Name: "Distance With MIL On", Unit: "km", Decode: decodeTwoByteInt},
		{Code: 0x2C, Mode: ModeCurrentData, Name: "Commanded EGR", Unit: "%", Decode: decodePercentOfByte},
		{Code: 0x2F, Mode: ModeCurrentData, Name: "Fuel Tank Level", Unit: "%", Decode: decodePercentOfByte},
		{Code: 0x30, Mode: ModeCurrentData, Name: "Warm-ups Since Codes Cleared", Unit: "count", Decode: decodeByteAsIs},
		{Code: 0x31, Mode: ModeCurrentData, Name: "Distance Since Codes Cleared", Unit: "km", Decode: decodeTwoByteInt},
		{Code: 0x33, Mode: ModeCurrentData, Name: "Barometric Pressure", Unit: "kPa", Decode: decodeByteAsIs},
		{Code: 0x42, Mode: ModeCurrentData, Name: "Control Module Voltage", Unit: "V", Decode: decodeControlModuleVoltage},
		{Code: 0x43, Mode: ModeCurrentData, Name: "Absolute Load Value", Unit: "%", Decode: decodeAbsoluteLoad},
		{Code: 0x45, Mode: ModeCurrentData, Name: "Relative Throttle Position", Unit: "%", Decode: decodePercentOfByte},
		{Code: 0x46, Mode: ModeCurrentData, Name: "Ambient Air Temperature", Unit: "C", Decode: decodeByteMinus40},
		{Code: 0x59, Mode: ModeCurrentData, Name: "Fuel Rail Pressure", Unit: "kPa", Decode: decodeFuelRailPressure},
		{Code: 0x5C, Mode: ModeCurrentData, Name: "Engine Oil Temperature", Unit: "C", Decode: decodeByteMinus40},
		{Code: 0x5E, Mode: ModeCurrentData, Name: "Engine Fuel Rate", Unit: "L/h", Decode: decodeEngineFuelRate},
	},
}

// subaruForester2023Wilderness is the off-road-package trim of the same
// 2023 Forester above -- same FB25 2.5L NA engine and control module as
// the base profile, so it reuses its PIDs slice as-is rather than
// duplicating it: Wilderness changes suspension, gearing, and
// cladding/skid plates, none of which touches what the ECU exposes over
// generic OBD2 Mode 01. Not independently hardware-verified (only the
// base trim above has been, per docs/defects.md's "Trend / anomaly
// detection" investigation), but this is an engineering inference about
// the same verified engine/ECU family, not a guess about an unverified
// vehicle the way a different Make/Model would be.
var subaruForester2023Wilderness = Profile{
	Make:  "Subaru",
	Model: "Forester",
	Year:  2023,
	Trim:  "Wilderness",
	PIDs:  subaruForester2023.PIDs,
}

// registry lists every vehicle profile this app is aware of. Adding
// support for a new car means appending here; internal/obd2 and the
// Android shell only ever depend on Profile, never on a literal
// Make/Model/Year/Trim (DESIGN.md section 5.2).
//
// Deliberately holds only the one real, hardware-verified vehicle
// family so far (both entries are the same 2023 Forester): unlike the SAE-standard PID formulas above (universally correct
// regardless of vehicle, filtered per-ECU by internal/obd2's discovery
// step at runtime), claiming a specific Make/Model is
// supported is a claim about that vehicle's PID list actually being
// right, which hasn't been verified for anything but the Forester yet
// — see docs/plan-multi-vehicle-support.md for how more are added.
var registry = []Profile{
	subaruForester2023,
	subaruForester2023Wilderness,
}

// Default returns the vehicle profile the app should decode readings
// against absent any user selection — see SelectedOrDefault.
func Default() Profile {
	return registry[0]
}

// Years returns every distinct model year present in registry,
// descending (newest first) — the first step of the in-app vehicle
// picker (DESIGN.md section 5.3).
func Years() []int {
	seen := make(map[int]bool)
	var years []int
	for _, p := range registry {
		if !seen[p.Year] {
			seen[p.Year] = true
			years = append(years, p.Year)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years
}

// Makes returns every distinct make with at least one registry profile
// for year, sorted alphabetically.
func Makes(year int) []string {
	seen := make(map[string]bool)
	var makes []string
	for _, p := range registry {
		if p.Year == year && !seen[p.Make] {
			seen[p.Make] = true
			makes = append(makes, p.Make)
		}
	}
	sort.Strings(makes)
	return makes
}

// Models returns every distinct model with at least one registry
// profile for year+make, sorted alphabetically.
func Models(year int, make string) []string {
	seen := map[string]bool{}
	var models []string
	for _, p := range registry {
		if p.Year == year && p.Make == make && !seen[p.Model] {
			seen[p.Model] = true
			models = append(models, p.Model)
		}
	}
	sort.Strings(models)
	return models
}

// Trims returns every distinct, non-empty trim for year+make+model,
// sorted alphabetically. Empty (not [""]) when that combination has
// exactly one, untrimmed profile — the picker UI uses that to skip its
// Trim/Engine step entirely rather than showing a single-item list with
// nothing to actually choose.
func Trims(year int, make, model string) []string {
	seen := map[string]bool{}
	var trims []string
	for _, p := range registry {
		if p.Year == year && p.Make == make && p.Model == model && p.Trim != "" && !seen[p.Trim] {
			seen[p.Trim] = true
			trims = append(trims, p.Trim)
		}
	}
	sort.Strings(trims)
	return trims
}

// Find returns the registry profile matching year/make/model/trim
// exactly (trim "" for an untrimmed single-variant profile), and
// whether one exists.
func Find(year int, make, model, trim string) (Profile, bool) {
	for _, p := range registry {
		if p.Year == year && p.Make == make && p.Model == model && p.Trim == trim {
			return p, true
		}
	}
	return Profile{}, false
}

const selectedVehicleFileName = "selected_vehicle.txt"

// SaveSelected persists p's identifying fields (not its PIDs, which are
// always resolved fresh from registry via Find — see LoadSelected) as
// the user's chosen vehicle, overriding Default() until changed again —
// see DESIGN.md section 5.3. Written as "year\nmake\nmodel\ntrim\n",
// matching internal/device's plain-text-over-JSON philosophy for small,
// simple, human-inspectable on-device state.
func SaveSelected(dir string, p Profile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create vehicle selection dir %q: %w", dir, err)
	}
	data := strconv.Itoa(p.Year) + "\n" + p.Make + "\n" + p.Model + "\n" + p.Trim + "\n"
	path := filepath.Join(dir, selectedVehicleFileName)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("write vehicle selection %q: %w", path, err)
	}
	return nil
}

// LoadSelected reads back a persisted selection under dir, if one
// exists, resolving it through Find rather than trusting a PIDs list
// read from disk (Profile.PIDs holds function values, which can't
// round-trip through a text file anyway). ok is false (not an error) if
// SaveSelected has never been called there, or if the saved
// year/make/model/trim no longer matches any registry entry (e.g. an
// app update removed it) — a stale selection falls back to Default()
// the same way "never selected" does, rather than surfacing an error
// for something the user didn't do wrong.
func LoadSelected(dir string) (profile Profile, ok bool, err error) {
	path := filepath.Join(dir, selectedVehicleFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("read vehicle selection %q: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 4 || lines[0] == "" || lines[1] == "" || lines[2] == "" {
		return Profile{}, false, fmt.Errorf("vehicle selection %q is malformed", path)
	}
	year, err := strconv.Atoi(lines[0])
	if err != nil {
		return Profile{}, false, fmt.Errorf("vehicle selection %q has invalid year %q: %w", path, lines[0], err)
	}
	p, found := Find(year, lines[1], lines[2], lines[3])
	if !found {
		return Profile{}, false, nil
	}
	return p, true, nil
}

// SelectedOrDefault returns LoadSelected's result if a valid selection
// exists under dir, else Default(). A read/parse/no-longer-resolves
// error is treated the same as "no selection" — best-effort, falls back
// rather than blocking every connection attempt over a corrupted or
// stale preference file.
func SelectedOrDefault(dir string) Profile {
	profile, ok, err := LoadSelected(dir)
	if err != nil || !ok {
		return Default()
	}
	return profile
}

// decodeRPM implements the SAE J1979 formula for PID 0x0C: ((A*256)+B)/4.
func decodeRPM(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("rpm: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) / 4.0, nil
}

// decodeByteMinus40 implements the "A - 40" formula shared by every
// temperature PID in this profile (coolant, intake air, ambient air,
// engine oil).
func decodeByteMinus40(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("want 1 data byte, got %d", len(data))
	}
	return float64(data[0]) - 40, nil
}

// decodePercentOfByte implements the "100/255 * A" percentage formula
// shared by throttle position, engine load, EGR, fuel tank level, and
// relative throttle position.
func decodePercentOfByte(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("want 1 data byte, got %d", len(data))
	}
	return float64(data[0]) * 100 / 255, nil
}

// decodeFuelTrim implements the "100/128 * A - 100" formula shared by
// the four short/long term fuel trim PIDs (banks 1 and 2).
func decodeFuelTrim(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("want 1 data byte, got %d", len(data))
	}
	return float64(data[0])*100/128 - 100, nil
}

// decodeByteAsIs implements a raw single-byte value with no scaling,
// shared by vehicle speed, intake manifold pressure, barometric
// pressure, and the warm-up counter.
func decodeByteAsIs(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("want 1 data byte, got %d", len(data))
	}
	return float64(data[0]), nil
}

// decodeTwoByteInt implements the "256*A + B" formula shared by the
// run-time and distance-counter PIDs.
func decodeTwoByteInt(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8 | uint16(data[1])), nil
}

// decodeO2SensorVoltage implements "A/200" for PIDs 0x14-0x17. The
// response carries a second data byte (short-term fuel trim) that is
// deliberately not decoded — see the profile comment above.
func decodeO2SensorVoltage(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("want at least 1 data byte, got %d", len(data))
	}
	return float64(data[0]) / 200, nil
}

// decodeFuelPressure implements the SAE J1979 formula for PID 0x0A: 3*A.
func decodeFuelPressure(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("fuel pressure: want 1 data byte, got %d", len(data))
	}
	return float64(data[0]) * 3, nil
}

// decodeTimingAdvance implements the SAE J1979 formula for PID 0x0E:
// A/2 - 64.
func decodeTimingAdvance(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("timing advance: want 1 data byte, got %d", len(data))
	}
	return float64(data[0])/2 - 64, nil
}

// decodeMassAirFlow implements the SAE J1979 formula for PID 0x10:
// (256A+B)/100.
func decodeMassAirFlow(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("mass air flow: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) / 100, nil
}

// decodeControlModuleVoltage implements the SAE J1979 formula for PID
// 0x42: (256A+B)/1000.
func decodeControlModuleVoltage(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("control module voltage: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) / 1000, nil
}

// decodeAbsoluteLoad implements the SAE J1979 formula for PID 0x43:
// 100/255 * (256A+B).
func decodeAbsoluteLoad(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("absolute load: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) * 100 / 255, nil
}

// decodeFuelRailPressure implements the SAE J1979 formula for PID 0x59:
// 10 * (256A+B).
func decodeFuelRailPressure(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("fuel rail pressure: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) * 10, nil
}

// decodeEngineFuelRate implements the SAE J1979 formula for PID 0x5E:
// (256A+B)/20.
func decodeEngineFuelRate(data []byte) (float64, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("engine fuel rate: want 2 data bytes, got %d", len(data))
	}
	return float64(uint16(data[0])<<8|uint16(data[1])) / 20, nil
}
