import AppKit
import SwiftUI

/// Compact icon control used for toolbars and action chips.
///
/// macOS `.borderless` / `.plain` buttons do not draw hover themselves; this
/// view owns the hover fill, tooltip, and accessibility label so every action
/// chip stays consistent.
struct ATMIconButton: View {
    /// 按**动作性质**选，不按页面选：此前同一个「更多操作」溢出菜单在任务页是常驻实底、
    /// 在收集 / 知识 / 快捷面板是 hover 才出底，同一个齿轮设置图标又比旁边的刷新多一圈底。
    enum Chrome {
        /// 常驻实底。只给「新建 / 添加」这类创建主操作——它需要在一排安静的工具图标里
        /// 自带重量。
        case chip
        /// hover 才出底。其余全部：刷新、设置、复制、编辑，以及 `…` 溢出菜单。
        case bare
    }

    let systemImage: String
    let help: String
    var chrome: Chrome = .chip
    var isEnabled: Bool = true
    /// When true, idle foreground uses accent (active archive, copied, etc.).
    var isEmphasized: Bool = false
    var side: CGFloat = 28
    var iconTier: ATMFont.Tier = .bodyLarge
    let action: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(ATMFont.font(iconTier, weight: .medium))
                .foregroundStyle(foreground)
                .frame(width: side, height: side)
                .contentShape(Rectangle())
                .background(backgroundFill, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
        .disabled(!isEnabled)
        .onHover { hovering in
            isHovered = isEnabled && hovering
        }
        .onChange(of: isEnabled) { enabled in
            if !enabled { isHovered = false }
        }
        .animation(.easeInOut(duration: 0.12), value: isHovered)
    }

    private var foreground: Color {
        guard isEnabled else { return ATMTheme.secondary.opacity(0.45) }
        if isHovered { return ATMTheme.primary }
        return isEmphasized ? ATMTheme.accent : ATMTheme.secondary
    }

    private var backgroundFill: Color {
        switch ATMIconButtonChrome.background(
            isHovered: isHovered,
            isEnabled: isEnabled,
            chrome: chrome
        ) {
        case .clear:
            return .clear
        case .controlFill(let opacity):
            return ATMTheme.controlFill.opacity(opacity)
        case .primaryOverlay(let opacity):
            return ATMTheme.primary.opacity(opacity)
        }
    }
}

/// Icon-only `Menu` label that matches `ATMIconButton` hover chrome.
struct ATMIconMenuLabel: View {
    let systemImage: String
    let help: String
    var chrome: ATMIconButton.Chrome = .bare
    var isEnabled: Bool = true
    var side: CGFloat = 30
    var iconTier: ATMFont.Tier = .bodyLarge

    @State private var isHovered = false

    var body: some View {
        Image(systemName: systemImage)
            .font(ATMFont.font(iconTier, weight: .medium))
            .foregroundStyle(foreground)
            .frame(width: side, height: side)
            .contentShape(Rectangle())
            .background(backgroundFill, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .help(help)
            .accessibilityLabel(help)
            .onHover { hovering in
                isHovered = isEnabled && hovering
            }
            .animation(.easeInOut(duration: 0.12), value: isHovered)
    }

    private var foreground: Color {
        guard isEnabled else { return ATMTheme.secondary.opacity(0.45) }
        return isHovered ? ATMTheme.primary : ATMTheme.secondary
    }

    private var backgroundFill: Color {
        switch ATMIconButtonChrome.background(
            isHovered: isHovered,
            isEnabled: isEnabled,
            chrome: chrome
        ) {
        case .clear:
            return .clear
        case .controlFill(let opacity):
            return ATMTheme.controlFill.opacity(opacity)
        case .primaryOverlay(let opacity):
            return ATMTheme.primary.opacity(opacity)
        }
    }
}

/// Full-width or labeled secondary control with hover fill (panel footers, filters).
struct ATMHoverLabelButton: View {
    let title: String
    var systemImage: String? = nil
    var trailingSystemImage: String? = nil
    let help: String
    var isEnabled: Bool = true
    var height: CGFloat = 34
    var tier: ATMFont.Tier = .body
    let action: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                if let systemImage {
                    Image(systemName: systemImage)
                }
                Text(title)
                    .lineLimit(1)
                Spacer(minLength: 0)
                if let trailingSystemImage {
                    // Chevrons and disclosure marks read heavy at label size.
                    Image(systemName: trailingSystemImage)
                        .font(ATMFont.font(.caption, weight: .semibold))
                }
            }
            .font(ATMFont.font(tier, weight: .semibold))
            .foregroundStyle(foreground)
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity, minHeight: height, maxHeight: height, alignment: .leading)
            .contentShape(Rectangle())
            .background(backgroundFill, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
        .disabled(!isEnabled)
        .onHover { hovering in
            isHovered = isEnabled && hovering
        }
        .onChange(of: isEnabled) { enabled in
            if !enabled { isHovered = false }
        }
        .animation(.easeInOut(duration: 0.12), value: isHovered)
    }

    private var foreground: Color {
        guard isEnabled else { return ATMTheme.secondary.opacity(0.45) }
        return isHovered ? ATMTheme.primary : ATMTheme.secondary
    }

    private var backgroundFill: Color {
        if !isEnabled {
            return ATMTheme.controlFill.opacity(0.55)
        }
        if isHovered {
            return ATMTheme.primary.opacity(0.08)
        }
        return ATMTheme.controlFill
    }
}

/// Pure policy for hover fill — unit-tested so chip/bare stay distinguishable.
enum ATMIconButtonChrome {
    enum Fill: Equatable {
        case clear
        case controlFill(opacity: Double)
        case primaryOverlay(opacity: Double)
    }

    static func background(
        isHovered: Bool,
        isEnabled: Bool,
        chrome: ATMIconButton.Chrome
    ) -> Fill {
        guard isEnabled else {
            return chrome == .chip ? .controlFill(opacity: 0.55) : .clear
        }
        if isHovered {
            return .primaryOverlay(opacity: chrome == .chip ? 0.10 : 0.07)
        }
        return chrome == .chip ? .controlFill(opacity: 1) : .clear
    }
}
