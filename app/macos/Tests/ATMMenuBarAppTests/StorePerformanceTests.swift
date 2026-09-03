import Combine
import XCTest
@testable import ATMMenuBarApp

final class StorePerformanceTests: XCTestCase {
    private let todoJSON = #"{"id":"t1","title":"Task","status":"open","priority":"P1","created":"2026-09-03"}"#

    private func todo() throws -> ATMTodo {
        try JSONDecoder().decode(ATMTodo.self, from: Data(todoJSON.utf8))
    }

    private func dashboard(ranges: String = "{}", todos: String = "[]", dayStats: String = "[]") -> String {
        """
        {"schema_version":6,"generated_at":"2026-09-03T00:00:00Z","work":{},"todos":\(todos),
         "ranges":\(ranges),"day_stats":\(dayStats),"live_status":{},"index_health":{"generated_at":"",
         "index":{"path":"","exists":true,"schema_version":1,"indexed_sessions":0},
         "sync":{"scope":"all","status":"fresh","run_status":"succeeded",
         "stale_after_seconds":600,"last_error":"","last_synced_files":0}}}
        """
    }

    private func envelope(_ verb: String, data: String) -> String {
        """
        {"envelope_version":1,"protocol_version":1,"request_id":"test","verb":"\(verb)","data":\(data)}
        """
    }

