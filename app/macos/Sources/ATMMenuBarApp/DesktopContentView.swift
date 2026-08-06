import AppKit
import Charts
import Combine
import SwiftUI

enum ATMDesktopSection: String, CaseIterable, Identifiable {
    case tasks
    case collection
    case agents
    case knowledge
    case usage
    case settings

    var id: String { rawValue }
    var title: String {
        switch self {
        case .tasks: return "任务"
        case .collection: return "收集"
        case .agents: return "Agent"
        case .knowledge: return "知识"
        case .usage: return "用量"
        case .settings: return "设置"
        }
    }
    var icon: String {
        switch self {
        case .tasks: return "checklist"
        case .collection: return "tray.full"
        case .agents: return "cpu"
        case .knowledge: return "books.vertical"
        case .usage: return "chart.xyaxis.line"
        case .settings: return "gearshape"
        }
    }
}

/// Status color / icon / label used by the task list and detail header.
enum ATMTodoStatusStyle {
    /// Parked by the human via “暂不处理”. Stored as waiting with this wake string
    /// so we do not need a new CLI status.
    static var deferredWake: String { ATMTodoDeferred.wakeCondition }

    static func isDeferred(_ todo: ATMTodo) -> Bool {
        ATMTodoStatusActions.isDeferred(todo)
    }

    static func label(for todo: ATMTodo) -> String {
        if isDeferred(todo) { return "暂不处理" }
        return label(forStatus: todo.status)
    }

    static func label(forStatus status: String) -> String {
        switch status {
        case "open": return "待开始"
        case "in_progress": return "工作中"
        case "waiting": return "等待中"
        case "review": return "待验收"
        case "blocked": return "阻塞"
        case "done": return "已完成"
        case "dropped": return "已放弃"
        default: return status
        }
    }

    static func color(for todo: ATMTodo) -> Color {
        if isDeferred(todo) {
            return Color(red: 117 / 255, green: 128 / 255, blue: 145 / 255)
        }
        return color(forStatus: todo.status)
    }

    static func color(forStatus status: String) -> Color {
        switch status {
        case "open":
            return Color(red: 117 / 255, green: 128 / 255, blue: 145 / 255)
        case "in_progress":
            return Color(red: 52 / 255, green: 112 / 255, blue: 246 / 255)
        case "waiting":
            return Color(red: 230 / 255, green: 139 / 255, blue: 24 / 255)
        case "review":
            return Color(red: 137 / 255, green: 87 / 255, blue: 229 / 255)
        case "blocked":
            return Color(red: 220 / 255, green: 50 / 255, blue: 67 / 255)
        case "done":
            return Color(red: 31 / 255, green: 157 / 255, blue: 104 / 255)
        case "dropped":
            return Color(red: 117 / 255, green: 128 / 255, blue: 145 / 255).opacity(0.55)
        default:
            return ATMTheme.secondary
        }
    }

    static func icon(for todo: ATMTodo) -> String {
        if isDeferred(todo) { return "moon.zzz.fill" }
        return icon(forStatus: todo.status)
    }

    static func icon(forStatus status: String) -> String {
        switch status {
        case "open": return "circle"
        // Fallback SF Symbol when a ProgressView is not practical (menus, a11y).
        case "in_progress": return "circle.dotted"
        case "waiting": return "clock.fill"
        case "review": return "person.crop.circle.badge.checkmark"
        case "blocked": return "exclamationmark.octagon.fill"
        case "done": return "checkmark.circle.fill"
        case "dropped": return "xmark.circle.fill"
        default: return "circle"
        }
    }

    /// True when the row should show a spinner instead of a static status glyph.
    static func usesLoadingIcon(for todo: ATMTodo) -> Bool {
        todo.status == "in_progress" && !isDeferred(todo)
    }

    static func usesStrikethrough(for todo: ATMTodo) -> Bool {
        todo.status == "dropped"
    }
}

/// Priority color / label shared by the task list, the detail header and the
/// collection rules editor, so P0 is the same red everywhere.
///
/// In a list row the priority is carried by the *color of the id* rather than by
/// its own chip: the id is in every row at a fixed width already, so tinting it
/// costs no space, and three P-chips down a column was three words to read where
/// what you wanted was "which of these is on fire".
enum ATMTodoPriorityStyle {
    static func color(for priority: String) -> Color {
        switch priority {
        case "P0": return ATMTheme.danger
        case "P1": return ATMTheme.accent
        default: return ATMTheme.secondary
        }
    }

    /// Spelled out, for the places color cannot carry it on its own — tooltips,
    /// pickers, accessibility.
    static func label(_ priority: String) -> String {
        switch priority {
        case "P0": return "P0 · 紧急"
        case "P1": return "P1 · 高"
        case "P3": return "P3 · 低"
        default: return "P2 · 普通"
        }
    }
}

/// Status glyph for todo lists and badges. Working tasks use a ProgressView so
/// the icon itself reads as “loading”, not a play button.
struct ATMTodoStatusGlyph: View {
    let todo: ATMTodo
    var tier: ATMFont.Tier = .body

    private var size: CGFloat { ATMFont.size(tier) }

    var body: some View {
        if ATMTodoStatusStyle.usesLoadingIcon(for: todo) {
            ProgressView()
                .controlSize(.mini)
                .scaleEffect(size / 11)
                .frame(width: size + 2, height: size + 2)
                .tint(ATMTodoStatusStyle.color(for: todo))
        } else {
            Image(systemName: ATMTodoStatusStyle.icon(for: todo))
                .font(ATMFont.font(tier, weight: .semibold))
                .foregroundStyle(ATMTodoStatusStyle.color(for: todo))
                .frame(width: size + 2, height: size + 2)
        }
    }
}

enum ATMTaskQuery {
    /// 最近完成 is a pure time window — no count cap. A cap truncated mid-day, so
    /// part of a day's work silently showed up under 完成历史.
    static let recentCompletionDays = 7

    /// Status sections in list order. 待验收 is first — it is the human gate.
    /// 暂不处理 sits near the bottom: parked work, not a current queue.
    static let groupSpecs: [(id: String, title: String)] = [
        ("review", "待验收"),
        ("working", "工作中"),
        ("waiting", "等待中"),
        ("blocked", "阻塞"),
        ("open", "待开始"),
        ("deferred", "暂不处理"),
        ("done", "最近完成"),
        ("dropped", "已放弃"),
        ("history", "完成历史"),
    ]

    private static let completionDayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = .current
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    static func apply(_ todos: [ATMTodo], query: String) -> [ATMTodo] {
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !needle.isEmpty else { return todos }
        return todos.filter { todo in
            [todo.id, todo.title, todo.description ?? "", todo.project ?? "", todo.source ?? ""]
                .contains { $0.lowercased().contains(needle) }
        }
    }

    static func visibleTodos(from todos: [ATMTodo], showsDropped: Bool) -> [ATMTodo] {
        guard !showsDropped else { return todos }
        return todos.filter { $0.status != "dropped" }
    }

    /// Newest first within a status group. `created` is YYYY-MM-DD; id breaks ties.
    static func sortedByCreatedDescending(_ todos: [ATMTodo]) -> [ATMTodo] {
        todos.sorted { left, right in
            if left.created != right.created {
                return left.created > right.created
            }
            return left.id > right.id
        }
    }

    static func completionDay(for todo: ATMTodo) -> String {
        if let closed = todo.closed?.trimmingCharacters(in: .whitespacesAndNewlines),
           !closed.isEmpty {
            return closed
        }
        if let doneTS = todo.doneTS {
            return completionDayFormatter.string(
                from: Date(timeIntervalSince1970: TimeInterval(doneTS))
            )
        }
        return todo.created
    }

    /// Newest completion first. Day is the primary key because that is what the
    /// 最近完成 / 完成历史 split is cut on; `done_ts` then orders within a day, and a
    /// numeric id is the fallback for rows closed before timestamps existed —
    /// comparing ids as strings put t99 above t100.
    static func sortedByCompletionDescending(_ todos: [ATMTodo]) -> [ATMTodo] {
        todos.sorted { completionSortKey($0) > completionSortKey($1) }
    }

    /// One total key rather than a chain of pairwise rules: mixing “compare by
    /// done_ts when both have one, else by id” is not a consistent ordering and
    /// sorts unpredictably. Rows with no timestamp land at the end of their day.
    private static func completionSortKey(_ todo: ATMTodo) -> (String, Int64, Int, String) {
        (completionDay(for: todo), todo.doneTS ?? 0, numericID(todo.id), todo.id)
    }

    private static func numericID(_ id: String) -> Int {
        Int(id.drop { !$0.isNumber }) ?? -1
    }

    static func groups(
        from todos: [ATMTodo],
        now: Date = Date()
    ) -> [(id: String, title: String, todos: [ATMTodo])] {
        let review = todos.filter { $0.status == "review" }
        let working = todos.filter { $0.status == "in_progress" }
        let deferred = todos.filter(ATMTodoStatusStyle.isDeferred)
        let waiting = todos.filter { $0.status == "waiting" && !ATMTodoStatusStyle.isDeferred($0) }
        let blocked = todos.filter { $0.status == "blocked" }
        let open = todos.filter { $0.status == "open" }
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: now)
        let cutoff = calendar.date(
            byAdding: .day,
            value: -(recentCompletionDays - 1),
            to: today
        ) ?? today
        let cutoffDay = completionDayFormatter.string(from: cutoff)
        let completed = sortedByCompletionDescending(todos.filter { $0.status == "done" })
        let done = completed.filter { completionDay(for: $0) >= cutoffDay }
        let history = completed.filter { completionDay(for: $0) < cutoffDay }
        let dropped = todos.filter { $0.status == "dropped" }
        let buckets: [String: [ATMTodo]] = [
            "review": review,
            "working": working,
            "waiting": waiting,
            "blocked": blocked,
            "open": open,
            "deferred": deferred,
            "done": done,
            "dropped": dropped,
            "history": history,
        ]
        let completionGroups: Set<String> = ["done", "dropped", "history"]
        return groupSpecs.compactMap { spec in
            let items = completionGroups.contains(spec.id)
                ? sortedByCompletionDescending(buckets[spec.id] ?? [])
                : sortedByCreatedDescending(buckets[spec.id] ?? [])
            guard !items.isEmpty else { return nil }
            return (spec.id, spec.title, items)
        }
    }

    static func preferredDefault(in todos: [ATMTodo]) -> ATMTodo? {
        // Match list order: human review first, then active work; newest within rank.
        // Deferred/parked work ranks just above closed.
        let statusRank: (ATMTodo) -> Int = { todo in
            if ATMTodoStatusStyle.isDeferred(todo) { return 5 }
            switch todo.status {
            case "review": return 0
            case "blocked": return 1
            case "in_progress": return 2
            case "waiting": return 3
            case "open": return 4
            case "done": return 6
            case "dropped": return 7
            default: return 99
            }
        }
        let priorityRank = ["P0": 0, "P1": 1, "P2": 2]
        return todos.min { left, right in
            let leftStatus = statusRank(left)
            let rightStatus = statusRank(right)
            if leftStatus != rightStatus { return leftStatus < rightStatus }
            let leftPriority = priorityRank[left.priority] ?? 99
            let rightPriority = priorityRank[right.priority] ?? 99
            if leftPriority != rightPriority { return leftPriority < rightPriority }
            if left.created != right.created { return left.created > right.created }
            return left.id > right.id
        }
    }
}

private struct ATMTaskGroup: Identifiable {
    let id: String
    let title: String
    let todos: [ATMTodo]
}

@MainActor
final class ATMDesktopNavigation: ObservableObject {
    @Published var section: ATMDesktopSection = .tasks
    @Published var selectedTodoID: String?
    @Published var selectedCollectionSourceID: String?
    @Published var selectedCollectionItemID: String?
    @Published var selectedAgentID: String?
    @Published var selectedKnowledgeLibraryID: String?
    /// A document id ("document:<id>") the knowledge library view should
    /// reveal and select, set when locating a result from global search.
    @Published var locateKnowledgeDocumentID: String?
    /// Bumped to ask the knowledge view to open its “new document” sheet
    /// for the currently selected library (right-click “在此新建知识”).
    @Published var knowledgeCreateRequest = 0
    /// Add-task modal is owned at the desktop root so the dimmed backdrop
    /// covers the sidebar and centers over the whole window.
    @Published var showAddTodo = false
}

struct ATMCollectionRef: Identifiable, Equatable {
    let id: String
    let name: String
    let count: Int
}

struct ATMTextAlert: Identifiable {
    let id = UUID()
    let message: String
}

/// ATM's app-wide navigation rail is the one persistent piece of brand chrome.
/// It has its own foreground tokens because system label colours are tuned for
/// the light working canvas and disappear on the dark rail in light appearance.
private struct ATMDesktopRailSurfaceModifier: ViewModifier {
    let isSelected: Bool
    var isNested = false

    @State private var isHovered = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, isNested ? 8 : 10)
            .frame(maxWidth: .infinity, minHeight: isNested ? 28 : 34, alignment: .leading)
            .foregroundStyle(isSelected ? ATMTheme.railPrimary : ATMTheme.railSecondary)
            .background(fill, in: RoundedRectangle(cornerRadius: isNested ? 7 : 9, style: .continuous))
            .contentShape(Rectangle())
            .onHover { isHovered = $0 }
    }

    private var fill: Color {
        if isSelected { return ATMTheme.railSelected }
        if isHovered { return ATMTheme.railRaised }
        return .clear
    }
}

private extension View {
    func atmDesktopRailSurface(isSelected: Bool, isNested: Bool = false) -> some View {
        modifier(ATMDesktopRailSurfaceModifier(isSelected: isSelected, isNested: isNested))
    }
}

