import SwiftUI

/// ATM UI Language 的基础度量。页面层只组合这些语义 token，不再按页面新增相近值。
enum ATMSpacing {
    static let xSmall: CGFloat = 4
    static let small: CGFloat = 8
    static let medium: CGFloat = 12
    static let large: CGFloat = 16
    static let xLarge: CGFloat = 24
    static let xxLarge: CGFloat = 32
}

enum ATMRadius {
    static let control: CGFloat = 6
    static let row: CGFloat = 10
    static let panel: CGFloat = 12
    static let feature: CGFloat = 16
}

enum ATMStroke {
    static let regular: CGFloat = 1
    static let strong: CGFloat = 1
}

/// 三栏工作区的统一宽度契约。业务页面只能调整 detail 的最小宽度，不能各自改变中栏节奏。
enum ATMWorkspaceLayout {
    static let navigatorDefaultWidth: CGFloat = 336
    static let navigatorMinWidth: CGFloat = 300
    static let navigatorMaxWidth: CGFloat = 420
    static let objectDetailMinWidth: CGFloat = 420
    static let readingDetailMinWidth: CGFloat = 440
    static let readingColumnMaxWidth: CGFloat = 900
}

/// One quiet, continuous card for the body below a detail header. The header
/// remains a flat, fixed band separated from this scrolling content by a rule.
struct ATMDetailBodySurface<Content: View>: View {
    let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        content
            .environment(\.atmWorkspaceCardPresentation, .embeddedSection)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(ATMTheme.elevated)
            .clipShape(RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
                    .stroke(ATMTheme.border, lineWidth: ATMStroke.regular)
            }
            .shadow(color: .black.opacity(0.035), radius: 8, y: 2)
            .padding(ATMDetailLayout.surfaceInset)
            .background(ATMTheme.canvas)
    }
}

/// GroupedNavigator 的固定视觉契约。中栏页面只填内容，不再自行决定纵向节奏和选中态。
enum ATMGroupedNavigatorMetrics {
    static let headerHeight: CGFloat = 64
    static let groupHeight: CGFloat = 32
    static let rowMinHeight: CGFloat = 64
    static let rowCornerRadius: CGFloat = ATMRadius.row
    static let selectedFillOpacity: Double = 0.08
    static let headerHorizontalInset: CGFloat = 20
    static let contentHorizontalInset: CGFloat = 12
    static let contentVerticalInset: CGFloat = 8
    static let groupSpacing: CGFloat = 4
}

/// 知识、Agent、收集和任务共享的中栏容器。
/// Header / Group / Row 各自由对应的公共组件约束，这里统一整列的结构与底色。
struct ATMGroupedNavigator<Header: View, Content: View>: View {
    let header: Header
    let content: Content

    init(
        @ViewBuilder header: () -> Header,
        @ViewBuilder content: () -> Content
    ) {
        self.header = header()
        self.content = content()
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(ATMTheme.listPane)
    }
}

/// GroupedNavigator 自己管理滚动与内容边距，避免系统 Sidebar List 注入额外缩进、
/// 顶部分隔线和不可控的 Section 间距。
struct ATMGroupedNavigatorScroll<Content: View>: View {
    var spacing: CGFloat = ATMGroupedNavigatorMetrics.groupSpacing
    let content: Content

    init(
        spacing: CGFloat = ATMGroupedNavigatorMetrics.groupSpacing,
        @ViewBuilder content: () -> Content
    ) {
        self.spacing = spacing
        self.content = content()
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: spacing) {
                content
            }
            .padding(.horizontal, ATMGroupedNavigatorMetrics.contentHorizontalInset)
            .padding(.vertical, ATMGroupedNavigatorMetrics.contentVerticalInset)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// 一组标题和内容行的固定组合。组间距由 `ATMGroupedNavigatorScroll` 统一管理。
struct ATMNavigatorGroup<Header: View, Content: View>: View {
    let header: Header
    let content: Content

