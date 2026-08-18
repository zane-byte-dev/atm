import AppKit
import SwiftUI

struct DesktopAgentsView: View {
    /// 侧栏的两种范围：活跃只看实时窗口内的会话，全部走持久索引。
    ///
    /// 只有活跃列表时，一个会话滑出实时窗口就再也点不到了——搜索要求你先知道搜什么。
    private enum ListScope: String, CaseIterable {
        case live
        case all

        var title: String {
            switch self {
            case .live: return "活跃"
            case .all: return "全部"
            }
        }
    }

    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var launchError: String?
    @State private var scope: ListScope = .live
    @State private var selectedIndexedSessionID: String?
    @AppStorage("ATMCollapsedAgentGroups") private var collapsedGroupsRaw = ""

    private var collapsedGroups: Set<String> {
        Set(collapsedGroupsRaw.split(separator: ",").map(String.init))
    }

    private func expandedBinding(for state: ATMAgentPresenceState) -> Binding<Bool> {
        Binding(
            get: { !collapsedGroups.contains(state.id) },
            set: { expanded in
                var set = collapsedGroups
                if expanded { set.remove(state.id) } else { set.insert(state.id) }
                collapsedGroupsRaw = set.sorted().joined(separator: ",")
            }
        )
    }

    private var sessions: [ATMLiveSession] {
        ATMAgentPresenceOrdering.sorted(
            store.snapshot.liveStatus.sessions.filter { $0.activityState != "unobserved" }
        )
    }

    private var selectedSession: ATMLiveSession? {
        guard let id = navigation.selectedAgentID else { return nil }
        return sessions.first { $0.id == id }
    }

    private var unobservedBindingCount: Int {
        store.snapshot.liveStatus.bindings.filter { !$0.observed }.count
    }

