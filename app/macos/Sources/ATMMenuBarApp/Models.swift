import Foundation

struct ATMTodoLink: Decodable, Hashable {
    let url: String
    let kind: String?
    let title: String?
    let relation: String?
}

enum ATMTodoActivityKind: String, Hashable {
    case progress = "进展"
    case supplement = "补充"
}

enum ATMTodoRefineMetadata {
    /// `internal/refine.FormatAnalysis` writes this human-visible marker on the
    /// first line of every model refinement. Reading the persisted card rather
    /// than the current setting keeps the App honest after a provider change.
    private static let sourceMarker = " · from "

    static func source(from content: String) -> String? {
        for line in content.components(separatedBy: "\n").reversed() {
            guard let marker = line.range(of: sourceMarker) else { continue }
            var value = String(line[marker.upperBound...])
            if let reason = value.range(of: "：") {
                value = String(value[..<reason.lowerBound])
            }
            value = value.trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty { return value }
        }
        return nil
    }
}

struct ATMTodoProgressEntry: Identifiable, Hashable {
    let id: Int
    let timestamp: String
    let text: String
    let isDoneMarker: Bool
    let kind: ATMTodoActivityKind

    /// Written by `collectionSupplementMarker` in internal/collector/service.go.
    /// Kept as one constant on this side too so the pairing is visible from
    /// either file — see displayText.
    static let collectionSupplementMarker = "钉钉采集"

    var nextAction: String? {
        guard let marker = text.range(of: "下一步：") else { return nil }
        let suffix = text[marker.upperBound...]
        let value = suffix
            .split(whereSeparator: { $0 == "；" || $0 == "\n" })
            .first
            .map(String.init)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return value?.isEmpty == false ? value : nil
    }

    /// Parse the activity sections of a todo markdown doc into one timeline.
    /// `进展` records execution milestones, while `补充` records newly collected
    /// requirements or context. Both are task activity and differ only by type.
    /// Each entry is a top-level list item like `- [2026-07-14 11:32] 内容`,
    /// with any indented continuation lines folded into the same entry.
    static func parse(from content: String) -> [ATMTodoProgressEntry] {
        let lines = content.components(separatedBy: "\n")
        var currentKind: ATMTodoActivityKind?
        var rawEntries: [(kind: ATMTodoActivityKind, text: String)] = []
        for line in lines {
            if line.hasPrefix("## ") {
                let heading = String(line.dropFirst(3))
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                currentKind = ATMTodoActivityKind(rawValue: heading)
                continue
            }
            guard let currentKind else { continue }
            if line.hasPrefix("- ") {
                rawEntries.append((currentKind, String(line.dropFirst(2))))
            } else if !rawEntries.isEmpty {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if !trimmed.isEmpty {
                    rawEntries[rawEntries.count - 1].text += "\n" + trimmed
                }
            }
        }

        var entries: [ATMTodoProgressEntry] = []
        for (index, rawEntry) in rawEntries.enumerated() {
            var timestamp = ""
            var body = rawEntry.text
            if body.hasPrefix("["), let close = body.firstIndex(of: "]") {
                timestamp = String(body[body.index(after: body.startIndex)..<close])
                body = String(body[body.index(after: close)...]).trimmingCharacters(in: .whitespaces)
            }
            let isDone = body.hasPrefix("[done]")
            if isDone { body = String(body.dropFirst("[done]".count)).trimmingCharacters(in: .whitespaces) }
            body = displayText(for: body, kind: rawEntry.kind)
            entries.append(ATMTodoProgressEntry(
                id: index,
                timestamp: timestamp,
                text: body,
                isDoneMarker: isDone,
                kind: rawEntry.kind
            ))
        }
        // Sections live in different places in the Markdown document, so their
        // file order is not a timeline. Log timestamps use an ISO-like format
        // whose lexical order is chronological; keep document order as a stable
        // tie-breaker and place legacy timestamp-less entries first.
        return entries.sorted {
            if $0.timestamp == $1.timestamp { return $0.id < $1.id }
            if $0.timestamp.isEmpty { return true }
            if $1.timestamp.isEmpty { return false }
            return $0.timestamp < $1.timestamp
        }
    }

