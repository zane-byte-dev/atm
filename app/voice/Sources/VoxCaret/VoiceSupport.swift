import SwiftUI
import os

// VoxCaret owns its presentation and logs; the app has no dependency on another product runtime.
enum VoxCaretTheme {
    static let primary = Color.primary
    static let secondary = Color.secondary
    static let accent = Color(red: 0.02, green: 0.67, blue: 0.88)
    static let accentFill = accent.opacity(0.12)
    static let indigo = Color(red: 0.16, green: 0.12, blue: 0.72)
    static let brandGradient = LinearGradient(
        colors: [indigo.opacity(0.16), accent.opacity(0.10)],
        startPoint: .topLeading,
        endPoint: .bottomTrailing
    )
    static let controlFill = Color(nsColor: .controlBackgroundColor)
    static let border = Color(nsColor: .separatorColor)
    static let success = Color.green
    static let warning = Color.orange
}

enum VoxCaretFont {
    enum Size { case body }
    static let footnote = Font.footnote
    static func font(_ size: Size, weight: Font.Weight) -> Font { .system(size: 13, weight: weight) }
    static func mono(_ size: Size, _ weight: Font.Weight) -> Font { .system(size: 13, weight: weight, design: .monospaced) }
}

enum VoxCaretLog {
    private static let logger = Logger(subsystem: "dev.zanebyte.voxcaret", category: "voice")
    static func failure(_ event: String, error: String? = nil, fields: [String: String] = [:]) {
        // Error details may contain text or paths; never make them public in unified logging.
        logger.error("\(event, privacy: .public): \(error ?? "", privacy: .private)")
    }
    static func lifecycle(_ event: String, fields: [String: String] = [:]) {
        logger.info("\(event, privacy: .public)")
    }
}