    init(
        @ViewBuilder header: () -> Header,
        @ViewBuilder content: () -> Content
    ) {
        self.header = header()
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// 中栏固定的第一行。内容可以是标题或紧凑分段，但高度、边界和左右基线只有这一份。
struct ATMNavigatorHeader<Leading: View, Trailing: View>: View {
    let leading: Leading
    let trailing: Trailing

    init(
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.leading = leading()
        self.trailing = trailing()
    }

    var body: some View {
        HStack(alignment: .center, spacing: ATMSpacing.medium) {
            leading
            Spacer(minLength: ATMSpacing.small)
            trailing
        }
        .atmDrawerHeaderRow()
    }
}

/// 中栏分组标题的完整交互壳：折叠命中区、语义色、计数和尾部动作保持一致。
struct ATMNavigatorGroupHeader<Trailing: View>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    let title: String
    let count: Int
    let tint: Color
    var systemImage: String?
    @Binding var isExpanded: Bool
    let trailing: Trailing

    init(
        title: String,
        count: Int,
        tint: Color,
        systemImage: String? = nil,
        isExpanded: Binding<Bool>,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.title = title
        self.count = count
        self.tint = tint
        self.systemImage = systemImage
        _isExpanded = isExpanded
        self.trailing = trailing()
    }

    var body: some View {
        HStack(spacing: ATMSpacing.small) {
            Button {
                withAnimation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion)) {
                    isExpanded.toggle()
                }
            } label: {
                ATMDrawerDisclosureLabel(
                    title: title,
                    count: count,
                    tint: tint,
                    isExpanded: isExpanded,
                    systemImage: systemImage
                )
                .frame(
                    maxWidth: .infinity,
                    minHeight: ATMGroupedNavigatorMetrics.groupHeight,
                    alignment: .leading
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            trailing
        }
        .textCase(nil)
    }
}

extension ATMNavigatorGroupHeader where Trailing == EmptyView {
    init(
        title: String,
        count: Int,
        tint: Color,
        systemImage: String? = nil,
        isExpanded: Binding<Bool>
    ) {
        self.init(
            title: title,
            count: count,
            tint: tint,
            systemImage: systemImage,
            isExpanded: isExpanded
        ) {
            EmptyView()
        }
    }
}

/// 中栏内容行的统一排版壳。页面提供三个插槽，组件负责对齐、间距、命中区与选中表面。
struct ATMNavigatorRow<Leading: View, Content: View, Trailing: View>: View {
    let isSelected: Bool
    let leading: Leading
    let content: Content
    let trailing: Trailing

    init(
        isSelected: Bool,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder content: () -> Content,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.isSelected = isSelected
        self.leading = leading()
        self.content = content()
        self.trailing = trailing()
    }

    var body: some View {
        HStack(alignment: .top, spacing: ATMContentRowLayout.leadingSpacing) {
            leading
            content
                .frame(maxWidth: .infinity, alignment: .leading)
                .layoutPriority(1)
            trailing
                .fixedSize(horizontal: true, vertical: false)
        }
        .atmRowSurface(isSelected: isSelected)
    }
}

extension ATMNavigatorRow where Leading == EmptyView, Trailing == EmptyView {
    init(isSelected: Bool, @ViewBuilder content: () -> Content) {
        self.init(isSelected: isSelected, leading: { EmptyView() }, content: content, trailing: { EmptyView() })
    }
}

extension ATMNavigatorRow where Trailing == EmptyView {
    init(
        isSelected: Bool,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder content: () -> Content
    ) {
        self.init(isSelected: isSelected, leading: leading, content: content, trailing: { EmptyView() })
    }
}

/// 右栏分页的固定带子。具体 tab 仍由 `ATMCapsuleTabs` 提供。
struct ATMDetailTabs<Content: View>: View {
    let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        HStack(spacing: 0) {
            content
            Spacer(minLength: 0)
        }
        .padding(.horizontal, ATMDetailLayout.tabsHorizontalPadding)
        .padding(.vertical, ATMDetailLayout.tabsVerticalPadding)
        .background(ATMTheme.canvas)
    }
}

/// Object 型详情的公共骨架：固定头、可选提示、分页和正文保持同一条纵向基线。
struct ATMDetailScaffold<Header: View, Notice: View, Tabs: View, Content: View>: View {
    let header: Header
    let notice: Notice
    let tabs: Tabs
    let content: Content

