import Combine
import Foundation

/// A narrow observation channel for task surfaces. Presence and usage updates
/// still reach their existing consumers through ATMDataStore.
@MainActor
final class ATMTaskState: ObservableObject {
    @Published private(set) var revision: UInt64 = 0
    private(set) var dataVersion: UInt64 = 0
    private(set) var refreshVersion: UInt64 = 0
    private(set) var isInitialWorkSettled = false
    private(set) var isInitialArchiveSettled = false

    /// Completion is distinct from an empty result: navigation must not let a
    /// faster archive response choose the initial task while work is pending.
    func settleInitialWork() {
        guard !isInitialWorkSettled else { return }
        isInitialWorkSettled = true
        revision &+= 1
    }

    func settleInitialArchive() {
        guard !isInitialArchiveSettled else { return }
        isInitialArchiveSettled = true
        revision &+= 1
    }

    func didRefreshWork() {
        refreshVersion &+= 1
        revision &+= 1
    }

    func invalidate(dataChanged: Bool = false) {
        if dataChanged { dataVersion &+= 1 }
        revision &+= 1
    }
}

struct ATMTodoDetailFreshness {
    let todo: ATMTodo?
    let loadedAt: Date

    func isFresh(for currentTodo: ATMTodo?, now: Date = Date()) -> Bool {
        todo == currentTodo && now.timeIntervalSince(loadedAt) < 60
    }
}

/// Tracks cache cost independently from the values, so transcripts and timelines
/// share one budget. Visible entries may exceed the budget, and are evicted as
/// soon as their final reader leaves. No full payload scans are needed on hits.
struct ATMReadCacheBudget {
    private struct Entry {
        let bytes: Int
        var access: UInt64
    }
    let byteLimit: Int
    let countLimit: Int
    private var entries: [String: Entry] = [:]
    private var clock: UInt64 = 0
    private(set) var totalBytes = 0

    init(byteLimit: Int, countLimit: Int) {
        self.byteLimit = max(0, byteLimit)
        self.countLimit = max(0, countLimit)
    }

    var count: Int { entries.count }

    mutating func touch(_ key: String) {
        guard entries[key] != nil else { return }
        clock &+= 1
        entries[key]?.access = clock
    }

    mutating func insert(_ key: String, bytes: Int) {
        totalBytes -= entries[key]?.bytes ?? 0
        clock &+= 1
        entries[key] = Entry(bytes: max(0, bytes), access: clock)
        totalBytes += max(0, bytes)
    }

    mutating func evict(protecting: Set<String>) -> [String] {
        var evicted: [String] = []
        for (key, entry) in entries.sorted(by: { $0.value.access < $1.value.access }) {
            guard totalBytes > byteLimit || entries.count > countLimit else { break }
            guard !protecting.contains(key) else { continue }
            totalBytes -= entry.bytes
            entries.removeValue(forKey: key)
            evicted.append(key)
        }
        return evicted
    }
}

extension ATMSessionTranscript {
    var estimatedCacheBytes: Int {
        512 + id.utf8.count + agent.utf8.count + project.utf8.count
            + tools.reduce(0) { $0 + $1.key.utf8.count + 64 }
            + turns.reduce(0) { count, turn in
                count + 128 + (turn.question?.utf8.count ?? 0)
                    + (turn.answer?.utf8.count ?? 0) + (turn.thinking?.utf8.count ?? 0)
            }
    }
}

extension ATMSessionTimelineEntry {
    var estimatedCacheBytes: Int {
        160 + kind.utf8.count + (role?.utf8.count ?? 0)
            + (model?.utf8.count ?? 0) + (content?.utf8.count ?? 0)
    }
}
