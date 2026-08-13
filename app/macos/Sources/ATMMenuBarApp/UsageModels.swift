import Foundation

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

    /// Whether these buckets are hours rather than days, read off the keys instead
    /// of the window: a single-day window whose hours the snapshot does not carry
    /// falls back to one day bucket, and labelling that tick "00:00" would claim an
    /// hourly reading the chart is not showing.
    static func isHourly(_ source: [String]) -> Bool {
        source.contains { $0.contains(" ") }
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
        let unit: TimeInterval = isHourly(source) ? 60 * 60 : 24 * 60 * 60
        let first = dates.first ?? Date()
        let last = dates.last ?? first

        // Swift Charts may drop an explicitly supplied final label when it sits
        // against the plot boundary, so reserve a full date-width on that side.
        return first.addingTimeInterval(-unit * 0.5)...last.addingTimeInterval(unit * 1.25)
    }
}
