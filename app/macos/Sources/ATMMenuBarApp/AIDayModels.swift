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
}

struct ATMAIDayConcept: Codable {
    let id: String
    let title: String
    let explanation: String
    let tags: [String]
    let evidence: [ATMAIDayEvidence]
    let confidence: Double
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
    let baselineDays: Int
    let generatedAt: Int64
    var id: String { day }
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
}

@MainActor
final class ATMAIDayStore: ObservableObject {
    @Published private(set) var today: ATMAIDayResult?
    @Published private(set) var atlas: ATMAIDayAtlas?
    @Published private(set) var history: ATMAIDayHistory?
    @Published private(set) var privacy: ATMAIDayPrivacy?
    @Published private(set) var isLoading = false
    @Published var errorMessage: String?

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
                errorMessage = nil
            } catch { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func submitFeedback(verdict: String, badge: String? = nil) {
        guard let day = today?.day else { return }
        mutate(ATMAIDayCommand.feedback(day: day, verdict: verdict, badge: badge))
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