struct DesktopContentView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    // Sidebar disclosure is independent from the selected workspace. Switching
    // to Usage (or another section) must not collapse an expanded library list.
    @State private var knowledgeExpanded = false
    @State private var showingCollectionCreate = false
    @State private var newCollectionID = ""
    @State private var newCollectionName = ""
    @State private var renameCollectionTarget: ATMCollectionRef?
    @State private var renameCollectionName = ""
    @State private var deleteCollectionTarget: ATMCollectionRef?
    @State private var collectionError: String?
    @State private var showingSearch = false

    var body: some View {
        HStack(spacing: 0) {
            desktopSidebar
                .frame(width: 212, alignment: .leading)
                .clipped()
                .background {
                    // Paint behind the transparent title bar outside the
                    // sidebar's content clip so the rail reaches the window edge.
                    ATMTheme.rail
                        .ignoresSafeArea(edges: .top)
                }
            Rectangle()
                .fill(ATMTheme.railBorder)
                .frame(width: 1)
            Group {
                switch navigation.section {
                case .tasks:
                    DesktopTasksView(store: store, navigation: navigation)
                case .collection:
                    DesktopCollectionView(store: store, navigation: navigation)
                case .agents:
                    DesktopAgentsView(store: store, navigation: navigation)
                case .knowledge:
                    DesktopKnowledgeView(store: store, navigation: navigation)
                        .id("knowledge-library")
                case .usage:
                    DesktopUsageView(store: store)
                case .settings:
                    DesktopSettingsView(store: store)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(ATMTheme.canvas)
        .frame(minWidth: 880, minHeight: 620)
        .onChange(of: store.knowledgeCollections.map(\.id)) { _ in
            selectDefaultKnowledgeLibraryIfNeeded()
        }
        // Full-window modal: covers sidebar + content and centers the card.
        .overlay {
            if navigation.showAddTodo {
                ZStack {
                    Color.black.opacity(0.28)
                        .ignoresSafeArea()
                        .contentShape(Rectangle())
                        .onTapGesture { navigation.showAddTodo = false }
                    DesktopAddTodoSheet(
                        store: store,
                        onCancel: { navigation.showAddTodo = false }
                    ) { draft in
                        store.addTodo(draft) { createdID in
                            navigation.section = .tasks
                            navigation.selectedTodoID = createdID
                        }
                        navigation.showAddTodo = false
                    }
                    .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .overlay(
                        RoundedRectangle(cornerRadius: 12, style: .continuous)
                            .stroke(ATMTheme.border)
                    )
                    .shadow(color: .black.opacity(0.18), radius: 24, y: 10)
                }
                .transition(.opacity)
            }
        }
        .animation(.easeInOut(duration: 0.15), value: navigation.showAddTodo)
        .sheet(isPresented: $showingCollectionCreate) {
            collectionCreateSheet
        }
        .sheet(isPresented: $showingSearch) {
            DesktopSearchPalette(store: store, navigation: navigation)
        }
        .alert(
            "重命名知识库",
            isPresented: Binding(
                get: { renameCollectionTarget != nil },
                set: { if !$0 { renameCollectionTarget = nil } }
            )
        ) {
            TextField("显示名称", text: $renameCollectionName)
            Button("保存") {
                if let target = renameCollectionTarget { renameCollection(target, to: renameCollectionName) }
                renameCollectionTarget = nil
            }
            Button("取消", role: .cancel) { renameCollectionTarget = nil }
        } message: {
            Text("仅修改显示名称（标识 ID 不变）。")
        }
        .confirmationDialog(
            "删除知识库 \(deleteCollectionTarget?.name ?? "")？",
            isPresented: Binding(
                get: { deleteCollectionTarget != nil },
                set: { if !$0 { deleteCollectionTarget = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let target = deleteCollectionTarget {
                if target.count > 0 {
                    Button("将 \(target.count) 篇移至 inbox 后删除") {
                        deleteCollection(target, force: false, moveTo: "inbox")
                        deleteCollectionTarget = nil
                    }
                    Button("连同 \(target.count) 篇文档强制删除", role: .destructive) {
                        deleteCollection(target, force: true, moveTo: nil)
                        deleteCollectionTarget = nil
                    }
                } else {
                    Button("删除", role: .destructive) {
                        deleteCollection(target, force: false, moveTo: nil)
                        deleteCollectionTarget = nil
                    }
                }
            }
            Button("取消", role: .cancel) { deleteCollectionTarget = nil }
        }
        .alert(item: Binding(
            get: { collectionError.map { ATMTextAlert(message: $0) } },
            set: { if $0 == nil { collectionError = nil } }
        )) { alert in
            Alert(title: Text("操作失败"), message: Text(alert.message), dismissButton: .default(Text("好")))
        }
    }

    private var collectionCreateSheet: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("新建知识库")
                .font(ATMFont.font(.title3, weight: .semibold))
            VStack(alignment: .leading, spacing: 4) {
                Text("标识 ID").font(ATMFont.font(.footnote, weight: .semibold)).foregroundStyle(ATMTheme.secondary)
                TextField("例如 research（小写字母/数字/-/_）", text: $newCollectionID)
                    .textFieldStyle(.roundedBorder)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text("显示名称（可选）").font(ATMFont.font(.footnote, weight: .semibold)).foregroundStyle(ATMTheme.secondary)
                TextField("默认用标识 ID", text: $newCollectionName)
                    .textFieldStyle(.roundedBorder)
            }
            HStack {
                Spacer()
                Button("取消") { showingCollectionCreate = false }
                Button("创建") { createCollection() }
                    .buttonStyle(.borderedProminent)
                    .disabled(newCollectionID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 420)
    }

    private func createCollection() {
        let id = newCollectionID.trimmingCharacters(in: .whitespacesAndNewlines)
        let name = newCollectionName
        Task {
            do {
                try await store.createCollection(id: id, name: name)
                store.refreshKnowledgeCatalog()
                await MainActor.run {
                    showingCollectionCreate = false
                    newCollectionID = ""
                    newCollectionName = ""
                    navigation.section = .knowledge
                    navigation.selectedKnowledgeLibraryID = id
                }
            } catch {
                await MainActor.run { collectionError = error.localizedDescription }
            }
        }
    }

    private func renameCollection(_ target: ATMCollectionRef, to name: String) {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != target.name else { return }
        Task {
            do {
                try await store.renameCollectionName(id: target.id, name: trimmed)
                store.refreshKnowledgeCatalog()
            } catch {
                await MainActor.run { collectionError = error.localizedDescription }
            }
        }
    }

    private func deleteCollection(_ target: ATMCollectionRef, force: Bool, moveTo: String?) {
        Task {
            do {
                try await store.deleteCollection(id: target.id, force: force, moveTo: moveTo)
                store.refreshKnowledgeCatalog()
                await MainActor.run {
                    if navigation.selectedKnowledgeLibraryID == target.id {
                        navigation.selectedKnowledgeLibraryID = nil
                    }
                }
            } catch {
                await MainActor.run { collectionError = error.localizedDescription }
            }
        }
    }

    private var desktopSidebar: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 8) {
                ATMBrandMark()
                    .frame(width: 38, height: 38)
                VStack(alignment: .leading, spacing: 0) {
                    Text("ATM")
                        .font(ATMFont.font(.title3, weight: .bold))
                        .foregroundStyle(ATMTheme.railPrimary)
                    Text("工作台")
                        .font(ATMFont.body)
                        .foregroundStyle(ATMTheme.railSecondary)
                }
                .lineLimit(1)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 14)
            .padding(.top, 16)
            .frame(maxWidth: .infinity, alignment: .leading)

            VStack(spacing: 4) {
                searchSidebarButton
                ForEach(ATMDesktopSection.allCases) { section in
                    if section == .knowledge {
                        knowledgeSidebarGroup
                    } else {
                        sidebarButton(section)
                    }
                }
            }
            .padding(.horizontal, 8)

            Spacer()

            // Session index syncs on launch, every 5 minutes, and when the
            // quick panel / desktop window opens. No permanent sidebar button.
            Text(appVersionLabel)
                .font(ATMFont.mono(.caption))
                .foregroundStyle(ATMTheme.railMuted)
                .frame(maxWidth: .infinity, alignment: .center)
                .padding(.bottom, 8)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(ATMTheme.rail)
    }

    private var appVersionLabel: String {
        let info = Bundle.main.infoDictionary
        guard let version = info?["CFBundleShortVersionString"] as? String, !version.isEmpty else {
            return "开发版"
        }
        let build = info?["CFBundleVersion"] as? String
        return build.map { "v\(version) (\($0))" } ?? "v\(version)"
    }

    private var searchSidebarButton: some View {
        Button {
            showingSearch = true
        } label: {
            HStack(spacing: 9) {
                Image(systemName: "magnifyingglass").frame(width: 18)
                Text("搜索")
                Spacer()
                Text("⌘K")
                    .font(ATMFont.mono(.footnote, .medium))
                    .foregroundStyle(ATMTheme.railMuted)
            }
            .font(ATMFont.font(.body, weight: .medium))
            .foregroundStyle(ATMTheme.railPrimary)
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity, minHeight: 36, alignment: .leading)
            .background(ATMTheme.railRaised, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .stroke(ATMTheme.railBorder)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .keyboardShortcut("k", modifiers: .command)
    }

    private func sidebarButton(_ section: ATMDesktopSection) -> some View {
        let selected = navigation.section == section
        return Button {
            navigation.section = section
        } label: {
            HStack(spacing: 9) {
                Image(systemName: section.icon).frame(width: 18)
                Text(section.title)
                Spacer()
            }
            .font(ATMFont.font(.body, weight: .medium))
            .atmDesktopRailSurface(isSelected: selected)
        }
        .buttonStyle(.plain)
    }

    private var knowledgeSidebarGroup: some View {
        VStack(spacing: 2) {
            let selected = navigation.section == .knowledge
            Button {
                if selected {
                    knowledgeExpanded.toggle()
                } else {
                    navigation.section = .knowledge
                    knowledgeExpanded = true
                    store.refreshKnowledgeCatalog()
                    if navigation.selectedKnowledgeLibraryID == nil {
                        navigation.selectedKnowledgeLibraryID =
                            sortedKnowledgeCollections.first?.id ?? ATMKnowledgeLibrary.memoryID
                    }
                }
            } label: {
                HStack(spacing: 9) {
                    Image(systemName: ATMDesktopSection.knowledge.icon).frame(width: 18)
                    Text(ATMDesktopSection.knowledge.title)
                    Spacer()
                    Image(systemName: knowledgeExpanded ? "chevron.down" : "chevron.right")
                        .font(ATMFont.font(.micro, weight: .semibold))
                }
                .font(ATMFont.font(.body, weight: .medium))
                .atmDesktopRailSurface(isSelected: selected)
            }
            .buttonStyle(.plain)

            if knowledgeExpanded {
                ScrollView(.vertical, showsIndicators: false) {
                    VStack(spacing: 1) {
                        knowledgeLibraryButton(
                            id: ATMKnowledgeLibrary.memoryID,
                            title: "共享记忆",
                            count: nil,
                            icon: "brain.head.profile"
                        )

                        Divider().padding(.leading, 28).padding(.vertical, 3)

                        ForEach(sortedKnowledgeCollections) { collection in
                            knowledgeLibraryButton(
                                id: collection.id,
                                title: collection.name,
                                count: collection.documentCount,
                                icon: collection.id == "inbox" ? "tray" : "folder"
                            )
                        }

                        if store.isKnowledgeCatalogLoading {
                            ProgressView()
                                .controlSize(.small)
                                .frame(maxWidth: .infinity)
                            .padding(.vertical, 4)
                        }
                    }
                    .padding(.trailing, 8)
                }
                .padding(.leading, 14)
                .frame(maxHeight: 172)
                .overlay(alignment: .bottom) {
                    if sortedKnowledgeCollections.count > 5 {
                        LinearGradient(
                            colors: [ATMTheme.rail.opacity(0), ATMTheme.rail.opacity(0.96)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                        .frame(height: 16)
                        .allowsHitTesting(false)
                    }
                }
            }
        }
    }

    private var sortedKnowledgeCollections: [ATMKnowledgeCollection] {
        store.knowledgeCollections.sorted {
            if $0.id == "inbox" { return true }
            if $1.id == "inbox" { return false }
            return $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
    }

    private func knowledgeLibraryButton(id: String, title: String, count: Int?, icon: String) -> some View {
        let selected = navigation.section == .knowledge && navigation.selectedKnowledgeLibraryID == id
        return Button {
            navigation.section = .knowledge
            navigation.selectedKnowledgeLibraryID = id
        } label: {
            HStack(spacing: 7) {
                Image(systemName: icon)
                    .frame(width: 15)
                Text(title)
                    .lineLimit(1)
                Spacer(minLength: 4)
                if let count {
                    Text("\(count)")
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(ATMTheme.railMuted)
                }
            }
            .font(ATMFont.font(.footnote, weight: selected ? .semibold : .regular))
            .atmDesktopRailSurface(isSelected: selected, isNested: true)
        }
        .buttonStyle(.plain)
        .help(title)
        .atmRightClickMenu {
            ATMMenuItem("新建知识库…") {
                newCollectionID = ""
                newCollectionName = ""
                showingCollectionCreate = true
            }
            if id != ATMKnowledgeLibrary.memoryID {
                ATMMenuItem("在此新建知识…") {
                    navigation.section = .knowledge
                    navigation.selectedKnowledgeLibraryID = id
                    navigation.knowledgeCreateRequest += 1
                }
                ATMMenuSeparator()
                ATMMenuItem("重命名…") {
                    renameCollectionName = title
                    renameCollectionTarget = ATMCollectionRef(id: id, name: title, count: count ?? 0)
                }
                ATMMenuItem("删除…", destructive: true) {
                    deleteCollectionTarget = ATMCollectionRef(id: id, name: title, count: count ?? 0)
                }
            }
            ATMMenuSeparator()
            ATMMenuItem("刷新目录") { store.refreshKnowledgeCatalog() }
            ATMMenuItem("复制知识库 ID") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(id, forType: .string)
            }
        }
    }

    private func selectDefaultKnowledgeLibraryIfNeeded() {
        if let selected = navigation.selectedKnowledgeLibraryID,
           selected == ATMKnowledgeLibrary.memoryID ||
           store.knowledgeCollections.contains(where: { $0.id == selected }) {
            return
        }
        if store.knowledgeCollections.contains(where: { $0.id == "atm" }) {
            navigation.selectedKnowledgeLibraryID = "atm"
        } else if let first = sortedKnowledgeCollections.first {
            navigation.selectedKnowledgeLibraryID = first.id
        } else {
            navigation.selectedKnowledgeLibraryID = ATMKnowledgeLibrary.memoryID
        }
    }

}

private struct DesktopTasksView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation

    @State private var deleteCandidate: ATMTodo?
    @AppStorage("ATMCollapsedTaskGroups")
    private var collapsedGroupsRaw = "done,deferred,dropped,history"
    @AppStorage("ATMDidApplyDefaultCollapsedTaskGroups") private var didApplyDefaultCollapsedGroups = false
    @AppStorage("ATMDidApplyClosedTaskGroupsV2") private var didApplyClosedTaskGroupsV2 = false
    @AppStorage(ATMTodoListPreferences.showDroppedKey)
    private var showsDropped = ATMTodoListPreferences.defaultShowsDropped

    private var collapsedGroups: Set<String> {
        Set(collapsedGroupsRaw.split(separator: ",").map(String.init))
    }

    private func expandedBinding(for group: ATMTaskGroup) -> Binding<Bool> {
        Binding(
            get: { !collapsedGroups.contains(group.id) },
            set: { expanded in
                var set = collapsedGroups
                if expanded { set.remove(group.id) } else { set.insert(group.id) }
                collapsedGroupsRaw = set.sorted().joined(separator: ",")
            }
        )
    }

    @ViewBuilder
    private func groupHeader(_ group: ATMTaskGroup, expanded: Binding<Bool>) -> some View {
        Button {
            withAnimation(.easeInOut(duration: 0.15)) { expanded.wrappedValue.toggle() }
        } label: {
            HStack {
                ATMDrawerDisclosureLabel(
                    title: group.title,
                    count: group.todos.count,
                    tint: groupAccent(group.id),
                    isExpanded: expanded.wrappedValue
                )
                Spacer()
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func groupAccent(_ id: String) -> Color {
        switch id {
        case "review": return ATMTodoStatusStyle.color(forStatus: "review")
        case "working": return ATMTodoStatusStyle.color(forStatus: "in_progress")
        case "waiting": return ATMTodoStatusStyle.color(forStatus: "waiting")
        case "blocked": return ATMTodoStatusStyle.color(forStatus: "blocked")
        case "done", "history": return ATMTodoStatusStyle.color(forStatus: "done")
        default: return ATMTheme.secondary
        }
    }

    private var todos: [ATMTodo] {
        store.allTodos
    }

    private var visibleTodos: [ATMTodo] {
        ATMTaskQuery.visibleTodos(from: todos, showsDropped: showsDropped)
    }

    private var selectedTodo: ATMTodo? {
        guard let id = navigation.selectedTodoID else { return nil }
        return visibleTodos.first { $0.id == id }
    }

    private var groups: [ATMTaskGroup] {
        ATMTaskQuery.groups(from: visibleTodos).map {
            ATMTaskGroup(id: $0.id, title: $0.title, todos: $0.todos)
        }
    }

    var body: some View {
        HSplitView {
            taskList
                .frame(minWidth: 260, idealWidth: 330, maxWidth: 420)

            Group {
                if let todo = selectedTodo {
                    DesktopTodoDetail(todo: todo, store: store)
                        // Identity is the Todo id alone. Folding title / description /
                        // status into it recreated the view on every background sync
                        // that touched them, which reset the selected tab and threw
                        // away an open edit form mid-typing. The form seeds itself
                        // from `todo` when 编辑 is picked, so it opens on current
                        // values without needing a fresh identity to do it.
                        .id(todo.id)
                } else {
                    VStack(spacing: 10) {
                        Image(systemName: "checklist")
                            .font(ATMFont.font(.display, weight: .light))
                            .foregroundStyle(ATMTheme.secondary)
                        Text("选择一个任务")
                            .font(ATMFont.font(.title3, weight: .semibold))
                        Text("从左侧列表查看详情、编辑 Markdown 或执行快捷操作。")
                            .font(ATMFont.body)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .frame(minWidth: 400, maxWidth: .infinity, maxHeight: .infinity)
            .background(ATMTheme.canvas)
        }
        .onAppear {
            applyDefaultCollapsedGroupsIfNeeded()
            // First paint may pre-select from a notification or keep a prior ID
            // before this view subscribed to selection changes — reveal once.
            selectFirstIfNeeded()
            revealSelectedGroup()
        }
        // Adding/refreshing todos must not re-expand groups the user collapsed
        // (e.g. 已完成). Only pick a default when the current selection is gone;
        // reveal stays on selection change / first appear.
        .onChange(of: todos.map(\.id)) { _ in selectFirstIfNeeded() }
        .onChange(of: showsDropped) { _ in selectFirstIfNeeded() }
        .onChange(of: navigation.selectedTodoID) { _ in revealSelectedGroup() }
    }

    private var taskList: some View {
        VStack(spacing: 0) {
            ATMDrawerHeader(title: "我的任务", count: visibleTodos.count) {
                Button {
                    navigation.showAddTodo = true
                } label: {
                    Label("新建", systemImage: "plus")
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
                .help("添加任务 (⌘N)")
                .keyboardShortcut("n", modifiers: .command)
            }

            if let error = store.errorMessage {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    // Wraps rather than truncates: the version-mismatch message
                    // ends in the command to run, and a clipped instruction is no
                    // instruction.
                    .fixedSize(horizontal: false, vertical: true)
                    .multilineTextAlignment(.leading)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 8)
            }

            List {
                ForEach(groups) { group in
                    let expanded = expandedBinding(for: group)
                    Section {
                        if expanded.wrappedValue {
                            ForEach(group.todos) { todo in
                                Button {
                                    navigation.selectedTodoID = todo.id
                                } label: {
                                    DesktopTodoRow(
                                        todo: todo,
                                        isSelected: navigation.selectedTodoID == todo.id
                                    )
                                }
                                    .buttonStyle(.plain)
                                    .focusable(false)
                                    .listRowInsets(EdgeInsets(top: 2, leading: 8, bottom: 2, trailing: 8))
                                    .listRowBackground(Color.clear)
                                    .atmRightClickMenu { todoMenuEntries(for: todo) }
                            }
                        }
                    } header: {
                        groupHeader(group, expanded: expanded)
                    }
                }
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            .overlay {
                if visibleTodos.isEmpty {
                    VStack(spacing: 7) {
                        Image(systemName: "magnifyingglass")
                            .font(ATMFont.font(.title2, weight: .light))
                        Text("没有匹配的任务")
                            .font(ATMFont.font(.body, weight: .medium))
                    }
                    .foregroundStyle(ATMTheme.secondary)
                    .allowsHitTesting(false)
                }
            }
        }
        .background(ATMTheme.listPane)
        .confirmationDialog(
            "删除 \(deleteCandidate?.id.uppercased() ?? "")？",
            isPresented: Binding(
                get: { deleteCandidate != nil },
                set: { if !$0 { deleteCandidate = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let todo = deleteCandidate {
                Button("永久删除", role: .destructive) {
                    store.perform(.delete, on: todo)
                    deleteCandidate = nil
                }
            }
            Button("取消", role: .cancel) { deleteCandidate = nil }
        } message: {
            Text(deleteCandidate?.title ?? "").font(ATMFont.body)
        }
    }

    @ATMMenuBuilder
    private func todoMenuEntries(for todo: ATMTodo) -> [ATMMenuEntry] {
        ATMMenuItem("用 VS Code 打开项目", systemImage: "chevron.left.forwardslash.chevron.right") {
            store.openTodoProjectInVSCode(todo)
        }
        ATMMenuSeparator()
        for item in ATMTodoStatusActions.items(for: todo) {
            ATMMenuItem(item.title, systemImage: item.systemImage) {
                store.perform(item.action, on: todo)
            }
        }
        if let links = todo.links, !links.isEmpty {
            ATMMenuSeparator()
            ATMMenuSubmenu("打开链接") {
                for link in links {
                    ATMMenuItem(link.title ?? link.url) {
                        if let url = URL(string: link.url) {
                            NSWorkspace.shared.open(url)
                        }
                    }
                }
            }
        }
        ATMMenuSeparator()
        ATMMenuItem("复制 ID") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(todo.id, forType: .string)
        }
        if ATMTodoStatusActions.showsLaunchPrompt(for: todo) {
            ATMMenuItem("复制启动提示") {
                Task {
                    guard let prompt = await store.launchPrompt(for: todo) else { return }
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(prompt, forType: .string)
                }
            }
        }
        ATMMenuSeparator()
        ATMMenuItem("删除…", destructive: true) { deleteCandidate = todo }
    }

    private func selectFirstIfNeeded() {
        if let selected = navigation.selectedTodoID,
           visibleTodos.contains(where: { $0.id == selected }) {
            // Keep the user's group collapse state. Reveal only when selection
            // changes (see onChange) or on first appear.
            return
        }
        navigation.selectedTodoID = ATMTaskQuery.preferredDefault(in: visibleTodos)?.id
    }

    private func applyDefaultCollapsedGroupsIfNeeded() {
        var set = collapsedGroups
        if !didApplyDefaultCollapsedGroups {
            set.insert("done")
            set.insert("deferred")
            didApplyDefaultCollapsedGroups = true
        }
        if !didApplyClosedTaskGroupsV2 {
            set.insert("dropped")
            set.insert("history")
            didApplyClosedTaskGroupsV2 = true
        }
        collapsedGroupsRaw = set.sorted().joined(separator: ",")
    }

    private func revealSelectedGroup() {
        guard let selected = navigation.selectedTodoID,
              let group = groups.first(where: { group in group.todos.contains(where: { $0.id == selected }) }),
              collapsedGroups.contains(group.id) else { return }
        var set = collapsedGroups
        set.remove(group.id)
        collapsedGroupsRaw = set.sorted().joined(separator: ",")
    }
}

private struct DesktopTodoRow: View {
    let todo: ATMTodo
    let isSelected: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            // Icon only — the section header already names the status, so no label.
            // Working tasks show a spinner instead of a static play glyph.
            ATMTodoStatusGlyph(todo: todo, tier: .body)
                .frame(width: 28, height: 28)
                .background(statusColor.opacity(0.10), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            VStack(alignment: .leading, spacing: 5) {
                // The id leads the title rather than sitting in the meta line: it
                // is what you scan the list for and what you type back at the CLI,
                // and below the title it was the one thing you had to look away to
                // find. Mono and baseline-aligned so it reads as a label on the
                // title, not as its first word.
                //
                // Its tint is the priority — see ATMTodoPriorityStyle. A closed
                // todo drops to secondary regardless: a finished P0 is not urgent,
                // and red on a struck-through row reads as a problem.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(todo.id.uppercased())
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(
                            isClosed
                                ? ATMTheme.secondary
                                : ATMTodoPriorityStyle.color(for: todo.priority)
                        )
                        // Color alone is not readable — the priority stays
                        // available by hover and to accessibility.
                        .help(ATMTodoPriorityStyle.label(todo.priority))
                        .accessibilityLabel("\(todo.id.uppercased()) \(ATMTodoPriorityStyle.label(todo.priority))")
                    Text(todo.title)
                        // Fixed weight — see ATMRowSurface: selection never changes
                        // type weight, or the title jumps as glyph widths reflow.
                        .font(ATMFont.font(.body, weight: .medium))
                        .foregroundStyle(isClosed ? ATMTheme.secondary : ATMTheme.primary)
                        .strikethrough(
                            ATMTodoStatusStyle.usesStrikethrough(for: todo),
                            color: ATMTheme.secondary
                        )
                        .lineLimit(2)
                }
                // Priority left this line for the id's tint, so the line can now be
                // empty — a todo with no project filed before `creator` existed has
                // nothing to say here. Skip it rather than leave the row padded
                // around a blank strip.
                if projectLabel != nil || creatorLabel != nil {
                    HStack(spacing: 6) {
                        if let project = projectLabel { Text(project) }
                        // Where the todo came from. The icon carries it — 收集 and
                        // agent-filed todos are the ones worth spotting, and they
                        // are spotted by glyph long before the name is read.
                        if let creator = creatorLabel {
                            Label {
                                Text(creator)
                            } icon: {
                                if let icon = ATMTodoCreator.icon(todo.creator) {
                                    Image(systemName: icon)
                                }
                            }
                        }
                    }
                    .font(ATMFont.mono(.caption, .medium))
                    .foregroundStyle(ATMTheme.secondary)
                }
            }
        }
        .atmRowSurface(isSelected: isSelected)
    }

    private var isClosed: Bool {
        todo.status == "done" || todo.status == "dropped"
    }

    private var statusColor: Color { ATMTodoStatusStyle.color(for: todo) }

    private var projectLabel: String? {
        guard let project = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines),
              !project.isEmpty else { return nil }
        return project
    }

    private var creatorLabel: String? { ATMTodoCreator.shortLabel(todo.creator) }
}

struct DesktopTodoDetail: View {
    private enum DetailTab: String, CaseIterable {
        case detail
        case sessions
    }

    let todo: ATMTodo
    @ObservedObject var store: ATMDataStore

    @State private var isEditing = false
    @State private var selectedTab: DetailTab = .detail
    @State private var copiedPrompt = false
    @State private var deleteCandidate: ATMTodo?
    @State private var isEditingSource = false
    @State private var title = ""
    @State private var description = ""
    @State private var priority = "P1"
    @State private var project = ""
    @State private var status = "open"
    @State private var wakeCondition = ""
    @State private var reviewAt = ""
    @State private var source = ""

    private static let reviewDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    var body: some View {
        VStack(spacing: 0) {
            if isEditing {
                editHeader
                Divider()
                editContent
            } else {
                detailHeader
                detailTabs
                if selectedTab == .detail {
                    readContent
                } else {
                    sessionContent
                }
            }
        }
        .background(ATMTheme.canvas)
        .onAppear { store.loadBoundSessions(for: todo.id) }
        .onChange(of: store.snapshot.refreshedAt) { _ in
            store.loadBoundSessions(for: todo.id)
        }
        .confirmationDialog(
            "删除 \(deleteCandidate?.id.uppercased() ?? "")？",
            isPresented: Binding(
                get: { deleteCandidate != nil },
                set: { if !$0 { deleteCandidate = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let target = deleteCandidate {
                Button("永久删除", role: .destructive) {
                    store.perform(.delete, on: target)
                    deleteCandidate = nil
                }
            }
            Button("取消", role: .cancel) { deleteCandidate = nil }
        } message: {
            Text(deleteCandidate?.title ?? "").font(ATMFont.body)
        }
    }

    private var detailTabs: some View {
        HStack(spacing: 22) {
            detailTabButton(.detail, title: "任务描述", icon: "doc.text")
            detailTabButton(.sessions, title: sessionTabTitle, icon: "terminal")
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 24)
        .frame(height: 46)
        .background(ATMTheme.elevated)
        .overlay(alignment: .bottom) { Rectangle().fill(ATMTheme.border).frame(height: 1) }
    }

    private func detailTabButton(_ tab: DetailTab, title: String, icon: String) -> some View {
        let selected = selectedTab == tab
        return Button {
            selectedTab = tab
        } label: {
            Label(title, systemImage: icon)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(selected ? ATMTheme.primary : ATMTheme.secondary)
                .padding(.horizontal, 2)
                .frame(height: 46)
                .overlay(alignment: .bottom) {
                    Capsule()
                        .fill(selected ? ATMTheme.accent : Color.clear)
                        .frame(height: 2)
                }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var sessionTabTitle: String {
        let count = store.boundSessions(for: todo.id).count
        return count == 0 ? "Agent Sessions" : "Agent Sessions \(count)"
    }

    private var detailHeader: some View {
        VStack(alignment: .leading, spacing: 15) {
            HStack(spacing: 7) {
                Label(todo.project ?? "未分项目", systemImage: "folder")
                Image(systemName: "chevron.right")
                    .font(ATMFont.font(.micro, weight: .semibold))
                Text(todo.id.uppercased())
                    .font(ATMFont.mono(.footnote, .semibold))
                    .foregroundStyle(ATMTheme.accent)
                Spacer(minLength: 10)
                detailActions
            }
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)

            HStack(alignment: .top, spacing: 10) {
                statusBadge
                    .padding(.top, 2)
                Text(todo.title)
                    .font(ATMFont.font(.title1, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                if store.isActing { ProgressView().controlSize(.small) }
            }

            LazyVGrid(
                columns: Array(
                    repeating: GridItem(.flexible(minimum: 84), spacing: 8),
                    count: 3
                ),
                spacing: 8
            ) {
                propertyCell("项目", value: todo.project ?? "未分项目", icon: "folder")
                propertyCell(
                    "优先级",
                    value: ATMTodoPriorityStyle.label(todo.priority),
                    icon: "flag",
                    valueColor: priorityColor
                )
                propertyCell("创建", value: todo.created, icon: "calendar")
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 17)
        .padding(.bottom, 18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated)
    }

    @ViewBuilder
    private var detailActions: some View {
        HStack(spacing: 3) {
            let inline = ATMTodoStatusActions.inlineItems(for: todo)
            let overflow = ATMTodoStatusActions.overflowItems(for: todo)
            ForEach(inline) { item in
                actionButton(item.systemImage, help: item.help) {
                    store.perform(item.action, on: todo)
                }
            }
            if ATMTodoStatusActions.showsLaunchPrompt(for: todo) {
                actionButton(
                    copiedPrompt ? "checkmark" : "doc.on.doc",
                    help: copiedPrompt ? "已复制启动提示" : "复制启动提示"
                ) {
                    copyLaunchPrompt(for: todo)
                }
            }
            overflowMenu(overflow: overflow, todo: todo)
        }
    }

    private func propertyCell(
        _ label: String,
        value: String,
        icon: String,
        valueColor: Color = ATMTheme.primary
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Label(label, systemImage: icon)
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
            Text(value)
                .font(ATMFont.font(.footnote, weight: .semibold))
                .foregroundStyle(valueColor)
                .lineLimit(1)
        }
        .padding(.horizontal, 11)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.listPane, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
    }

    /// Edit mode gets its own header rather than reusing the reading one: the read
    /// header's priority and status badges would sit right above the fields that
    /// edit them, showing the pre-edit value until save. Only the ID survives —
    /// it is the one thing the form does not own.
    private var editHeader: some View {
        HStack(spacing: 8) {
            badge(todo.id.uppercased(), ATMTheme.accent)
            Text("编辑中")
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
            if store.isActing { ProgressView().controlSize(.small) }
            Spacer(minLength: 12)
            Button("取消") { isEditing = false }
                .keyboardShortcut(.cancelAction)
                .help("放弃修改 (⎋)")
            Button("保存") { saveEdit() }
                .buttonStyle(.borderedProminent)
                // ⌘⏎ rather than ⏎: the description editor needs Return for newlines,
                // so a bare Return shortcut only ever fired when focus was elsewhere.
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(!canSaveEdit)
                .help("保存修改 (⌘⏎)")
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated)
    }

    private var readContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                if let nextAction = latestNextAction {
                    nextActionBanner(nextAction)
                }

                if let description = nonEmpty(todo.description) {
                    detailCard("任务描述", icon: "text.alignleft") {
                        DesktopTodoDescription(source: description)
                    }
                }

                detailCard("动态", icon: "clock.arrow.circlepath") {
                    TodoProgressView(todo: todo, store: store)
                }

                if let links = todo.links, !links.isEmpty {
                    detailCard("关联链接", icon: "link") {
                        VStack(alignment: .leading, spacing: 8) {
                            ForEach(Array(links.enumerated()), id: \.offset) { _, link in
                                if let url = URL(string: link.url) {
                                    Link(destination: url) {
                                        Label(link.title ?? link.url, systemImage: "arrow.up.right.square")
                                            .font(ATMFont.font(.body, weight: .medium))
                                            .lineLimit(2)
                                    }
                                }
                            }
                        }
                    }
                }

            }
            .padding(16)
            .frame(maxWidth: 860, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private var sessionContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                detailCard("绑定历史", icon: "terminal") {
                    TodoSessionHistoryView(todo: todo, store: store)
                }
            }
            .padding(16)
            .frame(maxWidth: 860, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private var latestNextAction: String? {
        guard todo.status != "done", todo.status != "dropped" else { return nil }
        return store.progress(for: todo.id).reversed().compactMap(\.nextAction).first
    }

    /// The header already carries id / priority / status / project / created,
    /// so the read view opens on the one thing it can't show: what to do next.
    private func nextActionBanner(_ nextAction: String) -> some View {
        HStack(alignment: .center, spacing: 11) {
            Image(systemName: "arrow.up.right")
                .font(ATMFont.font(.body, weight: .semibold))
                .frame(width: 32, height: 32)
                .background(ATMTheme.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading, spacing: 3) {
                Text("下一步")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                Text(nextAction)
                    .font(ATMFont.font(.body, weight: .medium))
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            Spacer(minLength: 0)
        }
        .foregroundStyle(ATMTheme.accent)
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.accent.opacity(0.075), in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 11, style: .continuous)
                .stroke(ATMTheme.accent.opacity(0.16))
        )
    }

    /// Two tiers, because the old flat run of eight identical fields gave 来源 the
    /// same weight as 标题: the two content fields get full-width boxes, everything
    /// else is metadata inside one aligned card.
    ///
    /// Short fields first, then the description. 描述 is the one field with no upper
    /// bound on height — below it, every attribute sat under the fold, so setting a
    /// priority meant scrolling past the whole body text to reach it.
    private var editContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                editSection("标题") {
                    fieldBox {
                        TextField("任务标题", text: $title)
                            .textFieldStyle(.plain)
                            .font(ATMFont.font(.body, weight: .medium))
                    }
                }

                detailCard("属性", icon: "slider.horizontal.3") {
                    attributeGrid
                }

                editSection("描述", hint: "Markdown") {
                    TextEditor(text: $description)
                        // Body rather than mono: descriptions are mostly Chinese
                        // prose, and CJK in a monospaced face is what made the old
                        // form read as a code editor.
                        .font(ATMFont.body)
                        .scrollContentBackground(.hidden)
                        .padding(7)
                        .frame(minHeight: 200)
                        .background(ATMTheme.white, in: RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
                }
            }
            // Capped and left-aligned: stretched across a wide detail pane, a
            // single-line title field ran on for hundreds of points.
            .frame(maxWidth: 620, alignment: .leading)
            .padding(.horizontal, 18)
            .padding(.vertical, 16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// One label column, one control column — the old layout put each label and its
    /// control in a `maxWidth: .infinity` cell, so labels hugged the cell's leading
    /// edge while the intrinsically-sized popup floated in the middle of it.
    private var attributeGrid: some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 9) {
            GridRow {
                gridLabel("优先级").gridColumnAlignment(.trailing)
                segmented(selection: $priority, values: ["P0", "P1", "P2"]) { $0 }
            }
            GridRow {
                gridLabel("状态")
                Picker("", selection: $status) {
                    ForEach(["open", "in_progress", "waiting", "review", "blocked"], id: \.self) { value in
                        Text(ATMTodoStatusStyle.label(forStatus: value)).tag(value)
                    }
                }
                .labelsHidden()
                .fixedSize()
                .gridCellColumns(3)
            }
            GridRow {
                gridLabel("项目")
                TextField("未分项目", text: $project)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 190)
                    .gridCellColumns(3)
            }
            GridRow {
                gridLabel("复查日期")
                reviewDateControl.gridCellColumns(3)
            }
            // 唤醒条件 only means anything while a Todo waits, so it stays out of the
            // way otherwise — but never hides a value that is already set.
            if status == "waiting" || !wakeCondition.trimmingCharacters(in: .whitespaces).isEmpty {
                GridRow {
                    gridLabel("唤醒条件")
                    TextField("等待什么条件", text: $wakeCondition)
                        .textFieldStyle(.roundedBorder)
                        .gridCellColumns(3)
                }
            }
            GridRow {
                gridLabel("来源")
                sourceControl.gridCellColumns(3)
            }
        }
    }

    /// `atm todo edit --review-at` takes `YYYY-MM-DD` and clears on empty, which a
    /// free-text field advertised only through its placeholder. A value that does
    /// not parse falls back to text so an odd existing date is never rewritten
    /// behind the user's back.
    @ViewBuilder
    private var reviewDateControl: some View {
        let trimmed = reviewAt.trimmingCharacters(in: .whitespaces)
        if trimmed.isEmpty {
            Button("设置日期") {
                reviewAt = Self.reviewDateFormatter.string(from: Date())
            }
            .buttonStyle(.link)
            .font(ATMFont.body)
        } else if let date = Self.reviewDateFormatter.date(from: trimmed) {
            HStack(spacing: 7) {
                DatePicker(
                    "",
                    selection: Binding(
                        get: { date },
                        set: { reviewAt = Self.reviewDateFormatter.string(from: $0) }
                    ),
                    displayedComponents: .date
                )
                .labelsHidden()
                .datePickerStyle(.field)
                .fixedSize()
                Button { reviewAt = "" } label: {
                    Image(systemName: "xmark.circle.fill").font(ATMFont.body)
                }
                .buttonStyle(.plain)
                .foregroundStyle(ATMTheme.secondary)
                .help("清除复查日期")
            }
        } else {
            HStack(spacing: 7) {
                TextField("YYYY-MM-DD", text: $reviewAt)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 130)
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .help("需要 YYYY-MM-DD")
            }
        }
    }

    /// 来源 is a provenance key the collection pipeline wrote
    /// (`dingtalk:cid…:msg…`), not prose: one stray keystroke in a text field and
    /// the trail back to the original message is gone. Read-only until asked
    /// otherwise, and truncated in the middle so both the connector prefix and the
    /// message suffix stay readable.
    @ViewBuilder
    private var sourceControl: some View {
        if isEditingSource {
            TextField("来源（可选）", text: $source)
                .textFieldStyle(.roundedBorder)
                .font(ATMFont.mono(.footnote))
        } else {
            HStack(spacing: 8) {
                Text(source.isEmpty ? "无" : source)
                    .font(ATMFont.mono(.footnote))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                    .help(source)
                Spacer(minLength: 8)
                Button(source.isEmpty ? "添加" : "编辑") { isEditingSource = true }
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
            }
        }
    }

    private var canSaveEdit: Bool {
        !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !store.isActing
            && editValue != savedValue
    }

    private func saveEdit() {
        guard canSaveEdit else { return }
        store.editTodo(todo, edit: editValue)
        isEditing = false
    }

    /// Seeded when 编辑 is picked rather than in `init`, so the form always opens on
    /// the Todo's current values — including the fields (project, 来源) that
    /// the detail view's identity does not track.
    private func beginEditing() {
        title = todo.title
        description = todo.description ?? ""
        priority = todo.priority
        project = todo.project ?? ""
        status = todo.status
        wakeCondition = todo.wakeCondition ?? ""
        reviewAt = todo.reviewAt ?? ""
        source = todo.source ?? ""
        isEditingSource = false
        isEditing = true
    }

    private var editValue: ATMTodoEdit {
        ATMTodoEdit(
            title: title,
            description: description,
            priority: priority,
            project: project,
            status: status,
            wakeCondition: wakeCondition,
            reviewAt: reviewAt,
            source: source
        )
    }

    /// What the form was seeded with, so 保存 can stay disabled until something
    /// actually changed instead of firing a no-op `atm todo edit`.
    private var savedValue: ATMTodoEdit {
        ATMTodoEdit(
            title: todo.title,
            description: todo.description ?? "",
            priority: todo.priority,
            project: todo.project ?? "",
            status: todo.status,
            wakeCondition: todo.wakeCondition ?? "",
            reviewAt: todo.reviewAt ?? "",
            source: todo.source ?? ""
        )
    }

    private func detailCard<Content: View>(_ title: String, icon: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(title, systemImage: icon)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
            content()
                .foregroundStyle(ATMTheme.primary)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).stroke(ATMTheme.border))
        .shadow(color: Color.black.opacity(0.045), radius: 8, y: 2)
    }

    private func actionButton(_ icon: String, help: String, action: @escaping () -> Void) -> some View {
        ATMIconButton(
            systemImage: icon,
            help: help,
            isEnabled: !store.isActing,
            side: 26,
            iconTier: .body,
            action: action
        )
    }

    /// 「···」溢出菜单：编辑、剩余 lifecycle（暂不处理/回到待办/放弃等）、删除。
    /// 形状恒定，内容随状态拼装。与 `actionButton` 同样的 chip 外观。
    @ViewBuilder
    private func overflowMenu(
        overflow: [ATMTodoLifecycleItem],
        todo: ATMTodo
    ) -> some View {
        Menu {
            Button {
                beginEditing()
            } label: {
                Label("编辑任务", systemImage: "pencil")
            }
            ForEach(overflow) { item in
                Button {
                    store.perform(item.action, on: todo)
                } label: {
                    Label(item.title, systemImage: item.systemImage)
                }
            }
            Divider()
            Button(role: .destructive) {
                deleteCandidate = todo
            } label: {
                Label("删除…", systemImage: "trash")
            }
        } label: {
            ATMIconMenuLabel(
                systemImage: "ellipsis",
                help: "更多操作",
                chrome: .chip,
                isEnabled: !store.isActing,
                side: 26,
                iconTier: .body
            )
        }
        .menuStyle(.borderlessButton)
    }

    private func copyLaunchPrompt(for todo: ATMTodo) {
        Task {
            guard let prompt = await store.launchPrompt(for: todo) else { return }
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(prompt, forType: .string)
            copiedPrompt = true
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            copiedPrompt = false
        }
    }

    private var statusBadge: some View {
        let color = ATMTodoStatusStyle.color(for: todo)
        return HStack(spacing: 3) {
            ATMTodoStatusGlyph(todo: todo, tier: .caption)
            Text(ATMTodoStatusStyle.label(for: todo))
                .font(ATMFont.mono(.caption, .semibold))
        }
        .foregroundStyle(color)
        .padding(.horizontal, 7)
        .padding(.vertical, 3)
        .background(color.opacity(0.12), in: Capsule())
    }

    private func badge(_ text: String, _ color: Color) -> some View {
        Text(text)
            .font(ATMFont.mono(.caption, .semibold))
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(color.opacity(0.10), in: Capsule())
    }

    private func editLabel(_ text: String) -> some View {
        Text(text)
            .font(ATMFont.font(.footnote, weight: .semibold))
            .foregroundStyle(ATMTheme.secondary)
    }

    private func editSection<Content: View>(
        _ label: String,
        hint: String? = nil,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                editLabel(label)
                if let hint {
                    Text(hint)
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 0)
            }
            content()
        }
    }

    private func fieldBox<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        content()
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(ATMTheme.white, in: RoundedRectangle(cornerRadius: 7))
            .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
    }

    private func gridLabel(_ text: String) -> some View {
        Text(text)
            .font(ATMFont.body)
            .foregroundStyle(ATMTheme.secondary)
    }

    /// Segmented rather than a popup for the two- and three-value fields: the
    /// current value is readable without opening anything, and changing it is one
    /// click instead of two.
    private func segmented(
        selection: Binding<String>,
        values: [String],
        label: @escaping (String) -> String
    ) -> some View {
        Picker("", selection: selection) {
            ForEach(values, id: \.self) { value in Text(label(value)).tag(value) }
        }
        .pickerStyle(.segmented)
        .labelsHidden()
        .fixedSize()
    }

    // The detail header keeps a spelled-out priority badge: it is also the legend
    // for the list, where the same color arrives with no word attached.
    private var priorityColor: Color { ATMTodoPriorityStyle.color(for: todo.priority) }
    private func nonEmpty(_ value: String?) -> String? {
        guard let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else { return nil }
        return trimmed
    }
}

/// Task descriptions written by an Agent can run to hundreds of lines. Left
/// unbounded they push 动态 / 关联链接 off the first screen, so a long one is
/// clipped to a reading-size-scaled height with an 展开/收起 toggle. Short
/// descriptions render exactly as before — no toggle, no clip.
private struct DesktopTodoDescription: View {
    let source: String

    @ObservedObject private var appearance = ATMAppearance.shared
    @State private var isExpanded = false
    @State private var contentHeight: CGFloat = 0
    @State private var toggleHovered = false

    /// About 14 lines of body text, so the height tracks the 正文字号 setting
    /// instead of shrinking to a handful of lines at 大 / 特大.
    private var collapsedMaxHeight: CGFloat {
        (appearance.contentTextSize.pointSize + 3) * 14
    }

    /// Slack below the cap, so a description that just grazes the limit gets shown
    /// whole rather than clipped for the sake of two pixels.
    private var isOverflowing: Bool {
        contentHeight > collapsedMaxHeight + 24
    }

    private var isClipped: Bool { isOverflowing && !isExpanded }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ATMMarkdownContentView(source: source)
                // Ideal height regardless of the cap below, so what we measure is the
                // full description and the clip can't feed back into the measurement.
                .fixedSize(horizontal: false, vertical: true)
                .background(
                    GeometryReader { proxy in
                        Color.clear.preference(key: DescriptionHeightKey.self, value: proxy.size.height)
                    }
                )
                .frame(maxHeight: isClipped ? collapsedMaxHeight : nil, alignment: .top)
                .clipped()
                .overlay(alignment: .bottom) {
                    if isClipped {
                        LinearGradient(
                            colors: [ATMTheme.elevated.opacity(0), ATMTheme.elevated],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                        .frame(height: 36)
                        .allowsHitTesting(false)
                    }
                }
            if isOverflowing {
                Button {
                    withAnimation(.easeInOut(duration: 0.16)) { isExpanded.toggle() }
                } label: {
                    Label(
                        isExpanded ? "收起描述" : "展开完整描述",
                        systemImage: isExpanded ? "chevron.up" : "chevron.down"
                    )
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .foregroundStyle(ATMTheme.accent)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        ATMTheme.accent.opacity(toggleHovered ? 0.10 : 0.0),
                        in: RoundedRectangle(cornerRadius: 5, style: .continuous)
                    )
                }
                .buttonStyle(.plain)
                .help(isExpanded ? "收起长描述" : "展开完整任务描述")
                .onHover { toggleHovered = $0 }
            }
        }
        .onPreferenceChange(DescriptionHeightKey.self) { height in
            contentHeight = height
        }
    }
}

private struct DescriptionHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

/// One text box plus three recommended fields. The recommendations come from what
/// was typed and from the existing todos, so the common case is: type the task,
/// press Return.
private struct DesktopAddTodoSheet: View {
    @ObservedObject var store: ATMDataStore
    var onCancel: () -> Void = {}
    @State private var text = ""
    @State private var projectOverride: String?
    @State private var priorityOverride: String?
    @State private var isEditingProject = false
    let onAdd: (ATMTodoDraft) -> Void

    private var suggestion: ATMTodoSuggestion {
        ATMTodoSuggestion.infer(
            text: text,
            todos: store.allTodos,
            liveSessions: store.snapshot.liveStatus.sessions
        )
    }

    private var draft: ATMTodoDraft {
        let suggestion = suggestion
        return ATMTodoDraft(
            text: text,
            project: projectOverride ?? suggestion.project,
            priority: priorityOverride ?? suggestion.priority,
        )
    }

    private var knownProjects: [String] {
        var seen: [String] = []
        for todo in store.allTodos {
            guard let project = todo.project, !project.isEmpty, !seen.contains(project) else { continue }
            seen.append(project)
        }
        return seen.sorted()
    }

    var body: some View {
        let draft = draft
        let suggestion = suggestion
        return VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 3) {
                Text("添加任务").font(ATMFont.font(.title2, weight: .bold))
                Text("第一行是标题，换行后写细节；⏎ 添加，⇧⏎ 换行")
                    .font(ATMFont.mono(.footnote))
                    .foregroundStyle(ATMTheme.secondary)
            }

            ATMComposerTextView(
                text: $text,
                placeholder: "要完成什么？",
                autoFocus: true
            ) { submit(draft) }
                .frame(height: 150)
                .background(ATMTheme.white, in: RoundedRectangle(cornerRadius: 7))
                .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))

            HStack(spacing: 8) {
                if isEditingProject {
                    TextField("项目名", text: Binding(
                        get: { projectOverride ?? "" },
                        set: { projectOverride = $0 }
                    ))
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 180)
                    .onSubmit { isEditingProject = false }
                } else {
                    projectChip(draft: draft, suggestion: suggestion)
                }
                priorityChip(draft: draft, suggestion: suggestion)
                Spacer()
                if isOverridden {
                    Button("恢复推荐") {
                        projectOverride = nil
                        priorityOverride = nil
                        isEditingProject = false
                    }
                    .buttonStyle(.link)
                    .font(ATMFont.body)
                }
            }

            HStack {
                Text(recommendationSummary(suggestion))
                    .font(ATMFont.mono(.footnote))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 12)
                Button("取消") { onCancel() }
                    .keyboardShortcut(.cancelAction)
                    .help("关闭 (⎋)")
                Button("添加") { submit(draft) }
                    .buttonStyle(.borderedProminent)
                    .disabled(!draft.isSubmittable)
                    .help("添加任务 (⏎)")
            }
        }
        .padding(22)
        .frame(width: 540)
    }

    private var isOverridden: Bool {
projectOverride != nil || priorityOverride != nil
    }

    private func projectChip(draft: ATMTodoDraft, suggestion: ATMTodoSuggestion) -> some View {
        Menu {
            Button("无项目") { projectOverride = "" }
            if !knownProjects.isEmpty { Divider() }
            ForEach(knownProjects, id: \.self) { project in
                Button(project) { projectOverride = project }
            }
            Divider()
            Button("其他项目…") {
                projectOverride = ""
                isEditingProject = true
            }
        } label: {
            chipLabel(
                icon: "folder",
                value: draft.project.isEmpty ? "无项目" : draft.project,
                isSuggested: projectOverride == nil
            )
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .help(projectOverride == nil ? "推荐依据：\(suggestion.projectReason)" : "已手动指定项目")
    }

    private func priorityChip(draft: ATMTodoDraft, suggestion: ATMTodoSuggestion) -> some View {
        Menu {
            ForEach(["P0", "P1", "P2"], id: \.self) { value in
                Button(value) { priorityOverride = value }
            }
        } label: {
            chipLabel(icon: "flag", value: draft.priority, isSuggested: priorityOverride == nil)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .help(priorityOverride == nil ? "推荐依据：\(suggestion.priorityReason)" : "已手动指定优先级")
    }


    /// Suggested values are marked so the sheet never looks like it is asserting a
    /// project the user chose; the mark disappears once they pick one themselves.
    private func chipLabel(icon: String, value: String, isSuggested: Bool) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon).font(ATMFont.caption)
            Text(value).font(ATMFont.font(.body, weight: .medium))
            if isSuggested {
                Image(systemName: "wand.and.stars").font(ATMFont.micro)
            }
        }
        .foregroundStyle(isSuggested ? ATMTheme.secondary : ATMTheme.primary)
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(ATMTheme.controlFill, in: Capsule())
        .overlay(Capsule().stroke(ATMTheme.border))
    }

    private func recommendationSummary(_ suggestion: ATMTodoSuggestion) -> String {
        isOverridden ? "已手动调整推荐值" : "推荐依据：\(suggestion.projectReason)"
    }

    private func submit(_ draft: ATMTodoDraft) {
        guard draft.isSubmittable else { return }
        onAdd(draft)
    }
}

