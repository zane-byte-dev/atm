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

enum ATMAIDayCommand {
    static let dashboard = ["day", "dashboard", "--days", "180", "--json"]
    static let today = ["day", "today", "--json"]
    static let atlas = ["day", "atlas", "--json"]
    static let history = ["day", "history", "--days", "180", "--json"]
    static let privacy = ["day", "privacy", "show", "--json"]
    static func feedback(day: String, verdict: String, badge: String? = nil) -> [String] {
        var args = ["day", "feedback", day, "--verdict", verdict]
        if let badge { args += ["--badge", badge] }
        return args + ["--json"]
    }
    static func clearFeedback(day: String) -> [String] {
        ["day", "feedback", day, "--clear", "--json"]
    }
    static func show(day: String) -> [String] {
        ["day", "show", day, "--json"]
    }
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

    /// A day in progress keeps changing as sessions flush into the mirror, so the
    /// pane refreshes on its own instead of showing whatever was true when it was
    /// first opened. Long enough not to rebuild constantly, short enough that the
    /// numbers do not visibly lag.
    private static let autoRefreshInterval: TimeInterval = 420
    private var timer: Timer?

    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }()

    func refresh() {
        guard !isLoading else { return }
        isLoading = true
        Task {
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(ATMAIDayCommand.dashboard)
                let dashboard = try decoder.decode(ATMAIDayDashboard.self, from: data)
                today = dashboard.today
                atlas = dashboard.atlas
                history = dashboard.history
                privacy = dashboard.privacy
                lastRefreshed = Date()
                errorMessage = nil
            } catch { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    /// Refreshes only when the data is old enough to be worth rebuilding for,
    /// so returning to the tab or reactivating the app is cheap.
    func refreshIfStale() {
        guard let lastRefreshed else {
            refresh()
            return
        }
        if Date().timeIntervalSince(lastRefreshed) >= Self.autoRefreshInterval { refresh() }
    }

    func startAutoRefresh() {
        guard timer == nil else { return }
        let timer = Timer(timeInterval: Self.autoRefreshInterval, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refreshIfStale() }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    func stopAutoRefresh() {
        timer?.invalidate()
        timer = nil
    }

    /// Loads one past day in full, for the history drill-down.
    func loadDay(_ day: String) async throws -> ATMAIDayResult {
        let data = try await ATMCommandRunner().run(ATMAIDayCommand.show(day: day))
        return try decoder.decode(ATMAIDayResult.self, from: data)
    }

    func submitFeedback(verdict: String, badge: String? = nil) {
        guard let day = today?.day else { return }
        mutate(ATMAIDayCommand.feedback(day: day, verdict: verdict, badge: badge))
    }

    /// Undo for the feedback buttons: a correction overrides the engine for that
    /// day indefinitely and is one click away, so it has to be one click back.
    func clearFeedback() {
        guard let day = today?.day else { return }
        mutate(ATMAIDayCommand.clearFeedback(day: day))
    }

    func setSource(_ source: ATMAIDaySource, enabled: Bool) {
        mutate(["day", "sources", enabled ? "resume" : "pause", source.source, "--json"])
    }

    func deleteSource(_ source: ATMAIDaySource) {
        mutate(["day", "sources", "delete", source.source, "--yes", "--json"])
    }

    func setSemantic(_ enabled: Bool) {
        mutate(["day", "privacy", "set", "--semantic", enabled ? "on" : "off", "--json"])
    }

    func setRetention(_ days: Int) {
        mutate(["day", "privacy", "set", "--retention", String(days), "--json"])
    }

    func deleteAll() {
        Task {
            do {
                _ = try await ATMCommandRunner().run(["day", "delete", "--all", "--yes", "--json"])
                today = nil
                atlas = nil
                history = nil
                errorMessage = nil
            } catch { errorMessage = error.localizedDescription }
        }
    }

    func exportData() async throws -> Data {
        try await ATMCommandRunner().run(["day", "export", "--json"])
    }

    private func mutate(_ arguments: [String]) {
        Task {
            do { _ = try await ATMCommandRunner().run(arguments); refresh() }
            catch { errorMessage = error.localizedDescription }
        }
    }
}