    private func quoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func runner(scriptBody: (URL) -> String) throws -> (ATMCommandRunner, URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("atm-store-perf-\(UUID())")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let executable = directory.appendingPathComponent("atm")
        try ("#!/bin/sh\n" + scriptBody(directory)).write(to: executable, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
        return (try ATMCommandRunner(environment: ["ATM_EXECUTABLE": executable.path]), directory)
    }

    @MainActor
    private func waitUntil(_ predicate: () -> Bool) async {
        for _ in 0..<250 {
            if predicate() { return }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTFail("Timed out waiting for Store state")
    }

    func testSessionBudgetEvictsLeastRecentlyUsedAndKeepsActiveReaders() {
        var budget = ATMReadCacheBudget(byteLimit: 100, countLimit: 3)
        budget.insert("old", bytes: 40)
        budget.insert("visible", bytes: 40)
        budget.touch("old")
        budget.insert("new", bytes: 40)
        XCTAssertEqual(budget.evict(protecting: ["visible"]), ["old"])
        XCTAssertEqual(budget.totalBytes, 80)
        budget.insert("oversized", bytes: 150)
        XCTAssertEqual(Set(budget.evict(protecting: ["oversized"])), ["visible", "new"])
        XCTAssertEqual(budget.totalBytes, 150)
        XCTAssertEqual(budget.evict(protecting: []), ["oversized"])
        XCTAssertEqual(budget.totalBytes, 0)
    }

    func testSessionBudgetBoundsZeroLengthAndReplacementEntries() {
        var budget = ATMReadCacheBudget(byteLimit: 100, countLimit: 2)
        budget.insert("one", bytes: 70)
        budget.insert("one", bytes: 10)
        budget.insert("two", bytes: 0)
        budget.insert("three", bytes: 0)
        XCTAssertEqual(budget.totalBytes, 10)
        XCTAssertEqual(budget.evict(protecting: []), ["one"])
        XCTAssertEqual(budget.count, 2)
    }

    func testDetailCacheInvalidatesAtTTLOrTaskVersion() throws {
        let original = try todo()
        let date = Date(timeIntervalSince1970: 100)
        let stamp = ATMTodoDetailFreshness(todo: original, loadedAt: date)
        XCTAssertTrue(stamp.isFresh(for: original, now: date.addingTimeInterval(59)))
        XCTAssertFalse(stamp.isFresh(for: original, now: date.addingTimeInterval(60)))
        XCTAssertFalse(stamp.isFresh(for: original.replacingLifecycle(status: "done"), now: date))
    }

    @MainActor
    func testTaskObservationIgnoresPresenceAndStatisticsChanges() throws {
        let store = ATMDataStore()
        var emissions = 0
        let subscription = store.taskState.objectWillChange.sink { emissions += 1 }
        var state = store.dashboardState
        state.snapshot = state.snapshot.replacingLiveStatus(ATMLiveStatus(sessions: [], time: "12:00:01"))
        store.applyDashboardRefresh(state)
        XCTAssertEqual(emissions, 0)
        let stats = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: Data(dashboard().utf8))
        state.snapshot = state.snapshot.mergingStats(stats)
        store.applyDashboardRefresh(state)
        XCTAssertEqual(emissions, 0)
        state.allTodos = [try todo()]
        store.applyDashboardRefresh(state)
        XCTAssertEqual(emissions, 1)
        XCTAssertEqual(store.taskState.dataVersion, 1)
        state.errorMessage = "Task read failed"
        store.applyDashboardRefresh(state)
        XCTAssertEqual(emissions, 2)
        XCTAssertEqual(store.taskState.dataVersion, 1)
        withExtendedLifetime(subscription) {}
    }

    func testSectionMergePreservesOtherRangeWorkAndIndependentTimestamps() throws {
        let first = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: Data(dashboard(
            ranges: #"{"today":{"start_date":"2026-09-03","end_date":"2026-09-03"}}"#
        ).utf8))
        let second = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: Data(dashboard(
            ranges: #"{"yesterday":{"start_date":"2026-09-02","end_date":"2026-09-02"}}"#
        ).utf8))
        let workDate = Date(timeIntervalSince1970: 10)
        let statsDate = Date(timeIntervalSince1970: 20)
        let work = ATMDashboardSnapshot.empty.replacingTodo(try todo())
        let merged = work.mergingStats(first, at: statsDate).mergingStats(second, at: statsDate)
        XCTAssertEqual(merged.work.open.map(\.id), ["t1"])
        XCTAssertEqual(merged.rangeData[.today]?.startDate, "2026-09-03")
        XCTAssertEqual(merged.rangeData[.yesterday]?.startDate, "2026-09-02")
        let refreshed = merged.mergingWork(first.makeSnapshot(refreshedAt: workDate))
        XCTAssertEqual(refreshed.refreshedAt, workDate)
        XCTAssertEqual(refreshed.statsRefreshedAt, statsDate)
        XCTAssertEqual(refreshed.rangeData[.yesterday]?.startDate, "2026-09-02")
    }

    @MainActor
    func testArchiveReadCannotResurrectMoveAfterWorkAcknowledgesIt() throws {
        let store = ATMDataStore()
        let original = try todo()
        var state = store.dashboardState
        state.allTodos = [original]
        store.applyDashboardRefresh(state)
        store.applySuccessfulTodoAction(.archive, on: original)
        // The work response has already observed removal; an older archive list
        // can still arrive without the moved row.
        state.allTodos = []
        store.applyDashboardRefresh(state)
        store.applyArchiveRefresh([])
        XCTAssertEqual(store.archivedTodos.map(\.id), ["t1"])
        store.applyArchiveRefresh([original])
        store.applySuccessfulTodoAction(.restore, on: original)
        state.allTodos = [original]
        store.applyDashboardRefresh(state)
        store.applyArchiveRefresh([original])
        XCTAssertTrue(store.archivedTodos.isEmpty)
        store.applyArchiveRefresh([])
        XCTAssertTrue(store.archivedTodos.isEmpty)
    }

    @MainActor
    func testWorkRefreshCompletesWhileQuotaIsStillBlockedAndNeverAsksForStats() async throws {
        let workResponse = envelope("dashboard.snapshot", data: dashboard(todos: "[\(todoJSON)]"))
        let quotaResponse = envelope("quota.snapshot", data: #"{"agents":{}}"#)
        let archiveResponse = envelope("todo.list", data: "[]")
        let (commandRunner, directory) = try runner { directory in
            """
            case "$2" in
            dashboard.snapshot)
                /bin/cat > \(quoted(directory.appendingPathComponent("work.json").path))
                /usr/bin/printf '%s' \(quoted(workResponse)) ;;
            todo.list)
                /bin/cat > /dev/null
                /usr/bin/printf '%s' \(quoted(archiveResponse)) ;;
            *)
                /bin/cat > /dev/null
                while [ ! -e \(quoted(directory.appendingPathComponent("release").path)) ]; do /bin/sleep 0.02; done
                /usr/bin/printf '%s' \(quoted(quotaResponse)) ;;
            esac
            """
        }
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeDashboardRunner: { commandRunner })
        store.refresh()
        await waitUntil { store.allTodos.count == 1 && !store.isLoading }
        let request = try JSONSerialization.jsonObject(with: Data(contentsOf: directory.appendingPathComponent("work.json"))) as? [String: Any]
        XCTAssertEqual(request?["sections"] as? [String], ["work", "summary"])
        XCTAssertTrue(store.loadingUsageRanges.isEmpty)
        try Data().write(to: directory.appendingPathComponent("release"))
        try? await Task.sleep(nanoseconds: 150_000_000)
    }

    @MainActor
    func testRepeatedTaskDetailReadsHitCacheAndCancellationAllowsImmediateRetry() async throws {
        let response = envelope("todo.doc", data: ###"{"exists":true,"content":"## Progress\n\n- Complete"}"###)
        let (commandRunner, directory) = try runner { directory in
            """
            /bin/cat > /dev/null
            /usr/bin/printf 'request\\n' >> \(quoted(directory.appendingPathComponent("calls").path))
            /usr/bin/printf '%s' \(quoted(response))
            """
        }
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: commandRunner) })
        store.retainTodoDetailReads(for: "t1")
        store.loadProgress(for: "t1")
        await waitUntil { !store.isLoadingProgress(for: "t1") }
        store.loadProgress(for: "t1")
        XCTAssertFalse(store.isLoadingProgress(for: "t1"))
        XCTAssertEqual(try String(contentsOf: directory.appendingPathComponent("calls")).split(separator: "\n").count, 1)
        store.loadProgress(for: "t1", force: true)
        store.cancelTodoDetailReads(for: "t1")
        XCTAssertFalse(store.isLoadingProgress(for: "t1"))
        store.retainTodoDetailReads(for: "t1")
        store.loadProgress(for: "t1", force: true)
        await waitUntil { !store.isLoadingProgress(for: "t1") }
        store.cancelTodoDetailReads(for: "t1")
    }

    func testCachedStatsCannotRollBackNewerMenuSummary() throws {
        let latestDay = #"[{"date":"2026-09-03","sessions":1,"queries":1,"input_tokens":100,"output_tokens":20,"cost_usd":1}]"#
        let oldDay = #"[{"date":"2026-09-03","sessions":1,"queries":1,"input_tokens":10,"output_tokens":2,"cost_usd":0.1}]"#
        let work = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: Data(dashboard(dayStats: latestDay).utf8))
        let cached = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: Data(dashboard(dayStats: oldDay).utf8))
        let state = ATMDashboardSnapshot.empty.mergingWork(work.makeSnapshot()).mergingStats(cached)
        XCTAssertEqual(state.todayStats?.totalTokens, 120)
        XCTAssertTrue(state.menuBarTitle.contains("120"))
    }

    @MainActor
    func testUsageLoadsOnlyRequestedCompactRangeAndRevisitsRestoreCachedBuckets() async throws {
        let today = envelope("dashboard.snapshot", data: dashboard(
            ranges: #"{"today":{"start_date":"2026-09-03","end_date":"2026-09-03"}}"#,
            dayStats: #"[{"date":"2026-09-03","sessions":1,"queries":1,"input_tokens":100,"output_tokens":20,"cost_usd":1}]"#
        ))
        let yesterday = envelope("dashboard.snapshot", data: dashboard(
            ranges: #"{"yesterday":{"start_date":"2026-09-02","end_date":"2026-09-02"}}"#,
            dayStats: #"[{"date":"2026-09-02","sessions":1,"queries":1,"input_tokens":50,"output_tokens":10,"cost_usd":1}]"#
        ))
        let (commandRunner, directory) = try runner { directory in
            """
            request=$(/bin/cat)
            /usr/bin/printf '%s\\n' "$request" >> \(quoted(directory.appendingPathComponent("requests").path))
            case "$request" in
            *yesterday*) /usr/bin/printf '%s' \(quoted(yesterday)) ;;
            *) /usr/bin/printf '%s' \(quoted(today)) ;;
            esac
            """
        }
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeDashboardRunner: { commandRunner })
        store.loadUsageStats(range: .today)
        await waitUntil { store.loadingUsageRanges.isEmpty }
        XCTAssertEqual(store.snapshot.dayStats.first?.date, "2026-09-03")
        store.loadUsageStats(range: .yesterday)
        await waitUntil { store.loadingUsageRanges.isEmpty }
        XCTAssertEqual(store.snapshot.dayStats.first?.date, "2026-09-02")
        store.loadUsageStats(range: .today)
        XCTAssertTrue(store.loadingUsageRanges.isEmpty)
        XCTAssertEqual(store.snapshot.dayStats.first?.date, "2026-09-03")
        let requests = try String(contentsOf: directory.appendingPathComponent("requests")).split(separator: "\n")
        XCTAssertEqual(requests.count, 2)
        let first = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(requests[0].utf8)) as? [String: Any])
        XCTAssertEqual(first["sections"] as? [String], ["stats"])
        XCTAssertEqual(first["ranges"] as? [String], ["today"])
        XCTAssertEqual(first["compact"] as? Bool, true)
    }

    @MainActor
    func testInitialSourcesSettleForEmptyResultsAndFailedReads() async throws {
        for fails in [false, true] {
            let workResponse = fails ? "invalid json" : envelope("dashboard.snapshot", data: dashboard())
            let archiveResponse = fails ? "invalid json" : envelope("todo.list", data: "[]")
            let quotaResponse = envelope("quota.snapshot", data: #"{"agents":{}}"#)
            let (commandRunner, directory) = try runner { _ in
                """
                /bin/cat > /dev/null
                case "$2" in
                dashboard.snapshot) /usr/bin/printf '%s' \(quoted(workResponse)) ;;
                todo.list) /usr/bin/printf '%s' \(quoted(archiveResponse)) ;;
                *) /usr/bin/printf '%s' \(quoted(quotaResponse)) ;;
                esac
                """
            }
            defer { try? FileManager.default.removeItem(at: directory) }
            let store = ATMDataStore(makeDashboardRunner: { commandRunner })
            XCTAssertFalse(store.taskState.isInitialWorkSettled)
            XCTAssertFalse(store.taskState.isInitialArchiveSettled)
            store.refresh()
            await waitUntil {
                store.taskState.isInitialWorkSettled && store.taskState.isInitialArchiveSettled
            }
            XCTAssertTrue(store.allTodos.isEmpty)
            XCTAssertTrue(store.archivedTodos.isEmpty)
            if fails { XCTAssertNotNil(store.errorMessage) }
        }
    }

}
