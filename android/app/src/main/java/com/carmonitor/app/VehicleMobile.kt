package com.carmonitor.app

import mobile.Mobile

/**
 * Thin seam over the gomobile-bound `Mobile` vehicle-selection calls —
 * same reasoning as [ObdMobile] (`mobile.Mobile` is a generated
 * `static native`-method class, not a Kotlin `object`, so it can't be
 * mocked directly), but for a different reason: unlike
 * `DeviceScanActivity` (whose device list comes entirely from Android's
 * own `BluetoothAdapter`, `Mobile` touched only at the point of final
 * selection), every list [VehiclePickerActivity] renders comes directly
 * from a `Mobile` call. Without this seam, none of that Activity's
 * actual drill-down/skip logic would be unit-testable under Robolectric
 * at all — see DESIGN.md sections 5.3 and 10.
 */
interface VehicleMobile {
    fun yearCount(): Long
    fun yearAt(i: Long): Long
    fun makeCount(year: Long): Long
    fun makeAt(year: Long, i: Long): String
    fun modelCount(year: Long, make: String): Long
    fun modelAt(year: Long, make: String, i: Long): String
    fun trimCount(year: Long, make: String, model: String): Long
    fun trimAt(year: Long, make: String, model: String, i: Long): String
    fun setSelectedVehicle(storageDir: String, year: Long, make: String, model: String, trim: String)
    fun selectedVehicleSummary(storageDir: String): String
}

object RealVehicleMobile : VehicleMobile {
    override fun yearCount(): Long = Mobile.vehicleYearCount()
    override fun yearAt(i: Long): Long = Mobile.vehicleYearAt(i)
    override fun makeCount(year: Long): Long = Mobile.vehicleMakeCount(year)
    override fun makeAt(year: Long, i: Long): String = Mobile.vehicleMakeAt(year, i)
    override fun modelCount(year: Long, make: String): Long = Mobile.vehicleModelCount(year, make)
    override fun modelAt(year: Long, make: String, i: Long): String = Mobile.vehicleModelAt(year, make, i)
    override fun trimCount(year: Long, make: String, model: String): Long = Mobile.vehicleTrimCount(year, make, model)
    override fun trimAt(year: Long, make: String, model: String, i: Long): String =
        Mobile.vehicleTrimAt(year, make, model, i)

    override fun setSelectedVehicle(storageDir: String, year: Long, make: String, model: String, trim: String) {
        Mobile.setSelectedVehicle(storageDir, year, make, model, trim)
    }

    override fun selectedVehicleSummary(storageDir: String): String = Mobile.selectedVehicleSummary(storageDir)
}
