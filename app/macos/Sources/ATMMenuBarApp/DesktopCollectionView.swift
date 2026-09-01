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
        guard let groupName else {
            return items.count > 1 ? "删除主记录及 \(items.count - 1) 条补充？" : "删除这条处理记录？"
        }
        return "清空「\(groupName)」的 \(items.count) 条记录？"
    }

    var confirmLabel: String {
        groupName == nil ? "删除记录" : "清空 \(items.count) 条"
    }

    var message: String {
        guard groupName != nil else {
            if items.count > 1 {
                return "主记录和折叠在详情里的 \(items.count - 1) 条补充会一起从本地删除，关联 Todo 保留。刚收集到的记录可能在下一轮重新出现。"
            }
            return items.first.map(collectionDeleteWarning(for:)) ?? ""
        }
        let kept = items.compactMap(\.todoID).filter { !$0.isEmpty }.count
        let todos = kept > 0 ? "，其中 \(kept) 条写出的 Todo 保留" : ""
        return "\(items.count) 条记录会从本地删除\(todos)。刚收集到的记录可能在下一轮重新出现，更早的删掉就不会再回来。"
    }
}

struct DesktopCollectionView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var showingAddSource = false
    @State private var editingSourceID: String?
    @State private var deleteCandidate: ATMCollectionSource?
    @State private var itemDeletion: CollectionItemDeletion?
    @State private var historySource: ATMCollectionSource?
    @State private var showingIgnoredItems = false
    @State private var showingCollectionSettings = false
    @State private var drawerTab = CollectionDrawerTab.records
    @State private var selectedSourceID: String?
    @State private var draggedSourceID: String?
    @AppStorage("ATMCollapsedCollectionSourceGroups") private var collapsedSourceGroupsRaw = ""
    @AppStorage(ATMManualOrder.collectionSourcesKey) private var collectionSourceOrder = ""
    @AppStorage(ATMNavigatorPresentationPreferences.collectionKey)
    private var recordListPresentationRaw = ATMNavigatorPresentationPreferences.defaultValue

    private var collapsedSourceGroups: Set<String> {
        Set(collapsedSourceGroupsRaw.split(separator: ",").map(String.init))
    }

    private var recordListPresentation: ATMNavigatorPresentation {
        ATMNavigatorPresentation.resolve(recordListPresentationRaw)
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

    private var groupedItems: [ATMCollectionItem] {
        ATMCollectionItemGrouping.visibleItems(filteredItems)
    }

    private var selectedItem: ATMCollectionItem? {
        guard let id = navigation.selectedCollectionItemID else { return displayedItems.first }
        return displayedItems.first { $0.id == id } ?? displayedItems.first
    }

    private var selectedSource: ATMCollectionSource? {
        let sources = orderedSources
        guard let selectedSourceID else { return sources.first }
        return sources.first { $0.id == selectedSourceID } ?? sources.first
    }

    private var orderedSources: [ATMCollectionSource] {
        ATMManualOrder.ordered(
            store.collectionOverview.sources,
            stored: collectionSourceOrder,
            id: \.id
        )
    }

    private var primaryItems: [ATMCollectionItem] {
        groupedItems.filter { !shouldCollapse($0) }
    }

    private var ignoredItems: [ATMCollectionItem] {
        groupedItems.filter(shouldCollapse)
    }

    private var displayedItems: [ATMCollectionItem] {
        if recordListPresentation == .flat { return flattenedItems }
        return showingIgnoredItems ? primaryItems + ignoredItems : primaryItems
    }

    /// Match grouped presentation order: configured sources first, orphaned
    /// records next, and records normally folded under the closed section last.
    private var flattenedItems: [ATMCollectionItem] {
        let sourceItems = orderedSources.flatMap { source in
            primaryItems.filter { $0.sourceID == source.id }
        }
        let unknownItems = primaryItems.filter { source(for: $0) == nil }
        return sourceItems + unknownItems + ignoredItems
    }

    var body: some View {
        ATMSplitColumn(
            id: "collection",
            defaultWidth: ATMWorkspaceLayout.navigatorDefaultWidth,
            minWidth: ATMWorkspaceLayout.navigatorMinWidth,
            maxWidth: ATMWorkspaceLayout.navigatorMaxWidth,
            detailMinWidth: ATMWorkspaceLayout.objectDetailMinWidth
        ) {
            ATMGroupedNavigator {
                collectionDrawerTabs
            } content: {
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
        } detail: {
            detailColumn
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .atmAnimatedSwap(collectionDetailIdentity, style: .detail)
        }
        .background(Color.clear)
        .onAppear {
            store.refreshCollection()
            selectDefaultItem()
            selectDefaultSource()
            revealSelectedSourceGroup()
            markSelectedItemRead()
        }
        .onChange(of: store.collectionOverview.items.map(\.id)) { _ in
            selectDefaultItem()
            markSelectedItemRead()
        }
        .onChange(of: store.collectionOverview.sources.map(\.id)) { _ in selectDefaultSource() }
        .onChange(of: drawerTab) { tab in
            if tab == .sources {
                selectDefaultSource()
            } else {
                editingSourceID = nil
            }
        }
        .onChange(of: showingIgnoredItems) { _ in selectDefaultItem() }
        .onChange(of: recordListPresentationRaw) { _ in
            selectDefaultItem()
            revealSelectedSourceGroup()
        }
        .onChange(of: navigation.selectedCollectionItemID) { _ in
            revealSelectedSourceGroup()
            markSelectedItemRead()
        }
        .onChange(of: navigation.collectionItemRevealRequest) { _ in
            drawerTab = .records
            revealSelectedSourceGroup()
            markSelectedItemRead()
        }
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
        ATMNavigatorHeader {
            ATMCompactSegmentedTabs(
                selection: $drawerTab,
                items: [(.records, "记录"), (.sources, "来源")]
            )
        } trailing: {
            HStack(spacing: ATMSpacing.xSmall) {
                if drawerTab == .records {
                    ATMNavigatorPresentationToggle(storedValue: $recordListPresentationRaw)
                }
                ATMIconButton(
                    systemImage: "gearshape",
                    help: "收集设置",
                    chrome: .bare,
                    side: 30,
                    iconTier: .bodyLarge
                ) {
                    showingCollectionSettings.toggle()
                }
                .popover(isPresented: $showingCollectionSettings, arrowEdge: .top) {
                    CollectionAutomationSettings(store: store)
                }
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
        }
    }

    /// 采集失败有来源可归属，右栏的“采集状态”卡片已经说了；但添加/删除来源、
    /// 修正、撤销、生成知识文档失败没有来源可挂，只写进 store 的共享错误。
    /// 少了这条横幅，它们在触发操作的工作区里完全没有反馈。
    @ViewBuilder
    private var workspaceErrorBanner: some View {
        if let error = workspaceError {
            let presentation = ATMErrorPresentation.resolve(error, fallbackTitle: "操作失败")
            let prompt = ATMCollectionWorkspaceNotice.loginPrompt(for: store.collectionOverview)
            let waiting = prompt.map { store.awaitingLoginConnector == $0.connector } ?? false
            ATMInlineNotice(
                severity: .warning,
                title: presentation.title,
                message: presentation.message,
                details: error,
                // The one failure a person can end: offer the login itself rather than
                // a banner that only says it expired. After the terminal is open the
                // same button becomes the retry — nobody has to guess when a browser
                // flow finished, and the forced run beats the background backoff.
                actionTitle: prompt == nil ? nil : (waiting ? "登录好了，立即重试" : "重新登录"),
                actionSystemImage: waiting ? "arrow.clockwise" : "person.crop.circle.badge.checkmark",
                isActionEnabled: !store.isCollecting,
                onAction: prompt.map { prompt in
                    {
                        if waiting {
                            store.retryCollectionAfterLogin()
                        } else {
                            store.startConnectorLogin(prompt)
                        }
                    }
                },
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
                ATMEmptyState(
                    icon: "tray.2",
                    title: "还没有收集来源",
                    detail: "点击右上角添加来源，ATM 会按设定周期自动收集。"
                )
            } else {
                ScrollView {
                    // Lazy rows scrolled out of view are not drop targets, so a
                    // source cannot be dragged past the top of the viewport; the
                    // right-click 上移/下移 pair is the answer for that, not a plain
                    // `VStack` — rows size off `maxWidth: .infinity`, which needs the
                    // definite width proposal only the lazy stack passes down.
                    LazyVStack(spacing: 0) {
                        ForEach(orderedSources) { source in
                            sourceManagementRow(source)
                                .atmContentStackRow()
                                .atmManualOrderRow(
                                    id: source.id,
                                    title: source.displayName,
                                    dragged: $draggedSourceID,
                                    move: moveCollectionSource
                                )
                        }
                    }
                    .padding(.horizontal, ATMGroupedNavigatorMetrics.contentHorizontalInset)
                    .padding(.vertical, ATMGroupedNavigatorMetrics.contentVerticalInset)
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
            ATMNavigatorRow(isSelected: selected) {
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
            } content: {
                VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                    Text(source.displayName)
                        .font(ATMFont.font(.body, weight: .medium))
                        .foregroundStyle(ATMTheme.primary)
                        .lineLimit(1)
                    Text(
                        "\(source.connector) · \(collectionKindLabel(source.kind)) · 每 \(source.effectiveIntervalMinutes) 分钟"
                    )
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                }
            } trailing: {
                HStack(spacing: 5) {
                    // Mute has no other visible trace: without a glyph here the
                    // only symptom is a banner that never comes.
                    if !source.notifiesDesktop {
                        Image(systemName: "speaker.slash.fill")
                            .font(ATMFont.font(.caption, weight: .medium))
                            .foregroundStyle(ATMTheme.secondary)
                            .help("桌面通知已静默，仍照常收集并计入未读")
                    }
                    Circle()
                        .fill(sourceStatusColor(source))
                        .frame(width: 7, height: 7)
                    Text(sourceStatusText(source))
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                .fixedSize()
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.atmRow)
        .help("查看来源配置；拖动可调整顺序")
        .atmRightClickMenu {
            ATMMenuItem("查看聊天记录") { historySource = source }
            ATMMenuItem("编辑") { editSource(source) }
            ATMMenuItem(source.enabled ? "暂停" : "启用") {
                store.setCollectionSource(source, enabled: !source.enabled)
            }
            ATMMenuItem(source.notifiesDesktop ? "静默通知" : "恢复通知") {
                store.setCollectionSource(source, muted: source.notifiesDesktop)
            }
            ATMMenuSeparator()
            ATMManualOrder.moveMenuEntries(
                for: source.id,
                in: orderedSources.map(\.id),
                move: moveCollectionSource
            )
            ATMMenuSeparator()
            ATMMenuItem("删除", destructive: true) { deleteCandidate = source }
        }
    }

    private var itemColumn: some View {
        VStack(spacing: 0) {
            if filteredItems.isEmpty {
                ATMEmptyState(
                    icon: "tray",
                    title: "暂无处理记录",
                    detail: "添加来源后，ATM 会在后台自动收集。"
                )
            } else {
                ATMGroupedNavigatorScroll {
                    if recordListPresentation == .grouped {
                        ForEach(orderedSources) { source in
                            let items = primaryItems.filter { $0.sourceID == source.id }
                            if !items.isEmpty {
                                let expanded = expandedBinding(for: source.id)
                                ATMNavigatorGroup {
                                    sourceSectionHeader(source, items: items, expanded: expanded)
                                } content: {
                                    if expanded.wrappedValue {
                                        ForEach(items) { item in
                                            itemRow(item)
                                        }
                                    }
                                }
                            }
                        }

                        let unknownItems = primaryItems.filter { source(for: $0) == nil }
                        if !unknownItems.isEmpty {
                            let expanded = expandedBinding(for: "__unknown__")
                            ATMNavigatorGroup {
                                genericSourceSectionHeader(
                                    "其他来源",
                                    systemImage: "questionmark.folder",
                                    items: unknownItems,
                                    expanded: expanded,
                                    clear: { requestClear("其他来源", items: unknownItems) }
                                )
                            } content: {
                                if expanded.wrappedValue {
                                    ForEach(unknownItems) { item in
                                        itemRow(item)
                                    }
                                }
                            }
                        }

                        if !ignoredItems.isEmpty {
                            ATMNavigatorGroup {
                                genericSourceSectionHeader(
                                    "已保存与已了结",
                                    items: ignoredItems,
                                    expanded: $showingIgnoredItems,
                                    clear: { requestClear("已保存与已了结", items: ignoredItems) }
                                )
                            } content: {
                                if showingIgnoredItems {
                                    ForEach(ignoredItems) { item in
                                        itemRow(item).opacity(0.78)
                                    }
                                }
                            }
                        }
                    } else {
                        ForEach(flattenedItems) { item in
                            itemRow(item, showsSource: true)
                                .opacity(shouldCollapse(item) ? 0.78 : 1)
                        }
                    }
                }
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
        itemDeletion = CollectionItemDeletion(
            items: recordsIncludingSupplements(items),
            groupName: groupName
        )
    }

    private func sourceSectionHeader(
        _ source: ATMCollectionSource,
        items: [ATMCollectionItem],
        expanded: Binding<Bool>
    ) -> some View {
        ATMNavigatorGroupHeader(
            title: source.displayName,
            count: items.count,
            tint: source.enabled ? ATMTheme.accent : ATMTheme.secondary,
            systemImage: source.symbolName,
            isExpanded: expanded
        ) {
            HStack(spacing: ATMSpacing.small) {
                if !source.enabled {
                    Text("已暂停")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                sectionActionMenu {
                    collectionGroupActions(for: items)
                    if hasCollectionGroupActions(for: items) {
                        Divider()
                    }
                    Button("查看聊天记录") {
                        historySource = source
                    }
                    Button("编辑") { editSource(source) }
                    Button(source.enabled ? "暂停" : "启用") {
                        store.setCollectionSource(source, enabled: !source.enabled)
                    }
                    Button(source.notifiesDesktop ? "静默通知" : "恢复通知") {
                        store.setCollectionSource(source, muted: source.notifiesDesktop)
                    }
                    Divider()
                    Button("清空记录", role: .destructive) {
                        requestClear(source.displayName, items: items)
                    }
                    Button("删除来源", role: .destructive) { deleteCandidate = source }
                }
            }
        }
    }

    private func genericSourceSectionHeader(
        _ title: String,
        systemImage: String? = nil,
        items: [ATMCollectionItem],
        expanded: Binding<Bool>,
        clear: (() -> Void)? = nil
    ) -> some View {
        ATMNavigatorGroupHeader(
            title: title,
            count: items.count,
            tint: ATMTheme.secondary,
            systemImage: systemImage,
            isExpanded: expanded
        ) {
            if let clear {
                sectionActionMenu {
                    collectionGroupActions(for: items)
                    if hasCollectionGroupActions(for: items) {
                        Divider()
                    }
                    Button("清空记录", role: .destructive, action: clear)
                }
            }
        }
        .foregroundStyle(ATMTheme.secondary)
    }

    /// 分组标题右侧的省略号菜单。三种分组（来源、其他来源、已保存与已了结）用同一个，
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

    /// Bulk actions belong to the group they affect. Keeping them in the section
    /// menu avoids crowding the narrow navigator toolbar and makes the scope of
    /// “全部” explicit.
    @ViewBuilder
    private func collectionGroupActions(for items: [ATMCollectionItem]) -> some View {
        let records = recordsIncludingSupplements(items)
        let unread = records.filter(\.isUnread)
        let settleable = records.filter(\.isSettleableConclusion)
        if !unread.isEmpty {
            Button("全部标为已读") {
                store.setCollectionItemsRead(unread, read: true)
            }
        }
        if !settleable.isEmpty {
            Button("全部了结") {
                store.setCollectionItemsArchived(settleable, archived: true)
            }
            .disabled(store.isCollecting)
        }
    }

    private func hasCollectionGroupActions(for items: [ATMCollectionItem]) -> Bool {
        let records = recordsIncludingSupplements(items)
        return records.contains(where: \.isUnread)
            || records.contains(where: \.isSettleableConclusion)
    }

    private func itemRow(_ item: ATMCollectionItem, showsSource: Bool = false) -> some View {
        // 选中判定比的是 `selectedItem`，不是 selectedCollectionItemID —— 后者为 nil 时
        // 详情栏会回退展示首条，直接比 ID 会出现「右栏有内容、中栏没高亮」。
        let selected = selectedItem?.id == item.id
        let records = [item] + supplements(for: item)
        let rowUnreadCount = records.filter(\.isUnread).count
        return Button {
            if rowUnreadCount > 0 {
                store.setCollectionItemsRead(records, read: true)
            }
            navigation.selectedCollectionItemID = item.id
        } label: {
            CollectionItemRow(
                item: item,
                supplementCount: records.count - 1,
                unreadCount: rowUnreadCount,
                isSelected: selected,
                sourceName: showsSource ? source(for: item)?.displayName ?? "其他来源" : nil
            )
        }
        .buttonStyle(.atmRow)
        .focusable(false)
        .atmContentStackRow()
        // 右键只放导航、了结和删除。重新处理、修正、撤销这些要看记录状态才知道能不能做，
        // 判定在详情栏（见 CollectionItemDetail），在这儿抄一遍就是抄两套规则。
        .atmRightClickMenu {
            if rowUnreadCount > 0 {
                ATMMenuItem("标为已读", systemImage: "checkmark.circle") {
                    store.setCollectionItemsRead(records, read: true)
                }
            } else {
                ATMMenuItem("标为未读", systemImage: "circle.fill") {
                    store.setCollectionItemsRead(records, read: false)
                }
            }
            if item.todoID != nil {
                ATMMenuItem("打开 Todo") { openTodo(item) }
            }
            ATMMenuItem("复制记录 ID", systemImage: "number") {
                copyCollectionItemID(item.id)
            }
            ATMMenuSeparator()
            if item.isArchived {
                ATMMenuItem("重新打开", systemImage: "arrow.uturn.backward") {
                    store.setCollectionItemsArchived(records, archived: false)
                }
            } else if !item.shouldCollapseInCollection {
                ATMMenuItem("了结记录", systemImage: "archivebox") {
                    store.setCollectionItemsArchived(records, archived: true)
                }
            }
            ATMMenuItem("删除记录", destructive: true) {
                itemDeletion = CollectionItemDeletion(items: recordsIncludingSupplements([item]))
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
                onToggleMute: { store.setCollectionSource(source, muted: source.notifiesDesktop) },
                onCollect: { store.runCollectionNow(source: source) },
                onDelete: { deleteCandidate = source }
            )
        } else if drawerTab == .sources {
            ATMDetailBodySurface {
                ATMEmptyState(
                    icon: "tray.2",
                    title: "还没有收集来源",
                    detail: "在中栏点击添加来源后，这里会显示它的配置。",
                    size: .inline,
                    minHeight: 180
                )
            }
        } else if let item = selectedItem {
            CollectionItemDetail(
                store: store,
                item: item,
                source: source(for: item),
                supplements: supplements(for: item),
                openTodo: { openTodo(item) },
                openKnowledge: { collection, documentID in
                    navigation.selectedKnowledgeLibraryID = collection
                    navigation.locateKnowledgeDocumentID = "document:\(documentID)"
                    navigation.section = .knowledge
                }
            )
        } else {
            ATMDetailBodySurface {
                ATMEmptyState(
                    icon: "doc.text.magnifyingglass",
                    title: "选择一条处理记录",
                    size: .inline,
                    minHeight: 180
                )
            }
        }
    }

    private func openTodo(_ item: ATMCollectionItem) {
        guard let todoID = item.todoID else { return }
        navigation.taskListMode = item.todoArchived == true ? .archive : .active
        navigation.selectedTodoID = todoID
        navigation.section = .tasks
    }

    private func source(for item: ATMCollectionItem) -> ATMCollectionSource? {
        store.collectionOverview.sources.first { $0.id == item.sourceID }
    }

    private func supplements(for item: ATMCollectionItem) -> [ATMCollectionItem] {
        ATMCollectionItemGrouping.supplements(for: item, in: filteredItems)
    }

    private func recordsIncludingSupplements(_ items: [ATMCollectionItem]) -> [ATMCollectionItem] {
        var seen = Set<String>()
        return items.flatMap { [$0] + supplements(for: $0) }.filter { seen.insert($0.id).inserted }
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
        let sources = orderedSources
        guard !sources.contains(where: { $0.id == selectedSourceID }) else { return }
        selectedSourceID = sources.first?.id
    }

    private func moveCollectionSource(_ draggedID: String, _ targetID: String) {
        collectionSourceOrder = ATMManualOrder.moving(
            draggedID,
            over: targetID,
            stored: collectionSourceOrder,
            fallback: orderedSources.map(\.id)
        )
    }

    private func revealSelectedSourceGroup() {
        guard recordListPresentation == .grouped,
              let item = selectedItem,
              !shouldCollapse(item) else { return }
        let groupID = source(for: item)?.id ?? "__unknown__"
        var set = collapsedSourceGroups
        guard set.remove(groupID) != nil else { return }
        collapsedSourceGroupsRaw = set.sorted().joined(separator: ",")
    }

    private func markSelectedItemRead() {
        guard drawerTab == .records, let item = selectedItem else { return }
        let records = [item] + supplements(for: item)
        guard records.contains(where: \.isUnread) else { return }
        store.setCollectionItemsRead(records, read: true)
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
    let onToggleMute: () -> Void
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
                    ATMDetailBodySurface {
                        VStack(alignment: .leading, spacing: 14) {
                            runCard
                            identityCard
                            scheduleCard
                            rulesCard
                        }
                        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
                        .padding(.vertical, 24)
                        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
        .background(Color.clear)
    }

    private var header: some View {
        ATMDetailHeader(title: source.displayName, titleLineLimit: 2) {
            Label("收集来源", systemImage: source.symbolName)
                .font(ATMFont.footnote)
                .foregroundStyle(source.enabled ? ATMTheme.accent : ATMTheme.secondary)
        } actions: {
            HStack(spacing: 6) {
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
            }
            .fixedSize()
        } meta: {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 14) {
                    sourceStatus
                    Spacer(minLength: 8)
                    sourceToggles
                }
                VStack(alignment: .leading, spacing: 10) {
                    sourceStatus
                    sourceToggles
                }
            }
        }
    }

    private var sourceStatus: some View {
        Label(sourceStatusText, systemImage: sourceStatusIcon)
            .font(ATMFont.caption)
            .foregroundStyle(sourceStatusColor)
    }

    private var sourceToggles: some View {
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

            Toggle(
                "桌面通知",
                isOn: Binding(
                    get: { source.notifiesDesktop },
                    set: { _ in onToggleMute() }
                )
            )
            .toggleStyle(.switch)
            .controlSize(.small)
            .help("关掉只是不再弹通知：这个来源照常收集，结果照常计入未读")
        }
        .fixedSize()
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
                    "新建 \(run.createdCount) · 补充 \(run.appendedCount) · 结论 \(run.insightCount) · 忽略 \(run.ignoredCount)"
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
            if let connectorHealth {
                Divider()
                CollectionConnectorHealthSummary(health: connectorHealth, showsName: false)
            }
        }
    }

    private var connectorHealth: ATMCollectionConnectorHealth? {
        store.collectionOverview.connectorHealth.first {
            $0.connector.caseInsensitiveCompare(source.connector) == .orderedSame
        }
    }

    private var scheduleCard: some View {
        sourceCard("自动收集", systemImage: "clock") {
            sourceValueRow("来源开关", source.enabled ? "已启用" : "已暂停")
            sourceValueRow(
                "桌面通知",
                source.notifiesDesktop ? "有新收集会提醒" : "已静默，仍计入未读"
            )
            sourceValueRow("自动调度", store.collectionOverview.enabled ? "正在运行" : "总开关已关闭")
            sourceValueRow("间隔", "每 \(source.effectiveIntervalMinutes) 分钟")
            sourceValueRow("处理方式", source.effectiveStrategy == "observe" ? "收集结论，按需保存" : "创建或补充 Todo")
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
                    sourceValueRow("默认保存到", knowledgeCollection)
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

/// Global scheduling belongs to the Collection workspace: it controls when all
/// enabled sources are checked, while source-specific switches and rules stay on
/// each source. Keeping this in a popover makes it available from both 记录 and 来源
/// without recreating a second source-management screen under Settings.
private struct CollectionAutomationSettings: View {
    @ObservedObject var store: ATMDataStore

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "tray.and.arrow.down")
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    .foregroundStyle(ATMTheme.accent)
                    .frame(width: 34, height: 34)
                    .background(ATMTheme.accentFill, in: RoundedRectangle(cornerRadius: 9))
                VStack(alignment: .leading, spacing: 2) {
                    Text("收集设置")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("全局调度与连接器状态")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("刷新") { store.refreshCollection() }
                    .controlSize(.small)
            }
            .padding(18)

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    VStack(alignment: .leading, spacing: 12) {
                        HStack(alignment: .top, spacing: 14) {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("自动收集")
                                    .font(ATMFont.font(.body, weight: .semibold))
                                Text("ATM 常驻期间自动检查已启用来源；单个来源仍可暂停或手动收集。")
                                    .font(ATMFont.footnote)
                                    .foregroundStyle(ATMTheme.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            Spacer(minLength: 12)
                            Toggle(
                                "自动收集",
                                isOn: Binding(
                                    get: { store.collectionOverview.enabled },
                                    set: { store.setCollectionEnabled($0) }
                                )
                            )
                            .labelsHidden()
                            .toggleStyle(.switch)
                            .controlSize(.small)
                        }

                        Stepper(
                            "后台检查间隔：\(store.collectionOverview.intervalMinutes) 分钟",
                            value: Binding(
                                get: { store.collectionOverview.intervalMinutes },
                                set: { store.setCollectionInterval($0) }
                            ),
                            in: 1...60
                        )
                        .font(ATMFont.body)

                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Text("分类模型")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                                .frame(width: 72, alignment: .leading)
                            Text(store.collectionOverview.model)
                                .font(ATMFont.mono(.footnote))
                                .textSelection(.enabled)
                            Spacer(minLength: 0)
                        }
                    }

                    Divider()

                    VStack(alignment: .leading, spacing: 12) {
                        Text("连接器")
                            .font(ATMFont.font(.body, weight: .semibold))
                        if store.collectionOverview.connectorHealth.isEmpty {
                            Label("尚未配置连接器", systemImage: "link.badge.plus")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                        } else {
                            ForEach(store.collectionOverview.connectorHealth, id: \.connector) { health in
                                CollectionConnectorHealthSummary(health: health)
                                if health.connector != store.collectionOverview.connectorHealth.last?.connector {
                                    Divider()
                                }
                            }
                        }
                    }
                }
                .padding(18)
            }
            .frame(maxHeight: 520)
        }
        .frame(width: 440)
        .background(ATMTheme.elevated)
        .onAppear { store.refreshCollection() }
    }
}

/// The same connector truth is shown in the global collection control and on
/// every source that uses it. That keeps status vocabulary and error handling
/// from drifting between two screens again.
private struct CollectionConnectorHealthSummary: View {
    let health: ATMCollectionConnectorHealth
    var showsName = true

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 8) {
                if showsName {
                    Text(health.connector)
                        .font(ATMFont.mono(.footnote))
                        .frame(width: 92, alignment: .leading)
                }
                Label(health.statusLabel, systemImage: health.statusIcon)
                    .font(ATMFont.font(.footnote, weight: .medium))
                    .foregroundStyle(ATMTheme.collectionHealthColor(health.status))
                Spacer(minLength: 8)
                Text(checkedAtLabel)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            if let note = health.transientNote {
                Text(note)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            } else if let error = health.error, !error.isEmpty {
                Text(error)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                    .padding(.leading, showsName ? 100 : 0)
            }
        }
    }

    private var checkedAtLabel: String {
        guard let checkedAt = health.checkedAt else { return "等待首次收集" }
        return "检测于 \(collectionRelativeTime(checkedAt))"
    }
}

private struct CollectionItemRow: View {
    let item: ATMCollectionItem
    let supplementCount: Int
    let unreadCount: Int
    let isSelected: Bool
    var sourceName: String? = nil

    private var itemType: ATMCollectionItemType {
        ATMCollectionItemType.resolve(item.itemType)
    }

    private var retryStopped: Bool { item.retryStopped == true }

    var body: some View {
        ATMNavigatorRow(isSelected: isSelected) {
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
        } content: {
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                Text(item.title?.isEmpty == false
                    ? item.title!
                    : collectionActionTitle(
                        item.action,
                        retryStopped: retryStopped,
                        saved: item.knowledgeDocumentID?.isEmpty == false
                    ))
                    .font(ATMFont.font(.body, weight: unreadCount > 0 ? .semibold : .medium))
                    .lineLimit(2)
                HStack(spacing: 5) {
                    if let sourceName {
                        Text(sourceName)
                        Text("·")
                    }
                    Text(itemType.title)
                    Text("·")
                    Text(collectionActionTitle(
                        item.action,
                        retryStopped: retryStopped,
                        saved: item.knowledgeDocumentID?.isEmpty == false
                    ))
                    if supplementCount > 0 {
                        Text("·")
                        Text("补充 \(supplementCount)")
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
        } trailing: {
            if unreadCount > 0 {
                Circle()
                    .fill(ATMTheme.accent)
                    .frame(width: 7, height: 7)
                    .padding(.top, 7)
                    .accessibilityLabel("未读")
            }
        }
    }
}

/// 一条处理记录要说清四件事：消息从哪儿来、ATM 为什么这样处理、结论是什么、关联到
/// 哪个 Todo。这四件都在「处理详情」里；消息原文按条数可以长到几十行，单独一个 tab，
/// 免得它把四件事顶出屏幕。分页用 `ATMCapsuleTabs`，与任务 / Agent 详情一致。
private struct CollectionItemDetail: View {
    private enum DetailTab: String, CaseIterable {
        case decision
        case transcript
    }

    @ObservedObject var store: ATMDataStore
    let item: ATMCollectionItem
    let source: ATMCollectionSource?
    let supplements: [ATMCollectionItem]
    let openTodo: () -> Void
    let openKnowledge: (String, String) -> Void

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
        ATMDetailScaffold {
            header
        } tabs: {
            ATMCapsuleTabs(
                selection: $selectedTab,
                items: [
                    (.decision, "处理详情"),
                    (.transcript, transcriptTabTitle),
                ]
            )
        } content: {
            content
        }
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
            supplements.isEmpty ? "删除这条处理记录？" : "删除主记录及 \(supplements.count) 条补充？",
            isPresented: $confirmingDelete,
            titleVisibility: .visible
        ) {
            Button("删除记录", role: .destructive) {
                store.deleteCollectionItems([item] + supplements)
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text(supplements.isEmpty
                ? collectionDeleteWarning(for: item)
                : "主记录和折叠在详情里的 \(supplements.count) 条补充会一起从本地删除，关联 Todo 保留。")
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
        VStack(alignment: .leading, spacing: 0) {
            switch selectedTab {
            case .decision:
                collectionDetailSection(showsDivider: true) { sourceSummary }
                collectionDetailSection(showsDivider: true) { decisionSummary }
                collectionDetailSection { outcomeSummary }
            case .transcript:
                rawContextSummary
                    .padding(.vertical, 20)
            }
        }
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    private var transcriptTabTitle: String {
        let count = transcript.messageCount
        return count == 0 ? "消息原文" : "消息原文 \(count)"
    }

    private var header: some View {
        ATMDetailHeader(title: item.title?.isEmpty == false ? item.title! : "未生成标题") {
            Label(
                collectionActionTitle(
                    item.action,
                    retryStopped: retryStopped,
                    saved: item.knowledgeDocumentID?.isEmpty == false
                ),
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
        } actions: {
            HStack(spacing: 8) {
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
                    if item.isArchived {
                        Button("重新打开") {
                            store.setCollectionItemsArchived([item] + supplements, archived: false)
                        }
                    } else if !item.shouldCollapseInCollection {
                        Button("了结记录") {
                            store.setCollectionItemsArchived([item] + supplements, archived: true)
                        }
                    }
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
        } meta: {
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
    /// Todo 写了什么，append 时是这次补充加了什么，insight 时是先留在这里、等待用户
    /// 明确保存的内容。
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
            if item.action == "insight" {
                if let documentID = item.knowledgeDocumentID, !documentID.isEmpty {
                    Button {
                        openKnowledge(item.knowledgeCollection ?? knowledgeDestination, documentID)
                    } label: {
                        Label("打开已保存知识", systemImage: "arrow.up.right.square")
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                } else {
                    Button {
                        store.saveCollectionItemToKnowledge(item)
                    } label: {
                        Label("保存到 \(knowledgeDestination)", systemImage: "tray.and.arrow.down")
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .disabled(store.isCollecting)
                }
            }
            if !supplements.isEmpty {
                Divider()
                    .padding(.vertical, 4)
                HStack(alignment: .firstTextBaseline) {
                    sectionTitle("补充内容")
                    Spacer()
                    Text("\(supplements.count) 条")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(supplements) { supplement in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 6) {
                                Text(collectionSupplementTime(supplement))
                                if let sender = supplement.sender, !sender.isEmpty {
                                    Text("·")
                                    Text(sender)
                                }
                            }
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.secondary)
                            Text(supplement.summary?.isEmpty == false
                                ? supplement.summary!
                                : supplement.title ?? "暂无补充摘要。")
                                .font(ATMFont.body)
                                .fixedSize(horizontal: false, vertical: true)
                                .textSelection(.enabled)
                        }
                    }
                }
            }
            if let todoID = item.todoID, !todoID.isEmpty {
                Text(todoRelationLine(todoID))
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var knowledgeDestination: String {
        let configured = source?.knowledgeCollection?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return configured.isEmpty ? "inbox" : configured
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

    private func collectionDetailSection<Content: View>(
        showsDivider: Bool = false,
        @ViewBuilder content: () -> Content
    ) -> some View {
        content()
            .padding(.vertical, 20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .bottom) {
                if showsDivider {
                    Divider()
                }
            }
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

enum CollectionSourceEditorLayout {
    static let sheetWidth: CGFloat = 560
    static let addSheetHeight: CGFloat = 560
    static let editSheetHeight: CGFloat = 520
    static let advancedTriggerMinimumHeight: CGFloat = 56
    static let choiceCardMinimumHeight: CGFloat = 70

    static func sheetHeight(isNewSource: Bool) -> CGFloat {
        isNewSource ? addSheetHeight : editSheetHeight
    }
}

enum CollectionIntervalUnit: Int, CaseIterable, Identifiable {
    case minute = 1
    case hour = 60
    case day = 1440

    var id: Int { rawValue }

    var label: String {
        switch self {
        case .minute: return "分钟"
        case .hour: return "小时"
        case .day: return "天"
        }
    }
}

struct CollectionIntervalInput {
    let text: String
    let unit: CollectionIntervalUnit

    var minutes: Int? {
        guard validationMessage == nil else { return nil }
        return Int(text.trimmingCharacters(in: .whitespacesAndNewlines)).map {
            $0 * unit.rawValue
        }
    }

    var validationMessage: String? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return "请输入采集频率" }
        guard trimmed.allSatisfy({ $0.isNumber }), let value = Int(trimmed), value > 0 else {
            return "采集频率必须是正整数"
        }
        guard value <= 1440 / unit.rawValue else {
            return "采集间隔不能超过 1 天"
        }
        return nil
    }

    static func displayValue(for minutes: Int) -> (text: String, unit: CollectionIntervalUnit) {
        if minutes.isMultiple(of: CollectionIntervalUnit.day.rawValue) {
            return (String(minutes / CollectionIntervalUnit.day.rawValue), .day)
        }
        if minutes.isMultiple(of: CollectionIntervalUnit.hour.rawValue) {
            return (String(minutes / CollectionIntervalUnit.hour.rawValue), .hour)
        }
        return (String(minutes), .minute)
    }
}

private struct CollectionIntervalPreset: Identifiable {
    let minutes: Int
    let label: String

    var id: Int { minutes }
}

private let collectionIntervalPresets = [
    CollectionIntervalPreset(minutes: 5, label: "每 5 分钟"),
    CollectionIntervalPreset(minutes: 15, label: "每 15 分钟"),
    CollectionIntervalPreset(minutes: 30, label: "每 30 分钟"),
    CollectionIntervalPreset(minutes: 60, label: "每小时"),
    CollectionIntervalPreset(minutes: 360, label: "每 6 小时"),
    CollectionIntervalPreset(minutes: 720, label: "每 12 小时"),
    CollectionIntervalPreset(minutes: 1440, label: "每天"),
]

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
    @State private var intervalValue = "5"
    @State private var intervalUnit = CollectionIntervalUnit.minute
    @State private var usesCustomInterval = false
    @State private var showsAdvancedSettings = false

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
        let interval = CollectionIntervalInput.displayValue(
            for: source?.effectiveIntervalMinutes ?? 5
        )
        _intervalValue = State(initialValue: interval.text)
        _intervalUnit = State(initialValue: interval.unit)
        _usesCustomInterval = State(
            initialValue: !collectionIntervalPresets.contains {
                $0.minutes == (source?.effectiveIntervalMinutes ?? 5)
            }
        )
        _showsAdvancedSettings = State(initialValue: source != nil)
    }

    var body: some View {
        Group {
            if presentation == .detail {
                editorContent
                    .frame(maxWidth: .infinity)
            } else {
                editorContent
                    .frame(
                        width: CollectionSourceEditorLayout.sheetWidth,
                        height: CollectionSourceEditorLayout.sheetHeight(
                            isNewSource: source == nil
                        )
                    )
            }
        }
        .background(presentation == .detail ? Color.clear : ATMTheme.canvas)
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
            if presentation == .detail {
                ATMDetailBodySurface { editorBody }
            } else {
                sheetEditorBody
            }
        }
    }

    /// The sheet has a bounded viewport: only its form scrolls, while the title
    /// and save controls stay reachable on smaller displays.
    private var sheetEditorBody: some View {
        VStack(spacing: 0) {
            ScrollView {
                editorForm
            }
            Divider()
            footer
        }
    }

    private var editorBody: some View {
        VStack(spacing: 0) {
            editorForm
            Divider()
            footer
        }
    }

    private var editorForm: some View {
        VStack(alignment: .leading, spacing: 0) {
            sourceSection
            formDivider
            collectionContentSection
            formDivider
            decisionSection
            formDivider
            frequencySection
            advancedSection
                .padding(.top, 20)
        }
        .padding(20)
        .frame(maxWidth: 760, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var sheetHeader: some View {
        HStack(spacing: 11) {
            Image(systemName: source == nil ? "tray" : "slider.horizontal.3")
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                .foregroundStyle(ATMTheme.accent)
                .frame(width: 34, height: 34)
                .background(ATMTheme.accentFill, in: RoundedRectangle(cornerRadius: 9))

            VStack(alignment: .leading, spacing: 3) {
                Text(source == nil ? "添加收集来源" : "编辑收集来源")
                    .font(ATMFont.font(.title3, weight: .semibold))
                Text(source == nil ? "配置来源和收集规则" : "调整来源和收集规则")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
        .background(presentation == .detail ? ATMTheme.elevated : ATMTheme.surface)
    }

    private var footer: some View {
        HStack(spacing: 10) {
            if let reason = saveBlockReason {
                Label(reason, systemImage: "exclamationmark.circle")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
            Button("取消", action: onClose)
                .keyboardShortcut(.cancelAction)
            Button(source == nil ? "添加来源" : "保存", action: save)
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(saveBlockReason != nil)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .background(ATMTheme.surface)
    }

    private var saveBlockReason: String? {
        if let reason = identity.blockReason { return reason }
        if let reason = intervalInput.validationMessage { return reason }
        return nil
    }

    private func save() {
        guard let target = identity.target, let intervalMinutes = intervalInput.minutes else {
            return
        }
        store.addCollectionSource(
            connector: identity.trimmedConnector, target: target, name: name,
            project: project, priority: priority,
            excludePattern: excludePattern, instruction: instruction,
            knowledgeCollection: knowledgeCollection, strategy: strategy,
            decisionUnit: decisionUnit,
            intervalMinutes: intervalMinutes,
            enabled: source?.enabled ?? true
        )
        onClose()
    }

    // MARK: - 来源

    @ViewBuilder
    private var sourceSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            sectionHeading(
                "来源",
                detail: identity.isEditing || identity.selection != nil ? "已选择" : nil
            )

            if identity.isEditing {
                lockedIdentityCard
            } else if let selected = identity.selection, !identity.manualEntry {
                selectedCandidateCard(selected)
            } else {
                VStack(alignment: .leading, spacing: 12) {
                    connectorField
                    if identity.manualEntry {
                        manualIdentifierFields
                    } else {
                        searchField
                        candidateList
                    }
                }
                .padding(14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 11))
                .overlay {
                    RoundedRectangle(cornerRadius: 11)
                        .stroke(ATMTheme.border, lineWidth: 1)
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
            HStack(alignment: .top, spacing: 9) {
                Image(systemName: "link.badge.plus")
                    .foregroundStyle(ATMTheme.warning)
                VStack(alignment: .leading, spacing: 3) {
                    Text("还没有配置连接器")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("请先在收集设置中配置连接器")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            .padding(11)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ATMTheme.warningFill, in: RoundedRectangle(cornerRadius: 8))
        } else {
            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 10) {
                    Text("连接器")
                        .font(ATMFont.font(.body, weight: .medium))
                    Picker("连接器", selection: $identity.connector) {
                        if identity.trimmedConnector.isEmpty {
                            Text("请选择连接器").tag("")
                        }
                        ForEach(connectorOptions, id: \.connector) { health in
                            Text(health.connector).tag(health.connector)
                        }
                    }
                    .labelsHidden()
                    .frame(maxWidth: 220)
                    .onChange(of: identity.connector) { _ in resetSearch() }
                    Spacer(minLength: 0)
                }

                if let health = selectedConnectorHealth, health.needsAttention {
                    connectorHealthLine(health)
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
            } else if let note = health.transientNote {
                // The rate, not the latest message: the rate is what says whether to
                // care, and this connector is already retrying.
                Text("· \(note)")
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
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
                Text(source?.displayName ?? "")
                    .font(ATMFont.font(.body, weight: .medium))
                Text("\(source?.connector ?? "") · \(collectionKindLabel(source?.kind))")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
            Spacer(minLength: 0)
            Image(systemName: "lock.fill")
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
        }
        .padding(11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 11))
        .overlay {
            RoundedRectangle(cornerRadius: 11)
                .stroke(ATMTheme.border, lineWidth: 1)
        }
    }

    private var searchField: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Picker("范围", selection: $searchKind) {
                    ForEach(ATMCollectionSearchKind.allCases) { kind in
                        Text(kind.label).tag(kind)
                    }
                }
                .labelsHidden()
                .frame(width: 92)
                .disabled(identity.trimmedConnector.isEmpty)
                .onChange(of: searchKind) { _ in
                    candidates = []
                    searchedKeyword = nil
                    searchError = nil
                }

                TextField("搜索群聊、联系人或机器人", text: $keyword)
                    .textFieldStyle(.roundedBorder)
                    .focused($keywordFocused)
                    .onSubmit(search)
                    .disabled(identity.trimmedConnector.isEmpty)
                if isSearching {
                    ProgressView().controlSize(.small)
                } else {
                    Button("搜索", action: search)
                        .disabled(!canSearch)
                }
            }

            if let searchError {
                Label(searchError, systemImage: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }

            Button("手动填写来源 ID") {
                identity.manualEntry = true
                identity.manualKind = searchKind == .all ? "group" : searchKind.rawValue
            }
            .buttonStyle(.link)
            .font(ATMFont.caption)
            .disabled(identity.trimmedConnector.isEmpty)
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
        .buttonStyle(.atmRow)
    }

    /// What the connector resolved, kept on screen after picking: the ID that
    /// gets saved is the part nobody can verify later from memory.
    private func selectedCandidateCard(_ candidate: ATMCollectionCandidate) -> some View {
        HStack(spacing: 10) {
            Image(systemName: candidate.symbolName)
                .foregroundStyle(ATMTheme.success)
                .frame(width: 30, height: 30)
                .background(ATMTheme.successFill, in: Circle())
            VStack(alignment: .leading, spacing: 2) {
                Text(candidate.name)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .lineLimit(1)
                Text("\(collectionKindLabel(candidate.kind)) · \(identity.trimmedConnector)")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
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
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 11))
        .overlay {
            RoundedRectangle(cornerRadius: 11)
                .stroke(ATMTheme.border, lineWidth: 1)
        }
        .help(candidate.externalID)
    }

    private var manualIdentifierFields: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .top, spacing: 14) {
                compactField("类型") {
                    Picker("来源类型", selection: $identity.manualKind) {
                        ForEach(manualKindOptions, id: \.self) { kind in
                            Text(collectionKindLabel(kind)).tag(kind)
                        }
                    }
                    .labelsHidden()
                }
                compactField("来源 ID") {
                    TextField("粘贴来源 ID", text: $identity.externalID)
                        .textFieldStyle(.roundedBorder)
                        .font(ATMFont.mono(.body))
                }
            }
            Button("返回搜索") {
                identity.manualEntry = false
                identity.externalID = ""
                keywordFocused = true
            }
            .buttonStyle(.link)
            .font(ATMFont.caption)
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

    // MARK: - 核心收集规则

    private var collectionContentSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            sectionHeading("收集内容")
            HStack(spacing: 10) {
                choiceCard(
                    title: "提取任务",
                    detail: "识别需求、缺陷和待办",
                    icon: "checklist",
                    isSelected: strategy == "tasks"
                ) {
                    selectStrategy("tasks")
                }
                choiceCard(
                    title: "收集结论",
                    detail: "沉淀决策、方案和共识",
                    icon: "books.vertical",
                    isSelected: strategy == "observe"
                ) {
                    selectStrategy("observe")
                }
            }
        }
    }

    private var decisionSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            sectionHeading("判断方式")
            HStack(spacing: 10) {
                choiceCard(
                    title: "按对话时段",
                    detail: "合并连续消息，适合群聊",
                    icon: "text.bubble",
                    isSelected: decisionUnit == "window"
                ) {
                    decisionUnit = "window"
                }
                choiceCard(
                    title: "按单条消息",
                    detail: "逐条判断，适合通知消息",
                    icon: "bell",
                    isSelected: decisionUnit == "message"
                ) {
                    decisionUnit = "message"
                }
            }
        }
    }

    private var frequencySection: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 18) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("检查频率")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("仅影响当前来源")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 12)
                Picker("检查频率", selection: intervalPresetSelection) {
                    ForEach(collectionIntervalPresets) { preset in
                        Text(preset.label).tag(preset.minutes)
                    }
                    Text("自定义…").tag(-1)
                }
                .labelsHidden()
                .frame(width: 160)
            }

            if usesCustomInterval {
                VStack(alignment: .trailing, spacing: 5) {
                    HStack(spacing: 8) {
                        Spacer()
                        TextField("数值", text: $intervalValue)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 72)
                        Picker("单位", selection: $intervalUnit) {
                            ForEach(CollectionIntervalUnit.allCases) { unit in
                                Text(unit.label).tag(unit)
                            }
                        }
                        .labelsHidden()
                        .frame(width: 90)
                    }
                    if let message = intervalInput.validationMessage {
                        Text(message)
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.danger)
                    }
                }
            }
        }
    }

    // MARK: - 高级设置

    private var advancedSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.easeInOut(duration: 0.16)) {
                    showsAdvancedSettings.toggle()
                }
            } label: {
                HStack(spacing: 11) {
                    Image(systemName: "slider.horizontal.3")
                        .foregroundStyle(ATMTheme.secondary)
                        .frame(width: 18)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("高级设置")
                            .font(ATMFont.font(.body, weight: .semibold))
                            .foregroundStyle(ATMTheme.primary)
                        Text("名称、筛选和结果归属")
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    Spacer(minLength: 12)
                    Image(systemName: "chevron.down")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                        .rotationEffect(.degrees(showsAdvancedSettings ? 180 : 0))
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 11)
                .frame(
                    maxWidth: .infinity,
                    minHeight: CollectionSourceEditorLayout.advancedTriggerMinimumHeight,
                    alignment: .leading
                )
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(showsAdvancedSettings ? "收起高级设置" : "展开高级设置")

            if showsAdvancedSettings {
                Divider()
                VStack(alignment: .leading, spacing: 16) {
                    advancedGroupTitle("名称与筛选")
                    inlineField("显示名称") {
                        TextField(namePlaceholder, text: $name)
                            .textFieldStyle(.roundedBorder)
                    }
                    inlineField("重点关注", alignment: .top) {
                        TextField("例如：MR、需求和线上问题", text: $instruction, axis: .vertical)
                            .textFieldStyle(.roundedBorder)
                            .lineLimit(1...3)
                    }
                    inlineField("排除关键词", alignment: .top) {
                        TextField("例如：闲聊, 打卡", text: $excludePattern, axis: .vertical)
                            .textFieldStyle(.roundedBorder)
                            .lineLimit(1...2)
                    }

                    Divider()

                    advancedGroupTitle("结果归属")
                    if strategy == "observe" {
                        inlineField("知识空间") {
                            TextField("inbox", text: $knowledgeCollection)
                                .textFieldStyle(.roundedBorder)
                        }
                    } else {
                        HStack(alignment: .top, spacing: 12) {
                            compactField("默认项目") {
                                TextField("可留空", text: $project)
                                    .textFieldStyle(.roundedBorder)
                            }
                            compactField("优先级") {
                                Picker("默认优先级", selection: $priority) {
                                    ForEach(["P0", "P1", "P2", "P3"], id: \.self) { value in
                                        Text(priorityLabel(value)).tag(value)
                                    }
                                }
                                .labelsHidden()
                            }
                            .frame(width: 170)
                        }
                    }
                }
                .padding(16)
            }
        }
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 12))
        .overlay {
            RoundedRectangle(cornerRadius: 12)
                .stroke(ATMTheme.border, lineWidth: 1)
        }
    }

    // MARK: - 通用外壳

    private var formDivider: some View {
        Divider()
            .padding(.vertical, 20)
    }

    private func sectionHeading(_ title: String, detail: String? = nil) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(title)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
            Spacer(minLength: 0)
            if let detail {
                Text(detail)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
    }

    private func choiceCard(
        title: String,
        detail: String,
        icon: String,
        isSelected: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: isSelected ? "checkmark.circle.fill" : icon)
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(isSelected ? ATMTheme.accent : ATMTheme.secondary)
                    .frame(width: 20)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title)
                        .font(ATMFont.font(.body, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                    Text(detail)
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
            }
            .padding(12)
            .frame(
                maxWidth: .infinity,
                minHeight: CollectionSourceEditorLayout.choiceCardMinimumHeight,
                alignment: .leading
            )
            .contentShape(RoundedRectangle(cornerRadius: 11))
            .background(
                isSelected ? ATMTheme.accentFill : ATMTheme.surface,
                in: RoundedRectangle(cornerRadius: 11)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 11)
                    .stroke(isSelected ? ATMTheme.accent : ATMTheme.border, lineWidth: isSelected ? 1.5 : 1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityValue(isSelected ? "已选择" : "")
    }

    private func advancedGroupTitle(_ title: String) -> some View {
        Text(title)
            .font(ATMFont.font(.body, weight: .semibold))
            .foregroundStyle(ATMTheme.primary)
    }

    private func inlineField<Content: View>(
        _ title: String,
        alignment: VerticalAlignment = .center,
        @ViewBuilder content: () -> Content
    ) -> some View {
        HStack(alignment: alignment, spacing: 12) {
            Text(title)
                .font(ATMFont.font(.body, weight: .medium))
                .frame(width: 88, alignment: .leading)
                .padding(.top, alignment == .top ? 5 : 0)
            content()
                .frame(maxWidth: .infinity)
        }
    }

    private func compactField<Content: View>(
        _ title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(ATMFont.font(.body, weight: .medium))
            content()
                .frame(maxWidth: .infinity)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var intervalInput: CollectionIntervalInput {
        CollectionIntervalInput(text: intervalValue, unit: intervalUnit)
    }

    private var intervalPresetSelection: Binding<Int> {
        Binding(
            get: {
                if usesCustomInterval { return -1 }
                guard let minutes = intervalInput.minutes,
                      collectionIntervalPresets.contains(where: { $0.minutes == minutes })
                else { return -1 }
                return minutes
            },
            set: { minutes in
                if minutes < 0 {
                    usesCustomInterval = true
                    return
                }
                usesCustomInterval = false
                setIntervalMinutes(minutes)
            }
        )
    }

    private func setIntervalMinutes(_ minutes: Int) {
        let display = CollectionIntervalInput.displayValue(for: minutes)
        intervalValue = display.text
        intervalUnit = display.unit
        usesCustomInterval = !collectionIntervalPresets.contains { $0.minutes == minutes }
    }

    private func selectStrategy(_ value: String) {
        guard strategy != value else { return }
        let previousDefault = value == "observe" ? 5 : 60
        strategy = value
        if intervalInput.minutes == previousDefault {
            setIntervalMinutes(value == "observe" ? 60 : 5)
        }
    }

    private func priorityLabel(_ value: String) -> String {
        ATMTodoPriorityStyle.label(value)
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
    if item.knowledgeDocumentID?.isEmpty == false {
        return "记录会从本地删除，已经保存的知识保留。刚收集到的记录可能在下一轮重新出现。"
    }
    if let todoID = item.todoID, !todoID.isEmpty {
        return "记录会从本地删除，\(todoID) 保留。如果是这次判断错了，请先用「撤销自动处理」。"
    }
    return "记录会从本地删除。刚收集到的记录可能在下一轮重新出现，更早的删掉就不会再回来。"
}

private func collectionActionTitle(
    _ action: String,
    retryStopped: Bool = false,
    saved: Bool = false
) -> String {
    switch action {
    case "create": return "已创建"
    case "append": return "已补充"
    case "insight": return saved ? "已保存" : "待保存"
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

private func collectionSupplementTime(_ item: ATMCollectionItem) -> String {
    let timestamp = item.occurredAt ?? item.createdAt
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "zh_CN")
    formatter.timeZone = .current
    formatter.dateFormat = "MM-dd HH:mm"
    return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(timestamp)))
}

private func collectionNextRunText(_ overview: ATMCollectionOverview) -> String {
    guard let latest = overview.latestRun else { return "即将运行" }
    let next = Int(latest.startedAt) + max(overview.intervalMinutes, 1) * 60
    let remaining = next - Int(Date().timeIntervalSince1970)
    if remaining <= 0 { return "即将运行" }
    if remaining < 60 { return "下次 <1 分钟" }
    return "下次 \((remaining + 59) / 60) 分钟"
}
