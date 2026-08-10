import AppKit
import SwiftUI

struct DesktopAgentsView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var launchError: String?
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
        store.snapshot.liveStatus.sessions
            .filter { $0.activityState != "unobserved" }
            .sorted {
                if $0.presenceState != $1.presenceState {
                    return presenceOrder($0.presenceState) < presenceOrder($1.presenceState)
                }
                if $0.ageSeconds != $1.ageSeconds { return $0.ageSeconds < $1.ageSeconds }
                return $0.id < $1.id
            }
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
            defaultWidth: 330,
            minWidth: 260,
            maxWidth: 420,
            detailMinWidth: 440
        ) {
            agentList
        } detail: {
            Group {
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
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .atmAnimatedSwap(selectedSession?.id ?? "empty", style: .detail)
        }
        .onAppear {
            selectFirstIfNeeded()
            revealSelectedGroup()
            store.startLiveStatusPolling()
        }
        .onDisappear { store.stopLiveStatusPolling() }
        .onChange(of: sessions.map(\.id)) { _ in selectFirstIfNeeded() }
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
        VStack(spacing: 0) {
            ATMDrawerHeader(
                title: "Agent",
                count: sessions.count
            ) {
                Circle()
                    .fill(sessions.contains(where: { $0.presenceState == .attention }) ? ATMTheme.warning : ATMTheme.success)
                    .frame(width: 8, height: 8)
                    .help(activeAgentSummary)
            }

            if sessions.isEmpty {
                VStack(spacing: 9) {
                    Image(systemName: "cpu")
                        .font(ATMFont.font(.display, weight: .light))
                    Text("当前没有活跃 Agent")
                        .font(ATMFont.font(.bodyLarge, weight: .medium))
                    Text("ATM 会在检测到新的 Agent 会话活动后自动显示。")
                        .font(ATMFont.footnote)
                        .multilineTextAlignment(.center)
                }
                .foregroundStyle(ATMTheme.secondary)
                .padding(28)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List {
                    ForEach(ATMAgentPresenceState.allCases) { state in
                        let values = sessions.filter { $0.presenceState == state }
                        if !values.isEmpty {
                            let expanded = expandedBinding(for: state)
                            Section {
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
                                        .buttonStyle(.plain)
                                        // 行里不再逐张画来源，tooltip 兜住完整来源。
                                        .help(originLabel(session))
                                        .atmContentListRow()
                                    }
                                }
                            } header: {
                                Button {
                                    withAnimation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion)) {
                                        expanded.wrappedValue.toggle()
                                    }
                                } label: {
                                    HStack {
                                        ATMDrawerDisclosureLabel(
                                            title: state.title,
                                            count: values.count,
                                            tint: state.tint,
                                            isExpanded: expanded.wrappedValue
                                        )
                                        Spacer()
                                    }
                                    .contentShape(Rectangle())
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
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
        .background(ATMTheme.listPane)
    }

    private func revealSelectedGroup() {
        guard let session = selectedSession else { return }
        var set = collapsedGroups
        guard set.remove(session.presenceState.id) != nil else { return }
        collapsedGroupsRaw = set.sorted().joined(separator: ",")
    }

    private var activeAgentSummary: String {
        let active = sessions.filter(\.isCurrentlyActive)
        var parts = [
            active.isEmpty
                ? "最近 10 分钟有 \(sessions.count) 个可见会话"
                : "\(active.count) 个会话正在活跃"
        ]
        if let origin = dominantOrigin {
            parts.append(isOriginUniform ? origin : "\(origin) 等")
        }
        return parts.joined(separator: " · ")
    }

    /// 栏内最常见的来源。这一句是列表的「默认来源」，行里只画偏离它的那些——
    /// 单项目单客户端时（最常见的情况）`Codex Desktop · atm` 本来会在每张卡的
    /// 首行、最抢眼的位置复读一遍，而它恰恰是各行之间唯一不变的东西。
    private var dominantOrigin: String? {
        var counts: [String: Int] = [:]
        for session in sessions {
            counts[originLabel(session), default: 0] += 1
        }
        // 计数相同时按字典序定序，免得来源标签随轮询顺序跳动。
        return counts.max { lhs, rhs in
            lhs.value == rhs.value ? lhs.key > rhs.key : lhs.value < rhs.value
        }?.key
    }

    private var isOriginUniform: Bool {
        Set(sessions.map(originLabel)).count <= 1
    }

    private func originLabel(_ session: ATMLiveSession) -> String {
        let client = ATMAgentDisplay.clientName(session)
        let project = ATMAgentDisplay.projectName(session)
        return "\(client) · \(project)"
    }

    private var emptyDetail: some View {
        VStack(spacing: 10) {
            Image(systemName: "cpu")
                .font(ATMFont.font(.display, weight: .light))
                .foregroundStyle(ATMTheme.secondary)
            Text("选择一个 Agent 会话")
                .font(ATMFont.font(.title3, weight: .semibold))
            Text("查看它在哪里、正在做什么，以及是否需要你介入。")
                .font(ATMFont.body)
                .foregroundStyle(ATMTheme.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
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

    private func presenceOrder(_ state: ATMAgentPresenceState) -> Int {
        switch state {
        case .attention: return 0
        case .active: return 1
        case .recent: return 2
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
        VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
            HStack(spacing: ATMContentRowLayout.leadingSpacing) {
                ATMAgentMark(agent: session.tool, size: 16)
                    .frame(
                        width: ATMContentRowLayout.leadingVisualSize,
                        height: ATMContentRowLayout.leadingVisualSize
                    )
                Text(session.presenceTitle)
                    // 固定字重 —— 见 ATMRowSurface：选中不切字重，否则标题会抖。
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(1)
                Spacer(minLength: 6)
                trailingMeta
            }

            if session.bindingTodoID != nil || session.latestUserInputBelowTitle != nil {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    if let todoID = session.bindingTodoID {
                        Text(todoID.uppercased())
                            .font(ATMFont.mono(.caption, .medium))
                            .foregroundStyle(ATMTheme.accent)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 2)
                            .background(ATMTheme.accent.opacity(0.08), in: Capsule())
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
        .atmRowSurface(isSelected: isSelected)
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
    @State private var showingTranscript = false
    @State private var transcript = ""
    @State private var transcriptError: String?
    @State private var isLoadingTranscript = false
    @State private var showingInterruptConfirmation = false

    var body: some View {
        VStack(spacing: 0) {
            detailHeader
            detailTabs
            Group {
                switch selectedTab {
                case .overview:
                    overviewContent
                case .updates:
                    updatesContent
                case .logs:
                    fullLogsContent
                }
            }
            .atmAnimatedSwap(selectedTab.rawValue, style: .detail)
        }
        .background(ATMTheme.canvas)
        .sheet(isPresented: $showingTranscript) {
            DesktopAgentTranscriptSheet(
                session: session,
                transcript: transcript,
                errorMessage: transcriptError,
                isLoading: isLoadingTranscript,
                onRetry: loadTranscript
            )
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
        VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    headerIdentity
                    Spacer(minLength: 8)
                    headerActions
                }
                VStack(alignment: .leading, spacing: 9) {
                    headerIdentity
                    headerActions
                }
            }

            Text(session.presenceTitle)
                .font(ATMFont.font(.title2, weight: .semibold))
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 6) {
                Circle()
                    .fill(session.presenceState.tint)
                    .frame(width: 7, height: 7)
                Text(session.presenceState == .attention ? "需要你" : activityLabel)
                    .font(ATMFont.font(.footnote, weight: .medium))
                    .foregroundStyle(session.presenceState.tint)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface)
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
                    showTranscript()
                } label: {
                    Label("查看完整动态", systemImage: "rectangle.on.rectangle")
                }
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

    /// 分页样式沿用任务详情 / 收集详情（见 DesktopTodoDetail.detailTabs）。
    private var detailTabs: some View {
        HStack(spacing: 0) {
            detailTabButton(.overview, title: "概览", icon: "doc.text")
            detailTabButton(.updates, title: "执行动态", icon: "clock.arrow.circlepath")
            // Only a session that is itself a dispatched run has a raw log.
            if taskRun != nil {
                detailTabButton(.logs, title: "全部日志", icon: "terminal")
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
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
                .padding(.horizontal, 12)
                .frame(height: 46)
                .overlay(alignment: .bottom) {
                    Capsule()
                        .fill(selected ? ATMTheme.accent : Color.clear)
                        .frame(height: 2)
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
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

    private func showTranscript() {
        showingTranscript = true
        loadTranscript()
    }

    private func loadTranscript() {
        isLoadingTranscript = true
        transcript = ""
        transcriptError = nil
        Task {
            do {
                transcript = try await store.sessionTranscript(session.sessionID)
            } catch {
                transcriptError = error.localizedDescription
            }
            isLoadingTranscript = false
        }
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
/// background rows or the always-on notch.
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

private struct DesktopAgentTranscriptSheet: View {
    @Environment(\.dismiss) private var dismiss

    let session: ATMLiveSession
    let transcript: String
    let errorMessage: String?
    let isLoading: Bool
    let onRetry: () -> Void

    @State private var selectedTab: ATMAgentActivitySheetTab = .updates

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                ATMAgentMark(agent: session.tool, size: 22)
                VStack(alignment: .leading, spacing: 3) {
                    Text("\(ATMAgentDisplay.name(session.tool)) · \(session.project)")
                        .font(ATMFont.font(.title3, weight: .semibold))
                    Text(session.sessionID)
                        .font(ATMFont.mono(.footnote))
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("关闭") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(16)
            .background(ATMTheme.surface)

            activityTabs

            Group {
                switch selectedTab {
                case .updates:
                    fullActivityContent
                case .transcript:
                    transcriptContent
                }
            }
            .atmAnimatedSwap(selectedTab.rawValue, style: .detail)
            .background(ATMTheme.canvas)
        }
        .frame(minWidth: 760, minHeight: 620)
    }

    private var activityTabs: some View {
        HStack(spacing: 0) {
            ForEach(ATMAgentActivitySheetTab.allCases) { tab in
                let selected = selectedTab == tab
                Button {
                    selectedTab = tab
                } label: {
                    Label(tab.title, systemImage: tab.systemImage)
                        .font(ATMFont.font(.body, weight: .semibold))
                        .foregroundStyle(selected ? ATMTheme.primary : ATMTheme.secondary)
                        .padding(.horizontal, 14)
                        .frame(height: 46)
                        .overlay(alignment: .bottom) {
                            Capsule()
                                .fill(selected ? ATMTheme.accent : Color.clear)
                                .frame(height: 2)
                        }
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .frame(height: 46)
        .background(ATMTheme.canvas)
    }

    private var fullActivityContent: some View {
        let updates = Array(session.visibleUpdates.reversed())
        return Group {
            if updates.isEmpty {
                VStack(spacing: 10) {
                    Image(systemName: "clock.arrow.circlepath")
                        .font(ATMFont.font(.display, weight: .light))
                    Text("暂无执行动态")
                        .font(ATMFont.font(.title3, weight: .semibold))
                    Text("Agent 输出阶段性进展时，会按时间出现在这里。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 20) {
                        ForEach(Array(updates.enumerated()), id: \.offset) { _, update in
                            ATMMarkdownContentView(source: update)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                    .padding(20)
                    .frame(maxWidth: 900, alignment: .leading)
                    .frame(maxWidth: .infinity, alignment: .topLeading)
                }
            }
        }
    }

    @ViewBuilder
    private var transcriptContent: some View {
        if isLoading {
            VStack(spacing: 10) {
                ProgressView()
                Text("正在读取完整会话")
                    .font(ATMFont.body)
                    .foregroundStyle(ATMTheme.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let errorMessage {
            VStack(spacing: 10) {
                Image(systemName: "exclamationmark.triangle")
                    .font(ATMFont.font(.display, weight: .light))
                Text("无法读取会话")
                    .font(ATMFont.font(.title3, weight: .semibold))
                Text(errorMessage)
                    .font(ATMFont.body)
                    .foregroundStyle(ATMTheme.secondary)
                    .multilineTextAlignment(.center)
                Button("重试", action: onRetry)
                    .buttonStyle(.borderedProminent)
            }
            .padding(28)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ATMTranscriptTextView(
                text: transcript.isEmpty ? "该会话暂无可展示内容。" : transcript,
                accessibilityLabel: "Agent 完整对话"
            )
        }
    }
}