    init(
        @ViewBuilder header: () -> Header,
        @ViewBuilder notice: () -> Notice,
        @ViewBuilder tabs: () -> Tabs,
        @ViewBuilder content: () -> Content
    ) {
        self.header = header()
        self.notice = notice()
        self.tabs = tabs()
        self.content = content()
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ATMDetailTabs { tabs }
            ATMDetailBodySurface {
                VStack(spacing: 0) {
                    notice
                    content
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.clear)
    }
}

extension ATMDetailScaffold where Notice == EmptyView {
    init(
        @ViewBuilder header: () -> Header,
        @ViewBuilder tabs: () -> Tabs,
        @ViewBuilder content: () -> Content
    ) {
        self.init(header: header, notice: { EmptyView() }, tabs: tabs, content: content)
    }
}

struct ATMMetadataItem: Identifiable {
    let id: String
    let label: String
    let value: String
    let systemImage: String
    var valueColor: Color = ATMTheme.primary

    init(
        _ id: String,
        label: String,
        value: String,
        systemImage: String,
        valueColor: Color = ATMTheme.primary
    ) {
        self.id = id
        self.label = label
        self.value = value
        self.systemImage = systemImage
        self.valueColor = valueColor
    }
}

/// 详情头下面的属性条。使用 adaptive grid，窄栏自动换行而不是挤成三条细缝。
struct ATMMetadataStrip: View {
    let items: [ATMMetadataItem]

    var body: some View {
        LazyVGrid(
            columns: [GridItem(.adaptive(minimum: 132), spacing: ATMSpacing.small)],
            spacing: ATMSpacing.small
        ) {
            ForEach(items) { item in
                VStack(alignment: .leading, spacing: ATMSpacing.xSmall) {
                    Label(item.label, systemImage: item.systemImage)
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                    Text(item.value)
                        .font(ATMFont.font(.footnote, weight: .semibold))
                        .foregroundStyle(item.valueColor)
                        .lineLimit(1)
                }
                .padding(.horizontal, 11)
                .padding(.vertical, 9)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// 阅读型与对象型详情共享的 section 标题基线。
struct ATMDetailSection<Actions: View, Content: View>: View {
    let title: String
    let actions: Actions
    let content: Content

    init(
        _ title: String,
        @ViewBuilder actions: () -> Actions,
        @ViewBuilder content: () -> Content
    ) {
        self.title = title
        self.actions = actions()
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: ATMSpacing.medium) {
            HStack(alignment: .firstTextBaseline, spacing: ATMSpacing.small) {
                Text(title)
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                Spacer(minLength: ATMSpacing.small)
                actions
            }
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension ATMDetailSection where Actions == EmptyView {
    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.init(title, actions: { EmptyView() }, content: content)
    }
}

/// 数据页的标准有界容器。数据类型与图表仍由调用方决定，表面语言不再分叉。
struct ATMDataPanel<Header: View, Content: View>: View {
    @Environment(\.atmWorkspaceCardPresentation) private var presentation
    let header: Header
    let content: Content

    init(
        @ViewBuilder header: () -> Header,
        @ViewBuilder content: () -> Content
    ) {
        self.header = header()
        self.content = content()
    }

    @ViewBuilder
    var body: some View {
        switch presentation {
        case .raised:
            panelContent
                .padding(18)
                .background(
                    ATMTheme.elevated,
                    in: RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
                )
                .overlay {
                    RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
                        .stroke(ATMTheme.border, lineWidth: ATMStroke.regular)
                }
        case .embeddedSection:
            panelContent
                .padding(.vertical, 18)
                .overlay(alignment: .bottom) {
                    Rectangle()
                        .fill(ATMTheme.border.opacity(0.72))
                        .frame(height: 1)
                }
        }
    }

    private var panelContent: some View {
        VStack(alignment: .leading, spacing: ATMSpacing.medium) {
            header
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
