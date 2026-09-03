import Foundation

/// Selection never changes these values. Keep all task-list derivation behind
/// the task collection's revision so a click cannot sort completed history.
struct ATMTaskPresentation {
    let groups: [ATMTaskGroup]
    let managedGroups: [ATMTaskGroup]
    let flattenedTodos: [ATMTodo]
    let visibleTodos: [ATMTodo]
    let todosByID: [String: ATMTodo]
    let archivedIDs: Set<String>
    let defaultTodoID: String?

    init(todos: [ATMTodo], archived: [ATMTodo], now: Date) {
        groups = ATMTaskQuery.groups(from: todos, includingArchived: archived, now: now).map {
            ATMTaskGroup(id: $0.id, title: $0.title, todos: $0.todos)
        }
        let groupsByID = Dictionary(uniqueKeysWithValues: groups.map { ($0.id, $0) })
        managedGroups = (ATMTaskQuery.groupSpecs + [ATMTaskQuery.archiveGroupSpec]).map {
            groupsByID[$0.id] ?? ATMTaskGroup(id: $0.id, title: $0.title, todos: [])
        }
        flattenedTodos = groups.flatMap(\.todos)
        visibleTodos = todos + archived
        // Favor the current task if a restore refresh briefly also includes an
        // archived copy; this matches the old first(where:) selection behavior.
        todosByID = Dictionary(visibleTodos.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        archivedIDs = Set(archived.map(\.id))
        defaultTodoID = ATMTaskQuery.preferredDefault(in: todos.isEmpty ? archived : todos)?.id
    }
}

enum ATMTaskSelection {
    /// Work and archives arrive independently. A fast archive must not choose
    /// the initial task, and an unresolved explicit link may belong to the
    /// source that has not returned yet.
    static func resolve(
        currentID: String?,
        in presentation: ATMTaskPresentation,
        workSettled: Bool,
        archivesSettled: Bool
    ) -> String? {
        if let currentID {
            if presentation.todosByID[currentID] != nil { return currentID }
            if !workSettled || !archivesSettled { return currentID }
        }
        guard workSettled else { return currentID }
        return presentation.defaultTodoID
    }
}

final class ATMTaskPresentationCache {
    private(set) var rebuildCount = 0
    private var version: UInt64?
    private var day: Date?
    private var timeZone: TimeZone?
    private var value: ATMTaskPresentation?

    func presentation(
        version: UInt64,
        now: Date = Date(),
        todos: () -> [ATMTodo],
        archived: () -> [ATMTodo]
    ) -> ATMTaskPresentation {
        let calendar = Calendar.current
        let day = calendar.startOfDay(for: now)
        if self.version == version, self.day == day,
           timeZone == calendar.timeZone, let value {
            return value
        }
        let next = ATMTaskPresentation(todos: todos(), archived: archived(), now: now)
        rebuildCount += 1
        self.version = version
        self.day = day
        self.timeZone = calendar.timeZone
        self.value = next
        return next
    }
}

/// A sheet can recompute its text recommendation without scanning and sorting
/// all tasks again for each keystroke or for each menu field.
final class ATMTodoProjectCache {
    private var version: UInt64?
    private(set) var ranked: [String] = []
    private(set) var alphabetical: [String] = []

    func projects(version: UInt64, todos: () -> [ATMTodo]) -> ATMTodoProjectCache {
        guard self.version != version else { return self }
        ranked = ATMTodoSuggestion.knownProjects(in: todos())
        alphabetical = ranked.sorted()
        self.version = version
        return self
    }
}
