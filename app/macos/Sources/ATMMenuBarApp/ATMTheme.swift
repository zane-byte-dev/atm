import AppKit
import SwiftUI

enum ATMTheme {
    static let accent = Color.accentColor
    static let white = Color(nsColor: .textBackgroundColor)
    static let canvas = Color(nsColor: .windowBackgroundColor)
    static let surface = Color(nsColor: .controlBackgroundColor)
    /// Quietly separates list columns from document/detail canvases without
    /// introducing another saturated brand color.
    static let listPane = Color(nsColor: .underPageBackgroundColor).opacity(0.22)
    /// Raised rows and reading cards use the text background: in light mode it
    /// reads as a crisp sheet over listPane; in dark mode it stays system-aware.
    static let elevated = Color(nsColor: .textBackgroundColor)
    static let sidebar = Color(nsColor: .windowBackgroundColor)
    static let controlFill = Color(nsColor: .controlBackgroundColor)
    static let border = Color(nsColor: .separatorColor).opacity(0.65)
    static let borderStrong = Color(nsColor: .separatorColor).opacity(0.92)
    static let primary = Color(nsColor: .labelColor)
    static let secondary = Color(nsColor: .secondaryLabelColor)

    // The desktop app rail is deliberately tonal rather than another system
    // sidebar. It gives ATM one stable piece of chrome while the working panes
    // continue to follow the user's light/dark appearance.
    static let rail = Color(red: 18 / 255, green: 25 / 255, blue: 42 / 255)
    static let railRaised = Color(red: 30 / 255, green: 39 / 255, blue: 59 / 255)
    static let railSelected = Color(red: 42 / 255, green: 57 / 255, blue: 91 / 255)
    static let railPrimary = Color.white.opacity(0.96)
    static let railSecondary = Color.white.opacity(0.62)
    static let railMuted = Color.white.opacity(0.42)
    static let railBorder = Color.white.opacity(0.10)

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
}

extension View {
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