    /// Older collector builds copied their entire connector context into the Todo
    /// supplement. Keep that audit data in collection history, but present only
    /// the extracted action in the task timeline. New entries carry the same
    /// idempotency marker in an HTML comment and take this path as well.
    ///
    /// The literal is `collectionSupplementMarker` in
    /// internal/collector/service.go, which is what writes it. Change one side and
    /// the marker stops being stripped here — it does not vary with the connector
    /// for exactly that reason.
    private static func displayText(for body: String, kind: ATMTodoActivityKind) -> String {
        guard kind == .supplement else { return body }
        var text = body
        let visibleMarkerPrefix = "[\(collectionSupplementMarker):"
        let hiddenMarkerPrefix = "<!-- [\(collectionSupplementMarker):"
        let isCollected = text.hasPrefix(visibleMarkerPrefix) || text.contains(hiddenMarkerPrefix)
        guard isCollected else { return body }

        if text.hasPrefix(visibleMarkerPrefix), let markerEnd = text.firstIndex(of: "]") {
            text = String(text[text.index(after: markerEnd)...])
                .trimmingCharacters(in: .whitespacesAndNewlines)
        }
        for boundary in ["\n来源对话：", "\n来源对话:", "\n<!--"] {
            if let range = text.range(of: boundary) {
                text = String(text[..<range.lowerBound])
            }
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

struct ATMTodo: Decodable, Identifiable, Hashable {
    let id: String
    let title: String
    let description: String?
    let priority: String
    let status: String
    let project: String?
    let tags: [String]?
    let wakeCondition: String?
    let reviewAt: String?
    let maintenanceLimit: Int?
    let dependsOn: [String]?
    let links: [ATMTodoLink]?
    let created: String
    let source: String?
    /// Who filed the todo: "me", "collect", or an agent name. Nil on every todo
    /// created before the CLI had the field.
    let creator: String?
    let closed: String?
    let closedReason: String?
    let onDone: String?
    let startTS: Int64?
    let doneTS: Int64?

    enum CodingKeys: String, CodingKey {
        case id, title, description, priority, status, project, tags, links, created, source
        case closed, creator
        case closedReason = "closed_reason"
        case wakeCondition = "wake_condition"
        case reviewAt = "review_at"
        case maintenanceLimit = "maintenance_limit"
        case dependsOn = "depends_on"
        case onDone = "on_done"
        case startTS = "start_ts"
        case doneTS = "done_ts"
    }

    /// Closing a todo that is waiting in review is the human accepting what an
    /// agent submitted; closing any other todo is the human finishing it. Both go
    /// through `todo done`, so this is what tells them apart -- in the button label
    /// and, via the closing reason, in the record afterwards.
    var completionVerb: String { status == "review" ? "验收" : "完成" }

    func replacingLifecycle(
        status: String,
        wakeCondition: String? = nil,
        reviewAt: String? = nil
    ) -> ATMTodo {
        ATMTodo(
            copying: self,
            status: status,
            wakeCondition: wakeCondition,
            reviewAt: reviewAt
        )
    }

    private init(
        copying todo: ATMTodo,
        status: String,
        wakeCondition: String?,
        reviewAt: String?
    ) {
        id = todo.id
        title = todo.title
        description = todo.description
        priority = todo.priority
        self.status = status
        project = todo.project
        tags = todo.tags
        self.wakeCondition = wakeCondition
        self.reviewAt = reviewAt
        maintenanceLimit = todo.maintenanceLimit
        dependsOn = todo.dependsOn
        links = todo.links
        created = todo.created
        source = todo.source
        creator = todo.creator
        closed = todo.closed
        closedReason = todo.closedReason
        onDone = todo.onDone
        startTS = todo.startTS
        doneTS = todo.doneTS
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        title = try values.decode(String.self, forKey: .title)
        description = try values.decodeIfPresent(String.self, forKey: .description)
        priority = try values.decode(String.self, forKey: .priority)
        status = try values.decode(String.self, forKey: .status)
        let decodedTags = try values.decodeIfPresent([String].self, forKey: .tags) ?? []
        project = try values.decodeIfPresent(String.self, forKey: .project)
        tags = decodedTags.isEmpty ? nil : decodedTags.sorted()
        wakeCondition = try values.decodeIfPresent(String.self, forKey: .wakeCondition)
        reviewAt = try values.decodeIfPresent(String.self, forKey: .reviewAt)
        maintenanceLimit = try values.decodeIfPresent(Int.self, forKey: .maintenanceLimit)
        dependsOn = try values.decodeIfPresent([String].self, forKey: .dependsOn)
        links = try values.decodeIfPresent([ATMTodoLink].self, forKey: .links)
        created = try values.decode(String.self, forKey: .created)
        source = try values.decodeIfPresent(String.self, forKey: .source)
        creator = try values.decodeIfPresent(String.self, forKey: .creator)
        closed = try values.decodeIfPresent(String.self, forKey: .closed)
        closedReason = try values.decodeIfPresent(String.self, forKey: .closedReason)
        onDone = try values.decodeIfPresent(String.self, forKey: .onDone)
        startTS = try values.decodeIfPresent(Int64.self, forKey: .startTS)
        doneTS = try values.decodeIfPresent(Int64.self, forKey: .doneTS)
    }
}

/// Renders a todo's creator the way the CLI does, so the same record reads the
/// same in both places. Only "me" is localised, and only the display side knows
/// the nickname: the stored token stays "me" so renaming yourself never rewrites
/// a record. Returns nil for a todo filed before the field existed — there is
/// nothing to show and a placeholder would imply the creator was lost.
enum ATMTodoCreator {
    static func label(_ creator: String?, ownerName: String) -> String? {
        guard let creator = normalized(creator) else { return nil }
        switch creator {
        case "me":
            let name = ownerName.trimmingCharacters(in: .whitespacesAndNewlines)
            return name.isEmpty ? "我" : "\(name)（我）"
        case "collect":
            return "收集"
        default:
            return creator
        }
    }

    /// The list-row form. Same rendering as `label` except it drops the owner's
    /// nickname: it is identical on every row it appears on, so in a row it is
    /// width spent on nothing. The detail header still shows the full name.
    static func shortLabel(_ creator: String?) -> String? {
        label(creator, ownerName: "")
    }

    /// Icon per creator kind, so provenance reads at a glance instead of by
    /// reading the name. Deliberately borrowed from `ATMDesktopSection`: a todo
    /// filed by 收集 carries the same tray as the 收集 section, and one filed by
    /// an agent the same cpu as the Agent section, so the icon points at where
    /// the todo came from. Nil whenever `label` is nil — no creator, no icon.
    static func icon(_ creator: String?) -> String? {
        guard let creator = normalized(creator) else { return nil }
        switch creator {
        case "me": return "person.fill"
        case "collect": return "tray.and.arrow.down"
        default: return "cpu"
        }
    }

    private static func normalized(_ creator: String?) -> String? {
        guard let creator = creator?.trimmingCharacters(in: .whitespacesAndNewlines),
              !creator.isEmpty else { return nil }
        return creator
    }
}

struct ATMWorkSummary: Decodable, Equatable {
    let open: Int
    let inProgress: Int
    let waiting: Int
    let review: Int
    let blocked: Int
    let due: Int
    let maintenance: Int

    enum CodingKeys: String, CodingKey {
        case open, waiting, review, blocked, due, maintenance
        case inProgress = "in_progress"
    }

    static let empty = ATMWorkSummary(
        open: 0, inProgress: 0, waiting: 0,
        review: 0, blocked: 0, due: 0, maintenance: 0
    )

    var actionable: Int { review + blocked + due }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        open = try values.decodeIfPresent(Int.self, forKey: .open) ?? 0
        inProgress = try values.decodeIfPresent(Int.self, forKey: .inProgress) ?? 0
        waiting = try values.decodeIfPresent(Int.self, forKey: .waiting) ?? 0
        review = try values.decodeIfPresent(Int.self, forKey: .review) ?? 0
        blocked = try values.decodeIfPresent(Int.self, forKey: .blocked) ?? 0
        due = try values.decodeIfPresent(Int.self, forKey: .due) ?? 0
        maintenance = try values.decodeIfPresent(Int.self, forKey: .maintenance) ?? 0
    }

    init(open: Int, inProgress: Int, waiting: Int, review: Int, blocked: Int, due: Int, maintenance: Int) {
        self.open = open
        self.inProgress = inProgress
        self.waiting = waiting
        self.review = review
        self.blocked = blocked
        self.due = due
        self.maintenance = maintenance
    }
}

struct ATMNowSnapshot: Decodable {
    let generatedAt: String
    let open: [ATMTodo]
    let working: [ATMTodo]
    let waiting: [ATMTodo]
    let review: [ATMTodo]
    let blocked: [ATMTodo]
    let due: [ATMTodo]
    let summary: ATMWorkSummary

    enum CodingKeys: String, CodingKey {
        case open, working, waiting, review, blocked, due, summary
        case generatedAt = "generated_at"
    }

    static let empty = ATMNowSnapshot(
        generatedAt: "",
        open: [],
        working: [],
        waiting: [],
        review: [],
        blocked: [],
        due: [],
        summary: .empty
    )

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        generatedAt = try values.decodeIfPresent(String.self, forKey: .generatedAt) ?? ""
        working = try values.decodeIfPresent([ATMTodo].self, forKey: .working) ?? []
        open = try values.decodeIfPresent([ATMTodo].self, forKey: .open) ?? []
        waiting = try values.decodeIfPresent([ATMTodo].self, forKey: .waiting) ?? []
        review = try values.decodeIfPresent([ATMTodo].self, forKey: .review) ?? []
        blocked = try values.decodeIfPresent([ATMTodo].self, forKey: .blocked) ?? []
        due = try values.decodeIfPresent([ATMTodo].self, forKey: .due) ?? []
        summary = try values.decodeIfPresent(ATMWorkSummary.self, forKey: .summary) ?? .empty
    }

    init(
        generatedAt: String,
        open: [ATMTodo],
        working: [ATMTodo],
        waiting: [ATMTodo],
        review: [ATMTodo],
        blocked: [ATMTodo],
        due: [ATMTodo],
        summary: ATMWorkSummary
    ) {
        self.generatedAt = generatedAt
        self.open = open
        self.working = working
        self.waiting = waiting
        self.review = review
        self.blocked = blocked
        self.due = due
        self.summary = summary
    }

    var needsAction: [ATMTodo] { review + blocked + due }

    var allTodos: [ATMTodo] {
        var values: [String: ATMTodo] = [:]
        for todo in working + needsAction + waiting + open {
            values[todo.id] = todo
        }
        return values.values.sorted {
            if $0.priority != $1.priority { return $0.priority < $1.priority }
            return $0.id < $1.id
        }
    }

    func removingTodos(withIDs ids: Set<String>) -> ATMNowSnapshot {
        guard !ids.isEmpty else { return self }
        let removed = allTodos.filter { ids.contains($0.id) }
        let removedMaintenance = removed.filter {
            $0.tags?.contains("maintenance") == true
        }.count
        func remaining(_ count: Int, in values: [ATMTodo]) -> Int {
            max(0, count - values.filter { ids.contains($0.id) }.count)
        }

        return ATMNowSnapshot(
            generatedAt: generatedAt,
            open: open.filter { !ids.contains($0.id) },
            working: working.filter { !ids.contains($0.id) },
            waiting: waiting.filter { !ids.contains($0.id) },
            review: review.filter { !ids.contains($0.id) },
            blocked: blocked.filter { !ids.contains($0.id) },
            due: due.filter { !ids.contains($0.id) },
            summary: ATMWorkSummary(
                open: remaining(summary.open, in: open),
                inProgress: remaining(summary.inProgress, in: working),
                waiting: remaining(summary.waiting, in: waiting),
                review: remaining(summary.review, in: review),
                blocked: remaining(summary.blocked, in: blocked),
                due: remaining(summary.due, in: due),
                maintenance: max(0, summary.maintenance - removedMaintenance)
            )
        )
    }

    func replacingTodo(_ todo: ATMTodo) -> ATMNowSnapshot {
        let withoutOldValue = removingTodos(withIDs: [todo.id])
        guard todo.status != "done", todo.status != "dropped" else {
            return withoutOldValue
        }

        var open = withoutOldValue.open
        var working = withoutOldValue.working
        var waiting = withoutOldValue.waiting
        var review = withoutOldValue.review
        var blocked = withoutOldValue.blocked
        var summary = withoutOldValue.summary
        let maintenanceIncrement = todo.tags?.contains("maintenance") == true ? 1 : 0

        switch todo.status {
        case "in_progress":
            working.append(todo)
            summary = ATMWorkSummary(
                open: summary.open,
                inProgress: summary.inProgress + 1,
                waiting: summary.waiting,
                review: summary.review,
                blocked: summary.blocked,
                due: summary.due,
                maintenance: summary.maintenance + maintenanceIncrement
            )
        case "waiting":
            waiting.append(todo)
            summary = ATMWorkSummary(
                open: summary.open,
                inProgress: summary.inProgress,
                waiting: summary.waiting + 1,
                review: summary.review,
                blocked: summary.blocked,
                due: summary.due,
                maintenance: summary.maintenance + maintenanceIncrement
            )
        case "review":
            review.append(todo)
            summary = ATMWorkSummary(
                open: summary.open,
                inProgress: summary.inProgress,
                waiting: summary.waiting,
                review: summary.review + 1,
                blocked: summary.blocked,
                due: summary.due,
                maintenance: summary.maintenance + maintenanceIncrement
            )
        case "blocked":
            blocked.append(todo)
            summary = ATMWorkSummary(
                open: summary.open,
                inProgress: summary.inProgress,
                waiting: summary.waiting,
                review: summary.review,
                blocked: summary.blocked + 1,
                due: summary.due,
                maintenance: summary.maintenance + maintenanceIncrement
            )
        default:
            open.append(todo)
            summary = ATMWorkSummary(
                open: summary.open + 1,
                inProgress: summary.inProgress,
                waiting: summary.waiting,
                review: summary.review,
                blocked: summary.blocked,
                due: summary.due,
                maintenance: summary.maintenance + maintenanceIncrement
            )
        }

        return ATMNowSnapshot(
            generatedAt: generatedAt,
            open: open,
            working: working,
            waiting: waiting,
            review: review,
            blocked: blocked,
            due: withoutOldValue.due,
            summary: summary
        )
    }
}

enum ATMKnowledgeLibrary {
    static let memoryID = "__memory__"
    /// UI-only aggregate that keeps archived documents in one predictable place
    /// without changing their real collection metadata.
    static let archiveID = "__archive__"
}

struct ATMKnowledgeCollection: Decodable, Identifiable, Equatable {
    let id: String
    let name: String
    let role: String?
    let description: String
    let topics: [String]
    let useWhen: [String]
    let avoidWhen: [String]
    let instructions: [String]
    let documentCount: Int

