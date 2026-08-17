import Foundation

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
    let autoDispatch: Bool?
    let enabled: Bool
    let createdAt: Int64
    let updatedAt: Int64

    enum CodingKeys: String, CodingKey {
        case id, connector, kind, name, project, priority, enabled, strategy, instruction
        case autoDispatch = "auto_dispatch"
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
    var automaticallyDispatches: Bool { effectiveStrategy == "tasks" && autoDispatch == true }

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

/// Keeps the App's manual action on the same source-scoped CLI path as the
/// scheduler. A source ID is mandatory here: the Collection workspace no
/// longer exposes the old "run every source" action.
enum ATMCollectionRunCommand {
    static func arguments(sourceID: String) -> [String] {
        ["collect", "run", "--source", sourceID, "--json"]
    }
}

/// Which failure the Collection workspace still has to announce itself. Source
/// level failures already have a home in that source's 采集状态 card, while
/// 添加/删除来源、修正、撤销、生成知识文档 have no source to hang on and would
/// otherwise fail silently in the very workspace that triggered them.
enum ATMCollectionWorkspaceNotice {
    static func message(shared: String?, sourceErrors: [String: String]) -> String? {
        guard let shared = shared?.trimmingCharacters(in: .whitespacesAndNewlines),
              !shared.isEmpty,
              !sourceErrors.values.contains(shared) else { return nil }
        return shared
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
    /// Empty while an insight only lives in the Collection workspace's
    /// conclusion. Set after the user explicitly saves it as knowledge.
    let knowledgeDocumentID: String?
    let knowledgeCollection: String?
    let todoID: String?
    let status: String
    /// How many times this batch has been tried. Absent from older CLI output,
    /// which is read as zero: a fresh budget is the safe reading, because it
    /// describes an item the next run will pick up rather than one already
    /// retired.
    let attempts: Int?
    let dispatchStatus: String?
    let dispatchError: String?
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
        case dispatchStatus = "dispatch_status"
        case dispatchError = "dispatch_error"
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
        case knowledgeDocumentID = "knowledge_document_id"
        case knowledgeCollection = "knowledge_collection"
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
    /// or wants filed. An unsaved insight still needs the user's decision, so it
    /// stays visible until its conclusion has explicitly been saved to knowledge.
    /// A record whose Todo is already closed is no longer work either: keeping it
    /// up here turned the workspace into a history feed, where twelve of twenty
    /// rows wanted nothing.
    var shouldCollapseInCollection: Bool {
        action == "ignore"
            || (action == "insight" && knowledgeDocumentID?.isEmpty == false)
            || todoClosed
    }
}

/// Collection's middle column is one row per filed Todo, not one row per write.
/// A create is the durable headline; later append decisions remain audit records
/// but are presented inside that create's detail. An append without a matching
/// create stays visible so older or externally-created Todos never disappear.
enum ATMCollectionItemGrouping {
    static func visibleItems(_ items: [ATMCollectionItem]) -> [ATMCollectionItem] {
        let createdTodoIDs = Set(items.compactMap { item -> String? in
            guard item.action == "create", let todoID = item.todoID, !todoID.isEmpty else { return nil }
            return todoID
        })
        return items.filter { item in
            guard item.action == "append", let todoID = item.todoID else { return true }
            return !createdTodoIDs.contains(todoID)
        }
    }

    static func supplements(
        for item: ATMCollectionItem,
        in items: [ATMCollectionItem]
    ) -> [ATMCollectionItem] {
        guard item.action == "create", let todoID = item.todoID, !todoID.isEmpty else { return [] }
        return items
            .filter { $0.action == "append" && $0.todoID == todoID }
            .sorted {
                let lhs = $0.occurredAt ?? $0.createdAt
                let rhs = $1.occurredAt ?? $1.createdAt
                return lhs == rhs ? $0.id < $1.id : lhs < rhs
            }
    }
}

/// 一条处理记录的原始聊天，解析成能当聊天渲染的样子。collector 写出的每一行是
/// `[新消息] 2026-08-06 15:04:05 [张三] 内容`（见 formatMessageContext），按需分析没有
/// 标记前缀。三件事决定了这个解析器的形状：消息正文自己可能换行，所以认不出头部的行
/// 算上一条的续行；[新消息] 不一定连续（被关键词排除的消息会夹在中间当上下文），所以
/// 分界线只认第一条新消息的位置，不假设新旧各成一段；格式完全认不出来时退回原文，
/// 显示得糙一点也比丢内容好。
struct ATMCollectionTranscript: Equatable {
    /// 连续、同一发送者、同一新旧归属的消息合成一块，头部只出现一次——逐行重复
    /// 「谁在几点几分几秒说」是这段原文里最多的冗余。
    struct Block: Identifiable, Equatable {
        let id: Int
        let sender: String
        /// 已经按需带上日期：开头那块和跨天的那块是 `08-06 15:04`，其余只留 `15:04`。
        let time: String
        let isFresh: Bool
        let lines: [String]
        /// 这一块之前要不要画「新消息」分界线。整段都是新消息时没有分界线可画。
        let startsFresh: Bool
    }

    let blocks: [Block]
    /// 一行都没解析出来时的原文兜底。
    let fallback: String?

    /// 消息条数（不是块数）：并块是显示手段，条数才是「这段聊了多少」。
    var messageCount: Int {
        blocks.reduce(0) { $0 + $1.lines.count }
    }

    static let empty = ATMCollectionTranscript(blocks: [], fallback: nil)

    static func parse(_ raw: String?) -> ATMCollectionTranscript {
        let text = raw ?? ""
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return .empty }

        var messages: [ParsedMessage] = []
        for rawLine in text.split(separator: "\n", omittingEmptySubsequences: false) {
            let line = String(rawLine)
            if let message = ParsedMessage(line: line) {
                messages.append(message)
            } else if var last = messages.popLast() {
                last.text += "\n" + line
                messages.append(last)
            } else {
                // 开头就不是消息行：这不是 collector 写的格式，整段原样交出去。
                return ATMCollectionTranscript(blocks: [], fallback: text)
            }
        }
        guard !messages.isEmpty else { return ATMCollectionTranscript(blocks: [], fallback: text) }
        return ATMCollectionTranscript(blocks: blocks(from: messages), fallback: nil)
    }

    private static func blocks(from messages: [ParsedMessage]) -> [Block] {
        var blocks: [Block] = []
        var current: [ParsedMessage] = []
        var previousDate = ""
        var seenFresh = false

        func flush() {
            guard let first = current.first else { return }
            let time = first.date == previousDate
                ? first.time
                : "\(first.date.suffix(5)) \(first.time)"
            previousDate = first.date
            // 一块内部新旧一致（分组时就在新旧变化处断开），所以分界线只画在第一块
            // 新消息前面，而不是每遇到一条新消息就画一条。
            let startsFresh = !blocks.isEmpty && !seenFresh && first.isFresh
            seenFresh = seenFresh || first.isFresh
            blocks.append(
                Block(
                    id: blocks.count,
                    sender: first.sender,
                    time: time,
                    isFresh: first.isFresh,
                    lines: current.map { $0.text.trimmedTrailingWhitespace },
                    startsFresh: startsFresh
                )
            )
            current = []
        }

        for message in messages {
            if let previous = current.last,
               previous.sender != message.sender
                || previous.isFresh != message.isFresh
                || previous.date != message.date {
                flush()
            }
            current.append(message)
        }
        flush()
        return blocks
    }

    /// 一行解析出来的消息。`isFresh` 在没有标记前缀时为 true：按需分析里每一行都参与了
    /// 判断，没有「上面这些只是背景」这回事，也就没有分界线。
    private struct ParsedMessage {
        let isFresh: Bool
        let date: String
        let time: String
        let sender: String
        var text: String

        init?(line: String) {
            var rest = Substring(line)
            var isFresh = true
            if rest.hasPrefix(ATMCollectionTranscript.freshMarker) {
                rest = rest.dropFirst(ATMCollectionTranscript.freshMarker.count)
            } else if rest.hasPrefix(ATMCollectionTranscript.contextMarker) {
                isFresh = false
                rest = rest.dropFirst(ATMCollectionTranscript.contextMarker.count)
            }
            guard let stamp = ATMCollectionTranscript.leadingStamp(rest) else { return nil }
            rest = rest.dropFirst(stamp.count)
            guard rest.hasPrefix(" [") else { return nil }
            rest = rest.dropFirst(2)
            guard let close = rest.firstIndex(of: "]") else { return nil }
            self.isFresh = isFresh
            self.date = String(stamp.prefix(10))
            self.time = String(stamp.dropFirst(11).prefix(5))
            self.sender = String(rest[rest.startIndex..<close])
            var content = rest[close...].dropFirst()
            if content.hasPrefix(" ") { content = content.dropFirst() }
            self.text = String(content)
        }
    }

    private static let freshMarker = "[新消息] "
    private static let contextMarker = "[上下文] "

    /// `2006-01-02 15:04:05`，正好 19 个字符。按位校验而不是上正则：这段每条记录都要跑，
    /// 而且要认的只有 collector 自己写出来的这一种格式。
    private static func leadingStamp(_ text: Substring) -> Substring? {
        guard text.count >= 19 else { return nil }
        let stamp = text.prefix(19)
        let digits = [0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15, 17, 18]
        let dashes = [4, 7]
        let colons = [13, 16]
        for (offset, character) in stamp.enumerated() {
            if digits.contains(offset), !character.isNumber { return nil }
            if dashes.contains(offset), character != "-" { return nil }
            if colons.contains(offset), character != ":" { return nil }
            if offset == 10, character != " " { return nil }
        }
        return stamp
    }
}

private extension String {
    var trimmedTrailingWhitespace: String {
        var text = self
        while let last = text.last, last.isWhitespace || last.isNewline {
            text.removeLast()
        }
        return text
    }
}

struct ATMCollectionOverview: Decodable, Equatable {
    let enabled: Bool
    let intervalMinutes: Int
    let lookbackMinutes: Int
    let model: String
    let connectorHealth: [ATMCollectionConnectorHealth]
    let summary: ATMCollectionSummary
    let sources: [ATMCollectionSource]
    let runs: [ATMCollectionRun]
    let items: [ATMCollectionItem]
    let digests: [ATMCollectionDigest]

    enum CodingKeys: String, CodingKey {
        case enabled, model, summary, sources, runs, items, digests
        case intervalMinutes = "interval_minutes"
        case lookbackMinutes = "lookback_minutes"
        case connectorHealth = "connector_health"
    }

    static let empty = ATMCollectionOverview(
        enabled: false, intervalMinutes: 5, lookbackMinutes: 60,
        model: "deepseek-v4-flash", connectorHealth: [], summary: .empty,
        sources: [], runs: [], items: [], digests: []
    )

    var latestRun: ATMCollectionRun? { runs.max { $0.startedAt < $1.startedAt } }

    func latestRun(for sourceID: String) -> ATMCollectionRun? {
        runs
            .filter { $0.sourceID == sourceID }
            .max { lhs, rhs in
                if lhs.startedAt == rhs.startedAt { return lhs.id < rhs.id }
                return lhs.startedAt < rhs.startedAt
            }
    }

    func latestSuccessfulRun(for sourceID: String) -> ATMCollectionRun? {
        runs
            .filter { $0.sourceID == sourceID && $0.status == "succeeded" }
            .max { lhs, rhs in
                if lhs.startedAt == rhs.startedAt { return lhs.id < rhs.id }
                return lhs.startedAt < rhs.startedAt
            }
    }
}
