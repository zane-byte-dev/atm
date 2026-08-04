import SwiftUI

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
        case .content: return 0.11
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
            .contentShape(Rectangle())
            .onHover { isHovered = $0 }
            .accessibilityValue(isSelected ? "已选择" : "")
            .accessibilityAddTraits(isSelected ? .isSelected : [])
    }

    /// 选中优先于 hover——否则悬停在已选中行上会叠成第三种颜色。
    private var fill: Color {
        if isSelected { return ATMTheme.accent.opacity(surface.selectedFillOpacity) }
        if isHovered { return ATMTheme.primary.opacity(0.04) }
        return .clear
    }
}

extension View {
    /// 套上统一的行表面：内边距、圆角、选中填充、hover 填充、命中区域与选中态无障碍标注。
    func atmRowSurface(_ surface: ATMRowSurface = .content, isSelected: Bool) -> some View {
        modifier(ATMRowSurfaceModifier(surface: surface, isSelected: isSelected))
    }
}
