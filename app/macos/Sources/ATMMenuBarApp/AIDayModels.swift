import AppKit
import Combine
import Foundation

struct ATMAIDayEvidence: Codable, Hashable, Identifiable {
    let metric: String
    let value: Double
    let unit: String?
    let comparison: String?
    var id: String { "\(metric)-\(value)" }
}

struct ATMAIDayFeatures: Codable {
    let sessionCount: Int64
    let eventCount: Int64
    let turnCount: Int64
    let toolCalls: Int64
    let sourceCount: Int64
    let inputTokens: Int64
    let outputTokens: Int64
    let cacheCreateTokens: Int64
    let cacheReadTokens: Int64
    let generationSeconds: Int64
    let activeSeconds: Int64
    let foregroundSeconds: Int64
    let backgroundSeconds: Int64
    let semanticCounts: [String: Int64]
    let modalityCounts: [String: Int64]

    var totalTokens: Int64 { inputTokens + outputTokens + cacheCreateTokens + cacheReadTokens }
    /// Excludes cache reads, which scale with context size rather than with work
    /// done and are one to two orders of magnitude larger than the rest.
    var workTokens: Int64 { inputTokens + outputTokens + cacheCreateTokens }
}

struct ATMAIDayConcept: Codable {
    let id: String
    let title: String
    let explanation: String
    let tags: [String]
    let evidence: [ATMAIDayEvidence]
    let confidence: Double
    // Optional so a newer app against an older `atm` degrades to the previous
    // contract instead of failing to decode the pane entirely.
    let evidenceStrength: Double?
    let origin: String?
    let computedId: String?
    let computedTitle: String?

    var strength: Double { evidenceStrength ?? 0 }
    var isUserCorrected: Bool { (origin ?? "computed") == "user_corrected" }
}

struct ATMAIDayCoverage: Codable {
    let complete: Bool
    let expectedSources: Int
    let presentSources: Int
    let missingSources: [String]?
    let dataThrough: Int64

    var dataThroughDate: Date? { dataThrough > 0 ? Date(timeIntervalSince1970: Double(dataThrough)) : nil }
}

struct ATMAIDayFeedback: Codable {
    let day: String
    let verdict: String
    let correctedBadgeId: String?
    let semanticLabels: [String]?
    let updatedAt: Int64
}

struct ATMAIDayBadge: Codable, Identifiable, Hashable {
    let id: String
    let name: String
    let description: String
    let family: String
    let kind: String
    let level: Int
    let unlocked: Bool
    let qualifiedDays: Int
    let qualifiedDates: [String]
    let nextLevelDays: Int
    let progress: Double
    let lastQualified: String?
    let cooldownUntil: String?
    let score: Double?
    let evidence: [ATMAIDayEvidence]?
}

struct ATMAIDayResult: Codable, Identifiable {
    let schemaVersion: Int
    let day: String
    let state: String
    let timezone: String
    let features: ATMAIDayFeatures
    let concept: ATMAIDayConcept?
    let badge: ATMAIDayBadge?
    let candidates: [ATMAIDayBadge]?
    let baselineDays: Int
    let provisional: Bool?
    let coverage: ATMAIDayCoverage?
    let feedback: ATMAIDayFeedback?
    let generatedAt: Int64
    var id: String { day }

    var isProvisional: Bool { provisional ?? false }
    var generatedAtDate: Date { Date(timeIntervalSince1970: Double(generatedAt)) }
    var hasContent: Bool { state != "empty" && badge != nil && concept != nil }
}

struct ATMAIDayAtlas: Codable {
    let schemaVersion: Int
    let generatedAt: Int64
    let unlocked: Int
    let total: Int
    let badges: [ATMAIDayBadge]
}

struct ATMAIDayHistory: Codable {
    let schemaVersion: Int
    let from: String
    let to: String
    let days: [ATMAIDayResult]
}

