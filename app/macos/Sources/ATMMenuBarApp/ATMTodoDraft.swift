import Foundation

/// What the add-task sheet sends to `atm todo add`. The composer is one block of
/// text: the first non-empty line is the title, everything after it is the
/// description, so a task and its details are typed in one go.
struct ATMTodoDraft: Equatable {
    let title: String
    let description: String
    let project: String
    let priority: String
    let lane: String

    init(text: String, project: String, priority: String, lane: String) {
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let titleIndex = lines.firstIndex { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        self.title = titleIndex.map { lines[$0].trimmingCharacters(in: .whitespaces) } ?? ""
        self.description = titleIndex
            .map { lines[(($0) + 1)...].joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines) }
            ?? ""
        self.project = project.trimmingCharacters(in: .whitespacesAndNewlines)
        self.priority = priority
        self.lane = lane
    }

    var isSubmittable: Bool { !title.isEmpty }
}

/// Project, priority and lane inferred from what was typed plus what the existing
/// todos and live sessions already say. Everything here is a recommendation the
/// sheet shows with its reason and lets you override -- picking all three by hand
/// on every task was the part that felt like busywork.
struct ATMTodoSuggestion: Equatable {
    var project: String
    var projectReason: String
    var priority: String
    var priorityReason: String
    var lane: String
    var laneReason: String

    static let empty = ATMTodoSuggestion(
        project: "",
        projectReason: "无历史项目可参考",
        priority: "P1",
        priorityReason: "默认",
        lane: "personal",
        laneReason: "默认"
    )

    /// Words that move a task off the default priority. They are matched against
    /// the raw text, so a recommendation can be wrong -- which is why the sheet
    /// shows it as a chip you can change rather than applying it silently.
    private static let urgentMarkers = [
        "紧急", "立刻", "马上", "尽快", "崩", "挂了", "故障", "线上", "阻塞", "卡住", "严重", "数据丢失",
        "blocker", "asap", "urgent", "outage",
    ]
    private static let deferrableMarkers = [
        "顺手", "有空", "以后", "后续", "欠账", "技术债", "不急", "暂时", "低优", "小问题",
        "later", "someday", "nice to have", "tech debt", "cleanup",
    ]

    static func infer(
        text: String,
        todos: [ATMTodo],
        liveSessions: [ATMLiveSession] = []
    ) -> ATMTodoSuggestion {
        let haystack = text.lowercased()
        var suggestion = empty

        let known = knownProjects(in: todos)
        if let mentioned = known.first(where: { haystack.contains($0.lowercased()) }) {
            suggestion.project = mentioned
            suggestion.projectReason = "文本提到 \(mentioned)"
        } else if let live = liveSessions
            .filter({ !$0.project.isEmpty })
            .min(by: { $0.ageSeconds < $1.ageSeconds }) {
            suggestion.project = matchKnown(live.project, in: known) ?? live.project
            suggestion.projectReason = "当前会话在 \(live.project)"
        } else if let recent = known.first {
            suggestion.project = recent
            suggestion.projectReason = "最近常用项目"
        }

        if let explicit = explicitPriority(in: haystack) {
            suggestion.priority = explicit
            suggestion.priorityReason = "文本写了 \(explicit)"
        } else if let marker = urgentMarkers.first(where: { haystack.contains($0) }) {
            suggestion.priority = "P0"
            suggestion.priorityReason = "文本提到「\(marker)」"
        } else if let marker = deferrableMarkers.first(where: { haystack.contains($0) }) {
            suggestion.priority = "P2"
            suggestion.priorityReason = "文本提到「\(marker)」"
        }

        if let lane = dominantLane(of: suggestion.project, in: todos) {
            suggestion.lane = lane
            suggestion.laneReason = suggestion.project.isEmpty
                ? "最近任务多在此领域"
                : "\(suggestion.project) 的任务多在此领域"
        }

        return suggestion
    }

    /// Projects the user actually files todos under, most recently used first.
    /// Recency beats volume here: the project of this week's work is the better
    /// guess even when an older project has more todos.
    private static func knownProjects(in todos: [ATMTodo]) -> [String] {
        var latest: [String: String] = [:]
        for todo in todos {
            guard let project = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !project.isEmpty else { continue }
            if let seen = latest[project], seen >= todo.created { continue }
            latest[project] = todo.created
        }
        return latest.keys.sorted {
            if latest[$0] != latest[$1] { return latest[$0, default: ""] > latest[$1, default: ""] }
            return $0 < $1
        }
    }

    /// Session projects and todo projects are not always spelled the same
    /// ("mox-atm" against "atm"), so fall back to a containment match before
    /// recommending a project name the user has never filed a todo under.
    private static func matchKnown(_ project: String, in known: [String]) -> String? {
        let value = project.lowercased()
        if let exact = known.first(where: { $0.lowercased() == value }) { return exact }
        return known.first { value.contains($0.lowercased()) || $0.lowercased().contains(value) }
    }

    /// A standalone "P0" is the user stating the priority; the word boundaries keep
    /// strings such as "top10" from being read as one.
    private static func explicitPriority(in haystack: String) -> String? {
        guard let range = haystack.range(of: "\\bp[012]\\b", options: .regularExpression) else { return nil }
        return haystack[range].uppercased()
    }

    private static func dominantLane(of project: String, in todos: [ATMTodo]) -> String? {
        let scoped = todos.filter { todo in
            guard let lane = todo.lane, !lane.isEmpty else { return false }
            guard !project.isEmpty else { return true }
            return todo.project == project
        }
        guard !scoped.isEmpty else { return nil }
        let counts = Dictionary(grouping: scoped, by: { $0.lane ?? "" }).mapValues(\.count)
        return counts.keys.sorted {
            if counts[$0] != counts[$1] { return counts[$0, default: 0] > counts[$1, default: 0] }
            return $0 < $1
        }.first
    }
}
