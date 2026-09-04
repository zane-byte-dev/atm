import SwiftUI
import os

// Voice owns its presentation and logs. Nothing here reads ATM's database or runtime.
enum ATMTheme {
    static let primary = Color.primary
    static let secondary = Color.secondary
    static let accent = Color.accentColor
    static let accentFill = Color.accentColor.opacity(0.12)
    static let controlFill = Color(nsColor: .controlBackgroundColor)
    static let border = Color(nsColor: .separatorColor)
    static let success = Color.green
    static let warning = Color.orange
}

enum ATMFont {
    enum Size { case body }
    static let footnote = Font.footnote
    static func font(_ size: Size, weight: Font.Weight) -> Font { .system(size: 13, weight: weight) }
    static func mono(_ size: Size, _ weight: Font.Weight) -> Font { .system(size: 13, weight: weight, design: .monospaced) }
}

enum ATMLog {
    private static let logger = Logger(subsystem: "dev.zanebyte.atm.voice", category: "voice")
    static func failure(_ event: String, error: String? = nil, fields: [String: String] = [:]) {
        // Error details may contain text or paths; never make them public in unified logging.
        logger.error("\(event, privacy: .public): \(error ?? "", privacy: .private)")
    }
    static func lifecycle(_ event: String, fields: [String: String] = [:]) {
        logger.info("\(event, privacy: .public)")
    }
}