struct ATMAIDaySource: Codable, Identifiable {
    let source: String
    let enabled: Bool
    let semanticEnabled: Bool
    let eventCount: Int64
    let lastEventAt: Int64
    var id: String { source }
}

struct ATMAIDayPrivacy: Codable {
    let schemaVersion: Int
    let semanticEnabled: Bool
    let retentionDays: Int
    let rawContentRetained: Bool
    let sources: [ATMAIDaySource]
}

struct ATMAIDayDashboard: Codable {
    let schemaVersion: Int
    let today: ATMAIDayResult
    let atlas: ATMAIDayAtlas
    let history: ATMAIDayHistory
    let privacy: ATMAIDayPrivacy
}

struct ATMAIDayShowRequest: Encodable {
    let day: String
}

struct ATMAIDayFeedbackRequest: Encodable {
    let day: String
    let verdict: String?
    let correctedBadgeID: String?
    let semanticLabels: [String]?
    let clear: Bool?

    init(
        day: String,
        verdict: String? = nil,
        correctedBadgeID: String? = nil,
        semanticLabels: [String]? = nil,
        clear: Bool? = nil
    ) {
        self.day = day
        self.verdict = verdict
        self.correctedBadgeID = correctedBadgeID
        self.semanticLabels = semanticLabels
        self.clear = clear
    }

    enum CodingKeys: String, CodingKey {
        case day
        case verdict
        case correctedBadgeID = "corrected_badge_id"
        case semanticLabels = "semantic_labels"
        case clear
    }
}

struct ATMAIDaySourceSetRequest: Encodable {
    let source: String
    let enabled: Bool
    let semanticEnabled: Bool

    enum CodingKeys: String, CodingKey {
        case source
        case enabled
        case semanticEnabled = "semantic_enabled"
    }
}

struct ATMAIDaySourceDeleteRequest: Encodable {
    let source: String
    let confirmed: Bool
}

struct ATMAIDayPrivacyPatch: Encodable {
    let semanticEnabled: Bool?
    let retentionDays: Int?

    init(semanticEnabled: Bool? = nil, retentionDays: Int? = nil) {
        self.semanticEnabled = semanticEnabled
        self.retentionDays = retentionDays
    }

    enum CodingKeys: String, CodingKey {
        case semanticEnabled = "semantic_enabled"
        case retentionDays = "retention_days"
    }
}

struct ATMAIDayDeleteRequest: Encodable {
    let all: Bool?
    let from: String?
    let to: String?
    let confirmed: Bool

    init(all: Bool? = nil, from: String? = nil, to: String? = nil, confirmed: Bool) {
        self.all = all
        self.from = from
        self.to = to
        self.confirmed = confirmed
    }
}

struct ATMAIDaySourceDeleteResult: Decodable {
    let source: String
    let eventsDeleted: Int64
    let paused: Bool
}

struct ATMAIDayDeleteSummary: Decodable {
    let from: String
    let to: String
    let eventsDeleted: Int64
    let projectionsDeleted: Int64
    let feedbackDeleted: Int64
}

/// The complete AI Day surface consumed by the App. These are typed IPC methods,
/// not human CLI argv builders; adding a screen operation requires adding one
/// descriptor here and one registered Go verb.
enum ATMAIDayCommand {
    static let snapshot = ATMIPCMethod<ATMIPCNoRequest, ATMAIDayDashboard>(
        "day.snapshot",
        timeout: 60
    )
    static let show = ATMIPCMethod<ATMAIDayShowRequest, ATMAIDayResult>("day.show")
    static let feedback = ATMIPCMethod<ATMAIDayFeedbackRequest, ATMAIDayResult>(
        "day.feedback",
        timeout: 60
    )
    static let setSource = ATMIPCMethod<ATMAIDaySourceSetRequest, ATMAIDayPrivacy>("day.source.set")
    static let deleteSource = ATMIPCMethod<ATMAIDaySourceDeleteRequest, ATMAIDaySourceDeleteResult>(
        "day.source.delete",
        timeout: 60
    )
    static let setPrivacy = ATMIPCMethod<ATMAIDayPrivacyPatch, ATMAIDayPrivacy>("day.privacy.set")
    static let deleteData = ATMIPCMethod<ATMAIDayDeleteRequest, ATMAIDayDeleteSummary>(
        "day.data.delete",
        timeout: 60
    )
    // Keep the export payload as a JSON tree rather than decoding through the
    // App's current model. Export is a user-owned backup format and must retain
    // fields a newer CLI knows even if this App version does not display them.
    static let exportData = ATMIPCMethod<ATMIPCNoRequest, ATMJSONValue>(
        "day.data.export",
        timeout: 60
    )
}

