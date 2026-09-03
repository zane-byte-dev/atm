import Foundation

/// A small, deterministic LRU for immutable prepared content. The budgets count
/// estimated retained bytes (including source keys), not only entry count.
/// Parsing is deliberately performed outside the lock.
final class ATMBoundedContentCache<Key: Hashable, Value>: @unchecked Sendable {
    private struct Entry {
        let value: Value
        let cost: Int
        var access: UInt64
    }

    private let lock = NSLock()
    private let countLimit: Int
    private let costLimit: Int
    private var entries: [Key: Entry] = [:]
    private var totalCost = 0
    private var access: UInt64 = 0

    init(countLimit: Int, costLimit: Int) {
        self.countLimit = max(0, countLimit)
        self.costLimit = max(0, costLimit)
    }

    func value(for key: Key) -> Value? {
        lock.lock()
        defer { lock.unlock() }
        guard var entry = entries[key] else { return nil }
        access &+= 1
        entry.access = access
        entries[key] = entry
        return entry.value
    }

    func insert(_ value: Value, for key: Key, cost: Int) {
        let cost = max(0, cost)
        lock.lock()
        defer { lock.unlock() }
        // An oversized document stays with its current reader; it must not
        // flush every useful small entry out of the shared cache.
        guard countLimit > 0, cost <= costLimit else { return }
        if let old = entries.removeValue(forKey: key) { totalCost -= old.cost }
        access &+= 1
        entries[key] = Entry(value: value, cost: cost, access: access)
        totalCost += cost
        while entries.count > countLimit || totalCost > costLimit {
            guard let oldest = entries.min(by: { $0.value.access < $1.value.access }) else { break }
            totalCost -= oldest.value.cost
            entries.removeValue(forKey: oldest.key)
        }
    }
}
