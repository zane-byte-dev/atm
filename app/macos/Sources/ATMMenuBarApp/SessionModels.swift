import Foundation

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