@MainActor
final class ATMAIDayStore: ObservableObject {
    @Published private(set) var today: ATMAIDayResult?
    @Published private(set) var atlas: ATMAIDayAtlas?
    @Published private(set) var history: ATMAIDayHistory?
    @Published private(set) var privacy: ATMAIDayPrivacy?
    @Published private(set) var isLoading = false
    @Published private(set) var lastRefreshed: Date?
    @Published var errorMessage: String?
    @Published var selectedTab: ATMAIDayTab = .today
    @Published var showAtlasMap = true
    @Published var historyFilter: String?
    let scrollPositions = ATMWorkspaceScrollPositions()

    private let loadDashboard: @MainActor () async throws -> ATMAIDayDashboard
    private var refreshTask: Task<Void, Never>?
    private var refreshGeneration = 0
    private var mutationCount = 0
    private var isVisible = false

    init(loadDashboard: @escaping @MainActor () async throws -> ATMAIDayDashboard = {
        try await ATMIPCClient().call(ATMAIDayCommand.snapshot)
    }) {
        self.loadDashboard = loadDashboard
    }

    /// A day in progress keeps changing as sessions flush into the mirror, so the
    /// pane refreshes on its own instead of showing whatever was true when it was
    /// first opened. Long enough not to rebuild constantly, short enough that the
    /// numbers do not visibly lag.
    nonisolated static let autoRefreshInterval: TimeInterval = 420
    /// The timer ticks far more often than data is considered stale, and every tick
    /// re-checks the elapsed time. Ticking at exactly `autoRefreshInterval` looks
    /// equivalent but is not: `lastRefreshed` is stamped when the *first* load
    /// finishes, seconds after the timer starts, so the tick at 420s is always a
    /// few seconds short of the threshold, skips, and pushes the first automatic
    /// refresh out to ~14 minutes. Decoupling the two bounds lateness by one tick.
    nonisolated static let refreshCheckInterval: TimeInterval = 60
    private var timer: Timer?

    /// Pure decision so the cadence is testable without a run loop.
    nonisolated static func shouldRefresh(lastRefreshed: Date?, now: Date) -> Bool {
        guard let lastRefreshed else { return true }
        return now.timeIntervalSince(lastRefreshed) >= autoRefreshInterval
    }

    func refresh() {
        guard refreshTask == nil, mutationCount == 0 else { return }
        refreshGeneration += 1
        let generation = refreshGeneration
        isLoading = true
        refreshTask = Task {
            defer {
                if refreshGeneration == generation {
                    refreshTask = nil
                    isLoading = mutationCount > 0
                }
            }
            do {
                let dashboard = try await loadDashboard()
                try Task.checkCancellation()
                guard refreshGeneration == generation else { return }
                today = dashboard.today
                atlas = dashboard.atlas
                history = dashboard.history
                privacy = dashboard.privacy
                lastRefreshed = Date()
                errorMessage = nil
            } catch is CancellationError {
                return
            } catch {
                guard !Task.isCancelled, refreshGeneration == generation else { return }
                errorMessage = error.localizedDescription
            }
        }
    }

    private func cancelRead() {
        refreshGeneration += 1
        refreshTask?.cancel()
        refreshTask = nil
        isLoading = mutationCount > 0
    }

