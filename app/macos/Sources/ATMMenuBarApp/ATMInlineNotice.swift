import AppKit
import SwiftUI

enum ATMNoticeSeverity {
    case info
    case warning
    case error

    fileprivate var color: Color {
        switch self {
        case .info: return ATMTheme.accent
        case .warning: return ATMTheme.warning
        case .error: return ATMTheme.danger
        }
    }

    fileprivate var fill: Color {
        switch self {
        case .info: return ATMTheme.accentFill
        case .warning: return ATMTheme.warningFill
        case .error: return ATMTheme.dangerFill
        }
    }

    fileprivate var systemImage: String {
        switch self {
        case .info: return "info.circle.fill"
        case .warning: return "exclamationmark.triangle.fill"
        case .error: return "xmark.octagon.fill"
        }
    }
}

/// 工作区统一的内联提示。
///
/// 默认只给结论和下一步；CLI / JSON 原文收进限高详情区。后台错误因此不会再把中栏挤成
/// 一整屏日志，同时仍保留排障需要的完整文本、复制和重试能力。
struct ATMInlineNotice: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let severity: ATMNoticeSeverity
    let title: String
    let message: String
    let details: String?
    let actionTitle: String?
    let actionSystemImage: String
    let isActionEnabled: Bool
    let onAction: (() -> Void)?
    let onDismiss: (() -> Void)?

    @State private var isShowingDetails = false
    @State private var didCopy = false

    init(
        severity: ATMNoticeSeverity,
        title: String,
        message: String,
        details: String? = nil,
        actionTitle: String? = nil,
        actionSystemImage: String = "arrow.clockwise",
        isActionEnabled: Bool = true,
        onAction: (() -> Void)? = nil,
        onDismiss: (() -> Void)? = nil
    ) {
        self.severity = severity
        self.title = title
        self.message = message
        self.details = details?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.actionTitle = actionTitle
        self.actionSystemImage = actionSystemImage
        self.isActionEnabled = isActionEnabled
        self.onAction = onAction
        self.onDismiss = onDismiss
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: severity.systemImage)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(severity.color)
                .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: 7) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(title)
                        .font(ATMFont.font(.footnote, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                    Spacer(minLength: 4)
                    if let onDismiss {
                        Button(action: onDismiss) {
                            Image(systemName: "xmark")
                                .font(ATMFont.font(.caption, weight: .semibold))
                                .foregroundStyle(ATMTheme.secondary)
                                .frame(width: 20, height: 20)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .help("忽略")
                    }
                }

                Text(message)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(isShowingDetails ? nil : 2)
                    .fixedSize(horizontal: false, vertical: true)

                HStack(spacing: 12) {
                    if let actionTitle, let onAction {
                        Button(action: onAction) {
                            Label(actionTitle, systemImage: actionSystemImage)
                                .font(ATMFont.font(.caption, weight: .semibold))
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(severity.color)
                        .disabled(!isActionEnabled)
                    }
                    if hasDetails {
                        Button(isShowingDetails ? "收起详情" : "查看详情") {
                            withAnimation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion)) {
                                isShowingDetails.toggle()
                            }
                        }
                        .buttonStyle(.plain)
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                    }
                    Spacer(minLength: 0)
                }

                if isShowingDetails, let details, !details.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        ScrollView {
                            Text(details)
                                .font(ATMFont.mono(.caption))
                                .foregroundStyle(ATMTheme.secondary)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(maxHeight: 140)
                        .padding(8)
                        .background(ATMTheme.elevated.opacity(0.72), in: RoundedRectangle(cornerRadius: 6))

                        Button {
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString(details, forType: .string)
                            didCopy = true
                        } label: {
                            Label(didCopy ? "已复制" : "复制详情", systemImage: didCopy ? "checkmark" : "doc.on.doc")
                                .font(ATMFont.font(.caption, weight: .semibold))
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(didCopy ? ATMTheme.success : ATMTheme.secondary)
                    }
                    .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
        }
        .padding(11)
        .background(severity.fill, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 9, style: .continuous)
                .stroke(severity.color.opacity(0.20))
        }
        .onChange(of: details) { _ in
            isShowingDetails = false
            didCopy = false
        }
    }

    private var hasDetails: Bool {
        guard let details, !details.isEmpty else { return false }
        return details != message
    }
}

struct ATMErrorPresentation: Equatable {
    let title: String
    let message: String

    static func resolve(_ rawValue: String, fallbackTitle: String = "操作失败") -> Self {
        let compact = ATMErrorText.compact(rawValue, limit: 180)
        let lowercased = rawValue.lowercased()

        if lowercased.contains("network_unreachable")
            || lowercased.contains("cannot connect")
            || lowercased.contains("i/o timeout")
            || lowercased.contains("lookup ")
            || lowercased.contains("mcp server") {
            return Self(
                title: "网络连接失败",
                message: "无法连接到服务。请检查网络、代理或 DNS 设置后重试。"
            )
        }
        if lowercased.contains("timed out") || lowercased.contains("timeout") {
            return Self(
                title: "请求超时",
                message: "服务暂时没有响应，请稍后重试。"
            )
        }
        if lowercased.contains("unauthorized")
            || lowercased.contains("authentication")
            || lowercased.contains("permission denied") {
            return Self(
                title: "登录或权限失效",
                message: "请检查登录状态和访问权限后重试。"
            )
        }
        return Self(title: fallbackTitle, message: compact)
    }
}
