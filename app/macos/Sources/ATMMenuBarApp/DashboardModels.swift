import Foundation

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

    /// Whether the CLI sent this window's bounds. Without them `contains` matches
    /// every bucket, which is a harmless superset for day buckets but would let
    /// today's hours be drawn under yesterday's label.
    var isDated: Bool { !startDate.isEmpty && !endDate.isEmpty }

    /// Whether a bucket falls inside this window. Day buckets are `yyyy-MM-dd` and
    /// hour buckets `yyyy-MM-dd HH:mm`; only the leading day decides, so both kinds
    /// compare against the same inclusive yyyy-MM-dd bounds as text.
    func contains(date: String) -> Bool {
        guard isDated else { return true }
        let day = String(date.prefix(10))
        return day >= startDate && day <= endDate
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

    /// Shared with the `_ipc` protocol check via ATMVersionSkewAdvice: two skew
    /// messages quoting different install commands would send whichever user hit
    /// the other one down the wrong path.
    var recoverySuggestion: String? {
        switch direction {
        case .appTooOld:
            return ATMVersionSkewAdvice.appTooOld.text
        case .cliTooOld:
            return ATMVersionSkewAdvice.cliTooOld.text
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

/// One aggregate request replaces the ordinary `dashboard` argv contract. The
/// two sections remain independently selectable so cold start can paint work
/// before the statistics queries finish.
struct ATMDashboardRequest: Encodable, Equatable {
    let sections: [String]
    let sessionID: String?

    enum CodingKeys: String, CodingKey {
        case sections
        case sessionID = "session_id"
    }

    init(sections: [String] = [], sessionID: String? = nil) {
        self.sections = sections
        self.sessionID = sessionID?.isEmpty == false ? sessionID : nil
    }
}

enum ATMDashboardIPCCommand {
    static let snapshot = ATMIPCMethod<ATMDashboardRequest, ATMDashboardEnvelope>(
        "dashboard.snapshot",
        timeout: 30,
        responseKeyDecoding: .useDefault
    )
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
        guard let window = hourWindow(for: range) else { return stats(for: range) }
        return hourStats.filter { window.contains(date: $0.date) }
    }

    /// The window's own bounds when it is to be drawn in hour buckets, nil when it
    /// is to be drawn in day buckets.
    ///
    /// A single day reads better by hour, and both windows in the day group are
    /// labelled as such ("今日/昨日分时用量"), but the snapshot only carries hours
    /// for the last couple of days. A single day outside that span therefore falls
    /// back to its one day bucket rather than borrowing another day's hours — which
    /// is also why the hours are selected by the window's dates instead of taken
    /// wholesale.
    private func hourWindow(for range: ATMMetricsRange) -> ATMRangeData? {
        guard range.isSingleDay, let window = rangeData[range], window.isDated else { return nil }
        guard hourStats.contains(where: { window.contains(date: $0.date) }) else { return nil }
        return window
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
            return Self.merged(
                projectSeriesSource(for: range).map {
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

    /// Model rows in the same buckets `trendStats` uses, so a chart drawn over hour
    /// buckets never looks its series up in day rows and finds nothing.
    private func modelSeriesSource(for range: ATMMetricsRange) -> [ATMModelDayStats] {
        guard let window = hourWindow(for: range) else { return modelDayStats }
        return modelHourStats.filter { window.contains(date: $0.date) }
    }

    /// See modelSeriesSource: the project rows, bucketed the same way.
    private func projectSeriesSource(for range: ATMMetricsRange) -> [ATMProjectDayStats] {
        guard let window = hourWindow(for: range) else { return projectDayStats }
        return projectHourStats.filter { window.contains(date: $0.date) }
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
            return Self.merged(
                projectSeriesSource(for: range).compactMap { row -> ATMUsageSeriesPoint? in
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

    /// How many agents are waiting on a human right now.
    ///
    /// Looser than the condition that raises a notification
    /// (`needsHookAttention`): a count that is occasionally generous costs the
    /// reader nothing, so this keeps the keyword heuristic and therefore covers
    /// agents ATM cannot install hooks into.
    var attentionSessionCount: Int {
        liveStatus.sessions.filter(\.needsAnyAttention).count
    }

    var menuBarTitle: String {
        guard refreshedAt != .distantPast, let todayStats else { return "" }
        let base = "\(work.working.count) · \(NumberFormat.compact(todayStats.totalTokens))"
        // Leading, not trailing: this is the one thing here worth acting on, and
        // the quota suffix the status bar appends would otherwise push it off the
        // end of a crowded menu bar.
        guard attentionSessionCount > 0 else { return base }
        return "需要你 \(attentionSessionCount) · \(base)"
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