enum ATMUsagePageTab: String, CaseIterable, Identifiable {
    case overview
    case todaySessions

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return "概览"
        case .todaySessions: return "今日会话"
        }
    }
}

struct ATMMetricDisplayValue: Equatable {
    let main: String
    let unit: String

    static func compact(_ value: Int) -> ATMMetricDisplayValue {
        let formatted = NumberFormat.compact(value)
        guard let suffix = formatted.last, ["K", "M", "B"].contains(String(suffix)) else {
            return ATMMetricDisplayValue(main: formatted, unit: "")
        }
        return ATMMetricDisplayValue(main: String(formatted.dropLast()), unit: String(suffix))
    }

    static func percent(_ value: Double) -> ATMMetricDisplayValue {
        ATMMetricDisplayValue(main: String(format: "%.0f", value * 100), unit: "%")
    }

    static func throughput(_ value: Double) -> ATMMetricDisplayValue {
        ATMMetricDisplayValue(main: String(format: "%.0f", value), unit: "tok/s")
    }

    static func duration(_ seconds: Double) -> ATMMetricDisplayValue {
        if seconds < 1 {
            return ATMMetricDisplayValue(main: String(format: "%.1f", seconds), unit: "s")
        }
        if seconds < 60 {
            return ATMMetricDisplayValue(main: String(format: "%.0f", seconds), unit: "s")
        }
        let whole = Int(seconds.rounded())
        if whole < 3_600 {
            let minutes = whole / 60
            let remainder = whole % 60
            return ATMMetricDisplayValue(
                main: "\(minutes)",
                unit: remainder == 0 ? "m" : "m \(remainder)s"
            )
        }
        let hours = whole / 3_600
        let minutes = (whole % 3_600) / 60
        return ATMMetricDisplayValue(
            main: "\(hours)",
            unit: minutes == 0 ? "h" : "h \(minutes)m"
        )
    }