    enum CodingKeys: String, CodingKey {
        case id, name, role, description, topics, instructions
        case useWhen = "use_when"
        case avoidWhen = "avoid_when"
        case documentCount = "document_count"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        name = try values.decode(String.self, forKey: .name)
        role = try values.decodeIfPresent(String.self, forKey: .role)
        description = try values.decodeIfPresent(String.self, forKey: .description) ?? ""
        topics = try values.decodeIfPresent([String].self, forKey: .topics) ?? []
        useWhen = try values.decodeIfPresent([String].self, forKey: .useWhen) ?? []
        avoidWhen = try values.decodeIfPresent([String].self, forKey: .avoidWhen) ?? []
        instructions = try values.decodeIfPresent([String].self, forKey: .instructions) ?? []
        documentCount = try values.decodeIfPresent(Int.self, forKey: .documentCount) ?? 0
    }
}

struct ATMKnowledgeDocumentSummary: Decodable, Identifiable, Equatable {
    let documentID: String
    let title: String
    let collection: String
    let status: String?
    let domains: [String]
    let tags: [String]
    let projects: [String]
    let producer: String?
    let createdAt: String?
    let updatedAt: String?
    let snippet: String?
    let score: Double?

    var id: String { documentID }

    enum CodingKeys: String, CodingKey {
        case title, collection, status, domains, tags, projects, producer, snippet, score
        case documentID = "document_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        documentID = try values.decode(String.self, forKey: .documentID)
        title = try values.decode(String.self, forKey: .title)
        collection = try values.decode(String.self, forKey: .collection)
        status = try values.decodeIfPresent(String.self, forKey: .status)
        domains = try values.decodeIfPresent([String].self, forKey: .domains) ?? []
        tags = try values.decodeIfPresent([String].self, forKey: .tags) ?? []
        projects = try values.decodeIfPresent([String].self, forKey: .projects) ?? []
        producer = try values.decodeIfPresent(String.self, forKey: .producer)
        createdAt = try values.decodeIfPresent(String.self, forKey: .createdAt)
        updatedAt = try values.decodeIfPresent(String.self, forKey: .updatedAt)
        snippet = try values.decodeIfPresent(String.self, forKey: .snippet)
        score = try values.decodeIfPresent(Double.self, forKey: .score)
    }
}

struct ATMKnowledgeDocumentMetadata: Decodable, Equatable {
    let id: String
    let schemaVersion: Int
    let title: String
    let status: String
    let domains: [String]
    let tags: [String]
    let projects: [String]
    let producer: String
    let createdAt: String
    let updatedAt: String
    let source: ATMKnowledgeSourceInfo?

