import AppKit
import SwiftUI

private enum CollectionDrawerTab: String {
    case records
    case sources
}

/// 一次待确认的记录删除。单条删除和分组清空共用一个请求，因为它们做的是同一件事，
/// 只有措辞不同：分组清空要说清是哪一组、有多少条，单条删除要说清它写出的那个 Todo。
private struct CollectionItemDeletion {
    let items: [ATMCollectionItem]
    /// 分组清空时是分组名；单条删除为 nil。
    var groupName: String? = nil

    var title: String {
        guard let groupName else { return "删除这条处理记录？" }
        return "清空「\(groupName)」的 \(items.count) 条记录？"
    }

    var confirmLabel: String {
        groupName == nil ? "删除记录" : "清空 \(items.count) 条"
    }

    var message: String {
        guard groupName != nil else {
            return items.first.map(collectionDeleteWarning(for:)) ?? ""
        }
        let kept = items.compactMap(\.todoID).filter { !$0.isEmpty }.count
        let todos = kept > 0 ? "，其中 \(kept) 条写出的 Todo 保留" : ""
        return "\(items.count) 条记录会从本地删除\(todos)。刚收集到的记录可能在下一轮重新出现，更早的删掉就不会再回来。"
    }
}

struct DesktopCollectionView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var showingAddSource = false
    @State private var editingSourceID: String?
    @State private var deleteCandidate: ATMCollectionSource?
    @State private var itemDeletion: CollectionItemDeletion?
    @State private var historySource: ATMCollectionSource?
    @State private var showingIgnoredItems = false
    @State private var drawerTab = CollectionDrawerTab.records
    @State private var selectedSourceID: String?
    @AppStorage("ATMCollapsedCollectionSourceGroups") private var collapsedSourceGroupsRaw = ""

    private var collapsedSourceGroups: Set<String> {
        Set(collapsedSourceGroupsRaw.split(separator: ",").map(String.init))
    }

    private func expandedBinding(for id: String) -> Binding<Bool> {
        Binding(
            get: { !collapsedSourceGroups.contains(id) },
            set: { expanded in
                var set = collapsedSourceGroups
                if expanded { set.remove(id) } else { set.insert(id) }
                collapsedSourceGroupsRaw = set.sorted().joined(separator: ",")
            }
        )
    }

    private var filteredItems: [ATMCollectionItem] {
        store.collectionOverview.items
    }

    private var selectedItem: ATMCollectionItem? {
        guard let id = navigation.selectedCollectionItemID else { return displayedItems.first }
        return displayedItems.first { $0.id == id } ?? displayedItems.first
    }

    private var selectedSource: ATMCollectionSource? {
        let sources = store.collectionOverview.sources
        guard let selectedSourceID else { return sources.first }
        return sources.first { $0.id == selectedSourceID } ?? sources.first
    }

    private var primaryItems: [ATMCollectionItem] {
        filteredItems.filter { !shouldCollapse($0) }
    }

    private var ignoredItems: [ATMCollectionItem] {
        filteredItems.filter(shouldCollapse)
    }

    private var displayedItems: [ATMCollectionItem] {
        showingIgnoredItems ? primaryItems + ignoredItems : primaryItems
    }

    var body: some View {
        ATMSplitColumn(
            id: "collection",
            defaultWidth: 330,
            minWidth: 260,
            maxWidth: 420,
            detailMinWidth: 420
        ) {
            VStack(spacing: 0) {
                collectionDrawerTabs
                Divider()
                workspaceErrorBanner
                Group {
                    if drawerTab == .records {
                        itemColumn
                    } else {
                        sourceManagementColumn
                    }
                }
                .atmAnimatedSwap(drawerTab.rawValue, style: .tab)
            }
            // 中栏 surface / 右栏 canvas —— 和任务、Agent、知识一致，标题区也算中栏，
            // 底色铺在整根列上；只铺在列表上时标题区会漏出根视图的 canvas。
            .background(ATMTheme.listPane)
        } detail: {
            detailColumn
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .atmAnimatedSwap(collectionDetailIdentity, style: .detail)
        }
        .background(ATMTheme.canvas)
        .onAppear {
            store.refreshCollection()
            selectDefaultItem()
            selectDefaultSource()
            revealSelectedSourceGroup()
        }
        .onChange(of: store.collectionOverview.items.map(\.id)) { _ in selectDefaultItem() }
        .onChange(of: store.collectionOverview.sources.map(\.id)) { _ in selectDefaultSource() }
        .onChange(of: drawerTab) { tab in
            if tab == .sources {
                selectDefaultSource()
            } else {
                editingSourceID = nil
            }
        }
        .onChange(of: showingIgnoredItems) { _ in selectDefaultItem() }
        .onChange(of: navigation.selectedCollectionItemID) { _ in revealSelectedSourceGroup() }
        .sheet(isPresented: $showingAddSource) {
            CollectionSourceEditor(store: store) { showingAddSource = false }
        }
        .sheet(item: $historySource) { source in
            CollectionHistorySheet(store: store, source: source) {
                historySource = nil
            }
        }
        .confirmationDialog(
            "删除收集来源？",
            isPresented: Binding(
                get: { deleteCandidate != nil },
                set: { if !$0 { deleteCandidate = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button("删除来源", role: .destructive) {
                if let source = deleteCandidate { store.deleteCollectionSource(source) }
                deleteCandidate = nil
            }
            Button("取消", role: .cancel) { deleteCandidate = nil }
        } message: {
            Text("来源配置会删除，已有处理记录和 Todo 仍保留。")
        }
    }

    private var collectionDetailIdentity: String {
        switch drawerTab {
        case .records: return "record:\(selectedItem?.id ?? "empty")"
        case .sources: return "source:\(selectedSource?.id ?? "empty")"
        }
    }

    private var collectionDrawerTabs: some View {
        HStack(spacing: 12) {
            ATMCompactSegmentedTabs(
                selection: $drawerTab,
                items: [(.records, "记录"), (.sources, "来源")]
            )
            Spacer(minLength: 4)
            if drawerTab == .sources {
                ATMIconButton(
                    systemImage: "plus",
                    help: "添加来源",
                    chrome: .chip,
                    side: 30,
                    iconTier: .bodyLarge
                ) {
                    showingAddSource = true
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 16)
        .frame(height: 64)
    }

    /// 采集失败有来源可归属，右栏的“采集状态”卡片已经说了；但添加/删除来源、
    /// 修正、撤销、生成知识文档失败没有来源可挂，只写进 store 的共享错误。
    /// 少了这条横幅，它们在触发操作的工作区里完全没有反馈。
    @ViewBuilder
    private var workspaceErrorBanner: some View {
        if let error = workspaceError {
            let presentation = ATMErrorPresentation.resolve(error, fallbackTitle: "操作失败")
            ATMInlineNotice(
                severity: .warning,
                title: presentation.title,
                message: presentation.message,
                details: error,
                onDismiss: { store.collectionErrorMessage = nil }
            )
            .padding(8)
        }
    }

    private var workspaceError: String? {
        ATMCollectionWorkspaceNotice.message(
            shared: store.collectionErrorMessage,
            sourceErrors: store.collectionSourceErrors
        )
    }

    private var sourceManagementColumn: some View {
        Group {
            if store.collectionOverview.sources.isEmpty {
                CollectionEmptyState(
                    title: "还没有收集来源",
                    systemImage: "tray.2",
                    detail: "点击右上角添加来源，ATM 会按设定周期自动收集。"
                )
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(store.collectionOverview.sources) { source in
                            sourceManagementRow(source)
                                .atmContentStackRow()
                        }
                    }
                    .padding(.horizontal, ATMContentRowLayout.outerHorizontalPadding)
                    .padding(.vertical, 8)
                }
            }
        }
    }

    private func sourceManagementRow(_ source: ATMCollectionSource) -> some View {
        let selected = selectedSource?.id == source.id
        return Button {
            if selectedSourceID != source.id { editingSourceID = nil }
            selectedSourceID = source.id
        } label: {
            HStack(alignment: .top, spacing: ATMContentRowLayout.leadingSpacing) {
                Image(systemName: source.symbolName)
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(source.enabled ? ATMTheme.accent : ATMTheme.secondary)
                    .frame(
                        width: ATMContentRowLayout.leadingVisualSize,
                        height: ATMContentRowLayout.leadingVisualSize
                    )
                    .background(
                        (source.enabled ? ATMTheme.accent : ATMTheme.secondary).opacity(0.10),
                        in: Circle()
                    )
                VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                    HStack(spacing: 6) {
                        Text(source.displayName)
                            .font(ATMFont.font(.body, weight: .medium))
                            .foregroundStyle(ATMTheme.primary)
                            .lineLimit(1)
                    }
                    Text(
                        "\(source.connector) · \(collectionKindLabel(source.kind)) · 每 \(source.effectiveIntervalMinutes) 分钟"
                    )
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                }
                Spacer(minLength: 0)
                HStack(spacing: 5) {
                    Circle()
                        .fill(sourceStatusColor(source))
                        .frame(width: 7, height: 7)
                    Text(sourceStatusText(source))
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                .fixedSize()
            }
            .atmRowSurface(isSelected: selected)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help("查看来源配置")
        .atmRightClickMenu {
            ATMMenuItem("查看聊天记录") { historySource = source }
            ATMMenuItem("编辑") { editSource(source) }
            ATMMenuItem(source.enabled ? "暂停" : "启用") {
                store.setCollectionSource(source, enabled: !source.enabled)
            }
            ATMMenuSeparator()
            ATMMenuItem("删除", destructive: true) { deleteCandidate = source }
        }
    }

    private var itemColumn: some View {
        VStack(spacing: 0) {
            if filteredItems.isEmpty {
                CollectionEmptyState(
                    title: "暂无处理记录",
                    systemImage: "tray",
                    detail: "添加来源后，ATM 会在后台自动收集。"
                )
            } else {
                List {
                    ForEach(store.collectionOverview.sources) { source in
                        let items = primaryItems.filter { $0.sourceID == source.id }
                        if !items.isEmpty {
                            let expanded = expandedBinding(for: source.id)
                            Section {
                                if expanded.wrappedValue {
                                    ForEach(items) { item in
                                        itemRow(item)
                                    }
                                }
                            } header: {
                                sourceSectionHeader(source, items: items, expanded: expanded)
                            }
                        }
                    }

                    let unknownItems = primaryItems.filter { source(for: $0) == nil }
                    if !unknownItems.isEmpty {
                        let expanded = expandedBinding(for: "__unknown__")
                        Section {
                            if expanded.wrappedValue {
                                ForEach(unknownItems) { item in
                                    itemRow(item)
                                }
                            }
                        } header: {
                            genericSourceSectionHeader(
                                "其他来源",
                                systemImage: "questionmark.folder",
                                count: unknownItems.count,
                                expanded: expanded,
                                clear: { requestClear("其他来源", items: unknownItems) }
                            )
                        }
                    }

                    if !ignoredItems.isEmpty {
                        Section {
                            if showingIgnoredItems {
                                ForEach(ignoredItems) { item in
                                    itemRow(item).opacity(0.78)
                                }
                            }
                        } header: {
                            genericSourceSectionHeader(
                                "沉淀与已了结",
                                count: ignoredItems.count,
                                expanded: $showingIgnoredItems,
                                clear: { requestClear("沉淀与已了结", items: ignoredItems) }
                            )
                        }
                    }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
            }
        }
        // 记录的删除确认挂在中栏而不是根视图：来源删除已经占了根视图的
        // confirmationDialog，同一层挂两个在 macOS 上会互相顶掉。单条删除和分组清空
        // 因此也共用这一个（措辞由 CollectionItemDeletion 决定）。
        .confirmationDialog(
            itemDeletion?.title ?? "",
            isPresented: Binding(
                get: { itemDeletion != nil },
                set: { if !$0 { itemDeletion = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(itemDeletion?.confirmLabel ?? "删除记录", role: .destructive) {
                if let deletion = itemDeletion { store.deleteCollectionItems(deletion.items) }
                itemDeletion = nil
            }
            Button("取消", role: .cancel) { itemDeletion = nil }
        } message: {
            Text(itemDeletion?.message ?? "")
        }
    }

    private func requestClear(_ groupName: String, items: [ATMCollectionItem]) {
        guard !items.isEmpty else { return }
        itemDeletion = CollectionItemDeletion(items: items, groupName: groupName)
    }

    private func sourceSectionHeader(
        _ source: ATMCollectionSource,
        items: [ATMCollectionItem],
        expanded: Binding<Bool>
    ) -> some View {
        HStack(spacing: 7) {
            Button {
                withAnimation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion)) {
                    expanded.wrappedValue.toggle()
                }
            } label: {
                ATMDrawerDisclosureLabel(
                    title: source.displayName,
                    count: items.count,
                    tint: source.enabled ? ATMTheme.accent : ATMTheme.secondary,
                    isExpanded: expanded.wrappedValue,
                    systemImage: source.symbolName
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if !source.enabled {
                Text("已暂停")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
            sectionActionMenu {
                Button("查看聊天记录") {
                    historySource = source
                }
                Button("编辑") { editSource(source) }
                Button(source.enabled ? "暂停" : "启用") {
                    store.setCollectionSource(source, enabled: !source.enabled)
                }
                Divider()
                // 两条销毁动作紧挨着，所以都点明销毁的是什么：清空动的是这一组记录、
                // 来源留着继续收；删除动的是来源配置、记录留着。
                Button("清空记录", role: .destructive) {
                    requestClear(source.displayName, items: items)
                }
                Button("删除来源", role: .destructive) { deleteCandidate = source }
            }
        }
        .textCase(nil)
    }

    private func genericSourceSectionHeader(
        _ title: String,
        systemImage: String? = nil,
        count: Int,
        expanded: Binding<Bool>,
        clear: (() -> Void)? = nil
    ) -> some View {
        HStack(spacing: 7) {
            Button {
                withAnimation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion)) {
                    expanded.wrappedValue.toggle()
                }
            } label: {
                ATMDrawerDisclosureLabel(
                    title: title,
                    count: count,
                    tint: ATMTheme.secondary,
                    isExpanded: expanded.wrappedValue,
                    systemImage: systemImage
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            Spacer()
            if let clear {
                sectionActionMenu {
                    Button("清空记录", role: .destructive, action: clear)
                }
            }
        }
        .foregroundStyle(ATMTheme.secondary)
        .textCase(nil)
    }

    /// 分组标题右侧的省略号菜单。三种分组（来源、其他来源、沉淀与已了结）用同一个，
    /// 免得每加一项动作就有一处漏改样式。
    private func sectionActionMenu<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        Menu {
            content()
        } label: {
            Image(systemName: "ellipsis")
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 20, height: 20)
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
    }

    private func itemRow(_ item: ATMCollectionItem) -> some View {
        // 选中判定比的是 `selectedItem`，不是 selectedCollectionItemID —— 后者为 nil 时
        // 详情栏会回退展示首条，直接比 ID 会出现「右栏有内容、中栏没高亮」。
        let selected = selectedItem?.id == item.id
        return Button {
            navigation.selectedCollectionItemID = item.id
        } label: {
            CollectionItemRow(item: item)
                .atmRowSurface(isSelected: selected)
        }
        .buttonStyle(.plain)
        .focusable(false)
        .atmContentListRow()
        // 右键只放导航和删除。重新处理、修正、撤销这些要看记录状态才知道能不能做，
        // 判定在详情栏（见 CollectionItemDetail），在这儿抄一遍就是抄两套规则。
        .atmRightClickMenu {
            if item.todoID != nil {
                ATMMenuItem("打开 Todo") { openTodo(item) }
            }
            ATMMenuItem("复制记录 ID", systemImage: "number") {
                copyCollectionItemID(item.id)
            }
            ATMMenuSeparator()
            ATMMenuItem("删除记录", destructive: true) {
                itemDeletion = CollectionItemDeletion(items: [item])
            }
        }
    }

    @ViewBuilder
    private var detailColumn: some View {
        if drawerTab == .sources, let source = selectedSource {
            CollectionSourceDetail(
                store: store,
                source: source,
                isEditing: editingSourceID == source.id,
                onEdit: { editingSourceID = source.id },
                onFinishEditing: { editingSourceID = nil },
                onHistory: { historySource = source },
                onToggle: { store.setCollectionSource(source, enabled: !source.enabled) },
                onCollect: { store.runCollectionNow(source: source) },
                onDelete: { deleteCandidate = source }
            )
        } else if drawerTab == .sources {
            CollectionEmptyState(
                title: "还没有收集来源",
                systemImage: "tray.2",
                detail: "在中栏点击添加来源后，这里会显示它的配置。"
            )
        } else if let item = selectedItem {
            CollectionItemDetail(
                store: store,
                item: item,
                source: source(for: item)
            ) {
                openTodo(item)
            }
        } else {
            CollectionEmptyState(
                title: "选择一条处理记录",
                systemImage: "doc.text.magnifyingglass"
            )
        }
    }

    private func openTodo(_ item: ATMCollectionItem) {
        guard let todoID = item.todoID else { return }
        navigation.selectedTodoID = todoID
        navigation.section = .tasks
    }

    private func source(for item: ATMCollectionItem) -> ATMCollectionSource? {
        store.collectionOverview.sources.first { $0.id == item.sourceID }
    }

    private func sourceStatusText(_ source: ATMCollectionSource) -> String {
        if store.isCollecting(sourceID: source.id) { return "收集中" }
        if !source.enabled { return "已暂停" }
        if !store.collectionOverview.enabled { return "调度关闭" }
        switch store.collectionOverview.latestRun(for: source.id)?.status {
        case "failed": return "上次失败"
        case "succeeded": return "运行正常"
        case "running": return "收集中"
        default: return "待运行"
        }
    }

    private func sourceStatusColor(_ source: ATMCollectionSource) -> Color {
        if store.isCollecting(sourceID: source.id) { return ATMTheme.accent }
        if !source.enabled || !store.collectionOverview.enabled { return ATMTheme.secondary }
        switch store.collectionOverview.latestRun(for: source.id)?.status {
        case "failed": return ATMTheme.warning
        case "succeeded": return ATMTheme.success
        case "running": return ATMTheme.accent
        default: return ATMTheme.secondary
        }
    }

    /// Every edit affordance lands in the same right-hand editor. In particular,
    /// editing from a record group first reveals that source instead of opening a
    /// modal over a detail panel that still belongs to the record.
    private func editSource(_ source: ATMCollectionSource) {
        selectedSourceID = source.id
        drawerTab = .sources
        editingSourceID = source.id
    }

    private func shouldCollapse(_ item: ATMCollectionItem) -> Bool {
        item.shouldCollapseInCollection
    }

    private func selectDefaultItem() {
        guard !displayedItems.contains(where: { $0.id == navigation.selectedCollectionItemID }) else { return }
        navigation.selectedCollectionItemID = displayedItems.first?.id
    }

    private func selectDefaultSource() {
        let sources = store.collectionOverview.sources
        guard !sources.contains(where: { $0.id == selectedSourceID }) else { return }
        selectedSourceID = sources.first?.id
    }

    private func revealSelectedSourceGroup() {
        guard let item = selectedItem, !shouldCollapse(item) else { return }
        let groupID = source(for: item)?.id ?? "__unknown__"
        var set = collapsedSourceGroups
        guard set.remove(groupID) != nil else { return }
        collapsedSourceGroupsRaw = set.sorted().joined(separator: ",")
    }
}

/// “来源”tab 的右栏。中栏负责选择来源，查看和编辑都留在右栏，避免为了修改
/// 同一份配置再盖一层弹窗。
private struct CollectionSourceDetail: View {
    @ObservedObject var store: ATMDataStore
    let source: ATMCollectionSource
    let isEditing: Bool
    let onEdit: () -> Void
    let onFinishEditing: () -> Void
    let onHistory: () -> Void
    let onToggle: () -> Void
    let onCollect: () -> Void
    let onDelete: () -> Void

    var body: some View {
        Group {
            if isEditing {
                CollectionSourceEditor(
                    store: store,
                    source: source,
                    presentation: .detail,
                    onClose: onFinishEditing
                )
            } else {
                VStack(spacing: 0) {
                    header
                    Divider()
                    ScrollView {
                        VStack(alignment: .leading, spacing: 14) {
                            runCard
                            identityCard
                            scheduleCard
                            rulesCard
                        }
                        .padding(24)
                        .frame(maxWidth: 760, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
        .background(ATMTheme.canvas)
    }

    private var header: some View {
        VStack(spacing: 14) {
            HStack(spacing: 13) {
                Image(systemName: source.symbolName)
                    .font(ATMFont.font(.title3, weight: .semibold))
                    .symbolRenderingMode(.monochrome)
                    .foregroundStyle(source.enabled ? ATMTheme.accent : ATMTheme.secondary)
                    .frame(width: 40, height: 40)
                    .background(
                        (source.enabled ? ATMTheme.accent : ATMTheme.secondary).opacity(0.10),
                        in: RoundedRectangle(cornerRadius: 10, style: .continuous)
                    )

                VStack(alignment: .leading, spacing: 4) {
                    Text(source.displayName)
                        .font(ATMFont.font(.title2, weight: .bold))
                        .lineLimit(1)
                    Label(sourceStatusText, systemImage: sourceStatusIcon)
                        .font(ATMFont.caption)
                        .foregroundStyle(sourceStatusColor)
                }

                Spacer(minLength: 16)

                Menu {
                    Button(action: onHistory) {
                        Label("查看聊天记录", systemImage: "bubble.left.and.bubble.right")
                    }
                    Button(action: onEdit) {
                        Label("编辑来源", systemImage: "slider.horizontal.3")
                    }
                    Divider()
                    Button(role: .destructive, action: onDelete) {
                        Label("删除来源", systemImage: "trash")
                    }
                } label: {
                    ATMIconMenuLabel(
                        systemImage: "ellipsis",
                        help: "来源操作",
                        side: 28,
                        iconTier: .bodyLarge
                    )
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
            }

            HStack(spacing: 12) {
                Toggle(
                    "自动收集",
                    isOn: Binding(
                        get: { source.enabled },
                        set: { _ in onToggle() }
                    )
                )
                .toggleStyle(.switch)
                .controlSize(.small)

                Spacer(minLength: 12)

                Button(action: onEdit) {
                    Label("编辑", systemImage: "slider.horizontal.3")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)

                Button(action: onCollect) {
                    Label(
                        store.isCollecting(sourceID: source.id) ? "收集中" : "立即收集",
                        systemImage: store.isCollecting(sourceID: source.id) ? "hourglass" : "arrow.clockwise"
                    )
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .disabled(!source.enabled || store.isCollecting)
                .help(source.enabled ? "只收集这个来源" : "先启用这个来源")
            }
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 18)
        .background(ATMTheme.surface)
    }

    private var sourceStatusText: String {
        if store.isCollecting(sourceID: source.id) { return "正在收集这个来源" }
        if !source.enabled { return "已暂停" }
        if !store.collectionOverview.enabled { return "自动调度已关闭，可手动收集" }
        switch store.collectionOverview.latestRun(for: source.id)?.status {
        case "failed": return "上次收集失败"
        case "succeeded":
            let time = store.collectionOverview.latestRun(for: source.id)?.startedAt ?? 0
            return "运行正常 · \(collectionRelativeTime(time))"
        case "running": return "正在收集这个来源"
        default: return "等待首次收集"
        }
    }

    private var sourceStatusIcon: String {
        if store.isCollecting(sourceID: source.id) { return "arrow.triangle.2.circlepath" }
        if !source.enabled || !store.collectionOverview.enabled { return "pause.circle.fill" }
        switch store.collectionOverview.latestRun(for: source.id)?.status {
        case "failed": return "exclamationmark.triangle.fill"
        case "succeeded": return "checkmark.circle.fill"
        case "running": return "arrow.triangle.2.circlepath"
        default: return "clock.fill"
        }
    }

    private var sourceStatusColor: Color {
        if store.isCollecting(sourceID: source.id) { return ATMTheme.accent }
        if !source.enabled || !store.collectionOverview.enabled { return ATMTheme.secondary }
        switch store.collectionOverview.latestRun(for: source.id)?.status {
        case "failed": return ATMTheme.warning
        case "succeeded": return ATMTheme.success
        case "running": return ATMTheme.accent
        default: return ATMTheme.secondary
        }
    }

    @ViewBuilder
    private var runCard: some View {
        sourceCard("采集状态", systemImage: "waveform.path.ecg") {
            if let run = store.collectionOverview.latestRun(for: source.id) {
                sourceValueRow("状态", runStatusLabel(run))
                sourceValueRow("时间", collectionRelativeTime(run.startedAt))
                sourceValueRow("读取", "\(run.fetchedCount) 条")
                sourceValueRow(
                    "结果",
                    "新建 \(run.createdCount) · 补充 \(run.appendedCount) · 沉淀 \(run.insightCount) · 忽略 \(run.ignoredCount)"
                )
                if let error = store.collectionError(for: source.id), !error.isEmpty {
                    sourceTextBlock("失败原因", error)
                }
            } else if let error = store.collectionError(for: source.id), !error.isEmpty {
                sourceValueRow("状态", "运行失败")
                sourceTextBlock("失败原因", error)
            } else {
                Text(source.enabled ? "还没有采集记录，可以点击“立即收集”检查这个来源。" : "启用后可以单独采集这个来源。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
    }

    private func runStatusLabel(_ run: ATMCollectionRun) -> String {
        switch run.status {
        case "running": return "正在收集"
        case "succeeded": return "成功"
        case "failed": return "失败"
        default: return run.status
        }
    }

    private var identityCard: some View {
        sourceCard("来源", systemImage: "point.3.connected.trianglepath.dotted") {
            sourceValueRow("连接器", source.connector, monospaced: true)
            sourceValueRow("类型", collectionKindLabel(source.kind))
            sourceValueRow("来源 ID", source.externalID, monospaced: true)
            sourceValueRow("项目", value(source.project, fallback: "未指定") ?? "未指定")
        }
    }

    private var scheduleCard: some View {
        sourceCard("自动收集", systemImage: "clock") {
            sourceValueRow("来源开关", source.enabled ? "已启用" : "已暂停")
            sourceValueRow("自动调度", store.collectionOverview.enabled ? "正在运行" : "总开关已关闭")
            sourceValueRow("间隔", "每 \(source.effectiveIntervalMinutes) 分钟")
            sourceValueRow("处理方式", source.effectiveStrategy == "observe" ? "只观察，不创建 Todo" : "创建或补充 Todo")
            sourceValueRow("Agent 派发", source.automaticallyDispatches ? "新 Todo 自动交给 Codex" : "仅收集，由人决定")
            sourceValueRow("判断单位", source.effectiveDecisionUnit == "message" ? "单条消息" : "消息窗口")
            sourceValueRow("默认优先级", source.priority)
        }
    }

    @ViewBuilder
    private var rulesCard: some View {
        let instruction = value(source.instruction)
        let excludePattern = value(source.excludePattern)
        let knowledgeCollection = value(source.knowledgeCollection)
        sourceCard("处理规则", systemImage: "line.3.horizontal.decrease.circle") {
            if instruction == nil, excludePattern == nil, knowledgeCollection == nil {
                Text("没有额外规则，使用默认处理方式。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            } else {
                if let instruction {
                    sourceTextBlock("补充指令", instruction)
                }
                if let excludePattern {
                    sourceTextBlock("排除规则", excludePattern, monospaced: true)
                }
                if let knowledgeCollection {
                    sourceValueRow("知识库", knowledgeCollection)
                }
            }
        }
    }

    private func sourceCard<Content: View>(
        _ title: String,
        systemImage: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(title, systemImage: systemImage)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
            content()
        }
        .padding(15)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard(cornerRadius: 11)
    }

    private func sourceValueRow(
        _ label: String,
        _ value: String,
        monospaced: Bool = false
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 14) {
            Text(label)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 88, alignment: .leading)
            Text(value)
                .font(monospaced ? ATMFont.mono(.footnote) : ATMFont.footnote)
                .foregroundStyle(ATMTheme.primary)
                .textSelection(.enabled)
            Spacer(minLength: 0)
        }
    }

    private func sourceTextBlock(_ label: String, _ text: String, monospaced: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(label)
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
            Text(text)
                .font(monospaced ? ATMFont.mono(.footnote) : ATMFont.footnote)
                .foregroundStyle(ATMTheme.primary)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func value(_ rawValue: String?, fallback: String? = nil) -> String? {
        let trimmed = (rawValue ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? fallback : trimmed
    }
}

private struct CollectionEmptyState: View {
    let title: String
    let systemImage: String
    var detail: String? = nil

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: systemImage)
                .font(ATMFont.font(.display, weight: .light))
                .foregroundStyle(ATMTheme.secondary)
            Text(title)
                .font(ATMFont.font(.body, weight: .semibold))
            if let detail {
                Text(detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .multilineTextAlignment(.center)
            }
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct CollectionItemRow: View {
    let item: ATMCollectionItem

    private var itemType: ATMCollectionItemType {
        ATMCollectionItemType.resolve(item.itemType)
    }

    private var retryStopped: Bool { item.retryStopped == true }

    var body: some View {
        HStack(alignment: .top, spacing: ATMContentRowLayout.leadingSpacing) {
            Image(systemName: collectionActionIcon(item.action, retryStopped: retryStopped))
                .font(ATMFont.font(.footnote, weight: .semibold))
                .symbolRenderingMode(.monochrome)
                .foregroundStyle(collectionActionColor(item.action, retryStopped: retryStopped))
                .frame(
                    width: ATMContentRowLayout.leadingVisualSize,
                    height: ATMContentRowLayout.leadingVisualSize
                )
                .background(
                    collectionActionColor(item.action, retryStopped: retryStopped).opacity(0.10),
                    in: Circle()
                )
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                Text(item.title?.isEmpty == false
                    ? item.title!
                    : collectionActionTitle(item.action, retryStopped: retryStopped))
                    .font(ATMFont.font(.body, weight: .medium))
                    .lineLimit(2)
                HStack(spacing: 5) {
                    Text(itemType.title)
                    Text("·")
                    Text(collectionActionTitle(item.action, retryStopped: retryStopped))
                    if item.dispatchStatus == "failed" {
                        Text("·")
                        Text("Agent 派发失败")
                            .foregroundStyle(ATMTheme.danger)
                    } else if item.dispatchStatus == "dispatched" {
                        Text("· 已交给 Codex")
                    }
                    if item.todoClosed, let todoID = item.todoID, let status = item.todoStatus {
                        Text("·")
                        Text("\(todoID) \(ATMTodoStatusStyle.label(forStatus: status))")
                            .foregroundStyle(ATMTodoStatusStyle.color(forStatus: status))
                    }
                    if let time = item.occurredAt {
                        Text("·")
                        Text(collectionRelativeTime(time))
                    }
                }
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
            }
        }
    }
}

/// 一条处理记录要说清四件事：消息从哪儿来、ATM 为什么这样处理、结论是什么、关联到
/// 哪个 Todo。这四件都在「处理详情」里；消息原文按条数可以长到几十行，单独一个 tab，
/// 免得它把四件事顶出屏幕。分页样式沿用任务详情（见 DesktopTodoDetail.detailTabs）。
private struct CollectionItemDetail: View {
    private enum DetailTab: String, CaseIterable {
        case decision
        case transcript
    }

    @ObservedObject var store: ATMDataStore
    let item: ATMCollectionItem
    let source: ATMCollectionSource?
    let openTodo: () -> Void

    @State private var selectedTab: DetailTab = .decision
    @State private var showingCorrection = false
    @State private var confirmingRevert = false
    @State private var confirmingDelete = false
    @State private var copiedID = false

    private var itemType: ATMCollectionItemType {
        ATMCollectionItemType.resolve(item.itemType)
    }

    private var retryStopped: Bool { item.retryStopped == true }

    private var transcript: ATMCollectionTranscript {
        ATMCollectionTranscript.parse(item.rawContext)
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            detailTabs
            content
        }
        .background(ATMTheme.canvas)
        .sheet(isPresented: $showingCorrection) {
            CollectionCorrectionSheet(item: item) { title, project, priority in
                store.correctCollectionItem(
                    item, title: title, project: project, priority: priority
                )
                showingCorrection = false
            } onCancel: {
                showingCorrection = false
            }
        }
        .confirmationDialog(
            item.action == "create" ? "撤销并废弃自动创建的 Todo？" : "撤销这次自动补充？",
            isPresented: $confirmingRevert,
            titleVisibility: .visible
        ) {
            Button("撤销自动处理", role: .destructive) { store.revertCollectionItem(item) }
            Button("取消", role: .cancel) {}
        } message: {
            Text(item.action == "create"
                 ? "ATM 会将这条采集自动创建的 Todo 标记为已废弃，并保留审计记录。"
                 : "ATM 会追加一条撤销说明；原补充保留供审计，不会改写历史。")
        }
        .confirmationDialog(
            "删除这条处理记录？",
            isPresented: $confirmingDelete,
            titleVisibility: .visible
        ) {
            Button("删除记录", role: .destructive) { store.deleteCollectionItem(item) }
            Button("取消", role: .cancel) {}
        } message: {
            Text(collectionDeleteWarning(for: item))
        }
        .onChange(of: item.id) { _ in copiedID = false }
    }

    /// 修正和撤销都改写这条记录写出去的 Todo，一旦那个 Todo 已经完成或废弃就没有意义了：
    /// 撤销的语义是「这次采集判断错了，把它建的任务废掉」，而不是把做完的事重新废一遍。
    /// 已了结的记录仍可「打开 Todo」，从任务侧自行处理。
    private var canAmendTodoWrite: Bool {
        (item.action == "create" || item.action == "append") && !item.todoClosed
    }

    @ViewBuilder
    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                switch selectedTab {
                case .decision:
                    sourceSummary
                    detailDivider
                    decisionSummary
                    detailDivider
                    outcomeSummary
                case .transcript:
                    rawContextSummary
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 20)
            .frame(maxWidth: 880, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private var detailTabs: some View {
        HStack(spacing: 22) {
            detailTabButton(.decision, title: "处理详情", icon: "doc.text")
            detailTabButton(
                .transcript,
                title: transcriptTabTitle,
                icon: "bubble.left.and.bubble.right"
            )
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 24)
        .frame(height: 46)
        .background(ATMTheme.canvas)
    }

    private func detailTabButton(_ tab: DetailTab, title: String, icon: String) -> some View {
        let selected = selectedTab == tab
        return Button {
            selectedTab = tab
        } label: {
            Label(title, systemImage: icon)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(selected ? ATMTheme.primary : ATMTheme.secondary)
                .padding(.horizontal, 2)
                .frame(height: 46)
                .overlay(alignment: .bottom) {
                    Capsule()
                        .fill(selected ? ATMTheme.accent : Color.clear)
                        .frame(height: 2)
                }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var transcriptTabTitle: String {
        let count = transcript.messageCount
        return count == 0 ? "消息原文" : "消息原文 \(count)"
    }

    private var header: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 6) {
                Label(
                    collectionActionTitle(item.action, retryStopped: retryStopped),
                    systemImage: collectionActionIcon(item.action, retryStopped: retryStopped)
                )
                .font(ATMFont.footnote)
                .foregroundStyle(collectionActionColor(item.action, retryStopped: retryStopped))
                .padding(.horizontal, 9)
                .padding(.vertical, 5)
                .background(
                    collectionActionColor(item.action, retryStopped: retryStopped).opacity(0.10),
                    in: Capsule()
                )
                Text(item.title?.isEmpty == false ? item.title! : "未生成标题")
                    .font(ATMFont.font(.title3, weight: .bold))
                    .textSelection(.enabled)
                Button {
                    copyCollectionItemID(item.id)
                    copiedID = true
                } label: {
                    HStack(spacing: 6) {
                        Text("记录 ID")
                        Text(item.id)
                            .font(ATMFont.mono(.caption, .medium))
                            .textSelection(.enabled)
                        Image(systemName: copiedID ? "checkmark" : "doc.on.doc")
                    }
                    .font(ATMFont.caption)
                    .foregroundStyle(copiedID ? ATMTheme.success : ATMTheme.secondary)
                }
                .buttonStyle(.plain)
                .help(copiedID ? "已复制" : "复制记录 ID")
                .accessibilityLabel(copiedID ? "已复制记录 ID \(item.id)" : "复制记录 ID \(item.id)")
            }
            Spacer()
            if item.todoID != nil {
                Button("打开 Todo", action: openTodo)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
            }
            if item.action == "ignore" {
                Button("转成 Todo") { store.promoteCollectionItem(item) }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
            }
            // 撤销过的 item 状态回落成 `processed`（见 collector 的 revert），它的消息
            // 因此进了 handled 名单、再也不会被自动重新收集——这个按钮是它唯一的回头路，
            // 所以留在主位。重试已停止的失败项同理：不点它就真的没有下一轮了。还在预算
            // 内的失败正相反，下一轮自己会重来，于是降级进菜单，只服务「刚修好连接器、
            // 不想等下一轮」这种情况。
            if item.action == "reverted" {
                Button("重新处理") { store.reprocessCollectionItem(item) }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
            } else if retryStopped {
                Button("重试") { store.reprocessCollectionItem(item) }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
            }
            // 其余条目要看状态才知道能不能做，删除对任何一条记录都成立，所以
            // 菜单不再需要「有没有动作」那道门——它永远至少有一项。
            Menu {
                if item.action == "ignore" {
                    Button("重新判断") { store.reprocessCollectionItem(item) }
                }
                if item.action == "failed", !retryStopped {
                    Button("立即重试") { store.reprocessCollectionItem(item) }
                }
                if canAmendTodoWrite {
                    Button("修正标题、项目和优先级") { showingCorrection = true }
                    Button("撤销自动处理", role: .destructive) { confirmingRevert = true }
                    Divider()
                }
                Button("删除记录", role: .destructive) { confirmingDelete = true }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
        }
        .padding(.horizontal, 24)
        .padding(.top, 17)
        .padding(.bottom, 18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated)
    }

    private var sourceSummary: some View {
        VStack(alignment: .leading, spacing: 8) {
            sectionTitle("来源")
            HStack(spacing: 10) {
                Image(systemName: collectionKindSymbol(source?.kind))
                    .foregroundStyle(ATMTheme.secondary)
                    .frame(width: 28, height: 28)
                    .background(ATMTheme.controlFill, in: Circle())
                VStack(alignment: .leading, spacing: 2) {
                    Text(source?.displayName ?? item.sourceID)
                        .font(ATMFont.body)
                        .lineLimit(1)
                    Text(sourceDetailLine)
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                }
                Spacer()
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var sourceDetailLine: String {
        var parts = [item.connector]
        if let sender = item.sender, !sender.isEmpty {
            parts.append(sender)
        }
        // 「新消息」而不是「消息」：这里数的是触发这次判断的那一批，「消息原文」tab 上的
        // 条数还含着用来解析指代的上下文，两个数不一样大。
        parts.append("\(item.messageIDs.count) 条新消息")
        if let time = item.occurredAt {
            parts.append(collectionRelativeTime(time))
        }
        return parts.joined(separator: " · ")
    }

    private var decisionSummary: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                sectionTitle("为什么这样处理")
                Spacer()
                if let confidence = item.confidence {
                    Text("置信度 " + String(format: "%.0f%%", confidence * 100))
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            Text(item.reason?.isEmpty == false ? item.reason! : "暂无判断说明。")
                .font(ATMFont.body)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            if let error = item.error, !error.isEmpty {
                // 同一条报错，读法完全不同：还会自动重来的是过程信息，重试停下的才是
                // 待办事项。前者压到次要色，避免一次抖动在详情里长成一条红杠。
                Label(error, systemImage: retryStopped ? "exclamationmark.triangle.fill" : "arrow.clockwise")
                    .font(ATMFont.footnote)
                    .foregroundStyle(retryStopped ? ATMTheme.danger : ATMTheme.secondary)
                    .textSelection(.enabled)
                if item.action == "failed" {
                    Text(retryStopped
                        ? "已连续失败 \(item.attempts ?? 0) 次，自动重试已停止。"
                        : "下一轮自动重试。")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// 结论：这批消息最后落成了什么。`summary` 是分类器留下的正文——create 时是建出的
    /// Todo 写了什么，append 时是这次补充加了什么，insight 时是沉淀进知识库的内容。
    private var outcomeSummary: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                sectionTitle("结论")
                Spacer()
                metadataLabel(itemType.title, systemImage: itemType.systemImage)
                if let project = item.project, !project.isEmpty {
                    metadataLabel(project, systemImage: "folder")
                }
                if let priority = item.priority, !priority.isEmpty {
                    metadataLabel(priority, systemImage: "flag")
                }
            }
            Text(item.summary?.isEmpty == false ? item.summary! : "暂无内容摘要。")
                .font(ATMFont.body)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            if let todoID = item.todoID, !todoID.isEmpty {
                Text(todoRelationLine(todoID))
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
            if item.dispatchStatus == "failed" {
                Label(
                    item.dispatchError?.isEmpty == false ? item.dispatchError! : "Agent 派发失败",
                    systemImage: "exclamationmark.triangle.fill"
                )
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.danger)
                .textSelection(.enabled)
                Text("Todo 已保留；打开 Todo 后可点击“重试”再次交给 Codex。")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            } else if item.dispatchStatus == "dispatched" {
                Label("已自动交给 Codex，可在 Todo 详情查看进度和日志。", systemImage: "play.circle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.success)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// 关联的 Todo 及其当前状态。归档要点出来：`打开 Todo` 会落到任务台不再列出的
    /// 任务上，说一句比看着像坏了好。
    private func todoRelationLine(_ todoID: String) -> String {
        var parts = ["关联 \(todoID)"]
        if let status = item.todoStatus, !status.isEmpty {
            parts.append(ATMTodoStatusStyle.label(forStatus: status))
        }
        if item.todoArchived == true {
            parts.append("已归档")
        }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var rawContextSummary: some View {
        let transcript = ATMCollectionTranscript.parse(item.rawContext)
        VStack(alignment: .leading, spacing: 10) {
            // 不叫「聊天记录」：来源菜单里那一项是同名的、来源最近 N 条的另一个东西。
            sectionTitle("消息原文")
            if let fallback = transcript.fallback {
                // 认不出格式就原样显示，糙一点也比丢内容好。
                Text(fallback)
                    .font(ATMFont.mono(.footnote))
                    .foregroundStyle(ATMTheme.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            } else if transcript.blocks.isEmpty {
                Text("未保存原始聊天上下文。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(transcript.blocks) { block in
                        if block.startsFresh { freshDivider }
                        transcriptBlock(block)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// 分界线之上是已经处理过、只用来解析指代的上下文；之下才是这次真正参与判断的
    /// 新消息。上下文那几块同时压成次要色，一眼就能看出判断依据从哪儿开始。
    private var freshDivider: some View {
        HStack(spacing: 8) {
            Text("新消息")
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.accent)
            Rectangle()
                .fill(ATMTheme.accent.opacity(0.28))
                .frame(height: 1)
        }
        .padding(.vertical, 2)
    }

    private func transcriptBlock(_ block: ATMCollectionTranscript.Block) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(block.sender)
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(block.isFresh ? ATMTheme.primary : ATMTheme.secondary)
                    .lineLimit(1)
                Text(block.time)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            ForEach(Array(block.lines.enumerated()), id: \.offset) { _, line in
                Text(line)
                    .font(ATMFont.footnote)
                    .foregroundStyle(block.isFresh ? ATMTheme.primary : ATMTheme.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        // 新消息用正常气泡、上下文压暗一档。强调色留给分界线本身：几十条消息全上
        // accent 底，整段就只剩一片蓝。
        .background(
            block.isFresh ? ATMTheme.controlFill : ATMTheme.controlFill.opacity(0.5),
            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
        )
    }

    private func sectionTitle(_ text: String) -> some View {
        Text(text)
            .font(ATMFont.font(.body, weight: .semibold))
    }

    private func metadataLabel(_ text: String, systemImage: String) -> some View {
        Label(text, systemImage: systemImage)
            .font(ATMFont.caption)
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)
    }

    private var detailDivider: some View {
        Divider()
            .padding(.vertical, 20)
    }
}

private func copyCollectionItemID(_ id: String) {
    NSPasteboard.general.clearContents()
    NSPasteboard.general.setString(id, forType: .string)
}

private struct CollectionCorrectionSheet: View {
    let item: ATMCollectionItem
    let onSave: (String, String, String) -> Void
    let onCancel: () -> Void

    @State private var title: String
    @State private var project: String
    @State private var priority: String

    init(
        item: ATMCollectionItem,
        onSave: @escaping (String, String, String) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self.item = item
        self.onSave = onSave
        self.onCancel = onCancel
        _title = State(initialValue: item.title ?? "")
        _project = State(initialValue: item.project ?? "")
        _priority = State(initialValue: item.priority ?? "P2")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("修正收集结果")
                .font(ATMFont.font(.title3, weight: .bold))
            Text("修正会同步更新处理记录和关联 Todo，原始聊天上下文保持不变。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)

            Form {
                TextField("标题", text: $title)
                TextField("项目（可留空）", text: $project)
                Picker("优先级", selection: $priority) {
                    ForEach(["P0", "P1", "P2", "P3"], id: \.self) { Text($0).tag($0) }
                }
            }
            .formStyle(.grouped)

            HStack {
                Spacer()
                Button("取消", action: onCancel)
                Button("保存") {
                    onSave(
                        title.trimmingCharacters(in: .whitespacesAndNewlines),
                        project.trimmingCharacters(in: .whitespacesAndNewlines),
                        priority
                    )
                }
                .buttonStyle(.borderedProminent)
                .disabled(title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(24)
        .frame(width: 520)
    }
}

private enum CollectionSourceEditorPresentation: Equatable {
    case sheet
    case detail
}

/// One source form shared by the add sheet and the right-hand detail editor.
/// Keeping the fields here means validation and CLI arguments cannot drift
/// between the two presentations.
private struct CollectionSourceEditor: View {
    @ObservedObject var store: ATMDataStore
    let source: ATMCollectionSource?
    let presentation: CollectionSourceEditorPresentation
    let onClose: () -> Void

    /// Connector plus what it points at, in one value — see
    /// `ATMCollectionSourceIdentity` for why the save rules live outside the view.
    @State private var identity: ATMCollectionSourceIdentity
    @State private var name = ""
    @State private var project = ""
    @State private var priority = "P2"
    @State private var excludePattern = ""
    @State private var instruction = ""
    @State private var knowledgeCollection = ""
    @State private var strategy = "tasks"
    @State private var decisionUnit = "window"
    @State private var intervalMinutes = 5
    @State private var autoDispatch = false

    @State private var searchKind = ATMCollectionSearchKind.all
    @State private var keyword = ""
    @State private var candidates: [ATMCollectionCandidate] = []
    @State private var searchedKeyword: String?
    @State private var isSearching = false
    @State private var searchError: String?
    /// The last name a candidate filled in, so a 显示名称 the person typed
    /// themselves is never overwritten by picking a different candidate.
    @State private var autoFilledName: String?
    @FocusState private var keywordFocused: Bool

    init(
        store: ATMDataStore,
        source: ATMCollectionSource? = nil,
        presentation: CollectionSourceEditorPresentation = .sheet,
        onClose: @escaping () -> Void
    ) {
        self.store = store
        self.source = source
        self.presentation = presentation
        self.onClose = onClose
        var identity = ATMCollectionSourceIdentity()
        if let source {
            identity.connector = source.connector
            identity.locked = .identifier(kind: source.kind, externalID: source.externalID)
            identity.manualKind = source.kind
            identity.externalID = source.externalID
        } else {
            // With one connector configured there is no choice to make, and
            // pre-selecting it is what lets the search field work right away.
            let configured = Self.connectorOptions(in: store, including: nil)
            identity.connector = configured.count == 1 ? configured[0].connector : ""
        }
        _identity = State(initialValue: identity)
        _name = State(initialValue: source?.name ?? "")
        _project = State(initialValue: source?.project ?? "")
        _priority = State(initialValue: source?.priority ?? "P2")
        _excludePattern = State(initialValue: source?.excludePattern ?? "")
        // Carried through the sheet even though only the CLI writes it today:
        // `collect source add` is an upsert over every column, so a save that
        // omitted these would silently clear what the CLI had configured.
        _instruction = State(initialValue: source?.instruction ?? "")
        _knowledgeCollection = State(initialValue: source?.knowledgeCollection ?? "")
        _strategy = State(initialValue: source?.effectiveStrategy ?? "tasks")
        _decisionUnit = State(initialValue: source?.effectiveDecisionUnit ?? "window")
        _intervalMinutes = State(initialValue: source?.effectiveIntervalMinutes ?? 5)
        _autoDispatch = State(initialValue: source?.automaticallyDispatches ?? false)
    }

    var body: some View {
        Group {
            if presentation == .detail {
                editorContent
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                editorContent
                    // Editing has no connector picker, no search and no candidate list,
                    // so it needs materially less room than adding does.
                    .frame(width: 620, height: source == nil ? 700 : 640)
            }
        }
        .background(ATMTheme.canvas)
        .onAppear {
            // Opening on the one field that always needs typing; when the
            // connector is still unchosen the picker is one Tab away.
            keywordFocused = source == nil && !identity.trimmedConnector.isEmpty
        }
    }

    private var editorContent: some View {
        VStack(spacing: 0) {
            sheetHeader

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    sourceSection
                    scopeSection
                    processingSection
                }
                .padding(22)
                .frame(maxWidth: 760, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .leading)
            }

            Divider()

            footer
        }
    }

    private var sheetHeader: some View {
        HStack(spacing: 13) {
            Image(systemName: source == nil ? "person.badge.plus" : "slider.horizontal.3")
                .font(ATMFont.font(.title3, weight: .semibold))
                .foregroundStyle(ATMTheme.accent)
                .frame(width: 38, height: 38)
                .background(ATMTheme.accentFill, in: RoundedRectangle(cornerRadius: 10))

            VStack(alignment: .leading, spacing: 3) {
                Text(source == nil ? "添加收集来源" : "编辑收集来源")
                    .font(ATMFont.font(.title2, weight: .bold))
                Text(subtitle)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer()
        }
        .padding(.horizontal, 22)
        .padding(.vertical, 18)
        .background(ATMTheme.surface)
    }

    private var footer: some View {
        HStack(spacing: 10) {
            if let reason = saveBlockReason {
                Label(reason, systemImage: "arrow.up.circle")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            } else {
                Text("配置将在下一次收集时生效")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
            Button("取消", action: onClose)
                .keyboardShortcut(.cancelAction)
            Button(source == nil ? "添加" : "保存", action: save)
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(saveBlockReason != nil)
        }
        .padding(.horizontal, 22)
        .padding(.vertical, 14)
        .background(ATMTheme.surface)
    }

    private var saveBlockReason: String? {
        if let reason = identity.blockReason { return reason }
        if autoDispatch && project.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return "自动派发前请指定项目"
        }
        return nil
    }

    private func save() {
        guard let target = identity.target else { return }
        store.addCollectionSource(
            connector: identity.trimmedConnector, target: target, name: name,
            project: project, priority: priority,
            excludePattern: excludePattern, instruction: instruction,
            knowledgeCollection: knowledgeCollection, strategy: strategy,
            decisionUnit: decisionUnit,
            intervalMinutes: intervalMinutes, autoDispatch: autoDispatch,
            enabled: source?.enabled ?? true
        )
        onClose()
    }

    // MARK: - 来源

    @ViewBuilder
    private var sourceSection: some View {
        settingsSection(title: "来源", icon: "point.3.connected.trianglepath.dotted") {
            VStack(alignment: .leading, spacing: 14) {
                if identity.isEditing {
                    lockedIdentityCard
                } else {
                    connectorField
                    Divider()
                    if let selected = identity.selection, !identity.manualEntry {
                        selectedCandidateCard(selected)
                    } else if identity.manualEntry {
                        manualIdentifierFields
                    } else {
                        searchField
                        candidateList
                    }
                }

                formField("显示名称", hint: "在来源列表和处理记录里显示，可留空则显示 ID") {
                    TextField(namePlaceholder, text: $name)
                        .textFieldStyle(.roundedBorder)
                }
            }
        }
    }

    /// Every configured connector, plus the one an existing source was saved
    /// with — a connector removed from config.json still has to be nameable, or
    /// its sources become uneditable.
    private static func connectorOptions(
        in store: ATMDataStore,
        including connector: String?
    ) -> [ATMCollectionConnectorHealth] {
        var options = store.collectionOverview.connectorHealth
        let existing = (connector ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !existing.isEmpty, !options.contains(where: { $0.connector == existing }) {
            options.append(
                ATMCollectionConnectorHealth(
                    connector: existing, status: "not_checked", error: nil, checkedAt: nil
                )
            )
        }
        return options.sorted { $0.connector < $1.connector }
    }

    private var connectorOptions: [ATMCollectionConnectorHealth] {
        Self.connectorOptions(in: store, including: source?.connector)
    }

    private var selectedConnectorHealth: ATMCollectionConnectorHealth? {
        connectorOptions.first { $0.connector == identity.trimmedConnector }
    }

    @ViewBuilder
    private var connectorField: some View {
        if connectorOptions.isEmpty {
            // Nothing to pick from is a configuration problem, not an empty
            // dropdown: say where connectors come from instead of showing one.
            HStack(alignment: .top, spacing: 9) {
                Image(systemName: "link.badge.plus")
                    .foregroundStyle(ATMTheme.warning)
                VStack(alignment: .leading, spacing: 3) {
                    Text("还没有配置连接器")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("在 ~/.atm/config.json 的 collection_connectors 里登记一个可执行连接器后，这里就能选到它。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(11)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ATMTheme.warningFill, in: RoundedRectangle(cornerRadius: 8))
        } else {
            formField("连接器", hint: "已登记的连接器，来自 config.json 的 collection_connectors") {
                VStack(alignment: .leading, spacing: 7) {
                    Picker("连接器", selection: $identity.connector) {
                        if identity.trimmedConnector.isEmpty {
                            Text("请选择连接器").tag("")
                        }
                        ForEach(connectorOptions, id: \.connector) { health in
                            Text(health.connector).tag(health.connector)
                        }
                    }
                    .labelsHidden()
                    .onChange(of: identity.connector) { _ in resetSearch() }

                    if let health = selectedConnectorHealth {
                        connectorHealthLine(health)
                    }
                }
            }
        }
    }

    private func connectorHealthLine(_ health: ATMCollectionConnectorHealth) -> some View {
        HStack(spacing: 6) {
            Image(systemName: health.statusIcon)
            Text(health.statusLabel)
            if health.isUnverified {
                Text("· 运行一次收集后才能确认")
                    .foregroundStyle(ATMTheme.secondary)
            } else if let error = health.error, !error.isEmpty {
                Text("· \(error)")
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .font(ATMFont.footnote)
        .foregroundStyle(ATMTheme.collectionHealthColor(health.status))
    }

    /// An existing source's identity in one read-only row. Three disabled text
    /// fields said the same thing while looking editable.
    private var lockedIdentityCard: some View {
        HStack(spacing: 10) {
            Image(systemName: collectionKindSymbol(source?.kind))
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 28, height: 28)
                .background(ATMTheme.controlFill, in: Circle())
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(source?.connector ?? "")
                        .font(ATMFont.mono(.footnote))
                    Text("·")
                        .foregroundStyle(ATMTheme.secondary)
                    Text(collectionKindLabel(source?.kind))
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Text(source?.externalID ?? "")
                    .font(ATMFont.mono(.footnote))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                    .textSelection(.enabled)
            }
            Spacer(minLength: 0)
            Label("已锁定", systemImage: "lock.fill")
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
        }
        .padding(11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 9))
    }

    private var searchField: some View {
        formField("搜索来源", hint: "连接器按名称查找并返回稳定 ID；找不到时可以手动填写") {
            VStack(alignment: .leading, spacing: 9) {
                Picker("范围", selection: $searchKind) {
                    ForEach(ATMCollectionSearchKind.allCases) { kind in
                        Text(kind.label).tag(kind)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .disabled(identity.trimmedConnector.isEmpty)
                .onChange(of: searchKind) { _ in
                    // The previous results answered a different question.
                    candidates = []
                    searchedKeyword = nil
                    searchError = nil
                }

                HStack(spacing: 8) {
                    TextField("名称或关键词", text: $keyword)
                        .textFieldStyle(.roundedBorder)
                        .focused($keywordFocused)
                        .onSubmit(search)
                    if isSearching {
                        ProgressView().controlSize(.small)
                    }
                    Button("搜索", action: search)
                        .disabled(!canSearch)
                }
                .disabled(identity.trimmedConnector.isEmpty)

                if let searchError {
                    Label(searchError, systemImage: "exclamationmark.triangle.fill")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.danger)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }

                Button("找不到？手动填写来源 ID") {
                    identity.manualEntry = true
                    identity.manualKind = searchKind == .all ? "group" : searchKind.rawValue
                }
                .buttonStyle(.link)
                .font(ATMFont.footnote)
                .disabled(identity.trimmedConnector.isEmpty)
            }
        }
    }

    private var canSearch: Bool {
        !identity.trimmedConnector.isEmpty
            && !keyword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !isSearching
    }

    @ViewBuilder
    private var candidateList: some View {
        if !candidates.isEmpty {
            VStack(alignment: .leading, spacing: 5) {
                Text("找到 \(candidates.count) 个结果，选一个")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                ScrollView {
                    VStack(spacing: 1) {
                        ForEach(candidates) { candidate in
                            candidateRow(candidate)
                        }
                    }
                    .padding(4)
                }
                .frame(maxHeight: 168)
                .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 9))
                .overlay {
                    RoundedRectangle(cornerRadius: 9)
                        .stroke(ATMTheme.border, lineWidth: 1)
                }
            }
        } else if !isSearching, let searchedKeyword, searchError == nil {
            Text("没有找到「\(searchedKeyword)」。换个更短、连续的关键词试试，或手动填写来源 ID。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func candidateRow(_ candidate: ATMCollectionCandidate) -> some View {
        Button {
            select(candidate)
        } label: {
            HStack(spacing: 9) {
                // The glyph is what separates a robot from the person it is
                // named after when both come back for the same keyword.
                Image(systemName: candidate.symbolName)
                    .frame(width: 18)
                    .foregroundStyle(ATMTheme.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(candidate.name)
                        .font(ATMFont.font(.body, weight: .medium))
                        .lineLimit(1)
                    HStack(spacing: 5) {
                        Text(collectionKindLabel(candidate.kind))
                        if let detail = candidate.detail, !detail.isEmpty {
                            Text("· \(detail)")
                                .lineLimit(1)
                        }
                    }
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            .atmRowSurface(isSelected: identity.selection == candidate)
        }
        .buttonStyle(.plain)
    }

    /// What the connector resolved, kept on screen after picking: the ID that
    /// gets saved is the part nobody can verify later from memory.
    private func selectedCandidateCard(_ candidate: ATMCollectionCandidate) -> some View {
        formField("已选择来源", hint: "由连接器解析，ATM 保存的就是这个 ID") {
            HStack(spacing: 10) {
                Image(systemName: candidate.symbolName)
                    .foregroundStyle(ATMTheme.success)
                    .frame(width: 28, height: 28)
                    .background(ATMTheme.successFill, in: Circle())
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(candidate.name)
                            .font(ATMFont.font(.body, weight: .semibold))
                            .lineLimit(1)
                        Text(collectionKindLabel(candidate.kind))
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    Text(candidate.externalID)
                        .font(ATMFont.mono(.footnote))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                        .textSelection(.enabled)
                }
                Spacer(minLength: 0)
                Button("更换") {
                    identity.selection = nil
                    keywordFocused = true
                }
                .controlSize(.small)
            }
            .padding(11)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 9))
        }
    }

    private var manualIdentifierFields: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 14) {
                formField("来源类型", hint: "由连接器定义") {
                    Picker("来源类型", selection: $identity.manualKind) {
                        ForEach(manualKindOptions, id: \.self) { kind in
                            Text("\(collectionKindLabel(kind))（\(kind)）").tag(kind)
                        }
                    }
                    .labelsHidden()
                }
                formField("来源 ID", hint: "连接器使用的稳定唯一标识") {
                    TextField("粘贴来源 ID", text: $identity.externalID)
                        .textFieldStyle(.roundedBorder)
                        .font(ATMFont.mono(.body))
                }
            }
            Button("返回按名称搜索") {
                identity.manualEntry = false
                identity.externalID = ""
                keywordFocused = true
            }
            .buttonStyle(.link)
            .font(ATMFont.footnote)
        }
    }

    /// Kinds worth offering: what this connector's existing sources already use,
    /// plus the shapes every chat connector has. Kinds are connector-defined, so
    /// this is a shortcut list rather than a closed set — anything unusual is
    /// still reachable by adding the source with the CLI.
    private var manualKindOptions: [String] {
        let used = store.collectionOverview.sources
            .filter { $0.connector == identity.trimmedConnector }
            .map(\.kind)
        var options = ["group", "user", "bot"]
        for kind in used + [identity.manualKind] where !options.contains(kind) && !kind.isEmpty {
            options.append(kind)
        }
        return options
    }

    // MARK: - 收集范围

    private var scopeSection: some View {
        settingsSection(title: "收集范围", icon: "line.3.horizontal.decrease.circle") {
            VStack(alignment: .leading, spacing: 14) {
                formField("排除关键词", hint: excludeHint) {
                    // Vertical axis: a real exclusion list runs past one line, and
                    // a single-line field hid everything but the last few words.
                    TextField("例如：闲聊, 打卡", text: $excludePattern, axis: .vertical)
                        .textFieldStyle(.roundedBorder)
                        .lineLimit(1...3)
                }
                formField("重点关注", hint: "用自然语言描述希望提取的内容；群聊消息不会被当作指令") {
                    TextField("例如：只关注 MR、需求和线上问题", text: $instruction, axis: .vertical)
                        .textFieldStyle(.roundedBorder)
                        .lineLimit(2...5)
                        .help("这是可信指令，聊天内容不是。")
                }
            }
        }
    }

    private var excludeHint: String {
        let count = excludePattern
            .split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .count
        let base = "包含这些词的消息会被跳过，多个词用逗号分隔"
        return count > 0 ? "\(base) · 已排除 \(count) 个词" : base
    }

    // MARK: - 处理方式

    private var processingSection: some View {
        settingsSection(title: "处理方式", icon: "slider.horizontal.3") {
            VStack(alignment: .leading, spacing: 16) {
                formField("处理策略", hint: strategyHint) {
                    Picker("处理策略", selection: $strategy) {
                        Label("提取任务", systemImage: "checklist").tag("tasks")
                        Label("沉淀知识", systemImage: "books.vertical").tag("observe")
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .onChange(of: strategy) { value in
                        // Only nudge the interval when it is still the other
                        // strategy's default; a frequency someone chose survives.
                        let previousDefault = value == "observe" ? 5 : 60
                        if intervalMinutes == previousDefault {
                            intervalMinutes = value == "observe" ? 60 : 5
                        }
                        if value == "observe" { autoDispatch = false }
                    }
                }

                formField("判定单位", hint: decisionUnitHint) {
                    Picker("判定单位", selection: $decisionUnit) {
                        Label("按时段", systemImage: "clock").tag("window")
                        Label("按消息", systemImage: "text.line.first.and.arrowtriangle.forward").tag("message")
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                }

                HStack(alignment: .top, spacing: 14) {
                    formField("采集频率", hint: "每次检查新消息的间隔") {
                        Stepper(value: $intervalMinutes, in: 1...1440) {
                            Text("每 \(intervalMinutes) 分钟")
                                .monospacedDigit()
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                    if strategy == "observe" {
                        formField("知识库集合", hint: "留空时沉淀到 inbox") {
                            TextField("inbox", text: $knowledgeCollection)
                                .textFieldStyle(.roundedBorder)
                        }
                    } else {
                        formField("默认优先级", hint: "用于新提取的任务") {
                            Picker("默认优先级", selection: $priority) {
                                ForEach(["P0", "P1", "P2", "P3"], id: \.self) { value in
                                    Text(priorityLabel(value)).tag(value)
                                }
                            }
                            .labelsHidden()
                        }
                        .frame(width: 150)
                    }
                }

                if strategy != "observe" {
                    formField("默认项目", hint: "新任务默认归属，可留空") {
                        TextField("例如：atm", text: $project)
                            .textFieldStyle(.roundedBorder)
                    }

                    Toggle(isOn: $autoDispatch) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("新 Todo 自动交给 Codex")
                                .font(ATMFont.font(.body, weight: .medium))
                            Text("使用受保护策略在项目目录启动 Agent；成功后进入待验收")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                        }
                    }
                    .toggleStyle(.switch)
                }
            }
        }
    }

    // MARK: - 通用外壳

    private func settingsSection<Content: View>(
        title: String,
        icon: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 9) {
            Label(title, systemImage: icon)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)

            content()
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 12))
                .overlay {
                    RoundedRectangle(cornerRadius: 12)
                        .stroke(ATMTheme.border, lineWidth: 1)
                }
        }
    }

    private func formField<Content: View>(
        _ title: String,
        hint: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(ATMFont.font(.body, weight: .medium))
            content()
                .frame(maxWidth: .infinity)
            Text(hint)
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var strategyHint: String {
        strategy == "observe"
            ? "识别可复用信息并写入知识库，不创建任务"
            : "从消息中识别需求、缺陷和待办，创建或补充任务"
    }

    /// One batch yields one decision, so this is what decides how many separate
    /// events can survive the same window — not merely how work is split up.
    private var decisionUnitHint: String {
        decisionUnit == "message"
            ? "每条消息单独判定，同一时段的其他消息只作上下文。通知机器人这类「一条消息就是一件事」的来源用这个"
            : "同一会话、间隔 15 分钟内的消息合并判定，得到一个结果。聊天用这个：一句请求和随后的补充说明是同一件事"
    }

    private func priorityLabel(_ value: String) -> String {
        ATMTodoPriorityStyle.label(value)
    }

    private var subtitle: String {
        if source != nil {
            return "连接器与来源 ID 已锁定；名称、筛选范围与处理方式都可以改。"
        }
        return "选一个连接器，按名称找到会话，其余保持默认也能用。"
    }

    private var namePlaceholder: String {
        identity.selection?.name ?? source?.externalID ?? "例如：需求讨论群"
    }

    // MARK: - 搜索

    private func select(_ candidate: ATMCollectionCandidate) {
        identity.selection = candidate
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedName.isEmpty || trimmedName == autoFilledName {
            name = candidate.name
            autoFilledName = candidate.name
        }
    }

    private func resetSearch() {
        candidates = []
        identity.selection = nil
        searchedKeyword = nil
        searchError = nil
    }

    @MainActor
    private func search() {
        let trimmed = keyword.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSearch else { return }
        isSearching = true
        searchError = nil
        Task {
            let (found, error) = await store.searchCollectionSources(
                connector: identity.trimmedConnector, kind: searchKind.rawValue, keyword: trimmed
            )
            candidates = found
            searchError = error
            searchedKeyword = trimmed
            // One result is not ambiguous, so picking it saves a click; more than
            // one has to be chosen, and the previous pick is no longer in the list.
            identity.selection = nil
            if found.count == 1, let only = found.first { select(only) }
            isSearching = false
        }
    }
}

/// A look at what a source has been saying lately. Deliberately plain: no
/// paging, no search, no editing — catching up at a glance is the whole job, and
/// `atm collect search` is there for digging.
private struct CollectionHistorySheet: View {
    @ObservedObject var store: ATMDataStore
    let source: ATMCollectionSource
    let onClose: () -> Void

    @State private var messages: [ATMCollectionMessage] = []
    @State private var errorMessage: String?
    @State private var isStale = false
    @State private var isLoading = true
    /// True while the shown messages came from the local archive and the connector read
    /// is still in flight — the sheet is readable, just not caught up yet.
    @State private var isCatchingUp = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(source.displayName)
                        .font(ATMFont.font(.title3, weight: .bold))
                    Text(recentSubtitle)
                        .font(ATMFont.footnote)
                        .foregroundStyle(isStale ? .orange : ATMTheme.secondary)
                }
                Spacer()
                if isLoading {
                    ProgressView().controlSize(.small)
                } else {
                    Button {
                        Task { await refresh() }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.plain)
                    .help("重新读取")
                }
            }

            if let errorMessage {
                Text(errorMessage)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .fixedSize(horizontal: false, vertical: true)
            }

            ScrollView {
                if messages.isEmpty && !isLoading && errorMessage == nil {
                    Text("这段时间没有消息。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                } else {
                    VStack(alignment: .leading, spacing: 10) {
                        ForEach(messages) { message in
                            VStack(alignment: .leading, spacing: 3) {
                                HStack(spacing: 6) {
                                    Text(message.sender?.isEmpty == false ? message.sender! : "—")
                                        .font(ATMFont.font(.footnote, weight: .semibold))
                                    Text(message.time)
                                        .font(ATMFont.caption)
                                        .foregroundStyle(ATMTheme.secondary)
                                }
                                Text(message.content)
                                    .font(ATMFont.mono(.footnote))
                                    .fixedSize(horizontal: false, vertical: true)
                                    .textSelection(.enabled)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                }
            }
            .frame(height: 380)

            HStack {
                Spacer()
                Button("关闭", action: onClose)
                    .keyboardShortcut(.cancelAction)
            }
        }
        .padding(24)
        .frame(width: 560)
        .task { await load() }
    }

    private var recentSubtitle: String {
        if isCatchingUp {
            return "最近 \(messages.count) 条 · 本地已同步的记录，正在读取最新…"
        }
        if isStale {
            return "最近 \(messages.count) 条 · 连接器读取失败，显示本地已同步的记录"
        }
        return "最近 \(messages.count) 条 · 实时读取并同步到本地"
    }

    /// Two phases: the local archive answers in about a tenth of a second so the
    /// sheet is readable immediately, then the connector read replaces it with whatever
    /// has been said since. Waiting only on the network would leave this spinning
    /// for ~2s every time, including on a conversation just looked at.
    @MainActor
    private func load() async {
        isLoading = true
        errorMessage = nil
        let (cached, _) = await store.collectionHistory(source: source, local: true)
        if let cached, !cached.messages.isEmpty {
            messages = cached.messages
            isStale = false
            isCatchingUp = true
        }
        await refresh()
    }

    @MainActor
    private func refresh() async {
        isLoading = true
        let (history, error) = await store.collectionHistory(source: source)
        if let history {
            messages = history.messages
            // The CLI serves the archive itself when the connector is unreachable and marks
            // it stale; the subtitle explains that, so it is not an error here.
            isStale = history.stale == true
            errorMessage = nil
        } else {
            // Nothing came back. Anything already on screen came off disk, so
            // keep it and say it is behind rather than blanking the sheet.
            isStale = !messages.isEmpty
            errorMessage = messages.isEmpty ? error : nil
        }
        isCatchingUp = false
        isLoading = false
    }
}

/// 删除确认里要说的两件事。一是记录和 Todo 是两回事：Todo 可能已经有人在做了，
/// 不能因为清掉一条来源备注就跟着消失；判断本身错了该走「撤销自动处理」。二是刚
/// 收集到的消息还在下一轮的重读窗口里，那一轮会把这条记录重新建出来——不说，看着
/// 就像删除失败。
private func collectionDeleteWarning(for item: ATMCollectionItem) -> String {
    if let todoID = item.todoID, !todoID.isEmpty {
        return "记录会从本地删除，\(todoID) 保留。如果是这次判断错了，请先用「撤销自动处理」。"
    }
    return "记录会从本地删除。刚收集到的记录可能在下一轮重新出现，更早的删掉就不会再回来。"
}

private func collectionActionTitle(_ action: String, retryStopped: Bool = false) -> String {
    switch action {
    case "create": return "已创建"
    case "append": return "已补充"
    case "insight": return "已沉淀"
    case "ignore": return "无需处理"
    // 「等待重试」听着像在等人按一下；实际是下一轮 collect 自动重来。重试预算用尽后
    // 就真的没有下一轮了，这时才需要人出手，两种情况必须分开说。
    case "failed": return retryStopped ? "重试已停止" : "下轮自动重试"
    case "reverted": return "已撤销"
    default: return "等待处理"
    }
}

private func collectionActionIcon(_ action: String, retryStopped: Bool = false) -> String {
    switch action {
    // 行本身已经提供统一的圆形底板，这里只返回裸字形；否则 plus.circle、book、
    // branch 等符号各自带一套轮廓，虽然布局 frame 相同，视觉宽度仍然忽大忽小。
    case "create": return "plus"
    case "append": return "arrow.down"
    case "insight": return "book.closed"
    case "ignore": return "minus"
    case "failed": return retryStopped ? "exclamationmark" : "arrow.clockwise"
    case "reverted": return "arrow.uturn.backward"
    default: return "clock"
    }
}

/// 动作色。create / failed 是状态，走状态色；insight / reverted 是分类，走
/// ATMTheme.palette，不再另开裸 `.purple` / `.indigo`。
///
/// 会自己好的失败不配红色：一次连接器抖动在下一轮就没了，用 danger 画它等于把
/// 「无需处理」喊成事故，整页扫过去只剩红点。红色留给重试已经停下、真的在等人的那一种。
private func collectionActionColor(_ action: String, retryStopped: Bool = false) -> Color {
    switch action {
    case "create": return ATMTheme.success
    case "append": return ATMTheme.accent
    case "insight": return ATMTheme.palette[2]
    case "ignore": return ATMTheme.secondary
    case "failed": return retryStopped ? ATMTheme.danger : ATMTheme.secondary
    case "reverted": return ATMTheme.palette[5]
    default: return ATMTheme.warning
    }
}

private func collectionRelativeTime(_ timestamp: Int64) -> String {
    let elapsed = max(Int(Date().timeIntervalSince1970) - Int(timestamp), 0)
    if elapsed < 60 { return "刚刚" }
    if elapsed < 3_600 { return "\(elapsed / 60) 分钟前" }
    if elapsed < 86_400 { return "\(elapsed / 3_600) 小时前" }
    return "\(elapsed / 86_400) 天前"
}

private func collectionNextRunText(_ overview: ATMCollectionOverview) -> String {
    guard let latest = overview.latestRun else { return "即将运行" }
    let next = Int(latest.startedAt) + max(overview.intervalMinutes, 1) * 60
    let remaining = next - Int(Date().timeIntervalSince1970)
    if remaining <= 0 { return "即将运行" }
    if remaining < 60 { return "下次 <1 分钟" }
    return "下次 \((remaining + 59) / 60) 分钟"
}
