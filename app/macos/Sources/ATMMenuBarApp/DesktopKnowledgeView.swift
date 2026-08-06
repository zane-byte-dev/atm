import AppKit
import SwiftUI
import UniformTypeIdentifiers

struct DesktopKnowledgeView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var items: [KnowledgeListItem] = []
    @State private var selectedItemID: String?
    @State private var document: ATMKnowledgeDocument?
    @State private var isListLoading = false
    @State private var isDetailLoading = false
    @State private var listError: String?
    @State private var detailError: String?
    @State private var refreshGeneration = 0
    @State private var showingInfo = false
    @State private var copiedIdentifier: String?
    @State private var editingItemID: String?
    @State private var editContent = ""
    @State private var originalEditContent = ""
    @State private var editorMode = KnowledgeEditorMode.edit
    @State private var isSaving = false
    @State private var editError: String?
    @State private var showingDiscardAlert = false
    @State private var pendingSelectionID: String?
    @State private var showingArchived = false
    @State private var showingCreateSheet = false
    @State private var showingImporter = false
    @State private var showingMetadataSheet = false
    @State private var showingGovernanceSheet = false
    @State private var showingArchiveConfirmation = false
    @State private var deleteSummary: ATMKnowledgeDocumentSummary?
    @State private var isImporting = false
    @State private var operationError: KnowledgeOperationError?
    @State private var renameSummary: ATMKnowledgeDocumentSummary?
    @State private var renameText = ""
    @State private var feedbackDraft: KnowledgeFeedbackDraft?
    @State private var feedbackStatus: String?
    @State private var isFeedbackSaving = false
    @FocusState private var editorFocused: Bool

    private var selectedLibraryID: String {
        navigation.selectedKnowledgeLibraryID ?? "atm"
    }

    private var selectedLibrary: ATMKnowledgeCollection? {
        store.knowledgeCollections.first { $0.id == selectedLibraryID }
    }

    private var libraryTitle: String {
        if showingArchived { return "\(selectedLibrary?.name ?? selectedLibraryID) · 归档" }
        if selectedLibraryID == ATMKnowledgeLibrary.memoryID { return "共享记忆" }
        return selectedLibrary?.name ?? selectedLibraryID
    }

    private var selectedItem: KnowledgeListItem? {
        guard let selectedItemID else { return nil }
        return items.first { $0.id == selectedItemID }
    }

    private var isEditing: Bool { editingItemID != nil }
    private var hasUnsavedChanges: Bool { editContent != originalEditContent }

    var body: some View {
        HSplitView {
            itemList
                .frame(minWidth: 260, idealWidth: 316, maxWidth: 410)

            detailPane
                .frame(minWidth: 400, maxWidth: .infinity, maxHeight: .infinity)
                .background(ATMTheme.canvas)
        }
        .task {
            if store.knowledgeCollections.isEmpty {
                store.refreshKnowledgeCatalog()
            }
        }
        .task(id: KnowledgeLoadKey(
            libraryID: selectedLibraryID,
            generation: refreshGeneration,
            showingArchived: showingArchived
        )) {
            await loadItems()
        }
        .task(id: selectedItemID) {
            await loadSelectedDocument()
        }
        .onChange(of: navigation.locateKnowledgeDocumentID) { target in
            guard let target else { return }
            showingArchived = false
            selectedItemID = target
            navigation.locateKnowledgeDocumentID = nil
        }
        .onAppear {
            guard let target = navigation.locateKnowledgeDocumentID else { return }
            showingArchived = false
            selectedItemID = target
            navigation.locateKnowledgeDocumentID = nil
        }
        .alert("放弃未保存的修改？", isPresented: $showingDiscardAlert) {
            Button("继续编辑", role: .cancel) { pendingSelectionID = nil }
            Button("放弃修改", role: .destructive) {
                let selection = pendingSelectionID
                finishEditing()
                if let selection { selectedItemID = selection }
            }
        } message: {
            Text("当前 Markdown 修改尚未保存。")
        }
        .sheet(isPresented: $showingCreateSheet) {
            KnowledgeCreateSheet(
                collections: store.knowledgeCollections,
                defaultCollectionID: writableCollectionID
            ) { draft in
                let created = try await store.addKnowledgeDocument(draft)
                await MainActor.run {
                    showingArchived = false
                    navigation.selectedKnowledgeLibraryID = created.collection
                    navigation.section = .knowledge
                    selectedItemID = KnowledgeListItem.documentSummaryID(created.metadata.id)
                    refreshGeneration += 1
                    store.refreshKnowledgeCatalog()
                }
            }
        }
        .sheet(isPresented: $showingMetadataSheet) {
            if let document {
                KnowledgeMetadataSheet(document: document, collections: store.knowledgeCollections) { edit in
                    let updated = try await store.editKnowledgeDocument(document.metadata.id, edit: edit)
                    await MainActor.run {
                        self.document = updated
                        showingArchived = updated.metadata.status == "archived"
                        navigation.selectedKnowledgeLibraryID = updated.collection
                        navigation.section = .knowledge
                        selectedItemID = KnowledgeListItem.documentSummaryID(updated.metadata.id)
                        refreshGeneration += 1
                        store.refreshKnowledgeCatalog()
                    }
                }
            }
        }
        .sheet(isPresented: $showingGovernanceSheet) {
            KnowledgeGovernanceSheet(store: store)
        }
        .sheet(item: $feedbackDraft) { draft in
            KnowledgeFeedbackSheet(draft: draft) { note in
                try await store.recordKnowledgeFeedback(
                    documentID: draft.documentID,
                    outcome: draft.outcome,
                    note: note
                )
                await MainActor.run {
                    feedbackStatus = draft.outcome == "corrected" ? "已记录纠正" : "已标记不相关"
                }
            }
        }
        .fileImporter(
            isPresented: $showingImporter,
            allowedContentTypes: [UTType(filenameExtension: "md") ?? .plainText, .plainText],
            allowsMultipleSelection: true
        ) { result in
            switch result {
            case .success(let urls): Task { await importKnowledge(urls) }
            case .failure(let error): operationError = KnowledgeOperationError(message: error.localizedDescription)
            }
        }
        .confirmationDialog(
            document?.metadata.status == "archived" ? "恢复这条知识？" : "归档这条知识？",
            isPresented: $showingArchiveConfirmation,
            titleVisibility: .visible
        ) {
            Button(document?.metadata.status == "archived" ? "恢复" : "归档") {
                Task { await toggleArchive() }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text(document?.metadata.status == "archived" ? "恢复后会重新出现在知识库中。" : "内容不会删除，可在归档视图中恢复。")
        }
        .confirmationDialog(
            "永久删除“\(deleteSummary?.title ?? "")”？",
            isPresented: Binding(
                get: { deleteSummary != nil },
                set: { if !$0 { deleteSummary = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button("永久删除", role: .destructive) {
                if let summary = deleteSummary {
                    deleteSummary = nil
                    Task { await deleteDocument(summary) }
                }
            }
            Button("取消", role: .cancel) { deleteSummary = nil }
        } message: {
            Text("这会永久移出 ATM 知识库，无法恢复。如果知识来自外部导入文件，原始 Markdown 不会被删除。")
        }
        .alert(item: $operationError) { error in
            Alert(title: Text("操作失败"), message: Text(error.message), dismissButton: .default(Text("好")))
        }
        .alert(
            "重命名知识",
            isPresented: Binding(
                get: { renameSummary != nil },
                set: { if !$0 { renameSummary = nil } }
            )
        ) {
            TextField("知识标题", text: $renameText)
            Button("保存") {
                if let summary = renameSummary {
                    let title = renameText
                    Task { await renameDocument(summary, to: title) }
                }
                renameSummary = nil
            }
            Button("取消", role: .cancel) { renameSummary = nil }
        }
        .onChange(of: selectedLibraryID) { _ in
            if showingArchived {
                showingArchived = false
            }
        }
        .onChange(of: navigation.knowledgeCreateRequest) { _ in
            showingCreateSheet = true
        }
    }

    private var writableCollectionID: String {
        selectedLibraryID == ATMKnowledgeLibrary.memoryID ? (store.knowledgeCollections.first?.id ?? "inbox") : selectedLibraryID
    }

    private var itemList: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 5) {
                Text("知识库")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                HStack(spacing: 8) {
                    Image(systemName: selectedLibraryID == ATMKnowledgeLibrary.memoryID ? "brain.head.profile" : "folder")
                        .foregroundStyle(ATMTheme.accent)
                    Text(libraryTitle)
                        .font(ATMFont.font(.title3, weight: .semibold))
                        .lineLimit(1)
                    Text("\(items.count)")
                        .font(ATMFont.mono(.caption, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(ATMTheme.controlFill, in: Capsule())
                    Spacer(minLength: 4)
                    if selectedLibraryID != ATMKnowledgeLibrary.memoryID {
                        ATMIconButton(
                            systemImage: showingArchived ? "archivebox.fill" : "archivebox",
                            help: showingArchived ? "返回当前知识库" : "查看归档",
                            chrome: .bare,
                            isEmphasized: showingArchived,
                            side: 28,
                            iconTier: .bodyLarge
                        ) { showingArchived.toggle() }

                        Menu {
                            Button {
                                showingCreateSheet = true
                            } label: {
                                Label("新建知识", systemImage: "square.and.pencil")
                            }
                            Button {
                                showingImporter = true
                            } label: {
                                Label("导入 Markdown…", systemImage: "square.and.arrow.down")
                            }
                            Divider()
                            Button {
                                showingGovernanceSheet = true
                            } label: {
                                Label("知识健康", systemImage: "checkmark.shield")
                            }
                        } label: {
                            ATMIconMenuLabel(
                                systemImage: isImporting ? "hourglass" : "plus",
                                help: "添加知识",
                                isEnabled: !isImporting,
                                side: 28,
                                iconTier: .bodyLarge
                            )
                        }
                        .menuStyle(.borderlessButton)
                        .menuIndicator(.hidden)
                        .disabled(isImporting)
                    }
                    ATMIconButton(
                        systemImage: isListLoading ? "hourglass" : "arrow.clockwise",
                        help: "刷新当前知识库",
                        chrome: .bare,
                        isEnabled: !isListLoading,
                        side: 28,
                        iconTier: .bodyLarge
                    ) { refreshGeneration += 1 }
                }
            }
            .padding(.horizontal, 14)
            .frame(height: 88)

            Divider()

            Group {
                if isListLoading && items.isEmpty {
                    knowledgeState(icon: "hourglass", title: "正在读取知识")
                } else if let listError {
                    knowledgeState(icon: "exclamationmark.triangle", title: "加载失败", detail: listError)
                } else if items.isEmpty {
                    knowledgeState(
                        icon: showingArchived ? "archivebox" : "doc",
                        title: showingArchived ? "归档中没有内容" : "这个知识库还是空的"
                    )
                } else {
                    ScrollView {
                        LazyVStack(spacing: 4) {
                            ForEach(items) { item in
                                knowledgeRow(item)
                            }
                        }
                        .padding(8)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(ATMTheme.listPane)
    }

    private var detailPane: some View {
        Group {
            if let item = selectedItem, editingItemID == item.id {
                knowledgeEditor(for: item)
            } else if isDetailLoading {
                knowledgeState(icon: "hourglass", title: "正在读取详情")
            } else if let detailError {
                knowledgeState(icon: "exclamationmark.triangle", title: "详情加载失败", detail: detailError)
            } else if let item = selectedItem {
                switch item {
                case .document(let summary):
                    if let document {
                        documentDetail(document, summary: summary)
                    } else {
                        knowledgeState(icon: "doc.text", title: summary.title)
                    }
                case .memory(let memory):
                    memoryDetail(memory)
                }
            } else {
                knowledgeState(
                    icon: selectedLibraryID == ATMKnowledgeLibrary.memoryID ? "brain.head.profile" : "doc.text",
                    title: "选择一条内容",
                    detail: "从中栏查看知识详情"
                )
            }
        }
        .id(selectedItemID)
    }

    private func knowledgeRow(_ item: KnowledgeListItem) -> some View {
        let selected = selectedItemID == item.id
        return Button {
            requestSelection(item.id)
        } label: {
            HStack(alignment: .top, spacing: 9) {
                Image(systemName: item.icon)
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(selected ? ATMTheme.accent : ATMTheme.secondary)
                    .frame(width: 17, height: 20)
                VStack(alignment: .leading, spacing: 5) {
                    Text(item.title)
                        .font(ATMFont.font(.body, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                    if let subtitle = item.subtitle, !subtitle.isEmpty {
                        Text(subtitle)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)
                    }
                    HStack(spacing: 6) {
                        if let date = item.dateText, !date.isEmpty {
                            Text(date)
                        }
                    }
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 0)
            }
            .atmRowSurface(isSelected: selected)
        }
        .buttonStyle(.plain)
        .atmRightClickMenu { knowledgeMenuEntries(for: item) }
    }

    @ATMMenuBuilder
    private func knowledgeMenuEntries(for item: KnowledgeListItem) -> [ATMMenuEntry] {
        switch item {
        case .document(let summary):
            ATMMenuItem("重命名…") {
                renameText = summary.title
                renameSummary = summary
            }
            ATMMenuSubmenu("移动到", systemImage: "folder") {
                let destinations = store.knowledgeCollections.filter { $0.id != summary.collection }
                if destinations.isEmpty {
                    ATMMenuItem("没有其他知识库", enabled: false) {}
                } else {
                    for collection in destinations {
                        ATMMenuItem(collection.name, systemImage: "folder") {
                            Task { await moveDocument(summary, to: collection.id) }
                        }
                    }
                }
            }
            let archived = summary.status == "archived"
            ATMMenuItem(archived ? "恢复" : "归档") {
                Task { await toggleArchive(summary: summary, archived: archived) }
            }
            ATMMenuItem("复制 ID") { copyIdentifier(summary.documentID) }
            ATMMenuSeparator()
            ATMMenuItem("新建知识…") { showingCreateSheet = true }
            ATMMenuSeparator()
            ATMMenuItem("永久删除…", destructive: true) { deleteSummary = summary }
        case .memory(let memory):
            ATMMenuItem("复制 ID") { copyIdentifier(memory.id) }
            ATMMenuSeparator()
            ATMMenuItem("新建知识…") { showingCreateSheet = true }
        }
    }

    @MainActor
    private func moveDocument(_ summary: ATMKnowledgeDocumentSummary, to collectionID: String) async {
        let itemID = KnowledgeListItem.documentSummaryID(summary.documentID)
        if editingItemID == itemID && hasUnsavedChanges {
            operationError = KnowledgeOperationError(message: "请先保存或放弃这条知识的 Markdown 修改，再移动知识库。")
            return
        }

        do {
            _ = try await store.moveKnowledgeDocument(summary.documentID, to: collectionID)
            if selectedItemID == itemID {
                finishEditing()
                selectedItemID = nil
                document = nil
            }
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
            await loadItems()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    @MainActor
    private func renameDocument(_ summary: ATMKnowledgeDocumentSummary, to newTitle: String) async {
        let trimmed = newTitle.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != summary.title else { return }
        do {
            let updated = try await store.editKnowledgeDocument(
                summary.documentID,
                edit: ATMKnowledgeMetadataEdit(
                    title: trimmed,
                    collection: summary.collection,
                    status: summary.status ?? "active",
                    domains: summary.domains,
                    tags: summary.tags,
                    projects: summary.projects
                )
            )
            if selectedItemID == KnowledgeListItem.documentSummaryID(summary.documentID) {
                document = updated
            }
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
            await loadItems()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    @MainActor
    private func toggleArchive(summary: ATMKnowledgeDocumentSummary, archived: Bool) async {
        do {
            _ = try await store.editKnowledgeDocument(
                summary.documentID,
                edit: ATMKnowledgeMetadataEdit(
                    title: summary.title,
                    collection: summary.collection,
                    status: archived ? "active" : "archived",
                    domains: summary.domains,
                    tags: summary.tags,
                    projects: summary.projects
                )
            )
            if selectedItemID == KnowledgeListItem.documentSummaryID(summary.documentID) {
                selectedItemID = nil
                document = nil
            }
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
            await loadItems()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    @MainActor
    private func deleteDocument(_ summary: ATMKnowledgeDocumentSummary) async {
        do {
            try await store.deleteKnowledgeDocument(summary.documentID)
            if selectedItemID == KnowledgeListItem.documentSummaryID(summary.documentID) {
                finishEditing()
                selectedItemID = nil
                document = nil
            }
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
            await loadItems()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    private func documentDetail(_ document: ATMKnowledgeDocument, summary: ATMKnowledgeDocumentSummary) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                detailHeader(
                    title: document.metadata.title,
                    identifier: document.metadata.id,
                    infoTitle: "知识信息",
                    infoFields: documentInfoFields(document),
                    onEdit: isEditable(document) ? { startEditingDocument(document) } : nil,
                    onEditInfo: { showingMetadataSheet = true },
                    onArchive: { showingArchiveConfirmation = true },
                    onDelete: { deleteSummary = summary },
                    archiveTitle: document.metadata.status == "archived" ? "恢复知识" : "归档知识"
                )

                knowledgeFeedbackBar(document)

                ATMMarkdownContentView(
                    source: ATMMarkdown.documentBody(document.content, removingTitle: document.metadata.title)
                )

            }
            .padding(20)
            .atmWorkspaceCard()
            .padding(24)
            .frame(maxWidth: 900, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    @ViewBuilder
    private func knowledgeFeedbackBar(_ document: ATMKnowledgeDocument) -> some View {
        if store.currentSessionID != nil {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    Label(
                        store.snapshot.currentSession?.todo == nil
                            ? "这条知识对你有帮助吗？"
                            : "这条知识是否帮助了当前任务？",
                        systemImage: "sparkles"
                    )
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                    Spacer()
                    if isFeedbackSaving {
                        ProgressView().controlSize(.small)
                    }
                    if let feedbackStatus {
                        Text(feedbackStatus)
                            .font(ATMFont.font(.footnote, weight: .semibold))
                            .foregroundStyle(ATMTheme.success)
                    }
                }

                if let todo = store.snapshot.currentSession?.todo {
                    Text("当前任务：\(todo.id) · \(todo.title)")
                        .font(ATMFont.body)
                        .foregroundStyle(ATMTheme.primary)
                        .lineLimit(2)
                        .textSelection(.enabled)
                }

                HStack(spacing: 8) {
                    Spacer()
                    Button("有帮助") {
                        Task {
                            await submitKnowledgeFeedback(
                                documentID: document.metadata.id,
                                outcome: "adopted",
                                note: ""
                            )
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .disabled(isFeedbackSaving)
                    Button("需要纠正…") {
                        feedbackDraft = KnowledgeFeedbackDraft(
                            documentID: document.metadata.id,
                            title: document.metadata.title,
                            outcome: "corrected"
                        )
                    }
                    .controlSize(.small)
                    Button("不相关…") {
                        feedbackDraft = KnowledgeFeedbackDraft(
                            documentID: document.metadata.id,
                            title: document.metadata.title,
                            outcome: "rejected"
                        )
                    }
                    .controlSize(.small)
                }
            }
            .padding(11)
            .background(ATMTheme.controlFill.opacity(0.72), in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(ATMTheme.border))
        }
    }

    @MainActor
    private func submitKnowledgeFeedback(documentID: String, outcome: String, note: String) async {
        guard !isFeedbackSaving else { return }
        isFeedbackSaving = true
        defer { isFeedbackSaving = false }
        do {
            try await store.recordKnowledgeFeedback(
                documentID: documentID,
                outcome: outcome,
                note: note
            )
            feedbackStatus = "已记录有帮助"
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    private func memoryDetail(_ memory: ATMMemoryHit) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                detailHeader(
                    title: memory.title,
                    identifier: memory.id,
                    infoTitle: "记忆信息",
                    infoFields: memoryInfoFields(memory),
                    onEdit: { startEditingMemory(memory) }
                )

                ATMMarkdownContentView(source: memory.content)
            }
            .padding(20)
            .atmWorkspaceCard()
            .padding(24)
            .frame(maxWidth: 900, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func detailHeader(
        title: String,
        identifier: String,
        infoTitle: String,
        infoFields: [KnowledgeInfoField],
        onEdit: (() -> Void)? = nil,
        onEditInfo: (() -> Void)? = nil,
        onArchive: (() -> Void)? = nil,
        onDelete: (() -> Void)? = nil,
        archiveTitle: String = "归档知识"
    ) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(libraryTitle)
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.accent)

            HStack(alignment: .top, spacing: 10) {
                Text(title)
                    .font(ATMFont.font(.metric, weight: .bold))
                    .lineLimit(4)
                    .textSelection(.enabled)
                    .layoutPriority(1)

                Spacer(minLength: 2)

                HStack(spacing: 2) {
                    if let onEdit {
                        ATMIconButton(
                            systemImage: "pencil",
                            help: "编辑 Markdown",
                            chrome: .bare,
                            side: 26,
                            iconTier: .bodyLarge,
                            action: onEdit
                        )
                    }

                    ATMIconButton(
                        systemImage: "info.circle",
                        help: infoTitle,
                        chrome: .bare,
                        isEmphasized: showingInfo,
                        side: 26,
                        iconTier: .bodyLarge
                    ) { showingInfo.toggle() }
                    .popover(isPresented: $showingInfo, arrowEdge: .top) {
                        knowledgeInfoPopover(title: infoTitle, fields: infoFields)
                    }

                    ATMIconButton(
                        systemImage: copiedIdentifier == identifier ? "checkmark" : "doc.on.doc",
                        help: copiedIdentifier == identifier ? "已复制" : "复制 ID",
                        chrome: .bare,
                        isEmphasized: copiedIdentifier == identifier,
                        side: 26,
                        iconTier: .bodyLarge
                    ) { copyIdentifier(identifier) }

                    if onEditInfo != nil || onArchive != nil || onDelete != nil {
                        Menu {
                            if let onEditInfo {
                                Button(action: onEditInfo) {
                                    Label("编辑信息", systemImage: "slider.horizontal.3")
                                }
                            }
                            if let onArchive {
                                Divider()
                                Button(action: onArchive) {
                                    Label(archiveTitle, systemImage: archiveTitle == "恢复知识" ? "arrow.uturn.backward" : "archivebox")
                                }
                            }
                            if let onDelete {
                                Divider()
                                Button(role: .destructive, action: onDelete) {
                                    Label("永久删除…", systemImage: "trash")
                                }
                            }
                        } label: {
                            ATMIconMenuLabel(
                                systemImage: "ellipsis",
                                help: "管理知识",
                                side: 26,
                                iconTier: .bodyLarge
                            )
                        }
                        .menuStyle(.borderlessButton)
                        .menuIndicator(.hidden)
                    }
                }
                .padding(.top, 1)
            }
        }
    }

    private func knowledgeEditor(for item: KnowledgeListItem) -> some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 12) {
                Text(item.title)
                    .font(ATMFont.font(.title2, weight: .bold))
                    .lineLimit(3)
                    .frame(maxWidth: .infinity, alignment: .leading)

                HStack(spacing: 10) {
                    Text(editorSaveHint(for: item))
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)

                    Spacer(minLength: 0)

                    ATMIconButton(
                        systemImage: editorMode == .edit ? "eye" : "pencil",
                        help: editorMode == .edit ? "预览 Markdown" : "返回编辑",
                        chrome: .bare,
                        side: 26,
                        iconTier: .body
                    ) {
                        editorMode = editorMode == .edit ? .preview : .edit
                    }

                    Button("取消") { requestCancelEditing() }
                        .keyboardShortcut(.cancelAction)
                        .disabled(isSaving)

                    Button {
                        saveEditing(item)
                    } label: {
                        HStack(spacing: 5) {
                            if isSaving {
                                ProgressView()
                                    .controlSize(.small)
                            }
                            Text(isSaving ? "保存中" : "保存")
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut("s", modifiers: .command)
                    .disabled(isSaving || !hasUnsavedChanges || editContent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }

                if let editError {
                    Label(editError, systemImage: "exclamationmark.triangle.fill")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.danger)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(ATMTheme.dangerFill, in: RoundedRectangle(cornerRadius: 7))
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 14)
            .background(ATMTheme.surface.opacity(0.72))

            Divider()

            if editorMode == .edit {
                TextEditor(text: $editContent)
                    .font(ATMFont.mono(.body))
                    .lineSpacing(3)
                    .scrollContentBackground(.hidden)
                    .padding(14)
                    .background(ATMTheme.canvas)
                    .focused($editorFocused)
                    .disabled(isSaving)
            } else {
                ScrollView {
                    ATMMarkdownContentView(source: editContent)
                        .padding(24)
                        .frame(maxWidth: 980, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .background(ATMTheme.canvas)
            }
        }
    }

    private func editorSaveHint(for item: KnowledgeListItem) -> String {
        switch item {
        case .memory:
            return "保存会创建新版本，旧记忆仍保留在历史中"
        case .document:
            if document?.metadata.source?.type == "file" {
                return "保存会回写源 Markdown，并自动重新导入"
            }
            return "Markdown 内容将更新到当前知识条目"
        }
    }

    private func isEditable(_ document: ATMKnowledgeDocument) -> Bool {
        guard let source = document.metadata.source else { return true }
        return source.type == "file"
    }

    private func startEditingDocument(_ document: ATMKnowledgeDocument) {
        beginEditing(
            itemID: KnowledgeListItem.documentSummaryID(document.metadata.id),
            content: ATMMarkdown.documentBody(document.content, removingTitle: document.metadata.title)
        )
    }

    private func startEditingMemory(_ memory: ATMMemoryHit) {
        beginEditing(itemID: KnowledgeListItem.memoryID(memory.id), content: memory.content)
    }

    private func beginEditing(itemID: String, content: String) {
        showingInfo = false
        copiedIdentifier = nil
        feedbackStatus = nil
        editingItemID = itemID
        editContent = content
        originalEditContent = content
        editorMode = .edit
        editError = nil
        pendingSelectionID = nil
        Task { @MainActor in editorFocused = true }
    }

    private func requestSelection(_ itemID: String) {
        guard itemID != selectedItemID else { return }
        if isEditing && hasUnsavedChanges {
            pendingSelectionID = itemID
            showingDiscardAlert = true
            return
        }
        finishEditing()
        selectedItemID = itemID
    }

    private func requestCancelEditing() {
        if hasUnsavedChanges {
            pendingSelectionID = nil
            showingDiscardAlert = true
        } else {
            finishEditing()
        }
    }

    private func finishEditing() {
        editingItemID = nil
        editContent = ""
        originalEditContent = ""
        editorMode = .edit
        isSaving = false
        editError = nil
        pendingSelectionID = nil
        editorFocused = false
    }

    private func saveEditing(_ item: KnowledgeListItem) {
        guard !isSaving else { return }
        let content = editContent.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !content.isEmpty else { return }

        isSaving = true
        editError = nil
        Task { @MainActor in
            do {
                switch item {
                case .document(let summary):
                    document = try await store.updateKnowledgeDocument(summary.documentID, content: editContent)
                    originalEditContent = editContent
                    finishEditing()
                    refreshGeneration += 1
                case .memory(let memory):
                    try await store.supersedeMemory(memory, content: editContent)
                    originalEditContent = editContent
                    finishEditing()
                    refreshGeneration += 1
                }
            } catch {
                isSaving = false
                editError = error.localizedDescription
            }
        }
    }

    private func copyIdentifier(_ identifier: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(identifier, forType: .string)
        copiedIdentifier = identifier
        Task { @MainActor in
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            if copiedIdentifier == identifier { copiedIdentifier = nil }
        }
    }

    private func knowledgeInfoPopover(title: String, fields: [KnowledgeInfoField]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "info.circle.fill")
                    .foregroundStyle(ATMTheme.accent)
                Text(title)
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 11) {
                    ForEach(fields) { field in
                        metadataRow(field.label, field.value)
                    }
                }
                .padding(16)
            }
            .frame(maxHeight: 340)
        }
        .frame(width: 386)
        .background(ATMTheme.surface)
    }

    private func documentInfoFields(_ document: ATMKnowledgeDocument) -> [KnowledgeInfoField] {
        let documentLibrary = store.knowledgeCollections.first { $0.id == document.collection }?.name ?? document.collection
        var fields = [
            KnowledgeInfoField(label: "状态", value: localizedStatus(document.metadata.status)),
            KnowledgeInfoField(label: "知识库", value: documentLibrary),
            KnowledgeInfoField(label: "贡献者", value: document.metadata.producer),
            KnowledgeInfoField(label: "创建时间", value: KnowledgeDateFormatter.display(document.metadata.createdAt)),
            KnowledgeInfoField(label: "更新时间", value: KnowledgeDateFormatter.display(document.metadata.updatedAt)),
        ]
        if !document.metadata.domains.isEmpty {
            fields.append(KnowledgeInfoField(label: "领域", value: document.metadata.domains.joined(separator: " · ")))
        }
        if !document.metadata.projects.isEmpty {
            fields.append(KnowledgeInfoField(label: "项目", value: document.metadata.projects.joined(separator: " · ")))
        }
        if !document.metadata.tags.isEmpty {
            fields.append(KnowledgeInfoField(label: "标签", value: document.metadata.tags.joined(separator: " · ")))
        }
        if let source = document.metadata.source {
            fields.append(KnowledgeInfoField(label: "来源", value: source.uri.isEmpty ? source.type : source.uri))
        }
        return fields
    }

    private func memoryInfoFields(_ memory: ATMMemoryHit) -> [KnowledgeInfoField] {
        var fields = [
            KnowledgeInfoField(label: "范围", value: localizedScope(memory.scope)),
            KnowledgeInfoField(label: "创建时间", value: KnowledgeDateFormatter.display(memory.createdAt)),
            KnowledgeInfoField(label: "来源", value: memory.metadata["source"] ?? memory.source),
        ]
        if !memory.tags.isEmpty {
            fields.append(KnowledgeInfoField(label: "标签", value: memory.tags.joined(separator: " · ")))
        }
        for key in memory.metadata.keys.sorted() where key != "source" {
            fields.append(KnowledgeInfoField(label: key, value: memory.metadata[key] ?? ""))
        }
        return fields
    }

    private func metadataRow(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text(label)
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 76, alignment: .leading)
            Text(value)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(ATMFont.body)
    }

    private func knowledgeState(icon: String, title: String, detail: String? = nil) -> some View {
        VStack(spacing: 9) {
            Image(systemName: icon)
                .font(ATMFont.font(.display, weight: .light))
                .foregroundStyle(ATMTheme.secondary)
            Text(title)
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                .multilineTextAlignment(.center)
            if let detail, !detail.isEmpty {
                Text(detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .multilineTextAlignment(.center)
                    .lineLimit(5)
            }
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    @MainActor
    private func loadItems() async {
        isListLoading = true
        listError = nil
        defer { isListLoading = false }

        do {
            let loaded: [KnowledgeListItem]
            if selectedLibraryID == ATMKnowledgeLibrary.memoryID {
                loaded = try await store.memories(query: "").map(KnowledgeListItem.memory)
            } else {
                loaded = try await store.knowledgeDocuments(
                    collectionID: selectedLibraryID,
                    status: showingArchived ? "archived" : "active"
                )
                    .map(KnowledgeListItem.document)
            }
            guard !Task.isCancelled else { return }
            items = loaded
            selectFirstItemIfNeeded()
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled else { return }
            items = []
            selectedItemID = nil
            document = nil
            listError = error.localizedDescription
        }
    }

    @MainActor
    private func loadSelectedDocument() async {
        showingInfo = false
        copiedIdentifier = nil
        document = nil
        detailError = nil
        guard case .document(let summary) = selectedItem else {
            isDetailLoading = false
            return
        }
        isDetailLoading = true
        defer { isDetailLoading = false }
        do {
            let loaded = try await store.knowledgeDocument(summary.documentID)
            guard !Task.isCancelled, selectedItemID == KnowledgeListItem.document(summary).id else { return }
            document = loaded
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled else { return }
            detailError = error.localizedDescription
        }
    }

    private func selectFirstItemIfNeeded() {
        if let selectedItemID, items.contains(where: { $0.id == selectedItemID }) { return }
        selectedItemID = items.first?.id
        document = nil
    }

    @MainActor
    private func importKnowledge(_ urls: [URL]) async {
        guard !urls.isEmpty else { return }
        isImporting = true
        defer { isImporting = false }
        do {
            var lastImported: ATMKnowledgeDocument?
            for url in urls {
                let scoped = url.startAccessingSecurityScopedResource()
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                let imported = try await store.importKnowledge(at: url, collectionID: writableCollectionID)
                lastImported = imported.last ?? lastImported
            }
            if let lastImported {
                showingArchived = false
                navigation.selectedKnowledgeLibraryID = lastImported.collection
                navigation.section = .knowledge
                selectedItemID = KnowledgeListItem.documentSummaryID(lastImported.metadata.id)
            }
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    @MainActor
    private func toggleArchive() async {
        guard let document else { return }
        let targetStatus = document.metadata.status == "archived" ? "active" : "archived"
        do {
            let updated = try await store.editKnowledgeDocument(
                document.metadata.id,
                edit: ATMKnowledgeMetadataEdit(
                    title: document.metadata.title,
                    collection: document.collection,
                    status: targetStatus,
                    domains: document.metadata.domains,
                    tags: document.metadata.tags,
                    projects: document.metadata.projects
                )
            )
            self.document = updated
            selectedItemID = nil
            refreshGeneration += 1
            store.refreshKnowledgeCatalog()
        } catch {
            operationError = KnowledgeOperationError(message: error.localizedDescription)
        }
    }

    private func localizedStatus(_ status: String) -> String {
        ["active": "生效中", "draft": "草稿", "archived": "已归档"][status] ?? status
    }

    private func localizedScope(_ scope: String) -> String {
        ["global": "全局", "project": "项目", "session": "会话"][scope] ?? scope
    }
}

private struct KnowledgeLoadKey: Equatable {
    let libraryID: String
    let generation: Int
    let showingArchived: Bool
}

private struct KnowledgeOperationError: Identifiable {
    let id = UUID()
    let message: String
}

private struct KnowledgeInfoField: Identifiable, Equatable {
    let label: String
    let value: String

    var id: String { "\(label):\(value)" }
}

private enum KnowledgeEditorMode: String {
    case edit
    case preview
}

private enum KnowledgeListItem: Identifiable, Equatable {
    case document(ATMKnowledgeDocumentSummary)
    case memory(ATMMemoryHit)

    var id: String {
        switch self {
        case .document(let item): return "document:\(item.documentID)"
        case .memory(let item): return "memory:\(item.id)"
        }
    }

    var title: String {
        switch self {
        case .document(let item): return item.title
        case .memory(let item): return item.title
        }
    }

    var subtitle: String? {
        switch self {
        case .document(let item): return item.snippet
        case .memory(let item): return item.content
        }
    }

    var dateText: String? {
        switch self {
        case .document(let item): return item.updatedAt.map(KnowledgeDateFormatter.short)
        case .memory(let item): return KnowledgeDateFormatter.short(item.createdAt)
        }
    }

    var icon: String {
        switch self {
        case .document: return "doc.text"
        case .memory: return "brain.head.profile"
        }
    }

    static func documentSummaryID(_ documentID: String) -> String { "document:\(documentID)" }
    static func memoryID(_ memoryID: String) -> String { "memory:\(memoryID)" }
}

enum KnowledgeDateFormatter {
    private static let fractionalParser: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
    private static let parser = ISO8601DateFormatter()
    private static let displayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = "yyyy-MM-dd HH:mm"
        return formatter
    }()
    private static let shortFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = "MM-dd"
        return formatter
    }()

    static func display(_ source: String) -> String {
        guard let date = fractionalParser.date(from: source) ?? parser.date(from: source) else { return source }
        return displayFormatter.string(from: date)
    }

    static func short(_ source: String) -> String {
        guard let date = fractionalParser.date(from: source) ?? parser.date(from: source) else { return String(source.prefix(10)) }
        return shortFormatter.string(from: date)
    }
}

private extension ATMMemoryHit {
    var title: String {
        let firstLine = content.split(whereSeparator: \.isNewline).first.map(String.init) ?? content
        let trimmed = firstLine.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > 44 else { return trimmed }
        return String(trimmed.prefix(44)) + "…"
    }
}

private struct KnowledgeCreateSheet: View {
    let collections: [ATMKnowledgeCollection]
    let onSave: (ATMKnowledgeDraft) async throws -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var collectionID: String
    @State private var domains = ""
    @State private var tags = ""
    @State private var projects = ""
    @State private var content = ""
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(
        collections: [ATMKnowledgeCollection],
        defaultCollectionID: String,
        onSave: @escaping (ATMKnowledgeDraft) async throws -> Void
    ) {
        self.collections = collections
        self.onSave = onSave
        _collectionID = State(initialValue: defaultCollectionID)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("新建知识")
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text("创建后会保存到 ATM 中央知识库")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("取消") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                    .disabled(isSaving)
                Button {
                    save()
                } label: {
                    if isSaving { ProgressView().controlSize(.small) } else { Text("创建") }
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(18)

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    knowledgeFormRow("标题") {
                        TextField("知识标题", text: $title)
                    }
                    knowledgeFormRow("知识库") {
                        Picker("", selection: $collectionID) {
                            ForEach(collections) { collection in
                                Text(collection.name).tag(collection.id)
                            }
                        }
                        .labelsHidden()
                    }
                    knowledgeFormRow("标签") { TextField("多个值用逗号分隔", text: $tags) }
                    knowledgeFormRow("领域") { TextField("例如 architecture, design", text: $domains) }
                    knowledgeFormRow("项目") { TextField("例如 atm", text: $projects) }

                    VStack(alignment: .leading, spacing: 7) {
                        Text("Markdown 内容")
                            .font(ATMFont.font(.body, weight: .semibold))
                        TextEditor(text: $content)
                            .font(ATMFont.mono(.body))
                            .scrollContentBackground(.hidden)
                            .padding(8)
                            .frame(minHeight: 240)
                            .background(ATMTheme.canvas, in: RoundedRectangle(cornerRadius: 8))
                            .overlay(RoundedRectangle(cornerRadius: 8).stroke(ATMTheme.border))
                    }

                    if let errorMessage {
                        Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.danger)
                    }
                }
                .padding(18)
            }
        }
        .frame(minWidth: 700, minHeight: 670)
    }

    private func save() {
        guard !isSaving else { return }
        isSaving = true
        errorMessage = nil
        let draft = ATMKnowledgeDraft(
            title: title.trimmingCharacters(in: .whitespacesAndNewlines),
            collection: collectionID,
            domains: splitKnowledgeValues(domains),
            tags: splitKnowledgeValues(tags),
            projects: splitKnowledgeValues(projects),
            content: content
        )
        Task {
            do {
                try await onSave(draft)
                await MainActor.run { dismiss() }
            } catch {
                await MainActor.run {
                    isSaving = false
                    errorMessage = error.localizedDescription
                }
            }
        }
    }
}

private struct KnowledgeMetadataSheet: View {
    let document: ATMKnowledgeDocument
    let collections: [ATMKnowledgeCollection]
    let onSave: (ATMKnowledgeMetadataEdit) async throws -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var title: String
    @State private var collectionID: String
    @State private var status: String
    @State private var domains: String
    @State private var tags: String
    @State private var projects: String
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(
        document: ATMKnowledgeDocument,
        collections: [ATMKnowledgeCollection],
        onSave: @escaping (ATMKnowledgeMetadataEdit) async throws -> Void
    ) {
        self.document = document
        self.collections = collections
        self.onSave = onSave
        _title = State(initialValue: document.metadata.title)
        _collectionID = State(initialValue: document.collection)
        _status = State(initialValue: document.metadata.status)
        _domains = State(initialValue: document.metadata.domains.joined(separator: ", "))
        _tags = State(initialValue: document.metadata.tags.joined(separator: ", "))
        _projects = State(initialValue: document.metadata.projects.joined(separator: ", "))
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("编辑知识信息")
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text(document.metadata.source?.type == "file" ? "改名会同步回写源 Markdown" : "调整分类、标签和状态")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("取消") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                    .disabled(isSaving)
                Button {
                    save()
                } label: {
                    if isSaving { ProgressView().controlSize(.small) } else { Text("保存") }
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(18)

            Divider()

            VStack(alignment: .leading, spacing: 14) {
                knowledgeFormRow("标题") { TextField("知识标题", text: $title) }
                knowledgeFormRow("知识库") {
                    Picker("", selection: $collectionID) {
                        ForEach(collections) { collection in Text(collection.name).tag(collection.id) }
                    }
                    .labelsHidden()
                }
                knowledgeFormRow("状态") {
                    Picker("", selection: $status) {
                        Text("生效中").tag("active")
                        Text("草稿").tag("draft")
                        Text("已归档").tag("archived")
                    }
                    .labelsHidden()
                }
                knowledgeFormRow("标签") { TextField("多个值用逗号分隔", text: $tags) }
                knowledgeFormRow("领域") { TextField("多个值用逗号分隔", text: $domains) }
                knowledgeFormRow("项目") { TextField("多个值用逗号分隔", text: $projects) }

                if let errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.danger)
                }
            }
            .padding(18)

            Spacer()
        }
        .frame(width: 600, height: 480)
    }

    private func save() {
        guard !isSaving else { return }
        isSaving = true
        errorMessage = nil
        let edit = ATMKnowledgeMetadataEdit(
            title: title.trimmingCharacters(in: .whitespacesAndNewlines),
            collection: collectionID,
            status: status,
            domains: splitKnowledgeValues(domains),
            tags: splitKnowledgeValues(tags),
            projects: splitKnowledgeValues(projects)
        )
        Task {
            do {
                try await onSave(edit)
                await MainActor.run { dismiss() }
            } catch {
                await MainActor.run {
                    isSaving = false
                    errorMessage = error.localizedDescription
                }
            }
        }
    }
}

private struct KnowledgeFeedbackDraft: Identifiable {
    let documentID: String
    let title: String
    let outcome: String

    var id: String { "\(documentID):\(outcome)" }
}

private struct KnowledgeFeedbackSheet: View {
    let draft: KnowledgeFeedbackDraft
    let onSave: (String) async throws -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var note = ""
    @State private var isSaving = false
    @State private var errorMessage: String?

    private var outcomeTitle: String {
        draft.outcome == "corrected" ? "记录纠正" : "标记不相关"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(outcomeTitle)
                .font(ATMFont.font(.title2, weight: .bold))
            Text(draft.title)
                .font(ATMFont.font(.body, weight: .medium))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(2)
            TextEditor(text: $note)
                .font(ATMFont.body)
                .frame(minHeight: 100)
                .padding(7)
                .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 8))
                .overlay(RoundedRectangle(cornerRadius: 8).stroke(ATMTheme.border))
            Text(draft.outcome == "corrected" ? "写下需要补充或修正的内容。" : "写下它为什么与当前任务不相关。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
            if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
            }
            HStack {
                Spacer()
                Button("取消") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(isSaving ? "保存中" : "保存反馈") {
                    save()
                }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(isSaving || note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 516)
    }

    private func save() {
        guard !isSaving else { return }
        isSaving = true
        errorMessage = nil
        Task {
            do {
                try await onSave(note.trimmingCharacters(in: .whitespacesAndNewlines))
                await MainActor.run { dismiss() }
            } catch {
                await MainActor.run {
                    isSaving = false
                    errorMessage = error.localizedDescription
                }
            }
        }
    }
}

private struct KnowledgeGovernanceSheet: View {
    @ObservedObject var store: ATMDataStore
    @Environment(\.dismiss) private var dismiss
    @State private var report: ATMKnowledgeAuditReport?
    @State private var qualities: [ATMKnowledgeQuality] = []
    @State private var isLoading = true
    @State private var errorMessage: String?
    @State private var generation = 0

    private var feedbackCount: Int {
        qualities.reduce(0) { $0 + $1.adopted + $1.corrected + $1.rejected }
    }

    private var retrievalCount: Int {
        qualities.reduce(0) { $0 + $1.retrievals }
    }

    private var reviewedQualities: [ATMKnowledgeQuality] {
        qualities
            .filter { $0.retrievals > 0 || $0.adopted + $0.corrected + $0.rejected > 0 }
            .sorted { $0.score < $1.score }
            .prefix(6)
            .map { $0 }
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("知识健康")
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text("只读巡检，不会自动归档或修改内容")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                ATMIconButton(
                    systemImage: "arrow.clockwise",
                    help: "重新巡检",
                    chrome: .bare,
                    isEnabled: !isLoading,
                    side: 28,
                    iconTier: .bodyLarge
                ) { generation += 1 }
                Button("完成") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(18)

            Divider()

            if isLoading && report == nil {
                ProgressView("正在巡检知识库…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let errorMessage {
                knowledgeGovernanceState(icon: "exclamationmark.triangle", title: "巡检失败", detail: errorMessage)
            } else if let report {
                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        HStack(spacing: 10) {
                            governanceMetric("活跃知识", value: "\(report.active)", icon: "doc.text")
                            governanceMetric("待复核", value: "\(report.issues.count)", icon: "exclamationmark.bubble")
                            governanceMetric("召回", value: "\(retrievalCount)", icon: "magnifyingglass")
                            governanceMetric("结果反馈", value: "\(feedbackCount)", icon: "hand.thumbsup")
                        }

                        if report.issues.isEmpty {
                            knowledgeGovernanceState(icon: "checkmark.circle", title: "没有发现治理问题", detail: "重复、陈旧、来源漂移和低质量检查均通过")
                                .frame(minHeight: 180)
                        } else {
                            governanceSectionTitle("待复核问题")
                            VStack(spacing: 8) {
                                ForEach(report.issues) { issue in
                                    VStack(alignment: .leading, spacing: 7) {
                                        HStack(spacing: 7) {
                                            Image(systemName: issue.severity == "error" ? "xmark.octagon.fill" : "exclamationmark.triangle.fill")
                                                .foregroundStyle(issue.severity == "error" ? ATMTheme.danger : ATMTheme.warning)
                                            Text(issue.title ?? issue.documentIDs.joined(separator: ", "))
                                                .font(ATMFont.font(.body, weight: .semibold))
                                            Spacer()
                                            Text(localizedAuditCode(issue.code))
                                                .font(ATMFont.mono(.caption))
                                                .foregroundStyle(ATMTheme.secondary)
                                        }
                                        Text(issue.detail)
                                            .font(ATMFont.body)
                                        Label(issue.suggestedAction, systemImage: "arrow.turn.down.right")
                                            .font(ATMFont.footnote)
                                            .foregroundStyle(ATMTheme.secondary)
                                        if !issue.documentIDs.isEmpty {
                                            Text(issue.documentIDs.joined(separator: " · "))
                                                .font(ATMFont.mono(.caption))
                                                .foregroundStyle(ATMTheme.secondary)
                                                .textSelection(.enabled)
                                        }
                                    }
                                    .padding(12)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .background(ATMTheme.controlFill.opacity(0.7), in: RoundedRectangle(cornerRadius: 9))
                                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(ATMTheme.border))
                                }
                            }
                        }

                        if !reviewedQualities.isEmpty {
                            governanceSectionTitle("已有使用记录 · 低分优先")
                            VStack(spacing: 6) {
                                ForEach(reviewedQualities) { quality in
                                    HStack(spacing: 10) {
                                        Text(String(format: "%.2f", quality.score))
                                            .font(ATMFont.mono(.body, .semibold))
                                            .frame(width: 46, alignment: .leading)
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(quality.title).font(ATMFont.font(.body, weight: .medium)).lineLimit(1)
                                            Text("采纳 \(quality.adopted) · 纠正 \(quality.corrected) · 拒绝 \(quality.rejected) · 召回 \(quality.retrievals)")
                                                .font(ATMFont.caption)
                                                .foregroundStyle(ATMTheme.secondary)
                                        }
                                        Spacer()
                                        Text(quality.collection)
                                            .font(ATMFont.mono(.caption))
                                            .foregroundStyle(ATMTheme.secondary)
                                    }
                                    .padding(.vertical, 6)
                                }
                            }
                        }
                    }
                    .padding(18)
                }
            }
        }
        .frame(minWidth: 820, minHeight: 700)
        .task(id: generation) { await load() }
    }

    @MainActor
    private func load() async {
        isLoading = true
        errorMessage = nil
        do {
            async let audit = store.knowledgeAudit()
            async let quality = store.knowledgeQuality()
            report = try await audit
            qualities = try await quality
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    private func governanceMetric(_ title: String, value: String, icon: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(ATMTheme.accent)
            VStack(alignment: .leading, spacing: 2) {
                Text(value).font(ATMFont.rounded(.title2, .bold))
                Text(title).font(ATMFont.caption).foregroundStyle(ATMTheme.secondary)
            }
            Spacer()
        }
        .padding(12)
        .frame(maxWidth: .infinity)
        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 9))
    }

    private func governanceSectionTitle(_ title: String) -> some View {
        Text(title).font(ATMFont.font(.body, weight: .semibold))
    }

    private func knowledgeGovernanceState(icon: String, title: String, detail: String) -> some View {
        VStack(spacing: 9) {
            Image(systemName: icon).font(ATMFont.font(.display, weight: .light)).foregroundStyle(ATMTheme.secondary)
            Text(title).font(ATMFont.font(.bodyLarge, weight: .semibold))
            Text(detail).font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center)
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func localizedAuditCode(_ code: String) -> String {
        [
            "duplicate_title": "标题重复", "duplicate_content": "内容重复", "stale": "长期未复核",
            "source_missing": "来源缺失", "source_invalid": "来源异常", "source_drift": "来源漂移", "low_quality": "质量偏低",
        ][code] ?? code
    }
}

private func knowledgeFormRow<Content: View>(_ label: String, @ViewBuilder content: () -> Content) -> some View {
    HStack(alignment: .firstTextBaseline, spacing: 14) {
        Text(label)
            .font(ATMFont.font(.body, weight: .medium))
            .foregroundStyle(ATMTheme.secondary)
            .frame(width: 74, alignment: .leading)
        content()
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private func splitKnowledgeValues(_ source: String) -> [String] {
    source
        .components(separatedBy: CharacterSet(charactersIn: ",，\n"))
        .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty }
}
