import Foundation

struct ATMTodoLink: Decodable, Hashable {
    let url: String
    let kind: String?
    let title: String?
    let relation: String?
}

// MARK: - Automatic collection

struct ATMCollectionSummary: Decodable, Equatable {
    let sources: Int
    let enabledSources: Int
    let fetchedToday: Int
    let createdToday: Int
    let appendedToday: Int
    let insightToday: Int
    let ignoredToday: Int
    let failedToday: Int
    /// 自动重试已用尽、正在等人处理的记录数。按整个台账统计，不限今天：值为 0 时界面
    /// 什么都不说，非 0 才是唯一需要人看一眼的失败信号。旧版 CLI 不返回，按 0 读。
    let retryStopped: Int?

    enum CodingKeys: String, CodingKey {
        case sources
        case enabledSources = "enabled_sources"
        case fetchedToday = "fetched_today"
        case createdToday = "created_today"
        case appendedToday = "appended_today"
        case insightToday = "insight_today"
        case ignoredToday = "ignored_today"
        case failedToday = "failed_today"
        case retryStopped = "retry_stopped"
    }

    static let empty = ATMCollectionSummary(
        sources: 0, enabledSources: 0, fetchedToday: 0, createdToday: 0,
        appendedToday: 0, insightToday: 0, ignoredToday: 0, failedToday: 0,
        retryStopped: 0
    )
}

struct ATMCollectionSource: Decodable, Identifiable, Equatable {
    let id: String
    let connector: String
    let kind: String
    let externalID: String
    let name: String?
    let project: String?
    let excludePattern: String?
    let instruction: String?
    let knowledgeCollection: String?
    let strategy: String?
    let decisionUnit: String?
    let intervalMinutes: Int?
    let priority: String
    let enabled: Bool
    let createdAt: Int64
    let updatedAt: Int64

    enum CodingKeys: String, CodingKey {
        case id, connector, kind, name, project, priority, enabled, strategy, instruction
        case externalID = "external_id"
        case decisionUnit = "decision_unit"
        case excludePattern = "exclude_pattern"
        case knowledgeCollection = "knowledge_collection"
        case intervalMinutes = "interval_minutes"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var displayName: String {
        let trimmed = (name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? externalID : trimmed
    }

    var effectiveStrategy: String { strategy == "observe" ? "observe" : "tasks" }

    /// Older databases and hand-written fixtures predate the column, and window
    /// is what they behaved as.
    var effectiveDecisionUnit: String { decisionUnit == "message" ? "message" : "window" }

    var effectiveIntervalMinutes: Int {
        intervalMinutes ?? (effectiveStrategy == "observe" ? 60 : 5)
    }

    var symbolName: String { collectionKindSymbol(kind) }
}

/// Glyph for a connector-defined source kind. Kinds belong to the connector, so
/// this recognizes the few shapes that read differently at a glance — a robot
/// feed is not a person you talk to — and leaves everything else as one contact.
func collectionKindSymbol(_ kind: String?) -> String {
    switch kind {
    case "group", "channel": return "person.3.fill"
    case "bot": return "cpu"
    default: return "person.fill"
    }
}

/// Readable name for a connector-defined source kind, matching the CLI's own
/// vocabulary (`collectionKindLabel` in internal/cmd/collect.go). A kind ATM has
/// never seen prints as itself: kinds belong to the connector, so an unknown one
/// is a label to show, not an error.
func collectionKindLabel(_ kind: String?) -> String {
    switch kind {
    case "group", "channel": return "群聊"
    case "user", "contact", "open_dingtalk_id": return "联系人"
    case "bot": return "机器人"
    case .none, "", "all": return "来源"
    case .some(let kind): return kind
    }
}

/// How `atm collect source search` is narrowed. This is a discovery filter, not
/// the kind that gets persisted — the kind ATM saves always comes back on the
/// chosen candidate, so picking 群聊 here never decides what a source is stored as.
enum ATMCollectionSearchKind: String, CaseIterable, Identifiable {
    case all
    case group
    case user
    case bot

    var id: String { rawValue }

    var label: String {
        switch self {
        case .all: return "全部"
        case .group: return "群聊"
        case .user: return "联系人"
        case .bot: return "机器人"
        }
    }

    var systemImage: String {
        switch self {
        case .all: return "square.grid.2x2"
        case .group: return "person.3.fill"
        case .user: return "person.fill"
        case .bot: return "cpu"
        }
    }
}

/// One `atm collect source search` result, carrying the identifier the connector
/// needs plus enough detail to tell two same-named results apart.
struct ATMCollectionCandidate: Decodable, Identifiable, Equatable {
    let kind: String
    let externalID: String
    let name: String
    let detail: String?

    enum CodingKeys: String, CodingKey {
        case kind, name, detail
        case externalID = "external_id"
    }

    var id: String { "\(kind)/\(externalID)" }

    var isGroup: Bool { kind == "group" }

    var symbolName: String { collectionKindSymbol(kind) }
}

struct ATMCollectionCandidateList: Decodable, Equatable {
    let candidates: [ATMCollectionCandidate]
}

/// One message from `atm collect history`. Read-only: nothing about a history
/// view is stored, so these never appear in the collection overview.
struct ATMCollectionMessage: Decodable, Identifiable, Equatable {
    let id: String
    let sender: String?
    let createdAt: Int64?
    let content: String

    enum CodingKeys: String, CodingKey {
        case id, sender, content
        case createdAt = "created_at"
    }

    var time: String {
        guard let createdAt else { return "" }
        let formatter = DateFormatter()
        formatter.dateFormat = "MM-dd HH:mm"
        return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(createdAt)))
    }
}

/// Identity of the conversation a history read came from. `atm collect history`
/// works on groups that were never added as sources, so this carries no
/// configuration — only enough to label the view.
struct ATMCollectionHistorySource: Decodable, Equatable {
    let id: String?
    let kind: String
    let externalID: String
    let name: String?

    enum CodingKeys: String, CodingKey {
        case id, kind, name
        case externalID = "external_id"
    }

    var displayName: String {
        let trimmed = (name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? externalID : trimmed
    }
}

struct ATMCollectionHistory: Decodable, Equatable {
    let source: ATMCollectionHistorySource?
    let messages: [ATMCollectionMessage]
    /// How many of these messages were new to the local archive.
    let synced: Int?
    /// True when the connector could not be reached and these messages came off disk. The
    /// difference between "nothing new" and "could not check" matters enough to
    /// show, so the caller must be able to see it.
    let stale: Bool?
    let error: String?
}

/// What a collection source points at. Candidate discovery is connector-owned;
/// ATM persists only the resolved kind and external identifier.
enum ATMCollectionSourceTarget: Equatable {
    case identifier(kind: String, externalID: String)

    static func candidate(_ candidate: ATMCollectionCandidate) -> ATMCollectionSourceTarget {
        .identifier(kind: candidate.kind, externalID: candidate.externalID)
    }

    var arguments: [String] {
        switch self {
        case .identifier(let kind, _): return ["--kind", kind, "--id", value]
        }
    }

