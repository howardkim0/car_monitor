package com.carmonitor.app

/**
 * Test double for [VehicleMobile] — no native/JNI touch, fully
 * in-memory, mirroring [FakeObdMobile]'s role for [ObdMobile].
 *
 * [registry] mirrors internal/vehicle's shape closely enough for
 * [VehiclePickerActivityTest]'s purposes: year -> make -> model -> list
 * of trims. An empty trim list means a single, untrimmed variant —
 * matching internal/vehicle.Trims's "empty means skip the Trim step"
 * contract exactly, so tests can exercise both the with-trim and
 * skip-trim paths by choosing which model they drill into.
 */
class FakeVehicleMobile(
    var registry: Map<Long, Map<String, Map<String, List<String>>>> = mapOf(
        2023L to mapOf(
            "Subaru" to mapOf(
                "Forester" to listOf("Wilderness"),
                "Outback" to emptyList(),
            )
        )
    ),
    var summary: String = "2023 Subaru Forester",
) : VehicleMobile {

    data class SetSelectedCall(
        val storageDir: String,
        val year: Long,
        val make: String,
        val model: String,
        val trim: String,
    )

    val setSelectedCalls = mutableListOf<SetSelectedCall>()
    var setSelectedThrows: Exception? = null

    private fun years(): List<Long> = registry.keys.sortedDescending()
    private fun makes(year: Long): List<String> = registry[year]?.keys?.sorted().orEmpty()
    private fun models(year: Long, make: String): List<String> = registry[year]?.get(make)?.keys?.sorted().orEmpty()
    private fun trims(year: Long, make: String, model: String): List<String> =
        registry[year]?.get(make)?.get(model).orEmpty()

    override fun yearCount(): Long = years().size.toLong()
    override fun yearAt(i: Long): Long = years().getOrElse(i.toInt()) { 0L }

    override fun makeCount(year: Long): Long = makes(year).size.toLong()
    override fun makeAt(year: Long, i: Long): String = makes(year).getOrElse(i.toInt()) { "" }

    override fun modelCount(year: Long, make: String): Long = models(year, make).size.toLong()
    override fun modelAt(year: Long, make: String, i: Long): String = models(year, make).getOrElse(i.toInt()) { "" }

    override fun trimCount(year: Long, make: String, model: String): Long = trims(year, make, model).size.toLong()
    override fun trimAt(year: Long, make: String, model: String, i: Long): String =
        trims(year, make, model).getOrElse(i.toInt()) { "" }

    override fun setSelectedVehicle(storageDir: String, year: Long, make: String, model: String, trim: String) {
        setSelectedCalls += SetSelectedCall(storageDir, year, make, model, trim)
        setSelectedThrows?.let { throw it }
    }

    override fun selectedVehicleSummary(storageDir: String): String = summary
}
