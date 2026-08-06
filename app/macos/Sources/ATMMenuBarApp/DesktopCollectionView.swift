import SwiftUI

private struct CollectionHistoryRequest: Identifiable {
    let source: ATMCollectionSource
    let item: ATMCollectionItem?

    var id: String { item?.id ?? "source:\(source.id)" }
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
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var showingAddSource = false
    @State private var editingSource: ATMCollectionSource?
    @State private var deleteCandidate: ATMCollectionSource?
    @State private var itemDeletion: CollectionItemDeletion?
    @State private var historyRequest: CollectionHistoryRequest?
    @State private var showingIgnoredItems = false
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
        HSplitView {
            VStack(spacing: 0) {
                header
                itemColumn
            }
            .frame(minWidth: 260, idealWidth: 330, maxWidth: 420)

            detailColumn
                .frame(minWidth: 420, maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(ATMTheme.canvas)
        .onAppear {
            store.refreshCollection()
            selectDefaultItem()
            revealSelectedSourceGroup()
        }
        .onChange(of: store.collectionOverview.items.map(\.id)) { _ in selectDefaultItem() }
        .onChange(of: showingIgnoredItems) { _ in selectDefaultItem() }
        .onChange(of: navigation.selectedCollectionItemID) { _ in revealSelectedSourceGroup() }
        .sheet(isPresented: $showingAddSource) {
            AddCollectionSourceSheet(store: store) { showingAddSource = false }
        }
        .sheet(item: $editingSource) { source in
            AddCollectionSourceSheet(store: store, source: source) { editingSource = nil }
        }
        .sheet(item: $historyRequest) { request in
            CollectionHistorySheet(store: store, source: request.source, focusedItem: request.item) {
                historyRequest = nil
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

    private var header: some View {
        VStack(alignment: .leading, spacing: 0) {
            ATMDrawerHeader(title: "收集", count: primaryItems.count) {
                HStack(spacing: 8) {
                    Circle()
                        .fill(collectionHealthColor)
                        .frame(width: 8, height: 8)
                        .help("\(collectionHealthText) · \(collectionSummaryHelp)")

                    sourceManagementMenu

                    Button {
                        store.runCollectionNow()
                    } label: {
                        Label("收集", systemImage: store.isCollecting ? "hourglass" : "arrow.clockwise")
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .help(store.isCollecting ? "正在收集" : "立即收集")
                    .disabled(store.isCollecting || store.collectionOverview.summary.enabledSources == 0)
                }
            }

            if let error = store.collectionErrorMessage, !error.isEmpty {
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(ATMTheme.warning)
                    Text(error)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .textSelection(.enabled)
                    Spacer()
                }
                .padding(.horizontal, 14)
                .padding(.bottom, 8)
            }
        }
    }

    private var sourceManagementMenu: some View {
        Menu {
            Button {
                store.setCollectionEnabled(!store.collectionOverview.enabled)
            } label: {
                Label(
                    store.collectionOverview.enabled ? "关闭自动收集" : "开启自动收集",
                    systemImage: store.collectionOverview.enabled ? "pause.circle" : "play.circle"
                )
            }

            Divider()

            Button {
                showingAddSource = true
            } label: {
                Label("添加来源", systemImage: "plus")
            }

            if !store.collectionOverview.sources.isEmpty {
                Divider()
                ForEach(store.collectionOverview.sources) { source in
                    Menu(source.displayName) {
                        Button("查看聊天记录") {
                            historyRequest = CollectionHistoryRequest(source: source, item: nil)
                        }
                        Button("编辑") { editingSource = source }
                        Button(source.enabled ? "暂停" : "启用") {
                            store.setCollectionSource(source, enabled: !source.enabled)
                        }
                        Divider()
                        Button("删除", role: .destructive) { deleteCandidate = source }
                    }
                }
            }
        } label: {
            Label("来源", systemImage: "tray.2")
                .font(ATMFont.font(.footnote, weight: .medium))
                .padding(.horizontal, 8)
                .frame(height: 24)
                .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("添加或管理收集来源")
    }

    private var collectionHealthText: String {
        if store.isCollecting { return "正在收集" }
        if store.collectionErrorMessage?.isEmpty == false { return "运行异常" }
        if !store.collectionOverview.enabled { return "自动收集已关闭" }
        if let latest = store.collectionOverview.latestSuccessfulRun {
            return "运行正常 · \(collectionRelativeTime(latest.startedAt))"
        }
        return "等待首次运行"
    }

    private var collectionHealthColor: Color {
        if store.collectionErrorMessage?.isEmpty == false { return ATMTheme.warning }
        if !store.collectionOverview.enabled { return ATMTheme.secondary }
        if store.collectionOverview.latestSuccessfulRun != nil || store.isCollecting { return ATMTheme.success }
        return ATMTheme.secondary
    }

    private var collectionSummaryHelp: String {
        let summary = store.collectionOverview.summary
        var parts = [
            "今日新建 \(summary.createdToday) · 补充 \(summary.appendedToday) · 沉淀 \(summary.insightToday)",
            "读取 \(summary.fetchedToday) 条"
        ]
        if let digest = todaysDigest {
            parts.append("已写入知识库 \(digest)")
        } else if summary.ignoredToday > 0 {
            parts.append("另有 \(summary.ignoredToday) 条无需处理")
        }
        if let stopped = summary.retryStopped, stopped > 0 {
            parts.append("\(stopped) 条已停止自动重试")
        }
        return parts.joined(separator: " · ")
    }

    private static let digestDayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = .current
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    /// Today's digest collections, named so the count above has somewhere to lead.
    private var todaysDigest: String? {
        let today = Self.digestDayFormatter.string(from: Date())
        let collections = store.collectionOverview.digests
            .filter { $0.digestDate == today }
            .compactMap { $0.collection?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        guard !collections.isEmpty else { return nil }
        return Array(Set(collections)).sorted().joined(separator: "、")
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
        // 中栏 surface / 右栏 canvas —— 和任务、Agent、知识一致。此前中栏也是 canvas，
        // 与详情栏同色，三栏之间只剩一根 Divider。
        .background(ATMTheme.listPane)
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
                withAnimation(.easeInOut(duration: 0.15)) {
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
                    historyRequest = CollectionHistoryRequest(source: source, item: nil)
                }
                Button("编辑") { editingSource = source }
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
                withAnimation(.easeInOut(duration: 0.15)) {
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
        .listRowInsets(EdgeInsets(top: 2, leading: 8, bottom: 2, trailing: 8))
        .listRowBackground(Color.clear)
        // 右键只放导航和删除。重新处理、修正、撤销这些要看记录状态才知道能不能做，
        // 判定在详情栏（见 CollectionItemDetail），在这儿抄一遍就是抄两套规则。
        .atmRightClickMenu {
            if let source = source(for: item) {
                ATMMenuItem("查看聊天记录") {
                    historyRequest = CollectionHistoryRequest(source: source, item: item)
                }
            }
            if item.todoID != nil {
                ATMMenuItem("打开 Todo") { openTodo(item) }
            }
            ATMMenuSeparator()
            ATMMenuItem("删除记录", destructive: true) {
                itemDeletion = CollectionItemDeletion(items: [item])
            }
        }
    }

    @ViewBuilder
    private var detailColumn: some View {
        if let item = selectedItem {
            CollectionItemDetail(
                store: store,
                item: item,
                source: source(for: item),
                openHistory: {
                    guard let source = source(for: item) else { return }
                    historyRequest = CollectionHistoryRequest(source: source, item: item)
                }
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
        navigation.section = .tasks
        navigation.selectedTodoID = todoID
    }

    private func source(for item: ATMCollectionItem) -> ATMCollectionSource? {
        store.collectionOverview.sources.first { $0.id == item.sourceID }
    }

    private func shouldCollapse(_ item: ATMCollectionItem) -> Bool {
        item.shouldCollapseInCollection
    }

    private func selectDefaultItem() {
        guard !displayedItems.contains(where: { $0.id == navigation.selectedCollectionItemID }) else { return }
        navigation.selectedCollectionItemID = displayedItems.first?.id
    }

    private func revealSelectedSourceGroup() {
        guard let item = selectedItem, !shouldCollapse(item) else { return }
        let groupID = source(for: item)?.id ?? "__unknown__"
        var set = collapsedSourceGroups
        guard set.remove(groupID) != nil else { return }
        collapsedSourceGroupsRaw = set.sorted().joined(separator: ",")
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
        HStack(alignment: .top, spacing: 9) {
            Image(systemName: collectionActionIcon(item.action, retryStopped: retryStopped))
                .foregroundStyle(collectionActionColor(item.action, retryStopped: retryStopped))
                .frame(width: 24, height: 24)
                .background(
                    collectionActionColor(item.action, retryStopped: retryStopped).opacity(0.10),
                    in: Circle()
                )
            VStack(alignment: .leading, spacing: 4) {
                Text(item.title?.isEmpty == false
                    ? item.title!
                    : collectionActionTitle(item.action, retryStopped: retryStopped))
                    .font(ATMFont.font(.body, weight: .medium))
                    .lineLimit(2)
                HStack(spacing: 5) {
                    Text(itemType.title)
                    Text("·")
                    Text(collectionActionTitle(item.action, retryStopped: retryStopped))
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

private struct CollectionItemDetail: View {
    @ObservedObject var store: ATMDataStore
    let item: ATMCollectionItem
    let source: ATMCollectionSource?
    let openHistory: () -> Void
    let openTodo: () -> Void

    @State private var showingCorrection = false
    @State private var confirmingRevert = false
    @State private var confirmingDelete = false
    @State private var showingTechnicalDetails = false

    private var itemType: ATMCollectionItemType {
        ATMCollectionItemType.resolve(item.itemType)
    }

    private var retryStopped: Bool { item.retryStopped == true }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 6) {
                        Label(collectionActionBadgeTitle, systemImage: collectionActionIcon(item.action, retryStopped: retryStopped))
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

                detailDivider
                decisionSummary

                detailDivider
                classificationSummary

                detailDivider
                sourceSummary

                detailDivider
                DisclosureGroup("运行与原始上下文", isExpanded: $showingTechnicalDetails) {
                    VStack(alignment: .leading, spacing: 12) {
                        if let run = store.collectionOverview.runs.first(where: { $0.sourceID == item.sourceID }) {
                            detailCard("最近运行") {
                                detailLine("状态", run.status)
                                detailLine("时间", collectionRelativeTime(run.startedAt))
                                detailLine("读取", "\(run.fetchedCount) 条")
                                detailLine("处理", "新增 \(run.createdCount) · 补充 \(run.appendedCount) · 沉淀 \(run.insightCount) · 无需处理 \(run.ignoredCount)")
                            }
                            Divider()
                        }

                        detailCard("判断信息") {
                            detailLine("原始类型", item.itemType ?? "未分类")
                            detailLine(
                                "置信度",
                                item.confidence.map { String(format: "%.0f%%", $0 * 100) } ?? "—"
                            )
                        }

                        Divider()

                        detailCard("原始聊天上下文") {
                            Text(item.rawContext ?? "无上下文")
                                .font(ATMFont.mono(.footnote))
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .fixedSize(horizontal: false, vertical: true)
                                .textSelection(.enabled)
                        }
                    }
                    .padding(.top, 10)
                }
                .font(ATMFont.font(.body, weight: .medium))
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 20)
            .frame(maxWidth: 880, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
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
            "删除这条处理记录？",
            isPresented: $confirmingDelete,
            titleVisibility: .visible
        ) {
            Button("删除记录", role: .destructive) { store.deleteCollectionItem(item) }
            Button("取消", role: .cancel) {}
        } message: {
            Text(collectionDeleteWarning(for: item))
        }
    }

    /// 修正和撤销都改写这条记录写出去的 Todo，一旦那个 Todo 已经完成或废弃就没有意义了：
    /// 撤销的语义是「这次采集判断错了，把它建的任务废掉」，而不是把做完的事重新废一遍。
    /// 已了结的记录仍可「打开 Todo」，从任务侧自行处理。
    private var canAmendTodoWrite: Bool {
        (item.action == "create" || item.action == "append") && !item.todoClosed
    }

    private var collectionActionBadgeTitle: String {
        let actionTitle = collectionActionTitle(item.action, retryStopped: retryStopped)
        guard let todoID = item.todoID, !todoID.isEmpty else { return actionTitle }
        var title = "\(actionTitle)到 \(todoID)"
        if let status = item.todoStatus, !status.isEmpty {
            title += " · " + ATMTodoStatusStyle.label(forStatus: status)
        }
        // Archived Todos have left the working set, so 打开 Todo lands on a task the
        // 任务 workspace no longer lists. Saying so beats looking broken.
        if item.todoArchived == true {
            title += " · 已归档"
        }
        return title
    }

    private var decisionSummary: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("为什么这样处理")
                .font(ATMFont.font(.body, weight: .semibold))
            Text(item.reason?.isEmpty == false ? item.reason! : "暂无判断说明。")
                .font(ATMFont.body)
                .foregroundStyle(ATMTheme.secondary)
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
                        ? "已连续失败 \(item.attempts ?? 0) 次，自动重试已停止。修掉原因后用「立即重试」。"
                        : "下一轮收集会自动重试，通常不用管。")
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var classificationSummary: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 8) {
                Label(itemType.title, systemImage: itemType.systemImage)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.accent)
                Spacer()
                if let project = item.project, !project.isEmpty {
                    metadataLabel(project, systemImage: "folder")
                }
                if let priority = item.priority, !priority.isEmpty {
                    metadataLabel(priority, systemImage: "flag")
                }
            }
            Text(itemType.explanation)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func metadataLabel(_ text: String, systemImage: String) -> some View {
        Label(text, systemImage: systemImage)
            .font(ATMFont.caption)
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)
    }

    private var sourceSummary: some View {
        HStack(spacing: 10) {
            Image(systemName: collectionKindSymbol(source?.kind))
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 28, height: 28)
                .background(ATMTheme.controlFill, in: Circle())
            VStack(alignment: .leading, spacing: 2) {
                Text(source?.displayName ?? item.sourceID)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .lineLimit(1)
                HStack(spacing: 5) {
                    Text(item.connector)
                    if let sender = item.sender, !sender.isEmpty {
                        Text("· \(sender)")
                            .lineLimit(1)
                    }
                    Text("· \(item.messageIDs.count) 条消息")
                }
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
            if source != nil {
                // The context here is only the few lines around this item;
                // the full conversation lives in the connector-backed archive.
                Button("查看聊天记录", action: openHistory)
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
                    .fixedSize()
                    .layoutPriority(1)
            }
        }
        .padding(.vertical, 2)
    }

    private func detailCard<Content: View>(
        _ title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .font(ATMFont.font(.body, weight: .semibold))
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var detailDivider: some View {
        Divider()
            .padding(.vertical, 20)
    }

    private func detailLine(_ label: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label)
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 64, alignment: .leading)
            Text(value)
                .textSelection(.enabled)
            Spacer()
        }
        .font(ATMFont.body)
    }
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

private struct AddCollectionSourceSheet: View {
    @ObservedObject var store: ATMDataStore
    let source: ATMCollectionSource?
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
        onClose: @escaping () -> Void
    ) {
        self.store = store
        self.source = source
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
    }

    var body: some View {
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
            }

            Divider()

            footer
        }
        .background(ATMTheme.canvas)
        // Editing has no connector picker, no search and no candidate list, so it
        // needs materially less room than adding does.
        .frame(width: 620, height: source == nil ? 700 : 640)
        .onAppear {
            // Opening on the one field that always needs typing; when the
            // connector is still unchosen the picker is one Tab away.
            keywordFocused = source == nil && !identity.trimmedConnector.isEmpty
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
            if let reason = identity.blockReason {
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
                .disabled(identity.blockReason != nil)
        }
        .padding(.horizontal, 22)
        .padding(.vertical, 14)
        .background(ATMTheme.surface)
    }

    private func save() {
        guard let target = identity.target else { return }
        store.addCollectionSource(
            connector: identity.trimmedConnector, target: target, name: name,
            project: project, priority: priority,
            excludePattern: excludePattern, instruction: instruction,
            knowledgeCollection: knowledgeCollection, strategy: strategy,
            decisionUnit: decisionUnit,
            intervalMinutes: intervalMinutes, enabled: source?.enabled ?? true
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
    let focusedItem: ATMCollectionItem?
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
                    Text(subtitle)
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

            if let focusedItem {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text("本次处理记录")
                            .font(ATMFont.font(.body, weight: .semibold))
                        Spacer()
                        Text("\(focusedItem.messageIDs.count) 条新消息")
                            .font(ATMFont.mono(.caption))
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    ScrollView {
                        Text(focusedItem.rawContext?.isEmpty == false
                            ? focusedItem.rawContext!
                            : "未保存原始聊天上下文")
                            .font(ATMFont.mono(.footnote))
                            .fixedSize(horizontal: false, vertical: true)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(maxHeight: 210)
                }
                .padding(12)
                .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 10))

                HStack(alignment: .firstTextBaseline) {
                    Text("最近消息")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Spacer()
                    Text(recentSubtitle)
                        .font(ATMFont.caption)
                        .foregroundStyle(isStale ? .orange : ATMTheme.secondary)
                }
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
            .frame(height: focusedItem == nil ? 380 : 240)

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

    private var subtitle: String {
        if let focusedItem {
            return "优先显示当时参与判断的上下文 · \(focusedItem.messageIDs.count) 条新消息"
        }
        return recentSubtitle
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
    case "create": return "plus.circle.fill"
    case "append": return "arrow.triangle.branch"
    case "insight": return "book.closed.fill"
    case "ignore": return "minus.circle"
    case "failed": return retryStopped ? "exclamationmark.triangle.fill" : "arrow.clockwise"
    case "reverted": return "arrow.uturn.backward.circle.fill"
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
