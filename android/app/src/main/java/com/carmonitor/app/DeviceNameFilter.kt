package com.carmonitor.app

/**
 * Filters Bluetooth device names down to ones that look like OBD2
 * scanners, so both "Pair Bluetooth OBD2 Scanners" and "Show Paired
 * Devices" only ever list likely OBD2 dongles, not every other
 * Bluetooth device nearby or ever paired (phones, headphones, etc.).
 * Pure string matching — no device.Profile state, no Go round-trip
 * needed (DESIGN.md section 5.1).
 */
object DeviceNameFilter {
    private val OBD2_KEYWORDS = listOf("obd", "elm")

    /**
     * True if [name] looks like an OBD2 scanner's advertised name — a
     * case-insensitive substring match against a small keyword list
     * ("obd", "elm"), covering names like "ELM327", "OBDLink", "OBDII".
     * A null or blank name (unresolved, or permission not yet granted)
     * can't be confirmed either way, so it's excluded: the goal is
     * showing only OBD2 scanners, not showing everything unless proven
     * not to be one.
     */
    fun looksLikeObd2Scanner(name: String?): Boolean {
        if (name.isNullOrBlank()) return false
        val lower = name.lowercase()
        return OBD2_KEYWORDS.any { lower.contains(it) }
    }
}
