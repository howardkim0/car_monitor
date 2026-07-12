// Package vehicle holds per-car OBD2 PID definitions and decode formulas.
// See DESIGN.md section 5.2: today there is exactly one hardcoded vehicle
// profile, but internal/obd2 only ever depends on Profile, so adding a
// second car is additive.
package vehicle

import "fmt"

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

// Default returns the vehicle profile the app should decode readings
// against.
func Default() Profile {
	return subaruForester2023
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