    static func plain(_ value: String) -> ATMMetricDisplayValue {
        ATMMetricDisplayValue(main: value, unit: "")
    }
}

struct ATMUsageRenderKey: Equatable {
    let refreshedAt: Date
    let quota: ATMQuotaSnapshot
    let grokLiveQuotaEnabled: Bool
    let todaySessionsState: ATMTodaySessionsState
}

private struct DesktopUsageView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject private var todaySessionsStore: ATMTodaySessionsStore
    @State private var showingDataHealth = false

    init(store: ATMDataStore) {
        self.store = store
        todaySessionsStore = store.todaySessionsStore
    }

    var body: some View {
        DesktopUsageContent(
            snapshot: store.snapshot,
            quota: store.quota,
            grokLiveQuotaEnabled: store.grokLiveQuotaEnabled,
            todaySessionsState: todaySessionsStore.state,
            setGrokLiveQuota: { store.setGrokLiveQuota($0) },
            loadTodaySessions: { todaySessionsStore.loadIfNeeded() },
            refreshTodaySessions: { todaySessionsStore.refresh() },
            showDataHealth: { showingDataHealth = true }
        )
        .equatable()
        .sheet(isPresented: $showingDataHealth) {
            ATMDataHealthSheet(store: store)
        }
    }
}

