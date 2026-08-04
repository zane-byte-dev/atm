import AppKit
import SwiftUI

struct DesktopAgentsView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var launchError: String?

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
        HSplitView {
            agentList
                .frame(minWidth: 285, idealWidth: 325, maxWidth: 420)

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
            .frame(minWidth: 440, maxWidth: .infinity, maxHeight: .infinity)
        }
        .onAppear {
            selectFirstIfNeeded()
            store.startLiveStatusPolling()
        }
        .onDisappear { store.stopLiveStatusPolling() }
        .onChange(of: sessions.map(\.id)) { _ in selectFirstIfNeeded() }
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
            VStack(alignment: .leading, spacing: 5) {
                HStack(alignment: .firstTextBaseline) {
                    HStack(spacing: 8) {
                        Text("活跃 Agent")
                            .font(ATMFont.font(.title2, weight: .semibold))
                        Text("\(sessions.filter(\.isCurrentlyActive).count)")
                            .font(ATMFont.mono(.footnote, .semibold))
                            .foregroundStyle(ATMTheme.success)
                    }
                    Spacer()
                    if !store.snapshot.liveStatus.time.isEmpty {
                        Text(store.snapshot.liveStatus.time)
                            .font(ATMFont.mono(.caption, .medium))
                            .foregroundStyle(ATMTheme.secondary)
                    }
                }
                Text(activeAgentSummary)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
            }
            .padding(.horizontal, 14)
            .frame(height: 72)

            Divider()

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
                            Section {
                                ForEach(values) { session in
                                    Button {
                                        navigation.selectedAgentID = session.id
                                    } label: {
                                        DesktopAgentPresenceRow(
                                            session: session,
                                            isSelected: navigation.selectedAgentID == session.id
                                        )
                                    }
                                    .buttonStyle(.plain)
                                    .help("选择 Agent 会话")
                                    .listRowInsets(EdgeInsets(top: 2, leading: 8, bottom: 2, trailing: 8))
                                    .listRowBackground(Color.clear)
                                }
                            } header: {
                                HStack {
                                    Text(state.title)
                                    Spacer()
                                    Text("\(values.count)")
                                        .font(ATMFont.mono(.caption, .semibold))
                                }
                            }
                        }
                    }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
            }

            if unobservedBindingCount > 0 {
                Divider()
                Text("另有 \(unobservedBindingCount) 条 binding 暂无实时活动，不计入活跃 Agent")
                    .font(ATMFont.font(.caption, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                    .padding(.horizontal, 14)
                    .frame(maxWidth: .infinity, minHeight: 34, alignment: .leading)
            }
        }
        .background(ATMTheme.surface)
    }

    private var activeAgentSummary: String {
        let active = sessions.filter(\.isCurrentlyActive)
        guard !active.isEmpty else { return "最近 10 分钟有 \(sessions.count) 个可见会话" }

        var counts: [String: Int] = [:]
        for session in active {
            counts[ATMAgentDisplay.name(session.tool), default: 0] += 1
        }
        let clients = counts.keys.sorted().map { "\($0) \(counts[$0] ?? 0)" }
        return "\(active.count) 个会话正在活跃 · " + clients.joined(separator: " · ")
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

private struct DesktopAgentPresenceRow: View {
    let session: ATMLiveSession
    let isSelected: Bool

    @State private var isHovered = false

    // 只有 chevron 用得到 hover，行表面的 hover 由 atmRowSurface 自己管。
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 7) {
                ATMAgentMark(agent: session.tool, size: 16)
                Circle()
                    .fill(presenceColor)
                    .frame(width: 7, height: 7)
                Text("\(clientDisplayName) · \(displayProject)")
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                Spacer(minLength: 4)
                if session.presenceState == .attention {
                    Text("需要你")
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(ATMTheme.danger)
                }
                if isHovered {
                    Image(systemName: "chevron.right")
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }

            Text(session.presenceTitle)
                // 固定字重 —— 见 ATMRowSurface：选中不切字重，否则标题会抖。
                .font(ATMFont.font(.body, weight: .medium))
                .foregroundStyle(ATMTheme.primary)
                .lineLimit(1)

            if let input = session.latestUserInputBelowTitle {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text("你")
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                    Text(input)
                        .font(ATMFont.font(.footnote))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                }
            }

            HStack(spacing: 6) {
                statusChip(presenceText, color: presenceColor)
                if let todoID = session.bindingTodoID {
                    statusChip(todoID.uppercased(), color: ATMTheme.accent)
                } else {
                    statusChip("未绑定", color: ATMTheme.secondary)
                }
                Spacer(minLength: 4)
                if let model = nonEmpty(session.model) {
                    Text(model)
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                }
            }
        }
        .atmRowSurface(isSelected: isSelected)
        .onHover { isHovered = $0 }
    }

    private var displayProject: String {
        nonEmpty(session.project) ?? "未知项目"
    }

    private var clientDisplayName: String {
        nonEmpty(session.client) ?? ATMAgentDisplay.name(session.tool)
    }

    private var presenceText: String {
        if session.presenceState == .attention { return "等待介入" }
        if session.isCurrentlyActive { return NumberFormat.age(session.ageSeconds) }
        return "\(NumberFormat.age(session.ageSeconds))活跃"
    }

    private func statusChip(_ text: String, color: Color) -> some View {
        Text(text)
            .font(ATMFont.mono(.caption, .medium))
            .foregroundStyle(color)
            .lineLimit(1)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .background(color.opacity(0.08), in: Capsule())
    }

    private func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    private var presenceColor: Color {
        switch session.presenceState {
        case .attention: return ATMTheme.danger
        case .active: return ATMTheme.success
        case .recent: return ATMTheme.secondary
        }
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
                    .fill(presenceColor)
                    .frame(width: 7, height: 7)
                Text(session.presenceState == .attention ? "需要你" : activityLabel)
                    .font(ATMFont.font(.footnote, weight: .medium))
                    .foregroundStyle(presenceColor)
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
            Text(nonEmpty(session.client) ?? ATMAgentDisplay.name(session.tool))
            Text("·")
            Label(nonEmpty(session.project) ?? "未知项目", systemImage: "folder")
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
            VStack(alignment: .leading, spacing: 0) {
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
            .frame(maxWidth: 760, alignment: .leading)
            .padding(.horizontal, 24)
            .padding(.bottom, 24)
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
        .padding(.vertical, 15)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Divider() }
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

    private func latestResult(_ text: String) -> some View {
        detailSection("最新完成结果") {
            VStack(alignment: .leading, spacing: 12) {
                Label(resultStateLabel, systemImage: resultStateIcon)
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .foregroundStyle(presenceColor)
                ATMMarkdownContentView(source: text)
            }
        }
    }

    private var executionUpdates: some View {
        detailSection("执行动态") {
            VStack(alignment: .leading, spacing: 13) {
                ForEach(Array(session.visibleUpdates.enumerated()), id: \.offset) { index, update in
                    HStack(alignment: .top, spacing: 10) {
                        Image(systemName: "text.bubble")
                            .font(ATMFont.font(.footnote, weight: .semibold))
                            .foregroundStyle(ATMTheme.accent)
                            .frame(width: 16)
                        ATMMarkdownContentView(source: update)
                    }
                    if index < session.visibleUpdates.count - 1 {
                        Divider()
                            .padding(.leading, 26)
                    }
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
                infoRow("来源", nonEmpty(session.client) ?? ATMAgentDisplay.name(session.tool))
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
        .padding(.vertical, 16)
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

    private var resultStateLabel: String {
        switch session.presenceState {
        case .attention: return "等待你的输入"
        case .active: return "Agent 正在继续处理"
        case .recent: return "最近完成"
        }
    }

    private var resultStateIcon: String {
        switch session.presenceState {
        case .attention: return "person.crop.circle.badge.exclamationmark"
        case .active: return "waveform.path"
        case .recent: return "checkmark.circle"
        }
    }

    private var presenceColor: Color {
        switch session.presenceState {
        case .attention: return ATMTheme.danger
        case .active: return ATMTheme.success
        case .recent: return ATMTheme.secondary
        }
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
        .padding(.vertical, 18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Divider() }
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