    /// A snapshot that started before a write must never overwrite that write.
    private func beginMutation() {
        mutationCount += 1
        lastRefreshed = nil
        cancelRead()
    }

    private func endMutation(refreshAfterward: Bool = true) {
        mutationCount -= 1
        isLoading = mutationCount > 0
        if mutationCount == 0, refreshAfterward, isVisible { refresh() }
    }

    /// Refreshes only when the data is old enough to be worth rebuilding for,
    /// so returning to the tab or reactivating the app is cheap.
    func refreshIfStale() {
        guard isVisible else { return }
        if Self.shouldRefresh(lastRefreshed: lastRefreshed, now: Date()) { refresh() }
    }

    func startAutoRefresh() {
        isVisible = true
        refreshIfStale()
        guard timer == nil else { return }
        let timer = Timer(timeInterval: Self.refreshCheckInterval, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refreshIfStale() }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    func stopAutoRefresh() {
        isVisible = false
        timer?.invalidate()
        timer = nil
        cancelRead()
    }

    /// Loads one past day in full, for the history drill-down.
    func loadDay(_ day: String) async throws -> ATMAIDayResult {
        try await ATMIPCClient().call(
            ATMAIDayCommand.show,
            request: ATMAIDayShowRequest(day: day)
        )
    }

    func submitFeedback(verdict: String, badge: String? = nil) {
        guard let day = today?.day else { return }
        mutate(
            ATMAIDayCommand.feedback,
            request: ATMAIDayFeedbackRequest(
                day: day,
                verdict: verdict,
                correctedBadgeID: badge
            )
        )
    }

    /// Undo for the feedback buttons: a correction overrides the engine for that
    /// day indefinitely and is one click away, so it has to be one click back.
    func clearFeedback() {
        guard let day = today?.day else { return }
        mutate(
            ATMAIDayCommand.feedback,
            request: ATMAIDayFeedbackRequest(day: day, clear: true)
        )
    }

    func setSource(_ source: ATMAIDaySource, enabled: Bool) {
        mutate(
            ATMAIDayCommand.setSource,
            request: ATMAIDaySourceSetRequest(
                source: source.source,
                enabled: enabled,
                // The old pause/resume commands changed both switches together:
                // resume opted semantic classification back in; pause opted out.
                semanticEnabled: enabled
            )
        )
    }

    func deleteSource(_ source: ATMAIDaySource) {
        mutate(
            ATMAIDayCommand.deleteSource,
            request: ATMAIDaySourceDeleteRequest(source: source.source, confirmed: true)
        )
    }

    func setSemantic(_ enabled: Bool) {
        mutate(
            ATMAIDayCommand.setPrivacy,
            request: ATMAIDayPrivacyPatch(semanticEnabled: enabled)
        )
    }

    func setRetention(_ days: Int) {
        mutate(
            ATMAIDayCommand.setPrivacy,
            request: ATMAIDayPrivacyPatch(retentionDays: days)
        )
    }

    func deleteAll() {
        beginMutation()
        Task {
            defer { endMutation(refreshAfterward: false) }
            do {
                _ = try await ATMIPCClient().call(
                    ATMAIDayCommand.deleteData,
                    request: ATMAIDayDeleteRequest(all: true, confirmed: true)
                )
                today = nil
                atlas = nil
                history = nil
                errorMessage = nil
            } catch { errorMessage = error.localizedDescription }
        }
    }

    func exportData() async throws -> Data {
        try await ATMIPCClient().callRawPayload(ATMAIDayCommand.exportData)
    }

    private func mutate<Request: Encodable, Response: Decodable>(
        _ method: ATMIPCMethod<Request, Response>,
        request: Request
    ) {
        beginMutation()
        Task {
            defer { endMutation() }
            do {
                _ = try await ATMIPCClient().call(method, request: request)
                errorMessage = nil
            }
            catch { errorMessage = error.localizedDescription }
        }
    }
}
