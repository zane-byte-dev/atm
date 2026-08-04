import SwiftUI

private struct CollectionHistoryRequest: Identifiable {
    let source: ATMCollectionSource
    let item: ATMCollectionItem?

    var id: String { item?.id ?? "source:\(source.id)" }
}

struct DesktopCollectionView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var showingAddSource = false
    @State private var editingSource: ATMCollectionSource?
    @State private var deleteCandidate: ATMCollectionSource?
    @State private var historyRequest: CollectionHistoryRequest?
    @State private var showingIgnoredItems = false

    private var filteredItems: [ATMCollectionItem] {
        guard let sourceID = navigation.selectedCollectionSourceID else {
            return store.collectionOverview.items
        }
        return store.collectionOverview.items.filter { $0.sourceID == sourceID }
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
        VStack(spacing: 0) {
            header
            Divider()
            dailySummary
            Divider()
            HStack(spacing: 0) {
                sourceColumn
                    .frame(width: 190)
                Divider()
                itemColumn
                    .frame(width: 330)
                Divider()
                detailColumn
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(ATMTheme.canvas)
        .onAppear {
            store.refreshCollection()
            selectDefaultItem()
        }
        .onChange(of: navigation.selectedCollectionSourceID) { _ in
            showingIgnoredItems = false
            selectDefaultItem()
        }
        .onChange(of: store.collectionOverview.items.map(\.id)) { _ in selectDefaultItem() }
        .onChange(of: showingIgnoredItems) { _ in selectDefaultItem() }
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
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("收集")
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text("把外部消息转成可追溯任务")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                collectionHealthPill
                Toggle(
                    "自动收集",
                    isOn: Binding(
                        get: { store.collectionOverview.enabled },
                        set: { store.setCollectionEnabled($0) }
                    )
                )
                .toggleStyle(.switch)
                .controlSize(.small)
                .help("每 \(store.collectionOverview.intervalMinutes) 分钟自动收集")
                Button {
                    store.runCollectionNow()
                } label: {
                    Label(store.isCollecting ? "收集中" : "立即收集", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .disabled(store.isCollecting || store.collectionOverview.summary.enabledSources == 0)
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
                .padding(9)
                .background(ATMTheme.warningFill, in: RoundedRectangle(cornerRadius: 8))
            }
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 16)
    }

    private var collectionHealthPill: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(collectionHealthColor)
                .frame(width: 7, height: 7)
            Text(collectionHealthText)
                .lineLimit(1)
        }
        .font(ATMFont.footnote)
        .foregroundStyle(collectionHealthColor)
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(collectionHealthColor.opacity(0.10), in: Capsule())
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

    private var dailySummary: some View {
        let summary = store.collectionOverview.summary
        return HStack(spacing: 12) {
            Text("今日")
                .font(ATMFont.font(.footnote, weight: .semibold))
            // 与 collectionActionColor 一一对应，同一个动作在汇总和记录行里同色。
            summaryMetric("新建", value: summary.createdToday, color: collectionActionColor("create"))
            summaryMetric("补充", value: summary.appendedToday, color: collectionActionColor("append"))
            summaryMetric("沉淀", value: summary.insightToday, color: collectionActionColor("insight"))
            // 这里不再放失败数。三重机制保证失败会自愈：游标不前移（collector 的
            // `checkpoint was not advanced`）、失败消息不进 HandledCollectionMessageIDs、
            // 失败 item 不在跳过分支里——下一轮连着原始上下文重新分析，人不用做任何事。
            // 而且这个数是按 run 累加的：同一条失败 47 次会显示成「47」，读起来像 47 条待处理。
            Spacer()
            if let digest = todaysDigest {
                // The digest is the readable form of everything filed as an
                // insight; without naming it, "沉淀 12" has nowhere to lead.
                Text("已写入知识库 \(digest)")
                    .help("atm collect digest 按来源每天写一篇，当天内容变多就原地重写")
            } else if summary.ignoredToday > 0 {
                Text("另有 \(summary.ignoredToday) 条无需处理")
            }
            Text("读取 \(summary.fetchedToday) 条")
                .help("拉取结果可能包含时间窗口重叠的消息")
        }
        .font(ATMFont.footnote)
        .foregroundStyle(ATMTheme.secondary)
        .padding(.horizontal, 24)
        .frame(height: 42)
        .background(ATMTheme.controlFill.opacity(0.35))
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

    private func summaryMetric(_ title: String, value: Int, color: Color) -> some View {
        HStack(spacing: 4) {
            Circle().fill(color).frame(width: 6, height: 6)
            Text("\(title) \(value)")
                .lineLimit(1)
        }
        .foregroundStyle(value > 0 ? color : ATMTheme.secondary)
    }

    private var sourceColumn: some View {
        VStack(spacing: 0) {
            HStack {
                Text("来源")
                    .font(ATMFont.font(.body, weight: .semibold))
                Spacer()
                Button { showingAddSource = true } label: {
                    Image(systemName: "plus")
                }
                .buttonStyle(.plain)
                .help("添加钉钉来源")
            }
            .padding(.horizontal, 14)
            .frame(height: 44)
            Divider()

            List {
                sourceRow(
                    id: nil,
                    name: "全部来源",
                    icon: "tray.full",
                    enabled: true,
                    count: store.collectionOverview.items.filter { !shouldCollapse($0) }.count
                )
                ForEach(store.collectionOverview.sources) { source in
                    sourceRow(
                        id: source.id,
                        name: source.displayName,
                        icon: source.kind == "group" ? "person.3.fill" : "person.fill",
                        enabled: source.enabled,
                        count: store.collectionOverview.items.filter {
                            $0.sourceID == source.id && !$0.shouldCollapseInCollection
                        }.count
                    )
                    .contextMenu {
                        Button("查看聊天记录") {
                            historyRequest = CollectionHistoryRequest(source: source, item: nil)
                        }
                        Button("编辑") { editingSource = source }
                        Button(source.enabled ? "暂停" : "启用") {
                            store.setCollectionSource(source, enabled: !source.enabled)
                        }
                        Button("删除", role: .destructive) { deleteCandidate = source }
                    }
                }
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
        }
        .background(ATMTheme.sidebar)
    }

    private func sourceRow(
        id: String?,
        name: String,
        icon: String,
        enabled: Bool,
        count: Int
    ) -> some View {
        let selected = navigation.selectedCollectionSourceID == id
        return Button {
            navigation.selectedCollectionSourceID = id
        } label: {
            HStack(spacing: 8) {
                Image(systemName: icon)
                    .frame(width: 18)
                    .foregroundStyle(enabled ? ATMTheme.primary : ATMTheme.secondary)
                Text(name)
                    .lineLimit(1)
                Spacer()
                if !enabled {
                    Image(systemName: "pause.circle")
                        .foregroundStyle(ATMTheme.secondary)
                }
                Text(String(count))
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
            }
            .font(ATMFont.font(.body, weight: selected ? .semibold : .medium))
            .foregroundStyle(selected ? ATMTheme.accent : ATMTheme.primary)
            .atmRowSurface(.navigation, isSelected: selected)
        }
        .buttonStyle(.plain)
        .focusable(false)
        .listRowInsets(EdgeInsets(top: 1, leading: 8, bottom: 1, trailing: 8))
        .listRowBackground(Color.clear)
    }

    private var itemColumn: some View {
        VStack(spacing: 0) {
            HStack {
                Text("处理记录")
                    .font(ATMFont.font(.body, weight: .semibold))
                Spacer()
                Text(String(primaryItems.count))
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
            }
            .padding(.horizontal, 14)
            .frame(height: 44)
            Divider()

            if filteredItems.isEmpty {
                CollectionEmptyState(
                    title: "暂无处理记录",
                    systemImage: "tray",
                    detail: "添加来源后，ATM 会在后台自动收集。"
                )
            } else {
                List {
                    ForEach(primaryItems) { item in
                        itemRow(item)
                    }
                    if !ignoredItems.isEmpty {
                        Section {
                            Button {
                                showingIgnoredItems.toggle()
                            } label: {
                                HStack(spacing: 7) {
                                    Image(systemName: showingIgnoredItems ? "chevron.down" : "chevron.right")
                                    Text("沉淀与无需处理")
                                    Spacer()
                                    Text(String(ignoredItems.count))
                                        .font(ATMFont.mono(.caption))
                                }
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)

                            if showingIgnoredItems {
                                ForEach(ignoredItems) { item in
                                    itemRow(item).opacity(0.78)
                                }
                            }
                        }
                    }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
            }
        }
        // 中栏 surface / 右栏 canvas —— 和任务、Agent、知识一致。此前中栏也是 canvas，
        // 与详情栏同色，三栏之间只剩一根 Divider。
        .background(ATMTheme.surface)
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
                guard let todoID = item.todoID else { return }
                navigation.section = .tasks
                navigation.selectedTodoID = todoID
            }
        } else {
            CollectionEmptyState(
                title: "选择一条处理记录",
                systemImage: "doc.text.magnifyingglass"
            )
        }
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

    var body: some View {
        HStack(alignment: .top, spacing: 9) {
            Image(systemName: collectionActionIcon(item.action))
                .foregroundStyle(collectionActionColor(item.action))
                .frame(width: 24, height: 24)
                .background(
                    collectionActionColor(item.action).opacity(0.10),
                    in: Circle()
                )
            VStack(alignment: .leading, spacing: 4) {
                Text(item.title?.isEmpty == false
                    ? item.title!
                    : collectionActionTitle(item.action))
                    .font(ATMFont.font(.body, weight: .medium))
                    .lineLimit(2)
                HStack(spacing: 5) {
                    Text(itemType.title)
                    Text("·")
                    Text(collectionActionTitle(item.action))
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
    @State private var showingTechnicalDetails = false

    private var itemType: ATMCollectionItemType {
        ATMCollectionItemType.resolve(item.itemType)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 6) {
                        Label(collectionActionBadgeTitle, systemImage: collectionActionIcon(item.action))
                            .font(ATMFont.footnote)
                            .foregroundStyle(collectionActionColor(item.action))
                            .padding(.horizontal, 9)
                            .padding(.vertical, 5)
                            .background(
                                collectionActionColor(item.action).opacity(0.10),
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
                    // 所以留在主位。失败正相反：下一轮自己会重来，于是降级进菜单，只服务
                    // 「刚修好连接器、不想等下一轮」这种情况。
                    if item.action == "reverted" {
                        Button("重新处理") { store.reprocessCollectionItem(item) }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.small)
                    }
                    if item.action == "ignore" || item.action == "create"
                        || item.action == "append" || item.action == "failed" {
                        Menu {
                            if item.action == "ignore" {
                                Button("重新判断") { store.reprocessCollectionItem(item) }
                            }
                            if item.action == "failed" {
                                Button("立即重试") { store.reprocessCollectionItem(item) }
                            }
                            if item.action == "create" || item.action == "append" {
                                Button("修正标题、项目和优先级") { showingCorrection = true }
                                Button("撤销自动处理", role: .destructive) { confirmingRevert = true }
                            }
                        } label: {
                            Image(systemName: "ellipsis.circle")
                        }
                        .menuStyle(.borderlessButton)
                        .fixedSize()
                    }
                }

                decisionSummary

                classificationSummary

                sourceSummary

                DisclosureGroup("运行与原始上下文", isExpanded: $showingTechnicalDetails) {
                    VStack(alignment: .leading, spacing: 12) {
                        if let run = store.collectionOverview.runs.first(where: { $0.sourceID == item.sourceID }) {
                            detailCard("最近运行") {
                                detailLine("状态", run.status)
                                detailLine("时间", collectionRelativeTime(run.startedAt))
                                detailLine("读取", "\(run.fetchedCount) 条")
                                detailLine("处理", "新增 \(run.createdCount) · 补充 \(run.appendedCount) · 沉淀 \(run.insightCount) · 无需处理 \(run.ignoredCount)")
                            }
                        }

                        detailCard("判断信息") {
                            detailLine("原始类型", item.itemType ?? "未分类")
                            detailLine(
                                "置信度",
                                item.confidence.map { String(format: "%.0f%%", $0 * 100) } ?? "—"
                            )
                        }

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
            .padding(22)
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
    }

    private var collectionActionBadgeTitle: String {
        let actionTitle = collectionActionTitle(item.action)
        if let todoID = item.todoID, !todoID.isEmpty {
            return "\(actionTitle)到 \(todoID)"
        }
        return actionTitle
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
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .textSelection(.enabled)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 10))
        .overlay(alignment: .leading) {
            Capsule()
                .fill(collectionActionColor(item.action))
                .frame(width: 3)
                .padding(.vertical, 8)
        }
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
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill.opacity(0.5), in: RoundedRectangle(cornerRadius: 9))
    }

    private func metadataLabel(_ text: String, systemImage: String) -> some View {
        Label(text, systemImage: systemImage)
            .font(ATMFont.caption)
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)
    }

    private var sourceSummary: some View {
        HStack(spacing: 10) {
            Image(systemName: source?.kind == "group" ? "person.3.fill" : "person.fill")
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
            .layoutPriority(1)
            Spacer()
            if source != nil {
                // The context here is only the few lines around this item;
                // the full conversation lives in the connector-backed archive.
                Button("查看聊天记录", action: openHistory)
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
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
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill.opacity(0.65), in: RoundedRectangle(cornerRadius: 10))
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

    @State private var connector = ""
    @State private var kind = "channel"
    @State private var externalID = ""
    @State private var name = ""
    @State private var project = ""
    @State private var priority = "P2"
    @State private var excludePattern = ""
    @State private var instruction = ""
    @State private var knowledgeCollection = ""
    @State private var strategy = "tasks"
    @State private var intervalMinutes = 5

    @State private var keyword = ""
    @State private var candidates: [ATMCollectionCandidate] = []
    @State private var selection: ATMCollectionCandidate?
    @State private var searchedKeyword: String?
    @State private var isSearching = false
    @State private var searchError: String?
    @State private var manualEntry = false

    init(
        store: ATMDataStore,
        source: ATMCollectionSource? = nil,
        onClose: @escaping () -> Void
    ) {
        self.store = store
        self.source = source
        self.onClose = onClose
        _connector = State(initialValue: source?.connector ?? "")
        _kind = State(initialValue: source?.kind ?? "channel")
        _externalID = State(initialValue: source?.externalID ?? "")
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
        _intervalMinutes = State(initialValue: source?.effectiveIntervalMinutes ?? 5)
        // An existing source already has its identifier; only new ones search.
        _manualEntry = State(initialValue: source != nil)
    }

    var body: some View {
        VStack(spacing: 0) {
            sheetHeader

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    sourceSection

                    settingsSection(title: "基本信息", icon: "rectangle.and.pencil.and.ellipsis") {
                        HStack(alignment: .top, spacing: 14) {
                            formField("显示名称", hint: "在来源列表中显示") {
                                TextField("例如：需求讨论群", text: $name)
                                    .textFieldStyle(.roundedBorder)
                            }
                            formField("默认项目", hint: "新任务默认归属，可留空") {
                                TextField("例如：atm", text: $project)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                    }

                    settingsSection(title: "收集范围", icon: "line.3.horizontal.decrease.circle") {
                        VStack(alignment: .leading, spacing: 14) {
                            formField("排除关键词", hint: "包含这些词的消息会被跳过，多个词用逗号分隔") {
                                TextField("例如：闲聊, 打卡", text: $excludePattern)
                                    .textFieldStyle(.roundedBorder)
                            }
                            formField("重点关注", hint: "用自然语言描述希望提取的内容；群聊消息不会被当作指令") {
                                TextField("例如：只关注 MR、需求和线上问题", text: $instruction)
                                    .textFieldStyle(.roundedBorder)
                                    .help("这是可信指令，聊天内容不是。")
                            }
                        }
                    }

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
                                    intervalMinutes = value == "observe" ? 60 : 5
                                }
                            }

                            HStack(alignment: .top, spacing: 14) {
                                formField("采集频率", hint: "每次检查新消息的间隔") {
                                    Stepper(value: $intervalMinutes, in: 1...1440) {
                                        Text("每 \(intervalMinutes) 分钟")
                                            .monospacedDigit()
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                    }
                                }
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

                            formField("知识库集合", hint: "留空时沉淀到 inbox") {
                                TextField("inbox", text: $knowledgeCollection)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                    }
                }
                .padding(22)
            }

            Divider()

            HStack(spacing: 10) {
                Text("配置将在下一次收集时生效")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                Spacer()
                Button("取消", action: onClose)
                    .keyboardShortcut(.cancelAction)
                Button(source == nil ? "添加" : "保存") {
                    store.addCollectionSource(
                        connector: connector, target: target, name: name, project: project, priority: priority,
                        excludePattern: excludePattern, instruction: instruction,
                        knowledgeCollection: knowledgeCollection, strategy: strategy,
                        intervalMinutes: intervalMinutes, enabled: source?.enabled ?? true
                    )
                    onClose()
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(connector.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          kind.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || target.value.isEmpty)
            }
            .padding(.horizontal, 22)
            .padding(.vertical, 14)
            .background(ATMTheme.surface)
        }
        .background(ATMTheme.canvas)
        .frame(width: 600, height: source == nil ? 680 : 640)
    }

    private var sheetHeader: some View {
        HStack(spacing: 13) {
            Image(systemName: source == nil ? "person.badge.plus" : "person.3.fill")
                .font(ATMFont.font(.title3, weight: .semibold))
                .foregroundStyle(ATMTheme.accent)
                .frame(width: 38, height: 38)
                .background(ATMTheme.accentFill, in: RoundedRectangle(cornerRadius: 10))

            VStack(alignment: .leading, spacing: 3) {
                Text(source == nil ? "添加连接器来源" : "编辑连接器来源")
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

    @ViewBuilder
    private var sourceSection: some View {
        settingsSection(title: "来源", icon: "point.3.connected.trianglepath.dotted") {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .top, spacing: 14) {
                    formField("连接器", hint: "对应 config.json 中 collection_connectors 的键") {
                        TextField("例如：slack", text: $connector)
                            .textFieldStyle(.roundedBorder)
                            .disabled(source != nil)
                            .onChange(of: connector) { _ in resetSearch() }
                    }
                    formField("来源类型", hint: "由连接器定义，例如 channel、issue") {
                        TextField("channel", text: $kind)
                            .textFieldStyle(.roundedBorder)
                            .disabled(source != nil)
                            .onChange(of: kind) { _ in resetSearch() }
                    }
                }

                if manualEntry {
                    manualIdentifierField
                } else {
                    searchField
                    candidateList
                }
            }
        }
    }

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

    private func priorityLabel(_ value: String) -> String {
        switch value {
        case "P0": return "P0 · 紧急"
        case "P1": return "P1 · 高"
        case "P3": return "P3 · 低"
        default: return "P2 · 普通"
        }
    }

    private var subtitle: String {
        if source != nil {
            return "来源 ID 已锁定；你可以调整名称、筛选范围与处理方式。"
        }
        return "选择已配置的连接器，并搜索或手动填写来源标识。"
    }

    /// A picked candidate carries the connector-resolved kind and stable ID.
    private var target: ATMCollectionSourceTarget {
        if let source {
            return .identifier(kind: source.kind, externalID: source.externalID)
        }
        if let selection, !manualEntry {
            return .candidate(selection)
        }
        return .identifier(kind: kind, externalID: externalID)
    }

    private var searchField: some View {
        formField("搜索来源", hint: "连接器支持搜索时可按名称选择；也可以直接填写稳定 ID") {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    TextField("名称或关键词", text: $keyword)
                        .textFieldStyle(.roundedBorder)
                        .onSubmit(search)
                    Button("搜索", action: search)
                        .disabled(connector.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                                  keyword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSearching)
                    if isSearching {
                        ProgressView().controlSize(.small)
                    }
                }
                if let searchError {
                    Text(searchError)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.danger)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Button("改为手动填写 ID") { manualEntry = true }
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
            }
        }
    }

    @ViewBuilder
    private var candidateList: some View {
        if !candidates.isEmpty {
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(candidates) { candidate in
                        candidateRow(candidate)
                        if candidate != candidates.last { Divider() }
                    }
                }
            }
            .frame(maxHeight: 180)
            .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 8))
            .overlay {
                RoundedRectangle(cornerRadius: 8)
                    .stroke(ATMTheme.border, lineWidth: 1)
            }
        } else if !isSearching, let searchedKeyword, searchError == nil {
            Text("没有找到「\(searchedKeyword)」。请换个关键词，或改为手动填写 ID。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func candidateRow(_ candidate: ATMCollectionCandidate) -> some View {
        Button {
            let previous = selection
            selection = candidate
            // The resolved name is a starting point, not a lock: a 显示名称 the
            // person typed themselves survives switching candidates.
            if name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || name == previous?.name {
                name = candidate.name
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "link.circle.fill")
                    .frame(width: 18)
                    .foregroundStyle(ATMTheme.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(candidate.name)
                        .lineLimit(1)
                    if let detail = candidate.detail, !detail.isEmpty {
                        Text(detail)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer()
                if selection == candidate {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(ATMTheme.success)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var manualIdentifierField: some View {
        formField("来源 ID", hint: source == nil ? "填写连接器使用的稳定唯一标识" : "唯一标识由连接器提供，保存时不会更改") {
            VStack(alignment: .leading, spacing: 8) {
                if source != nil {
                    HStack(spacing: 8) {
                        Image(systemName: "lock.fill")
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.secondary)
                        Text(externalID)
                            .font(ATMFont.mono(.body))
                            .foregroundStyle(ATMTheme.primary)
                            .lineLimit(1)
                            .textSelection(.enabled)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 10)
                    .frame(height: 30)
                    .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 6))
                    .overlay {
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(ATMTheme.border, lineWidth: 1)
                    }
                } else {
                    TextField("粘贴来源 ID", text: $externalID)
                        .textFieldStyle(.roundedBorder)
                    Button("返回按名称搜索") {
                        manualEntry = false
                        externalID = ""
                    }
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
                }
            }
        }
    }

    private func resetSearch() {
        candidates = []
        selection = nil
        searchedKeyword = nil
        searchError = nil
    }

    @MainActor
    private func search() {
        let trimmed = keyword.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isSearching else { return }
        isSearching = true
        searchError = nil
        Task {
            let (found, error) = await store.searchCollectionSources(
                connector: connector, kind: kind, keyword: trimmed
            )
            candidates = found
            searchError = error
            searchedKeyword = trimmed
            selection = found.count == 1 ? found.first : nil
            if let only = found.first, found.count == 1,
               name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                name = only.name
            }
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

private func collectionActionTitle(_ action: String) -> String {
    switch action {
    case "create": return "已创建"
    case "append": return "已补充"
    case "insight": return "已沉淀"
    case "ignore": return "无需处理"
    // 「等待重试」听着像在等人按一下；实际是下一轮 collect 自动重来。
    case "failed": return "下轮自动重试"
    case "reverted": return "已撤销"
    default: return "等待处理"
    }
}

private func collectionActionIcon(_ action: String) -> String {
    switch action {
    case "create": return "plus.circle.fill"
    case "append": return "arrow.triangle.branch"
    case "insight": return "book.closed.fill"
    case "ignore": return "minus.circle"
    case "failed": return "exclamationmark.triangle.fill"
    case "reverted": return "arrow.uturn.backward.circle.fill"
    default: return "clock"
    }
}

/// 动作色。create / failed 是状态，走状态色；insight / reverted 是分类，走
/// ATMTheme.palette，不再另开裸 `.purple` / `.indigo`。
private func collectionActionColor(_ action: String) -> Color {
    switch action {
    case "create": return ATMTheme.success
    case "append": return ATMTheme.accent
    case "insight": return ATMTheme.palette[2]
    case "ignore": return ATMTheme.secondary
    case "failed": return ATMTheme.danger
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
