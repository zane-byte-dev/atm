import Foundation

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

    /// Flattened windows for compact surfaces such as the quick panel, where
    /// each rate-limit window is one row.
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

    /// One desktop card per service. A service can report more than one
    /// rate-limit window (for example Antigravity's 5h and 1w limits); those
    /// belong together under one identity rather than repeating the service
    /// chrome as separate cards.
    var serviceCards: [ATMQuotaServiceCard] {
        entries.map { entry in
            ATMQuotaServiceCard(
                id: entry.agent,
                agent: entry.agent,
                plan: entry.quota.plan,
                windows: entry.quota.windows,
                source: entry.quota.source,
                products: entry.quota.products ?? []
            )
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

/// One service tile on the desktop usage page, containing every limit window
/// reported for that service.
struct ATMQuotaServiceCard: Identifiable, Equatable {
    let id: String
    let agent: String
    let plan: String?
    let windows: [ATMQuotaWindow]
    let source: String?
    let products: [ATMQuotaProduct]

    var sourceLabel: String? {
        switch source {
        case "live": return "实时"
        case "cache": return "缓存"
        case "log": return "日志"
        default: return nil
        }
    }

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
        case "claude code": return "claude"
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
        case "antigravity": return "Antigravity"
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
        case "antigravity": return "Ag"
        case "": return "?"
        default:
            let label = name(agent)
            guard let first = label.first else { return "?" }
            return String(first).uppercased()
        }
    }

    /// Bundled, user-approved full-color mark used by dense app surfaces.
    /// Unknown clients keep the generated monogram fallback.
    static func iconResourceName(_ agent: String) -> String? {
        switch key(agent) {
        case "claude": return "agent-claude"
        case "codex": return "agent-codex"
        case "pi": return "agent-pi"
        case "copilot": return "agent-copilot"
        case "cursor": return "agent-cursor"
        case "qoder", "qodercli", "qoderwork": return "agent-qoder"
        case "grokbuild": return "agent-grok"
        default: return nil
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
        case "antigravity": return "paperplane.fill"
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