    var body: some View {
        ATMSplitColumn(
            id: "agents",
            defaultWidth: ATMWorkspaceLayout.navigatorDefaultWidth,
            minWidth: ATMWorkspaceLayout.navigatorMinWidth,
            maxWidth: ATMWorkspaceLayout.navigatorMaxWidth,
            detailMinWidth: ATMWorkspaceLayout.readingDetailMinWidth
        ) {
            agentList
        } detail: {
            Group {
                switch scope {
                case .live:
                    if let session = selectedSession {
                        DesktopAgentPresenceDetail(
                            session: session,
                            relatedTodo: relatedTodo(for: session),
                            runTodoID: navigation.selectedAgentRunTodoID ?? session.bindingTodoID,
                            store: store,
                            navigation: navigation,
                            onOpenSession: { openSession(session) }
                        )
                        .id(session.id)
                    } else {
                        emptyDetail
                    }
                case .all:
                    if let indexed = selectedIndexedSession {
                        DesktopIndexedSessionDetail(
                            session: indexed,
                            relatedTodo: relatedTodo(forSessionID: indexed.id),
                            store: store,
                            navigation: navigation
                        )
                        .id(indexed.id)
                    } else {
                        emptyDetail
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .atmAnimatedSwap(detailIdentity, style: .detail)
        }
        .onAppear {
            selectFirstIfNeeded()
            revealSelectedGroup()
            store.startLiveStatusPolling()
        }
        .onDisappear { store.stopLiveStatusPolling() }
        .onChange(of: sessions.map(\.id)) { _ in selectFirstIfNeeded() }
        .onChange(of: scope) { newScope in
            if newScope == .all {
                // 第一次切到全部才拉索引：活跃列表本来就够用的人不该为一千行付代价。
                if store.indexedSessions.isEmpty { store.loadIndexedSessions(reset: true) }
                if selectedIndexedSessionID == nil { selectedIndexedSessionID = store.indexedSessions.first?.id }
            }
        }
        .onChange(of: store.indexedSessions.map(\.id)) { ids in
            guard scope == .all else { return }
            if let selected = selectedIndexedSessionID, ids.contains(selected) { return }
            selectedIndexedSessionID = ids.first
        }
        .onChange(of: navigation.selectedAgentID) { _ in revealSelectedGroup() }
        .alert(
            "无法打开来源会话",
            isPresented: Binding(
                get: { launchError != nil },
                set: { if !$0 { launchError = nil } }
            )
        ) {
            Button("好") { launchError = nil }
        } message: {
            Text(launchError ?? "未知错误")
        }
    }

    private var agentList: some View {
        ATMGroupedNavigator {
            ATMNavigatorHeader {
                ATMCompactSegmentedTabs(
                    selection: $scope,
                    items: ListScope.allCases.map { (value: $0, title: $0.title) }
                )
            } trailing: {
                EmptyView()
            }
        } content: {
            if scope == .all {
                indexedList
            } else {
                liveList
            }
        }
    }

    private var liveList: some View {
        VStack(spacing: 0) {
            if sessions.isEmpty {
                ATMEmptyState(
                    icon: "cpu",
                    title: "当前没有活跃 Agent",
                    detail: "ATM 会在检测到新的 Agent 会话活动后自动显示。"
                )
            } else {
                ATMGroupedNavigatorScroll {
                    ForEach(ATMAgentPresenceState.allCases) { state in
                        let values = sessions.filter { $0.presenceState == state }
                        if !values.isEmpty {
                            let expanded = expandedBinding(for: state)
                            ATMNavigatorGroup {
                                ATMNavigatorGroupHeader(
                                    title: state.title,
                                    count: values.count,
                                    tint: state.tint,
                                    isExpanded: expanded
                                )
                            } content: {
                                if expanded.wrappedValue {
                                    ForEach(values) { session in
                                        Button {
                                            navigation.selectedAgentID = session.id
                                            navigation.selectedAgentRunTodoID = nil
                                        } label: {
                                            DesktopAgentPresenceRow(
                                                session: session,
                                                isSelected: navigation.selectedAgentID == session.id
                                            )
                                        }
                                        .buttonStyle(.atmRow)
                                        // 行里不再逐张画来源，tooltip 兜住完整来源。
                                        .help(originLabel(session))
                                        .atmContentStackRow()
                                    }
                                }
                            }
                        }
                    }
                }
            }

            if unobservedBindingCount > 0 {
                Divider()
                // 说会话状态，不说内部模型：「binding / 不计入活跃 Agent」是实现词汇。
                Text("另有 \(unobservedBindingCount) 个会话休眠中")
                    .font(ATMFont.font(.caption, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                    .padding(.horizontal, 14)
                    .frame(maxWidth: .infinity, minHeight: 34, alignment: .leading)
                    .help("这些会话有显式绑定但当前没有实时活动，因此不计入活跃 Agent。")
            }
        }
    }

    /// 持久索引列表：按开始时间倒序，一页 200 条，滚到底再要下一页。
    private var indexedList: some View {
        VStack(spacing: 0) {
            if let error = store.indexedSessionsError {
                let presentation = ATMErrorPresentation.resolve(error, fallbackTitle: "会话列表加载失败")
                ATMInlineNotice(
                    severity: .warning,
                    title: presentation.title,
                    message: presentation.message,
                    details: error,
                    actionTitle: "重试",
                    onAction: { store.loadIndexedSessions(reset: true) },
                    onDismiss: { store.indexedSessionsError = nil }
                )
                .padding(8)
            }
            if store.indexedSessions.isEmpty {
                ATMEmptyState(
                    icon: "clock.arrow.circlepath",
                    title: store.isLoadingIndexedSessions ? "读取会话索引…" : "索引里还没有会话",
                    detail: "同步过的 Agent 会话都会出现在这里，包括早已结束的。"
                )
            } else {
                ATMGroupedNavigatorScroll(spacing: 0) {
                    ForEach(store.indexedSessions) { session in
                        Button {
                            selectedIndexedSessionID = session.id
                        } label: {
                            DesktopIndexedSessionRow(
                                session: session,
                                isSelected: selectedIndexedSessionID == session.id
                            )
                        }
                        .buttonStyle(.atmRow)
                        .atmContentStackRow()
                    }
                    if !store.indexedSessionsReachedEnd {
                        Button {
                            store.loadIndexedSessions()
                        } label: {
                            HStack {
                                Spacer()
                                Text(store.isLoadingIndexedSessions ? "读取中…" : "更多会话")
                                    .font(ATMFont.footnote)
                                    .foregroundStyle(ATMTheme.secondary)
                                Spacer()
                            }
                            .frame(minHeight: 34)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .disabled(store.isLoadingIndexedSessions)
                        .atmContentStackRow()
                    }
                }
            }
        }
    }

    private var selectedIndexedSession: ATMIndexedSession? {
        guard let id = selectedIndexedSessionID else { return nil }
        return store.indexedSessions.first { $0.id == id }
    }

    private var detailIdentity: String {
        switch scope {
        case .live: return selectedSession?.id ?? "empty"
        case .all: return selectedIndexedSessionID ?? "empty"
        }
    }

    private func revealSelectedGroup() {
        guard let session = selectedSession else { return }
        var set = collapsedGroups
        guard set.remove(session.presenceState.id) != nil else { return }
        collapsedGroupsRaw = set.sorted().joined(separator: ",")
    }

    /// 行里不画来源（单项目单客户端时它是各行唯一不变的东西），完整来源只挂 tooltip。
    private func originLabel(_ session: ATMLiveSession) -> String {
        let client = ATMAgentDisplay.clientName(session)
        let project = ATMAgentDisplay.projectName(session)
        return "\(client) · \(project)"
    }

    private var emptyDetail: some View {
        ATMEmptyState(
            icon: "cpu",
            title: "选择一个 Agent 会话",
            detail: "查看它在哪里、正在做什么，以及是否需要你介入。"
        )
    }

    private func selectFirstIfNeeded() {
        if let selected = navigation.selectedAgentID,
           sessions.contains(where: { $0.id == selected }) {
            return
        }
        navigation.selectedAgentID = sessions.first?.id
        navigation.selectedAgentRunTodoID = nil
    }

    private func relatedTodo(for session: ATMLiveSession) -> ATMTodo? {
        guard let todoID = session.bindingTodoID else { return nil }
        return store.allTodos.first { $0.id.caseInsensitiveCompare(todoID) == .orderedSame }
    }

    /// 归档会话没有 live 载荷里的绑定字段，所以从实时快照的绑定账本里反查。
    /// 找不到只表示这个会话没绑过 Todo，不是错误。
    private func relatedTodo(forSessionID sessionID: String) -> ATMTodo? {
        // Codex 绑定存的是线程 uuid，索引存的是 rollout 文件名，后者以前者结尾。
        guard let context = store.snapshot.liveStatus.bindings.first(where: {
            $0.binding.sessionID == sessionID || sessionID.hasSuffix("-\($0.binding.sessionID)")
        }) else { return nil }
        return store.allTodos.first { $0.id.caseInsensitiveCompare(context.binding.todoID) == .orderedSame }
    }

    private func openSession(_ session: ATMLiveSession) {
        navigation.selectedAgentID = session.id
        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        guard route.isAvailable else { return }
        do {
            _ = try ATMAgentSessionLauncher.open(session)
        } catch {
            launchError = error.localizedDescription
        }
    }

}

private extension ATMAgentPresenceState {
    var tint: Color {
        switch self {
        case .attention: return ATMTheme.danger
        case .active: return ATMTheme.success
        case .recent: return ATMTheme.secondary
        }
    }
}

/// 紧凑卡片：第一行是身份与状态；只有关联任务或最新输入时才出现第二行。
///
/// 之前是四行，其中三行在同一栏里逐张复读：`Codex Desktop · atm`（栏内恒定，且占着
/// 卡片首行最抢眼的位置）、`未绑定`（默认态，等于把「什么都没发生」高亮成胶囊）、
/// 模型名（同一客户端下几乎恒定，详情页「技术信息」已有）。状态本身也被说了三遍——
/// 分区标题、彩色圆点、`N 分钟活跃` 里的「活跃」二字。分区标题已经承载状态语义，
/// 圆点在同色分区里没有信息，时长因此退回纯时长。来源与项目也已经在详情头部完整展示，
/// 不再作为每张卡的标签复读。
private struct DesktopAgentPresenceRow: View {
    let session: ATMLiveSession
    let isSelected: Bool

    var body: some View {
        ATMNavigatorRow(isSelected: isSelected) {
            ATMAgentMark(agent: session.tool, size: 16)
                .frame(
                    width: ATMContentRowLayout.leadingVisualSize,
                    height: ATMContentRowLayout.leadingVisualSize
                )
        } content: {
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                Text(session.presenceTitle)
                    // 固定字重 —— 见 ATMRowSurface：选中不切字重，否则标题会抖。
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(1)

                if session.bindingTodoID != nil || session.latestUserInputBelowTitle != nil {
                    HStack(alignment: .firstTextBaseline, spacing: 5) {
                        if let todoID = session.bindingTodoID {
                            Text(todoID.uppercased())
                                .font(ATMFont.mono(.caption, .medium))
                                .foregroundStyle(ATMTheme.accent)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(ATMTheme.accentFill, in: Capsule())
                        }
                        if let input = session.latestUserInputBelowTitle {
                            Text("你")
                                .font(ATMFont.font(.caption, weight: .semibold))
                            Text(input)
                                .font(ATMFont.footnote)
                                .lineLimit(1)
                        }
                        Spacer(minLength: 0)
                    }
                    .foregroundStyle(ATMTheme.secondary)
                }
            }
        } trailing: {
            trailingMeta
        }
    }

    /// 尾部整块 `fixedSize`：让长标题先被截断，而不是把介入状态和时长挤没。
    private var trailingMeta: some View {
        HStack(spacing: 6) {
            if session.presenceState == .attention {
                Text("需要你")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.danger)
            }
            Text(NumberFormat.age(session.ageSeconds))
                .font(ATMFont.mono(.caption, .medium))
                .foregroundStyle(ATMTheme.secondary)
        }
        .fixedSize()
    }
}

private struct DesktopAgentPresenceDetail: View {
    private enum DetailTab: String, CaseIterable {
        case overview
        case updates
        case transcript
        case logs
    }

    /// 执行动态 tab 里默认展开的条数；更早的收进 DisclosureGroup。
    /// CLI 侧 RecentUpdates 也截到这个上限，所以常态下不会触发折叠。
    private static let expandedUpdateLimit = 10

    let session: ATMLiveSession
    let relatedTodo: ATMTodo?
    let runTodoID: String?
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    let onOpenSession: () -> Void

    @State private var selectedTab: DetailTab = .overview
    @State private var copied = false
    @State private var showingInterruptConfirmation = false

    var body: some View {
        ATMDetailScaffold {
            detailHeader
        } tabs: {
            ATMCapsuleTabs(selection: $selectedTab, items: detailTabItems)
        } content: {
            Group {
                switch selectedTab {
                case .overview:
                    overviewContent
                case .updates:
                    updatesContent
                case .transcript:
                    DesktopSessionTranscriptView(
                        sessionID: session.sessionID,
                        agentLabel: ATMAgentDisplay.name(session.tool),
                        store: store
                    )
                case .logs:
                    fullLogsContent
                }
            }
            .atmAnimatedSwap(selectedTab.rawValue, style: .detail)
        }
        .confirmationDialog(
            "中断当前 Agent 执行？",
            isPresented: $showingInterruptConfirmation,
            titleVisibility: .visible
        ) {
            // Interrupt the run this session *is*, never a same-Todo run that
            // belongs to somebody else's process.
            if let run = taskRun {
                Button("中断执行", role: .destructive) {
                    store.interruptTaskRun(todoID: run.todoID)
                }
            }
            Button("继续执行", role: .cancel) {}
        } message: {
            Text("Agent 进程会停止，关联 Todo 保持工作中。")
        }
        .onAppear { loadTaskRunIfNeeded() }
        .onChange(of: session.id) { _ in
            // 全部日志 only exists for a dispatched run, so a different session
            // must not keep a tab that is no longer on screen selected.
            if selectedTab == .logs { selectedTab = .overview }
        }
        .task(id: fullLogRefreshKey) {
            guard selectedTab == .logs, let todoID = runTodoID else { return }
            store.loadTaskRuns(for: todoID)
            if taskRun != nil {
                store.loadTaskRunLog(for: todoID)
            }
        }
    }

    private var detailHeader: some View {
        ATMDetailHeader(title: session.presenceTitle) {
            headerIdentity
        } actions: {
            headerActions
        } meta: {
            HStack(spacing: 6) {
                Circle()
                    .fill(session.presenceState.tint)
                    .frame(width: 7, height: 7)
                Text(session.presenceState == .attention ? "需要你" : activityLabel)
                    .font(ATMFont.font(.footnote, weight: .medium))
                    .foregroundStyle(session.presenceState.tint)
            }
        }
        .onChange(of: session.id) { _ in copied = false }
    }

    private var headerIdentity: some View {
        HStack(spacing: 7) {
            ATMAgentMark(agent: session.tool, size: 18)
            Text(ATMAgentDisplay.clientName(session))
            Text("·")
            Label(ATMAgentDisplay.projectName(session), systemImage: "folder")
            Text("·")
            Text(shortSessionID(session.sessionID))
                .font(ATMFont.mono(.footnote, .medium))
        }
        .font(ATMFont.font(.footnote, weight: .medium))
        .foregroundStyle(ATMTheme.secondary)
        .lineLimit(1)
    }

    private var headerActions: some View {
        HStack(spacing: 6) {
            if taskRun?.isActive == true {
                Button {
                    showingInterruptConfirmation = true
                } label: {
                    Label("中断", systemImage: "stop.fill")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .tint(ATMTheme.danger)
                .disabled(store.isActing)
                .help("中断当前 Agent 执行")
            }
            Menu {
                Button {
                    copySessionID()
                } label: {
                    Label(copied ? "已复制会话 ID" : "复制会话 ID", systemImage: copied ? "checkmark" : "doc.on.doc")
                }
            } label: {
                Image(systemName: "ellipsis.circle")
                    .font(ATMFont.font(.body, weight: .medium))
            }
            .menuStyle(.borderlessButton)
            .controlSize(.small)
            .help("更多会话操作")

            Button(action: onOpenSession) {
                Label(launchRoute.actionTitle, systemImage: "arrow.up.forward.app")
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
            .disabled(!launchRoute.isAvailable)
            .help(launchRoute.destinationLabel)
        }
        .font(ATMFont.font(.footnote, weight: .medium))
        .fixedSize()
    }

    private var detailTabItems: [(value: DetailTab, title: String)] {
        var items: [(value: DetailTab, title: String)] = [
            (.overview, "概览"),
            (.updates, "执行动态"),
            (.transcript, "对话"),
        ]
        // Only a session that is itself a dispatched run has a raw log.
        if taskRun != nil {
            items.append((.logs, "全部日志"))
        }
        return items
    }

    private var overviewContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if session.presenceState == .attention {
                    attentionBanner
                }
                if let input = session.latestUserInputBelowTitle {
                    latestUserInput(input)
                }
                if let update = session.latestVisibleUpdate {
                    latestExecutionUpdate(update)
                }
                if let latestResultText = session.latestResultText {
                    latestResult(latestResultText)
                }
                if session.bindingTodoID != nil {
                    relatedTask
                }
                technicalDetails
            }
            .frame(maxWidth: 820, alignment: .leading)
            .padding(.horizontal, 24)
            .padding(.vertical, 20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var updatesContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                executionUpdates
            }
            .frame(maxWidth: 820, alignment: .leading)
            .padding(.horizontal, 24)
            .padding(.vertical, 20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    @ViewBuilder
    private var fullLogsContent: some View {
        if let todoID = runTodoID, taskRun != nil {
            VStack(spacing: 0) {
                HStack(spacing: 8) {
                    Label("Codex 原始执行日志", systemImage: "terminal")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Spacer(minLength: 12)
                    Button("刷新") { store.loadTaskRunLog(for: todoID) }
                        .controlSize(.small)
                }
                .padding(.horizontal, 16)
                .frame(height: 42)
                .background(ATMTheme.listPane)

                if let log = store.taskRunLog(for: todoID) {
                    ATMTranscriptTextView(
                        text: log.isEmpty ? "该执行尚未产生日志。" : log,
                        font: .monospacedSystemFont(ofSize: ATMFont.Tier.caption.size, weight: .regular),
                        insets: NSSize(width: 16, height: 14),
                        accessibilityLabel: "Codex 全部执行日志",
                        scrollsToEndOnUpdate: true
                    )
                    .background(ATMTheme.canvas)
                } else if let error = store.taskRunLogError(for: todoID) {
                    VStack(spacing: 10) {
                        Image(systemName: "exclamationmark.triangle")
                            .font(ATMFont.font(.display, weight: .light))
                        Text("无法读取执行日志")
                            .font(ATMFont.font(.title3, weight: .semibold))
                        Text(error)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .multilineTextAlignment(.center)
                        Button("重试") { store.loadTaskRunLog(for: todoID) }
                            .buttonStyle(.borderedProminent)
                    }
                    .padding(28)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    VStack(spacing: 10) {
                        ProgressView()
                        Text("正在读取全部日志")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        } else {
            VStack(spacing: 10) {
                Image(systemName: "terminal")
                    .font(ATMFont.font(.display, weight: .light))
                Text("没有 Codex 执行日志")
                    .font(ATMFont.font(.title3, weight: .semibold))
                Text("只有通过 Todo 的“交给 Codex”启动的 Agent 会话会保存原始执行日志。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(28)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// The dispatched run this session actually *is* — never merely a run of the
    /// same Todo. A hand-bound session (you working the same Todo in another
    /// Agent) would otherwise inherit that Todo's Codex run, and the raw-log tab
    /// would present somebody else's log while 中断 would stop somebody else's
    /// process.
    private var taskRun: ATMTaskRun? {
        guard let todoID = runTodoID else { return nil }
        let runs = store.taskRuns(for: todoID)
        if let linked = runs.first(where: {
            ATMTaskRunSessionRouting.identifiersMatch($0.sessionID, session)
        }) {
            return linked
        }
        // Arriving from that Todo's “Codex 执行” card is itself the pairing: the
        // card resolved this session from that run, and a run that has not
        // reported its session id yet has nothing else to match on.
        guard navigation.selectedAgentRunTodoID == todoID else { return nil }
        return runs.first
    }

    private var fullLogRefreshKey: String {
        [
            session.id,
            runTodoID ?? "none",
            selectedTab.rawValue,
            taskRun?.id ?? "none",
            taskRun?.status ?? "none",
        ].joined(separator: "|")
    }

    private func loadTaskRunIfNeeded() {
        guard let runTodoID else { return }
        store.loadTaskRuns(for: runTodoID)
    }

    private var attentionBanner: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "person.crop.circle.badge.exclamationmark")
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                .foregroundStyle(ATMTheme.danger)
            VStack(alignment: .leading, spacing: 4) {
                Text("这个 Agent 需要你")
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .foregroundStyle(ATMTheme.danger)
                Text(attentionText)
                    .font(ATMFont.font(.bodyLarge, weight: .medium))
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.dangerFill, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(ATMTheme.danger.opacity(0.18))
        }
    }

    private func latestUserInput(_ text: String) -> some View {
        detailSection("你的输入") {
            VStack(alignment: .leading, spacing: 10) {
                ATMMarkdownContentView(source: text)
                if let cwd = nonEmpty(session.cwd) {
                    Label(cwd, systemImage: "folder")
                        .font(ATMFont.mono(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                        .textSelection(.enabled)
                }
            }
        }
    }

    /// Keep the overview useful while the Agent is still working: the newest
    /// stage update is visible without opening the full timeline. The content
    /// fades between polling updates, while the small live mark breathes only
    /// for an active session and honors Reduce Motion.
    private func latestExecutionUpdate(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: 11) {
            HStack(spacing: 8) {
                DesktopAgentLiveUpdateMark(isActive: session.isCurrentlyActive)
                Text("最新动态")
                    .font(ATMFont.font(.body, weight: .semibold))
                Spacer(minLength: 8)
                Button("查看全部") { selectedTab = .updates }
                    .buttonStyle(.plain)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.accent)
            }

            ATMMarkdownContentView(source: text)
                .frame(maxWidth: .infinity, alignment: .leading)
                .atmAnimatedSwap(text, style: .detail)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    /// 不叫「最新完成结果」：`latestResult` 是最后一次 final answer，会话往下可能还在跑，
    /// 于是标题写着「完成」、正文的状态标签写着「正在继续处理」，自己打自己。状态也不在
    /// 这里重复——头部的圆点加文字已经说了一遍，需要介入时 attentionBanner 还会说一遍。
    private func latestResult(_ text: String) -> some View {
        detailSection("最新进展") {
            ATMMarkdownContentView(source: text)
        }
    }

    /// 最新一条在最上面；默认展开最近 `expandedUpdateLimit` 条，更早的收进折叠。
    ///
    /// `updates` 从 CLI 出来是按时间正序的（见 parser 里的 `recentUpdates`），全展开时
    /// 得从上往下读完才知道现在是什么状态，而越靠上的越旧——往往还是同一件事的多次重述。
    /// 单独 tab 后可以放宽展开窗口；再往上就只挡视线了。
    /// 不再套带标题或逐条浮起的卡片：tab 自己已经叫「执行动态」，各条之间留白即可。
    private var executionUpdates: some View {
        let updates = Array(session.visibleUpdates.reversed())
        return Group {
            if updates.isEmpty {
                VStack(spacing: 9) {
                    Image(systemName: "clock.arrow.circlepath")
                        .font(ATMFont.font(.display, weight: .light))
                    Text("暂无执行动态")
                        .font(ATMFont.font(.bodyLarge, weight: .medium))
                    Text("Agent 输出阶段性进展时，会按时间出现在这里。")
                        .font(ATMFont.footnote)
                        .multilineTextAlignment(.center)
                }
                .foregroundStyle(ATMTheme.secondary)
                .frame(maxWidth: .infinity, minHeight: 180)
                .padding(.vertical, 28)
            } else {
                VStack(alignment: .leading, spacing: 12) {
                    executionUpdateList(Array(updates.prefix(Self.expandedUpdateLimit)))
                    if updates.count > Self.expandedUpdateLimit {
                        let older = Array(updates.dropFirst(Self.expandedUpdateLimit))
                        DisclosureGroup("更早 \(older.count) 条动态") {
                            executionUpdateList(older)
                            .padding(.top, 10)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                    }
                }
            }
        }
    }

    /// 扁平列表让连续的短进展保持一组，留白负责区分消息，避免形成表格感。
    private func executionUpdateList(_ updates: [String]) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            ForEach(Array(updates.enumerated()), id: \.offset) { _, update in
                ATMMarkdownContentView(source: update)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 12)
    }

    private var relatedTask: some View {
        detailSection("关联任务") {
            if let todoID = session.bindingTodoID {
                Button {
                    navigation.selectedTodoID = todoID
                    navigation.section = .tasks
                } label: {
                    HStack(spacing: 10) {
                        Image(systemName: "checklist")
                            .foregroundStyle(ATMTheme.accent)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(todoID.uppercased())
                                .font(ATMFont.mono(.caption, .semibold))
                                .foregroundStyle(ATMTheme.accent)
                            Text(relatedTodo?.title ?? session.todo?.title ?? "已关联 Todo")
                                .font(ATMFont.font(.body, weight: .medium))
                                .foregroundStyle(ATMTheme.primary)
                                .lineLimit(2)
                        }
                        Spacer(minLength: 0)
                        Image(systemName: "chevron.right")
                            .font(ATMFont.font(.caption, weight: .semibold))
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .help("打开关联任务")
            }
        }
    }

    private var technicalDetails: some View {
        detailSection("技术信息") {
            VStack(spacing: 9) {
                infoRow("来源", ATMAgentDisplay.clientName(session))
                infoRow("入口", launchRoute.destinationLabel)
                infoRow("模型", session.model ?? "未知")
                infoRow("会话", session.sessionID)
                if let resumeID = nonEmpty(session.resumeID) { infoRow("完整 ID", resumeID) }
                if let pid = nonEmpty(session.pid) { infoRow("进程", "PID \(pid)") }
                if let tty = nonEmpty(session.tty) { infoRow("终端", tty) }
                if !session.recentTools.isEmpty {
                    infoRow("动作", session.recentTools.joined(separator: " · "))
                }
                if !session.topics.isEmpty {
                    infoRow("主题", session.topics.joined(separator: " · "))
                }
            }
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)
        }
    }

    private var attentionText: String {
        if session.bindingState != "bound" && session.bindingState != "unbound" {
            return "Session binding 与 Todo 当前状态不一致，请打开任务确认。"
        }
        return "最近回复可能正在等待你的确认或补充信息。"
    }

    private var activityLabel: String {
        if session.isCurrentlyActive { return "刚刚更新" }
        return "\(NumberFormat.age(session.ageSeconds))活跃"
    }

    private var launchRoute: ATMAgentSessionLaunchRoute {
        ATMAgentSessionLaunchRoute.resolve(for: session)
    }

    private func detailSection<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 11) {
            Text(title)
                .font(ATMFont.font(.body, weight: .semibold))
            content()
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    private func infoRow(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top, spacing: 9) {
            Text(label)
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 46, alignment: .leading)
            Text(value)
                .textSelection(.enabled)
            Spacer(minLength: 0)
        }
        .font(ATMFont.footnote)
    }

    private func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    private func shortSessionID(_ value: String) -> String {
        let value = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return value.count > 12 ? String(value.prefix(8)) : value
    }

    private func copySessionID() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(session.resumeID ?? session.sessionID, forType: .string)
        copied = true
    }

}

enum ATMAgentActivitySheetTab: String, CaseIterable, Identifiable {
    case updates
    case transcript

    var id: String { rawValue }

    var title: String {
        switch self {
        case .updates: return "执行动态"
        case .transcript: return "完整对话"
        }
    }

    var systemImage: String {
        switch self {
        case .updates: return "clock.arrow.circlepath"
        case .transcript: return "text.bubble"
        }
    }
}

/// A restrained activity cue for the overview's newest update. It lives only
/// in the selected Agent detail, so the continuous animation never repaints
/// background rows.
private struct DesktopAgentLiveUpdateMark: View {
    let isActive: Bool

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isBreathing = false

    var body: some View {
        ZStack {
            if isActive {
                Circle()
                    .stroke(ATMTheme.accent.opacity(0.45), lineWidth: 1)
                    .frame(width: 12, height: 12)
                    .scaleEffect(reduceMotion || !isBreathing ? 0.72 : 1.18)
                    .opacity(reduceMotion ? 0.55 : (isBreathing ? 0.08 : 0.65))
            }
            Circle()
                .fill(isActive ? ATMTheme.accent : ATMTheme.secondary)
                .frame(width: 6, height: 6)
        }
        .frame(width: 12, height: 12)
        .onAppear { syncBreathing() }
        .onChange(of: isActive) { _ in syncBreathing() }
        .onChange(of: reduceMotion) { _ in syncBreathing() }
        .accessibilityHidden(true)
    }

    private func syncBreathing() {
        withAnimation(
            reduceMotion || !isActive
                ? .linear(duration: 0)
                : .easeInOut(duration: 1.2).repeatForever(autoreverses: true)
        ) {
            isBreathing = isActive && !reduceMotion
        }
    }
}


/// 索引里的一行会话：身份、项目、开始时间和问答轮数。
///
/// 不画实时状态：这一栏本来就是「不在实时窗口里」的会话，画个灰点只会让人以为它还活着。
private struct DesktopIndexedSessionRow: View {
    let session: ATMIndexedSession
    let isSelected: Bool

    var body: some View {
        ATMNavigatorRow(isSelected: isSelected) {
            ATMAgentMark(agent: session.agent, size: 16)
                .frame(
                    width: ATMContentRowLayout.leadingVisualSize,
                    height: ATMContentRowLayout.leadingVisualSize
                )
        } content: {
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                Text(session.title)
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(1)

                HStack(spacing: 6) {
                    Label(session.project.isEmpty ? "未归属项目" : session.project, systemImage: "folder")
                        .lineLimit(1)
                    Text("·")
                    Text(session.startedAt.map(ATMSessionTimeFormat.clock) ?? session.createdAt)
                        .font(ATMFont.mono(.caption, .medium))
                    Spacer(minLength: 0)
                }
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
            }
        } trailing: {
            Text("Q\(session.qCount)")
                .font(ATMFont.mono(.caption, .medium))
                .foregroundStyle(ATMTheme.secondary)
        }
    }
}

/// 归档会话的详情：头部给身份与归属，正文交给三段式阅读器。
///
/// 与实时详情分开而不是共用一个视图：实时详情的中断、跳回会话、执行动态都建立在实时载荷
/// 之上，对一个已经结束的会话既拿不到也没意义；这里能提供的就是可信的历史内容本身。
private struct DesktopIndexedSessionDetail: View {
    let session: ATMIndexedSession
    let relatedTodo: ATMTodo?
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    var body: some View {
        VStack(spacing: 0) {
            header
            DesktopSessionTranscriptView(
                sessionID: session.id,
                agentLabel: ATMAgentDisplay.name(session.agent),
                store: store
            )
        }
        .background(ATMTheme.canvas)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 7) {
                ATMAgentMark(agent: session.agent, size: 18)
                Text(ATMAgentDisplay.name(session.agent))
                Text("·")
                Label(session.project.isEmpty ? "未归属项目" : session.project, systemImage: "folder")
                Text("·")
                Text(session.shortID)
                    .font(ATMFont.mono(.footnote, .medium))
                Spacer(minLength: 0)
            }
            .font(ATMFont.font(.footnote, weight: .medium))
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)

            Text(session.title)
                .font(ATMFont.font(.title2, weight: .semibold))
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 10) {
                Text(timeRange)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                if let todo = relatedTodo {
                    Button {
                        navigation.section = .tasks
                        navigation.selectedTodoID = todo.id
                    } label: {
                        Label("\(todo.id.uppercased()) \(todo.title)", systemImage: "checklist")
                            .lineLimit(1)
                    }
                    .buttonStyle(.borderless)
                    .controlSize(.small)
                    .help("跳到这个会话绑定的任务")
                }
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface)
    }

    /// 开始与最后活动分开显示，两者相同时只说一次：会话跨天时这是唯一能看出来的地方。
    private var timeRange: String {
        let start = session.startedAt.map(ATMSessionTimeFormat.clock) ?? session.createdAt
        guard let end = session.endedAt.map(ATMSessionTimeFormat.clock), end != start else {
            return start
        }
        return "\(start) → \(end)"
    }
}
