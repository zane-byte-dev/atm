import SwiftUI

private struct ATMSearchOutcome<Value> {
    let value: Value?
    let error: String?
}

enum ATMSearchResultAnchor {
    static func task(_ id: String) -> String { "task:\(id)" }
    static func session(_ id: String) -> String { "session:\(id)" }
    static func document(_ id: String) -> String { "document:\(id)" }
    static func memory(_ id: String) -> String { "memory:\(id)" }
}

/// Spotlight-style global search across ATM work, conversations, knowledge,
/// and shared memory.
struct DesktopSearchPalette: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    @Environment(\.dismiss) private var dismiss

    @State private var query = ""
    @State private var taskResults: [ATMTodo] = []
    @State private var sessionResults: [ATMSessionSearchHit] = []
    @State private var docResults: [ATMKnowledgeDocumentSummary] = []
    @State private var memoryResults: [ATMMemoryHit] = []
    @State private var isSearching = false
    @State private var selectedSession: ATMSessionSearchHit?
    @State private var selectedResultIndex = 0
    @State private var searchErrorMessage: String?
    @FocusState private var searchFocused: Bool

    private var trimmedQuery: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var hasResults: Bool {
        !taskResults.isEmpty || !sessionResults.isEmpty || !docResults.isEmpty || !memoryResults.isEmpty
    }

    private var selectedResultAnchor: String? {
        var index = selectedResultIndex
        if index < taskResults.count {
            return ATMSearchResultAnchor.task(taskResults[index].id)
        }
        index -= taskResults.count
        if index < sessionResults.count {
            return ATMSearchResultAnchor.session(sessionResults[index].shortID)
        }
        index -= sessionResults.count
        if index < docResults.count {
            return ATMSearchResultAnchor.document(docResults[index].documentID)
        }
        index -= docResults.count
        if index < memoryResults.count {
            return ATMSearchResultAnchor.memory(memoryResults[index].id)
        }
        return nil
    }

    var body: some View {
        VStack(spacing: 0) {
            searchField
            Divider()
            content
        }
        .frame(width: 640, height: 520)
        .background(ATMTheme.surface)
        .task(id: query) { await runSearch() }
        .sheet(item: $selectedSession) { session in
            ATMSessionTranscriptSheet(
                agent: session.agent,
                title: nil,
                shortID: session.shortID,
                project: session.project,
                store: store
            )
        }
        .onMoveCommand(perform: moveSelection)
        .onExitCommand { dismiss() }
    }

    private var searchField: some View {
        HStack(spacing: 9) {
            Image(systemName: isSearching ? "hourglass" : "magnifyingglass")
                .foregroundStyle(ATMTheme.secondary)
            TextField("搜索任务、会话、知识与记忆", text: $query)
                .textFieldStyle(.plain)
                .font(ATMFont.title3)
                .focused($searchFocused)
                .onSubmit(openSelectedResult)
            if !query.isEmpty {
                ATMIconButton(
                    systemImage: "xmark.circle.fill",
                    help: "清除搜索",
                    chrome: .bare,
                    side: 24,
                    iconTier: .bodyLarge
                ) { query = "" }
            }
            Text("esc")
                .font(ATMFont.mono(.footnote, .medium))
                .foregroundStyle(ATMTheme.secondary)
                .padding(.horizontal, 5)
                .padding(.vertical, 2)
                .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 4))
                .onTapGesture { dismiss() }
        }
        .padding(.horizontal, 16)
        .frame(height: 58)
        .onAppear { searchFocused = true }
    }

    @ViewBuilder
    private var content: some View {
        if trimmedQuery.isEmpty {
            emptyState(icon: "magnifyingglass", title: "搜索 ATM", detail: "输入关键词查找任务、会话、知识与记忆")
        } else if let searchErrorMessage, !hasResults && !isSearching {
            searchFailureState(searchErrorMessage)
        } else if !hasResults && !isSearching {
            emptyState(icon: "questionmark.circle", title: "没有匹配结果", detail: "没有足够相关的内容，试试更短或更具体的关键词")
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 2) {
                        if let searchErrorMessage {
                            searchWarning(searchErrorMessage)
                        }
                        if !taskResults.isEmpty {
                            sectionHeader("任务", count: taskResults.count)
                            ForEach(taskResults) { todo in
                                let index = taskResults.firstIndex(of: todo) ?? 0
                                resultRow(
                                    icon: "checklist",
                                    title: todo.title,
                                    subtitle: todo.description,
                                    trailing: todo.id.uppercased(),
                                    tag: todo.project,
                                    isSelected: selectedResultIndex == index
                                ) { openTask(todo) }
                                .id(ATMSearchResultAnchor.task(todo.id))
                            }
                        }
                        if !sessionResults.isEmpty {
                            sectionHeader("会话", count: sessionResults.count)
                            ForEach(sessionResults) { session in
                                let index = taskResults.count + (sessionResults.firstIndex(of: session) ?? 0)
                                resultRow(
                                    icon: ATMAgentDisplay.systemImage(session.agent),
                                    title: "\(ATMAgentDisplay.name(session.agent)) · \(session.project)",
                                    subtitle: session.content,
                                    trailing: session.shortID,
                                    tag: session.createdAt,
                                    isSelected: selectedResultIndex == index
                                ) { openSession(session) }
                                .id(ATMSearchResultAnchor.session(session.shortID))
                            }
                        }
                        if !docResults.isEmpty {
                            sectionHeader("知识", count: docResults.count)
                            ForEach(docResults) { doc in
                                let index = taskResults.count + sessionResults.count + (docResults.firstIndex(of: doc) ?? 0)
                                resultRow(
                                    icon: "doc.text",
                                    title: doc.title,
                                    subtitle: doc.snippet,
                                    trailing: doc.updatedAt.map(KnowledgeDateFormatter.short),
                                    tag: doc.collection,
                                    isSelected: selectedResultIndex == index
                                ) { openDocument(doc) }
                                .id(ATMSearchResultAnchor.document(doc.documentID))
                            }
                        }
                        if !memoryResults.isEmpty {
                            sectionHeader("共享记忆", count: memoryResults.count)
                            ForEach(memoryResults) { memory in
                                let index = taskResults.count + sessionResults.count + docResults.count
                                    + (memoryResults.firstIndex(of: memory) ?? 0)
                                resultRow(
                                    icon: "brain.head.profile",
                                    title: memoryTitle(memory),
                                    subtitle: memory.content,
                                    trailing: KnowledgeDateFormatter.short(memory.createdAt),
                                    tag: memory.scope,
                                    isSelected: selectedResultIndex == index
                                ) { openMemory(memory) }
                                .id(ATMSearchResultAnchor.memory(memory.id))
                            }
                        }
                    }
                    .padding(8)
                }
                .onChange(of: selectedResultIndex) { _ in
                    guard let anchor = selectedResultAnchor else { return }
                    withAnimation(.easeOut(duration: 0.12)) {
                        proxy.scrollTo(anchor, anchor: .center)
                    }
                }
            }
        }
    }

    private func sectionHeader(_ title: String, count: Int) -> some View {
        HStack(spacing: 6) {
            Text(title)
                .font(ATMFont.font(.footnote, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
            Text("\(count)")
                .font(ATMFont.mono(.caption, .semibold))
                .foregroundStyle(ATMTheme.secondary)
        }
        .padding(.horizontal, 8)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    private func resultRow(
        icon: String,
        title: String,
        subtitle: String?,
        trailing: String?,
        tag: String?,
        isSelected: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: icon)
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.accent)
                    .frame(width: 18, height: 20)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title)
                        .font(ATMFont.font(.body, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                        .lineLimit(1)
                    if let subtitle, !subtitle.isEmpty {
                        Text(subtitle)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .lineLimit(1)
                    }
                    if let tag, !tag.isEmpty {
                        Text(tag)
                            .font(ATMFont.mono(.caption))
                            .foregroundStyle(ATMTheme.accent)
                    }
                }
                Spacer(minLength: 4)
                if let trailing, !trailing.isEmpty {
                    Text(trailing)
                        .font(ATMFont.mono(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            .atmRowSurface(isSelected: isSelected)
        }
        .buttonStyle(.plain)
    }

    private func emptyState(icon: String, title: String, detail: String) -> some View {
        VStack(spacing: 8) {
            Image(systemName: icon)
                .font(ATMFont.font(.metric, weight: .light))
                .foregroundStyle(ATMTheme.secondary)
            Text(title)
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
            Text(detail)
                .font(ATMFont.body)
                .foregroundStyle(ATMTheme.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func searchFailureState(_ detail: String) -> some View {
        VStack(spacing: 9) {
            Image(systemName: "exclamationmark.triangle")
                .font(ATMFont.font(.metric, weight: .light))
                .foregroundStyle(ATMTheme.warning)
            Text("部分搜索服务不可用")
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
            Text(detail)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .multilineTextAlignment(.center)
                .lineLimit(3)
            Button("重试") {
                Task { await runSearch() }
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func searchWarning(_ detail: String) -> some View {
        Label(detail, systemImage: "exclamationmark.triangle.fill")
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.warning)
            .lineLimit(2)
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ATMTheme.warningFill, in: RoundedRectangle(cornerRadius: 8))
    }

    @MainActor
    private func runSearch() async {
        let needle = trimmedQuery
        guard !needle.isEmpty else {
            taskResults = []
            sessionResults = []
            docResults = []
            memoryResults = []
            searchErrorMessage = nil
            isSearching = false
            return
        }

        isSearching = true
        taskResults = []
        sessionResults = []
        docResults = []
        memoryResults = []
        searchErrorMessage = nil
        selectedResultIndex = 0
        do {
            try await Task.sleep(nanoseconds: 200_000_000)
        } catch {
            return
        }
        guard !Task.isCancelled else { return }

        async let tasksRequest = searchOutcome { try await store.searchTodos(needle) }
        async let sessionsRequest = searchOutcome { try await store.searchSessions(needle) }
        async let docsRequest = searchOutcome { try await store.searchKnowledge(needle, status: "active") }
        async let memoriesRequest = searchOutcome { try await store.memories(query: needle) }
        let tasks = await tasksRequest
        let sessions = await sessionsRequest
        let docs = await docsRequest
        let memories = await memoriesRequest
        guard !Task.isCancelled, trimmedQuery == needle else { return }
        taskResults = tasks.value ?? []
        sessionResults = sessions.value ?? []
        docResults = docs.value ?? []
        memoryResults = memories.value ?? []
        let errorMessages = [
            tasks.error.map { "任务：\($0)" },
            sessions.error.map { "会话：\($0)" },
            docs.error.map { "知识：\($0)" },
            memories.error.map { "记忆：\($0)" },
        ]
        .compactMap { $0 }
        searchErrorMessage = errorMessages.isEmpty ? nil : errorMessages.joined(separator: "；")
        selectedResultIndex = 0
        isSearching = false
    }

    private func searchOutcome<Value>(
        _ operation: () async throws -> Value
    ) async -> ATMSearchOutcome<Value> {
        do {
            return ATMSearchOutcome(value: try await operation(), error: nil)
        } catch {
            return ATMSearchOutcome(
                value: nil,
                error: ATMErrorText.compact(error.localizedDescription, limit: 160)
            )
        }
    }

    private func moveSelection(_ direction: MoveCommandDirection) {
        let count = taskResults.count + sessionResults.count + docResults.count + memoryResults.count
        guard count > 0 else { return }
        switch direction {
        case .down:
            selectedResultIndex = min(selectedResultIndex + 1, count - 1)
        case .up:
            selectedResultIndex = max(selectedResultIndex - 1, 0)
        default:
            break
        }
    }

    private func openSelectedResult() {
        var index = selectedResultIndex
        if index < taskResults.count {
            openTask(taskResults[index])
            return
        }
        index -= taskResults.count
        if index < sessionResults.count {
            openSession(sessionResults[index])
            return
        }
        index -= sessionResults.count
        if index < docResults.count {
            openDocument(docResults[index])
            return
        }
        index -= docResults.count
        if index < memoryResults.count {
            openMemory(memoryResults[index])
        }
    }

    private func openTask(_ todo: ATMTodo) {
        navigation.selectedTodoID = todo.id
        navigation.section = .tasks
        dismiss()
    }

    private func openDocument(_ doc: ATMKnowledgeDocumentSummary) {
        navigation.selectedKnowledgeLibraryID = doc.collection
        navigation.locateKnowledgeDocumentID = "document:\(doc.documentID)"
        navigation.section = .knowledge
        dismiss()
    }

    private func openMemory(_ memory: ATMMemoryHit) {
        navigation.selectedKnowledgeLibraryID = ATMKnowledgeLibrary.memoryID
        navigation.locateKnowledgeDocumentID = "memory:\(memory.id)"
        navigation.section = .knowledge
        dismiss()
    }

    private func openSession(_ session: ATMSessionSearchHit) {
        selectedSession = session
    }

    private func memoryTitle(_ memory: ATMMemoryHit) -> String {
        let firstLine = memory.content.split(whereSeparator: \.isNewline).first.map(String.init) ?? memory.content
        let trimmed = firstLine.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.count <= 52 ? trimmed : String(trimmed.prefix(52)) + "…"
    }
}
