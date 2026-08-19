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
    case aiDay
    case settings

    var id: String { rawValue }
    var title: String {
        switch self {
        case .tasks: return "任务"
        case .collection: return "收集"
        case .agents: return "Agent"
        case .knowledge: return "知识"
        case .usage: return "用量"
        case .aiDay: return "AI Day"
        case .settings: return "设置"
        }
    }
    var icon: String {
        switch self {
        case .tasks: return "checklist"
        case .collection: return "tray.and.arrow.down"
        case .agents: return "cpu"
        case .knowledge: return "books.vertical"
        case .usage: return "chart.xyaxis.line"
        case .aiDay: return "sparkles"
        case .settings: return "gearshape"
        }
    }
}

/// One restorable place in the desktop app. Keeping the selected row with its
/// section makes history useful for detail-to-detail links, not just for moving
/// between the six sidebar tabs.
enum ATMDesktopLocation: Equatable {
    case tasks(todoID: String?, listMode: ATMTaskListMode)
    case collection(sourceID: String?, itemID: String?)
    case agents(sessionID: String?, runTodoID: String?)
    case knowledge(libraryID: String?, documentID: String?)
    case usage
    case aiDay
    case settings
}

enum ATMTaskListMode: Equatable {
    case active
    case trash
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

    static func shouldRevealDropped(
        selectedID: String?,
        in todos: [ATMTodo],
        showsDropped: Bool
    ) -> Bool {
        guard !showsDropped, let selectedID else { return false }
        return todos.first(where: { $0.id == selectedID })?.status == "dropped"
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
    @Published var section: ATMDesktopSection = .tasks {
        didSet { navigationDidChange() }
    }
    @Published var selectedTodoID: String? {
        didSet { if section == .tasks { navigationDidChange() } }
    }
    /// Which task collection owns selectedTodoID. Direct links to trashed Todos
    /// switch this before the task view validates the selection.
    @Published var taskListMode: ATMTaskListMode = .active {
        didSet { if section == .tasks { navigationDidChange() } }
    }
    @Published var selectedCollectionSourceID: String? {
        didSet { if section == .collection { navigationDidChange() } }
    }
    @Published var selectedCollectionItemID: String? {
        didSet { if section == .collection { navigationDidChange() } }
    }
    /// Bumped even when the destination ID is unchanged, so clicking a collection
    /// notification always returns an already-open workspace from 来源 to 记录.
    @Published private(set) var collectionItemRevealRequest = 0
    @Published var selectedAgentID: String? {
        didSet { if section == .agents { navigationDidChange() } }
    }
    /// The Todo whose dispatched run requested the Agent selection. This keeps
    /// the raw-log destination available after a successful run closes its live
    /// binding while the recent Agent session is still visible.
    @Published var selectedAgentRunTodoID: String? {
        didSet {
            // Run context refines the current Agent destination (not a separate
            // visit). This preserves “all logs for this Todo” on forward replay
            // without adding a second history entry for the same Agent row.
            if section == .agents, !isRestoringLocation {
                recordedLocation = currentLocation
            }
        }
    }
    @Published var selectedKnowledgeLibraryID: String? {
        didSet { if section == .knowledge { navigationDidChange() } }
    }
    /// A document id ("document:<id>") the knowledge library view should
    /// reveal and select, set when locating a result from global search.
    @Published var locateKnowledgeDocumentID: String?
    /// Bumped to ask the knowledge view to open its “new document” sheet
    /// for the currently selected library (right-click “在此新建知识”).
    @Published var knowledgeCreateRequest = 0
    /// Add-task modal is owned at the desktop root so the dimmed backdrop
    /// covers the sidebar and centers over the whole window.
    @Published var showAddTodo = false
    /// A todo id whose detail pane should open directly in its edit form, set by
    /// the task row's right-click 编辑任务 and cleared once the form is open.
    @Published var editTodoID: String?

    @Published private(set) var canGoBack = false
    @Published private(set) var canGoForward = false

    private var backStack: [ATMDesktopLocation] = []
    private var forwardStack: [ATMDesktopLocation] = []
    private var recordedLocation: ATMDesktopLocation = .tasks(todoID: nil, listMode: .active)
    private var isRestoringLocation = false
    private let maximumHistoryCount = 100

    func goBack() {
        guard let target = backStack.popLast() else { return }
        forwardStack.append(recordedLocation)
        restore(target)
    }

    func goForward() {
        guard let target = forwardStack.popLast() else { return }
        backStack.append(recordedLocation)
        trimHistory(&backStack)
        restore(target)
    }

    func revealCollectionItem(_ itemID: String) {
        selectedCollectionItemID = itemID
        collectionItemRevealRequest += 1
    }

    private var currentLocation: ATMDesktopLocation {
        switch section {
        case .tasks:
            return .tasks(todoID: selectedTodoID, listMode: taskListMode)
        case .collection:
            return .collection(
                sourceID: selectedCollectionSourceID,
                itemID: selectedCollectionItemID
            )
        case .agents:
            return .agents(
                sessionID: selectedAgentID,
                runTodoID: selectedAgentRunTodoID
            )
        case .knowledge:
            return .knowledge(
                libraryID: selectedKnowledgeLibraryID,
                documentID: locateKnowledgeDocumentID
            )
        case .usage:
            return .usage
        case .aiDay:
            return .aiDay
        case .settings:
            return .settings
        }
    }

    private func navigationDidChange() {
        guard !isRestoringLocation else { return }
        let next = currentLocation
        guard next != recordedLocation else { return }
        backStack.append(recordedLocation)
        trimHistory(&backStack)
        forwardStack.removeAll()
        recordedLocation = next
        updateHistoryAvailability()
    }

    private func restore(_ location: ATMDesktopLocation) {
        isRestoringLocation = true
        switch location {
        case .tasks(let todoID, let listMode):
            selectedTodoID = todoID
            taskListMode = listMode
            section = .tasks
        case .collection(let sourceID, let itemID):
            selectedCollectionSourceID = sourceID
            selectedCollectionItemID = itemID
            section = .collection
        case .agents(let sessionID, let runTodoID):
            selectedAgentID = sessionID
            selectedAgentRunTodoID = runTodoID
            section = .agents
        case .knowledge(let libraryID, let documentID):
            selectedKnowledgeLibraryID = libraryID
            locateKnowledgeDocumentID = documentID
            section = .knowledge
        case .usage:
            section = .usage
        case .aiDay:
            section = .aiDay
        case .settings:
            section = .settings
        }
        isRestoringLocation = false
        recordedLocation = location
        updateHistoryAvailability()
    }

    private func trimHistory(_ history: inout [ATMDesktopLocation]) {
        if history.count > maximumHistoryCount {
            history.removeFirst(history.count - maximumHistoryCount)
        }
    }

    private func updateHistoryAvailability() {
        canGoBack = !backStack.isEmpty
        canGoForward = !forwardStack.isEmpty
    }
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

/// ATM's app-wide navigation rail follows the active app appearance while using
/// dedicated tokens for its hover, selection and hierarchy states.
private struct ATMDesktopRailSurfaceModifier: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
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
            .animation(ATMMotion.resolved(ATMMotion.hover, reduceMotion: reduceMotion), value: isHovered)
            .animation(ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion), value: isSelected)
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

enum ATMDesktopLayout {
    static let titleBarHeight: CGFloat = 38
    /// 中栏头部的固定高度。四个工作区（任务的标题头、收集 / 知识 / Agent 的段控头）都用
    /// 它定高，切页时中栏第一行才不会上下跳——此前任务栏靠 18/14 的内边距长到约 56pt，
    /// 另外三处各自写死 64pt。
    static let drawerHeaderHeight: CGFloat = ATMGroupedNavigatorMetrics.headerHeight
    static let drawerHeaderHorizontalPadding: CGFloat = 16
    static let expandedSidebarWidth: CGFloat = 160
    static let collapsedSidebarWidth: CGFloat = 58
    static let railDividerWidth: CGFloat = 1
    static let railDragHandleWidth: CGFloat = 10
    static let minimumExpandedSidebarWidth: CGFloat = 132
    static let maximumExpandedSidebarWidth: CGFloat = 280
    static let sidebarCollapseThreshold: CGFloat = 100
    static let sidebarExpansionThreshold: CGFloat = 116

    static let sidebarWidthDefaultsKey = "ATMDesktopSidebarWidth"

    static func resolvedExpandedSidebarWidth(_ requested: CGFloat) -> CGFloat {
        let requested = requested.isFinite ? requested : expandedSidebarWidth
        return min(
            max(requested.rounded(), minimumExpandedSidebarWidth),
            maximumExpandedSidebarWidth
        )
    }

    static func sidebarIsCollapsed(
        at requestedWidth: CGFloat,
        wasCollapsed: Bool
    ) -> Bool {
        if requestedWidth.isNaN { return true }
        // A small dead zone stops the rail flickering between text and icon modes
        // when the pointer pauses on the snap point.
        if wasCollapsed { return requestedWidth < sidebarExpansionThreshold }
        return requestedWidth <= sidebarCollapseThreshold
    }
}

struct DesktopContentView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    @AppStorage("ATMDesktopSidebarCollapsed") private var sidebarCollapsed = false
    @AppStorage(ATMDesktopLayout.sidebarWidthDefaultsKey)
    private var storedSidebarWidth = Double(ATMDesktopLayout.expandedSidebarWidth)
    @State private var draggedSidebarWidth: CGFloat?
    @State private var sidebarDragOrigin: CGFloat?
    @State private var isHoveringSidebarDivider = false
    @State private var sidebarResizeCursorPushed = false
    @State private var showingCollectionCreate = false
    @State private var newCollectionID = ""
    @State private var newCollectionName = ""
    @State private var renameCollectionTarget: ATMCollectionRef?
    @State private var renameCollectionName = ""
    @State private var deleteCollectionTarget: ATMCollectionRef?
    @State private var collectionError: String?

