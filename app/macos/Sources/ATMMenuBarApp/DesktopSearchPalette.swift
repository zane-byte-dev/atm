import AppKit
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

enum ATMSearchSelection {
    static func movedIndex(current: Int, resultCount: Int, step: Int) -> Int {
        guard resultCount > 0 else { return 0 }
        return min(max(current + step, 0), resultCount - 1)
    }
}

enum ATMSearchResultPolicy {
    /// Keep every domain represented in the compact dropdown. Backends may
    /// return hundreds of literal matches; showing all of them lets one noisy
    /// domain bury the next section even after the strongest hits are ranked.
    static let perSectionLimit = 6

    static func top<Value>(_ values: [Value]) -> [Value] {
        Array(values.prefix(perSectionLimit))
    }
}

/// AppKit owns command dispatch for an actively edited text field on macOS 13.
/// Handling the four search commands here keeps arrow navigation reliable while
/// leaving IME candidate selection and ordinary caret movement untouched.
struct ATMSearchTextField: NSViewRepresentable {
    @Binding var text: String
    @Binding var isFocused: Bool
    var placeholder: String
    var onMove: (Int) -> Void
    var onSubmit: () -> Void
    var onCancel: () -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(
            text: $text,
            isFocused: $isFocused,
            onMove: onMove,
            onSubmit: onSubmit,
            onCancel: onCancel
        )
    }

    func makeNSView(context: Context) -> NSTextField {
        let field = NSTextField()
        field.delegate = context.coordinator
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.font = ATMFont.nsFont(.footnote, weight: .medium)
        field.placeholderString = placeholder
        field.lineBreakMode = .byTruncatingTail
        field.usesSingleLineMode = true
        field.cell?.wraps = false
        field.cell?.isScrollable = true
        field.stringValue = text
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        context.coordinator.text = $text
        context.coordinator.isFocused = $isFocused
        context.coordinator.onMove = onMove
        context.coordinator.onSubmit = onSubmit
        context.coordinator.onCancel = onCancel
        field.placeholderString = placeholder
        if field.stringValue != text {
            // While the field owns the editor, `stringValue` is not what is on
            // screen — the field editor is. Writing only `stringValue` is why the
            // ✕ button used to empty `query` while the typed text stayed visible.
            // Marked text is the one case to leave alone: rewriting the editor
            // mid-composition would discard an in-progress pinyin candidate.
            if let editor = field.currentEditor() {
                if (editor as? NSTextView)?.hasMarkedText() != true {
                    editor.string = text
                    editor.selectedRange = NSRange(location: (text as NSString).length, length: 0)
                }
            } else {
                field.stringValue = text
            }
        }

        if isFocused, field.currentEditor() == nil {
            DispatchQueue.main.async { [weak field, weak coordinator = context.coordinator] in
                guard let field, coordinator?.isFocused.wrappedValue == true else { return }
                field.window?.makeFirstResponder(field)
            }
        } else if !isFocused, field.currentEditor() != nil {
            DispatchQueue.main.async { [weak field] in
                guard let field, field.currentEditor() != nil else { return }
                field.window?.makeFirstResponder(nil)
            }
        }
    }

    @MainActor
    final class Coordinator: NSObject, NSTextFieldDelegate {
        var text: Binding<String>
        var isFocused: Binding<Bool>
        var onMove: (Int) -> Void
        var onSubmit: () -> Void
        var onCancel: () -> Void

        init(
            text: Binding<String>,
            isFocused: Binding<Bool>,
            onMove: @escaping (Int) -> Void,
            onSubmit: @escaping () -> Void,
            onCancel: @escaping () -> Void
        ) {
            self.text = text
            self.isFocused = isFocused
            self.onMove = onMove
            self.onSubmit = onSubmit
            self.onCancel = onCancel
        }

        func controlTextDidBeginEditing(_ notification: Notification) {
            isFocused.wrappedValue = true
        }

        func controlTextDidEndEditing(_ notification: Notification) {
            isFocused.wrappedValue = false
        }

        func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSTextField else { return }
            text.wrappedValue = field.stringValue
        }

        func control(
            _ control: NSControl,
            textView: NSTextView,
            doCommandBy commandSelector: Selector
        ) -> Bool {
            // Return and arrows belong to the input method while pinyin/kana is
            // still marked; consuming them here would make Chinese search unusable.
            guard !textView.hasMarkedText() else { return false }
            switch commandSelector {
            case #selector(NSResponder.moveDown(_:)):
                onMove(1)
            case #selector(NSResponder.moveUp(_:)):
                onMove(-1)
            case #selector(NSResponder.insertNewline(_:)):
                onSubmit()
            case #selector(NSResponder.cancelOperation(_:)):
                onCancel()
            default:
                return false
            }
            return true
        }
    }
}

