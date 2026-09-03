import Foundation

/// Shared read requests belong to their consumers, not to a transient view.
/// The final consumer leaving cancels the underlying IPC; invalidation also
/// prevents a late response from putting obsolete data back in the cache.
@MainActor
final class ATMWorkspaceRequestCache<Key: Hashable, Value> {
    private struct Entry {
        let value: Value
        let expires: Date
        var accessed: Date
    }

    private final class Flight {
        let task: Task<Value, Error>
        var consumers: Set<UUID> = []

        init(task: Task<Value, Error>) { self.task = task }
    }

    private let lifetime: TimeInterval
    private let capacity: Int
    private let now: () -> Date
    private var entries: [Key: Entry] = [:]
    private var flights: [Key: Flight] = [:]
    private var generation = 0

    init(lifetime: TimeInterval = 120, capacity: Int = 64, now: @escaping () -> Date = Date.init) {
        self.lifetime = lifetime
        self.capacity = max(1, capacity)
        self.now = now
    }

    func isFresh(_ key: Key) -> Bool {
        guard let entry = entries[key] else { return false }
        return entry.expires > now()
    }

    func value(for key: Key, load: @escaping @MainActor () async throws -> Value) async throws -> Value {
        try Task.checkCancellation()
        if var entry = entries[key], entry.expires > now() {
            entry.accessed = now()
            entries[key] = entry
            return entry.value
        }

        let requestGeneration = generation
        let flight: Flight
        if let existing = flights[key] {
            flight = existing
        } else {
            flight = Flight(task: Task { try await load() })
            flights[key] = flight
        }
        let consumer = UUID()
        flight.consumers.insert(consumer)
        defer { release(key, flight: flight, consumer: consumer) }

        let value = try await withTaskCancellationHandler {
            try await flight.task.value
        } onCancel: {
            Task { @MainActor in self.release(key, flight: flight, consumer: consumer) }
        }
        try Task.checkCancellation()
        guard generation == requestGeneration else { throw CancellationError() }
        let date = now()
        entries[key] = Entry(value: value, expires: date.addingTimeInterval(lifetime), accessed: date)
        while entries.count > capacity, let oldest = entries.min(by: { $0.value.accessed < $1.value.accessed })?.key {
            entries.removeValue(forKey: oldest)
        }
        return value
    }

    func invalidate() {
        generation += 1
        entries.removeAll()
        cancelPending()
    }

    func cancelPending() {
        generation += 1
        for flight in flights.values { flight.task.cancel() }
        flights.removeAll()
    }

    private func release(_ key: Key, flight: Flight, consumer: UUID) {
        guard flight.consumers.remove(consumer) != nil else { return }
        if flight.consumers.isEmpty {
            flight.task.cancel()
            if flights[key] === flight { flights.removeValue(forKey: key) }
        }
    }
}
