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
    @AppStorage("ATMExpandedAgentLineages") private var expandedLineagesRaw = ""
    @AppStorage(ATMNavigatorPresentationPreferences.agentsKey)
    private var liveListPresentationRaw = ATMNavigatorPresentationPreferences.defaultValue

    private var collapsedGroups: Set<String> {
        Set(collapsedGroupsRaw.split(separator: ",").map(String.init))
    }

    private var expandedLineages: Set<String> {
        Set(expandedLineagesRaw.split(separator: ",").map(String.init))
    }

    private var liveListPresentation: ATMNavigatorPresentation {
        ATMNavigatorPresentation.resolve(liveListPresentationRaw)
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

    private func selectedSession(in sessions: [ATMLiveSession]) -> ATMLiveSession? {
        guard let id = navigation.selectedAgentID else { return nil }
        return sessions.first { $0.id == id }
    }

    private var unobservedBindingCount: Int {
        store.snapshot.liveStatus.bindings.filter { !$0.observed }.count
    }

    var body: some View {
        // Sorting and lineage construction are list-level work. Keep one
        // snapshot for this render pass so row construction cannot re-run them.
        let sessions = sessions
        let selectedSession = selectedSession(in: sessions)
        return ATMSplitColumn(
            id: "agents",
            defaultWidth: ATMWorkspaceLayout.navigatorDefaultWidth,
            minWidth: ATMWorkspaceLayout.navigatorMinWidth,
            maxWidth: ATMWorkspaceLayout.navigatorMaxWidth,
            detailMinWidth: ATMWorkspaceLayout.readingDetailMinWidth
        ) {
            agentList(sessions: sessions)
        } detail: {
            Group {
                    switch scope {
                    case .live:
                        if let session = selectedSession {
                            DesktopAgentPresenceDetail(
                                session: session,
                                relatedTodo: relatedTodo(for: session),
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
            .atmAnimatedSwap(
                detailIdentity(selectedLiveSession: selectedSession),
                style: .detail
            )
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
        .onChange(of: liveListPresentationRaw) { _ in revealSelectedGroup() }
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

    private func agentList(sessions: [ATMLiveSession]) -> some View {
        ATMGroupedNavigator {
            ATMNavigatorHeader {
                ATMCompactSegmentedTabs(
                    selection: $scope,
                    items: ListScope.allCases.map { (value: $0, title: $0.title) }
                )
            } trailing: {
                if scope == .live {
                    ATMNavigatorPresentationToggle(storedValue: $liveListPresentationRaw)
                }
            }
        } content: {
            if scope == .all {
                indexedList
            } else {
                liveList(sessions: sessions)
            }
        }
    }

    private func liveList(sessions: [ATMLiveSession]) -> some View {
        let sessionsByState = Dictionary(grouping: sessions, by: \.presenceState)
        let visibleByState = sessionsByState.mapValues {
            ATMAgentPresenceOrdering.visibleSessions(
                $0,
                expandedLineages: expandedLineages,
                selectedID: navigation.selectedAgentID
            )
        }
        let visibleDepths = ATMAgentPresenceOrdering.visibleDepths(in: visibleByState)
        let childCounts = sessionsByState.values.reduce(into: [String: Int]()) { result, values in
            result.merge(ATMAgentPresenceOrdering.childCounts(values), uniquingKeysWith: +)
        }
        let visibleIDs = Set(visibleByState.values.flatMap { $0 }.map(\.id))
        return VStack(spacing: 0) {
            if sessions.isEmpty {
                ATMEmptyState(
                    icon: "cpu",
                    title: "当前没有活跃 Agent",
                    detail: "ATM 会在检测到新的 Agent 会话活动后自动显示。"
                )
            } else {
                ATMGroupedNavigatorScroll {
                    if liveListPresentation == .grouped {
                        ForEach(ATMAgentPresenceState.allCases) { state in
                            let allValues = sessionsByState[state] ?? []
                            let values = visibleByState[state] ?? []
                            if !allValues.isEmpty {
                                let expanded = expandedBinding(for: state)
                                ATMNavigatorGroup {
                                    ATMNavigatorGroupHeader(
                                        title: state.title,
                                        count: allValues.count,
                                        tint: state.tint,
                                        isExpanded: expanded
                                    )
                                } content: {
                                    if expanded.wrappedValue {
                                        ForEach(values) { session in
                                            liveSessionRow(
                                                session,
                                                showsPresence: false,
                                                subagentDisplayDepth: visibleDepths[session.id] ?? 0,
                                                subagentCount: childCounts[session.id] ?? 0,
                                                lineageExpanded: expandedLineages.contains(
                                                    ATMAgentPresenceOrdering.lineageID(session)
                                                )
                                            )
                                        }
                                    }
                                }
                            }
                        }
                    } else {
                        ForEach(sessions.filter { visibleIDs.contains($0.id) }) { session in
                            liveSessionRow(
                                session,
                                showsPresence: true,
                                subagentDisplayDepth: visibleDepths[session.id] ?? 0,
                                subagentCount: childCounts[session.id] ?? 0,
                                lineageExpanded: expandedLineages.contains(
                                    ATMAgentPresenceOrdering.lineageID(session)
                                )
                            )
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

    private func liveSessionRow(
        _ session: ATMLiveSession,
        showsPresence: Bool,
        subagentDisplayDepth: Int,
        subagentCount: Int,
        lineageExpanded: Bool
    ) -> some View {
        Button {
            navigation.selectedAgentID = session.id
            navigation.selectedAgentRunTodoID = nil
            if subagentCount > 0 {
                toggleLineage(ATMAgentPresenceOrdering.lineageID(session))
            }
        } label: {
            DesktopAgentPresenceRow(
                session: session,
                isSelected: navigation.selectedAgentID == session.id,
                showsPresence: showsPresence,
                subagentDisplayDepth: subagentDisplayDepth,
                subagentCount: subagentCount,
                lineageExpanded: lineageExpanded
            )
        }
        .buttonStyle(.atmRow)
        // 行里不再逐张画来源，tooltip 兜住完整来源。
        .help(originLabel(session))
        .atmContentStackRow()
    }

    private func detailIdentity(selectedLiveSession: ATMLiveSession?) -> String {
        switch scope {
        case .live: return selectedLiveSession?.id ?? "empty"
        case .all: return selectedIndexedSessionID ?? "empty"
        }
    }

    private func revealSelectedGroup() {
        revealSelectedLineage()
        guard liveListPresentation == .grouped,
              let session = selectedSession(in: sessions) else { return }
        var set = collapsedGroups
        guard set.remove(session.presenceState.id) != nil else { return }
        collapsedGroupsRaw = set.sorted().joined(separator: ",")
    }

    private func revealSelectedLineage() {
        guard let session = selectedSession(in: sessions), session.isSubagent else { return }
        var set = expandedLineages
        guard set.insert(ATMAgentPresenceOrdering.lineageID(session)).inserted else { return }
        expandedLineagesRaw = set.sorted().joined(separator: ",")
    }

    private func toggleLineage(_ lineage: String) {
        var set = expandedLineages
        if set.contains(lineage) {
            set.remove(lineage)
        } else {
            set.insert(lineage)
        }
        expandedLineagesRaw = set.sorted().joined(separator: ",")
    }

    /// 行里不画来源（单项目单客户端时它是各行唯一不变的东西），完整来源只挂 tooltip。
    private func originLabel(_ session: ATMLiveSession) -> String {
        let client = ATMAgentDisplay.clientName(session)
        let project = ATMAgentDisplay.projectName(session)
        return "\(client) · \(project)"
    }

    private var emptyDetail: some View {
        ATMDetailBodySurface {
            ATMEmptyState(
                icon: "cpu",
                title: "选择一个 Agent 会话",
                detail: "查看它在哪里、正在做什么，以及是否需要你介入。",
                size: .inline,
                minHeight: 180
            )
        }
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
    var showsPresence = false
    var subagentDisplayDepth = 0
    var subagentCount = 0
    var lineageExpanded = false

    var body: some View {
        // Markdown summarisation is deliberately computed once: the optional
        // drives both row existence and content, and some child updates are long.
        let subtitle = session.presenceSubtitle
        return ATMNavigatorRow(isSelected: isSelected) {
            leadingIdentity
        } content: {
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                Text(session.presenceTitle)
                    // 固定字重 —— 见 ATMRowSurface：选中不切字重，否则标题会抖。
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(1)

                if session.bindingTodoID != nil || subtitle != nil {
                    HStack(alignment: .firstTextBaseline, spacing: 5) {
                        if let todoID = session.bindingTodoID {
                            Text(todoID.uppercased())
                                .font(ATMFont.mono(.caption, .medium))
                                .foregroundStyle(ATMTheme.accent)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(ATMTheme.accentFill, in: Capsule())
                        }
                        if let subtitle {
                            if !session.isSubagent {
                                Text("你")
                                    .font(ATMFont.font(.caption, weight: .semibold))
                            }
                            Text(subtitle)
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

    /// Root sessions retain the established 24pt mark. Child rows add one quiet
    /// branch glyph, then a small capped offset for each deeper collaboration
    /// level so siblings remain easy to scan in the narrow navigator.
    private var leadingIdentity: some View {
        HStack(spacing: 3) {
            if session.isSubagent {
                Image(systemName: "arrow.turn.down.right")
                    .font(ATMFont.font(.caption, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary.opacity(0.75))
            }
            ATMAgentMark(agent: session.tool, size: 16)
                .frame(
                    width: ATMContentRowLayout.leadingVisualSize,
                    height: ATMContentRowLayout.leadingVisualSize
                )
        }
        .padding(.leading, CGFloat(max(subagentDisplayDepth - 1, 0)) * 7)
    }

    /// 尾部整块 `fixedSize`：让长标题先被截断，而不是把介入状态和时长挤没。
    private var trailingMeta: some View {
        HStack(spacing: 6) {
            if subagentCount > 0 {
                HStack(spacing: 3) {
                    Text("\(subagentCount) 子")
                    Image(systemName: lineageExpanded ? "chevron.down" : "chevron.right")
                }
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .accessibilityLabel("\(subagentCount) 个子 Agent，\(lineageExpanded ? "已展开" : "已折叠")")
            }
            if showsPresence {
                HStack(spacing: 4) {
                    Circle()
                        .fill(session.presenceState.tint)
                        .frame(width: 6, height: 6)
                    Text(session.presenceState.title)
                }
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(session.presenceState.tint)
            } else if session.presenceState == .attention {
                Text("需要你")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.danger)
            }
            Text(ATMAgentPresenceAge.label(seconds: session.ageSeconds))
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
    }

    /// 执行动态 tab 里默认展开的条数；更早的收进 DisclosureGroup。
    /// CLI 侧 RecentUpdates 也截到这个上限，所以常态下不会触发折叠。
    private static let expandedUpdateLimit = 10

    let session: ATMLiveSession
    let relatedTodo: ATMTodo?
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    let onOpenSession: () -> Void

    @State private var selectedTab: DetailTab = .overview
    @State private var transcriptMode: ATMSessionReadMode = .brief
    @State private var copied = false

    var body: some View {
        ATMDetailScaffold {
            detailHeader
        } tabs: {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    ATMCapsuleTabs(selection: $selectedTab, items: detailTabItems)
                    if selectedTab == .transcript {
                        Spacer(minLength: 12)
                        transcriptReadControls
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                VStack(alignment: .leading, spacing: 8) {
                    ATMCapsuleTabs(selection: $selectedTab, items: detailTabItems)
                    if selectedTab == .transcript {
                        HStack(spacing: 0) {
                            Spacer(minLength: 0)
                            transcriptReadControls
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
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
                        store: store,
                        mode: $transcriptMode,
                        showsReadControls: false
                    )
                }
            }
            .atmAnimatedSwap(selectedTab.rawValue, style: .detail)
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
        [
            (.overview, "概览"),
            (.updates, "执行动态"),
            (.transcript, "对话"),
        ]
    }

    private var transcriptReadControls: some View {
        DesktopSessionReadControls(
            sessionID: session.sessionID,
            store: store,
            mode: $transcriptMode
        )
    }

    private var overviewContent: some View {
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
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var updatesContent: some View {
        VStack(alignment: .leading, spacing: 12) {
            executionUpdates
        }
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity, alignment: .leading)
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

    /// A light timeline keeps consecutive updates in one reading stream while
    /// making their boundaries visible. Cards would incorrectly imply that each
    /// polling update is an independent object.
    private func executionUpdateList(_ updates: [String]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(updates.enumerated()), id: \.offset) { index, update in
                HStack(alignment: .top, spacing: 12) {
                    VStack(spacing: 0) {
                        Circle()
                            .fill(index == 0 ? ATMTheme.accent : ATMTheme.secondary.opacity(0.55))
                            .frame(width: 8, height: 8)
                            .padding(.top, 6)
                        if index < updates.count - 1 {
                            Rectangle()
                                .fill(ATMTheme.border)
                                .frame(width: 1)
                                .frame(maxHeight: .infinity)
                                .padding(.vertical, 5)
                        }
                    }
                    .frame(width: 10)

                    ATMMarkdownContentView(source: update)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.bottom, index < updates.count - 1 ? 20 : 4)
                }
                .fixedSize(horizontal: false, vertical: true)
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
    @State private var transcriptMode: ATMSessionReadMode = .brief

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ATMDetailBodySurface {
                DesktopSessionTranscriptView(
                    sessionID: session.id,
                    agentLabel: ATMAgentDisplay.name(session.agent),
                    store: store,
                    mode: $transcriptMode
                )
            }
        }
        .background(Color.clear)
    }

    private var header: some View {
        ATMDetailHeader(title: session.title, titleLineLimit: 3) {
            HStack(spacing: 7) {
                ATMAgentMark(agent: session.agent, size: 18)
                Text(ATMAgentDisplay.name(session.agent))
                Text("·")
                Label(session.project.isEmpty ? "未归属项目" : session.project, systemImage: "folder")
                Text("·")
                Text(session.shortID)
                    .font(ATMFont.mono(.footnote, .medium))
            }
            .font(ATMFont.font(.footnote, weight: .medium))
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)
        } actions: {
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
        } meta: {
            Label(timeRange, systemImage: "clock")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
        }
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
