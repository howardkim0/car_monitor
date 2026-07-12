plugins {
    id("com.android.application") version "8.4.0" apply false
    id("org.jetbrains.kotlin.android") version "1.9.23" apply false
    // Coverage reporting only (informational) — see DESIGN.md section 13
    // for why android/ isn't held to the same 100% gate go/ is.
    id("org.jetbrains.kotlinx.kover") version "0.7.6" apply false
}
