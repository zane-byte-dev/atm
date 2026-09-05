import Foundation

struct CompanionMenuRow: Equatable {
    let title: String
    let detail: String
    let route: RuntimeRoute
}

enum CompanionMenuPresentation {
    private static let quotaRowLimit = 3

    static func serviceTitle(active: Int, attention: Int) -> String {
        var parts = ["服务运行中", "\(max(0, active)) 个会话"]
        if attention > 0 { parts.append("\(attention) 项待处理") }
        return parts.joined(separator: " · ")
    }

    static func taskRows(_ todos: CompanionTodos?) -> [CompanionMenuRow] {
        guard let todos else { return [] }
        return todos.items.prefix(5).map { todo in
            let status = taskStateLabel(todo)
            let priority = todo.priority.uppercased()
            let title = "\(priority.isEmpty ? "" : "[\(priority)] ")\(todo.id.uppercased()) · \(clip(todo.title, 44))"
            let detail = "\(status) · \(clip(taskProjectLabel(todo), 24))"
            return CompanionMenuRow(title: title, detail: detail, route: .task(todo.id))
        }
    }

    static func quotaRows(_ quota: CompanionQuota?, now: Date = Date()) -> [CompanionMenuRow] {
        guard let quota else { return [] }
        return quota.windows.prefix(quotaRowLimit).map { window in
            let agent = agentDisplayName(window.agent)
            let title = window.windowMinutes > 0 ? "\(agent) · \(quotaWindowName(window.windowMinutes))" : agent
            let usage: String
            if let reason = window.unavailableReason, !reason.isEmpty {
                usage = "暂无数据"
            } else if window.resetElapsed {
                if let raw = window.usedPercent {
                    usage = "重置后待更新 · 上次 \(Int(min(max(raw, 0), 100).rounded()))%"
                } else {
                    usage = "重置后待更新"
                }
            } else if let raw = window.usedPercent {
                let display = min(max(raw, 0), 100)
                usage = "已用 \(Int(display.rounded()))%"
            } else {
                usage = "暂无数据"
            }
            var details = [usage]
            if !window.resetElapsed, let seconds = window.resetsAt, seconds > Int64(now.timeIntervalSince1970) {
                details.append(resetText(seconds, now: now))
            }
            if window.stale, !window.resetElapsed { details.append("记录较旧") }
            return CompanionMenuRow(title: title, detail: details.joined(separator: " · "), route: .usage(agent: window.agent))
        }
    }

    static func taskHeader(_ todos: CompanionTodos?) -> String {
        guard let todos else { return "当前任务 · 加载中" }
        if todos.error != nil { return "当前任务 · 暂不可用" }
        if todos.total <= 0 { return "当前任务 · 无" }
        return "当前任务 · \(max(0, todos.total))"
    }

    static func quotaHeader(_ quota: CompanionQuota?) -> String {
        guard let quota else { return "Agent 额度 · 加载中" }
        if quota.error != nil { return "Agent 额度 · 暂不可用" }
        if quota.windows.isEmpty { return "Agent 额度 · 暂无数据" }
        return "Agent 额度 · \(Set(quota.windows.map(\.agent)).count)"
    }

    /// Keep the ordinary state to today's compact Token total. Attention and
    /// quota warnings may expand it, while task/session counts stay in the menu.
    static func statusBarTitle(
        attention: Int,
        quota: CompanionQuota?,
        todayTokens: TodayTokenMenuState
    ) -> String {
        var leadingParts: [String] = []
        if attention > 0 { leadingParts.append("待 \(attention)") }

        var suffixParts: [String] = []
        if let window = tightestQuotaWindow(quota), displayPercent(window) >= 75 {
            let staleMark = window.stale ? "~" : ""
            suffixParts.append("\(clip(agentDisplayName(window.agent), 10)) \(staleMark)\(Int(displayPercent(window).rounded()))%")
        }
        if let value = TodayTokenMenuPresentation.statusBarValue(todayTokens) {
            suffixParts.append(value)
        }
        return boundedStatusBarTitle(leading: leadingParts, suffix: suffixParts)
    }

    static func statusBarTooltip(
        active: Int,
        attention: Int,
        todos: CompanionTodos?,
        quota: CompanionQuota?,
        todayTokens: TodayTokenMenuState
    ) -> String {
        var parts = ["\(max(0, active)) 个活跃会话", "\(max(0, attention)) 项待处理"]
        if let todos { parts.append("\(max(0, todos.total)) 个当前任务") }
        if let window = tightestQuotaWindow(quota) {
            parts.append("\(agentDisplayName(window.agent)) \(quotaWindowName(window.windowMinutes))已用 \(Int(displayPercent(window).rounded()))%")
        }
        if let value = TodayTokenMenuPresentation.statusBarValue(todayTokens) {
            parts.append("今日 Token \(value)")
        }
        return parts.joined(separator: " · ")
    }

