import AppKit
import XCTest
@testable import ATMMenuBarApp

final class ATMTypographyTests: XCTestCase {
    private let ladder: [ATMFont.Tier] = [
        .micro, .caption, .footnote, .body, .bodyLarge, .title3, .title2, .title1, .metric, .display,
    ]

    func testChromeLadderMatchesMacOSStandard() {
        XCTAssertEqual(ATMFont.Tier.body.size, 13)
        XCTAssertEqual(ATMFont.Tier.footnote.size, 12)
        XCTAssertEqual(ATMFont.Tier.caption.size, 11)
    }

    /// If two tiers ever resolve to the same point size the visual hierarchy
    /// silently disappears, which is hard to spot by eye.
    func testChromeLadderIsStrictlyIncreasing() {
        let sizes = ladder.map(\.size)
        for (lower, upper) in zip(sizes, sizes.dropFirst()) {
            XCTAssertLessThan(lower, upper, "档位塌陷：\(lower) 不小于 \(upper)")
        }
    }

    /// The setting can only make content larger than chrome body text, never
    /// smaller — the original complaint was that everything read too small, so no
    /// option may reintroduce that.
    func testContentSizesNeverGoBelowChromeBody() {
        for size in ATMContentTextSize.allCases {
            XCTAssertGreaterThanOrEqual(size.pointSize, ATMFont.Tier.body.size, "\(size.label) 档低于界面正文")
        }
    }

    func testContentSizesAreDistinctAndAscending() {
        let sizes = ATMContentTextSize.allCases.map(\.pointSize)
        XCTAssertEqual(sizes, [13, 15, 17, 20])
        XCTAssertEqual(sizes, sizes.sorted())
        XCTAssertEqual(Set(sizes).count, sizes.count)
    }

    func testContentSizePersistsAndDefaultsToMedium() throws {
        let suite = "ATMTypographyTests"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
        defer { defaults.removePersistentDomain(forName: suite) }

        let appearance = ATMAppearance(defaults: defaults)
        // 中 is what the detail and document bodies already rendered at, so a fresh
        // install looks unchanged until someone opens 设置.
        XCTAssertEqual(appearance.contentTextSize, .medium)

        appearance.contentTextSize = .extraLarge
        XCTAssertEqual(ATMAppearance(defaults: defaults).contentTextSize, .extraLarge, "档位未从 UserDefaults 恢复")
    }

    func testThemeDefaultsToSystemAndPersists() throws {
        let suite = "ATMThemeModeTests"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
        defer { defaults.removePersistentDomain(forName: suite) }

        let appearance = ATMAppearance(defaults: defaults)
        XCTAssertEqual(appearance.themeMode, .system)

        appearance.themeMode = .dark
        XCTAssertEqual(ATMAppearance(defaults: defaults).themeMode, .dark, "主题未从 UserDefaults 恢复")
    }

    func testInvalidStoredThemeFallsBackToSystem() throws {
        let suite = "ATMInvalidThemeModeTests"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set("sepia", forKey: "atmThemeMode")

        XCTAssertEqual(ATMAppearance(defaults: defaults).themeMode, .system)
    }

    func testThemeModesMapToAppKitAppearances() {
        XCTAssertNil(ATMThemeMode.system.nsAppearance)
        XCTAssertEqual(ATMThemeMode.light.nsAppearance?.name, .aqua)
        XCTAssertEqual(ATMThemeMode.dark.nsAppearance?.name, .darkAqua)
    }

    func testThemeModesKeepPreviewCardOrderAndLabels() {
        XCTAssertEqual(ATMThemeMode.allCases, [.system, .light, .dark])
        XCTAssertEqual(ATMThemeMode.allCases.map(\.label), ["跟随系统", "浅色", "深色"])
    }

    func testDesktopRailUsesDistinctLightAndDarkPalettes() throws {
        let light = try XCTUnwrap(NSAppearance(named: .aqua))
        let dark = try XCTUnwrap(NSAppearance(named: .darkAqua))
        let lightRail = try resolvedRGB(ATMTheme.railNSColor, appearance: light)
        let darkRail = try resolvedRGB(ATMTheme.railNSColor, appearance: dark)

        XCTAssertEqual(lightRail.redComponent, 244.0 / 255, accuracy: 0.001)
        XCTAssertEqual(lightRail.greenComponent, 247.0 / 255, accuracy: 0.001)
        XCTAssertEqual(lightRail.blueComponent, 251.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkRail.redComponent, 18.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkRail.greenComponent, 25.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkRail.blueComponent, 42.0 / 255, accuracy: 0.001)
    }

    func testListPaneUsesOpaqueCoolNeutralPalette() throws {
        let light = try XCTUnwrap(NSAppearance(named: .aqua))
        let dark = try XCTUnwrap(NSAppearance(named: .darkAqua))
        let lightPane = try resolvedRGB(ATMTheme.listPaneNSColor, appearance: light)
        let darkPane = try resolvedRGB(ATMTheme.listPaneNSColor, appearance: dark)

        XCTAssertEqual(lightPane.redComponent, 247.0 / 255, accuracy: 0.001)
        XCTAssertEqual(lightPane.greenComponent, 249.0 / 255, accuracy: 0.001)
        XCTAssertEqual(lightPane.blueComponent, 252.0 / 255, accuracy: 0.001)
        XCTAssertEqual(lightPane.alphaComponent, 1, accuracy: 0.001)
        XCTAssertEqual(darkPane.redComponent, 27.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkPane.greenComponent, 29.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkPane.blueComponent, 34.0 / 255, accuracy: 0.001)
        XCTAssertEqual(darkPane.alphaComponent, 1, accuracy: 0.001)
    }

    private func resolvedRGB(_ color: NSColor, appearance: NSAppearance) throws -> NSColor {
        var resolved: NSColor?
        appearance.performAsCurrentDrawingAppearance {
            resolved = color.usingColorSpace(.deviceRGB)
        }
        return try XCTUnwrap(resolved)
    }
}