    var body: some View {
        VStack(spacing: 0) {
            desktopTitleBar
                // The custom search dropdown extends below title-bar bounds.
                // Keep it above the workspace instead of clipping it into the
                // first content column like an ordinary sibling.
                .zIndex(2)
            desktopWorkspace
                .zIndex(0)
        }
        // The app owns the full-size title bar. Laying the root into the top safe
        // area makes this one surface sit behind the traffic lights while the
        // three workspace columns begin only below it.
        .ignoresSafeArea(.container, edges: .top)
        .frame(minWidth: 880, minHeight: 620)
        .atmHidesScrollBars()
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
                            navigation.selectedTodoID = createdID
                            navigation.section = .tasks
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
        .animation(ATMMotion.resolved(ATMMotion.disclosure, reduceMotion: reduceMotion), value: navigation.showAddTodo)
        .animation(
            sidebarDragOrigin == nil
                ? ATMMotion.resolved(ATMMotion.selection, reduceMotion: reduceMotion)
                : nil,
            value: sidebarCollapsed
        )
        .sheet(isPresented: $showingCollectionCreate) {
            collectionCreateSheet
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

    private var desktopWorkspace: some View {
        HStack(spacing: 0) {
            desktopSidebar
                .frame(
                    width: currentSidebarWidth,
                    alignment: .leading
                )
                .clipped()
            sidebarDivider
                .zIndex(1)
            Group {
                switch navigation.section {
                case .tasks:
                    DesktopTasksView(store: store, navigation: navigation)
                case .collection:
                    DesktopCollectionView(store: store, navigation: navigation)
                case .agents:
                    DesktopAgentsView(store: store, navigation: navigation)
                case .knowledge:
                    DesktopKnowledgeView(
                        store: store,
                        navigation: navigation,
                        onCreateCollection: {
                            newCollectionID = ""
                            newCollectionName = ""
                            showingCollectionCreate = true
                        },
                        onRenameCollection: { collection in
                            renameCollectionName = collection.name
                            renameCollectionTarget = ATMCollectionRef(
                                id: collection.id,
                                name: collection.name,
                                count: collection.documentCount
                            )
                        },
                        onDeleteCollection: { collection in
                            deleteCollectionTarget = ATMCollectionRef(
                                id: collection.id,
                                name: collection.name,
                                count: collection.documentCount
                            )
                        }
                    )
                        .id("knowledge-library")
                case .usage:
                    DesktopUsageView(store: store)
                case .aiDay:
                    DesktopAIDayView()
                case .settings:
                    DesktopSettingsView(store: store)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .atmAnimatedSwap(navigation.section, style: .workspace)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(ATMTheme.canvas)
    }

    private var currentSidebarWidth: CGFloat {
        if sidebarCollapsed { return ATMDesktopLayout.collapsedSidebarWidth }
        return draggedSidebarWidth
            ?? ATMDesktopLayout.resolvedExpandedSidebarWidth(CGFloat(storedSidebarWidth))
    }

    private var sidebarDivider: some View {
        Color.clear
            .frame(width: ATMDesktopLayout.railDividerWidth)
            .overlay {
                Capsule()
                    .fill(
                        ATMTheme.accent.opacity(
                            isHoveringSidebarDivider || sidebarDragOrigin != nil ? 0.62 : 0
                        )
                    )
                    .frame(width: 2, height: 36)
                    .animation(
                        ATMMotion.hover,
                        value: isHoveringSidebarDivider || sidebarDragOrigin != nil
                    )
            }
            .overlay {
                Color.clear
                    .frame(width: ATMDesktopLayout.railDragHandleWidth)
                    .contentShape(Rectangle())
                    .help("拖拽调整侧栏宽度")
                    .onHover { hovering in
                        isHoveringSidebarDivider = hovering
                        setSidebarResizeCursorPushed(hovering || sidebarDragOrigin != nil)
                    }
                    .onDisappear { setSidebarResizeCursorPushed(false) }
                    .gesture(
                        DragGesture(minimumDistance: 1, coordinateSpace: .global)
                            .onChanged(handleSidebarDrag)
                            .onEnded { _ in finishSidebarDrag() }
                    )
            }
    }

    private func handleSidebarDrag(_ value: DragGesture.Value) {
        let origin = sidebarDragOrigin ?? currentSidebarWidth
        sidebarDragOrigin = origin
        let requested = origin + value.translation.width

        if ATMDesktopLayout.sidebarIsCollapsed(
            at: requested,
            wasCollapsed: sidebarCollapsed
        ) {
            sidebarCollapsed = true
            draggedSidebarWidth = nil
        } else {
            sidebarCollapsed = false
            draggedSidebarWidth = ATMDesktopLayout.resolvedExpandedSidebarWidth(requested)
        }
        setSidebarResizeCursorPushed(true)
    }

    private func finishSidebarDrag() {
        if !sidebarCollapsed, let draggedSidebarWidth {
            storedSidebarWidth = Double(draggedSidebarWidth)
        }
        draggedSidebarWidth = nil
        sidebarDragOrigin = nil
        setSidebarResizeCursorPushed(isHoveringSidebarDivider)
    }

    private func setSidebarResizeCursorPushed(_ pushed: Bool) {
        guard pushed != sidebarResizeCursorPushed else { return }
        sidebarResizeCursorPushed = pushed
        if pushed {
            NSCursor.resizeLeftRight.push()
        } else {
            NSCursor.pop()
        }
    }

    private var desktopTitleBar: some View {
        ZStack {
            DesktopSearchPalette(store: store, navigation: navigation)

            HStack {
                HStack(spacing: 2) {
                    ATMIconButton(
                        systemImage: "chevron.left",
                        help: "后退 (⌘[)",
                        chrome: .bare,
                        isEnabled: navigation.canGoBack,
                        side: 26,
                        iconTier: .body
                    ) {
                        navigation.goBack()
                    }
                    ATMIconButton(
                        systemImage: "chevron.right",
                        help: "前进 (⌘])",
                        chrome: .bare,
                        isEnabled: navigation.canGoForward,
                        side: 26,
                        iconTier: .body
                    ) {
                        navigation.goForward()
                    }
                }
                // Leave the native traffic-light cluster unobstructed.
                .padding(.leading, 76)

                Spacer(minLength: 12)
                ATMIconButton(
                    systemImage: "sidebar.left",
                    help: sidebarCollapsed ? "展开侧栏" : "收起侧栏",
                    chrome: .bare,
                    side: 26,
                    iconTier: .body
                ) {
                    sidebarCollapsed.toggle()
                }
            }
            .padding(.trailing, 8)
            // Native traffic lights sit slightly above the geometric centre of a
            // full-size title bar. Nudge this row — which sits right beside them —
            // onto that same visual baseline. The search field is 76pt away from
            // the lights and reads against the bar's own top and bottom edges
            // instead, so it stays centred in the bar and takes no nudge.
            .padding(.bottom, 4)
        }
        .frame(maxWidth: .infinity)
        .frame(height: ATMDesktopLayout.titleBarHeight)
        .background(.ultraThinMaterial)
        // 一条实线，不是投影：0.04 的软阴影落在三栏各自的底色上几乎看不见，顶栏和工作区
        // 只剩材质通透度的差别，边界是「大概在这一带」而不是一条线。
        .overlay(alignment: .bottom) { Divider() }
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
                    navigation.selectedKnowledgeLibraryID = id
                    navigation.section = .knowledge
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
            VStack(spacing: 4) {
                ForEach(ATMDesktopSection.allCases.filter { $0 != .settings }) { section in
                    sidebarButton(section)
                }
            }
            .padding(.horizontal, sidebarCollapsed ? 7 : 8)
            .padding(.top, 16)

            Spacer()

            sidebarButton(.settings)
                .padding(.horizontal, sidebarCollapsed ? 7 : 8)
                .padding(.bottom, 8)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(ATMTheme.rail)
    }

    private func sidebarButton(_ section: ATMDesktopSection) -> some View {
        let selected = navigation.section == section
        let hasUnreadCollection = section == .collection
            && (store.collectionOverview.summary.unreadCount ?? 0) > 0
        return Button {
            navigation.section = section
            if section == .knowledge {
                store.refreshKnowledgeCatalog()
            }
        } label: {
            HStack(spacing: 9) {
                ZStack(alignment: .topTrailing) {
                    Image(systemName: section.icon).frame(width: 18)
                    if sidebarCollapsed, hasUnreadCollection {
                        Circle()
                            .fill(ATMTheme.accent)
                            .frame(width: 6, height: 6)
                            .offset(x: 3, y: -2)
                            .accessibilityHidden(true)
                    }
                }
                if !sidebarCollapsed {
                    Text(section.title)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .layoutPriority(1)
                    Spacer(minLength: 4)
                    if hasUnreadCollection {
                        Circle()
                            .fill(ATMTheme.accent)
                            .frame(width: 7, height: 7)
                            .accessibilityHidden(true)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: sidebarCollapsed ? .center : .leading)
            .font(ATMFont.font(.body, weight: .medium))
            .atmDesktopRailSurface(isSelected: selected)
        }
        .buttonStyle(.plain)
        .help(hasUnreadCollection ? "\(section.title)，有未读" : section.title)
        .accessibilityLabel(hasUnreadCollection ? "\(section.title)，有未读" : section.title)
    }

    private var sortedKnowledgeCollections: [ATMKnowledgeCollection] {
        store.knowledgeCollections.sorted {
            if $0.id == "inbox" { return true }
            if $1.id == "inbox" { return false }
            return $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
    }

    private func selectDefaultKnowledgeLibraryIfNeeded() {
        if let selected = navigation.selectedKnowledgeLibraryID,
           selected == ATMKnowledgeLibrary.memoryID || selected == ATMKnowledgeLibrary.archiveID ||
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

    private var showingTrash: Bool {
        navigation.taskListMode == .trash
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
        ATMNavigatorGroupHeader(
            title: group.title,
            count: group.todos.count,
            tint: groupAccent(group.id),
            isExpanded: expanded
        )
    }

    private func groupAccent(_ id: String) -> Color {
        switch id {
        case "trash": return ATMTheme.secondary
        case "review": return ATMTodoStatusStyle.color(forStatus: "review")
        case "working": return ATMTodoStatusStyle.color(forStatus: "in_progress")
        case "waiting": return ATMTodoStatusStyle.color(forStatus: "waiting")
        case "blocked": return ATMTodoStatusStyle.color(forStatus: "blocked")
        case "done", "history": return ATMTodoStatusStyle.color(forStatus: "done")
        default: return ATMTheme.secondary
        }
    }

    private var todos: [ATMTodo] {
        showingTrash ? store.trashedTodos : store.allTodos
    }

    private var visibleTodos: [ATMTodo] {
        if showingTrash { return todos }
        return ATMTaskQuery.visibleTodos(from: todos, showsDropped: showsDropped)
    }

    private var selectedTodo: ATMTodo? {
        guard let id = navigation.selectedTodoID else { return nil }
        return visibleTodos.first { $0.id == id }
    }

    private var groups: [ATMTaskGroup] {
        if showingTrash {
            return [ATMTaskGroup(id: "trash", title: "已删除", todos: visibleTodos)]
        }
        return ATMTaskQuery.groups(from: visibleTodos).map {
            ATMTaskGroup(id: $0.id, title: $0.title, todos: $0.todos)
        }
    }

    var body: some View {
        ATMSplitColumn(
            id: "tasks",
            defaultWidth: ATMWorkspaceLayout.navigatorDefaultWidth,
            minWidth: ATMWorkspaceLayout.navigatorMinWidth,
            maxWidth: ATMWorkspaceLayout.navigatorMaxWidth,
            detailMinWidth: ATMWorkspaceLayout.objectDetailMinWidth
        ) {
            taskList
        } detail: {
            Group {
                if let todo = selectedTodo {
                    DesktopTodoDetail(
                        todo: todo,
                        store: store,
                        navigation: navigation,
                        isTrashed: showingTrash
                    )
                            // Identity is the Todo id alone. Folding title / description /
                            // status into it recreated the view on every background sync
                            // that touched them, which reset the selected tab and threw
                            // away an open edit form mid-typing. The form seeds itself
                            // from `todo` when 编辑 is picked, so it opens on current
                            // values without needing a fresh identity to do it.
                        .id(todo.id)
                } else {
                    ATMDetailBodySurface {
                        ATMEmptyState(
                            icon: showingTrash ? "trash" : "checklist",
                            title: showingTrash ? "选择一个已删除任务" : "选择一个任务",
                            detail: "从中栏查看详情、编辑 Markdown 或执行快捷操作。",
                            size: .inline,
                            minHeight: 180
                        )
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .atmAnimatedSwap(
                "todo:\(selectedTodo?.id ?? "empty"):\(showingTrash)",
                style: .detail
            )
        }
        .onAppear {
            applyDefaultCollapsedGroupsIfNeeded()
            revealSelectedTodoIfFiltered()
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
        .onChange(of: navigation.taskListMode) { _ in selectFirstIfNeeded() }
        .onChange(of: navigation.selectedTodoID) { _ in
            revealSelectedTodoIfFiltered()
            selectFirstIfNeeded()
            revealSelectedGroup()
        }
    }

    private var taskList: some View {
        ATMGroupedNavigator {
            ATMDrawerHeader(title: showingTrash ? "回收站" : "任务", count: visibleTodos.count) {
                if showingTrash {
                    Button {
                        navigation.taskListMode = .active
                    } label: {
                        Label("返回任务", systemImage: "chevron.left")
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                } else {
                    // 纯图标一律走 ATMIconButton——原生 `.bordered` 的方框让它比收集 / 知识
                    // 同一位置的图标多一圈边，而带文字的「新建」「返回任务」才留给原生按钮。
                    ATMIconButton(
                        systemImage: "trash",
                        help: "回收站",
                        chrome: .bare,
                        side: 30,
                        iconTier: .bodyLarge
                    ) {
                        navigation.taskListMode = .trash
                    }

                    Button {
                        navigation.showAddTodo = true
                    } label: {
                        Label("新建", systemImage: "plus")
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    // ⌘N 归主菜单「文件 → 新建任务」，不挂在这个按钮上：挂在这儿的话
                    // 快捷键跟着「任务」页一起消失，切到收集或知识就按不动了。
                    .help("添加任务 (⌘N)")
                }
            }
        } content: {
            if let error = store.errorMessage {
                let presentation = ATMErrorPresentation.resolve(error, fallbackTitle: "任务加载失败")
                ATMInlineNotice(
                    severity: .error,
                    title: presentation.title,
                    message: presentation.message,
                    details: error,
                    actionTitle: "重试",
                    onAction: { store.refresh() },
                    onDismiss: { store.dismissDashboardError() }
                )
                .padding(.horizontal, 8)
                .padding(.bottom, 8)
            }

            ATMGroupedNavigatorScroll {
                ForEach(groups) { group in
                    let expanded = expandedBinding(for: group)
                    ATMNavigatorGroup {
                        groupHeader(group, expanded: expanded)
                    } content: {
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
                                    .buttonStyle(.atmRow)
                                    .focusable(false)
                                    .atmContentStackRow()
                                    .atmRightClickMenu { todoMenuEntries(for: todo) }
                            }
                        }
                    }
                }
            }
            .overlay {
                if visibleTodos.isEmpty {
                    ATMEmptyState(
                        icon: "magnifyingglass",
                        title: showingTrash ? "回收站为空" : "没有匹配的任务"
                    )
                    .allowsHitTesting(false)
                }
            }
        }
        .confirmationDialog(
            "永久删除 \(deleteCandidate?.id.uppercased() ?? "")？",
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
            Text("\(deleteCandidate?.title ?? "")\n此操作无法恢复。").font(ATMFont.body)
        }
    }

    private func todoMenuEntries(for todo: ATMTodo) -> [ATMMenuEntry] {
        ATMTodoMenu.entries(
            for: todo,
            store: store,
            isTrashed: showingTrash,
            // Editing lives in the detail pane's form, so the row menu selects the
            // todo and asks the detail to open straight into it.
            onEdit: {
                navigation.selectedTodoID = todo.id
                navigation.editTodoID = todo.id
            },
            onPermanentDelete: { deleteCandidate = todo }
        )
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

    /// A collection record can point at a dropped Todo. Reveal the dropped group
    /// before validating that direct selection; otherwise selectFirstIfNeeded
    /// silently replaces it with an unrelated first row.
    private func revealSelectedTodoIfFiltered() {
        guard !showingTrash,
              ATMTaskQuery.shouldRevealDropped(
                selectedID: navigation.selectedTodoID,
                in: store.allTodos,
                showsDropped: showsDropped
              ) else { return }
        showsDropped = true
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
        // No status glyph. It sat in a 28pt tile at the head of every row and said
        // only what the section header the row is filed under already says — every
        // row in 工作中 carried the same spinner, every row in 等待中 the same clock.
        // Dropping it gives the title back ~38pt of a 260–420pt column and takes a
        // line's worth of height off each row, which is the whole point of the list:
        // scan ids and titles, not re-read the section you are already inside.
        ATMNavigatorRow(isSelected: isSelected) {
            VStack(alignment: .leading, spacing: ATMContentRowLayout.contentSpacing) {
                // The id leads the title rather than sitting in the meta line: it
                // is what you scan the list for and what you type back at the CLI.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(todo.id.uppercased())
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(
                            isClosed
                                ? ATMTheme.secondary
                                : ATMTodoPriorityStyle.color(for: todo.priority)
                        )
                        .help(ATMTodoPriorityStyle.label(todo.priority))
                        .accessibilityLabel(
                            "\(todo.id.uppercased()) \(ATMTodoPriorityStyle.label(todo.priority)) \(ATMTodoStatusStyle.label(for: todo))"
                        )
                    Text(todo.title)
                        .font(ATMFont.font(.body, weight: .medium))
                        .foregroundStyle(isClosed ? ATMTheme.secondary : ATMTheme.primary)
                        .strikethrough(
                            ATMTodoStatusStyle.usesStrikethrough(for: todo),
                            color: ATMTheme.secondary
                        )
                        .lineLimit(2)
                }

                if projectLabel != nil || creatorLabel != nil {
                    HStack(spacing: 6) {
                        if let project = projectLabel { Text(project) }
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
    }

    private var isClosed: Bool {
        todo.status == "done" || todo.status == "dropped"
    }

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
        case activity
        case taskRun
        case sessions
    }

    let todo: ATMTodo
    @ObservedObject var store: ATMDataStore
    @ObservedObject var navigation: ATMDesktopNavigation
    let isTrashed: Bool

    @State private var isEditing = false
    @State private var selectedTab: DetailTab = .detail
    @State private var copiedPrompt = false
    @State private var deleteCandidate: ATMTodo?
    @State private var showingDispatchSheet = false
    @State private var showingCodexContinuation = false
    @State private var showingTaskRunInterruptConfirmation = false
    @State private var taskRunLaunchError: String?
    @State private var codexContinuationInstructions = ""
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
                ATMDetailBodySurface { editContent }
            } else {
                ATMDetailScaffold {
                    detailHeader
                } notice: {
                    refineNotice
                } tabs: {
                    ATMCapsuleTabs(selection: $selectedTab, items: detailTabItems)
                } content: {
                    Group {
                        if selectedTab == .detail {
                            readContent
                        } else if selectedTab == .activity {
                            activityContent
                        } else if selectedTab == .taskRun, hasTaskRuns {
                            taskRunContent
                        } else {
                            sessionContent
                        }
                    }
                }
            }
        }
        .background(Color.clear)
        .onAppear {
            if !isTrashed {
                store.loadBoundSessions(for: todo.id)
                store.loadTaskRuns(for: todo.id)
                store.loadTaskRunAgents()
                store.loadProgress(for: todo.id)
            }
            // Selecting another row rebuilds this view (`.id(todo.id)`), so a
            // request aimed at a not-yet-selected todo arrives here rather than in
            // onChange.
            consumeEditRequest()
        }
        .onChange(of: navigation.editTodoID) { _ in consumeEditRequest() }
        .onChange(of: store.snapshot.refreshedAt) { _ in
            if !isTrashed {
                store.loadBoundSessions(for: todo.id)
                store.loadTaskRuns(for: todo.id)
            }
        }
        // Both of these can remove the page that is currently selected: trashing
        // hides three of them, and 「Agent 执行」 only exists while a run does.
        .onChange(of: isTrashed) { _ in normalizeSelectedTab() }
        .onChange(of: hasTaskRuns) { _ in normalizeSelectedTab() }
        .task(id: taskRunRefreshKey) {
            guard !isTrashed else { return }
            store.loadTaskRuns(for: todo.id)
            if selectedTab == .taskRun {
                store.refreshLiveStatus()
            }
            while latestTaskRun?.isActive == true, !Task.isCancelled {
                do {
                    try await Task.sleep(nanoseconds: 2_000_000_000)
                } catch {
                    break
                }
                store.loadTaskRuns(for: todo.id)
                if selectedTab == .taskRun {
                    store.refreshLiveStatus()
                }
            }
        }
        .confirmationDialog(
            "永久删除 \(deleteCandidate?.id.uppercased() ?? "")？",
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
            Text("\(deleteCandidate?.title ?? "")\n此操作无法恢复。").font(ATMFont.body)
        }
        .confirmationDialog(
            "中断当前 Codex 执行？",
            isPresented: $showingTaskRunInterruptConfirmation,
            titleVisibility: .visible
        ) {
            Button("中断执行", role: .destructive) {
                store.interruptTaskRun(todoID: todo.id)
            }
            Button("继续执行", role: .cancel) {}
        } message: {
            Text("Agent 进程会停止，Todo 保持工作中；之后可以重新执行或继续该会话。")
        }
        .sheet(isPresented: $showingCodexContinuation) {
            codexContinuationSheet
        }
        .sheet(isPresented: $showingDispatchSheet) {
            taskRunDispatchSheet
        }
        .alert(
            "无法打开 Agent 会话",
            isPresented: Binding(
                get: { taskRunLaunchError != nil },
                set: { if !$0 { taskRunLaunchError = nil } }
            )
        ) {
            Button("好") { taskRunLaunchError = nil }
        } message: {
            Text(taskRunLaunchError ?? "")
        }
    }

    @ViewBuilder
    private var refineNotice: some View {
        if store.refiningTodoIDs.contains(todo.id) {
            ATMInlineNotice(
                severity: .info,
                title: "正在整理任务",
                message: "模型在润色标题和需求；复杂工作会拆成子任务并写一份计划。"
            )
            .padding(.horizontal, ATMDetailLayout.horizontalPadding)
            .padding(.top, 10)
        } else if let error = store.refineErrorByTodoID[todo.id], !error.isEmpty {
            ATMInlineNotice(
                severity: .warning,
                title: "任务整理失败",
                message: error,
                actionTitle: "重试",
                onAction: { store.refineTodo(id: todo.id) },
                onDismiss: { store.dismissRefineError(for: todo.id) }
            )
            .padding(.horizontal, ATMDetailLayout.horizontalPadding)
            .padding(.top, 10)
        }
    }

    /// 「Agent 执行」只在真的有执行记录时出现。
    ///
    /// 默认的交接路径（`todo handoff` / 「在 Codex 里打开」）不产生 `task_runs`
    /// ——会话由 Codex 自己拥有，ATM 通过 `session bind` 认识它——所以这个页在
    /// 默认路径上永远是空的，而它的空态还在指路一个已经不存在的「选择 Agent」
    /// 动作。有执行记录才显示，跟 Agent 页的「全部日志」是同一条规矩。
    private var hasTaskRuns: Bool { !store.taskRuns(for: todo.id).isEmpty }

    private var detailTabItems: [(value: DetailTab, title: String)] {
        var items: [(value: DetailTab, title: String)] = [(.detail, "任务描述")]
        if !isTrashed {
            items.append((.activity, "动态"))
            if hasTaskRuns {
                items.append((.taskRun, "Agent 执行"))
            }
            items.append((.sessions, sessionTabTitle))
        }
        return items
    }

    /// 选中的页消失时把选择拉回来。
    ///
    /// `selectedTab` 是 `@State`，在任务之间切换时不会重置：从一个有执行记录的
    /// 任务切到没有的，选择会停在一个已经不在胶囊里的值上，于是内容区落到
    /// 兜底分支、而胶囊一个都不高亮。回收站视图早就有这个问题（它藏掉三个页），
    /// 只是没人注意。
    private func normalizeSelectedTab() {
        if !detailTabItems.contains(where: { $0.value == selectedTab }) {
            selectedTab = .detail
        }
    }

    private var sessionTabTitle: String {
        let count = store.boundSessions(for: todo.id).count
        return count == 0 ? "Agent Sessions" : "Agent Sessions \(count)"
    }

    private var detailHeader: some View {
        ATMDetailHeader(title: todo.title) {
            HStack(spacing: 7) {
                Label(todo.project ?? "未分项目", systemImage: "folder")
                Image(systemName: "chevron.right")
                    .font(ATMFont.font(.micro, weight: .semibold))
                Text(todo.id.uppercased())
                    .font(ATMFont.mono(.footnote, .semibold))
                    .foregroundStyle(ATMTheme.accent)
                statusBadge
                if store.isActing { ProgressView().controlSize(.small) }
            }
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)
        } actions: {
            detailActions
        }
    }

    @ViewBuilder
    private var detailActions: some View {
        HStack(spacing: 3) {
            if isTrashed {
                actionButton("arrow.uturn.backward", help: "恢复任务") {
                    store.perform(.restore, on: todo)
                }
                Menu {
                    Button(role: .destructive) {
                        deleteCandidate = todo
                    } label: {
                        Label("永久删除…", systemImage: "trash.slash")
                    }
                } label: {
                    ATMIconMenuLabel(
                        systemImage: "ellipsis",
                        help: "更多操作",
                        chrome: .bare,
                        isEnabled: !store.isActing,
                        side: 26,
                        iconTier: .body
                    )
                }
                .menuStyle(.borderlessButton)
            } else {
                let inline = ATMTodoStatusActions.inlineItems(for: todo)
                let overflow = ATMTodoStatusActions.overflowItems(for: todo)
                ForEach(inline) { item in
                    actionButton(item.systemImage, help: item.help) {
                        store.perform(item.action, on: todo)
                    }
                }
                if canContinueTaskRun {
                    actionButton("arrow.trianglehead.clockwise", help: "继续上次 Agent 任务") {
                        presentCodexContinuation()
                    }
                    .disabled(store.isActing)
                }
                if ATMTodoStatusActions.showsLaunchPrompt(for: todo) {
                    Button {
                        presentDispatchSheet()
                    } label: {
                        Label(taskRunActionTitle, systemImage: taskRunActionIcon)
                            .font(ATMFont.font(.footnote, weight: .semibold))
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .help(taskRunActionHelp)
                    .disabled(latestTaskRun?.isActive == true || store.isActing)
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
        VStack(alignment: .leading, spacing: 14) {
            taskMetadata

            if let refineSource = store.refineSource(for: todo.id) {
                Label("曾由 \(refineSource) 整理", systemImage: "sparkles")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.accent)
                    .padding(.horizontal, 9)
                    .padding(.vertical, 5)
                    .background(ATMTheme.accentFill, in: Capsule())
                    .help("历史任务优化来源；描述后续可能已修改")
            }

            if let nextAction = latestNextAction {
                ATMInlineNotice(
                    severity: .info,
                    title: "下一步",
                    message: nextAction
                )
            }

            if let description = nonEmpty(todo.description) {
                detailCard("任务目标", icon: "scope") {
                    ATMMarkdownContentView(source: description)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
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

            if nonEmpty(todo.description) == nil, todo.links?.isEmpty != false {
                Text("暂无任务描述。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.vertical, 16)
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    private var taskMetadata: some View {
        ATMMetadataStrip(items: [
            ATMMetadataItem(
                "project",
                label: "项目",
                value: todo.project ?? "未分项目",
                systemImage: "folder"
            ),
            ATMMetadataItem(
                "priority",
                label: "优先级",
                value: ATMTodoPriorityStyle.label(todo.priority),
                systemImage: "flag",
                valueColor: priorityColor
            ),
            ATMMetadataItem(
                "created",
                label: "创建",
                value: todo.created,
                systemImage: "calendar"
            ),
        ])
        .padding(.bottom, 14)
        .overlay(alignment: .bottom) { Divider() }
    }

    private var latestTaskRun: ATMTaskRun? {
        store.taskRuns(for: todo.id).first
    }

    private var canContinueTaskRun: Bool {
        guard !isTrashed,
              let run = latestTaskRun,
              !run.isActive,
              run.status == "completed" || run.status == "failed" || run.status == "interrupted" else {
            return false
        }
        // Same test as `atm todo run --continue`: an id Codex cannot resolve to a
        // thread would quietly start a fresh session, so don't offer the action.
        return ATMTaskRunSessionRouting.resumableThreadID(run.sessionID) != nil
    }

    private var taskRunSession: ATMLiveSession? {
        guard let run = latestTaskRun else { return nil }
        let visible = store.snapshot.liveStatus.sessions.filter { $0.activityState != "unobserved" }
        return ATMTaskRunSessionRouting.session(for: run, todoID: todo.id, in: visible)
    }

    private var taskRunLaunchRoute: ATMAgentSessionLaunchRoute? {
        guard let run = latestTaskRun else { return nil }
        return ATMAgentSessionLaunchRoute.resolve(for: run, live: taskRunSession)
    }

    /// The run's session as the index knows it, used once it has aged out of live
    /// status. Without this the outcome text disappears from the Todo the moment
    /// the session goes quiet, even though ATM has it indexed.
    private var taskRunArchivedSession: ATMBoundSession? {
        guard let run = latestTaskRun, let sessionID = run.sessionID else { return nil }
        return store.boundSessions(for: todo.id).first {
            $0.sessionID == sessionID || $0.indexedID == sessionID
        }
    }

    private var taskRunRefreshKey: String {
        [
            todo.id,
            selectedTab.rawValue,
            latestTaskRun?.id ?? "none",
            latestTaskRun?.status ?? "none",
        ].joined(separator: "|")
    }

    private var taskRunActionHelp: String {
        switch latestTaskRun?.status {
        case "starting", "running": return "Agent 正在处理"
        case "failed", "interrupted": return "重新委派"
        default: return "委派给 Codex"
        }
    }

    private var taskRunActionTitle: String {
        switch latestTaskRun?.status {
        case "starting", "running": return "委派中"
        case "failed": return "重新委派"
        default: return "委派"
        }
    }

    private var taskRunActionIcon: String {
        switch latestTaskRun?.status {
        case "starting", "running": return "gearshape.2"
        case "failed": return "arrow.clockwise"
        default: return "paperplane.fill"
        }
    }

    @ViewBuilder
    private var taskRunContent: some View {
        if let run = latestTaskRun {
            VStack(alignment: .leading, spacing: 14) {
                    taskRunSummary(run)

                    if let session = taskRunSession {
                        taskRunAgentPreview(session)
                    } else if let archived = taskRunArchivedSession {
                        taskRunArchivedPreview(archived)
                    } else {
                        VStack(alignment: .leading, spacing: 7) {
                            Label(
                                run.isActive ? "正在建立 Agent 会话" : "未找到关联 Agent 会话",
                                systemImage: run.isActive ? "arrow.triangle.2.circlepath" : "person.crop.circle.badge.questionmark"
                            )
                            .font(ATMFont.font(.body, weight: .semibold))
                            Text(taskRunMissingSessionMessage(run))
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                        }
                        .padding(16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .atmWorkspaceCard()
                    }
            }
            .padding(.horizontal, ATMDetailLayout.horizontalPadding)
            .padding(.vertical, 18)
            .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        } else {
            // 这一页已经只在有执行记录时出现，所以这里只可能是加载中的一瞬，或者
            // 记录刚被清掉。不再摆一个空状态插画和一个「委派任务」按钮：委派入口
            // 在头部，而一个装着引导的空页会让人以为自己找错了地方。
            VStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text("正在读取执行记录…")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
            .frame(maxWidth: .infinity, minHeight: 180)
        }
    }

    private func taskRunSummary(_ run: ATMTaskRun) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Circle()
                    .fill(taskRunColor(run.status))
                    .frame(width: 8, height: 8)
                Text(taskRunStatusLabel(run.status))
                    .font(ATMFont.font(.body, weight: .semibold))
                if run.isActive { ProgressView().controlSize(.small) }
                Spacer(minLength: 12)
                if let route = taskRunLaunchRoute, route.isAvailable {
                    Button {
                        openTaskRunSession(route)
                    } label: {
                        Label(route.actionTitle, systemImage: "arrow.up.forward.app")
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .help("\(route.actionTitle)（\(route.destinationLabel)）；交互控制由原生 Agent 提供")
                }
                if run.isActive {
                    Button("中断", role: .destructive) {
                        showingTaskRunInterruptConfirmation = true
                    }
                    .controlSize(.small)
                    .disabled(store.isActing)
                }
                if canContinueTaskRun {
                    Button("继续修改") { presentCodexContinuation() }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                        .disabled(store.isActing)
                }
                if run.status == "failed" || run.status == "interrupted" {
                    Button("重新委派") { presentDispatchSheet() }
                        .controlSize(.small)
                        .disabled(store.isActing)
                }
                Button("刷新") {
                    store.loadTaskRuns(for: todo.id)
                    store.refreshLiveStatus()
                }
                .controlSize(.small)
            }

            Text(run.message ?? "run \(run.id)")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .textSelection(.enabled)
            if let route = taskRunLaunchRoute, route.isAvailable {
                Label(
                    "需要输入、处理授权或使用更多控制时，请在 \(route.destinationLabel) 中继续；ATM 仍会同步状态和日志。",
                    systemImage: "arrow.up.forward.app"
                )
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
            }
            Label(run.workDir, systemImage: "folder")
                .font(ATMFont.mono(.caption))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)
                .textSelection(.enabled)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    private func taskRunMissingSessionMessage(_ run: ATMTaskRun) -> String {
        if taskRunLaunchRoute?.isAvailable == true {
            return run.isActive
                ? "原生会话已经可以打开；ATM 活动索引就绪后，这里还会显示执行动态。"
                : "该执行已超出最近活动窗口，但仍可回到原生 Agent 会话。"
        }
        return run.isActive
            ? "会话进入 Agent 列表后，可在详情中查看执行动态与全部日志。"
            : "该执行可能已超出 Agent 列表的最近活动窗口。"
    }

    private func taskRunAgentPreview(_ session: ATMLiveSession) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 9) {
                ATMAgentMark(agent: session.tool, size: 18)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Agent 详情")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("\(ATMAgentDisplay.clientName(session)) · \(ATMAgentDisplay.projectName(session))")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 8)
                Button {
                    navigation.selectedAgentID = session.id
                    navigation.selectedAgentRunTodoID = todo.id
                    navigation.section = .agents
                } label: {
                    Label("查看详情", systemImage: "chevron.right")
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            }

            Text(session.presenceTitle)
                .font(ATMFont.font(.bodyLarge, weight: .medium))
                .fixedSize(horizontal: false, vertical: true)

            if let result = session.latestResultText {
                Divider()
                ATMMarkdownContentView(source: result)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else if let update = session.visibleUpdates.last {
                Divider()
                ATMMarkdownContentView(source: update)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    /// 会话已经不在实时窗口里时的执行结果，取自持久索引。
    ///
    /// 刻意和实时卡片长得一样但不假装实时：没有状态点、没有「查看详情」跳转（那条路
    /// 指向的是实时列表，这个会话不在里面），只保留 Agent 自己最后说的那段话——
    /// 而它正是验收时唯一要读的东西。
    private func taskRunArchivedPreview(_ session: ATMBoundSession) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 9) {
                ATMAgentMark(agent: session.agent, size: 18)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Agent 执行结果")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("\(ATMAgentDisplay.name(session.agent)) · \(session.shortID)")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 8)
            }

            if let summary = session.summary?.trimmingCharacters(in: .whitespacesAndNewlines),
               !summary.isEmpty {
                Text(summary)
                    .font(ATMFont.font(.bodyLarge, weight: .medium))
                    .fixedSize(horizontal: false, vertical: true)
            }

            if let result = session.latestResult?.trimmingCharacters(in: .whitespacesAndNewlines),
               !result.isEmpty {
                Divider()
                ATMMarkdownContentView(source: result)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    private func openTaskRunSession(_ route: ATMAgentSessionLaunchRoute) {
        do {
            try ATMAgentSessionLauncher.open(route)
        } catch {
            taskRunLaunchError = error.localizedDescription
        }
    }

    private func taskRunStatusLabel(_ status: String) -> String {
        switch status {
        case "starting": return "正在启动"
        case "running": return "正在处理"
        case "completed": return todo.status == "review" ? "已提交验收" : "Agent 已完成"
        case "failed": return "执行失败"
        case "interrupted": return "已中断"
        default: return status
        }
    }

    private func taskRunColor(_ status: String) -> Color {
        switch status {
        case "starting", "running": return ATMTheme.accent
        case "completed": return ATMTheme.success
        case "failed": return ATMTheme.danger
        case "interrupted": return ATMTheme.warning
        default: return ATMTheme.secondary
        }
    }

    private var dispatchAgent: ATMTaskRunAgent? { store.taskRunAgents.first }

    /// Confirms one dispatch. Not a picker any more: Codex is the only target, and
    /// a radio list of one presents a choice that does not exist. What the sheet
    /// still owes the user is what will run, that it is installed, and that it
    /// costs model usage.
    private var taskRunDispatchSheet: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text("委派这个任务")
                    .font(ATMFont.font(.title2, weight: .semibold))
                Text("两种方式：在 Codex 里打开由你盯着做，或者没人在场时让它在后台跑完。都会产生模型用量。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }

            if let agent = dispatchAgent {
                HStack(alignment: .top, spacing: 12) {
                    ATMAgentMark(agent: agent.id, size: 22)
                    VStack(alignment: .leading, spacing: 5) {
                        HStack(spacing: 7) {
                            Text(agent.name)
                                .font(ATMFont.font(.body, weight: .semibold))
                            Text(agent.available ? "已安装" : "未安装")
                                .font(ATMFont.caption)
                                .foregroundStyle(agent.available ? ATMTheme.success : ATMTheme.secondary)
                        }
                        Text(agent.costNote)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        if !agent.available {
                            Label("PATH 里找不到 \(agent.binary)，先安装 Codex CLI 再委派。",
                                  systemImage: "exclamationmark.shield")
                                .font(ATMFont.caption)
                                .foregroundStyle(ATMTheme.warning)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    Spacer(minLength: 8)
                }
                .padding(13)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 10))
                .overlay { RoundedRectangle(cornerRadius: 10).stroke(ATMTheme.border) }
                .opacity(agent.available ? 1 : 0.58)
            } else {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("正在检查 Codex 是否已安装…")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 96)
            }

            // 「在 Codex 里打开」是主动作，不是因为它更常用，而是因为它是可逆的：
            // 输入框填好但不提交，人还能改、能撤、能换目录。后台跑完是不可逆的那一个。
            Text("在 Codex 里打开：填好这条任务的指针，等你按回车。后台跑完：无人应答的授权请求会让它失败。")
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)

            HStack {
                Spacer()
                Button("取消") { showingDispatchSheet = false }
                    .keyboardShortcut(.cancelAction)
                Button("后台跑完") { dispatchToAgent() }
                    .disabled(dispatchAgent?.available != true || store.isActing)
                Button("在 Codex 里打开") { handoffToCodex() }
                    .buttonStyle(.borderedProminent)
                    .disabled(dispatchAgent?.available != true || store.isActing)
            }
        }
        .padding(22)
        .frame(width: 520)
        .background(ATMTheme.canvas)
    }

    private func presentDispatchSheet() {
        showingDispatchSheet = true
        store.loadTaskRunAgents()
    }

    private func dispatchToAgent() {
        guard let agent = dispatchAgent, agent.available else { return }
        showingDispatchSheet = false
        store.dispatchTodo(todo, agent: agent)
    }

    private func handoffToCodex() {
        guard dispatchAgent?.available == true else { return }
        showingDispatchSheet = false
        store.handoffTodo(todo)
    }

    private var codexContinuationSheet: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text("继续上次 \(latestTaskRunAgentName) 任务")
                    .font(ATMFont.font(.title2, weight: .semibold))
                Text("\(latestTaskRunAgentName) 会保留上次执行的上下文，并把这次修改记录为新的执行轮次和模型用量。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }

            ZStack(alignment: .topLeading) {
                TextEditor(text: $codexContinuationInstructions)
                    .font(ATMFont.body)
                    .scrollContentBackground(.hidden)
                    .padding(7)
                    .frame(minHeight: 150)
                    .background(ATMTheme.white, in: RoundedRectangle(cornerRadius: 8))
                    .overlay(RoundedRectangle(cornerRadius: 8).stroke(ATMTheme.border))
                if codexContinuationInstructions.isEmpty {
                    Text("描述要调整的内容，例如：按钮改成主操作，并补充失败态测试")
                        .font(ATMFont.body)
                        .foregroundStyle(ATMTheme.secondary.opacity(0.72))
                        .padding(.horizontal, 13)
                        .padding(.vertical, 15)
                        .allowsHitTesting(false)
                }
            }

            HStack {
                Spacer()
                Button("取消") { showingCodexContinuation = false }
                    .keyboardShortcut(.cancelAction)
                Button("继续修改") { submitCodexContinuation() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.return, modifiers: .command)
                    .disabled(
                        codexContinuationInstructions
                            .trimmingCharacters(in: .whitespacesAndNewlines)
                            .isEmpty || store.isActing
                    )
            }
        }
        .padding(22)
        .frame(width: 520)
        .background(ATMTheme.canvas)
    }

    private func presentCodexContinuation() {
        codexContinuationInstructions = ""
        showingCodexContinuation = true
    }

    private func submitCodexContinuation() {
        let instructions = codexContinuationInstructions
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instructions.isEmpty, let run = latestTaskRun else { return }
        showingCodexContinuation = false
        store.continueTodo(todo, run: run, instructions: instructions)
    }

    private var latestTaskRunAgentName: String {
        guard let id = latestTaskRun?.agent else { return "Agent" }
        if let name = store.taskRunAgents.first(where: { $0.id == id })?.name { return name }
        switch id {
        case "codex": return "Codex"
        case "grokbuild": return "Grok Build"
        case "antigravity": return "Antigravity"
        case "pi": return "Pi"
        default: return "Agent"
        }
    }

    /// Dynamic entries have their own destination, so the timeline no longer sits
    /// inside a second titled card. The latest next action is surfaced beside the
    /// task description, where it can guide the work instead of reading as history.
    private var activityContent: some View {
        TodoProgressView(todo: todo, store: store)
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.vertical, 16)
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    /// Sessions are independent durable objects, so each one owns a bounded card;
    /// the page itself stays untitled because the tab already names the collection.
    private var sessionContent: some View {
        TodoSessionHistoryView(todo: todo, store: store)
            .padding(.horizontal, ATMDetailLayout.horizontalPadding)
            .padding(.vertical, 16)
            .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    private var latestNextAction: String? {
        guard todo.status != "done", todo.status != "dropped" else { return nil }
        return store.progress(for: todo.id).reversed().compactMap(\.nextAction).first
    }

    /// Two tiers, because the old flat run of eight identical fields gave 来源 the
    /// same weight as 标题: the two content fields get full-width boxes, everything
    /// else is metadata inside one aligned card.
    ///
    /// Short fields first, then the description. 描述 is the one field with no upper
    /// bound on height — below it, every attribute sat under the fold, so setting a
    /// priority meant scrolling past the whole body text to reach it.
    private var editContent: some View {
        VStack(alignment: .leading, spacing: 18) {
                // Titles here are usually a whole sentence — the task is typed as
                // one line and the description stays empty — so the field wraps and
                // grows instead of showing a 40-character window of it.
                editSection("标题", hint: "单行；⏎ 保存") {
                    ATMGrowingTextField(
                        text: $title,
                        placeholder: "任务标题",
                        font: ATMFont.nsFont(.body, weight: .medium),
                        maxLines: 6
                    ) { saveEdit() }
                        .background(ATMTheme.white, in: RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
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
        // Capped and left-aligned: stretched across a wide detail pane, the
        // text fields ran on for hundreds of points.
        .frame(maxWidth: 620, alignment: .leading)
        .padding(.horizontal, 18)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
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

    /// Opens the edit form when the task row's 编辑任务 pointed at this todo. The
    /// request is cleared on the way in so re-selecting the same row later does not
    /// drop the user back into an edit form.
    private func consumeEditRequest() {
        guard navigation.editTodoID == todo.id else { return }
        navigation.editTodoID = nil
        guard !isTrashed, !isEditing else { return }
        beginEditing()
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
        // The enclosing detail surface owns the horizontal and top inset. Keep
        // only space before this section's divider so embedded sections do not
        // turn into a second, padded card inside the body card.
        .padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard(cornerRadius: 11)
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
            if !ATMTodoStatusActions.isClosed(todo) {
                Button {
                    store.refineTodo(id: todo.id)
                } label: {
                    Label("优化任务", systemImage: "wand.and.stars")
                }
                .disabled(store.refiningTodoIDs.contains(todo.id))
            }
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
            Button {
                store.perform(.trash, on: todo)
            } label: {
                Label("移到回收站", systemImage: "trash")
            }
        } label: {
            ATMIconMenuLabel(
                systemImage: "ellipsis",
                help: "更多操作",
                chrome: .bare,
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
        let color = isTrashed ? ATMTheme.secondary : ATMTodoStatusStyle.color(for: todo)
        return HStack(spacing: 3) {
            if isTrashed {
                Image(systemName: "trash")
            } else {
                ATMTodoStatusGlyph(todo: todo, tier: .caption)
            }
            Text(isTrashed ? "已删除" : ATMTodoStatusStyle.label(for: todo))
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
                autoFocus: true,
                onSubmit: { submit(draft) }
            )
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
        VStack(spacing: 0) {
            usageModuleChrome
            Divider()
            ATMDetailTabs { usagePagePicker }
            ATMDetailBodySurface {
                LazyVStack(alignment: .leading, spacing: 20) {
                    usageFilterToolbar

                    // Quota is a pinned top-level summary, independent of the
                    // overview / today-sessions tab and usage filters below.
                    if !quota.isEmpty {
                        quotaModule
                    }

                    usageModule
                }
                .padding(.horizontal, 28)
                .padding(.top, 24)
                .padding(.bottom, 36)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(
                    LinearGradient(
                        colors: [ATMTheme.accent.opacity(0.025), ATMTheme.elevated, ATMTheme.elevated],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
            }
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
            if pageTab == .overview {
                let metrics = snapshot.usageMetrics(for: range, filters: filters)
                let featuredMetrics = metrics.filter(isFeaturedMetric)
                let supportingMetrics = metrics.filter { !isFeaturedMetric($0) }
                let trendStats = snapshot.filteredLineTrendStats(for: range, filters: filters)
                VStack(alignment: .leading, spacing: 10) {
                    Text("关键指标")
                        .font(ATMFont.font(.title3, weight: .semibold))
                    LazyVGrid(columns: Self.featuredMetricColumns, spacing: 10) {
                        ForEach(Array(featuredMetrics.enumerated()), id: \.offset) { _, metric in
                            metricCard(metric, trendValues: metricTrendValues(metric, from: trendStats))
                        }
                    }
                    if !supportingMetrics.isEmpty {
                        supportingMetricsStrip(supportingMetrics)
                    }
                }

                usageTrendCard
                dualPanelSection
            } else {
                todaySessionsCard
            }
        }
    }

    /// 右栏的页面分页，跟任务 / Agent / 收集详情共用胶囊分段，不用系统 segmented control：
    /// 后者在这里是自成一路的第三种 tab 样式，而且定宽 260 会把两个短标签撑得过散。
    private var usagePagePicker: some View {
        ATMCapsuleTabs(
            selection: $pageTab,
            items: ATMUsagePageTab.allCases.map { (value: $0, title: $0.title) }
        )
        .accessibilityLabel("用量页面")
    }

    /// The header owns identity and global health only. The page switch is the
    /// separate strip between this band and the content card.
    private var usageModuleChrome: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("用量")
                        .font(ATMFont.font(.title1, weight: .bold))
                    Text("额度、成本与模型效率")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                    HStack(spacing: 6) {
                        Circle()
                            .fill(ATMTheme.success)
                            .frame(width: 7, height: 7)
                        Text(usageScopeLabel)
                    }
                    .font(ATMFont.font(.caption, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 8)
                dataHealthButton
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("用量")
                        .font(ATMFont.font(.title1, weight: .bold))
                    Text("额度、成本与模型效率")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                    HStack(spacing: 6) {
                        Circle()
                            .fill(ATMTheme.success)
                            .frame(width: 7, height: 7)
                        Text(usageScopeLabel)
                    }
                    .font(ATMFont.font(.caption, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                }
                HStack {
                    Spacer()
                    dataHealthButton
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 28)
        .padding(.top, 24)
        .padding(.bottom, 18)
        .background(ATMTheme.elevated)
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
                Image(systemName: "slider.horizontal.3")
                    .font(ATMFont.font(.body, weight: .medium))
                    .foregroundStyle(ATMTheme.secondary)
                    .help("模型、客户端和项目筛选")
            }
            VStack(alignment: .leading, spacing: 8) {
                usageRangeOrRefreshControl
                usageFilterControls
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill.opacity(0.55), in: RoundedRectangle(cornerRadius: ATMRadius.control))
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
        let bucketDates = points.map(\.date)
        let peakIDs = Set(
            points
                .filter { $0.value > 0 }
                .sorted { $0.value > $1.value }
                .prefix(2)
                .map(\.id)
        )
        // Hour or day comes from the buckets themselves, not from the window: every
        // single-day window is drawn in hours when the snapshot carries them, and in
        // one day bucket when it does not.
        let hourlyAxis = ATMUsageDateAxis.isHourly(bucketDates)
        return ATMDataPanel {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(range.tokenTrendTitle)
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                    Text(trendMetric == .tokens ? "Token" : "每秒输出 Token")
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer(minLength: 12)
                Picker("", selection: $trendMetric) {
                    ForEach(ATMUsageTrendMetric.allCases) { item in Text(item.title).tag(item) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 154)
                .help("Token 看用了多少，速度看模型每秒输出多少 token（不含工具执行时间）")
            }
        } content: {
            if points.isEmpty {
                usageEmptyState(trendMetric.emptyStateTitle, icon: "chart.xyaxis.line")
            } else {
                let dominantSeries = seriesNames.first
                let dominantSeriesColor = seriesChartColors(
                    for: dominantSeries.map { [$0] } ?? [],
                    available: availableSeries
                ).first ?? ATMTheme.accent
                Chart(points) { point in
                    if point.series == dominantSeries {
                        AreaMark(
                            x: .value("日期", point.day),
                            y: .value(trendMetric.axisTitle, point.value)
                        )
                        .foregroundStyle(
                            LinearGradient(
                                colors: [
                                    dominantSeriesColor.opacity(0.16),
                                    dominantSeriesColor.opacity(0.01),
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
                    if peakIDs.contains(point.id) {
                        RuleMark(x: .value("峰值日期", point.day))
                            .foregroundStyle(ATMTheme.secondary.opacity(0.24))
                            .lineStyle(StrokeStyle(lineWidth: 0.8, dash: [3, 3]))
                            .annotation(position: .top, spacing: 4) {
                                VStack(spacing: 1) {
                                    Text(point.day, format: hourlyAxis
                                        ? .dateTime.hour().minute()
                                        : .dateTime.month(.defaultDigits).day())
                                    Text(trendMetric == .speed
                                        ? String(format: "%.0f", point.value)
                                        : NumberFormat.compact(Int(point.value)))
                                        .fontWeight(.semibold)
                                }
                                .font(ATMFont.mono(.micro))
                                .foregroundStyle(ATMTheme.secondary)
                            }
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
                        bucketDates,
                        // A month of daily ticks overlaps at this width; a week fits.
                        maximumLabels: range == .last30Days || range == .thisMonth ? 6 : 7
                    )) { value in
                        AxisTick(stroke: StrokeStyle(lineWidth: 0.7))
                            .foregroundStyle(ATMTheme.chartGrid)
                        AxisValueLabel {
                            if let date = value.as(Date.self) {
                                if hourlyAxis {
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
                    domain: ATMUsageDateAxis.paddedDomain(bucketDates),
                    range: .plotDimension(padding: 18)
                )
                .chartPlotStyle { plotArea in
                    plotArea
                        .background(
                            LinearGradient(
                                colors: [ATMTheme.accent.opacity(0.025), ATMTheme.chartPlotFill],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )
                        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                }
                .frame(height: 270)
            }
        }
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
                VStack(spacing: 9) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(ATMTheme.accent.opacity(0.07))
                        Image(systemName: "sparkles")
                            .font(ATMFont.font(.title2, weight: .medium))
                            .foregroundStyle(ATMTheme.accent)
                    }
                    .frame(width: 48, height: 48)
                    Text("暂无 Skill 调用")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("在对话或自动化任务中使用 Skill 后，调用分布会显示在这里。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 320)
                }
                .frame(maxWidth: .infinity, minHeight: 180)
            } else {
                VStack(spacing: 9) {
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
                VStack(spacing: 9) {
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
                    RoundedRectangle(cornerRadius: 2, style: .continuous)
                        .fill(ATMTheme.palette[index % ATMTheme.palette.count])
                        .frame(width: 3, height: 24)
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
        let level = ATMQuotaLevel.level(forPercent: percent)
        let color = ATMTheme.quotaColor(level)
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

            HStack(spacing: 14) {
                quotaGauge(percent: percent, color: color)

                VStack(alignment: .leading, spacing: 5) {
                    Text("已用")
                        .font(ATMFont.font(.body, weight: .semibold))
                    HStack(spacing: 5) {
                        Circle()
                            .fill(color)
                            .frame(width: 6, height: 6)
                        Text(quotaStatusLabel(level))
                    }
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)

                    // The rate turns the number into something to act on. Absent
                    // until history exists, so a fresh install stays quiet.
                    if let trend = window.trend {
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
                Spacer(minLength: 0)
            }

            if !card.products.isEmpty {
                // Keep product attribution as text; the circle is the only
                // progress visualization on every quota card.
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
        .frame(maxWidth: .infinity, minHeight: 172, alignment: .topLeading)
        .background(
            LinearGradient(
                colors: [color.opacity(0.055), ATMTheme.elevated, ATMTheme.elevated],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            ),
            in: RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
                .stroke(color.opacity(0.16))
        )
        .help("\(label) \(window.windowLabel) 窗口：\(String(format: "%.1f", percent))% 已用，\(window.resetText)")
    }

    private func quotaStatusLabel(_ level: ATMQuotaLevel) -> String {
        switch level {
        case .healthy: return "额度充足"
        case .warning: return "注意用量"
        case .critical: return "接近上限"
        }
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
        let primaryMetric = payload.metrics.first
        let percent = max(0, primaryMetric?.usedPercent ?? 0)
        let level = ATMQuotaLevel.level(forPercent: percent)
        let color = payload.isUnavailable ? ATMTheme.warning : ATMTheme.quotaColor(level)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 5) {
                ATMAgentMark(agent: card.agent, size: 15)
                Text(label)
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .lineLimit(1)
                if let period = payload.period, !period.isEmpty {
                    Text(period)
                        .font(ATMFont.mono(.caption, .semibold))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(ATMTheme.controlFill, in: Capsule())
                        .fixedSize()
                }
                Spacer(minLength: 4)
                Text(card.providerLabel)
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
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

            if payload.isUnavailable {
                providerQuotaEmptyState(payload)
            } else if let metric = primaryMetric {
                HStack(spacing: 14) {
                    quotaGauge(percent: percent, color: color)

                    VStack(alignment: .leading, spacing: 5) {
                        Text(payload.title)
                            .font(ATMFont.font(.body, weight: .semibold))
                            .lineLimit(1)
                        HStack(spacing: 5) {
                            Circle()
                                .fill(color)
                                .frame(width: 6, height: 6)
                            Text(quotaStatusLabel(level))
                        }
                        .font(ATMFont.caption)
                        .foregroundStyle(ATMTheme.secondary)
                        Text("\(metric.label) \(metric.valueText)")
                            .font(ATMFont.mono(.caption, .semibold))
                            .foregroundStyle(ATMTheme.secondary)
                            .lineLimit(1)
                            .minimumScaleFactor(0.72)
                    }
                    Spacer(minLength: 0)
                }

                if payload.metrics.count > 1 {
                    HStack(spacing: 10) {
                        ForEach(Array(payload.metrics.dropFirst())) { extraMetric in
                            providerQuotaMetricSummary(extraMetric)
                        }
                    }
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
        .frame(maxWidth: .infinity, minHeight: 172, alignment: .topLeading)
        .background(
            LinearGradient(
                colors: [color.opacity(0.055), ATMTheme.elevated, ATMTheme.elevated],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            ),
            in: RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: ATMRadius.panel, style: .continuous)
                .stroke(isHovered ? ATMTheme.accent : color.opacity(0.16))
        )
        // The card's own padding is part of the hit area, not a dead margin.
        .contentShape(RoundedRectangle(cornerRadius: ATMRadius.panel))
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

    private func providerQuotaMetricSummary(_ metric: ATMProviderQuotaMetric) -> some View {
        let percent = max(0, metric.usedPercent)
        let color = ATMTheme.quotaColor(ATMQuotaLevel.level(forPercent: percent))
        return HStack(spacing: 4) {
            Circle()
                .fill(color)
                .frame(width: 5, height: 5)
            Text(metric.label)
                .font(ATMFont.caption)
            Text(String(format: "%.0f%%", percent))
                .font(ATMFont.mono(.caption))
        }
        .foregroundStyle(ATMTheme.secondary)
        .lineLimit(1)
        .minimumScaleFactor(0.8)
    }

    /// Shared by built-in windows and provider readings so quota cards use one
    /// progress language: a single circular gauge, never a duplicate bar.
    private func quotaGauge(percent: Double, color: Color) -> some View {
        ZStack {
            Circle()
                .stroke(ATMTheme.controlFill, lineWidth: 5)
            Circle()
                .trim(from: 0, to: max(0, min(1, percent / 100)))
                .stroke(color, style: StrokeStyle(lineWidth: 5, lineCap: .round))
                .rotationEffect(.degrees(-90))
            HStack(alignment: .firstTextBaseline, spacing: 1) {
                Text(String(format: "%.0f", percent))
                    .font(ATMFont.mono(.title2, .bold))
                Text("%")
                    .font(ATMFont.mono(.micro, .semibold))
            }
            .foregroundStyle(ATMTheme.primary)
        }
        .frame(width: 62, height: 62)
    }


    /// Stable per-position colors for the product split; skips the palette's
    /// trailing neutral, which is reserved for "其他" aggregates.
    private static func quotaProductColor(_ index: Int) -> Color {
        let colors = ATMTheme.palette.dropLast()
        return colors[index % colors.count]
    }

    @ViewBuilder
    private func metricCard(
        _ metric: ATMUsageMetric,
        trendValues: [Double] = [],
        compact: Bool = false
    ) -> some View {
        switch metric {
        case let .seriesCount(count, title):
            metricCard(
                "\(title)数",
                .plain("\(count)"),
                "square.stack.3d.up",
                valueColor: ATMTheme.palette[2],
                trendValues: trendValues,
                compact: compact
            )
        case let .tokens(value):
            metricCard(
                "总 Token",
                .compact(value),
                "number.circle.fill",
                valueColor: ATMTheme.accent,
                trendValues: trendValues,
                compact: compact
            )
        case let .output(value):
            metricCard(
                "输出",
                .compact(value),
                "arrow.up.right.circle",
                valueColor: ATMTheme.palette[3],
                trendValues: trendValues,
                compact: compact
            )
        case let .cacheHitRate(rate):
            metricCard(
                "缓存命中率",
                .percent(rate),
                "bolt.shield.fill",
                valueColor: ATMTheme.cacheHitColor(rate),
                trendValues: trendValues,
                compact: compact
            )
        case let .sessions(count):
            metricCard(
                "会话",
                .plain("\(count)"),
                "bubble.left.and.bubble.right",
                valueColor: ATMTheme.palette[1],
                trendValues: trendValues,
                compact: compact
            )
        case let .queries(count):
            metricCard(
                "提问",
                .plain("\(count)"),
                "text.bubble",
                valueColor: ATMTheme.palette[4],
                trendValues: trendValues,
                compact: compact
            )
        case let .cost(value):
            metricCard(
                "估算费用",
                .plain(NumberFormat.currency(value)),
                "dollarsign.circle.fill",
                valueColor: ATMTheme.accent,
                trendValues: trendValues,
                compact: compact
            )
        case let .throughput(value):
            metricCard(
                "输出速度",
                .throughput(value),
                "speedometer",
                valueColor: ATMTheme.palette[5],
                trendValues: trendValues,
                compact: compact
            )
                .help("模型自身的生成速度，不含工具执行时间；由日志时间戳推导，只统计可测的请求")
        case let .turnWait(seconds):
            metricCard(
                "等待中位数",
                .duration(seconds),
                "hourglass",
                valueColor: ATMTheme.warning,
                trendValues: trendValues,
                compact: compact
            )
                .help("从你发出消息到模型给出最后一句回复，含工具执行与该轮内部的每次请求")
        }
    }

    private func metricCard(
        _ title: String,
        _ value: ATMMetricDisplayValue,
        _ icon: String,
        valueColor: Color = ATMTheme.primary,
        trendValues: [Double] = [],
        compact: Bool = false
    ) -> some View {
        VStack(alignment: .leading, spacing: compact ? 6 : 9) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(valueColor)
                    .frame(width: 22, height: 22)
                    .background(valueColor.opacity(0.11), in: RoundedRectangle(cornerRadius: 6))
                Text(title)
                    .foregroundStyle(ATMTheme.secondary)
                Spacer(minLength: 0)
            }
                .font(ATMFont.font(.footnote, weight: .semibold))
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            HStack(alignment: .bottom, spacing: 8) {
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
                Spacer(minLength: 4)
                if !compact, trendValues.count > 1 {
                    metricSparkline(trendValues, color: valueColor)
                        .frame(width: 82, height: 32)
                }
            }
        }
        .padding(compact ? 12 : 16)
        .frame(maxWidth: .infinity, minHeight: compact ? 72 : 92, alignment: .leading)
        .background(
            LinearGradient(
                colors: [valueColor.opacity(compact ? 0.025 : 0.045), ATMTheme.elevated],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            ),
            in: RoundedRectangle(
                cornerRadius: compact ? ATMRadius.row : ATMRadius.panel,
                style: .continuous
            )
        )
        .overlay(
            RoundedRectangle(
                cornerRadius: compact ? ATMRadius.row : ATMRadius.panel,
                style: .continuous
            )
                .stroke(valueColor.opacity(compact ? 0.09 : 0.13))
        )
    }

    private func metricSparkline(_ values: [Double], color: Color) -> some View {
        Chart(Array(values.enumerated()), id: \.offset) { item in
            AreaMark(
                x: .value("序号", item.offset),
                y: .value("值", item.element)
            )
            .foregroundStyle(
                LinearGradient(
                    colors: [color.opacity(0.18), color.opacity(0.01)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )
            .interpolationMethod(.catmullRom)
            LineMark(
                x: .value("序号", item.offset),
                y: .value("值", item.element)
            )
            .foregroundStyle(color)
            .lineStyle(StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
            .interpolationMethod(.catmullRom)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .accessibilityHidden(true)
    }

    /// KPI sparklines use the same filtered buckets as the main chart. They are
    /// not decorative sample curves: each metric chooses an honest aggregate
    /// that can be derived from the bucket payload.
    private func metricTrendValues(
        _ metric: ATMUsageMetric,
        from points: [ATMUsageSeriesPoint]
    ) -> [Double] {
        var dates: [String] = []
        for point in points where !dates.contains(point.date) {
            dates.append(point.date)
        }
        return dates.compactMap { date in
            let bucket = points.filter { $0.date == date }
            switch metric {
            case .tokens:
                return Double(bucket.reduce(0) { $0 + $1.totalTokens })
            case .cacheHitRate:
                let input = bucket.reduce(0) { $0 + $1.inputTokens }
                guard input > 0 else { return nil }
                return min(
                    Double(bucket.reduce(0) { $0 + $1.cacheReadTokens }) / Double(input),
                    1
                )
            case .cost:
                return bucket.reduce(0) { $0 + $1.costUSD }
            case .throughput:
                let duration = bucket.reduce(0) { $0 + $1.measuredDurationMS }
                let output = bucket.reduce(0) { $0 + $1.measuredOutputTokens }
                guard duration > 0, output > 0 else { return nil }
                return Double(output) / (Double(duration) / 1000)
            default:
                return nil
            }
        }
    }

    @ViewBuilder
    private func supportingMetricsStrip(_ metrics: [ATMUsageMetric]) -> some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 0) {
                ForEach(Array(metrics.enumerated()), id: \.offset) { index, metric in
                    compactMetricCell(metric)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if index < metrics.count - 1 {
                        Divider()
                            .padding(.vertical, 4)
                    }
                }
            }
            .padding(.vertical, 8)

            LazyVGrid(columns: Self.supportingMetricColumns, spacing: 8) {
                ForEach(Array(metrics.enumerated()), id: \.offset) { _, metric in
                    metricCard(metric, compact: true)
                }
            }
        }
    }

    @ViewBuilder
    private func compactMetricCell(_ metric: ATMUsageMetric) -> some View {
        switch metric {
        case let .seriesCount(count, title):
            compactMetricCell("\(title)数", .plain("\(count)"), "square.stack.3d.up", ATMTheme.palette[2])
        case let .tokens(value):
            compactMetricCell("总 Token", .compact(value), "number.circle.fill", ATMTheme.accent)
        case let .output(value):
            compactMetricCell("输出", .compact(value), "arrow.up.right.circle", ATMTheme.palette[3])
        case let .cacheHitRate(rate):
            compactMetricCell("缓存命中率", .percent(rate), "bolt.shield.fill", ATMTheme.cacheHitColor(rate))
        case let .sessions(count):
            compactMetricCell("会话", .plain("\(count)"), "bubble.left.and.bubble.right", ATMTheme.palette[1])
        case let .queries(count):
            compactMetricCell("提问", .plain("\(count)"), "text.bubble", ATMTheme.palette[4])
        case let .cost(value):
            compactMetricCell("估算费用", .plain(NumberFormat.currency(value)), "dollarsign.circle.fill", ATMTheme.accent)
        case let .throughput(value):
            compactMetricCell("输出速度", .throughput(value), "speedometer", ATMTheme.palette[5])
        case let .turnWait(seconds):
            compactMetricCell("等待中位数", .duration(seconds), "hourglass", ATMTheme.warning)
        }
    }

    private func compactMetricCell(
        _ title: String,
        _ value: ATMMetricDisplayValue,
        _ icon: String,
        _ color: Color
    ) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .foregroundStyle(color)
                Text(title)
                    .lineLimit(1)
                Spacer(minLength: 0)
            }
            .font(ATMFont.font(.caption, weight: .semibold))
            .foregroundStyle(ATMTheme.secondary)
            HStack(alignment: .firstTextBaseline, spacing: 3) {
                Text(value.main)
                    .font(ATMFont.rounded(.title3, .bold))
                    .foregroundStyle(ATMTheme.primary)
                if !value.unit.isEmpty {
                    Text(value.unit)
                        .font(ATMFont.rounded(.caption, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 4)
    }

    private var usageScopeLabel: String {
        let scope = pageTab == .todaySessions ? "今天" : range.pickerTitle
        return "\(scope) · 本地统计"
    }

    private func usageCard<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        ATMDataPanel {
            Text(title)
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
        } content: {
            content()
        }
    }

    /// 图表卡片里的占位。用量页的空态一律落在卡片内部，所以固定走 `.inline` 那一档，
    /// 高度跟着图表区，避免卡片在有数据 / 没数据之间抽一下。
    private func usageEmptyState(_ title: String, icon: String) -> some View {
        ATMEmptyState(icon: icon, title: title, size: .inline, minHeight: 220)
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