    private static func tightestQuotaWindow(_ quota: CompanionQuota?) -> CompanionQuotaWindow? {
        quota?.windows
            .filter { $0.usedPercent != nil && !$0.resetElapsed && !$0.stale }
            .max { displayPercent($0) < displayPercent($1) }
    }

    private static func displayPercent(_ window: CompanionQuotaWindow) -> Double {
        window.resetElapsed ? 0 : min(max(window.usedPercent ?? 0, 0), 100)
    }

    /// Keep the quota warning and Token suffix intact when an unusually large
    /// attention count would otherwise push it past the compact budget.
    private static func boundedStatusBarTitle(leading: [String], suffix: [String], limit: Int = 42) -> String {
        let separator = " · "
        let leadingText = leading.joined(separator: separator)
        let suffixText = suffix.joined(separator: separator)
        let full = (leading + suffix).joined(separator: separator)
        guard full.count > limit else { return full }
        guard !suffixText.isEmpty else { return clip(leadingText, limit) }
        guard !leadingText.isEmpty else { return clip(suffixText, limit) }

        let leadingLimit = limit - separator.count - suffixText.count
        guard leadingLimit >= 2 else { return clip(suffixText, limit) }
        return "\(clip(leadingText, leadingLimit))\(separator)\(suffixText)"
    }

    static func taskStateLabel(_ todo: CompanionTodo) -> String {
        switch todo.menuState ?? todo.status {
        case "review": return "待验收"
        case "due": return "到期需处理"
        case "waiting": return "进行中 · 等待"
        case "blocked": return "已阻塞"
        case "working", "in_progress": return "进行中"
        case "open": return "待办"
        default: return todo.status
        }
    }

    static func taskProjectLabel(_ todo: CompanionTodo) -> String {
        let value = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return value.isEmpty ? "未分项目" : value
    }

    static func agentDisplayName(_ value: String) -> String {
        switch value.lowercased() {
        case "codex": return "Codex"
        case "claude": return "Claude"
        case "cursor": return "Cursor"
        case "copilot": return "Copilot"
        case "qoder": return "Qoder"
        case "grok": return "Grok"
        case "grokbuild": return "Grok Build"
        case "antigravity": return "Antigravity"
        default: return value.prefix(1).uppercased() + String(value.dropFirst())
        }
    }

    static func quotaWindowName(_ minutes: Int) -> String {
        if minutes % 1440 == 0 { return "\(minutes / 1440) 天" }
        if minutes % 60 == 0 { return "\(minutes / 60) 小时" }
        return "\(minutes) 分钟"
    }

    private static func resetText(_ timestamp: Int64, now: Date) -> String {
        let remaining = max(0, Double(timestamp) - now.timeIntervalSince1970)
        if remaining < 3600 { return "\(max(1, Int(ceil(remaining / 60)))) 分钟后重置" }
        if remaining < 86400 { return "\(max(1, Int(ceil(remaining / 3600)))) 小时后重置" }
        return "\(max(1, Int(ceil(remaining / 86400)))) 天后重置"
    }

    private static func clip(_ text: String, _ limit: Int) -> String {
        guard text.count > limit else { return text }
        return String(text.prefix(limit - 1)) + "…"
    }
}

enum TodayTokenMenuState: Equatable {
    case loading
    case value(Int64)
    case unavailable(String?)
}

enum TodayTokenMenuPresentation {
    static func resolve(today: CompanionTodayUsage?, legacyQuick: CompanionLegacyQuick?) -> TodayTokenMenuState {
        if let today {
            if let error = today.error, !error.isEmpty { return .unavailable(error) }
            return .value(max(0, today.totalTokens))
        }
        if let usage = legacyQuick?.usage {
            if let error = usage.error, !error.isEmpty { return .unavailable(error) }
            if let today = usage.ranges["today"] { return .value(max(0, today.totalTokens)) }
        }
        return .unavailable(nil)
    }

    static func title(_ state: TodayTokenMenuState) -> String {
        switch state {
        case .loading: return "今日 Token · 加载中…"
        case .value(let total): return "今日 Token · \(compactNumber(total))"
        case .unavailable: return "今日 Token · 暂不可用"
        }
    }

    static func detail(_ state: TodayTokenMenuState) -> String? {
        if case .unavailable(let message) = state { return message }
        return nil
    }

    static func statusBarValue(_ state: TodayTokenMenuState) -> String? {
        guard case .value(let total) = state else { return nil }
        return compactNumber(max(0, total))
    }

    static func compactNumber(_ value: Int64) -> String {
        let magnitude = abs(value)
        if magnitude >= 1_000_000_000 { return compact(Double(value) / 1_000_000_000, suffix: "B") }
        if magnitude >= 1_000_000 { return compact(Double(value) / 1_000_000, suffix: "M") }
        if magnitude >= 1_000 { return compact(Double(value) / 1_000, suffix: "K") }
        return String(value)
    }

    private static func compact(_ value: Double, suffix: String) -> String {
        let rounded = value.rounded()
        return abs(value - rounded) < 0.05 ? "\(Int(rounded))\(suffix)" : String(format: "%.1f%@", value, suffix)
    }
}