/// Charts and session rows depend only on usage data. Todo actions, knowledge
/// loading and other ATMDataStore publications stop at this equality boundary.
private struct DesktopUsageContent: View, Equatable {
    let snapshot: ATMDashboardSnapshot
    let quota: ATMQuotaSnapshot
    let grokLiveQuotaEnabled: Bool
    let todaySessionsState: ATMTodaySessionsState
    let setGrokLiveQuota: (Bool) -> Void
    let loadTodaySessions: () -> Void
    let refreshTodaySessions: () -> Void
    let showDataHealth: () -> Void

    @State private var pageTab = ATMUsagePageTab.overview
    @State private var range = ATMMetricsRange.today
    /// Three independent cascaded selects — not a dimension tab.
    @AppStorage("atmUsageFilterModel") private var filterModel = ""
    @AppStorage("atmUsageFilterClient") private var filterClient = ""
    @AppStorage("atmUsageFilterProject") private var filterProject = ""
    /// Which reading the trend line shows; remembered because it is a standing
    /// question ("is it slower today?") rather than a per-visit choice.
    @AppStorage("atmUsageTrendMetric") private var trendMetric = ATMUsageTrendMetric.tokens
    @State private var todaySessionsPage = 0

    private var renderKey: ATMUsageRenderKey {
        ATMUsageRenderKey(
            refreshedAt: snapshot.refreshedAt,
            quota: quota,
            grokLiveQuotaEnabled: grokLiveQuotaEnabled,
            todaySessionsState: todaySessionsState
        )
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.renderKey == rhs.renderKey
    }

    private var filters: ATMUsageFilters {
        ATMUsageFilters(model: filterModel, client: filterClient, project: filterProject)
    }

    /// Metric / quota cards used a fixed column count equal to the number of
    /// items, so a narrow window crushed every card into a sliver. Adaptive
    /// columns keep a readable minimum width and wrap to extra rows instead.
    private static let featuredMetricColumns = [
        GridItem(.adaptive(minimum: 185, maximum: .infinity), spacing: 10),
    ]
    private static let supportingMetricColumns = [
        GridItem(.adaptive(minimum: 145, maximum: .infinity), spacing: 8),
    ]
    // Wide enough for the per-product legend (● Build 13% ● Imagine 4% …)
    // on one line without scaling down.
    private static let quotaCardColumns = [
        GridItem(.adaptive(minimum: 224, maximum: .infinity), spacing: 12),
    ]
    /// Side-by-side breakdown + skill panels need enough room for both lists;
    /// below this, stack them so neither column is pinched.
    private static let dualPanelMinWidth: CGFloat = 640
    private static let todaySessionsPageSize = 10

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                usageModuleChrome

                // Quota is a pinned top-level summary, independent of the
                // overview / today-sessions tab and usage filters below.
                if !quota.isEmpty {
                    quotaModule
                }

