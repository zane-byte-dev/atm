import SwiftUI

/// 任务、收集和 Agent 共用的中栏标题。三者都是同一种「导航抽屉」，标题区不应该
/// 因为业务不同而各自长出卡片、分隔线或额外的副标题层级。
struct ATMDrawerHeader<Trailing: View>: View {
    let title: String
    let count: Int
    let trailing: Trailing

    init(title: String, count: Int, @ViewBuilder trailing: () -> Trailing) {
        self.title = title
        self.count = count
        self.trailing = trailing()
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Text(title)
                        .font(ATMFont.font(.title2, weight: .semibold))
                    Text(String(count))
                        .font(ATMFont.mono(.footnote, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            Spacer()
            trailing
        }
        .padding(.horizontal, 16)
        .padding(.top, 18)
        .padding(.bottom, 14)
    }
}

/// 抽屉分组的固定视觉语法：折叠箭头、语义色、标题和紧随标题的数量。
/// 末尾操作（例如来源菜单）由调用方放在同一个 HStack 的尾部。
struct ATMDrawerDisclosureLabel: View {
    let title: String
    let count: Int
    let tint: Color
    let isExpanded: Bool
    var systemImage: String?

    var body: some View {
        HStack(spacing: 7) {
            Image(systemName: "chevron.right")
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .rotationEffect(.degrees(isExpanded ? 90 : 0))

            if let systemImage {
                Image(systemName: systemImage)
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(tint)
                    .frame(width: 13)
            } else {
                Circle()
                    .fill(tint)
                    .frame(width: 6, height: 6)
            }

            Text(title)
                .lineLimit(1)

            Text(String(count))
                .font(ATMFont.mono(.caption, .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(ATMTheme.controlFill, in: Capsule())
        }
        .font(ATMFont.font(.footnote, weight: .semibold))
    }
}

/// 列表行与导航行的选中/悬停表面。
///
/// 选中态**只用填充**：不描边，也不在选中时切字重。两者都会让行内文字重排——描边挤掉
/// 一圈可用宽度，字重切换改变字形宽度——结果是点一下行标题就左右抖一下。此前五个列表面
/// 各自实现了一遍，填充漂到 0.09–0.12 与系统原生实心 accent 四种，圆角漂到 6/7/8 且
/// continuous 与非 continuous 混用，hover 只有两处有。行内容仍归各自的视图，这里只负责表面。
enum ATMRowSurface {
    /// 中栏内容行：任务、Agent、知识、收集记录、搜索结果。
    case content
    /// 导航行：侧栏分区、收集来源。
    case navigation
    /// 二级导航行：侧栏「知识」下的知识库。比 `navigation` 再淡一档，撑出层级。
    case nestedNavigation

    var cornerRadius: CGFloat {
        switch self {
        case .content: return 8
        case .navigation: return 7
        case .nestedNavigation: return 6
        }
    }

    var selectedFillOpacity: Double {
        switch self {
        case .content: return 1
        case .navigation: return 0.12
        case .nestedNavigation: return 0.09
        }
    }

    var horizontalPadding: CGFloat {
        switch self {
        case .content: return 10
        case .navigation: return 10
        case .nestedNavigation: return 8
        }
    }

    var verticalPadding: CGFloat {
        switch self {
        case .content: return 7
        case .navigation, .nestedNavigation: return 0
        }
    }

    /// 导航行靠最小高度而不是纵向内边距定高，行高才不随标签行数变化。
    var minHeight: CGFloat? {
        switch self {
        case .content: return nil
        case .navigation: return 32
        case .nestedNavigation: return 25
        }
    }
}

private struct ATMRowSurfaceModifier: ViewModifier {
    let surface: ATMRowSurface
    let isSelected: Bool

    @State private var isHovered = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, surface.horizontalPadding)
            .padding(.vertical, surface.verticalPadding)
            .frame(
                maxWidth: .infinity,
                minHeight: surface.minHeight,
                alignment: .leading
            )
            .background(fill, in: RoundedRectangle(cornerRadius: surface.cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: surface.cornerRadius, style: .continuous)
                    .stroke(selectionBorder)
            }
            .shadow(color: selectionShadow, radius: 9, y: 3)
            .contentShape(Rectangle())
            .onHover { isHovered = $0 }
            .accessibilityValue(isSelected ? "已选择" : "")
            .accessibilityAddTraits(isSelected ? .isSelected : [])
    }

    /// 选中优先于 hover——否则悬停在已选中行上会叠成第三种颜色。
    private var fill: Color {
        if isSelected {
            return surface == .content
                ? ATMTheme.elevated
                : ATMTheme.accent.opacity(surface.selectedFillOpacity)
        }
        if isHovered { return ATMTheme.primary.opacity(0.04) }
        return .clear
    }

    private var selectionBorder: Color {
        isSelected && surface == .content ? ATMTheme.borderStrong : .clear
    }

    private var selectionShadow: Color {
        isSelected && surface == .content ? Color.black.opacity(0.08) : .clear
    }
}

extension View {
    /// 套上统一的行表面：内边距、圆角、选中填充、hover 填充、命中区域与选中态无障碍标注。
    func atmRowSurface(_ surface: ATMRowSurface = .content, isSelected: Bool) -> some View {
        modifier(ATMRowSurfaceModifier(surface: surface, isSelected: isSelected))
    }
}
