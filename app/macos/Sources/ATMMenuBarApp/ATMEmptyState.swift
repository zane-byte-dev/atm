import SwiftUI

/// 「这里什么都没有」的唯一画法。
///
/// 此前七处各写一遍：图标在 `.display` / `.metric` / `.title1` 之间漂，行距 8/9/10，
/// 内边距 0/24/28，标题在 body / bodyLarge × medium / semibold 四种组合里各挑一种——
/// 同一句「暂无内容」在不同页面是三种大小。现在只剩两档，按**位置**选，不按页面选。
struct ATMEmptyState: View {
    /// 占满一整栏（中栏列表、右栏详情、搜索面板）用 `.pane`；
    /// 卡片内部的占位用 `.inline`——它不该比卡片自己的标题还抢眼。
    enum Size {
        case pane
        case inline

        var iconTier: ATMFont.Tier { self == .pane ? .display : .metric }
        var titleTier: ATMFont.Tier { self == .pane ? .bodyLarge : .body }
        var titleColor: Color { self == .pane ? ATMTheme.primary : ATMTheme.secondary }
        var spacing: CGFloat { self == .pane ? 9 : 8 }
        var padding: CGFloat { self == .pane ? 24 : 16 }
    }

    let icon: String
    let title: String
    var detail: String? = nil
    var size: Size = .pane
    /// 暖色图标留给「不是空，是坏了」——例如搜索后端部分不可用。
    var isWarning: Bool = false
    var detailLineLimit: Int? = 5
    /// 默认撑满可用空间。卡片内的占位给一个 `minHeight`，让卡片自己定高。
    var minHeight: CGFloat? = nil
    var actionTitle: String? = nil
    var action: (() -> Void)? = nil

    var body: some View {
        VStack(spacing: size.spacing) {
            Image(systemName: icon)
                .font(ATMFont.font(size.iconTier, weight: .light))
                .foregroundStyle(isWarning ? ATMTheme.warning : ATMTheme.secondary)

            Text(title)
                .font(ATMFont.font(size.titleTier, weight: .semibold))
                .foregroundStyle(size.titleColor)
                .multilineTextAlignment(.center)

            if let detail, !detail.isEmpty {
                Text(detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .multilineTextAlignment(.center)
                    .lineLimit(detailLineLimit)
            }

            if let actionTitle, let action {
                Button(actionTitle, action: action)
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .padding(.top, 2)
            }
        }
        .padding(size.padding)
        .frame(
            maxWidth: .infinity,
            minHeight: minHeight,
            maxHeight: minHeight == nil ? .infinity : nil
        )
    }
}