                usageModule
            }
            .padding(.horizontal, 24)
            .padding(.top, 22)
            .padding(.bottom, 30)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(ATMTheme.canvas)
        .onAppear {
            normalizeFilters()
        }
        .onChange(of: pageTab) { tab in
            normalizeFilters()
            todaySessionsPage = 0
            if tab == .todaySessions {
                loadTodaySessions()
            }
        }
        .onChange(of: range) { _ in
            if pageTab == .overview {
                normalizeFilters()
            }
        }
        .onChange(of: filters) { _ in
            todaySessionsPage = 0
        }
        .onChange(of: snapshot.refreshedAt) { _ in
            if pageTab == .overview {
                normalizeFilters()
            }
        }
        .onChange(of: todaySessionsState.loadedAt) { _ in
            if pageTab == .todaySessions {
                normalizeFilters()
                normalizeTodaySessionsPage()
            }
        }
    }

    /// Rate-limit windows and external provider cards are not scoped by usage filters.
    private var quotaModule: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 3) {
                Text("额度概览")
                    .font(ATMFont.font(.title3, weight: .semibold))
                Text("各服务当前窗口与下一次重置")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }

            LazyVGrid(columns: Self.quotaCardColumns, spacing: 12) {
                ForEach(quota.cards) { card in
                    quotaCard(card)
                }
                ForEach(quota.providerCards) { card in
                    providerQuotaCard(card)
                }
            }
        }
    }

    /// Read / write behind the card gear. Config entry points only — the CLI
    /// decides live / cache / log per fetch, and live stays off until opted in,
    /// so no network request leaves the machine on its own.
    private func settingValue(_ setting: ATMQuotaCardSetting) -> Bool {
        switch setting {
        case .grokLiveQuota:
            return grokLiveQuotaEnabled
        }
    }

    private func updateSetting(_ setting: ATMQuotaCardSetting, _ enabled: Bool) {
        switch setting {
        case .grokLiveQuota:
            setGrokLiveQuota(enabled)
        }
    }

    /// Historical spend: title left; time + filters share one trailing column
    /// so their right edges stay aligned.
    private var usageModule: some View {
        LazyVStack(alignment: .leading, spacing: 14) {
            usageFilterToolbar

            if pageTab == .overview {
                let metrics = snapshot.usageMetrics(for: range, filters: filters)
                let featuredMetrics = metrics.filter(isFeaturedMetric)
                let supportingMetrics = metrics.filter { !isFeaturedMetric($0) }
                VStack(alignment: .leading, spacing: 10) {
                    Text("关键指标")
                        .font(ATMFont.font(.title3, weight: .semibold))
                    LazyVGrid(columns: Self.featuredMetricColumns, spacing: 10) {
                        ForEach(Array(featuredMetrics.enumerated()), id: \.offset) { _, metric in
                            metricCard(metric)
                        }
                    }
                    if !supportingMetrics.isEmpty {
                        LazyVGrid(columns: Self.supportingMetricColumns, spacing: 8) {
                            ForEach(Array(supportingMetrics.enumerated()), id: \.offset) { _, metric in
                                metricCard(metric, compact: true)
                            }
                        }
                    }
                }

                usageTrendCard
                dualPanelSection
            } else {
                todaySessionsCard
            }
        }
    }

    private var usagePagePicker: some View {
        Picker("用量页面", selection: $pageTab) {
            ForEach(ATMUsagePageTab.allCases) { tab in
                Text(tab.title).tag(tab)
            }
        }
        .labelsHidden()
        .pickerStyle(.segmented)
        .frame(width: 260)
        .accessibilityLabel("用量页面")
    }

    /// Wide: title | (time above filters, both trailing).
    /// Narrow: keep the page switch beside the title and move health below.
    private var usageModuleChrome: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .bottom, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("用量")
                        .font(ATMFont.font(.title1, weight: .semibold))
                    Text("额度、成本与模型效率")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 8)
                usagePagePicker
                dataHealthButton
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("用量")
                        .font(ATMFont.font(.title1, weight: .semibold))
                    Text("额度、成本与模型效率")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                HStack {
                    usagePagePicker
                    Spacer()
                    dataHealthButton
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var dataHealthButton: some View {
        ATMHoverLabelButton(
            title: "数据健康",
            systemImage: "stethoscope",
            help: "查看统计覆盖率、未知模型和数据源问题",
            height: 32,
            tier: .body
        ) {
            showDataHealth()
        }
        .frame(width: 126)
    }

    /// One quiet toolbar binds range, refresh and cascaded filters into a
    /// single reading flow without turning the whole header into another card.
    private var usageFilterToolbar: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                usageRangeOrRefreshControl
                Divider()
                    .frame(height: 20)
                usageFilterControls
                Spacer(minLength: 0)
            }
            VStack(alignment: .leading, spacing: 8) {
                usageRangeOrRefreshControl
                usageFilterControls
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            ATMTheme.elevated,
            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(ATMTheme.border.opacity(0.7))
        )
    }

    private func isFeaturedMetric(_ metric: ATMUsageMetric) -> Bool {
        switch metric {
        case .tokens, .cacheHitRate, .cost, .throughput:
            return true
        default:
            return false
        }
    }

    @ViewBuilder
    private var usageRangeOrRefreshControl: some View {
        if pageTab == .overview {
            // A dropdown, not six segments: the calendar windows do not fit this
            // width flat, and spreading them into their own row would turn a summary
            // into a filter panel. Grouped so the two kinds read apart at a glance.
            Picker("", selection: $range) {
                ForEach(ATMMetricsRange.Group.allCases) { group in
                    Section(group.title) {
                        ForEach(ATMMetricsRange.inGroup(group)) { item in
                            Text(item.pickerTitle).tag(item)
                        }
                    }
                }
            }
            .pickerStyle(.menu)
            .labelsHidden()
            .frame(width: 140)
        } else {
            HStack(spacing: 8) {
                Label("今天", systemImage: "calendar")
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                ATMHoverLabelButton(
                    title: todaySessionsState.isLoading ? "加载中" : "刷新",
                    systemImage: "arrow.clockwise",
                    help: "重新统计今日会话用量",
                    height: 32,
                    tier: .body
                ) {
                    refreshTodaySessions()
                }
                .frame(width: 104)
                .disabled(todaySessionsState.isLoading)
            }
        }
    }

    private var usageFilterControls: some View {
        HStack(spacing: 5) {
            filterSelect(dimension: .model, selection: $filterModel)
            filterSelect(dimension: .client, selection: $filterClient)
            filterSelect(dimension: .project, selection: $filterProject)
            clearFiltersButton
        }
    }

    private func filterSelect(
        dimension: ATMUsageDimension,
        selection: Binding<String>
    ) -> some View {
        let options = filterOptions(for: dimension)
        return Picker(dimension.title, selection: selection) {
            Text(dimension.filterTitle).tag("")
            if !options.isEmpty {
                Divider()
                ForEach(options, id: \.self) { name in
                    if dimension == .client {
                        Label(name, systemImage: ATMAgentDisplay.systemImage(name)).tag(name)
                    } else {
                        Text(name).tag(name)
                    }
                }
            }
        }
        .labelsHidden()
        .pickerStyle(.menu)
        // Hug the selected title instead of a fixed min width — that was
        // spreading the three selects farther apart than the HStack spacing.
        .fixedSize(horizontal: true, vertical: false)
        .help("按\(dimension.title)筛选用量；选项随其他筛选级联")
        .accessibilityLabel(dimension.title)
    }

    private func filterOptions(for dimension: ATMUsageDimension) -> [String] {
        if pageTab == .todaySessions {
            return todaySessionsState.sessions.filterOptions(
                dimension: dimension,
                filters: filters
            )
        }
        return snapshot.filterOptions(for: range, dimension: dimension, filters: filters)
    }

    @ViewBuilder
    private var clearFiltersButton: some View {
        if !filters.isEmpty {
            Button("清除") {
                filterModel = ""
                filterClient = ""
                filterProject = ""
            }
            .buttonStyle(.link)
            .font(ATMFont.body)
            .help("清除模型 / 客户端 / 项目筛选")
        }
    }

    /// Token volume and generation speed over the same buckets. They are one card
    /// with a toggle rather than two cards: the series, filters and colours are
    /// shared, and seeing them in the same frame is how "more tokens" gets told
    /// apart from "slower model".
    private var usageTrendCard: some View {
        let availableSeries = snapshot.filteredSeriesNames(for: range, filters: filters)
        let seriesStats = snapshot.filteredLineTrendStats(for: range, filters: filters)
        let seriesNames = snapshot.filteredLineTrendSeries(for: range, filters: filters)
        let seriesLabel = filters.project.isEmpty ? "模型" : "项目"
        let pinSeries = !filters.model.isEmpty || !filters.project.isEmpty
        // Buckets with no measurable request carry no speed at all. Dropping
        // them leaves a gap in the line; drawing them as 0 would claim the
        // model stalled.
        let speedStats = seriesStats.filter { $0.tokensPerSecond != nil }
        let points = trendMetric == .speed
            ? speedStats.map { ATMTrendPoint(from: $0, value: $0.tokensPerSecond ?? 0) }
            : seriesStats.map { ATMTrendPoint(from: $0, value: Double($0.totalTokens)) }
        return VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text(range.tokenTrendTitle)
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                Spacer(minLength: 12)
                Picker("", selection: $trendMetric) {
                    ForEach(ATMUsageTrendMetric.allCases) { item in Text(item.title).tag(item) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 154)
                .help("Token 看用了多少，速度看模型每秒输出多少 token（不含工具执行时间）")
            }

            if points.isEmpty {
                usageEmptyState(trendMetric.emptyStateTitle, icon: "chart.xyaxis.line")
            } else {
                let visibleSeries = Set(points.map(\.series))
                let singleSeriesColor = seriesChartColors(
                    for: Array(visibleSeries),
                    available: availableSeries
                ).first ?? ATMTheme.accent
                Chart(points) { point in
                    if visibleSeries.count == 1 {
                        AreaMark(
                            x: .value("日期", point.day),
                            y: .value(trendMetric.axisTitle, point.value)
                        )
                        .foregroundStyle(
                            LinearGradient(
                                colors: [
                                    singleSeriesColor.opacity(0.16),
                                    singleSeriesColor.opacity(0.01),
                                ],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )
                        .interpolationMethod(.catmullRom)
                    }
                    LineMark(
                        x: .value("日期", point.day),
                        y: .value(trendMetric.axisTitle, point.value),
                        series: .value(seriesLabel, point.series)
                    )
                    .foregroundStyle(by: .value(seriesLabel, point.series))
                    .lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                    .interpolationMethod(.catmullRom)
                    if point.value > 0 {
                        PointMark(
                            x: .value("日期", point.day),
                            y: .value(trendMetric.axisTitle, point.value)
                        )
                        .foregroundStyle(by: .value(seriesLabel, point.series))
                        .symbol(by: .value(seriesLabel, point.series))
                        .symbolSize(pinSeries ? 34 : 22)
                    }
                }
                .chartForegroundStyleScale(
                    domain: seriesNames,
                    range: seriesChartColors(for: seriesNames, available: availableSeries)
                )
                .chartLegend(position: .bottom, alignment: .leading, spacing: 12)
                .chartYAxis {
                    AxisMarks(position: .leading) { value in
                        AxisGridLine(stroke: StrokeStyle(lineWidth: 0.7))
                            .foregroundStyle(ATMTheme.chartGrid)
                        AxisValueLabel {
                            if let v = value.as(Double.self) {
                                // Token counts run to millions and want the compact
                                // form; a rate never leaves two digits.
                                Text(trendMetric == .speed
                                    ? String(format: "%.0f", v)
                                    : NumberFormat.compact(Int(v)))
                            }
                        }
                    }
                }
                .chartXAxis {
                    AxisMarks(values: ATMUsageDateAxis.values(
                        points.map(\.date),
                        // A month of daily ticks overlaps at this width; a week fits.
                        maximumLabels: range == .last30Days || range == .thisMonth ? 6 : 7
                    )) { value in
                        AxisTick(stroke: StrokeStyle(lineWidth: 0.7))
                            .foregroundStyle(ATMTheme.chartGrid)
                        AxisValueLabel {
                            if let date = value.as(Date.self) {
                                if range == .today {
                                    Text(date, format: .dateTime.hour().minute())
                                } else {
                                    Text(date, format: .dateTime.month(.defaultDigits).day())
                                }
                            }
                        }
                        .font(ATMFont.rounded(.footnote, .medium))
                    }
                }
                .chartXScale(
                    domain: ATMUsageDateAxis.paddedDomain(points.map(\.date)),
                    range: .plotDimension(padding: 18)
                )
                .chartPlotStyle { plotArea in
                    plotArea
                        .background(ATMTheme.chartPlotFill)
                        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                }
                .frame(height: 240)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).stroke(ATMTheme.border))
        .shadow(color: Color.black.opacity(0.04), radius: 8, y: 2)
    }

    private var todaySessionsCard: some View {
        let sessions = todaySessionsState.sessions.filtered(using: filters)
        let pageCount = ATMPagination.pageCount(
            itemCount: sessions.count,
            pageSize: Self.todaySessionsPageSize
        )
        let safePage = ATMPagination.clampedPage(
            todaySessionsPage,
            itemCount: sessions.count,
            pageSize: Self.todaySessionsPageSize
        )
        let visible = ATMPagination.items(
            sessions,
            page: safePage,
            pageSize: Self.todaySessionsPageSize
        )
        let rankOffset = safePage * Self.todaySessionsPageSize
        let scopedTotal = max(sessions.reduce(0) { $0 + $1.totalTokens }, 1)
        return usageCard("今日会话") {
            if todaySessionsState.isLoading && todaySessionsState.sessions.isEmpty {
                VStack(spacing: 10) {
                    ProgressView()
                        .controlSize(.small)
                    Text("正在统计今日会话…")
                        .font(ATMFont.font(.body, weight: .medium))
                        .foregroundStyle(ATMTheme.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 220)
            } else if let error = todaySessionsState.errorMessage,
                      todaySessionsState.sessions.isEmpty {
                VStack(spacing: 10) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(ATMFont.title1)
                        .foregroundStyle(ATMTheme.warning)
                    Text("今日会话加载失败")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text(error)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .multilineTextAlignment(.center)
                        .lineLimit(3)
                    Button("重试") {
                        refreshTodaySessions()
                    }
                    .controlSize(.small)
                }
                .frame(maxWidth: .infinity, minHeight: 220)
            } else if sessions.isEmpty {
                usageEmptyState("所选筛选暂无今日会话用量", icon: "bubble.left.and.bubble.right")
            } else {
                VStack(spacing: 0) {
                    HStack {
                        Text("\(sessions.count) 个会话 · 按今日 Token 排序")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                        Spacer()
                        if pageCount > 1 {
                            todaySessionsPagination(
                                currentPage: safePage,
                                pageCount: pageCount
                            )
                        } else if todaySessionsState.isLoading {
                            ProgressView()
                                .controlSize(.small)
                        }
                    }
                    .padding(.bottom, 8)

                    if let error = todaySessionsState.errorMessage {
                        Text("刷新失败，当前显示上次结果：\(error)")
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.warning)
                            .padding(.bottom, 8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }

                    ForEach(Array(visible.enumerated()), id: \.element.id) { index, session in
                        todaySessionRow(
                            session,
                            rank: rankOffset + index + 1,
                            share: Double(session.totalTokens) / Double(scopedTotal)
                        )
                        if index < visible.count - 1 {
                            Divider().padding(.leading, 42)
                        }
                    }
                }
            }
        }
    }

    private func todaySessionsPagination(
        currentPage: Int,
        pageCount: Int
    ) -> some View {
        HStack(spacing: 7) {
            Button {
                withAnimation(.easeInOut(duration: 0.16)) {
                    todaySessionsPage = currentPage - 1
                }
            } label: {
                Image(systemName: "chevron.left")
            }
            .disabled(currentPage == 0)
            .help("上一页")

            Text("\(currentPage + 1) / \(pageCount)")
                .font(ATMFont.mono(.footnote, .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .frame(minWidth: 44)

            Button {
                withAnimation(.easeInOut(duration: 0.16)) {
                    todaySessionsPage = currentPage + 1
                }
            } label: {
                Image(systemName: "chevron.right")
            }
            .disabled(currentPage >= pageCount - 1)
            .help("下一页")
        }
        .buttonStyle(.borderless)
        .controlSize(.small)
        .foregroundStyle(ATMTheme.accent)
    }

    private func normalizeTodaySessionsPage() {
        todaySessionsPage = ATMPagination.clampedPage(
            todaySessionsPage,
            itemCount: todaySessionsState.sessions.filtered(using: filters).count,
            pageSize: Self.todaySessionsPageSize
        )
    }

    private func todaySessionRow(
        _ session: ATMSessionUsage,
        rank: Int,
        share: Double
    ) -> some View {
        let color = ATMTheme.palette[(rank - 1) % ATMTheme.palette.count]
        return HStack(alignment: .top, spacing: 10) {
            Text(String(format: "%02d", rank))
                .font(ATMFont.mono(.footnote, .bold))
                .foregroundStyle(color)
                .frame(width: 32, height: 26)
                .background(color.opacity(0.1), in: RoundedRectangle(cornerRadius: 6))

            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 6) {
                    Text(session.shortID)
                        .font(ATMFont.mono(.body, .semibold))
                        .foregroundStyle(ATMTheme.primary)
                    Text(session.project.isEmpty ? "未分项目" : session.project)
                        .font(ATMFont.font(.footnote, weight: .medium))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                }

                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 5) {
                        todaySessionIdentity(session)
                    }
                    VStack(alignment: .leading, spacing: 3) {
                        todaySessionIdentity(session)
                    }
                }

                Text(
                    "输入 \(NumberFormat.compact(session.inputTokens)) · " +
                    "输出 \(NumberFormat.compact(session.outputTokens)) · " +
                    "缓存 \(NumberFormat.compact(session.cacheTokens)) · " +
                    "\(session.requests) 请求"
                )
                .font(ATMFont.mono(.caption))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.78)
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            VStack(alignment: .trailing, spacing: 3) {
                Text(NumberFormat.compact(session.totalTokens))
                    .font(ATMFont.mono(.title3, .bold))
                    .foregroundStyle(color)
                    .lineLimit(1)
                Text("\(NumberFormat.percent(share)) 今日")
                    .font(ATMFont.mono(.caption, .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                Text(NumberFormat.currency(session.costUSD))
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
            }
            .frame(width: 98, alignment: .trailing)
        }
        .padding(.vertical, 9)
        .help("Session \(session.sessionID)")
    }

    @ViewBuilder
    private func todaySessionIdentity(_ session: ATMSessionUsage) -> some View {
        Label(session.activityTimeText, systemImage: "clock")
        HStack(spacing: 3) {
            ATMAgentMark(agent: session.agent, size: 12)
            Text(ATMAgentDisplay.name(session.agent))
        }
        Label(session.model, systemImage: "cpu")
            .lineLimit(1)
            .truncationMode(.tail)
    }

    /// Breakdown + skill sit side by side when there is room; otherwise they
    /// stack so each panel keeps a full readable width.
    @ViewBuilder
    private var dualPanelSection: some View {
        let skillPanel = skillUsageCard
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .top, spacing: 14) {
                breakdownCard
                    .frame(minWidth: 330, maxWidth: .infinity, alignment: .topLeading)
                skillPanel
                    .frame(minWidth: 330, maxWidth: .infinity, alignment: .topLeading)
            }
            .frame(minWidth: Self.dualPanelMinWidth)
            VStack(alignment: .leading, spacing: 14) {
                breakdownCard
                skillPanel
            }
        }
    }

    private var skillUsageCard: some View {
        usageCard(range.skillTitle) {
            let skills = Array(snapshot.skillStats(for: range).prefix(8))
            let total = max(snapshot.skillCallTotal(for: range), 1)
            if skills.isEmpty {
                usageEmptyState("所选范围暂无 Skill 调用", icon: "sparkles")
            } else {
                VStack(spacing: 12) {
                    ForEach(Array(skills.enumerated()), id: \.element.id) { index, skill in
                        VStack(alignment: .leading, spacing: 5) {
                            HStack {
                                Text(skill.skill).lineLimit(1)
                                Spacer(minLength: 8)
                                Text("\(skill.calls) 次")
                                    .fixedSize()
                                Text("\(skill.sessions) 会话")
                                    .foregroundStyle(ATMTheme.secondary)
                                    .fixedSize()
                            }
                            ProgressView(value: Double(skill.calls), total: Double(total))
                                .tint(ATMTheme.palette[index % ATMTheme.palette.count])
                        }
                        .font(ATMFont.font(.body, weight: .medium))
                    }
                }
            }
        }
    }

    /// Ranked list under the filters. Clicking a row sets the matching select
    /// (model or project), which is how the cascade above is discovered.
    private var breakdownCard: some View {
        let result = snapshot.filteredBreakdown(for: range, filters: filters)
        let rows = Array(result.rows.prefix(8))
        let total = max(rows.reduce(0) { $0 + $1.totalTokens }, 1)
        return usageCard("\(range.breakdownTitle)（按\(result.dimension.title)）") {
            if rows.isEmpty {
                usageEmptyState("所选筛选暂无\(result.dimension.title)用量", icon: "chart.bar")
            } else {
                VStack(spacing: 12) {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        breakdownRow(row, dimension: result.dimension, index: index, total: total)
                    }
                }
            }
        }
    }

    private func breakdownRow(
        _ row: ATMUsageBreakdownRow,
        dimension: ATMUsageDimension,
        index: Int,
        total: Int
    ) -> some View {
        let isActive: Bool = {
            switch dimension {
            case .model: return filterModel == row.series
            case .client: return filterClient == row.series
            case .project: return filterProject == row.series
            }
        }()
        return Button {
            switch dimension {
            case .model:
                filterModel = isActive ? "" : row.series
            case .client:
                filterClient = isActive ? "" : row.series
            case .project:
                filterProject = isActive ? "" : row.series
            }
        } label: {
            VStack(alignment: .leading, spacing: 5) {
                HStack(alignment: .center, spacing: 8) {
                    if dimension == .client {
                        ATMAgentMark(agent: row.series, size: 16)
                    }
                    Text(row.label)
                        .lineLimit(1)
                        .frame(minWidth: 86, maxWidth: .infinity, alignment: .leading)
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(NumberFormat.compact(row.totalTokens))
                        ViewThatFits(in: .horizontal) {
                            HStack(spacing: 6) {
                                breakdownMeta(row)
                            }
                            VStack(alignment: .trailing, spacing: 2) {
                                breakdownMeta(row)
                            }
                        }
                    }
                    .font(ATMFont.mono(.footnote))
                    .fixedSize(horizontal: true, vertical: false)
                }
                ProgressView(value: Double(row.totalTokens), total: Double(total))
                    .tint(ATMTheme.palette[index % ATMTheme.palette.count])
            }
            .font(ATMFont.font(.body, weight: isActive ? .semibold : .medium))
            .padding(.horizontal, 6)
            .padding(.vertical, 4)
            .background(
                isActive ? ATMTheme.accent.opacity(0.1) : .clear,
                in: RoundedRectangle(cornerRadius: 6)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(isActive ? "取消筛选" : "只看 \(row.label)")
    }

    @ViewBuilder
    private func breakdownMeta(_ row: ATMUsageBreakdownRow) -> some View {
        if row.sessions > 0 {
            Text("\(row.sessions) 会话")
                .foregroundStyle(ATMTheme.secondary)
        }
        // A tilde marks spend ATM had to guess the rate for, so an estimate is
        // never read as a quote. `atm doctor` names the models.
        Text(row.costEstimated ? "~\(NumberFormat.currency(row.costUSD))" : NumberFormat.currency(row.costUSD))
            .foregroundStyle(ATMTheme.secondary)
            .help(row.costEstimated ? "费用为估算：该模型没有确切价格，按模型家族或保守默认价计算。可在 ~/.atm/pricing.json 指定精确价格。" : "")
        if row.cacheReadTokens > 0 {
            Text("缓存 \(NumberFormat.percent(row.cacheShare))")
                .foregroundStyle(ATMTheme.cacheHitColor(row.cacheShare))
        }
    }

    /// Spells out what the rate was measured over, and the one case worth acting
    /// on: at this pace the quota is gone before it refills.
    private func quotaTrendHelp(_ trend: ATMQuotaTrend, window: ATMQuotaWindow) -> String {
        var text = "近 \(trend.spanMinutes) 分钟 \(trend.samples) 个采样点：\(trend.rateText)"
        if trend.fullBeforeReset {
            text += "。按当前速度会在重置前打满。"
        }
        return text
    }

    private func quotaCard(_ card: ATMQuotaCard) -> some View {
        let window = card.window
        let percent = window.displayPercent
        let color = ATMTheme.quotaColor(ATMQuotaLevel.level(forPercent: percent))
        let label = ATMAgentDisplay.name(card.agent)
        return VStack(alignment: .leading, spacing: 9) {
            HStack(spacing: 5) {
                ATMAgentMark(agent: card.agent, size: 15)
                Text(label)
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .lineLimit(1)
                Text(window.windowLabel)
                    .font(ATMFont.mono(.caption, .semibold))
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(ATMTheme.controlFill, in: Capsule())
                    .fixedSize()
                Spacer(minLength: 4)
                if let plan = card.plan, !plan.isEmpty {
                    Text(plan)
                        .font(ATMFont.mono(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                }
                if !card.settings.isEmpty {
                    ATMQuotaCardSettingsButton(
                        agentLabel: label,
                        settings: card.settings,
                        isOn: settingValue,
                        setOn: updateSetting
                    )
                }
            }
            // Reserved for every card, so the one with a gear does not push its
            // number a few points lower than its neighbours.
            .frame(height: 20)
            .foregroundStyle(ATMTheme.secondary)

            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(String(format: "%.0f", percent))
                    .font(ATMFont.mono(.metric, .bold))
                    .foregroundStyle(color)
                    .lineLimit(1)
                    .minimumScaleFactor(0.75)
                Text("% 已用")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize()
                // The rate turns the number into something to act on. Absent until
                // history exists, so a fresh install shows the percentage alone
                // rather than an empty trend.
                if let trend = window.trend {
                    Spacer(minLength: 4)
                    HStack(spacing: 2) {
                        if let arrow = trend.arrow {
                            Text(arrow)
                        }
                        Text(trend.rateText)
                    }
                    .font(ATMFont.mono(.caption, .semibold))
                    .foregroundStyle(trend.fullBeforeReset
                        ? ATMTheme.quotaColor(.critical)
                        : ATMTheme.secondary)
                    .fixedSize()
                    .help(quotaTrendHelp(trend, window: window))
                }
            }

            // With per-product data the bar itself carries the split: stacked
            // colored segments of the same pool instead of extra text rows.
            // Segment widths are clamped so their sum never exceeds the
            // headline percent — if the API's product split ever drifts from
            // the total, the bar scales down instead of contradicting the number.
            GeometryReader { proxy in
                let productSum = card.products.reduce(0) { $0 + max(0, $1.usedPercent) }
                let productScale = productSum > percent && productSum > 0 ? percent / productSum : 1
                ZStack(alignment: .leading) {
                    Capsule().fill(ATMTheme.controlFill)
                    if card.products.isEmpty {
                        Capsule()
                            .fill(color)
                            .frame(width: max(0, min(1, percent / 100)) * proxy.size.width)
                    } else {
                        HStack(spacing: 0) {
                            ForEach(Array(card.products.enumerated()), id: \.element.id) { index, product in
                                Rectangle()
                                    .fill(Self.quotaProductColor(index))
                                    .frame(width: max(0, min(1, product.usedPercent * productScale / 100)) * proxy.size.width)
                            }
                            Spacer(minLength: 0)
                        }
                        .clipShape(Capsule())
                    }
                }
            }
            .frame(height: 5)

            if !card.products.isEmpty {
                // Legend for the stacked segments, one compact line.
                HStack(spacing: 10) {
                    ForEach(Array(card.products.enumerated()), id: \.element.id) { index, product in
                        HStack(spacing: 4) {
                            Circle()
                                .fill(Self.quotaProductColor(index))
                                .frame(width: 5, height: 5)
                            Text(product.displayName)
                                .font(ATMFont.caption)
                            Text(String(format: "%.0f%%", product.usedPercent))
                                .font(ATMFont.mono(.caption))
                        }
                        .help("\(product.displayName) 占本周额度池 \(String(format: "%.1f", product.usedPercent))%")
                    }
                }
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
            }

            // Pin the footer to the card's bottom edge so cards with and
            // without a product legend still share one height and baseline.
            Spacer(minLength: 0)

            HStack(spacing: 6) {
                Label(window.resetText, systemImage: "clock.arrow.circlepath")
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                Spacer(minLength: 4)
                if let sourceLabel = card.sourceLabel {
                    HStack(spacing: 3) {
                        Circle()
                            .fill(card.source == "live"
                                ? ATMTheme.quotaColor(.healthy)
                                : ATMTheme.secondary.opacity(0.45))
                            .frame(width: 4, height: 4)
                        Text(sourceLabel)
                    }
                    .help("额度数据来源：实时 = 账单接口，缓存 = 最近一次实时结果，日志 = 本地会话日志")
                }
            }
            .font(ATMFont.mono(.caption))
            .foregroundStyle(ATMTheme.secondary)
        }
        .padding(16)
        // One fixed height for every card: tall enough for the product-split
        // variant, and the Spacer above absorbs the slack in plain cards.
        .frame(maxWidth: .infinity, minHeight: 148, alignment: .topLeading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).stroke(ATMTheme.border))
        .shadow(color: Color.black.opacity(0.04), radius: 7, y: 2)
        .help("\(label) \(window.windowLabel) 窗口：\(String(format: "%.1f", percent))% 已用，\(window.resetText)")
    }

    /// A card whose provider named the page behind the reading is a way in to it:
    /// the whole card opens that page, so a quota running low is one click from
    /// wherever it is managed.
    @ViewBuilder
    private func providerQuotaCard(_ card: ATMProviderQuotaCard) -> some View {
        if let url = card.payload.linkURL {
            DesktopQuotaCardLink(url: url) { isHovered in
                providerQuotaCardBody(card, isHovered: isHovered)
            }
        } else {
            providerQuotaCardBody(card, isHovered: false)
        }
    }

    private func providerQuotaCardBody(_ card: ATMProviderQuotaCard, isHovered: Bool) -> some View {
        let payload = card.payload
        let label = ATMAgentDisplay.name(card.agent)
        let linksOut = payload.linkURL != nil
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 5) {
                ATMAgentMark(agent: card.agent, size: 15)
                Text(label)
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .lineLimit(1)
                Text(card.providerLabel)
                    .font(ATMFont.mono(.caption, .semibold))
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(ATMTheme.controlFill, in: Capsule())
                    .fixedSize()
                Spacer(minLength: 4)
                if let period = payload.period, !period.isEmpty {
                    Text(period)
                        .font(ATMFont.mono(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                }
                if linksOut {
                    // Drawn whether or not the pointer is here, so revealing it on
                    // hover cannot shove the period label sideways.
                    Image(systemName: "arrow.up.right")
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(ATMTheme.accent)
                        .opacity(isHovered ? 1 : 0)
                }
            }
            .frame(height: 20)
            .foregroundStyle(ATMTheme.secondary)

            Text(payload.title)
                .font(ATMFont.font(.caption, weight: .medium))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)

            if payload.isUnavailable {
                providerQuotaEmptyState(payload)
            } else {
                ForEach(payload.metrics) { metric in
                    providerQuotaMetric(metric)
                }
            }

            Spacer(minLength: 0)

            HStack(spacing: 6) {
                if !payload.observedAt.isEmpty {
                    Label(
                        payload.isUnavailable ? "上次 \(payload.observedTimeLabel)" : payload.observedTimeLabel,
                        systemImage: "clock"
                    )
                    .lineLimit(1)
                }
                Spacer(minLength: 4)
                if let sourceLabel = card.sourceLabel {
                    HStack(spacing: 3) {
                        Circle()
                            .fill(ATMTheme.quotaColor(.healthy))
                            .frame(width: 4, height: 4)
                        Text(sourceLabel)
                    }
                }
            }
            .font(ATMFont.mono(.caption))
            .foregroundStyle(ATMTheme.secondary)
        }
        .padding(16)
        .frame(maxWidth: .infinity, minHeight: 148, alignment: .topLeading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 11, style: .continuous)
                .stroke(isHovered ? ATMTheme.accent : ATMTheme.border)
        )
        .shadow(color: Color.black.opacity(isHovered ? 0.08 : 0.04), radius: 7, y: 2)
        // The card's own padding is part of the hit area, not a dead margin.
        .contentShape(RoundedRectangle(cornerRadius: 12))
        .help(
            "\(label) · \(card.providerLabel) · \(payload.title)："
                + (payload.isUnavailable
                    ? payload.unavailableText
                    : payload.metrics.map { "\($0.label) \(String(format: "%.1f", $0.usedPercent))%" }
                        .joined(separator: "，"))
                + (linksOut ? " · 点击打开" : "")
        )
    }

    /// A provider with nothing to report keeps its card and loses its numbers.
    /// Dropping the card instead read as "this quota no longer exists" — for a
    /// daily quota that is only observed when its page is open, that happened
    /// every morning. The reading is the only thing missing, so say so and let
    /// the timestamp below show how old the last one is.
    private func providerQuotaEmptyState(_ payload: ATMProviderQuotaPayload) -> some View {
        HStack(spacing: 5) {
            Image(systemName: "clock.arrow.circlepath")
            Text(payload.unavailableText)
        }
        .font(ATMFont.footnote)
        .foregroundStyle(ATMTheme.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func providerQuotaMetric(_ metric: ATMProviderQuotaMetric) -> some View {
        let percent = max(0, metric.usedPercent)
        let color = ATMTheme.quotaColor(ATMQuotaLevel.level(forPercent: percent))
        return VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(metric.label)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                Text(metric.valueText)
                    .font(ATMFont.mono(.body, .semibold))
                    .lineLimit(1)
                    .minimumScaleFactor(0.72)
                Spacer(minLength: 2)
                Text(String(format: "%.1f%%", percent))
                    .font(ATMFont.mono(.caption, .semibold))
                    .foregroundStyle(color)
                    .fixedSize()
            }
            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(ATMTheme.controlFill)
                    Capsule()
                        .fill(color)
                        .frame(width: max(0, min(1, percent / 100)) * proxy.size.width)
                }
            }
            .frame(height: 5)
        }
    }


    /// Stable per-position colors for the product split; skips the palette's
    /// trailing neutral, which is reserved for "其他" aggregates.
    private static func quotaProductColor(_ index: Int) -> Color {
        let colors = ATMTheme.palette.dropLast()
        return colors[index % colors.count]
    }

    @ViewBuilder
    private func metricCard(_ metric: ATMUsageMetric, compact: Bool = false) -> some View {
        switch metric {
        case let .seriesCount(count, title):
            metricCard(
                "\(title)数",
                .plain("\(count)"),
                "square.stack.3d.up",
                compact: compact
            )
        case let .tokens(value):
            metricCard(
                "总 Token",
                .compact(value),
                "number.circle.fill",
                emphasized: true,
                valueColor: ATMTheme.accent,
                compact: compact
            )
        case let .output(value):
            metricCard("输出", .compact(value), "arrow.up.right.circle", compact: compact)
        case let .cacheHitRate(rate):
            metricCard(
                "缓存命中率",
                .percent(rate),
                "bolt.shield.fill",
                emphasized: true,
                valueColor: ATMTheme.cacheHitColor(rate),
                compact: compact
            )
        case let .sessions(count):
            metricCard(
                "会话",
                .plain("\(count)"),
                "bubble.left.and.bubble.right",
                compact: compact
            )
        case let .queries(count):
            metricCard("提问", .plain("\(count)"), "text.bubble", compact: compact)
        case let .cost(value):
            metricCard(
                "估算费用",
                .plain(NumberFormat.currency(value)),
                "dollarsign.circle.fill",
                emphasized: true,
                valueColor: ATMTheme.accent,
                compact: compact
            )
        case let .throughput(value):
            metricCard("输出速度", .throughput(value), "speedometer", compact: compact)
                .help("模型自身的生成速度，不含工具执行时间；由日志时间戳推导，只统计可测的请求")
        case let .turnWait(seconds):
            metricCard("等待中位数", .duration(seconds), "hourglass", compact: compact)
                .help("从你发出消息到模型给出最后一句回复，含工具执行与该轮内部的每次请求")
        }
    }

    private func metricCard(
        _ title: String,
        _ value: ATMMetricDisplayValue,
        _ icon: String,
        emphasized: Bool = false,
        valueColor: Color = ATMTheme.primary,
        compact: Bool = false
    ) -> some View {
        VStack(alignment: .leading, spacing: compact ? 6 : 9) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(emphasized ? valueColor : ATMTheme.secondary)
                Text(title)
                    .foregroundStyle(ATMTheme.secondary)
                Spacer(minLength: 0)
            }
                .font(ATMFont.font(.footnote, weight: .semibold))
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                Text(value.main)
                    .font(ATMFont.rounded(.metric, .bold))
                    .foregroundStyle(valueColor)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                if !value.unit.isEmpty {
                    Text(value.unit)
                        .font(ATMFont.rounded(.body, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                }
            }
        }
        .padding(compact ? 12 : 16)
        .frame(maxWidth: .infinity, minHeight: compact ? 72 : 92, alignment: .leading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: compact ? 9 : 11, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: compact ? 9 : 11, style: .continuous)
                .stroke(ATMTheme.border)
        )
        .shadow(color: compact ? .clear : Color.black.opacity(0.04), radius: 7, y: 2)
        .overlay(alignment: .topLeading) {
            if emphasized {
                Capsule()
                    .fill(valueColor)
                    .frame(width: 28, height: 3)
                    .padding(.leading, 16)
                    .padding(.top, 8)
            }
        }
    }

    private func usageCard<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(title)
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
            content()
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).stroke(ATMTheme.border))
        .shadow(color: Color.black.opacity(0.04), radius: 8, y: 2)
    }

    private func usageEmptyState(_ title: String, icon: String) -> some View {
        VStack(spacing: 8) {
            Image(systemName: icon)
                .font(ATMFont.title1)
                .foregroundStyle(ATMTheme.secondary)
            Text(title)
                .font(ATMFont.font(.body, weight: .medium))
                .foregroundStyle(ATMTheme.secondary)
        }
        .frame(maxWidth: .infinity, minHeight: 220)
    }

    /// Drop filter values that no longer exist for the current range / cascade,
    /// so a stale pick does not silently zero the page.
    private func normalizeFilters() {
        let models = filterOptions(for: .model)
        let clients = filterOptions(for: .client)
        let projects = filterOptions(for: .project)
        if !filterModel.isEmpty, !models.contains(filterModel) { filterModel = "" }
        if !filterClient.isEmpty, !clients.contains(filterClient) { filterClient = "" }
        if !filterProject.isEmpty, !projects.contains(filterProject) { filterProject = "" }
    }

    private func seriesChartColors(for series: [String], available: [String]) -> [Color] {
        let categoricalCount = max(ATMTheme.palette.count - 1, 1)
        return series.map { name in
            let index = available.firstIndex(of: name) ?? 0
            return ATMTheme.palette[index % categoricalCount]
        }
    }

}

