import SwiftUI

enum ATMNavigatorPresentation: String {
    case grouped
    case flat

    static func resolve(_ storedValue: String) -> Self {
        Self(rawValue: storedValue) ?? .grouped
    }

    var toggled: Self {
        self == .grouped ? .flat : .grouped
    }
}

enum ATMNavigatorPresentationPreferences {
    static let defaultValue = ATMNavigatorPresentation.grouped.rawValue
    static let tasksKey = "ATMTaskListPresentation"
    static let collectionKey = "ATMCollectionRecordListPresentation"
    static let agentsKey = "ATMLiveAgentListPresentation"
    static let knowledgeKey = "ATMKnowledgeArticleListPresentation"
}

/// Every grouped middle-column list uses the same compact, destination-oriented
/// toggle. Each workspace owns its storage key so changing one list does not
/// unexpectedly rearrange the others.
struct ATMNavigatorPresentationToggle: View {
    @Binding var storedValue: String

    private var presentation: ATMNavigatorPresentation {
        ATMNavigatorPresentation.resolve(storedValue)
    }

    var body: some View {
        ATMIconButton(
            systemImage: presentation == .grouped ? "list.bullet" : "list.bullet.indent",
            help: presentation == .grouped ? "切换为平铺" : "切换为分组",
            chrome: .bare,
            side: 30,
            iconTier: .bodyLarge
        ) {
            storedValue = presentation.toggled.rawValue
        }
    }
}

/// 任务栏的中栏标题。只有标题本身承载状态（任务 / 归档）、尾部挂着真操作的抽屉才配
/// 这一层——纯复读左侧栏选中项的标题一律不画，收集 / 知识 / Agent 都直接从段控起头。
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
        ATMNavigatorHeader {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text(title)
                    .font(ATMFont.font(.title2, weight: .semibold))
                Text(String(count))
                    .font(ATMFont.mono(.footnote, .semibold))
                    .foregroundStyle(ATMTheme.secondary)
            }
        } trailing: {
            trailing
        }
    }
}

extension View {
    /// 中栏头部的统一外框：左右 16pt，纵向居中于 `drawerHeaderHeight`。
    ///
    /// 任务用大标题、收集 / 知识 / Agent 用段控，但四者都是同一条「中栏第一行」——
    /// 高度和纵向居中必须一致，否则切页时下面的列表会整体上下平移。内容各自决定，
    /// 这里只负责这一条带子本身。
    func atmDrawerHeaderRow() -> some View {
        padding(.horizontal, ATMGroupedNavigatorMetrics.headerHorizontalInset)
            .frame(height: ATMGroupedNavigatorMetrics.headerHeight)
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
        }
        .font(ATMFont.font(.footnote, weight: .medium))
    }
}

/// 中栏标题区的双态 / 多态切换。视觉跟 macOS 的紧凑 segmented control 一致：
/// 一块安静的底板（`segmentTrack`，比 listPane 沉一档）承载所有选项，当前项用抬升的
/// 实心表面表示，不再用网页式下划线。
/// 固定等宽，只适合「文章 / 知识库」这类短标签；详情页长标签用 `ATMCapsuleTabs`。
struct ATMCompactSegmentedTabs<Selection: Hashable>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Binding var selection: Selection
    let items: [(value: Selection, title: String)]

    private let segmentWidth: CGFloat = 56
    private let segmentHeight: CGFloat = 24

    private var selectedIndex: Int {
        items.firstIndex { $0.value == selection } ?? 0
    }

    var body: some View {
        ZStack(alignment: .leading) {
            // 始终是同一个实体视图，只改变横向位置。相比在两个 Button 中条件创建背景，
            // 这在 macOS 上不会被当成一次无动画的视图替换。
            RoundedRectangle(cornerRadius: ATMRadius.control, style: .continuous)
                .fill(ATMTheme.rowSelected)
                .frame(width: segmentWidth, height: segmentHeight)
                .overlay {
                    RoundedRectangle(cornerRadius: ATMRadius.control, style: .continuous)
                        .stroke(ATMTheme.border.opacity(0.75))
                }
                .shadow(color: .black.opacity(0.07), radius: 2, y: 1)
                .offset(x: CGFloat(selectedIndex) * segmentWidth)
                .allowsHitTesting(false)

            HStack(spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    let isSelected = selection == item.value
                    Button {
                        withAnimation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion)) {
                            selection = item.value
                        }
                    } label: {
                        Text(item.title)
                            // 选中前后保持相同字重，否则文字度量变化会让整个控件横向跳动。
                            .font(ATMFont.font(.footnote, weight: .medium))
                            .foregroundStyle(isSelected ? ATMTheme.primary : ATMTheme.secondary)
                            .frame(width: segmentWidth, height: segmentHeight)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(isSelected ? .isSelected : [])
                }
            }
        }
        .padding(2)
        .background(ATMTheme.segmentTrack, in: RoundedRectangle(cornerRadius: ATMRadius.row, style: .continuous))
        .animation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion), value: selectedIndex)
    }
}

