import AppKit
import SwiftUI

enum ATMTheme {
    static let accent = Color.accentColor
    static let white = Color(nsColor: .textBackgroundColor)
    static let canvas = Color(nsColor: .windowBackgroundColor)
    static let surface = Color(nsColor: .controlBackgroundColor)
    /// A calm, opaque middle-column surface. The old semi-transparent
    /// under-page colour was painted both by a split workspace and by its list
    /// child, so light appearance compounded into a muddy grey. Keeping the
    /// token opaque makes the three-column hierarchy stable at every nesting
    /// depth: blue-grey rail, cool neutral list, crisp document canvas.
    static let listPaneNSColor = adaptiveNSColor(
        light: rgb(247, 249, 252),
        dark: rgb(27, 29, 34)
    )
    static let listPane = Color(nsColor: listPaneNSColor)
    /// Raised rows and reading cards use the text background: in light mode it
    /// reads as a crisp sheet over listPane; in dark mode it stays system-aware.
    static let elevated = Color(nsColor: .textBackgroundColor)
    static let sidebar = Color(nsColor: .windowBackgroundColor)
    static let controlFill = Color(nsColor: .controlBackgroundColor)
    static let border = Color(nsColor: .separatorColor).opacity(0.65)
    static let borderStrong = Color(nsColor: .separatorColor).opacity(0.92)
    static let primary = Color(nsColor: .labelColor)
    static let secondary = Color(nsColor: .secondaryLabelColor)

    // Keep the rail distinctly ATM rather than flattening it into a generic
    // system list. Light appearance gets a restrained blue-grey surface; dark
    // appearance preserves the original ink-blue chrome. NSColor's dynamic
    // provider also follows live macOS changes while the app uses system mode.
    static let railNSColor = adaptiveNSColor(
        light: rgb(244, 247, 251),
        dark: rgb(18, 25, 42)
    )
    static let railRaisedNSColor = adaptiveNSColor(
        light: rgb(232, 238, 247),
        dark: rgb(30, 39, 59)
    )
    static let railSelectedNSColor = adaptiveNSColor(
        light: rgb(223, 233, 250),
        dark: rgb(42, 57, 91)
    )
    static let railPrimaryNSColor = adaptiveNSColor(
        light: rgb(27, 38, 56),
        dark: NSColor.white.withAlphaComponent(0.96)
    )
    static let railSecondaryNSColor = adaptiveNSColor(
        light: rgb(83, 97, 118),
        dark: NSColor.white.withAlphaComponent(0.64)
    )
    static let railMutedNSColor = adaptiveNSColor(
        light: rgb(124, 136, 153),
        dark: NSColor.white.withAlphaComponent(0.44)
    )
    static let railBorderNSColor = adaptiveNSColor(
        light: rgb(214, 222, 233),
        dark: NSColor.white.withAlphaComponent(0.10)
    )

    static let rail = Color(nsColor: railNSColor)
    static let railRaised = Color(nsColor: railRaisedNSColor)
    static let railSelected = Color(nsColor: railSelectedNSColor)
    static let railPrimary = Color(nsColor: railPrimaryNSColor)
    static let railSecondary = Color(nsColor: railSecondaryNSColor)
    static let railMuted = Color(nsColor: railMutedNSColor)
    static let railBorder = Color(nsColor: railBorderNSColor)

    // 状态色：好 / 警告 / 危险。全 App 只有这一套。
    //
    // 此前 `green` 和 `orange` 都是 `accentLight` 的别名，于是「警告」渲染成了蓝色
    // （搜索面板的 ⚠️ 就是蓝的），各处只好绕过主题直接写 `Color.orange` / `Color.green`,
    // 透明度散在 0.75–1.0 六个值；quotaColor / cacheHitColor 又硬编码了第三套 RGB。
    // 现在统一到 quota 卡里已经调过的这三个值——比系统 systemGreen/Orange/Red 更沉，
    // 中等明度，浅色和深色底上都读得出来。
    static let success = Color(red: 31 / 255, green: 157 / 255, blue: 104 / 255)
    static let warning = Color(red: 230 / 255, green: 139 / 255, blue: 24 / 255)
    static let danger = Color(red: 220 / 255, green: 50 / 255, blue: 67 / 255)

    // 状态色的淡底。此前 chip / banner 的底色在 0.08–0.12 之间各写各的。
    static let successFill = success.opacity(0.10)
    static let warningFill = warning.opacity(0.10)
    static let dangerFill = danger.opacity(0.10)
    static let accentFill = accent.opacity(0.10)

    // High-contrast categorical colors. The final neutral color is reserved
    // for aggregate buckets such as “其他”.
    static let palette = [
        Color(red: 52 / 255, green: 112 / 255, blue: 246 / 255),
        Color(red: 242 / 255, green: 143 / 255, blue: 43 / 255),
        Color(red: 137 / 255, green: 87 / 255, blue: 229 / 255),
        Color(red: 16 / 255, green: 166 / 255, blue: 153 / 255),
        Color(red: 230 / 255, green: 74 / 255, blue: 116 / 255),
        Color(red: 117 / 255, green: 128 / 255, blue: 145 / 255),
    ]
    static let chartPlotFill = primary.opacity(0.022)
    static let chartGrid = secondary.opacity(0.18)

    /// 连接器健康色。设置页和「添加来源」共用，别处不要再判一次状态字符串。
    static func collectionHealthColor(_ status: String?) -> Color {
        switch status {
        case "ready": return success
        case "auth_required", "permission_required", "error": return warning
        default: return secondary
        }
    }

    static func quotaColor(_ level: ATMQuotaLevel) -> Color {
        switch level {
        case .critical: return danger
        case .warning: return warning
        case .healthy: return success
        }
    }

    static func cacheHitColor(_ rate: Double) -> Color {
        switch rate {
        case ..<0.5: return danger
        case ..<0.8: return warning
        default: return success
        }
    }

    private static func adaptiveNSColor(light: NSColor, dark: NSColor) -> NSColor {
        NSColor(name: nil) { appearance in
            appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? dark : light
        }
    }

    private static func rgb(_ red: Int, _ green: Int, _ blue: Int) -> NSColor {
        NSColor(
            deviceRed: CGFloat(red) / 255,
            green: CGFloat(green) / 255,
            blue: CGFloat(blue) / 255,
            alpha: 1
        )
    }
}

extension View {
    /// ATM keeps its scroll bars out of sight. The panes are dense — nested cards,
    /// list columns, Markdown descriptions — and an overlay scroller drew a grey
    /// stripe down the edge of nearly every one of them, on top of a layout whose
    /// job is to be read at a glance.
    ///
    /// Applied at each window / panel / sheet root: the setting reaches every
    /// scroll view inside, so individual `ScrollView`s and `List`s stay clean.
    /// Horizontal bars inside Markdown code blocks and tables opt back in with
    /// `.scrollIndicators(.visible, axes: .horizontal)` — there the bar is the only
    /// hint that the content continues past the edge.
    func atmHidesScrollBars() -> some View {
        scrollIndicators(.hidden)
    }

    /// Shared raised surface for the desktop workspaces. Keeping this in one
    /// place prevents Collection, Agent, Knowledge and Settings from drifting
    /// into four subtly different card styles.
    func atmWorkspaceCard(cornerRadius: CGFloat = 12) -> some View {
        background(
            ATMTheme.elevated,
            in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                .stroke(ATMTheme.border, lineWidth: 1)
        }
        .shadow(color: .black.opacity(0.035), radius: 8, y: 2)
    }
}