/// Makes one quota card open a page.
///
/// Its own view because each card needs its own hover state — a single `@State`
/// on the grid's parent would light up every card at once. macOS `.plain`
/// buttons draw no hover of their own (see `ATMIconButton`), so the card body
/// takes the hover flag and decides what it means.
private struct DesktopQuotaCardLink<Content: View>: View {
    let url: URL
    @ViewBuilder var content: (Bool) -> Content

    @State private var isHovered = false

    var body: some View {
        Button {
            NSWorkspace.shared.open(url)
        } label: {
            content(isHovered)
        }
        .buttonStyle(.plain)
        .onHover { isHovered = $0 }
        .animation(.easeInOut(duration: 0.12), value: isHovered)
    }
}

/// Gear on a quota card that opens that agent's own settings.
///
/// A popover instead of an inline switch: the card is a glance surface, and one
/// visible toggle per knob would crowd it out as more knobs arrive. Each row
/// carries its own explanation, so the popover is the place to add the next one.
private struct ATMQuotaCardSettingsButton: View {
    let agentLabel: String
    let settings: [ATMQuotaCardSetting]
    let isOn: (ATMQuotaCardSetting) -> Bool
    let setOn: (ATMQuotaCardSetting, Bool) -> Void

    @State private var isPresented = false

