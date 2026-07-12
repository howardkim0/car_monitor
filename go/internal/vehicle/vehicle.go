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

// subaruForester2023 covers the standard SAE J1979 Mode 01 PIDs common to
// virtually every OBD2-compliant car; Subaru-specific PIDs can be added
// here once sourced/reverse-engineered.
var subaruForester2023 = Profile{
	Make:  "Subaru",
	Model: "Forester",
	Year:  2023,
	PIDs: []PID{
		{Code: 0x0C, Mode: ModeCurrentData, Name: "Engine RPM", Unit: "rpm", Decode: decodeRPM},
		{Code: 0x0D, Mode: ModeCurrentData, Name: "Vehicle Speed", Unit: "km/h", Decode: decodeSpeed},
		{Code: 0x05, Mode: ModeCurrentData, Name: "Coolant Temperature", Unit: "C", Decode: decodeCoolantTemp},
		{Code: 0x11, Mode: ModeCurrentData, Name: "Throttle Position", Unit: "%", Decode: decodeThrottlePosition},
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

// decodeSpeed implements the SAE J1979 formula for PID 0x0D: A, in km/h.
func decodeSpeed(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("speed: want 1 data byte, got %d", len(data))
	}
	return float64(data[0]), nil
}

// decodeCoolantTemp implements the SAE J1979 formula for PID 0x05: A-40, in C.
func decodeCoolantTemp(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("coolant temp: want 1 data byte, got %d", len(data))
	}
	return float64(data[0]) - 40, nil
}

// decodeThrottlePosition implements the SAE J1979 formula for PID 0x11:
// A*100/255, as a percentage.
func decodeThrottlePosition(data []byte) (float64, error) {
	if len(data) < 1 {
		return 0, fmt.Errorf("throttle position: want 1 data byte, got %d", len(data))
	}
	return float64(data[0]) * 100 / 255, nil
}
