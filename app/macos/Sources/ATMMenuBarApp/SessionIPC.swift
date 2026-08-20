import Foundation

/// Typed desktop reads for indexed sessions. The public `session` CLI remains
/// available to Agents and humans; the App sends read intent without CLI argv.
enum ATMSessionIPCCommand {
    static let list = ATMIPCMethod<ATMSessionListRequest, ATMSessionListResponse>(
        "session.list",
        responseKeyDecoding: .useDefault
    )
    static let search = ATMIPCMethod<ATMSessionSearchRequest, ATMSessionSearchResult>(
        "session.search",
        responseKeyDecoding: .useDefault
    )
    static let show = ATMIPCMethod<ATMSessionShowRequest, ATMSessionTranscript>(
        "session.show",
        responseKeyDecoding: .useDefault
    )
    static let timeline = ATMIPCMethod<ATMSessionTimelineRequest, ATMSessionTimelineResponse>(
        "session.timeline",
        responseKeyDecoding: .useDefault
    )
}

struct ATMSessionListRequest: Encodable, Equatable {
    let agent: String?
    let project: String?
    let days: Int
    let since: String?
    let review: String
    let includeAll: Bool
    let order: String
    let limit: Int
    let offset: Int
    let syncBeforeRead: Bool

    enum CodingKeys: String, CodingKey {
        case agent, project, days, since, review, order, limit, offset
        case includeAll = "all"
        case syncBeforeRead = "sync_before_read"
    }

    init(
        agent: String? = nil,
        project: String? = nil,
        days: Int = 1,
        since: String? = nil,
        review: String = "all",
        includeAll: Bool = false,
        order: String = "asc",
        limit: Int = 0,
        offset: Int = 0,
        syncBeforeRead: Bool = false
    ) {
        self.agent = agent
        self.project = project
        self.days = days
        self.since = since
        self.review = review
        self.includeAll = includeAll
        self.order = order
        self.limit = limit
        self.offset = offset
        self.syncBeforeRead = syncBeforeRead
    }
}

struct ATMSessionListResponse: Decodable, Equatable {
    let sessions: [ATMIndexedSession]
    let total: Int
}

struct ATMSessionSearchRequest: Encodable, Equatable {
    let keyword: String
    let agent: String?
    let project: String?
    let since: String?
    let days: Int
    let role: String?
    let limit: Int
    let snippet: Int
    let syncBeforeRead: Bool

    enum CodingKeys: String, CodingKey {
        case keyword, agent, project, since, days, role, limit, snippet
        case syncBeforeRead = "sync_before_read"
    }

    init(
        keyword: String,
        agent: String? = nil,
        project: String? = nil,
        since: String? = nil,
        days: Int = 0,
        role: String? = nil,
        limit: Int = 50,
        snippet: Int = 400,
        syncBeforeRead: Bool = false
    ) {
        self.keyword = keyword
        self.agent = agent
        self.project = project
        self.since = since
        self.days = days
        self.role = role
        self.limit = limit
        self.snippet = snippet
        self.syncBeforeRead = syncBeforeRead
    }
}

struct ATMSessionShowRequest: Encodable, Equatable {
    let sessionID: String
    let includeThinking: Bool
    let turns: String?
    let last: Int
    let maxChars: Int
    let syncBeforeRead: Bool

    enum CodingKeys: String, CodingKey {
        case turns, last
        case sessionID = "session_id"
        case includeThinking = "include_thinking"
        case maxChars = "max_chars"
        case syncBeforeRead = "sync_before_read"
    }

    init(
        sessionID: String,
        includeThinking: Bool = false,
        turns: String? = nil,
        last: Int = 0,
        maxChars: Int = 0,
        syncBeforeRead: Bool = false
    ) {
        self.sessionID = sessionID
        self.includeThinking = includeThinking
        self.turns = turns
        self.last = last
        self.maxChars = maxChars
        self.syncBeforeRead = syncBeforeRead
    }
}

struct ATMSessionTimelineRequest: Encodable, Equatable {
    let sessionID: String
    let syncBeforeRead: Bool

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case syncBeforeRead = "sync_before_read"
    }

    init(sessionID: String, syncBeforeRead: Bool = false) {
        self.sessionID = sessionID
        self.syncBeforeRead = syncBeforeRead
    }
}

struct ATMSessionTimelineResponse: Decodable, Equatable {
    let events: [ATMSessionTimelineEntry]

    private enum CodingKeys: String, CodingKey { case events }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        events = try values.decodeIfPresent([ATMSessionTimelineEntry].self, forKey: .events) ?? []
    }
}

struct ATMSessionIPCClient: Sendable {
    private let ipc: ATMIPCClient

    init(runner: ATMCommandRunner) {
        ipc = ATMIPCClient(runner: runner)
    }

    init() throws {
        ipc = try ATMIPCClient()
    }

    func list(_ request: ATMSessionListRequest) async throws -> ATMSessionListResponse {
        try await ipc.call(ATMSessionIPCCommand.list, request: request)
    }

    func search(_ request: ATMSessionSearchRequest) async throws -> ATMSessionSearchResult {
        try await ipc.call(ATMSessionIPCCommand.search, request: request)
    }

    func show(_ request: ATMSessionShowRequest) async throws -> ATMSessionTranscript {
        try await ipc.call(ATMSessionIPCCommand.show, request: request)
    }

    func timeline(_ request: ATMSessionTimelineRequest) async throws -> [ATMSessionTimelineEntry] {
        try await ipc.call(ATMSessionIPCCommand.timeline, request: request).events
    }
}

extension ATMSessionReadMode {
    func showRequest(sessionID: String) -> ATMSessionShowRequest? {
        switch self {
        case .brief:
            return ATMSessionShowRequest(
                sessionID: sessionID,
                last: Self.briefTurnCount,
                maxChars: Self.briefMaxChars
            )
        case .timeline:
            return nil
        case .full:
            return ATMSessionShowRequest(sessionID: sessionID, includeThinking: true)
        }
    }
}