    var body: some View {
        ATMIconButton(
            systemImage: "gearshape",
            help: "\(agentLabel) 额度设置",
            chrome: .bare,
            isEmphasized: isPresented,
            side: 22,
            iconTier: .footnote
        ) { isPresented.toggle() }
        .popover(isPresented: $isPresented, arrowEdge: .bottom) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 8) {
                    Image(systemName: "gearshape.fill")
                        .foregroundStyle(ATMTheme.accent)
                    Text("\(agentLabel) 额度设置")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                }
                .padding(.horizontal, 15)
                .padding(.vertical, 12)

                Divider()

                VStack(alignment: .leading, spacing: 12) {
                    ForEach(settings) { setting in
                        settingRow(setting)
                    }
                }
                .padding(15)
            }
            .frame(width: 376)
            .background(ATMTheme.surface)
        }
    }

    private func settingRow(_ setting: ATMQuotaCardSetting) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Toggle(setting.title, isOn: Binding(
                get: { isOn(setting) },
                set: { setOn(setting, $0) }
            ))
            .toggleStyle(.switch)
            .controlSize(.small)
            .font(ATMFont.font(.body, weight: .medium))
            Text(setting.detail)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct ATMDataHealthSheet: View {
    @ObservedObject var store: ATMDataStore
    @Environment(\.dismiss) private var dismiss
    @State private var report: ATMDoctorReport?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("数据健康")
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text("说明用量与成本统计的覆盖范围和已知误差")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("完成") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(18)
            Divider()

            if isLoading {
                ProgressView("正在运行 atm doctor…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let errorMessage {
                VStack(spacing: 9) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(ATMFont.display)
                    Text("数据健康检查失败").font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text(errorMessage)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .multilineTextAlignment(.center)
                }
                .padding(28)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let report {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        HStack(spacing: 10) {
                            healthMetric("数据源", "\(report.sources.count)", "externaldrive")
                            healthMetric(
                                "警告",
                                "\(report.issues.filter { $0.severity == "warning" || $0.severity == "error" }.count)",
                                "exclamationmark.triangle"
                            )
                            healthMetric("提示", "\(report.issues.filter { $0.severity == "info" }.count)", "info.circle")
                        }

                        Text("当前问题")
                            .font(ATMFont.font(.bodyLarge, weight: .semibold))
                        if report.issues.isEmpty {
                            Label("未发现数据覆盖问题", systemImage: "checkmark.circle.fill")
                                .foregroundStyle(ATMTheme.success)
                        } else {
                            VStack(spacing: 8) {
                                ForEach(report.issues) { issue in
                                    VStack(alignment: .leading, spacing: 6) {
                                        HStack {
                                            Label(
                                                "\(issue.subject) · \(issue.code)",
                                                systemImage: issue.severity == "warning"
                                                    ? "exclamationmark.triangle.fill"
                                                    : "info.circle.fill"
                                            )
                                            .font(ATMFont.font(.body, weight: .semibold))
                                            .foregroundStyle(issue.severity == "warning" ? ATMTheme.warning : ATMTheme.accent)
                                            Spacer()
                                            Text(issue.domain)
                                                .font(ATMFont.mono(.caption))
                                                .foregroundStyle(ATMTheme.secondary)
                                        }
                                        Text(issue.detail)
                                            .font(ATMFont.body)
                                        Label(issue.suggestion, systemImage: "arrow.turn.down.right")
                                            .font(ATMFont.footnote)
                                            .foregroundStyle(ATMTheme.secondary)
                                    }
                                    .padding(11)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 8))
                                    .overlay(RoundedRectangle(cornerRadius: 8).stroke(ATMTheme.border))
                                }
                            }
                        }
                    }
                    .padding(18)
                }
            }
        }
        .frame(minWidth: 800, minHeight: 600)
        .task { await load() }
    }

    private func healthMetric(_ title: String, _ value: String, _ icon: String) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Label(title, systemImage: icon)
                .font(ATMFont.font(.footnote, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary)
            Text(value)
                .font(ATMFont.mono(.title1, .bold))
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 9))
    }

    @MainActor
    private func load() async {
        isLoading = true
        errorMessage = nil
        do {
            report = try await store.doctor()
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}