    enum CodingKeys: String, CodingKey {
        case id, schemaVersion, title, status, domains, tags, projects, producer, createdAt, updatedAt, source
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        title = try values.decode(String.self, forKey: .title)
        status = try values.decode(String.self, forKey: .status)
        domains = try values.decodeIfPresent([String].self, forKey: .domains) ?? []
        tags = try values.decodeIfPresent([String].self, forKey: .tags) ?? []
        projects = try values.decodeIfPresent([String].self, forKey: .projects) ?? []
        producer = try values.decode(String.self, forKey: .producer)
        createdAt = try values.decode(String.self, forKey: .createdAt)
        updatedAt = try values.decode(String.self, forKey: .updatedAt)
        source = try values.decodeIfPresent(ATMKnowledgeSourceInfo.self, forKey: .source)
    }
}

struct ATMKnowledgeSourceInfo: Decodable, Equatable {
    let type: String
    let uri: String
    let hash: String?
    let importedAt: String?

    enum CodingKeys: String, CodingKey {
        case type, uri, hash, importedAt
    }
}

struct ATMKnowledgeDocument: Decodable, Equatable {
    let metadata: ATMKnowledgeDocumentMetadata
    let collection: String
    let content: String
}

struct ATMKnowledgeDraft: Equatable {
    let title: String
    let collection: String
    let domains: [String]
    let tags: [String]
    let projects: [String]
    let content: String
}

struct ATMKnowledgeMetadataEdit: Equatable {
    let title: String
    let collection: String
    let status: String
    let domains: [String]
    let tags: [String]
    let projects: [String]
}

struct ATMKnowledgeAuditIssue: Decodable, Identifiable, Equatable {
    let code: String
    let severity: String
    let documentIDs: [String]
    let collection: String?
    let title: String?
    let detail: String
    let suggestedAction: String