/// Title-bar global search across ATM work, conversations, knowledge, and
/// shared memory. The field stays in the window chrome and results are drawn as
/// an in-window dropdown, avoiding both the old modal sheet and native popover.
struct DesktopSearchPalette: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var query = ""
    @State private var taskResults: [ATMTodo] = []
    @State private var sessionResults: [ATMSessionSearchHit] = []
    @State private var docResults: [ATMKnowledgeDocumentSummary] = []
    @State private var memoryResults: [ATMMemoryHit] = []
    @State private var isSearching = false
    @State private var selectedSession: ATMSessionSearchHit?
    @State private var selectedResultIndex = 0
    @State private var searchErrorMessage: String?
    @State private var showingResults = false
    @State private var searchFocused = false

    private var trimmedQuery: String {
        query.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var hasResults: Bool {
        totalResultCount > 0
    }

    private var totalResultCount: Int {
        taskResults.count + sessionResults.count + docResults.count + memoryResults.count
    }

    private var resultSectionCount: Int {
        [taskResults.count, sessionResults.count, docResults.count, memoryResults.count]
            .filter { $0 > 0 }
            .count
    }

    private var resultsBodyHeight: CGFloat {
        if trimmedQuery.isEmpty { return 118 }
        if isSearching, !hasResults { return 92 }
        if !hasResults { return 148 }
        let rows = CGFloat(min(totalResultCount, 6)) * 58
        let headers = CGFloat(resultSectionCount) * 28
        let warning: CGFloat = searchErrorMessage == nil ? 0 : 46
        return min(max(rows + headers + warning + 16, 156), 382)
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
        searchField
        .overlay(alignment: .top) {
            if showingResults {
                resultsDropdown
                    .offset(y: 32)
                    .zIndex(2)
                    .transition(
                        .opacity.combined(with: .scale(scale: 0.985, anchor: .top))
                    )
            }
        }
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
        .animation(ATMMotion.disclosure, value: showingResults)
        .onDisappear { searchFocused = false }
    }

    private var searchField: some View {
        HStack(spacing: 7) {
            Image(systemName: isSearching ? "hourglass" : "magnifyingglass")
                .foregroundStyle(ATMTheme.secondary)
            ATMSearchTextField(
                text: $query,
                isFocused: $searchFocused,
                placeholder: "搜索任务、会话、知识与记忆",
                onMove: moveSelection,
                onSubmit: openSelectedResult,
                onCancel: closeResults
            )
            .frame(height: 20)
            if !query.isEmpty {
                ATMIconButton(
                    systemImage: "xmark.circle.fill",
                    help: "清除搜索",
                    chrome: .bare,
                    side: 24,
                    iconTier: .bodyLarge
                ) {
                    query = ""
                    showingResults = true
                    searchFocused = true
                }
            }
            Button {
                showingResults = true
                searchFocused = true
            } label: {
                Text("⌘K")
                    .font(ATMFont.mono(.caption, .medium))
                    .foregroundStyle(ATMTheme.secondary.opacity(0.78))
            }
            .buttonStyle(.plain)
            .keyboardShortcut("k", modifiers: .command)
            .help("全局搜索（⌘K）")
        }
        .foregroundStyle(ATMTheme.secondary)
        .padding(.horizontal, 9)
        .frame(width: 360, height: 26)
        .background(
            ATMTheme.controlFill.opacity(0.72),
            in: RoundedRectangle(cornerRadius: 6, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .stroke(
                    showingResults ? ATMTheme.accent.opacity(0.48) : ATMTheme.border,
                    lineWidth: 1
                )
        }
        .contentShape(Rectangle())
        .simultaneousGesture(TapGesture().onEnded {
            showingResults = true
            searchFocused = true
        })
        .onChange(of: searchFocused) { focused in
            if focused {
                showingResults = true
            } else {
                // Defer until the current click finishes. A result button gets
                // to perform its navigation first; a click elsewhere simply
                // dismisses the dropdown on the next run-loop turn.
                DispatchQueue.main.async {
                    if !searchFocused { showingResults = false }
                }
            }
        }
        .onChange(of: query) { _ in
            if searchFocused { showingResults = true }
        }
    }

    private var resultsDropdown: some View {
        VStack(spacing: 0) {
            content
                .frame(height: resultsBodyHeight)
            Divider()
            HStack(spacing: 16) {
                keyboardHint(keys: "↑ ↓", label: "切换")
                keyboardHint(keys: "↩", label: "打开")
                keyboardHint(keys: "esc", label: "关闭")
                Spacer(minLength: 0)
                if totalResultCount > 0 {
                    Text("共 \(totalResultCount) 项")
                        .font(ATMFont.mono(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            .padding(.horizontal, 12)
            .frame(height: 32)
        }
        .frame(width: 600)
        .background(
            ATMTheme.elevated,
            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(ATMTheme.borderStrong, lineWidth: 1)
        }
        .shadow(color: .black.opacity(0.14), radius: 18, y: 8)
    }

    private func keyboardHint(keys: String, label: String) -> some View {
        HStack(spacing: 5) {
            Text(keys)
                .font(ATMFont.mono(.caption, .semibold))
                .foregroundStyle(ATMTheme.primary)
            Text(label)
                .font(ATMFont.font(.caption))
                .foregroundStyle(ATMTheme.secondary)
        }
    }

    @ViewBuilder
    private var content: some View {
        if trimmedQuery.isEmpty {
            emptyState(icon: "magnifyingglass", title: "搜索 ATM", detail: "输入关键词查找任务、会话、知识与记忆")
        } else if isSearching, !hasResults {
            VStack(spacing: 8) {
                ProgressView()
                    .controlSize(.small)
                Text("正在搜索…")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
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
        taskResults = ATMSearchResultPolicy.top(tasks.value ?? [])
        sessionResults = ATMSearchResultPolicy.top(sessions.value ?? [])
        docResults = ATMSearchResultPolicy.top(docs.value ?? [])
        memoryResults = ATMSearchResultPolicy.top(memories.value ?? [])
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

    private func moveSelection(_ step: Int) {
        guard totalResultCount > 0 else { return }
        showingResults = true
        selectedResultIndex = ATMSearchSelection.movedIndex(
            current: selectedResultIndex,
            resultCount: totalResultCount,
            step: step
        )
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
        closeResults()
    }

    private func openDocument(_ doc: ATMKnowledgeDocumentSummary) {
        navigation.selectedKnowledgeLibraryID = doc.collection
        navigation.locateKnowledgeDocumentID = "document:\(doc.documentID)"
        navigation.section = .knowledge
        closeResults()
    }

    private func openMemory(_ memory: ATMMemoryHit) {
        navigation.selectedKnowledgeLibraryID = ATMKnowledgeLibrary.memoryID
        navigation.locateKnowledgeDocumentID = "memory:\(memory.id)"
        navigation.section = .knowledge
        closeResults()
    }

    private func openSession(_ session: ATMSessionSearchHit) {
        closeResults()
        selectedSession = session
    }

    private func closeResults() {
        showingResults = false
        searchFocused = false
    }

    private func memoryTitle(_ memory: ATMMemoryHit) -> String {
        let firstLine = memory.content.split(whereSeparator: \.isNewline).first.map(String.init) ?? memory.content
        let trimmed = firstLine.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.count <= 52 ? trimmed : String(trimmed.prefix(52)) + "…"
    }
}
