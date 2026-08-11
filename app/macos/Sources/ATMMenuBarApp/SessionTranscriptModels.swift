import Foundation

/// A session row from the durable index (`atm session list`), as opposed to
/// `ATMLiveSession`, which only exists while the session is inside the
/// live-status window. The index is the only way to reach a session that has
/// scrolled out of recent activity.
struct ATMIndexedSession: Decodable, Identifiable, Equatable {
    let id: String
    let shortID: String
    let agent: String
    let project: String
    let createdAt: String
    let lastAt: String?
    let qCount: Int
    let summary: String?
    let firstQuestion: String?

    enum CodingKeys: String, CodingKey {
        case id
        case shortID = "short_id"
        case agent
        case project
        case createdAt = "created_at"
        case lastAt = "last_at"
        case qCount = "q_count"
        case summary
        case firstQuestion = "first_q"
    }

    /// What to call the session in a list. The stored summary is the agent's own
    /// title; the opening question is the fallback, and the short id is the last
    /// resort so a row is never blank.
    var title: String {
        for candidate in [summary, firstQuestion] {
            if let value = candidate?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty {
                return value
            }
        }
        return shortID
    }

    var startedAt: Date? { ATMIndexedSession.parse(createdAt) }
    var endedAt: Date? { lastAt.flatMap(ATMIndexedSession.parse) }

    private static func parse(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        if let date = formatter.date(from: value) { return date }
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: value)
    }
}

/// One turn of `atm session show --json`. `thinking` is only ever present when
/// the caller asked for it and the agent's transcript actually stores the text.
struct ATMSessionTurn: Decodable, Identifiable, Equatable {
    let turn: Int
    let question: String?
    let answer: String?
    let thinking: String?

    var id: Int { turn }

    enum CodingKeys: String, CodingKey {
        case turn
        case question = "q"
        case answer = "a"
        case thinking
    }
}

/// The `atm session show --json` payload.
///
/// `thinkingSourceMissing` and `thinkingAbsent` are different facts and the view
/// must not merge them: the first means the agent rotated its transcript away,
/// the second that this agent records no thinking text at all (Claude Code keeps
/// only the signature). Both would otherwise render as an empty pane.
struct ATMSessionTranscript: Decodable, Equatable {
    let id: String
    let agent: String
    let project: String
    let turns: [ATMSessionTurn]
    let tools: [String: Int]
    let totalTurns: Int
    let returnedTurns: Int
    let truncated: Bool
    let thinkingSourceMissing: Bool
    let thinkingAbsent: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case agent
        case project
        case turns = "qa"
        case tools
        case totalTurns = "total_turns"
        case returnedTurns = "returned_turns"
        case truncated
        case thinkingSourceMissing = "thinking_source_missing"
        case thinkingAbsent = "thinking_absent"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decodeIfPresent(String.self, forKey: .id) ?? ""
        agent = try values.decodeIfPresent(String.self, forKey: .agent) ?? ""
        project = try values.decodeIfPresent(String.self, forKey: .project) ?? ""
        turns = try values.decodeIfPresent([ATMSessionTurn].self, forKey: .turns) ?? []
        tools = try values.decodeIfPresent([String: Int].self, forKey: .tools) ?? [:]
        totalTurns = try values.decodeIfPresent(Int.self, forKey: .totalTurns) ?? 0
        returnedTurns = try values.decodeIfPresent(Int.self, forKey: .returnedTurns) ?? 0
        truncated = try values.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
        thinkingSourceMissing = try values.decodeIfPresent(Bool.self, forKey: .thinkingSourceMissing) ?? false
        thinkingAbsent = try values.decodeIfPresent(Bool.self, forKey: .thinkingAbsent) ?? false
    }
}

/// One entry of `atm session timeline --json`: either a message or a model
/// request. Requests are what token and cost accounting is built from, and
/// seeing them interleaved with the messages is the only way to tell which turn
/// spent what.
struct ATMSessionTimelineEntry: Decodable, Identifiable, Equatable {
    let kind: String
    let role: String?
    let content: String?
    let model: String?
    let timestamp: Int64
    let inputTokens: Int64?
    let outputTokens: Int64?
    let cacheTokens: Int64?
    let costUSD: Double?

    /// Index-free identity: several requests can share a timestamp, so the
    /// position is folded in by the caller when building the list.
    var id: String {
        [kind, role ?? "", model ?? "", String(timestamp), String(content?.count ?? 0)]
            .joined(separator: "|")
    }

    var isMessage: Bool { kind == "message" }
    var date: Date { Date(timeIntervalSince1970: TimeInterval(timestamp)) }

    enum CodingKeys: String, CodingKey {
        case kind
        case role
        case content
        case model
        case timestamp = "ts"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case cacheTokens = "cache_tokens"
        case costUSD = "cost_usd"
    }
}

/// The three ways to read one session, from cheapest to most complete. They are
/// separate requests rather than one payload the view filters: the full read
/// parses the agent's raw transcript from disk, which is the expensive part and
/// pointless for a reader who only wants the outcome.
enum ATMSessionReadMode: String, CaseIterable, Identifiable {
    /// Just the tail of the conversation — what happened, without the machinery.
    case brief
    /// Messages and model requests in time order, with per-request spend.
    case timeline
    /// Every turn, plus the thinking chain when the transcript keeps it.
    case full

    var id: String { rawValue }

    var title: String {
        switch self {
        case .brief: return "摘要"
        case .timeline: return "时序"
        case .full: return "完整"
        }
    }

    var help: String {
        switch self {
        case .brief: return "最近几轮问答"
        case .timeline: return "消息与模型请求按时间交错，含每次请求的用量"
        case .full: return "全部轮次，并尽可能包含思考过程"
        }
    }

    /// The brief read is a tail, not a sample: a reader who opens a session wants
    /// its outcome, and the last turns are where it is.
    static let briefTurnCount = 4
    static let briefMaxChars = 6000

    func arguments(sessionID: String) -> [String] {
        switch self {
        case .brief:
            return [
                "session", "show", sessionID,
                "--last", String(Self.briefTurnCount),
                "--max-chars", String(Self.briefMaxChars),
                "--json",
            ]
        case .timeline:
            return ["session", "timeline", sessionID, "--json"]
        case .full:
            return ["session", "show", sessionID, "--thinking", "--json"]
        }
    }
}