    var id: String { "\(code):\(documentIDs.joined(separator: ","))" }

    enum CodingKeys: String, CodingKey {
        case code, severity, collection, title, detail
        case documentIDs = "document_ids"
        case suggestedAction = "suggested_action"
    }
}

struct ATMKnowledgeAuditReport: Decodable, Equatable {
    let generatedAt: String
    let staleDays: Int
    let documents: Int
    let active: Int
    let issues: [ATMKnowledgeAuditIssue]
    let counts: [String: Int]

    enum CodingKeys: String, CodingKey {
        case documents, active, issues, counts
        case generatedAt = "generated_at"
        case staleDays = "stale_days"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        generatedAt = try container.decode(String.self, forKey: .generatedAt)
        staleDays = try container.decode(Int.self, forKey: .staleDays)
        documents = try container.decode(Int.self, forKey: .documents)
        active = try container.decode(Int.self, forKey: .active)
        issues = try container.decodeIfPresent([ATMKnowledgeAuditIssue].self, forKey: .issues) ?? []
        counts = try container.decodeIfPresent([String: Int].self, forKey: .counts) ?? [:]
    }
}

struct ATMKnowledgeQuality: Decodable, Identifiable, Equatable {
    let documentID: String
    let title: String
    let collection: String
    let retrievals: Int
    let adopted: Int
    let corrected: Int
    let rejected: Int
    let score: Double

