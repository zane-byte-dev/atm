import AppKit
import SwiftUI

struct DesktopAgentsView: View {
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
                                        } label: {
                                            DesktopAgentPresenceRow(
                                                session: session,
                                                isSelected: navigation.selectedAgentID == session.id,
                                                showsOrigin: originLabel(session) != dominantOrigin
                                            )
                                        }
                                        .buttonStyle(.plain)
                                        // 行里不再逐张画来源，tooltip 兜住完整来源。
                                        .help(originLabel(session))
                                        .listRowInsets(EdgeInsets(top: 2, leading: 8, bottom: 2, trailing: 8))
                                        .listRowBackground(Color.clear)
                                    }
                                }
                            } header: {
                                Button {
                                    withAnimation(.easeInOut(duration: 0.15)) {
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

/// 两行卡片：第一行是身份与状态，第二行是上下文，两行都只画各行之间**不一样**的东西。
///
/// 之前是四行，其中三行在同一栏里逐张复读：`Codex Desktop · atm`（栏内恒定，且占着
/// 卡片首行最抢眼的位置）、`未绑定`（默认态，等于把「什么都没发生」高亮成胶囊）、
/// 模型名（同一客户端下几乎恒定，详情页「技术信息」已有）。状态本身也被说了三遍——
/// 分区标题、彩色圆点、`N 分钟活跃` 里的「活跃」二字。分区标题已经承载状态语义，
/// 圆点在同色分区里没有信息，时长因此退回纯时长。
private struct DesktopAgentPresenceRow: View {
    let session: ATMLiveSession
    let isSelected: Bool
    /// 该会话的来源是否偏离列表头写明的默认来源。
    let showsOrigin: Bool

    @State private var isHovered = false

    // 只有 chevron 用得到 hover，行表面的 hover 由 atmRowSurface 自己管。
    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 7) {
                ATMAgentMark(agent: session.tool, size: 16)
                Text(session.presenceTitle)
                    // 固定字重 —— 见 ATMRowSurface：选中不切字重，否则标题会抖。
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(1)
                Spacer(minLength: 6)
                trailingMeta
            }

            if session.latestUserInputBelowTitle != nil || showsOrigin {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    if let input = session.latestUserInputBelowTitle {
                        Text("你")
                            .font(ATMFont.font(.caption, weight: .semibold))
                        Text(input)
                            .font(ATMFont.footnote)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 6)
                    if showsOrigin {
                        Text("\(ATMAgentDisplay.clientName(session)) · \(ATMAgentDisplay.projectName(session))")
                            .font(ATMFont.font(.caption, weight: .medium))
                            .lineLimit(1)
                            .layoutPriority(1)
                    }
                }
                .foregroundStyle(ATMTheme.secondary)
            }
        }
        .atmRowSurface(isSelected: isSelected)
        .onHover { isHovered = $0 }
    }

    /// 尾部整块 `fixedSize`：让长标题先被截断，而不是把时长挤没。chevron 用 opacity
    /// 而不是条件插入，否则悬停时它挤掉的那点宽度会改变标题截断位置，行文字跟着抖一下。
    private var trailingMeta: some View {
        HStack(spacing: 6) {
            if session.presenceState == .attention {
                Text("需要你")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.danger)
            }
            if let todoID = session.bindingTodoID {
                Text(todoID.uppercased())
                    .font(ATMFont.mono(.caption, .medium))
                    .foregroundStyle(ATMTheme.accent)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 2)
                    .background(ATMTheme.accent.opacity(0.08), in: Capsule())
            }
            Text(NumberFormat.age(session.ageSeconds))
                .font(ATMFont.mono(.caption, .medium))
                .foregroundStyle(ATMTheme.secondary)
            Image(systemName: "chevron.right")
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .opacity(isHovered ? 1 : 0)
        }
        .fixedSize()
    }
}

private struct DesktopAgentPresenceDetail: View {
    let session: ATMLiveSession
    let relatedTodo: ATMTodo?
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    let onOpenSession: () -> Void

    @State private var copied = false
    @State private var showingTranscript = false
    @State private var transcript = ""
    @State private var transcriptError: String?
    @State private var isLoadingTranscript = false

    var body: some View {
        VStack(spacing: 0) {
            detailHeader
            Divider()
            detailContent
        }
        .background(ATMTheme.canvas)
        .sheet(isPresented: $showingTranscript) {
            DesktopAgentTranscriptSheet(
                session: session,
                transcript: transcript,
                errorMessage: transcriptError,
                isLoading: isLoadingTranscript
            )
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
            Menu {
                Button {
                    showTranscript()
                } label: {
                    Label("查看完整记录", systemImage: "text.bubble")
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

    private var detailContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if session.presenceState == .attention {
                    attentionBanner
                }
                if let input = session.latestUserInputBelowTitle {
                    latestUserInput(input)
                }
                if let latestResultText = session.latestResultText {
                    latestResult(latestResultText)
                }
                if !session.visibleUpdates.isEmpty {
                    executionUpdates
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

    /// 不叫「最新完成结果」：`latestResult` 是最后一次 final answer，会话往下可能还在跑，
    /// 于是标题写着「完成」、正文的状态标签写着「正在继续处理」，自己打自己。状态也不在
    /// 这里重复——头部的圆点加文字已经说了一遍，需要介入时 attentionBanner 还会说一遍。
    private func latestResult(_ text: String) -> some View {
        detailSection("最新进展") {
            ATMMarkdownContentView(source: text)
        }
    }

    /// 最新一条在最上面且默认只展开它。
    ///
    /// `updates` 从 CLI 出来是按时间正序的（见 parser 里的 `recentUpdates`），全展开时
    /// 得从上往下读三条才知道现在是什么状态，而越靠上的越旧、越没用——三条往往还是同一件
    /// 事的三次重述。逐条那个一模一样的 `text.bubble` 图标也去掉了：三条同图标等于没图标。
    private var executionUpdates: some View {
        let updates = Array(session.visibleUpdates.reversed())
        return detailSection("执行动态") {
            VStack(alignment: .leading, spacing: 12) {
                if let latest = updates.first {
                    ATMMarkdownContentView(source: latest)
                }
                if updates.count > 1 {
                    DisclosureGroup("更早 \(updates.count - 1) 条动态") {
                        VStack(alignment: .leading, spacing: 12) {
                            ForEach(Array(updates.dropFirst().enumerated()), id: \.offset) { _, update in
                                ATMMarkdownContentView(source: update)
                            }
                        }
                        .padding(.top, 10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                }
            }
        }
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
        DisclosureGroup("技术信息") {
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
            .padding(.top, 10)
        }
        .font(ATMFont.footnote)
        .foregroundStyle(ATMTheme.secondary)
        .padding(16)
        .atmWorkspaceCard()
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

private struct DesktopAgentTranscriptSheet: View {
    @Environment(\.dismiss) private var dismiss

    let session: ATMLiveSession
    let transcript: String
    let errorMessage: String?
    let isLoading: Bool

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

            Divider()

            Group {
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
                    }
                    .padding(28)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ScrollView {
                        Text(transcript.isEmpty ? "该会话暂无可展示内容。" : transcript)
                            .font(ATMFont.mono(.body))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(18)
                    }
                }
            }
            .background(ATMTheme.canvas)
        }
        .frame(minWidth: 720, minHeight: 600)
    }
}
