import Combine
import XCTest
@testable import ATMMenuBarApp

final class ATMWorkspaceCacheTests: XCTestCase {
    func testInitialScrollRestoreWaitsForAsyncContentAndRunsOnlyOnce() {
        var restore = ATMWorkspaceInitialScrollRestore()
        restore.begin(at: CGPoint(x: 0, y: 800))
        XCTAssertNil(restore.takeOffset(constrainedTo: .zero), "a placeholder must not erase the saved offset")
        XCTAssertNil(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 120)))
        XCTAssertEqual(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 800)), CGPoint(x: 0, y: 800))
        XCTAssertNil(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 900)), "ordinary later layout must not restore again")
    }

    func testUserScrollCancelsPendingRestoreAndTimeoutClampsShorterContent() {
        var restore = ATMWorkspaceInitialScrollRestore()
        restore.begin(at: CGPoint(x: 0, y: 800))
        restore.cancelForUserInteraction()
        XCTAssertNil(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 800)))
        XCTAssertNil(restore.takeOffset(constrainedTo: .zero, finalAttempt: true))
        restore.begin(at: CGPoint(x: 0, y: 800))
        XCTAssertEqual(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 200), finalAttempt: true), CGPoint(x: 0, y: 200))
        XCTAssertFalse(restore.isPending)
    }

    func testUnpreparedMarkdownCannotConsumeInitialOffsetEvenAtFinalClamp() {
        var restore = ATMWorkspaceInitialScrollRestore()
        restore.begin(at: CGPoint(x: 0, y: 800))
        XCTAssertNil(restore.takeOffset(constrainedTo: .zero, contentReady: false, finalAttempt: true))
        XCTAssertEqual(restore.desiredOffset, CGPoint(x: 0, y: 800))
        XCTAssertNil(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 800), contentReady: false))
        XCTAssertEqual(restore.takeOffset(constrainedTo: CGPoint(x: 0, y: 800), contentReady: true), CGPoint(x: 0, y: 800))
        XCTAssertNil(restore.takeOffset(constrainedTo: .zero, contentReady: true, finalAttempt: true))
    }

    @MainActor
    func testFreshValuesSurviveConsumerRemountAndExpire() async throws {
        var now = Date(timeIntervalSince1970: 1_000)
        let cache = ATMWorkspaceRequestCache<String, Int>(lifetime: 120, now: { now })
        var reads = 0
        let load = { @MainActor in reads += 1; return reads }
        let first = try await cache.value(for: "notes", load: load)
        cache.cancelPending() // The workspace disappears, but completed data stays.
        let revisited = try await cache.value(for: "notes", load: load)
        XCTAssertEqual(first, revisited)
        XCTAssertEqual(reads, 1)
        now = now.addingTimeInterval(121)
        let revalidated = try await cache.value(for: "notes", load: load)
        XCTAssertEqual(revalidated, 2)
    }

    @MainActor
    func testOverlappingListConsumersShareRequestAndOneMayCancel() async throws {
        let cache = ATMWorkspaceRequestCache<String, Int>()
        let started = expectation(description: "underlying read started")
        let attached = expectation(description: "second consumer attached")
        var continuation: CheckedContinuation<Int, Error>?
        var reads = 0
        let first = Task {
            try await cache.value(for: "notes") {
                reads += 1
                return try await withCheckedThrowingContinuation {
                    continuation = $0
                    started.fulfill()
                }
            }
        }
        await fulfillment(of: [started], timeout: 1)
        let second = Task {
            attached.fulfill()
            return try await cache.value(for: "notes") { reads += 1; return 99 }
        }
        await fulfillment(of: [attached], timeout: 1)
        first.cancel()
        continuation?.resume(returning: 7)
        let result = try await second.value
        XCTAssertEqual(result, 7)
        XCTAssertEqual(reads, 1)
        do { _ = try await first.value; XCTFail("cancelled consumer accepted a result") }
        catch is CancellationError {}
    }

    @MainActor
    func testLastConsumerCancellationCancelsUnderlyingRead() async {
        let cache = ATMWorkspaceRequestCache<String, Int>()
        let started = expectation(description: "read started")
        let cancelled = expectation(description: "read cancellation propagated")
        let task = Task {
            try await cache.value(for: "notes") {
                try await withTaskCancellationHandler {
                    started.fulfill()
                    try await Task.sleep(nanoseconds: 60_000_000_000)
                    return 1
                } onCancel: { cancelled.fulfill() }
            }
        }
        await fulfillment(of: [started], timeout: 1)
        task.cancel()
        await fulfillment(of: [cancelled], timeout: 1)
        _ = await task.result
        XCTAssertFalse(cache.isFresh("notes"))
    }

    @MainActor
    func testInvalidationRejectsLateResponseAfterAnEdit() async throws {
        let cache = ATMWorkspaceRequestCache<String, Int>()
        let started = expectation(description: "old read started")
        var continuation: CheckedContinuation<Int, Error>?
        let old = Task {
            try await cache.value(for: "notes") {
                try await withCheckedThrowingContinuation {
                    continuation = $0
                    started.fulfill()
                }
            }
        }
        await fulfillment(of: [started], timeout: 1)
        cache.invalidate()
        let updated = try await cache.value(for: "notes") { 42 }
        continuation?.resume(returning: 1) // Models an IPC that finishes despite cancellation.
        do { _ = try await old.value; XCTFail("obsolete read repopulated the cache") }
        catch is CancellationError {}
        let cached = try await cache.value(for: "notes") { 99 }
        XCTAssertEqual(updated, 42)
        XCTAssertEqual(cached, 42)
    }

    @MainActor
    func testCacheBudgetEvictsLeastRecentlyReadEntry() async throws {
        var now = Date(timeIntervalSince1970: 1_000)
        let cache = ATMWorkspaceRequestCache<String, Int>(capacity: 2, now: { now })
        _ = try await cache.value(for: "a") { 1 }
        now += 1
        _ = try await cache.value(for: "b") { 2 }
        now += 1
        _ = try await cache.value(for: "a") { 99 }
        now += 1
        _ = try await cache.value(for: "c") { 3 }
        XCTAssertTrue(cache.isFresh("a"))
        XCTAssertFalse(cache.isFresh("b"))
        XCTAssertTrue(cache.isFresh("c"))
    }

    @MainActor
    func testAIDayReentryKeepsFreshSnapshotAndViewSelection() async throws {
        let dashboard = try aidDayFixture()
        var reads = 0
        let store = ATMAIDayStore { reads += 1; return dashboard }
        let loaded = expectation(description: "snapshot applied")
        let subscription = store.$lastRefreshed.compactMap { $0 }.first().sink { _ in loaded.fulfill() }
        store.startAutoRefresh()
        await fulfillment(of: [loaded], timeout: 1)
        store.selectedTab = .history
        store.historyFilter = "writer"
        store.showAtlasMap = false
        store.stopAutoRefresh()
        store.startAutoRefresh()
        XCTAssertEqual(reads, 1)
        XCTAssertEqual(store.today?.day, "2026-09-03")
        XCTAssertEqual(store.selectedTab, .history)
        XCTAssertEqual(store.historyFilter, "writer")
        XCTAssertFalse(store.showAtlasMap)
        store.stopAutoRefresh()
        withExtendedLifetime(subscription) {}
    }

    @MainActor
    func testAIDayHiddenWorkspaceRejectsLateSnapshot() async throws {
        let dashboard = try aidDayFixture()
        let started = expectation(description: "snapshot started")
        let returned = expectation(description: "snapshot returned after cancellation")
        var continuation: CheckedContinuation<ATMAIDayDashboard, Error>?
        let store = ATMAIDayStore {
            let result = try await withCheckedThrowingContinuation {
                continuation = $0
                started.fulfill()
            }
            returned.fulfill()
            return result
        }
        store.startAutoRefresh()
        await fulfillment(of: [started], timeout: 1)
        store.stopAutoRefresh()
        continuation?.resume(returning: dashboard)
        await fulfillment(of: [returned], timeout: 1)
        XCTAssertNil(store.today)
        XCTAssertNil(store.lastRefreshed)
        XCTAssertFalse(store.isLoading)
        XCTAssertNil(store.errorMessage)
    }

    private func aidDayFixture() throws -> ATMAIDayDashboard {
        let data = Data("""
        {"schema_version":1,"today":{"schema_version":1,"day":"2026-09-03",
        "state":"empty","timezone":"Asia/Shanghai","features":{
        "session_count":0,"event_count":0,"turn_count":0,"tool_calls":0,
        "source_count":0,"input_tokens":0,"output_tokens":0,"cache_create_tokens":0,
        "cache_read_tokens":0,"generation_seconds":0,"active_seconds":0,
        "foreground_seconds":0,"background_seconds":0,"semantic_counts":{},"modality_counts":{}},
        "baseline_days":0,"generated_at":1},
        "atlas":{"schema_version":1,"generated_at":1,"unlocked":0,"total":0,"badges":[]},
        "history":{"schema_version":1,"from":"2026-09-01","to":"2026-09-03","days":[]},
        "privacy":{"schema_version":1,"semantic_enabled":true,"retention_days":90,
        "raw_content_retained":false,"sources":[]}}
        """.utf8)
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return try decoder.decode(ATMAIDayDashboard.self, from: data)
    }
}