    var id: String { documentID }

    enum CodingKeys: String, CodingKey {
        case title, collection, retrievals, adopted, corrected, rejected, score
        case documentID = "document_id"
    }
}

struct ATMDoctorReport: Decodable, Equatable {
    let sources: [ATMDoctorSource]
    let issues: [ATMDoctorIssue]
}

struct ATMDoctorSource: Decodable, Identifiable, Equatable {
    let agent: String
    let path: String
    let exists: Bool
    let files: Int
    let indexedSessions: Int
    let status: String

    var id: String { agent }

    enum CodingKeys: String, CodingKey {
        case agent, path, exists, files, status
        case indexedSessions = "indexed_sessions"
    }
}

struct ATMDoctorIssue: Decodable, Identifiable, Equatable {
    let severity: String
    let domain: String
    let code: String
    let subject: String
    let detail: String
    let suggestion: String

    var id: String { "\(domain):\(code):\(subject)" }
}

struct ATMIndexHealthReport: Decodable, Equatable {
    let generatedAt: String
    let index: ATMIndexHealth
    let sync: ATMIndexSyncHealth

    enum CodingKeys: String, CodingKey {
        case index, sync
        case generatedAt = "generated_at"
    }
}

struct ATMIndexHealth: Decodable, Equatable {
    let path: String
    let exists: Bool
    let schemaVersion: Int
    let indexedSessions: Int