    /// The trimmed identifier or search text this target carries.
    var value: String {
        switch self {
        case .identifier(_, let externalID): return externalID.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    var kind: String {
        switch self {
        case .identifier(let kind, _): return kind.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }
}

/// The identity half of the add/edit source sheet: which connector, and which
/// conversation on it. Kept out of the view so the two rules that decide whether
/// a save is even possible — what target gets persisted, and what is still
/// missing — are testable, and so the sheet has one source of truth instead of
/// four `@State` fields it has to keep consistent by hand.
struct ATMCollectionSourceIdentity: Equatable {
    var connector = ""
    /// Set when editing: `collect source add` upserts on connector + kind + id,
    /// so an existing source's target is what makes the save an edit rather than
    /// a second source. Never editable.
    var locked: ATMCollectionSourceTarget?
    /// The connector-resolved candidate. Preferred over the typed identifier
    /// because it carries the kind the connector itself uses for this source.
    var selection: ATMCollectionCandidate?
    /// True when the person gave up on search and is pasting an identifier.
    var manualEntry = false
    var manualKind = "group"
    var externalID = ""

    var isEditing: Bool { locked != nil }

    var trimmedConnector: String {
        connector.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }

    /// What would be saved, or nil while the sheet still has nothing to point at.
    var target: ATMCollectionSourceTarget? {
        if let locked { return locked }
        if !manualEntry, let selection { return .candidate(selection) }
        let target = ATMCollectionSourceTarget.identifier(kind: manualKind, externalID: externalID)
        guard !target.kind.isEmpty, !target.value.isEmpty else { return nil }
        return target
    }

    /// Why the save button is off, phrased as the next thing to do. Nil means
    /// ready — a disabled button with no reason is the thing this replaces.
    var blockReason: String? {
        if trimmedConnector.isEmpty { return "请先选择连接器" }
        guard target != nil else {
            return manualEntry ? "请填写来源类型和来源 ID" : "请搜索并选择一个来源"
        }
        return nil
    }
}

struct ATMCollectionRun: Decodable, Identifiable, Equatable {
    let id: String
    let connector: String
    let sourceID: String?
    let status: String
    let startedAt: Int64
    let finishedAt: Int64?
    let fetchedCount: Int
    let analyzedCount: Int
    let createdCount: Int
    let appendedCount: Int
    let insightCount: Int
    let ignoredCount: Int
    let failedCount: Int
    let error: String?

    enum CodingKeys: String, CodingKey {
        case id, connector, status, error
        case sourceID = "source_id"
        case startedAt = "started_at"
        case finishedAt = "finished_at"
        case fetchedCount = "fetched_count"
        case analyzedCount = "analyzed_count"
        case createdCount = "created_count"
        case appendedCount = "appended_count"
        case insightCount = "insight_count"
        case ignoredCount = "ignored_count"
        case failedCount = "failed_count"
    }
}

/// One day's knowledge document for one source, produced by `atm collect digest`
/// from that day's insight items. Rewritten in place as more insights arrive, so
/// there is at most one of these per source per day.
struct ATMCollectionDigest: Decodable, Identifiable, Equatable {
    let sourceID: String
    let digestDate: String
    let documentID: String
    let collection: String?
    let title: String?
    let itemCount: Int
    let updatedAt: Int64

    enum CodingKeys: String, CodingKey {
        case collection, title
        case sourceID = "source_id"
        case digestDate = "digest_date"
        case documentID = "document_id"
        case itemCount = "item_count"
        case updatedAt = "updated_at"
    }

    var id: String { "\(sourceID)/\(digestDate)" }
}

struct ATMCollectionConnectorHealth: Decodable, Equatable {
    let connector: String
    let status: String
    let error: String?
    let checkedAt: Int64?

    enum CodingKeys: String, CodingKey {
        case connector, status, error
        case checkedAt = "checked_at"
    }

    /// 状态词。设置页和「添加来源」都读这一份，同一个连接器不会在两处叫两个名字。
    var statusLabel: String {
        switch status {
        case "ready": return "可用"
        case "auth_required": return "需要登录"
        case "permission_required": return "缺少消息权限/权益"
        case "error": return "连接异常"
        default: return "尚未检测"
        }
    }

    var statusIcon: String {
        switch status {
        case "ready": return "checkmark.circle.fill"
        case "auth_required": return "person.crop.circle.badge.exclamationmark"
        case "permission_required": return "lock.trianglebadge.exclamationmark"
        case "error": return "exclamationmark.triangle.fill"
        default: return "questionmark.circle"
        }
    }

    /// True while nothing has run yet — the status says nothing about the
    /// connector either way, so callers can offer a first run instead of a verdict.
    var isUnverified: Bool { checkedAt == nil || status == "not_checked" }
}

struct ATMCollectionItem: Decodable, Identifiable, Equatable {
    let id: String
    let sourceID: String
    let connector: String
    let conversationID: String?
    let fingerprint: String
    let messageIDs: [String]
    let sender: String?
    let occurredAt: Int64?
    let rawContext: String?
    let action: String
    let title: String?
    let summary: String?
    let itemType: String?
    let project: String?
    let priority: String?
    let reason: String?
    let confidence: Double?
    let todoID: String?
    let status: String
    /// How many times this batch has been tried. Absent from older CLI output,
    /// which is read as zero: a fresh budget is the safe reading, because it
    /// describes an item the next run will pick up rather than one already
    /// retired.
    let attempts: Int?
    /// Whether the automatic retry has stopped. Derived by the CLI so the
    /// attempt ceiling lives in one place instead of being restated here.
    let retryStopped: Bool?
    let error: String?
    let createdAt: Int64
    let updatedAt: Int64
    /// The linked Todo's state as of this read. The CLI derives it from the Todo
    /// itself on every query, so it stays true no matter who closed the Todo.
    let todoStatus: String?
    let todoArchived: Bool?

    enum CodingKeys: String, CodingKey {
        case id, connector, fingerprint, sender, action, title, summary, project,
             priority, reason, confidence, status, attempts, error
        case sourceID = "source_id"
        case conversationID = "conversation_id"
        case messageIDs = "message_ids"
        case occurredAt = "occurred_at"
        case rawContext = "raw_context"
        case itemType = "item_type"
        case todoID = "todo_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case todoStatus = "todo_status"
        case todoArchived = "todo_archived"
        case retryStopped = "retry_stopped"
    }
}

enum ATMCollectionItemType: String, CaseIterable, Identifiable {
    case requirement
    case bug
    case investigation
    case followUp = "follow_up"
    case conversation
    case unknown

    var id: String { rawValue }

    static func resolve(_ rawValue: String?) -> ATMCollectionItemType {
        guard let rawValue else { return .unknown }
        return ATMCollectionItemType(rawValue: rawValue) ?? .unknown
    }

    var title: String {
        switch self {
        case .requirement: return "需求"
        case .bug: return "缺陷"
        case .investigation: return "排查"
        case .followUp: return "跟进"
        case .conversation: return "讨论"
        case .unknown: return "未分类"
        }
    }

    var explanation: String {
        switch self {
        case .requirement: return "明确提出要新增、调整或优化一项能力。"
        case .bug: return "现有行为不符合预期，需要定位并修复。"
        case .investigation: return "需要先查明原因、验证现状或评估方案。"
        case .followUp: return "已有事项的后续动作、补充信息或待确认结果。"
        case .conversation: return "背景交流、动态或尚不足以形成任务的讨论。"
        case .unknown: return "当前记录没有足够信息形成稳定分类。"
        }
    }

    var systemImage: String {
        switch self {
        case .requirement: return "sparkles"
        case .bug: return "ladybug"
        case .investigation: return "magnifyingglass"
        case .followUp: return "arrow.turn.down.right"
        case .conversation: return "bubble.left.and.bubble.right"
        case .unknown: return "questionmark.circle"
        }
    }
}

extension ATMCollectionItem {
    /// True once the Todo this record filed has been finished or dropped. The
    /// request that came in from the source has been answered, so the record is
    /// done too — whoever closed the Todo, and whenever.
    var todoClosed: Bool {
        todoStatus == "done" || todoStatus == "dropped"
    }

    /// The main list is what you glance at, and that means work: things ATM filed
    /// or wants filed. An insight is deliberately not work — its readable form is
    /// the day's digest in the knowledge base — so it collapses alongside noise
    /// rather than competing with Todos for attention. A record whose Todo is
    /// already closed is no longer work either: keeping it up here turned the
    /// workspace into a history feed, where twelve of twenty rows wanted nothing.
    var shouldCollapseInCollection: Bool {
        action == "ignore" || action == "insight" || todoClosed
    }
}

struct ATMCollectionOverview: Decodable, Equatable {
    let enabled: Bool
    let intervalMinutes: Int
    let lookbackMinutes: Int
    let modelCommand: String
    let connectorHealth: [ATMCollectionConnectorHealth]
    let summary: ATMCollectionSummary
    let sources: [ATMCollectionSource]
    let runs: [ATMCollectionRun]
    let items: [ATMCollectionItem]
    let digests: [ATMCollectionDigest]

    enum CodingKeys: String, CodingKey {
        case enabled, summary, sources, runs, items, digests
        case intervalMinutes = "interval_minutes"
        case lookbackMinutes = "lookback_minutes"
        case modelCommand = "model_command"
        case connectorHealth = "connector_health"
    }

    static let empty = ATMCollectionOverview(
        enabled: false, intervalMinutes: 5, lookbackMinutes: 60,
        modelCommand: "codex", connectorHealth: [], summary: .empty,
        sources: [], runs: [], items: [], digests: []
    )

    var latestRun: ATMCollectionRun? { runs.max { $0.startedAt < $1.startedAt } }
    var latestSuccessfulRun: ATMCollectionRun? {
        runs.filter { $0.status == "succeeded" }.max { $0.startedAt < $1.startedAt }
    }
}

enum ATMTodoActivityKind: String, Hashable {
    case progress = "进展"
    case supplement = "补充"
}

struct ATMTodoProgressEntry: Identifiable, Hashable {
    let id: Int
    let timestamp: String
    let text: String
    let isDoneMarker: Bool
    let kind: ATMTodoActivityKind

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
    private static func displayText(for body: String, kind: ATMTodoActivityKind) -> String {
        guard kind == .supplement else { return body }
        var text = body
        let visibleMarkerPrefix = "[钉钉采集:"
        let hiddenMarkerPrefix = "<!-- [钉钉采集:"
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
    let lane: String?
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
    let featurePath: String?
    let onDone: String?
    let startTS: Int64?
    let doneTS: Int64?

    enum CodingKeys: String, CodingKey {
        case id, title, description, priority, status, project, lane, tags, links, created, source
        case closed, featurePath, creator
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
        lane = todo.lane
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
        featurePath = todo.featurePath
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
        lane = try values.decodeIfPresent(String.self, forKey: .lane)
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
        featurePath = try values.decodeIfPresent(String.self, forKey: .featurePath)
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
        guard let creator = creator?.trimmingCharacters(in: .whitespacesAndNewlines),
              !creator.isEmpty else { return nil }
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

/// A named reporting window. Raw values are the dashboard's range keys, so the
/// snapshot is looked up by the same name the CLI computed it under.
///
/// These are calendar periods, not trailing day counts. "Last 30 days" cannot
/// answer "how much this month", which is the figure a bill is read against, and
/// "today" is close to empty every morning — exactly when the day before is the
/// thing worth looking at. The window's actual first and last day arrive with the
/// data (see ATMRangeData.startDate) rather than being recomputed here, so the app
/// and the CLI cannot disagree about where a month begins.
enum ATMMetricsRange: String, CaseIterable, Identifiable {
    case today
    case yesterday
    case thisWeek = "this_week"
    case lastWeek = "last_week"
    case thisMonth = "this_month"
    case last7Days = "last_7_days"
    case last30Days = "last_30_days"

    var id: String { rawValue }

    /// The three windows the menu bar panel offers. Its segmented control fits
    /// three labels, not seven, and a fixed slot is the wrong home for "this week"
    /// — that window holds only a few hours every Monday morning, so the rolling
    /// week goes there instead.
    static let compact: [ATMMetricsRange] = [.today, .last7Days, .last30Days]

    /// Which of the two named groups this window belongs to. Six flat options do
    /// not fit the picker's width, and grouping them keeps the page a summary
    /// rather than turning it into a filter panel.
    enum Group: String, CaseIterable, Identifiable {
        case day
        case period

        var id: String { rawValue }

        var title: String {
            switch self {
            case .day: return "日"
            case .period: return "周期"
            }
        }
    }

    var group: Group {
        switch self {
        case .today, .yesterday: return .day
        case .thisWeek, .lastWeek, .thisMonth, .last7Days, .last30Days: return .period
        }
    }

    static func inGroup(_ group: Group) -> [ATMMetricsRange] {
        allCases.filter { $0.group == group }
    }

    /// Whether the window is one day, and so reads better in hour buckets.
    var isSingleDay: Bool {
        switch self {
        case .today, .yesterday: return true
        default: return false
        }
    }

    var pickerTitle: String {
        switch self {
        case .today: return "今日"
        case .yesterday: return "昨日"
        case .thisWeek: return "本周"
        case .lastWeek: return "上周"
        case .thisMonth: return "本月"
        case .last7Days: return "近 7 日"
        case .last30Days: return "近 30 日"
        }
    }

    /// The same window, short enough for the menu bar's segmented control. Only
    /// the three windows in `compact` are ever drawn this way.
    var compactTitle: String {
        switch self {
        case .last7Days: return "7 天"
        case .last30Days: return "30 天"
        default: return pickerTitle
        }
    }

    var breakdownTitle: String { "\(pickerTitle)用量占比" }

    var tokenTrendTitle: String {
        isSingleDay ? "\(pickerTitle)分时用量" : "\(pickerTitle)用量"
    }

    var skillTitle: String { "\(pickerTitle) Skill" }
}

/// The three ways the same tokens can be sliced. The data for all of them comes
/// from one dashboard snapshot, so switching views costs nothing.
enum ATMUsageDimension: String, CaseIterable, Identifiable {
    case model
    case client
    case project

    var id: String { rawValue }

    var title: String {
        switch self {
        case .model: return "模型"
        case .client: return "客户端"
        case .project: return "项目"
        }
    }

    var filterTitle: String {
        switch self {
        case .model: return "全部模型"
        case .client: return "全部客户端"
        case .project: return "全部项目"
        }
    }

    var emptyStateTitle: String {
        switch self {
        case .model: return "所选范围暂无模型趋势"
        case .client: return "所选范围暂无客户端趋势"
        case .project: return "所选范围暂无项目趋势"
        }
    }
}

/// What the whole usage page is currently looking at. One lens drives the metric
/// cards, the chart and the ranked list together -- switching the chart alone left
/// the totals above it answering a different question.
enum ATMUsageLens: String, CaseIterable, Identifiable {
    case total
    case model
    case client
    case project

    var id: String { rawValue }

    var title: String {
        switch self {
        case .total: return "总量"
        case .model: return "模型"
        case .client: return "客户端"
        case .project: return "项目"
        }
    }

    /// nil means one aggregate line, so no series breakdown and no filter.
    var breakdown: ATMUsageDimension? {
        switch self {
        case .total: return nil
        case .model: return .model
        case .client: return .client
        case .project: return .project
        }
    }
}

/// What the trend line measures. Both readings share the same buckets, series and
/// filters, so switching is a change of y value rather than a different chart.
enum ATMUsageTrendMetric: String, CaseIterable, Identifiable {
    case tokens
    case speed

    var id: String { rawValue }

    var title: String {
        switch self {
        case .tokens: return "Token"
        case .speed: return "速度"
        }
    }

    var axisTitle: String {
        switch self {
        case .tokens: return "Token"
        case .speed: return "tok/s"
        }
    }

    /// Shown when the selected filters have data but this reading does not.
    var emptyStateTitle: String {
        switch self {
        case .tokens: return "所选筛选暂无趋势"
        case .speed: return "所选范围内没有可测速的请求"
        }
    }
}

/// Independent multi-select filters on the usage page. Empty string means "全部"
/// for that field. Options cascade: picking a client narrows the model list to
/// models that client used, and so on.
struct ATMUsageFilters: Equatable {
    var model: String = ""
    var client: String = ""
    var project: String = ""

    var isEmpty: Bool { model.isEmpty && client.isEmpty && project.isEmpty }

    var scopeDescription: String {
        var parts: [String] = []
        if !model.isEmpty { parts.append("模型 \(model)") }
        if !client.isEmpty { parts.append("客户端 \(client)") }
        if !project.isEmpty { parts.append("项目 \(project)") }
        if parts.isEmpty {
            return "配额、会话、Token 与费用 · 可按模型 / 客户端 / 项目筛选"
        }
        return "筛选：" + parts.joined(separator: " · ")
    }
}

enum ATMPagination {
    static func pageCount(itemCount: Int, pageSize: Int) -> Int {
        guard itemCount > 0 else { return 0 }
        let size = max(pageSize, 1)
        return (itemCount + size - 1) / size
    }

    static func clampedPage(_ page: Int, itemCount: Int, pageSize: Int) -> Int {
        let count = pageCount(itemCount: itemCount, pageSize: pageSize)
        guard count > 0 else { return 0 }
        return min(max(page, 0), count - 1)
    }

    static func items<Element>(
        _ items: [Element],
        page: Int,
        pageSize: Int
    ) -> [Element] {
        guard !items.isEmpty else { return [] }
        let size = max(pageSize, 1)
        let safePage = clampedPage(page, itemCount: items.count, pageSize: size)
        return Array(items.dropFirst(safePage * size).prefix(size))
    }
}

struct ATMDayStats: Decodable, Identifiable, Equatable {
    let date: String
    let sessions: Int
    let queries: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double

    var id: String { date }
    var totalTokens: Int { inputTokens + outputTokens }

    enum CodingKeys: String, CodingKey {
        case date, sessions, queries
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case costUSD = "cost_usd"
    }

    init(
        date: String,
        sessions: Int,
        queries: Int,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int = 0,
        costUSD: Double
    ) {
        self.date = date
        self.sessions = sessions
        self.queries = queries
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        date = try values.decode(String.self, forKey: .date)
        sessions = try values.decode(Int.self, forKey: .sessions)
        queries = try values.decode(Int.self, forKey: .queries)
        inputTokens = try values.decode(Int.self, forKey: .inputTokens)
        outputTokens = try values.decode(Int.self, forKey: .outputTokens)
        cacheReadTokens = try values.decodeIfPresent(Int.self, forKey: .cacheReadTokens) ?? 0
        costUSD = try values.decode(Double.self, forKey: .costUSD)
    }
}

struct ATMModelStats: Decodable, Identifiable, Equatable {
    let client: String
    let model: String
    let sessions: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double
    /// Whether this row's rate was ATM's guess — the model's family, or the
    /// conservative default when even that missed. An invented cost is
    /// indistinguishable from a known one on screen unless it is marked.
    let costEstimated: Bool

    var id: String { "\(client):\(model)" }
    var displayName: String { client.isEmpty ? model : "\(model) · \(client)" }
    var totalTokens: Int { inputTokens + outputTokens }
    var cacheShare: Double {
        guard totalTokens > 0 else { return 0 }
        return min(max(Double(cacheReadTokens) / Double(totalTokens), 0), 1)
    }

    enum CodingKeys: String, CodingKey {
        case client, model, sessions
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case costUSD = "cost_usd"
        case costEstimated = "cost_estimated"
    }

    init(
        client: String = "",
        model: String,
        sessions: Int,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int,
        costUSD: Double,
        costEstimated: Bool = false
    ) {
        self.client = client
        self.model = model
        self.sessions = sessions
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
        self.costEstimated = costEstimated
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        client = try values.decodeIfPresent(String.self, forKey: .client) ?? ""
        model = try values.decode(String.self, forKey: .model)
        sessions = try values.decode(Int.self, forKey: .sessions)
        inputTokens = try values.decode(Int.self, forKey: .inputTokens)
        outputTokens = try values.decode(Int.self, forKey: .outputTokens)
        cacheReadTokens = try values.decodeIfPresent(Int.self, forKey: .cacheReadTokens) ?? 0
        costUSD = try values.decode(Double.self, forKey: .costUSD)
        costEstimated = try values.decodeIfPresent(Bool.self, forKey: .costEstimated) ?? false
    }
}

struct ATMModelDayStats: Decodable, Identifiable, Equatable {
    let date: String
    let client: String
    let model: String
    let sessions: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double
    /// Output tokens and milliseconds of the requests in this bucket whose speed
    /// could be measured. Kept as sums, not a rate, so merging buckets or models
    /// divides totals instead of averaging averages.
    let measuredOutputTokens: Int
    let measuredDurationMS: Int

    var id: String { "\(date):\(client):\(model)" }
    var displayName: String { client.isEmpty ? model : "\(model) · \(client)" }
    var totalTokens: Int { inputTokens + outputTokens }

    enum CodingKeys: String, CodingKey {
        case date, client, model, sessions
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case costUSD = "cost_usd"
        case measuredOutputTokens = "measured_output_tokens"
        case measuredDurationMS = "measured_duration_ms"
    }

    init(
        date: String,
        client: String = "",
        model: String,
        sessions: Int,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int,
        costUSD: Double,
        measuredOutputTokens: Int = 0,
        measuredDurationMS: Int = 0
    ) {
        self.date = date
        self.client = client
        self.model = model
        self.sessions = sessions
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
        self.measuredOutputTokens = measuredOutputTokens
        self.measuredDurationMS = measuredDurationMS
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        date = try values.decode(String.self, forKey: .date)
        client = try values.decodeIfPresent(String.self, forKey: .client) ?? ""
        model = try values.decode(String.self, forKey: .model)
        sessions = try values.decode(Int.self, forKey: .sessions)
        inputTokens = try values.decode(Int.self, forKey: .inputTokens)
        outputTokens = try values.decode(Int.self, forKey: .outputTokens)
        cacheReadTokens = try values.decodeIfPresent(Int.self, forKey: .cacheReadTokens) ?? 0
        costUSD = try values.decode(Double.self, forKey: .costUSD)
        measuredOutputTokens = try values.decodeIfPresent(Int.self, forKey: .measuredOutputTokens) ?? 0
        measuredDurationMS = try values.decodeIfPresent(Int.self, forKey: .measuredDurationMS) ?? 0
    }
}

struct ATMProjectDayStats: Decodable, Identifiable, Equatable {
    let date: String
    let client: String
    let project: String
    let sessions: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double
    /// See ATMModelDayStats: measured-speed components, kept as sums.
    let measuredOutputTokens: Int
    let measuredDurationMS: Int

    var id: String { "\(date):\(client):\(project)" }
    var displayName: String { project.isEmpty ? "未归类" : project }
    var totalTokens: Int { inputTokens + outputTokens }

    enum CodingKeys: String, CodingKey {
        case date, client, project, sessions
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case costUSD = "cost_usd"
        case measuredOutputTokens = "measured_output_tokens"
        case measuredDurationMS = "measured_duration_ms"
    }

    init(
        date: String,
        client: String = "",
        project: String,
        sessions: Int,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int = 0,
        costUSD: Double,
        measuredOutputTokens: Int = 0,
        measuredDurationMS: Int = 0
    ) {
        self.date = date
        self.client = client
        self.project = project
        self.sessions = sessions
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
        self.measuredOutputTokens = measuredOutputTokens
        self.measuredDurationMS = measuredDurationMS
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        date = try values.decode(String.self, forKey: .date)
        client = try values.decodeIfPresent(String.self, forKey: .client) ?? ""
        project = try values.decodeIfPresent(String.self, forKey: .project) ?? ""
        sessions = try values.decode(Int.self, forKey: .sessions)
        inputTokens = try values.decode(Int.self, forKey: .inputTokens)
        outputTokens = try values.decode(Int.self, forKey: .outputTokens)
        cacheReadTokens = try values.decodeIfPresent(Int.self, forKey: .cacheReadTokens) ?? 0
        costUSD = try values.decode(Double.self, forKey: .costUSD)
        measuredOutputTokens = try values.decodeIfPresent(Int.self, forKey: .measuredOutputTokens) ?? 0
        measuredDurationMS = try values.decodeIfPresent(Int.self, forKey: .measuredDurationMS) ?? 0
    }
}

/// `atm stats` grouped by project and client. This is the only place the desktop
/// snapshot carries per-project cost, so the project view reads from it rather
/// than re-deriving totals from the session list.
struct ATMProjectStats: Decodable, Identifiable, Equatable {
    let project: String
    let agent: String
    let sessions: Int
    let queries: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double

    var id: String { "\(project):\(agent)" }
    var displayName: String { project.isEmpty ? "未归类" : project }
    var totalTokens: Int { inputTokens + outputTokens }

    enum CodingKeys: String, CodingKey {
        case project, agent, sessions, queries
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case costUSD = "cost_usd"
    }

    init(
        project: String,
        agent: String = "",
        sessions: Int,
        queries: Int = 0,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int = 0,
        costUSD: Double
    ) {
        self.project = project
        self.agent = agent
        self.sessions = sessions
        self.queries = queries
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        project = try values.decodeIfPresent(String.self, forKey: .project) ?? ""
        agent = try values.decodeIfPresent(String.self, forKey: .agent) ?? ""
        sessions = try values.decodeIfPresent(Int.self, forKey: .sessions) ?? 0
        queries = try values.decodeIfPresent(Int.self, forKey: .queries) ?? 0
        inputTokens = try values.decodeIfPresent(Int.self, forKey: .inputTokens) ?? 0
        outputTokens = try values.decodeIfPresent(Int.self, forKey: .outputTokens) ?? 0
        cacheReadTokens = try values.decodeIfPresent(Int.self, forKey: .cacheReadTokens) ?? 0
        costUSD = try values.decodeIfPresent(Double.self, forKey: .costUSD) ?? 0
    }
}

/// One point of a usage trend: a time bucket plus the label its series is grouped
/// by -- a model, a client or a project.
struct ATMUsageSeriesPoint: Identifiable, Equatable {
    let date: String
    let series: String
    let sessions: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double
    /// Measured-speed components for this bucket; see ATMModelDayStats.
    let measuredOutputTokens: Int
    let measuredDurationMS: Int

    init(
        date: String,
        series: String,
        sessions: Int,
        inputTokens: Int,
        outputTokens: Int,
        cacheReadTokens: Int,
        costUSD: Double,
        measuredOutputTokens: Int = 0,
        measuredDurationMS: Int = 0
    ) {
        self.date = date
        self.series = series
        self.sessions = sessions
        self.inputTokens = inputTokens
        self.outputTokens = outputTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
        self.measuredOutputTokens = measuredOutputTokens
        self.measuredDurationMS = measuredDurationMS
    }

    var id: String { "\(date):\(series)" }
    var totalTokens: Int { inputTokens + outputTokens }
    var cacheShare: Double {
        guard totalTokens > 0 else { return 0 }
        return min(max(Double(cacheReadTokens) / Double(totalTokens), 0), 1)
    }
    /// Output tokens per second, or nil when nothing in this bucket could be
    /// measured. nil is not 0: a bucket with no samples has no speed, and drawing
    /// it as zero would read as the model stalling.
    var tokensPerSecond: Double? {
        guard measuredDurationMS > 0, measuredOutputTokens > 0 else { return nil }
        return Double(measuredOutputTokens) / (Double(measuredDurationMS) / 1000)
    }
}

/// One plotted point, after a reading has been chosen. Both readings are Doubles
/// here so the chart is written once; the y-axis labels differ, not the chart.
struct ATMTrendPoint: Identifiable, Equatable {
    let date: String
    let series: String
    let value: Double

    var id: String { "\(date):\(series)" }
    /// The bucket as a Date, for the time axis. Unparseable buckets sort to the
    /// far past, matching what the chart did before this type existed.
    var day: Date { ATMUsageDateAxis.date(from: date) ?? .distantPast }

    init(date: String, series: String, value: Double) {
        self.date = date
        self.series = series
        self.value = value
    }

    init(from point: ATMUsageSeriesPoint, value: Double) {
        self.init(date: point.date, series: point.series, value: value)
    }
}

/// One metric card. Which cards exist depends on the lens, because the dimensions
/// are not measured the same way: `atm stats` counts queries per project but not per
/// model, and summing sessions across models would count one session once per model
/// it used. A card that cannot be measured honestly is left out rather than shown
/// as a number that means something else.
enum ATMUsageMetric: Equatable {
    case seriesCount(Int, String)
    case tokens(Int)
    case output(Int)
    case cacheHitRate(Double)
    case sessions(Int)
    case queries(Int)
    case cost(Double)
    /// Output tokens per second across the range's measurable requests, and the
    /// median wait from sending a message to the last reply. Both are derived from
    /// transcript timestamps, so both are absent when nothing could be measured.
    case throughput(Double)
    case turnWait(Double)
}

/// One row of the ranked breakdown under the chart. `series` is the same string the
/// chart and the filter use, so clicking a row can narrow the whole page to it.
struct ATMUsageBreakdownRow: Identifiable, Equatable {
    let series: String
    let label: String
    let subtitle: String
    let sessions: Int
    let totalTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double
    /// Whether any spend in this row was priced at a rate ATM guessed. Rows that
    /// merge several models inherit the mark if even one of them is estimated:
    /// the total is only as trustworthy as its weakest component.
    let costEstimated: Bool

    init(
        series: String,
        label: String,
        subtitle: String,
        sessions: Int,
        totalTokens: Int,
        cacheReadTokens: Int,
        costUSD: Double,
        costEstimated: Bool = false
    ) {
        self.series = series
        self.label = label
        self.subtitle = subtitle
        self.sessions = sessions
        self.totalTokens = totalTokens
        self.cacheReadTokens = cacheReadTokens
        self.costUSD = costUSD
        self.costEstimated = costEstimated
    }

    var id: String { series }
    var cacheShare: Double {
        guard totalTokens > 0 else { return 0 }
        return min(max(Double(cacheReadTokens) / Double(totalTokens), 0), 1)
    }
}

enum ATMUsageDateAxis {
    private static let dayParser: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = .current
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    private static let hourParser: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = .current
        formatter.dateFormat = "yyyy-MM-dd HH:mm"
        return formatter
    }()

    static func date(from value: String) -> Date? {
        if value.contains(" ") {
            return hourParser.date(from: value)
        }
        return dayParser.date(from: value)
    }

    static func values(_ source: [String], maximumLabels: Int = 7) -> [Date] {
        var seen = Set<Date>()
        let dates = source
            .compactMap(date)
            .filter { seen.insert($0).inserted }
            .sorted()
        guard dates.count > maximumLabels, maximumLabels > 1 else { return dates }

        let step = max(Int(ceil(Double(dates.count - 1) / Double(maximumLabels - 1))), 1)
        var selected = stride(from: 0, to: dates.count, by: step).map { dates[$0] }
        if let last = dates.last, selected.last != last {
            selected.append(last)
        }
        return selected
    }

    static func paddedDomain(_ source: [String]) -> ClosedRange<Date> {
        let dates = source.compactMap(date).sorted()
        let unit: TimeInterval = source.first?.contains(" ") == true ? 60 * 60 : 24 * 60 * 60
        let first = dates.first ?? Date()
        let last = dates.last ?? first

        // Swift Charts may drop an explicitly supplied final label when it sits
        // against the plot boundary, so reserve a full date-width on that side.
        return first.addingTimeInterval(-unit * 0.5)...last.addingTimeInterval(unit * 1.25)
    }
}

struct ATMCurrentSession: Decodable, Equatable {
    let binding: ATMCurrentSessionBinding?
    let bound: Bool
    let state: String
    let todo: ATMCurrentSessionTodo?

    enum CodingKeys: String, CodingKey {
        case binding, bound, state, todo
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        binding = try values.decodeIfPresent(ATMCurrentSessionBinding.self, forKey: .binding)
        bound = try values.decodeIfPresent(Bool.self, forKey: .bound) ?? false
        state = try values.decodeIfPresent(String.self, forKey: .state) ?? (bound ? "bound" : "unbound")
        todo = try values.decodeIfPresent(ATMCurrentSessionTodo.self, forKey: .todo)
    }
}

struct ATMCurrentSessionBinding: Decodable, Equatable {
    let sessionID: String
    let todoID: String
    let agent: String
    let project: String
    let cwd: String?
    let boundAt: Int64

    enum CodingKeys: String, CodingKey {
        case agent, project, cwd
        case sessionID = "session_id"
        case todoID = "todo_id"
        case boundAt = "bound_at"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        sessionID = try values.decode(String.self, forKey: .sessionID)
        todoID = try values.decode(String.self, forKey: .todoID)
        agent = try values.decodeIfPresent(String.self, forKey: .agent) ?? ""
        project = try values.decodeIfPresent(String.self, forKey: .project) ?? ""
        cwd = try values.decodeIfPresent(String.self, forKey: .cwd)
        boundAt = try values.decodeIfPresent(Int64.self, forKey: .boundAt) ?? 0
    }
}

struct ATMCurrentSessionTodo: Decodable, Equatable {
    let id: String
    let title: String
    let project: String?
    let status: String
}

struct ATMLiveSession: Decodable, Identifiable, Equatable {
    let tool: String
    let sessionID: String
    let resumeID: String?
    let project: String
    let client: String?
    let cwd: String?
    let model: String?
    let summary: String?
    let ageSeconds: Int
    let pid: String?
    let tty: String?
    let terminalApp: String?
    let firstQuestion: String?
    let lastQuestion: String?
    let lastAnswer: String?
    let latestResult: String?
    let updates: [String]
    let recentTools: [String]
    let topics: [String]
    let activityState: String
    let bindingState: String
    let binding: ATMCurrentSessionBinding?
    let todo: ATMCurrentSessionTodo?

    /// Live hook signal joined in by `ATMAgentEventBus` after decoding.
    ///
    /// Deliberately outside `CodingKeys`: hook events arrive on the notch socket,
    /// not in the `atm session status` payload, and they are what let the notch
    /// know about a blocked permission prompt — a moment the transcript this
    /// struct is otherwise built from never records.
    var attentionSignal: ATMAgentAttentionSignal?

    var id: String { "\(tool):\(sessionID)" }
    var bindingTodoID: String? { binding?.todoID }

    init(
        tool: String,
        sessionID: String,
        resumeID: String? = nil,
        project: String,
        client: String? = nil,
        cwd: String? = nil,
        model: String? = nil,
        summary: String? = nil,
        ageSeconds: Int,
        pid: String? = nil,
        tty: String? = nil,
        terminalApp: String? = nil,
        firstQuestion: String? = nil,
        lastQuestion: String? = nil,
        lastAnswer: String? = nil,
        latestResult: String? = nil,
        updates: [String] = [],
        recentTools: [String] = [],
        topics: [String] = [],
        activityState: String? = nil,
        bindingState: String? = nil,
        binding: ATMCurrentSessionBinding? = nil,
        todo: ATMCurrentSessionTodo? = nil
    ) {
        self.tool = tool
        self.sessionID = sessionID
        self.resumeID = resumeID
        self.project = project
        self.client = client
        self.cwd = cwd
        self.model = model
        self.summary = summary
        self.ageSeconds = ageSeconds
        self.pid = pid
        self.tty = tty
        self.terminalApp = terminalApp
        self.firstQuestion = firstQuestion
        self.lastQuestion = lastQuestion
        self.lastAnswer = lastAnswer
        self.latestResult = latestResult
        self.updates = updates
        self.recentTools = recentTools
        self.topics = topics
        self.activityState = activityState ?? (ageSeconds >= 120 ? "idle" : "active")
        self.bindingState = bindingState ?? (binding == nil ? "unbound" : "bound")
        self.binding = binding
        self.todo = todo
    }

    enum CodingKeys: String, CodingKey {
        case tool, project, client, cwd, model, summary, pid, tty, topics, binding, todo, updates
        case recentTools = "tools"
        case sessionID = "session_id"
        case resumeID = "resume_id"
        case terminalApp = "terminal_app"
        case ageSeconds = "age_seconds"
        case activityState = "activity_state"
        case bindingState = "binding_state"
        case firstQuestion = "first_q"
        case lastQuestion = "last_q"
        case lastAnswer = "last_a"
        case latestResult = "latest_result"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        tool = try values.decode(String.self, forKey: .tool)
        sessionID = try values.decode(String.self, forKey: .sessionID)
        resumeID = try values.decodeIfPresent(String.self, forKey: .resumeID)
        project = try values.decode(String.self, forKey: .project)
        client = try values.decodeIfPresent(String.self, forKey: .client)
        cwd = try values.decodeIfPresent(String.self, forKey: .cwd)
        model = try values.decodeIfPresent(String.self, forKey: .model)
        summary = try values.decodeIfPresent(String.self, forKey: .summary)
        ageSeconds = try values.decode(Int.self, forKey: .ageSeconds)
        pid = try values.decodeIfPresent(String.self, forKey: .pid)
        tty = try values.decodeIfPresent(String.self, forKey: .tty)
        terminalApp = try values.decodeIfPresent(String.self, forKey: .terminalApp)
        firstQuestion = try values.decodeIfPresent(String.self, forKey: .firstQuestion)
        lastQuestion = try values.decodeIfPresent(String.self, forKey: .lastQuestion)
        lastAnswer = try values.decodeIfPresent(String.self, forKey: .lastAnswer)
        latestResult = try values.decodeIfPresent(String.self, forKey: .latestResult)
        updates = try values.decodeIfPresent([String].self, forKey: .updates) ?? []
        recentTools = try values.decodeIfPresent([String].self, forKey: .recentTools) ?? []
        topics = try values.decodeIfPresent([String].self, forKey: .topics) ?? []
        activityState = try values.decodeIfPresent(String.self, forKey: .activityState)
            ?? (ageSeconds >= 120 ? "idle" : "active")
        binding = try values.decodeIfPresent(ATMCurrentSessionBinding.self, forKey: .binding)
        bindingState = try values.decodeIfPresent(String.self, forKey: .bindingState)
            ?? (binding == nil ? "unbound" : "bound")
        todo = try values.decodeIfPresent(ATMCurrentSessionTodo.self, forKey: .todo)
    }
}

struct ATMLiveBindingContext: Decodable, Equatable {
    let state: String
    let binding: ATMCurrentSessionBinding
    let todo: ATMCurrentSessionTodo?
    let observed: Bool
    let observedSessionID: String?

    enum CodingKeys: String, CodingKey {
        case state, binding, todo, observed
        case observedSessionID = "observed_session_id"
    }
}

enum ATMLiveSessionPhase: String, CaseIterable, Identifiable {
    case active
    case bound
    case idle
    case bindingIssue

    var id: String { rawValue }

    var title: String {
        switch self {
        case .active: return "最近活跃"
        case .bound: return "当前绑定"
        case .idle: return "最近空闲"
        case .bindingIssue: return "绑定异常"
        }
    }
}

extension ATMLiveSession {
    var phase: ATMLiveSessionPhase {
        if bindingState != "unbound" && bindingState != "bound" { return .bindingIssue }
        if activityState == "unobserved" { return bindingState == "bound" ? .bound : .bindingIssue }
        if activityState == "idle" || ageSeconds >= 120 { return .idle }
        return .active
    }

    var displayTitle: String {
        firstMeaningfulText(summary, lastQuestion, firstQuestion)
            .map { ATMMarkdown.plainSummary($0, limit: 72) }
            ?? "\(tool) 会话"
    }

    var currentObjective: String {
        firstMeaningfulText(lastQuestion, summary, firstQuestion)
            .map { ATMMarkdown.plainSummary($0, limit: 180) }
            ?? "暂未提取到当前目标"
    }

    var progressText: String {
        if phase == .bindingIssue, let bindingTodoID {
            return "会话仍记录为绑定 \(bindingTodoID.uppercased())，但对应 Todo 状态无效，需要解除或重新绑定。"
        }
        if activityState == "unobserved", let bindingTodoID {
            return "当前会话显式绑定 \(bindingTodoID.uppercased())；暂未检测到实时活动。"
        }
        return lastAnswer
            .flatMap { value in
                let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !trimmed.isEmpty, !Self.looksLikeInjectedInstructions(trimmed) else { return nil }
                return ATMMarkdown.plainSummary(trimmed, limit: 220)
            }
            ?? (phase == .idle ? "会话最近没有继续输出，目前处于空闲状态。" : "Agent 最近有活动，暂未产生可展示的回复。")
    }

    /// A separate reply block is useful only when it adds information below the
    /// conversation title. While title metadata is unavailable, presenceTitle
    /// may itself fall back to the latest answer; do not render that answer twice.
    var latestReplyText: String? {
        guard let answer = lastAnswer?.trimmingCharacters(in: .whitespacesAndNewlines),
              !answer.isEmpty,
              !Self.looksLikeInjectedInstructions(answer),
              let titleSource = firstMeaningfulText(summary, lastQuestion, firstQuestion) else {
            return nil
        }
        let normalizedAnswer = Self.comparableText(answer)
        guard normalizedAnswer != Self.comparableText(titleSource) else { return nil }
        return ATMMarkdown.plainSummary(answer, limit: 220)
    }

    var latestUserInputText: String? {
        guard let input = lastQuestion?.trimmingCharacters(in: .whitespacesAndNewlines),
              !input.isEmpty,
              !Self.looksLikeInjectedInstructions(input) else {
            return nil
        }
        return ATMMarkdown.plainSummary(input, limit: 220)
    }

    /// The latest user message, unless it is already serving as the title.
    ///
    /// `presenceTitle` falls back to `lastQuestion` whenever no summary exists,
    /// so a card that draws both prints one sentence twice — clipped at 72 in
    /// the title and at 220 below it. `latestReplyText` guards the answer side
    /// of exactly this; this is the mirror for the question side.
    ///
    /// Deliberately not folded into `latestUserInputText`: the notch and sound
    /// turn trackers diff that value to tell one turn from the next, and they
    /// have to keep seeing the input even on the cards that decline to draw it.
    var latestUserInputBelowTitle: String? {
        guard let input = latestUserInputText else { return nil }
        guard let question = lastQuestion?.trimmingCharacters(in: .whitespacesAndNewlines),
              let titleSource = firstMeaningfulText(summary, lastQuestion, firstQuestion, lastAnswer),
              Self.comparableText(question) == Self.comparableText(titleSource) else {
            return input
        }
        return nil
    }

    var latestResultText: String? {
        if let result = latestResult?.trimmingCharacters(in: .whitespacesAndNewlines),
           !result.isEmpty,
           !Self.looksLikeInjectedInstructions(result) {
            return String(result.prefix(1_200))
        }
        return latestReplyText
    }

    var visibleUpdates: [String] {
        let resultKey = latestResultText.map(Self.comparableText)
        var seen = Set<String>()
        return updates.compactMap { value in
            let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty, !Self.looksLikeInjectedInstructions(trimmed) else { return nil }
            let summary = ATMMarkdown.plainSummary(trimmed, limit: 400)
            let key = Self.comparableText(summary)
            guard key != resultKey, seen.insert(key).inserted else { return nil }
            return summary
        }
    }

    var needsUserAttention: Bool {
        // A hook told us outright, so stop guessing.
        if attentionSignal != nil { return true }
        return matchesAttentionKeywords
    }

    /// The pre-hook heuristic, kept for agents with no hook integration
    /// (copilot, qoder). It only sees text the agent actually wrote, so it
    /// is blind to a tool call blocked on a permission prompt — which is exactly
    /// why `attentionSignal` takes precedence.
    var matchesAttentionKeywords: Bool {
        guard let answer = lastAnswer?.trimmingCharacters(in: .whitespacesAndNewlines),
              !answer.isEmpty,
              !Self.looksLikeInjectedInstructions(answer) else { return false }
        let normalized = answer.lowercased()
        return [
            "等待用户", "等待你的", "需要你确认", "请确认", "请提供",
            "waiting for user", "need your confirmation", "please confirm", "please provide",
        ].contains { normalized.contains($0) }
    }

    var needsUserText: String {
        if let signal = attentionSignal {
            let tool = signal.tool.map { "（\($0)）" } ?? ""
            return "\(signal.displayReason)\(tool)。来自 \(signal.source) hook 的实时信号。"
        }
        if matchesAttentionKeywords {
            return "需要确认。Agent 最近回复中包含等待你的提示。"
        }
        switch phase {
        case .bound:
            return "当前绑定有效；暂未检测到需要你介入的实时信号。"
        case .bindingIssue:
            return "需要处理。当前 Session binding 与 Todo 生命周期不一致。"
        case .idle:
            return "未检测到需要你介入的信号。会话最近处于空闲状态。"
        case .active:
            return "暂不需要。Agent 最近仍有活动，可以继续观察执行进度。"
        }
    }

    private func firstMeaningfulText(_ candidates: String?...) -> String? {
        for candidate in candidates {
            guard let value = candidate?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !value.isEmpty,
                  !Self.looksLikeInjectedInstructions(value) else { continue }
            return value.replacingOccurrences(of: "\n", with: " ")
        }
        return nil
    }

    private static func looksLikeInjectedInstructions(_ value: String) -> Bool {
        let prefix = value.prefix(240).lowercased()
        return prefix.contains("agents.md instructions")
            || prefix.contains("<instructions>")
            || prefix.contains("system-reminder")
            || prefix.contains("some conversation entries were omitted")
            || prefix.contains("no retained transcript delta entries")
    }

    private static func comparableText(_ value: String) -> String {
        ATMMarkdown.plainSummary(value, limit: 4_000)
            .lowercased()
            .split(whereSeparator: { $0.isWhitespace })
            .joined(separator: " ")
    }

}

struct ATMLiveStatus: Decodable {
    let sessions: [ATMLiveSession]
    let bindings: [ATMLiveBindingContext]
    let time: String

    enum CodingKeys: String, CodingKey {
        case sessions, bindings, time
    }

    init(sessions: [ATMLiveSession], bindings: [ATMLiveBindingContext] = [], time: String) {
        self.sessions = sessions
        self.bindings = bindings
        self.time = time
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        sessions = try values.decodeIfPresent([ATMLiveSession].self, forKey: .sessions) ?? []
        bindings = try values.decodeIfPresent([ATMLiveBindingContext].self, forKey: .bindings) ?? []
        time = try values.decodeIfPresent(String.self, forKey: .time) ?? ""
    }

    static let empty = ATMLiveStatus(sessions: [], bindings: [], time: "")

    /// Stamps each session with the live hook signal for it, if any.
    ///
    /// Applied on every snapshot rather than once when the event arrives,
    /// because the poller replaces the whole session array each refresh and
    /// would otherwise wipe the overlay.
    func applyingAttentionSignals(
        _ signals: [String: ATMAgentAttentionSignal],
        now: Date = Date()
    ) -> ATMLiveStatus {
        guard !signals.isEmpty else { return self }
        return ATMLiveStatus(
            sessions: ATMAgentAttentionJoin.merge(sessions, signals: signals, now: now),
            bindings: bindings,
            time: time
        )
    }
}

enum ATMAgentPresenceState: String, CaseIterable, Identifiable {
    case attention
    case active
    case recent

    var id: String { rawValue }

    var title: String {
        switch self {
        case .attention: return "需要你"
        case .active: return "正在活跃"
        case .recent: return "刚刚活跃"
        }
    }
}

extension ATMLiveSession {
    var isCurrentlyActive: Bool {
        activityState == "active" && ageSeconds < 120
    }

    var presenceState: ATMAgentPresenceState {
        if needsUserAttention || (bindingState != "bound" && bindingState != "unbound") {
            return .attention
        }
        return isCurrentlyActive ? .active : .recent
    }

    /// Presence cards need a useful label even while a client has not emitted a
    /// summary or retained user turn yet. The latest Agent text is only a final
    /// fallback and still goes through the injected-instruction filter.
    var presenceTitle: String {
        firstMeaningfulText(summary, lastQuestion, firstQuestion, lastAnswer)
            .map { ATMMarkdown.plainSummary($0, limit: 72) }
            ?? "\(ATMAgentDisplay.name(tool)) 会话"
    }
}

struct ATMSessionSummary: Decodable, Identifiable, Equatable {
    let shortID: String
    let agent: String
    let project: String
    let createdAt: String
    let queryCount: Int
    let firstQuestion: String?

    var id: String { "\(agent):\(shortID)" }

    enum CodingKeys: String, CodingKey {
        case agent, project
        case shortID = "short_id"
        case createdAt = "created_at"
        case queryCount = "q_count"
        case firstQuestion = "first_q"
    }
}

/// Usage generated by one session during today's event-time window. These are
/// not lifetime session totals: a session resumed after midnight only carries
/// the requests that happened today.
struct ATMSessionUsage: Decodable, Identifiable, Equatable {
    let sessionID: String
    let shortID: String
    let agent: String
    let project: String
    let model: String
    let startedTS: Int
    let lastTS: Int
    let requests: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheCreateTokens: Int
    let cacheReadTokens: Int
    let totalTokens: Int
    let costUSD: Double
    let share: Double

    var id: String { sessionID }
    var cacheTokens: Int { cacheCreateTokens + cacheReadTokens }

    var activityTimeText: String {
        let start = Self.timeFormatter.string(from: Date(timeIntervalSince1970: TimeInterval(startedTS)))
        let end = Self.timeFormatter.string(from: Date(timeIntervalSince1970: TimeInterval(lastTS)))
        return start == end ? start : "\(start)–\(end)"
    }

    enum CodingKeys: String, CodingKey {
        case agent, project, model, requests, share
        case sessionID = "session_id"
        case shortID = "short_id"
        case startedTS = "started_ts"
        case lastTS = "last_ts"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheCreateTokens = "cache_create_tokens"
        case cacheReadTokens = "cache_read_tokens"
        case totalTokens = "total_tokens"
        case costUSD = "cost_usd"
    }

    private static let timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = "HH:mm"
        return formatter
    }()
}

extension Array where Element == ATMSessionUsage {
    func filtered(using filters: ATMUsageFilters) -> [ATMSessionUsage] {
        filter { session in
            if !filters.model.isEmpty && session.model != filters.model { return false }
            if !filters.client.isEmpty && ATMAgentDisplay.name(session.agent) != filters.client {
                return false
            }
            if !filters.project.isEmpty && session.project != filters.project { return false }
            return session.totalTokens > 0
        }
    }

    /// Session rows contain all three dimensions, so their menus can cascade
    /// exactly rather than using the looser joins needed by dashboard rollups.
    func filterOptions(
        dimension: ATMUsageDimension,
        filters: ATMUsageFilters
    ) -> [String] {
        var totals: [String: Int] = [:]
        for session in self where session.totalTokens > 0 {
            if dimension != .model,
               !filters.model.isEmpty,
               session.model != filters.model {
                continue
            }
            if dimension != .client,
               !filters.client.isEmpty,
               ATMAgentDisplay.name(session.agent) != filters.client {
                continue
            }
            if dimension != .project,
               !filters.project.isEmpty,
               session.project != filters.project {
                continue
            }

            let key: String
            switch dimension {
            case .model:
                key = session.model
            case .client:
                key = ATMAgentDisplay.name(session.agent)
            case .project:
                key = session.project
            }
            if !key.isEmpty {
                totals[key, default: 0] += session.totalTokens
            }
        }
        return totals.keys.sorted {
            if totals[$0, default: 0] != totals[$1, default: 0] {
                return totals[$0, default: 0] > totals[$1, default: 0]
            }
            return $0 < $1
        }
    }
}

struct ATMSkillStats: Decodable, Identifiable, Equatable {
    let skill: String
    let calls: Int
    let sessions: Int
    let agents: Int

    var id: String { skill }
}

/// One model's measured throughput over a range, as `atm stats --by speed`
/// computes it. The percentiles arrive precomputed: medians cannot be merged, so
/// the app displays them rather than deriving them.
struct ATMSpeedModelStats: Decodable, Identifiable, Equatable {
    let client: String
    let model: String
    let requests: Int
    /// How many of those requests carried a usable measurement. 0 means this
    /// model's speed is unknown, not that it was slow.
    let sampled: Int
    let tokensPerSecondP50: Double
    let tokensPerSecondP90: Double
    let durationP50Seconds: Double
    /// The two sums behind the rate. Combining models means adding these and
    /// dividing once, never averaging the per-model rates.
    let outputTokens: Int
    let sampledSeconds: Double

    var id: String { "\(client):\(model)" }
    var displayName: String { client.isEmpty ? model : "\(model) · \(client)" }

    enum CodingKeys: String, CodingKey {
        case client, model, requests, sampled
        case tokensPerSecondP50 = "tokens_per_second_p50"
        case tokensPerSecondP90 = "tokens_per_second_p90"
        case durationP50Seconds = "duration_p50_seconds"
        case outputTokens = "output_tokens"
        case sampledSeconds = "sampled_seconds"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        client = try values.decodeIfPresent(String.self, forKey: .client) ?? ""
        model = try values.decodeIfPresent(String.self, forKey: .model) ?? ""
        requests = try values.decodeIfPresent(Int.self, forKey: .requests) ?? 0
        sampled = try values.decodeIfPresent(Int.self, forKey: .sampled) ?? 0
        tokensPerSecondP50 = try values.decodeIfPresent(Double.self, forKey: .tokensPerSecondP50) ?? 0
        tokensPerSecondP90 = try values.decodeIfPresent(Double.self, forKey: .tokensPerSecondP90) ?? 0
        durationP50Seconds = try values.decodeIfPresent(Double.self, forKey: .durationP50Seconds) ?? 0
        outputTokens = try values.decodeIfPresent(Int.self, forKey: .outputTokens) ?? 0
        sampledSeconds = try values.decodeIfPresent(Double.self, forKey: .sampledSeconds) ?? 0
    }
}

/// One agent's turn wait over a range: message sent to last reply, tools and
/// internal requests included.
struct ATMSpeedTurnStats: Decodable, Identifiable, Equatable {
    let agent: String
    let turns: Int
    let waitP50Seconds: Double
    let waitP90Seconds: Double

    var id: String { agent }

    enum CodingKeys: String, CodingKey {
        case agent, turns
        case waitP50Seconds = "wait_p50_seconds"
        case waitP90Seconds = "wait_p90_seconds"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        agent = try values.decodeIfPresent(String.self, forKey: .agent) ?? ""
        turns = try values.decodeIfPresent(Int.self, forKey: .turns) ?? 0
        waitP50Seconds = try values.decodeIfPresent(Double.self, forKey: .waitP50Seconds) ?? 0
        waitP90Seconds = try values.decodeIfPresent(Double.self, forKey: .waitP90Seconds) ?? 0
    }
}

struct ATMSpeedStats: Decodable, Equatable {
    let models: [ATMSpeedModelStats]
    let turns: [ATMSpeedTurnStats]

    static let empty = ATMSpeedStats(models: [], turns: [])

    init(models: [ATMSpeedModelStats], turns: [ATMSpeedTurnStats]) {
        self.models = models
        self.turns = turns
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        models = try values.decodeIfPresent([ATMSpeedModelStats].self, forKey: .models) ?? []
        turns = try values.decodeIfPresent([ATMSpeedTurnStats].self, forKey: .turns) ?? []
    }

    enum CodingKeys: String, CodingKey {
        case models, turns
    }

    /// Total measured output over total measured time for the models that pass
    /// `include`. nil when nothing matching could be measured — which is not 0
    /// tok/s and must not be shown as one.
    func tokensPerSecond(where include: (ATMSpeedModelStats) -> Bool = { _ in true }) -> Double? {
        var tokens = 0
        var seconds = 0.0
        for model in models where model.sampled > 0 && include(model) {
            tokens += model.outputTokens
            seconds += model.sampledSeconds
        }
        guard seconds > 0, tokens > 0 else { return nil }
        return Double(tokens) / seconds
    }

    /// Median wait across the agents that pass `include`, weighted by how many
    /// turns each contributed.
    func turnWaitSeconds(where include: (ATMSpeedTurnStats) -> Bool = { _ in true }) -> Double? {
        let measured = turns.filter { $0.turns > 0 && $0.waitP50Seconds > 0 && include($0) }
        guard !measured.isEmpty else { return nil }
        let totalTurns = measured.reduce(0) { $0 + $1.turns }
        let weighted = measured.reduce(0.0) { $0 + $1.waitP50Seconds * Double($1.turns) }
        return weighted / Double(totalTurns)
    }
}

struct ATMRangeData {
    /// The window's first and last local calendar day, both inclusive, as computed
    /// by the CLI. Day buckets are selected against these instead of by taking the
    /// trailing N entries, which only ever worked for rolling windows: yesterday is
    /// one day set back, and last week ends on a Sunday.
    let startDate: String
    let endDate: String
    let modelStats: [ATMModelStats]
    let sessions: [ATMSessionSummary]
	let skillStats: [ATMSkillStats]
    let projectStats: [ATMProjectStats]
    let speed: ATMSpeedStats

    init(
        startDate: String = "",
        endDate: String = "",
        modelStats: [ATMModelStats],
        sessions: [ATMSessionSummary],
        skillStats: [ATMSkillStats] = [],
        projectStats: [ATMProjectStats] = [],
        speed: ATMSpeedStats = .empty
    ) {
        self.startDate = startDate
        self.endDate = endDate
        self.modelStats = modelStats
        self.sessions = sessions
        self.skillStats = skillStats
        self.projectStats = projectStats
        self.speed = speed
    }

    /// Whether a day bucket falls inside this window. Both bounds are inclusive
    /// yyyy-MM-dd strings, which compare correctly as text.
    func contains(date: String) -> Bool {
        guard !startDate.isEmpty, !endDate.isEmpty else { return true }
        return date >= startDate && date <= endDate
    }

    static let empty = ATMRangeData(modelStats: [], sessions: [])
}

enum ATMDashboardContract {
    static let supportedSchemaVersion = 6
}

/// A dashboard payload whose contract version this App cannot read.
///
/// This used to surface as `DecodingError`, which reached the user as
/// "Unsupported ATM dashboard schema 7" — accurate, and useless: it names
/// neither which half is behind nor what to do about it. The CLI and the App are
/// shipped separately and updated separately, so one of them being older is a
/// routine state, not a corrupt payload.
struct ATMDashboardSchemaMismatch: LocalizedError, Equatable {
    /// Which side has to move. Derived from the two versions rather than passed
    /// in, so the message cannot disagree with the numbers it quotes.
    enum Direction: Equatable {
        case appTooOld
        case cliTooOld
    }

    let cliVersion: Int
    let appVersion: Int

    var direction: Direction {
        cliVersion > appVersion ? .appTooOld : .cliTooOld
    }

    /// Both messages name the two versions, say which side is behind, and give a
    /// command that can be run. The App cannot update itself — it has no
    /// privilege to replace its own bundle — so "self-rescue" here means the user
    /// is never left guessing which of the two to touch.
    var errorDescription: String? {
        switch direction {
        case .appTooOld:
            return "ATM App 需要更新：CLI 输出仪表盘 v\(cliVersion)，本 App 只支持 v\(appVersion)。"
        case .cliTooOld:
            return "atm CLI 需要更新：本 App 需要仪表盘 v\(appVersion)，CLI 只输出 v\(cliVersion)。"
        }
    }

    var recoverySuggestion: String? {
        switch direction {
        case .appTooOld:
            return "下载新版 ATM.app 覆盖安装后重启 App；从源码构建则运行 "
                + "app/macos/Scripts/build-app.sh。CLI 与 App 必须配套升级。"
        case .cliTooOld:
            return "运行 curl -fsSL "
                + "https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh"
                + " 更新 CLI，然后点刷新。"
        }
    }

    /// One line for a surface that shows a single string. Kept here rather than
    /// composed at each call site so every surface says the same thing.
    var summary: String {
        [errorDescription, recoverySuggestion]
            .compactMap { $0 }
            .joined(separator: " ")
    }
}

struct ATMDashboardRangeEnvelope: Decodable {
    let startDate: String
    let endDate: String
    let modelStats: [ATMModelStats]
    let sessions: [ATMSessionSummary]
    let skillStats: [ATMSkillStats]
    let projectStats: [ATMProjectStats]
    let speed: ATMSpeedStats

    enum CodingKeys: String, CodingKey {
        case sessions, speed
        case startDate = "start_date"
        case endDate = "end_date"
        case modelStats = "model_stats"
        case skillStats = "skill_stats"
        case projectStats = "project_stats"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        startDate = try values.decodeIfPresent(String.self, forKey: .startDate) ?? ""
        endDate = try values.decodeIfPresent(String.self, forKey: .endDate) ?? ""
        modelStats = try values.decodeIfPresent([ATMModelStats].self, forKey: .modelStats) ?? []
        sessions = try values.decodeIfPresent([ATMSessionSummary].self, forKey: .sessions) ?? []
        skillStats = try values.decodeIfPresent([ATMSkillStats].self, forKey: .skillStats) ?? []
        projectStats = try values.decodeIfPresent([ATMProjectStats].self, forKey: .projectStats) ?? []
        speed = try values.decodeIfPresent(ATMSpeedStats.self, forKey: .speed) ?? .empty
    }
}

struct ATMDashboardEnvelope: Decodable {
    let schemaVersion: Int
    let generatedAt: String
    let work: ATMNowSnapshot
    let todos: [ATMTodo]
    let dayStats: [ATMDayStats]
    let hourStats: [ATMDayStats]
    let modelDayStats: [ATMModelDayStats]
    let modelHourStats: [ATMModelDayStats]
    let projectDayStats: [ATMProjectDayStats]
    let projectHourStats: [ATMProjectDayStats]
    let ranges: [String: ATMDashboardRangeEnvelope]
    let liveStatus: ATMLiveStatus
    let currentSession: ATMCurrentSession?
    let indexHealth: ATMIndexHealthReport

    enum CodingKeys: String, CodingKey {
        case work, todos, ranges
        case schemaVersion = "schema_version"
        case generatedAt = "generated_at"
        case dayStats = "day_stats"
        case hourStats = "hour_stats"
        case modelDayStats = "model_day_stats"
        case modelHourStats = "model_hour_stats"
        case projectDayStats = "project_day_stats"
        case projectHourStats = "project_hour_stats"
        case liveStatus = "live_status"
        case currentSession = "current_session"
        case indexHealth = "index_health"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        schemaVersion = try values.decode(Int.self, forKey: .schemaVersion)
        guard schemaVersion == ATMDashboardContract.supportedSchemaVersion else {
            // Deliberately not a DecodingError: this is a version skew between two
            // separately shipped binaries, and the user needs to know which one to
            // update. JSONDecoder propagates a non-DecodingError unchanged, so the
            // guidance survives to the surface that shows it.
            throw ATMDashboardSchemaMismatch(
                cliVersion: schemaVersion,
                appVersion: ATMDashboardContract.supportedSchemaVersion
            )
        }
        generatedAt = try values.decodeIfPresent(String.self, forKey: .generatedAt) ?? ""
        work = try values.decode(ATMNowSnapshot.self, forKey: .work)
        todos = try values.decodeIfPresent([ATMTodo].self, forKey: .todos) ?? []
        dayStats = try values.decodeIfPresent([ATMDayStats].self, forKey: .dayStats) ?? []
        hourStats = try values.decodeIfPresent([ATMDayStats].self, forKey: .hourStats) ?? []
        modelDayStats = try values.decodeIfPresent([ATMModelDayStats].self, forKey: .modelDayStats) ?? []
        modelHourStats = try values.decodeIfPresent([ATMModelDayStats].self, forKey: .modelHourStats) ?? []
        projectDayStats = try values.decodeIfPresent([ATMProjectDayStats].self, forKey: .projectDayStats) ?? []
        projectHourStats = try values.decodeIfPresent([ATMProjectDayStats].self, forKey: .projectHourStats) ?? []
        ranges = try values.decodeIfPresent([String: ATMDashboardRangeEnvelope].self, forKey: .ranges) ?? [:]
        liveStatus = try values.decode(ATMLiveStatus.self, forKey: .liveStatus)
        currentSession = try values.decodeIfPresent(ATMCurrentSession.self, forKey: .currentSession)
        indexHealth = try values.decode(ATMIndexHealthReport.self, forKey: .indexHealth)
    }

    func makeSnapshot(refreshedAt: Date = Date()) -> ATMDashboardSnapshot {
        func range(_ name: ATMMetricsRange) -> ATMRangeData {
            guard let value = ranges[name.rawValue] else { return .empty }
            return ATMRangeData(
                startDate: value.startDate,
                endDate: value.endDate,
                modelStats: value.modelStats,
                sessions: value.sessions,
                skillStats: value.skillStats,
                projectStats: value.projectStats,
                speed: value.speed
            )
        }
        return ATMDashboardSnapshot(
            work: work,
            dayStats: dayStats,
            hourStats: hourStats,
            modelDayStats: modelDayStats,
            modelHourStats: modelHourStats,
            projectDayStats: projectDayStats,
            projectHourStats: projectHourStats,
            rangeData: Dictionary(
                uniqueKeysWithValues: ATMMetricsRange.allCases.map { ($0, range($0)) }
            ),
            liveStatus: liveStatus,
            currentSession: currentSession,
            indexHealth: indexHealth,
            refreshedAt: refreshedAt
        )
    }
}

struct ATMRangeSummary {
    let sessions: Int
    let queries: Int
    let inputTokens: Int
    let outputTokens: Int
    let cacheReadTokens: Int
    let costUSD: Double

    var totalTokens: Int { inputTokens + outputTokens }
    var cacheHitRate: Double {
        guard inputTokens > 0 else { return 0 }
        return min(max(Double(cacheReadTokens) / Double(inputTokens), 0), 1)
    }
}

struct ATMDashboardSnapshot {
    let work: ATMNowSnapshot
    let dayStats: [ATMDayStats]
    let hourStats: [ATMDayStats]
    let modelDayStats: [ATMModelDayStats]
    let modelHourStats: [ATMModelDayStats]
    let projectDayStats: [ATMProjectDayStats]
    let projectHourStats: [ATMProjectDayStats]
    let rangeData: [ATMMetricsRange: ATMRangeData]
    let liveStatus: ATMLiveStatus
    let currentSession: ATMCurrentSession?
    let indexHealth: ATMIndexHealthReport?
    let refreshedAt: Date

    init(
        work: ATMNowSnapshot,
        dayStats: [ATMDayStats],
        hourStats: [ATMDayStats],
        modelDayStats: [ATMModelDayStats],
        modelHourStats: [ATMModelDayStats],
        projectDayStats: [ATMProjectDayStats] = [],
        projectHourStats: [ATMProjectDayStats] = [],
        rangeData: [ATMMetricsRange: ATMRangeData],
        liveStatus: ATMLiveStatus,
        currentSession: ATMCurrentSession?,
        indexHealth: ATMIndexHealthReport? = nil,
        refreshedAt: Date
    ) {
        self.work = work
        self.dayStats = dayStats
        self.hourStats = hourStats
        self.modelDayStats = modelDayStats
        self.modelHourStats = modelHourStats
        self.projectDayStats = projectDayStats
        self.projectHourStats = projectHourStats
        self.rangeData = rangeData
        self.liveStatus = liveStatus
        self.currentSession = currentSession
        self.indexHealth = indexHealth
        self.refreshedAt = refreshedAt
    }

    static let empty = ATMDashboardSnapshot(
        work: .empty,
        dayStats: [],
        hourStats: [],
        modelDayStats: [],
        modelHourStats: [],
        rangeData: [:],
        liveStatus: .empty,
        currentSession: nil,
        refreshedAt: .distantPast
    )

    func removingTodos(withIDs ids: Set<String>) -> ATMDashboardSnapshot {
        guard !ids.isEmpty else { return self }
        return ATMDashboardSnapshot(
            work: work.removingTodos(withIDs: ids),
            dayStats: dayStats,
            hourStats: hourStats,
            modelDayStats: modelDayStats,
            modelHourStats: modelHourStats,
            projectDayStats: projectDayStats,
            projectHourStats: projectHourStats,
            rangeData: rangeData,
            liveStatus: liveStatus,
            currentSession: currentSession,
            indexHealth: indexHealth,
            refreshedAt: refreshedAt
        )
    }

    func replacingTodo(_ todo: ATMTodo) -> ATMDashboardSnapshot {
        ATMDashboardSnapshot(
            work: work.replacingTodo(todo),
            dayStats: dayStats,
            hourStats: hourStats,
            modelDayStats: modelDayStats,
            modelHourStats: modelHourStats,
            projectDayStats: projectDayStats,
            projectHourStats: projectHourStats,
            rangeData: rangeData,
            liveStatus: liveStatus,
            currentSession: currentSession,
            indexHealth: indexHealth,
            refreshedAt: refreshedAt
        )
    }

    func replacingLiveStatus(_ liveStatus: ATMLiveStatus) -> ATMDashboardSnapshot {
        ATMDashboardSnapshot(
            work: work,
            dayStats: dayStats,
            hourStats: hourStats,
            modelDayStats: modelDayStats,
            modelHourStats: modelHourStats,
            projectDayStats: projectDayStats,
            projectHourStats: projectHourStats,
            rangeData: rangeData,
            liveStatus: liveStatus,
            currentSession: currentSession,
            indexHealth: indexHealth,
            refreshedAt: refreshedAt
        )
    }

    var todayStats: ATMDayStats? { dayStats.last }

    func sortedModelStats(for range: ATMMetricsRange) -> [ATMModelStats] {
        (rangeData[range]?.modelStats ?? [])
            .filter { $0.totalTokens > 0 }
            .sorted {
                if $0.totalTokens != $1.totalTokens { return $0.totalTokens > $1.totalTokens }
                return $0.displayName < $1.displayName
            }
    }

    /// Day buckets inside a window, selected by the window's own dates rather than
    /// by a trailing count. A count is only ever right for a window ending today,
    /// so "yesterday" used to return today and "last week" the last seven days.
    func stats(for range: ATMMetricsRange) -> [ATMDayStats] {
        let window = rangeData[range] ?? .empty
        return dayStats.filter { window.contains(date: $0.date) }
    }

    func trendStats(for range: ATMMetricsRange) -> [ATMDayStats] {
        // Hour buckets exist for today only; any other single day falls back to its
        // one day bucket rather than showing today's hours under yesterday's label.
        if range == .today {
            return hourStats.isEmpty ? stats(for: .today) : hourStats
        }
        return stats(for: range)
    }

    /// Series names for a dimension, biggest spender first. Doubles as the filter
    /// menu's contents.
    func seriesNames(for range: ATMMetricsRange, dimension: ATMUsageDimension) -> [String] {
        let dates = Set(trendStats(for: range).map(\.date))
        let totals = Dictionary(
            grouping: seriesSource(for: range, dimension: dimension)
                .filter { dates.contains($0.date) && $0.totalTokens > 0 },
            by: \.series
        )
        .mapValues { $0.reduce(0) { $0 + $1.totalTokens } }
        return totals.keys.sorted {
            if totals[$0, default: 0] != totals[$1, default: 0] {
                return totals[$0, default: 0] > totals[$1, default: 0]
            }
            return $0 < $1
        }
    }

    /// One point per (bucket, series) pair, including the zero-token buckets, so a
    /// line chart draws a continuous line instead of skipping quiet hours.
    func lineTrendStats(
        for range: ATMMetricsRange,
        dimension: ATMUsageDimension,
        selectedSeries: String? = nil,
        topSeriesCount: Int = 5
    ) -> [ATMUsageSeriesPoint] {
        let dates = trendStats(for: range).map(\.date)
        let series = lineTrendSeries(
            for: range,
            dimension: dimension,
            selectedSeries: selectedSeries,
            topSeriesCount: topSeriesCount
        )
        var source: [String: ATMUsageSeriesPoint] = [:]
        for value in seriesSource(for: range, dimension: dimension) {
            source["\(value.date)\u{0}\(value.series)"] = value
        }
        return dates.flatMap { date in
            series.map { name in
                source["\(date)\u{0}\(name)"] ?? ATMUsageSeriesPoint(
                    date: date,
                    series: name,
                    sessions: 0,
                    inputTokens: 0,
                    outputTokens: 0,
                    cacheReadTokens: 0,
                    costUSD: 0
                )
            }
        }
    }

    func lineTrendSeries(
        for range: ATMMetricsRange,
        dimension: ATMUsageDimension,
        selectedSeries: String? = nil,
        topSeriesCount: Int = 5
    ) -> [String] {
        selectedSeries.map { [$0] } ?? Array(seriesNames(for: range, dimension: dimension).prefix(topSeriesCount))
    }

    func seriesTotals(
        for range: ATMMetricsRange,
        dimension: ATMUsageDimension,
        series: String
    ) -> ATMUsageSeriesPoint? {
        let points = seriesSource(for: range, dimension: dimension).filter { $0.series == series }
        guard !points.isEmpty else { return nil }
        return ATMUsageSeriesPoint(
            date: "",
            series: series,
            sessions: points.reduce(0) { $0 + $1.sessions },
            inputTokens: points.reduce(0) { $0 + $1.inputTokens },
            outputTokens: points.reduce(0) { $0 + $1.outputTokens },
            cacheReadTokens: points.reduce(0) { $0 + $1.cacheReadTokens },
            costUSD: points.reduce(0) { $0 + $1.costUSD },
            measuredOutputTokens: points.reduce(0) { $0 + $1.measuredOutputTokens },
            measuredDurationMS: points.reduce(0) { $0 + $1.measuredDurationMS }
        )
    }

    /// The per-bucket points for a dimension. Model and client come out of the same
    /// model-by-time rows; client just collapses the models inside each client.
    private func seriesSource(for range: ATMMetricsRange, dimension: ATMUsageDimension) -> [ATMUsageSeriesPoint] {
        switch dimension {
        case .model:
            return modelSeriesSource(for: range).map {
                ATMUsageSeriesPoint(
                    date: $0.date,
                    series: $0.displayName,
                    sessions: $0.sessions,
                    inputTokens: $0.inputTokens,
                    outputTokens: $0.outputTokens,
                    cacheReadTokens: $0.cacheReadTokens,
                    costUSD: $0.costUSD,
                    measuredOutputTokens: $0.measuredOutputTokens,
                    measuredDurationMS: $0.measuredDurationMS
                )
            }
        case .client:
            return Self.merged(
                modelSeriesSource(for: range).map {
                    ATMUsageSeriesPoint(
                        date: $0.date,
                        series: ATMAgentDisplay.name($0.client),
                        sessions: $0.sessions,
                        inputTokens: $0.inputTokens,
                        outputTokens: $0.outputTokens,
                        cacheReadTokens: $0.cacheReadTokens,
                        costUSD: $0.costUSD,
                        measuredOutputTokens: $0.measuredOutputTokens,
                        measuredDurationMS: $0.measuredDurationMS
                    )
                }
            )
        case .project:
            let source = range == .today && !projectHourStats.isEmpty ? projectHourStats : projectDayStats
            return Self.merged(
                source.map {
                    ATMUsageSeriesPoint(
                        date: $0.date,
                        series: $0.displayName,
                        sessions: $0.sessions,
                        inputTokens: $0.inputTokens,
                        outputTokens: $0.outputTokens,
                        cacheReadTokens: $0.cacheReadTokens,
                        costUSD: $0.costUSD,
                        measuredOutputTokens: $0.measuredOutputTokens,
                        measuredDurationMS: $0.measuredDurationMS
                    )
                }
            )
        }
    }

    private func modelSeriesSource(for range: ATMMetricsRange) -> [ATMModelDayStats] {
        range == .today && !modelHourStats.isEmpty ? modelHourStats : modelDayStats
    }

    /// Session counts are summed rather than deduplicated: one session that spans
    /// two buckets is two data points here, which is what a per-bucket chart wants.
    private static func merged(_ points: [ATMUsageSeriesPoint]) -> [ATMUsageSeriesPoint] {
        var order: [String] = []
        var totals: [String: ATMUsageSeriesPoint] = [:]
        for point in points {
            let key = "\(point.date)\u{0}\(point.series)"
            guard let current = totals[key] else {
                order.append(key)
                totals[key] = point
                continue
            }
            totals[key] = ATMUsageSeriesPoint(
                date: point.date,
                series: point.series,
                sessions: current.sessions + point.sessions,
                inputTokens: current.inputTokens + point.inputTokens,
                outputTokens: current.outputTokens + point.outputTokens,
                cacheReadTokens: current.cacheReadTokens + point.cacheReadTokens,
                costUSD: current.costUSD + point.costUSD,
                measuredOutputTokens: current.measuredOutputTokens + point.measuredOutputTokens,
                measuredDurationMS: current.measuredDurationMS + point.measuredDurationMS
            )
        }
        return order.compactMap { totals[$0] }
    }

    /// The ranked list under the chart. Each dimension reads the narrowest source
    /// that actually measures it: models and clients from the model rows, projects
    /// from `atm stats`, which is the only per-project cost the snapshot carries.
    func breakdown(for range: ATMMetricsRange, dimension: ATMUsageDimension) -> [ATMUsageBreakdownRow] {
        let rows: [ATMUsageBreakdownRow]
        switch dimension {
        case .model:
            rows = sortedModelStats(for: range).map { model in
                ATMUsageBreakdownRow(
                    series: model.displayName,
                    label: model.model,
                    subtitle: model.client.isEmpty ? "" : ATMAgentDisplay.name(model.client),
                    sessions: model.sessions,
                    totalTokens: model.totalTokens,
                    cacheReadTokens: model.cacheReadTokens,
                    costUSD: model.costUSD,
                    costEstimated: model.costEstimated
                )
            }
        case .client:
            // Tokens come from the model rows; session counts come from the session
            // list, where each session appears once no matter how many models it used.
            // Series keys use the display name so chart filter and rows stay aligned.
            let sessionCounts = Dictionary(
                grouping: rangeData[range]?.sessions ?? [],
                by: { ATMAgentDisplay.name($0.agent) }
            ).mapValues(\.count)
            let grouped = Dictionary(
                grouping: rangeData[range]?.modelStats ?? [],
                by: { ATMAgentDisplay.name($0.client) }
            )
            rows = grouped.map { client, stats in
                ATMUsageBreakdownRow(
                    series: client,
                    label: client,
                    subtitle: "\(stats.count) 个模型",
                    sessions: sessionCounts[client] ?? 0,
                    totalTokens: stats.reduce(0) { $0 + $1.totalTokens },
                    cacheReadTokens: stats.reduce(0) { $0 + $1.cacheReadTokens },
                    costUSD: stats.reduce(0) { $0 + $1.costUSD },
                    costEstimated: stats.contains(where: \.costEstimated)
                )
            }
        case .project:
            let grouped = Dictionary(
                grouping: rangeData[range]?.projectStats ?? [],
                by: \.displayName
            )
            rows = grouped.map { project, stats in
                ATMUsageBreakdownRow(
                    series: project,
                    label: project,
                    subtitle: stats.map(\.agent).filter { !$0.isEmpty }.sorted()
                        .map { ATMAgentDisplay.name($0) }.joined(separator: " · "),
                    sessions: stats.reduce(0) { $0 + $1.sessions },
                    totalTokens: stats.reduce(0) { $0 + $1.totalTokens },
                    cacheReadTokens: stats.reduce(0) { $0 + $1.cacheReadTokens },
                    costUSD: stats.reduce(0) { $0 + $1.costUSD }
                )
            }
        }
        return rows
            .filter { $0.totalTokens > 0 }
            .sorted {
                if $0.totalTokens != $1.totalTokens { return $0.totalTokens > $1.totalTokens }
                return $0.label < $1.label
            }
    }

    func breakdownTokenTotal(for range: ATMMetricsRange, dimension: ATMUsageDimension) -> Int {
        breakdown(for: range, dimension: dimension).reduce(0) { $0 + $1.totalTokens }
    }

    func summary(for range: ATMMetricsRange) -> ATMRangeSummary {
        let values = stats(for: range)
        let fallbackSessions = range == .today ? values.last?.sessions ?? 0 : 0
        return ATMRangeSummary(
            sessions: rangeData[range]?.sessions.count ?? fallbackSessions,
            queries: values.reduce(0) { $0 + $1.queries },
            inputTokens: values.reduce(0) { $0 + $1.inputTokens },
            outputTokens: values.reduce(0) { $0 + $1.outputTokens },
            cacheReadTokens: values.reduce(0) { $0 + $1.cacheReadTokens },
            costUSD: values.reduce(0) { $0 + $1.costUSD }
        )
    }

    /// The same metric cards, narrowed to one series. Without this the totals above
    /// the chart would keep answering "everything" while the chart answered "atm".
    /// Reads the range sources rather than the per-bucket series so session counts
    /// stay distinct counts instead of a sum over buckets.
    func summary(
        for range: ATMMetricsRange,
        dimension: ATMUsageDimension?,
        series: String?
    ) -> ATMRangeSummary {
        guard let dimension, let series, !series.isEmpty else { return summary(for: range) }
        switch dimension {
        case .model:
            return Self.summarize(
                (rangeData[range]?.modelStats ?? []).filter { $0.displayName == series },
                sessions: nil
            )
        case .client:
            let sessions = (rangeData[range]?.sessions ?? [])
                .filter { ATMAgentDisplay.name($0.agent) == series }
                .count
            return Self.summarize(
                (rangeData[range]?.modelStats ?? [])
                    .filter { ATMAgentDisplay.name($0.client) == series },
                sessions: sessions
            )
        case .project:
            let stats = (rangeData[range]?.projectStats ?? []).filter { $0.displayName == series }
            return ATMRangeSummary(
                sessions: stats.reduce(0) { $0 + $1.sessions },
                queries: stats.reduce(0) { $0 + $1.queries },
                inputTokens: stats.reduce(0) { $0 + $1.inputTokens },
                outputTokens: stats.reduce(0) { $0 + $1.outputTokens },
                cacheReadTokens: stats.reduce(0) { $0 + $1.cacheReadTokens },
                costUSD: stats.reduce(0) { $0 + $1.costUSD }
            )
        }
    }

    /// The metric cards for the current lens. Unfiltered, a dimension leads with how
    /// many series it has; filtered, it drops that and shows what the single series
    /// actually reports.
    func usageMetrics(
        for range: ATMMetricsRange,
        lens: ATMUsageLens,
        series: String? = nil
    ) -> [ATMUsageMetric] {
        let dimension = lens.breakdown
        let scoped = series.flatMap { $0.isEmpty ? nil : $0 }
        let summary = summary(for: range, dimension: dimension, series: scoped)
        var metrics: [ATMUsageMetric] = []

        if let dimension, scoped == nil {
            metrics.append(.seriesCount(breakdown(for: range, dimension: dimension).count, dimension.title))
        }
        metrics.append(.tokens(summary.totalTokens))
        metrics.append(.output(summary.outputTokens))
        metrics.append(.cacheHitRate(summary.cacheHitRate))

        // Sessions are a distinct count for the whole range, for a client, and for a
        // project. Across models they are not: one session can use several models.
        if dimension != .model || scoped != nil {
            metrics.append(.sessions(summary.sessions))
        }
        if summary.queries > 0 {
            metrics.append(.queries(summary.queries))
        }
        metrics.append(.cost(summary.costUSD))
        metrics.append(contentsOf: speedMetrics(
            for: range,
            model: dimension == .model ? scoped : nil,
            client: dimension == .client ? scoped : nil
        ))
        return metrics
    }

    /// Speed cards, scoped to whatever the caller could scope. Model rows carry a
    /// client and a model, so those two narrow exactly; turn wait is per agent, so
    /// it only narrows by client. A project scope has no measurement of its own —
    /// requests are not attributed to projects in the speed rows — so it leaves
    /// both cards off rather than showing the whole range's number under a
    /// project's heading.
    private func speedMetrics(
        for range: ATMMetricsRange,
        model: String? = nil,
        client: String? = nil,
        project: String? = nil
    ) -> [ATMUsageMetric] {
        guard project == nil else { return [] }
        guard let speed = rangeData[range]?.speed else { return [] }
        var metrics: [ATMUsageMetric] = []
        let rate = speed.tokensPerSecond { row in
            if let model, row.model != model { return false }
            if let client, ATMAgentDisplay.name(row.client) != client { return false }
            return true
        }
        if let rate {
            metrics.append(.throughput(rate))
        }
        let wait = speed.turnWaitSeconds { row in
            guard let client else { return true }
            return ATMAgentDisplay.name(row.agent) == client
        }
        // A model scope says nothing about turns: one turn spans several models.
        if let wait, model == nil {
            metrics.append(.turnWait(wait))
        }
        return metrics
    }

    // MARK: - Cascaded multi-filters (模型 / 客户端 / 项目)

    /// Option lists for each filter, narrowed by the other two (cascade).
    func filterOptions(
        for range: ATMMetricsRange,
        dimension: ATMUsageDimension,
        filters: ATMUsageFilters
    ) -> [String] {
        switch dimension {
        case .model:
            let rows = (rangeData[range]?.modelStats ?? []).filter { row in
                if !filters.client.isEmpty && ATMAgentDisplay.name(row.client) != filters.client {
                    return false
                }
                // Project is not on model rows; client cascade is the link we have.
                return row.totalTokens > 0 && !row.model.isEmpty
            }
            let totals = Dictionary(grouping: rows, by: \.model)
                .mapValues { $0.reduce(0) { $0 + $1.totalTokens } }
            return totals.keys.sorted {
                if totals[$0, default: 0] != totals[$1, default: 0] {
                    return totals[$0, default: 0] > totals[$1, default: 0]
                }
                return $0 < $1
            }
        case .client:
            var totals: [String: Int] = [:]
            for row in rangeData[range]?.modelStats ?? [] where row.totalTokens > 0 {
                if !filters.model.isEmpty && row.model != filters.model { continue }
                let name = ATMAgentDisplay.name(row.client)
                totals[name, default: 0] += row.totalTokens
            }
            // Also surface clients that only appear on project rows for the
            // selected project (no model traffic in this range).
            if !filters.project.isEmpty {
                for row in rangeData[range]?.projectStats ?? [] where row.displayName == filters.project {
                    let name = ATMAgentDisplay.name(row.agent)
                    if totals[name] == nil, row.totalTokens > 0 {
                        totals[name] = row.totalTokens
                    }
                }
            }
            return totals.keys.sorted {
                if totals[$0, default: 0] != totals[$1, default: 0] {
                    return totals[$0, default: 0] > totals[$1, default: 0]
                }
                return $0 < $1
            }
        case .project:
            let rows = (rangeData[range]?.projectStats ?? []).filter { row in
                if !filters.client.isEmpty && ATMAgentDisplay.name(row.agent) != filters.client {
                    return false
                }
                return row.totalTokens > 0
            }
            let totals = Dictionary(grouping: rows, by: \.displayName)
                .mapValues { $0.reduce(0) { $0 + $1.totalTokens } }
            return totals.keys.sorted {
                if totals[$0, default: 0] != totals[$1, default: 0] {
                    return totals[$0, default: 0] > totals[$1, default: 0]
                }
                return $0 < $1
            }
        }
    }

    func filteredModelStats(for range: ATMMetricsRange, filters: ATMUsageFilters) -> [ATMModelStats] {
        (rangeData[range]?.modelStats ?? []).filter { row in
            if !filters.model.isEmpty && row.model != filters.model { return false }
            if !filters.client.isEmpty && ATMAgentDisplay.name(row.client) != filters.client {
                return false
            }
            return row.totalTokens > 0
        }
    }

    func filteredProjectStats(for range: ATMMetricsRange, filters: ATMUsageFilters) -> [ATMProjectStats] {
        (rangeData[range]?.projectStats ?? []).filter { row in
            if !filters.project.isEmpty && row.displayName != filters.project { return false }
            if !filters.client.isEmpty && ATMAgentDisplay.name(row.agent) != filters.client {
                return false
            }
            return row.totalTokens > 0
        }
    }

    /// Prefer project rollup when a project filter is set (model rows have no
    /// project); otherwise sum model rows filtered by model + client.
    func summary(for range: ATMMetricsRange, filters: ATMUsageFilters) -> ATMRangeSummary {
        if filters.isEmpty { return summary(for: range) }
        if !filters.project.isEmpty {
            let stats = filteredProjectStats(for: range, filters: filters)
            return ATMRangeSummary(
                sessions: stats.reduce(0) { $0 + $1.sessions },
                queries: stats.reduce(0) { $0 + $1.queries },
                inputTokens: stats.reduce(0) { $0 + $1.inputTokens },
                outputTokens: stats.reduce(0) { $0 + $1.outputTokens },
                cacheReadTokens: stats.reduce(0) { $0 + $1.cacheReadTokens },
                costUSD: stats.reduce(0) { $0 + $1.costUSD }
            )
        }
        let models = filteredModelStats(for: range, filters: filters)
        let sessions: Int?
        if !filters.client.isEmpty {
            sessions = (rangeData[range]?.sessions ?? [])
                .filter { ATMAgentDisplay.name($0.agent) == filters.client }
                .count
        } else if !filters.model.isEmpty {
            // One session can hit several models; leave sessions off the honest
            // path by passing nil so summarize does not invent a distinct count.
            sessions = nil
        } else {
            sessions = nil
        }
        return Self.summarize(models, sessions: sessions)
    }

    func usageMetrics(for range: ATMMetricsRange, filters: ATMUsageFilters) -> [ATMUsageMetric] {
        if filters.isEmpty {
            return usageMetrics(for: range, lens: .model)
        }
        let summary = summary(for: range, filters: filters)
        var metrics: [ATMUsageMetric] = [
            .tokens(summary.totalTokens),
            .output(summary.outputTokens),
            .cacheHitRate(summary.cacheHitRate),
        ]
        // Distinct session counts only when we have a client or project scope.
        if !filters.client.isEmpty || !filters.project.isEmpty {
            metrics.append(.sessions(summary.sessions))
        }
        if summary.queries > 0 {
            metrics.append(.queries(summary.queries))
        }
        metrics.append(.cost(summary.costUSD))
        metrics.append(contentsOf: speedMetrics(
            for: range,
            model: filters.model.isEmpty ? nil : filters.model,
            client: filters.client.isEmpty ? nil : filters.client,
            project: filters.project.isEmpty ? nil : filters.project
        ))
        return metrics
    }

    /// Chart series under multi-filters. Project filter drives project lines;
    /// otherwise model lines (optionally narrowed by model/client).
    func filteredLineTrendStats(
        for range: ATMMetricsRange,
        filters: ATMUsageFilters,
        topSeriesCount: Int = 5
    ) -> [ATMUsageSeriesPoint] {
        let dates = trendStats(for: range).map(\.date)
        let series = filteredLineTrendSeries(
            for: range,
            filters: filters,
            topSeriesCount: topSeriesCount
        )
        var source: [String: ATMUsageSeriesPoint] = [:]
        for point in filteredSeriesSource(for: range, filters: filters) {
            source["\(point.date)\u{0}\(point.series)"] = point
        }
        return dates.flatMap { date in
            series.map { name in
                source["\(date)\u{0}\(name)"] ?? ATMUsageSeriesPoint(
                    date: date,
                    series: name,
                    sessions: 0,
                    inputTokens: 0,
                    outputTokens: 0,
                    cacheReadTokens: 0,
                    costUSD: 0
                )
            }
        }
    }

    func filteredLineTrendSeries(
        for range: ATMMetricsRange,
        filters: ATMUsageFilters,
        topSeriesCount: Int = 5
    ) -> [String] {
        if !filters.project.isEmpty { return [filters.project] }
        if !filters.model.isEmpty { return [filters.model] }
        let names = filteredSeriesNames(for: range, filters: filters)
        if !filters.client.isEmpty { return Array(names.prefix(topSeriesCount)) }
        return Array(names.prefix(topSeriesCount))
    }

    func filteredSeriesNames(for range: ATMMetricsRange, filters: ATMUsageFilters) -> [String] {
        let dates = Set(trendStats(for: range).map(\.date))
        let totals = Dictionary(
            grouping: filteredSeriesSource(for: range, filters: filters)
                .filter { dates.contains($0.date) && $0.totalTokens > 0 },
            by: \.series
        ).mapValues { $0.reduce(0) { $0 + $1.totalTokens } }
        return totals.keys.sorted {
            if totals[$0, default: 0] != totals[$1, default: 0] {
                return totals[$0, default: 0] > totals[$1, default: 0]
            }
            return $0 < $1
        }
    }

    /// Ranked breakdown under multi-filters. Prefer project list when a project
    /// (or only project-shaped scope) is active; otherwise models.
    func filteredBreakdown(
        for range: ATMMetricsRange,
        filters: ATMUsageFilters
    ) -> (dimension: ATMUsageDimension, rows: [ATMUsageBreakdownRow]) {
        if !filters.project.isEmpty {
            let rows = filteredProjectStats(for: range, filters: filters).map { project in
                ATMUsageBreakdownRow(
                    series: project.displayName,
                    label: project.displayName,
                    subtitle: project.agent.isEmpty ? "" : ATMAgentDisplay.name(project.agent),
                    sessions: project.sessions,
                    totalTokens: project.totalTokens,
                    cacheReadTokens: project.cacheReadTokens,
                    costUSD: project.costUSD
                )
            }
            .filter { $0.totalTokens > 0 }
            .sorted {
                if $0.totalTokens != $1.totalTokens { return $0.totalTokens > $1.totalTokens }
                return $0.label < $1.label
            }
            // Merge same project across agents when client filter is empty.
            if filters.client.isEmpty {
                return (.project, Self.mergeBreakdownRows(rows))
            }
            return (.project, rows)
        }
        let rows = filteredModelStats(for: range, filters: filters).map { model in
            ATMUsageBreakdownRow(
                series: model.model,
                label: model.model,
                subtitle: model.client.isEmpty ? "" : ATMAgentDisplay.name(model.client),
                sessions: model.sessions,
                totalTokens: model.totalTokens,
                cacheReadTokens: model.cacheReadTokens,
                costUSD: model.costUSD,
                costEstimated: model.costEstimated
            )
        }
        .filter { $0.totalTokens > 0 }
        .sorted {
            if $0.totalTokens != $1.totalTokens { return $0.totalTokens > $1.totalTokens }
            return $0.label < $1.label
        }
        if filters.client.isEmpty && filters.model.isEmpty {
            return (.model, Self.mergeBreakdownRows(rows))
        }
        if filters.model.isEmpty {
            return (.model, Self.mergeBreakdownRows(rows))
        }
        return (.model, rows)
    }

    private static func mergeBreakdownRows(_ rows: [ATMUsageBreakdownRow]) -> [ATMUsageBreakdownRow] {
        var order: [String] = []
        var merged: [String: ATMUsageBreakdownRow] = [:]
        for row in rows {
            guard let current = merged[row.series] else {
                order.append(row.series)
                merged[row.series] = row
                continue
            }
            merged[row.series] = ATMUsageBreakdownRow(
                series: row.series,
                label: row.label,
                subtitle: "",
                sessions: current.sessions + row.sessions,
                totalTokens: current.totalTokens + row.totalTokens,
                cacheReadTokens: current.cacheReadTokens + row.cacheReadTokens,
                costUSD: current.costUSD + row.costUSD,
                costEstimated: current.costEstimated || row.costEstimated
            )
        }
        return order.compactMap { merged[$0] }.sorted {
            if $0.totalTokens != $1.totalTokens { return $0.totalTokens > $1.totalTokens }
            return $0.label < $1.label
        }
    }

    private func filteredSeriesSource(
        for range: ATMMetricsRange,
        filters: ATMUsageFilters
    ) -> [ATMUsageSeriesPoint] {
        if !filters.project.isEmpty {
            let source = range == .today && !projectHourStats.isEmpty ? projectHourStats : projectDayStats
            return Self.merged(
                source.compactMap { row -> ATMUsageSeriesPoint? in
                    if row.displayName != filters.project { return nil }
                    if !filters.client.isEmpty && ATMAgentDisplay.name(row.client) != filters.client {
                        return nil
                    }
                    return ATMUsageSeriesPoint(
                        date: row.date,
                        series: row.displayName,
                        sessions: row.sessions,
                        inputTokens: row.inputTokens,
                        outputTokens: row.outputTokens,
                        cacheReadTokens: row.cacheReadTokens,
                        costUSD: row.costUSD,
                        measuredOutputTokens: row.measuredOutputTokens,
                        measuredDurationMS: row.measuredDurationMS
                    )
                }
            )
        }
        return Self.merged(
            modelSeriesSource(for: range).compactMap { row -> ATMUsageSeriesPoint? in
                if !filters.model.isEmpty && row.model != filters.model { return nil }
                if !filters.client.isEmpty && ATMAgentDisplay.name(row.client) != filters.client {
                    return nil
                }
                return ATMUsageSeriesPoint(
                    date: row.date,
                    series: row.model,
                    sessions: row.sessions,
                    inputTokens: row.inputTokens,
                    outputTokens: row.outputTokens,
                    cacheReadTokens: row.cacheReadTokens,
                    costUSD: row.costUSD,
                    measuredOutputTokens: row.measuredOutputTokens,
                    measuredDurationMS: row.measuredDurationMS
                )
            }
        )
    }

    private static func summarize(_ stats: [ATMModelStats], sessions: Int?) -> ATMRangeSummary {
        ATMRangeSummary(
            sessions: sessions ?? stats.reduce(0) { $0 + $1.sessions },
            // Neither the model rows nor the client rows carry a query count.
            queries: 0,
            inputTokens: stats.reduce(0) { $0 + $1.inputTokens },
            outputTokens: stats.reduce(0) { $0 + $1.outputTokens },
            cacheReadTokens: stats.reduce(0) { $0 + $1.cacheReadTokens },
            costUSD: stats.reduce(0) { $0 + $1.costUSD }
        )
    }

    var menuBarTitle: String {
        guard refreshedAt != .distantPast, let todayStats else { return "" }
        return "\(work.working.count) · \(NumberFormat.compact(todayStats.totalTokens))"
    }

    var menuBarTooltip: String {
        let tokens = todayStats.map { NumberFormat.compact($0.totalTokens) } ?? "--"
        let base = "\(work.working.count) 项进行中 · 今日用量 \(tokens) · \(liveStatus.sessions.count) 个 Agent 会话 · \(work.summary.actionable) 项需处理"
        // Omit healthy / in-flight freshness — only mention index state when it needs attention.
        let freshness: String?
        switch indexHealth?.sync.status {
        case nil, "fresh", "syncing": freshness = nil
        case "stale": freshness = "索引过期"
        case "failed": freshness = "同步失败"
        case "missing", "never": freshness = "索引未就绪"
        default: freshness = "索引状态未知"
        }
        if let freshness {
            return "\(base) · \(freshness)"
        }
        return base
    }

    func skillStats(for range: ATMMetricsRange) -> [ATMSkillStats] {
        (rangeData[range]?.skillStats ?? []).sorted {
            if $0.calls != $1.calls { return $0.calls > $1.calls }
            return $0.skill < $1.skill
        }
    }

    func skillCallTotal(for range: ATMMetricsRange) -> Int {
        skillStats(for: range).reduce(0) { $0 + $1.calls }
    }
}

/// One rate-limit window reported by `atm quota --json`.
///
/// `resetsIn` is omitted by the CLI once `resetsAt` is in the past, which is
/// how we detect an expired window. The JSON keeps the stale `usedPercent` in
/// that case — `atm quota`'s text output zeroes it, so `displayPercent` does
/// the same rather than showing a percentage that no longer applies.
/// How fast a quota window is filling, computed by the CLI from its own sampled
/// history. Absent when there is not enough history to divide — which is not the
/// same as flat, so a fresh install shows the percentage alone rather than
/// claiming the quota is resting.
struct ATMQuotaTrend: Decodable, Equatable {
    let percentPerHour: Double
    let samples: Int
    let spanMinutes: Int
    let fullAt: Int64?
    let fullBeforeReset: Bool

    enum CodingKeys: String, CodingKey {
        case percentPerHour = "percent_per_hour"
        case samples
        case spanMinutes = "span_minutes"
        case fullAt = "full_at"
        case fullBeforeReset = "full_before_reset"
    }

    /// Matches quotaTrendFlatPercentPerHour in the CLI: below this the number is
    /// sampling jitter, and calling it "rising" would make a resting quota look
    /// like a problem.
    static let flatThreshold = 0.5

    var isFlat: Bool { abs(percentPerHour) < Self.flatThreshold }
    var isRising: Bool { percentPerHour >= Self.flatThreshold }

    /// A single glyph for the menu bar, where there is room for exactly one.
    var arrow: String? {
        if isFlat { return nil }
        return isRising ? "↑" : "↓"
    }

    var rateText: String {
        isFlat ? "持平" : String(format: "%+.1f%%/小时", percentPerHour)
    }
}

struct ATMQuotaWindow: Decodable, Equatable {
    let usedPercent: Double
    let windowMinutes: Int
    let resetsAt: Int64
    let resetsIn: String?
    let trend: ATMQuotaTrend?

    enum CodingKeys: String, CodingKey {
        case usedPercent = "used_percent"
        case windowMinutes = "window_minutes"
        case resetsAt = "resets_at"
        case resetsIn = "resets_in"
        case trend
    }

    var hasReset: Bool { resetsAt > 0 && resetsIn == nil }
    var displayPercent: Double { hasReset ? 0 : usedPercent }

    /// "1w" / "5h" / "30m", matching `formatQuotaWindow` in the CLI.
    var windowLabel: String {
        let minutes = windowMinutes
        guard minutes > 0 else { return "窗口" }
        if minutes % (7 * 24 * 60) == 0 { return "\(minutes / (7 * 24 * 60))w" }
        if minutes % (24 * 60) == 0 { return "\(minutes / (24 * 60))d" }
        if minutes % 60 == 0 { return "\(minutes / 60)h" }
        return "\(minutes)m"
    }

    var resetText: String {
        hasReset ? "已重置" : (resetsIn.map { "\($0) 后重置" } ?? "重置时间未知")
    }
}

/// One product's share of a shared credit pool (Grok bills Build / Chat /
/// Imagine against the same weekly window). Only present when live quota is on.
struct ATMQuotaProduct: Decodable, Equatable, Identifiable {
    let product: String
    let usedPercent: Double

    enum CodingKeys: String, CodingKey {
        case product
        case usedPercent = "used_percent"
    }

    var id: String { product }

    /// "GrokBuild" → "Build": the agent mark already says Grok.
    var displayName: String {
        product.hasPrefix("Grok") ? String(product.dropFirst(4)) : product
    }
}

/// One bounded metric supplied by an external quota provider. Providers keep
/// their credentials and service-specific API code outside ATM; the App only
/// needs values, bounds, and presentation metadata.
struct ATMProviderQuotaMetric: Decodable, Equatable, Identifiable {
    let id: String
    let label: String
    let used: Double
    let limit: Double
    let usedPercent: Double
    let unit: String?
    let currency: String?
    let precision: Int?

    enum CodingKeys: String, CodingKey {
        case id, label, used, limit, unit, currency, precision
        case usedPercent = "used_percent"
    }

    private func formatted(_ value: Double) -> String {
        let digits = max(0, min(precision ?? 0, 6))
        if digits == 0 { return NumberFormat.compact(Int(value.rounded())) }
        return String(format: "%.*f", digits, value)
    }

    var valueText: String {
        if let currency, !currency.isEmpty {
            let prefix = currency.uppercased() == "CNY" ? "¥" : "\(currency.uppercased()) "
            return "\(prefix)\(formatted(used)) / \(prefix)\(formatted(limit))"
        }
        let suffix = (unit?.isEmpty == false) ? " \(unit!)" : ""
        return "\(formatted(used)) / \(formatted(limit))\(suffix)"
    }
}

struct ATMProviderQuotaPayload: Decodable, Equatable, Identifiable {
    let id: String
    let provider: String
    let title: String
    let period: String?
    let observedAt: String
    let source: String?
    /// The page this reading came from, when the provider names one.
    let url: String?
    let metrics: [ATMProviderQuotaMetric]
    /// Set by ATM, never by the provider: the last card it returned, held in
    /// place with no reading behind it because the provider reported nothing or
    /// could not be reached. `unavailable_reason` is "empty" or "error".
    let unavailable: Bool?
    let unavailableReason: String?

    enum CodingKeys: String, CodingKey {
        case id, provider, title, period, source, url, metrics, unavailable
        case observedAt = "observed_at"
        case unavailableReason = "unavailable_reason"
    }

    /// Only http(s) reaches the browser. The CLI already rejects anything else,
    /// so this is the second gate — it also covers a hand-edited
    /// `quota_provider_cards.json`, where a `file://` or custom scheme would
    /// otherwise become a click that launches something.
    var linkURL: URL? {
        guard let raw = url?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty,
              let parsed = URL(string: raw),
              let scheme = parsed.scheme?.lowercased(), scheme == "http" || scheme == "https",
              parsed.host?.isEmpty == false
        else { return nil }
        return parsed
    }

    /// A card with no metrics counts too, so an ATM build that predates the flag
    /// still renders the empty state instead of a card with a title and nothing else.
    var isUnavailable: Bool { unavailable == true || metrics.isEmpty }

    var unavailableText: String {
        unavailableReason == "error" ? "读取失败" : "暂无数据"
    }

    /// Local wall clock — the raw timestamp is UTC, so slicing "HH:mm" out of it
    /// put a card observed at 22:48 Beijing time at 14:48. The date joins it once
    /// the observation is not from today: on a placeholder holding yesterday's
    /// card, a bare time reads as fresh.
    var observedTimeLabel: String {
        guard let date = ATMProviderQuotaPayload.parse(observedAt) else {
            guard let separator = observedAt.firstIndex(of: "T") else { return observedAt }
            return String(observedAt[observedAt.index(after: separator)...].prefix(5))
        }
        let sameDay = Calendar.current.isDateInToday(date)
        return (sameDay ? ATMProviderQuotaPayload.timeFormatter : ATMProviderQuotaPayload.dateTimeFormatter)
            .string(from: date)
    }

    private static let fractionalParser: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
    private static let parser = ISO8601DateFormatter()
    private static let timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "HH:mm"
        return formatter
    }()
    private static let dateTimeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "MM-dd HH:mm"
        return formatter
    }()

    private static func parse(_ value: String) -> Date? {
        fractionalParser.date(from: value) ?? parser.date(from: value)
    }
}

// Compatibility with ATM builds that emitted the former built-in daily_quota
// object. It is translated into the provider-neutral card model so
// upgrading the App restores an existing local observation immediately.
private struct ATMLegacyDailyCountQuota: Decodable, Equatable {
    let used: Double
    let limit: Double
    let usedPercent: Double

    enum CodingKeys: String, CodingKey {
        case used, limit
        case usedPercent = "used_percent"
    }
}

private struct ATMLegacyDailyAmountQuota: Decodable, Equatable {
    let used: Double
    let limit: Double
    let usedPercent: Double
    let currency: String

    enum CodingKeys: String, CodingKey {
        case used, limit, currency
        case usedPercent = "used_percent"
    }
}

private struct ATMLegacyDailyQuota: Decodable, Equatable {
    let cardTitle: String
    let day: String
    let count: ATMLegacyDailyCountQuota
    let amount: ATMLegacyDailyAmountQuota
    let observedAt: String
    let source: String?

    enum CodingKeys: String, CodingKey {
        case day, count, amount, source
        case cardTitle = "card_title"
        case observedAt = "observed_at"
    }

    func providerPayload(provider: String?) -> ATMProviderQuotaPayload {
        ATMProviderQuotaPayload(
            id: "legacy-daily-\(day)",
            provider: provider ?? "provider",
            title: cardTitle,
            period: "今日",
            observedAt: observedAt,
            source: source,
            url: nil,
            metrics: [
                ATMProviderQuotaMetric(
                    id: "count", label: "每日次数", used: count.used, limit: count.limit,
                    usedPercent: count.usedPercent, unit: "次", currency: nil, precision: 0
                ),
                ATMProviderQuotaMetric(
                    id: "amount", label: "每日金额", used: amount.used, limit: amount.limit,
                    usedPercent: amount.usedPercent, unit: nil, currency: amount.currency, precision: 2
                )
            ],
            unavailable: nil,
            unavailableReason: nil
        )
    }
}

struct ATMQuotaAgent: Decodable, Equatable {
    let plan: String?
    let primary: ATMQuotaWindow?
    let secondary: ATMQuotaWindow?
    let providerCards: [ATMProviderQuotaPayload]?
    private let provider: String?
    private let legacyDailyQuota: ATMLegacyDailyQuota?
    /// "log" / "live" / "cache"; absent from agents with a single source.
    let source: String?
    let products: [ATMQuotaProduct]?

    enum CodingKeys: String, CodingKey {
        case plan
        case primary
        case secondary
        case providerCards = "provider_cards"
        case provider
        case legacyDailyQuota = "daily_quota"
        case source
        case products
    }

    var windows: [ATMQuotaWindow] { [primary, secondary].compactMap { $0 } }

    var allProviderCards: [ATMProviderQuotaPayload] {
        var cards = providerCards ?? []
        if let legacyDailyQuota {
            cards.append(legacyDailyQuota.providerPayload(provider: provider))
        }
        return cards
    }
}

/// `atm quota --json` returns one entry per agent, keyed by agent name, and
/// the value is null when that agent has no rate-limit data in its logs.
struct ATMQuotaSnapshot: Decodable, Equatable {
    let agents: [String: ATMQuotaAgent]

    init(from decoder: Decoder) throws {
        let raw = try [String: ATMQuotaAgent?](from: decoder)
        agents = raw.compactMapValues { $0 }
    }

    init(agents: [String: ATMQuotaAgent]) {
        self.agents = agents
    }

    var isEmpty: Bool { cards.isEmpty && providerCards.isEmpty }

    /// Agents that actually reported a window, sorted for stable rendering.
    var entries: [(agent: String, quota: ATMQuotaAgent)] {
        agents
            .filter { !$0.value.windows.isEmpty }
            .sorted { $0.key < $1.key }
            .map { (agent: $0.key, quota: $0.value) }
    }

    /// The window closest to exhaustion, used for the at-a-glance readout.
    var tightestWindow: (agent: String, window: ATMQuotaWindow)? {
        cards
            .map { (agent: $0.agent, window: $0.window) }
            .max { $0.window.displayPercent < $1.window.displayPercent }
    }

    /// Flattened, uniquely identified cards for LazyVGrid. Nested ForEach with
    /// `id: \.offset` collides across agents (every primary is offset 0), so
    /// only one quota card would render.
    var cards: [ATMQuotaCard] {
        entries.flatMap { entry in
            entry.quota.windows.enumerated().map { index, window in
                ATMQuotaCard(
                    id: "\(entry.agent):\(index):\(window.windowMinutes)",
                    agent: entry.agent,
                    plan: entry.quota.plan,
                    window: window,
                    source: entry.quota.source,
                    // Products describe the shared pool, so pin them to the
                    // primary window's card instead of repeating per window.
                    products: index == 0 ? (entry.quota.products ?? []) : []
                )
            }
        }
    }

    var providerCards: [ATMProviderQuotaCard] {
        agents.flatMap { agent, quota in
            quota.allProviderCards.map { payload in
                ATMProviderQuotaCard(
                    id: "\(agent):\(payload.provider):\(payload.id)",
                    agent: agent,
                    payload: payload
                )
            }
        }
        .sorted { $0.id < $1.id }
    }

}

/// One quota tile on the usage page / quick panel.
struct ATMQuotaCard: Identifiable, Equatable {
    let id: String
    let agent: String
    let plan: String?
    let window: ATMQuotaWindow
    let source: String?
    let products: [ATMQuotaProduct]

    /// Short Chinese badge for the data source; nil hides the badge.
    var sourceLabel: String? {
        switch source {
        case "live": return "实时"
        case "cache": return "缓存"
        case "log": return "日志"
        default: return nil
        }
    }

    /// Config knobs this card offers behind its own gear icon.
    var settings: [ATMQuotaCardSetting] {
        ATMQuotaCardSetting.settings(for: agent)
    }
}

struct ATMProviderQuotaCard: Identifiable, Equatable {
    let id: String
    let agent: String
    let payload: ATMProviderQuotaPayload

    var providerLabel: String {
        let provider = payload.provider.trimmingCharacters(in: .whitespacesAndNewlines)
        return provider == provider.lowercased() ? provider.capitalized : provider
    }

    var sourceLabel: String? {
        switch payload.source {
        case "browser": return "浏览器"
        case "live": return "实时"
        case "cache": return "缓存"
        case "local": return "本地"
        default: return payload.source
        }
    }
}

/// A config knob reachable from a quota card's gear popover.
///
/// The switch belongs on the card it changes: in the 配额 header a Grok-only
/// toggle read as if it applied to every agent. Adding another knob means one
/// case here plus its read / write wiring where the popover is built.
enum ATMQuotaCardSetting: String, Identifiable, CaseIterable {
    case grokLiveQuota

    /// Cards with no knobs draw no gear at all.
    static func settings(for agent: String) -> [ATMQuotaCardSetting] {
        switch ATMAgentDisplay.key(agent) {
        case "grokbuild":
            return [.grokLiveQuota]
        default:
            return []
        }
    }

    var id: String { rawValue }

    var title: String {
        switch self {
        case .grokLiveQuota:
            return "实时额度"
        }
    }

    var detail: String {
        switch self {
        case .grokLiveQuota:
            return "开启后使用本机 ~/.grok/auth.json 访问 Grok 账单接口，获取实时额度和分产品占用；关闭则仅读取本地日志。失败时自动回退到日志 / 短缓存。若设置了 ATM_GROK_LIVE_QUOTA 环境变量，以环境变量为准。"
        }
    }
}

/// Display names and marks for agent / client IDs.
///
/// Known clients get a drawn brand-style mark (`ATMAgentMark`) plus an SF Symbol
/// fallback for menus / Labels. Marks are for local ATM UI, not redistributed
/// as standalone brand kits.
enum ATMAgentDisplay {
    /// Lowercased, trimmed id used for switch matching. Also accepts already
    /// pretty-printed names from the usage filters ("Grok", "QoderCLI").
    static func key(_ agent: String) -> String {
        let trimmed = agent.trimmingCharacters(in: .whitespacesAndNewlines)
        let lowered = trimmed.lowercased()
        switch lowered {
        case "grok": return "grokbuild"
        case "qoder cli": return "qodercli"
        case "qoder work": return "qoderwork"
        default: return lowered
        }
    }

    static func name(_ agent: String) -> String {
        switch key(agent) {
        case "claude": return "Claude"
        case "codex": return "Codex"
        case "pi": return "Pi"
        case "copilot": return "Copilot"
        case "cursor": return "Cursor"
        case "qoder": return "Qoder"
        case "qodercli": return "QoderCLI"
        case "qoderwork": return "QoderWork"
        case "grokbuild": return "Grok"
        case "": return "未知客户端"
        default:
            // Preserve multi-word IDs like "MyAgent" when we have no mapping.
            if agent == agent.lowercased() { return agent.capitalized }
            return agent.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

    /// The client label a session should carry: its own `client` when the tool
    /// reported one ("Codex Desktop"), otherwise the pretty tool name ("Codex").
    static func clientName(_ session: ATMLiveSession) -> String {
        let client = session.client?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return client.isEmpty ? name(session.tool) : client
    }

    static func projectName(_ session: ATMLiveSession) -> String {
        let project = session.project.trimmingCharacters(in: .whitespacesAndNewlines)
        return project.isEmpty ? "未知项目" : project
    }

    /// One- or two-character badge glyph. Prefer over a generic SF Symbol when
    /// the mark must stay readable at 14–18pt in dense lists.
    static func monogram(_ agent: String) -> String {
        switch key(agent) {
        case "claude": return "C"
        case "codex": return "X"
        case "pi": return "π"
        case "copilot": return "Co"
        case "cursor": return "Cu"
        case "qoder", "qodercli", "qoderwork": return "Q"
        case "grokbuild": return "G"
        case "": return "?"
        default:
            let label = name(agent)
            guard let first = label.first else { return "?" }
            return String(first).uppercased()
        }
    }

    /// SF Symbol used when a monogram badge is impractical (menus, Labels).
    static func systemImage(_ agent: String) -> String {
        switch key(agent) {
        case "claude": return "text.bubble.fill"
        case "codex": return "chevron.left.forwardslash.chevron.right"
        case "pi": return "function"
        case "copilot": return "airplane"
        case "cursor": return "cursorarrow.click.2"
        case "qoder", "qodercli", "qoderwork": return "q.circle.fill"
        case "grokbuild": return "bolt.fill"
        case "": return "questionmark.circle"
        default: return "cpu"
        }
    }
}

extension ATMQuotaSnapshot {
    /// Shown in the menu bar only once any built-in window or provider metric
    /// crosses the warning threshold. The most exhausted source wins.
    var menuBarSuffix: String? {
        var candidates = cards.map { card in
            (
                percent: card.window.displayPercent,
                arrow: card.window.trend?.arrow ?? ""
            )
        }
        candidates.append(contentsOf: providerCards.flatMap { card in
            card.payload.metrics.map { (percent: $0.usedPercent, arrow: "") }
        })
        guard let tightest = candidates.max(by: { $0.percent < $1.percent }) else { return nil }
        let percent = tightest.percent
        guard ATMQuotaLevel.level(forPercent: percent) != .healthy else { return nil }
        return String(format: "%.0f%%", percent) + tightest.arrow
    }

    /// The tooltip has no layout cost, so every window and provider metric appears.
    var tooltipText: String? {
        var parts = cards.map { card -> String in
            var text = "\(ATMAgentDisplay.name(card.agent)) \(card.window.windowLabel) "
                + "\(String(format: "%.0f", card.window.displayPercent))%"
            if let trend = card.window.trend, !trend.isFlat {
                text += " \(trend.rateText)"
            }
            return text
        }
        for card in providerCards {
            for metric in card.payload.metrics {
                parts.append(
                    "\(ATMAgentDisplay.name(card.agent)) \(card.providerLabel) "
                        + "\(metric.label) \(String(format: "%.0f", metric.usedPercent))%"
                )
            }
        }
        guard !parts.isEmpty else { return nil }
        return "配额 " + parts.joined(separator: " / ")
    }
}

enum ATMQuotaLevel {
    case healthy
    case warning
    case critical

    static func level(forPercent percent: Double) -> ATMQuotaLevel {
        if percent >= 90 { return .critical }
        if percent >= 75 { return .warning }
        return .healthy
    }
}

enum NumberFormat {
    static func compact(_ value: Int) -> String {
        if value >= 1_000_000_000 { return String(format: "%.1fB", Double(value) / 1_000_000_000) }
        if value >= 1_000_000 { return String(format: "%.1fM", Double(value) / 1_000_000) }
        if value >= 1_000 { return String(format: "%.1fK", Double(value) / 1_000) }
        return "\(value)"
    }

    static func percent(_ value: Double) -> String {
        String(format: "%.0f%%", value * 100)
    }

    static func currency(_ value: Double) -> String {
        if value >= 1_000 { return String(format: "$%.1fK", value / 1_000) }
        if value >= 100 { return String(format: "$%.0f", value) }
        if value >= 10 { return String(format: "$%.1f", value) }
        return String(format: "$%.2f", value)
    }

    static func age(_ seconds: Int) -> String {
        if seconds < 60 { return "刚刚" }
        if seconds < 3_600 { return "\(seconds / 60) 分钟" }
        if seconds < 86_400 { return "\(seconds / 3_600) 小时" }
        return "\(seconds / 86_400) 天"
    }

    /// A measured span, not an age: sub-minute waits keep their seconds because the
    /// difference between 8s and 40s is the thing being watched.
    static func duration(_ seconds: Double) -> String {
        if seconds < 1 { return String(format: "%.1fs", seconds) }
        if seconds < 60 { return String(format: "%.0fs", seconds) }
        let whole = Int(seconds.rounded())
        if whole < 3_600 {
            let minutes = whole / 60
            let remainder = whole % 60
            return remainder == 0 ? "\(minutes)m" : "\(minutes)m\(remainder)s"
        }
        let hours = whole / 3_600
        let minutes = (whole % 3_600) / 60
        return minutes == 0 ? "\(hours)h" : "\(hours)h\(minutes)m"
    }
}
