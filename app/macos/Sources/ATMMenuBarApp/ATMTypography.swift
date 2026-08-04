import AppKit
import SwiftUI

/// App-wide color appearance. `system` leaves `NSApp.appearance` unset so ATM
/// continues to follow the macOS setting, including changes made while it is
/// running.
enum ATMThemeMode: String, CaseIterable, Identifiable {
    case system
    case light
    case dark

    var id: String { rawValue }

    var label: String {
        switch self {
        case .system: return "跟随系统"
        case .light: return "浅色"
        case .dark: return "深色"
        }
    }

    var nsAppearance: NSAppearance? {
        switch self {
        case .system: return nil
        case .light: return NSAppearance(named: .aqua)
        case .dark: return NSAppearance(named: .darkAqua)
        }
    }
}

/// Reading size for long-form content — task descriptions, knowledge documents,
/// shared memories, progress entries, agent replies.
///
/// Only *content* is adjustable. Chrome (sidebar, list rows, tables, labels,
/// buttons) is pinned to the `ATMFont` ladder: scaling the whole UI made the large
/// setting overwhelming, and chrome is glanced at rather than read.
enum ATMContentTextSize: String, CaseIterable, Identifiable {
    case small
    case medium
    case large
    case extraLarge

    var id: String { rawValue }

    /// The floor is the fixed chrome body size, so content can only go up from the
    /// default — there is no setting that reintroduces the original tiny text.
    var pointSize: CGFloat {
        switch self {
        case .small: return 13
        case .medium: return 15
        case .large: return 17
        case .extraLarge: return 20
        }
    }

    var label: String {
        switch self {
        case .small: return "小"
        case .medium: return "中"
        case .large: return "大"
        case .extraLarge: return "特大"
        }
    }
}

/// Owns app appearance preferences. Views that render long-form text observe this
/// directly for reading size changes. The app controller observes `themeMode`
/// once and applies it to AppKit, covering every window and menu.
final class ATMAppearance: ObservableObject {
    static let shared = ATMAppearance()

    private static let contentTextSizeKey = "atmContentTextSize"
    private static let themeModeKey = "atmThemeMode"

    @Published var contentTextSize: ATMContentTextSize {
        didSet {
            guard contentTextSize != oldValue else { return }
            defaults.set(contentTextSize.rawValue, forKey: Self.contentTextSizeKey)
        }
    }

    @Published var themeMode: ATMThemeMode {
        didSet {
            guard themeMode != oldValue else { return }
            defaults.set(themeMode.rawValue, forKey: Self.themeModeKey)
        }
    }

    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        let storedContentTextSize = defaults.string(forKey: Self.contentTextSizeKey)
        let storedThemeMode = defaults.string(forKey: Self.themeModeKey)
        // 中 (15pt) is what the detail and document bodies already render at, so the
        // default changes nothing for someone who never opens the setting.
        contentTextSize = storedContentTextSize.flatMap(ATMContentTextSize.init(rawValue:)) ?? .medium
        themeMode = storedThemeMode.flatMap(ATMThemeMode.init(rawValue:)) ?? .system
    }
}

/// Semantic font ladder for chrome. Base sizes follow macOS standard typography
/// (body 13, callout 12, subheadline 11) rather than the tighter scale the app
/// started with, where body text sat at 9–12pt and read as uniformly cramped.
///
/// Monospaced variants sit at the *same* tier as their sans counterparts, not a
/// point below: ids, token counts and timestamps were the smallest text in the
/// app. Numeric column alignment comes from `.monospacedDigit()`, not from
/// shrinking the glyphs.
enum ATMFont {
    enum Tier {
        /// Corner badges and the smallest chrome.
        case micro
        /// Secondary metadata, group labels, table headers.
        case caption
        /// Secondary list rows, explanatory copy.
        case footnote
        /// Body copy, primary list rows, buttons, text fields.
        case body
        /// Detail body copy, section headings.
        case bodyLarge
        case title3
        case title2
        case title1
        /// Metric card numbers.
        case metric
        /// Empty-state glyphs and the largest headline numbers.
        case display

        var size: CGFloat {
            switch self {
            case .micro: return 10
            case .caption: return 11
            case .footnote: return 12
            case .body: return 13
            case .bodyLarge: return 15
            case .title3: return 17
            case .title2: return 20
            case .title1: return 22
            case .metric: return 26
            case .display: return 32
            }
        }
    }

    static func size(_ tier: Tier) -> CGFloat { tier.size }

    static func font(
        _ tier: Tier,
        weight: Font.Weight = .regular,
        design: Font.Design = .default
    ) -> Font {
        .system(size: tier.size, weight: weight, design: design)
    }

    static func mono(_ tier: Tier, _ weight: Font.Weight = .regular) -> Font {
        font(tier, weight: weight, design: .monospaced)
    }

    static func rounded(_ tier: Tier, _ weight: Font.Weight = .regular) -> Font {
        font(tier, weight: weight, design: .rounded)
    }

    /// For the AppKit text views, which need an `NSFont` rather than a `Font`.
    static func nsFont(_ tier: Tier, weight: NSFont.Weight = .regular) -> NSFont {
        .systemFont(ofSize: tier.size, weight: weight)
    }

    static var micro: Font { font(.micro) }
    static var caption: Font { font(.caption) }
    static var footnote: Font { font(.footnote) }
    static var body: Font { font(.body) }
    static var bodyLarge: Font { font(.bodyLarge) }
    static var title3: Font { font(.title3) }
    static var title2: Font { font(.title2) }
    static var title1: Font { font(.title1) }
    static var metric: Font { font(.metric) }
    static var display: Font { font(.display) }
}