    enum CodingKeys: String, CodingKey {
        case path, exists
        case schemaVersion = "schema_version"
        case indexedSessions = "indexed_sessions"
    }
}

struct ATMIndexSyncHealth: Decodable, Equatable {
    let scope: String
    let status: String
    let runStatus: String
    let lastAttemptAt: String?
    let lastSuccessAt: String?
    let ageSeconds: Int64?
    let staleAfterSeconds: Int64
    let lastError: String
    let lastSyncedFiles: Int

    enum CodingKeys: String, CodingKey {
        case scope, status
        case runStatus = "run_status"
        case lastAttemptAt = "last_attempt_at"
        case lastSuccessAt = "last_success_at"
        case ageSeconds = "age_seconds"
        case staleAfterSeconds = "stale_after_seconds"
        case lastError = "last_error"
        case lastSyncedFiles = "last_synced_files"
    }
}

struct ATMMemoryHit: Decodable, Identifiable, Equatable {
    let id: String
    let scope: String
    let content: String
    let tags: [String]
    let createdAt: String
    let score: Double
    let source: String
    let metadata: [String: String]

    enum CodingKeys: String, CodingKey {
        case id, scope, content, tags, score, source, metadata
        case createdAt = "created_at"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        scope = try values.decode(String.self, forKey: .scope)
        content = try values.decode(String.self, forKey: .content)
        tags = try values.decodeIfPresent([String].self, forKey: .tags) ?? []
        createdAt = try values.decode(String.self, forKey: .createdAt)
        score = try values.decodeIfPresent(Double.self, forKey: .score) ?? 0
        source = try values.decodeIfPresent(String.self, forKey: .source) ?? "memory"
        metadata = try values.decodeIfPresent([String: String].self, forKey: .metadata) ?? [:]
    }
}

/// One page of session search plus the size of the set it was cut from.
///
/// The CLI bounds `session search` so a broad keyword cannot return every match
/// at once. Decoding the page alone would leave the app unable to tell a
/// complete result from the first slice of a much larger one.
struct ATMSessionSearchResult: Decodable, Equatable {
    let total: Int
    let returned: Int
    let truncated: Bool
    let matches: [ATMSessionSearchHit]

    enum CodingKeys: String, CodingKey {
        case total, returned, truncated, matches
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        matches = try container.decodeIfPresent([ATMSessionSearchHit].self, forKey: .matches) ?? []
        total = try container.decodeIfPresent(Int.self, forKey: .total) ?? matches.count
        returned = try container.decodeIfPresent(Int.self, forKey: .returned) ?? matches.count
        truncated = try container.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
    }
}

struct ATMSessionSearchHit: Decodable, Identifiable, Equatable {
    let shortID: String
    let agent: String
    let project: String
    let createdAt: String
    let role: String
    let content: String

    var id: String { shortID }

    enum CodingKeys: String, CodingKey {
        case agent, project, role, content
        case shortID = "short_id"
        case createdAt = "created_at"
    }
}