/// 详情页分页：iOS 式可变宽度胶囊分段。
///
/// 浅灰整条胶囊作底板（`segmentTrack`），选中项是浮在上面的白胶囊（软阴影、无描边），
/// 未选中只剩灰色文案。底板必须自带灰度：右栏是 canvas，用系统 `controlFill` 时底板与
/// 选中块在浅色下都是纯白，整条控件会跟背景叠成一块白，选中态只剩阴影可辨。
/// 标签随文案伸缩，适合「执行动态」「Agent Sessions 3」这类长度不一的标题；
/// 中栏短标签仍用固定等宽的 `ATMCompactSegmentedTabs`。
struct ATMCapsuleTabs<Selection: Hashable>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Binding var selection: Selection
    let items: [(value: Selection, title: String)]

    private let segmentHeight: CGFloat = 28

    var body: some View {
        HStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                let isSelected = selection == item.value
                Button {
                    withAnimation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion)) {
                        selection = item.value
                    }
                } label: {
                    Text(item.title)
                        // 选中前后保持相同字重，避免胶囊宽度跟着字重跳。
                        .font(ATMFont.font(.footnote, weight: .medium))
                        .foregroundStyle(isSelected ? ATMTheme.primary : ATMTheme.secondary)
                        .padding(.horizontal, 14)
                        .frame(height: segmentHeight)
                        .background {
                            if isSelected {
                                Capsule()
                                    .fill(ATMTheme.rowSelected)
                                    .shadow(color: .black.opacity(0.10), radius: 3, y: 1)
                                    .shadow(color: .black.opacity(0.04), radius: 0.5, y: 0)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
        }
        .padding(3)
        .background(ATMTheme.segmentTrack, in: Capsule())
        .animation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion), value: selection)
    }
}

/// 列表行与导航行的选中/悬停表面。
///
/// 选中态**只用填充**：不描边、不投影，也不在选中时切字重。描边和字重切换都会让行内文字
/// 重排（描边挤掉一圈可用宽度、字重改变字形宽度），结果是点一下行标题就左右抖一下；投影则
/// 让每张选中卡在安静的中栏里浮起一层，中栏是导航抽屉而不是叠了卡片的画布，一块干净的实底
/// 就够说明「在读这条」。此前五个列表面各自实现了一遍，填充漂到 0.09–0.12 与系统原生实心
/// accent 四种，圆角漂到 6/7/8 且 continuous 与非 continuous 混用，hover 只有两处有。
/// 行内容仍归各自的视图，这里只负责表面。
enum ATMRowSurface {
    /// 中栏内容行：任务、Agent、知识、收集记录、搜索结果。
    case content
    /// 导航行：侧栏分区、收集来源。
    case navigation
    /// 二级导航行：侧栏「知识」下的知识库。比 `navigation` 再淡一档，撑出层级。
    case nestedNavigation

    var cornerRadius: CGFloat {
        switch self {
        case .content: return ATMGroupedNavigatorMetrics.rowCornerRadius
        case .navigation: return ATMRadius.row
        case .nestedNavigation: return ATMRadius.control
        }
    }

    var selectedFillOpacity: Double {
        ATMGroupedNavigatorMetrics.selectedFillOpacity
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
        case .content: return ATMGroupedNavigatorMetrics.rowMinHeight
        case .navigation: return ATMGroupedNavigatorMetrics.groupHeight
        case .nestedNavigation: return 25
        }
    }
}

/// 中栏内容条目的公共排版度量。
///
/// `ATMRowSurface` 负责卡片里面的 padding 和选中表面；这里负责卡片在中栏里的外边距，
/// 以及有前导图标时的文字起点。两层都集中后，`List` 与 `LazyVStack` 才不会各自长出
/// 一套看起来相近、实际相差几 pt 的布局。
enum ATMContentRowLayout {
    static let outerHorizontalPadding: CGFloat = ATMSpacing.small
    static let outerVerticalPadding: CGFloat = 2
    static let leadingVisualSize: CGFloat = 24
    static let leadingSpacing: CGFloat = ATMSpacing.small
    static let contentSpacing: CGFloat = ATMSpacing.xSmall

    static var listInsets: EdgeInsets {
        EdgeInsets(
            top: outerVerticalPadding,
            leading: outerHorizontalPadding,
            bottom: outerVerticalPadding,
            trailing: outerHorizontalPadding
        )
    }
}

private struct ATMRowSurfaceModifier: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
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
            .animation(ATMMotion.resolved(ATMMotion.hover, reduceMotion: reduceMotion), value: isHovered)
            .animation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion), value: isSelected)
            .accessibilityValue(isSelected ? "已选择" : "")
            .accessibilityAddTraits(isSelected ? .isSelected : [])
    }

    /// 选中优先于 hover——否则悬停在已选中行上会叠成第三种颜色。
    private var fill: Color {
        if isSelected {
            return ATMTheme.accent.opacity(surface.selectedFillOpacity)
        }
        if isHovered { return ATMTheme.primary.opacity(0.04) }
        return .clear
    }
}

/// 行按钮：点击时不给任何按下反馈。
///
/// macOS 的 `.plain` 会在按下期间把整个 label 调暗一档，落在中栏行上就是「点一下标题先灰
/// 一下、松手才变选中」——行本身已经用选中表面回应了这次点击，按下态的调暗只是让文字闪一下
/// 脏。命中区域和 hover 由 `atmRowSurface` 负责，这里只需要把 label 原样交回去。
struct ATMRowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
    }
}

extension ButtonStyle where Self == ATMRowButtonStyle {
    static var atmRow: ATMRowButtonStyle { ATMRowButtonStyle() }
}

extension View {
    /// 套上统一的行表面：内边距、圆角、选中填充、hover 填充、命中区域与选中态无障碍标注。
    func atmRowSurface(_ surface: ATMRowSurface = .content, isSelected: Bool) -> some View {
        modifier(ATMRowSurfaceModifier(surface: surface, isSelected: isSelected))
    }

    /// `List` 中栏条目的统一外边距与透明行底色。
    func atmContentListRow() -> some View {
        listRowInsets(ATMContentRowLayout.listInsets)
            .listRowBackground(Color.clear)
    }

    /// `ScrollView` / `LazyVStack` 中栏条目的统一纵向外边距。
    /// 水平 8pt 由滚动容器统一提供，知识库条目还会叠加有语义的层级缩进。
    func atmContentStackRow() -> some View {
        padding(.vertical, ATMContentRowLayout.outerVerticalPadding)
    }
}
