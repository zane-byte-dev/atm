import XCTest

@testable import ATMMenuBarApp

final class CollectionSchedulePolicyTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 10_000)

    func testRecentRunForOneSourceDoesNotPostponeAnotherDueSource() {
        let due = source(id: "slow", interval: 15)
        let recent = source(id: "fast", interval: 5)
        let overview = overview(
            sources: [due, recent],
            runs: [
                run(sourceID: due.id, startedAt: 9_000),
                run(sourceID: recent.id, startedAt: 9_900),
            ]
        )

        XCTAssertTrue(
            ATMCollectionSchedulePolicy.shouldRun(
                overview,
                lastAttemptAt: nil,
                now: now
            )
        )
    }

    func testEachSourceKeepsItsOwnCadence() {
        let source = source(id: "hourly", interval: 60)
        let overview = overview(
            sources: [source],
            runs: [run(sourceID: source.id, startedAt: 9_000)]
        )

        XCTAssertFalse(
            ATMCollectionSchedulePolicy.shouldRun(
                overview,
                lastAttemptAt: nil,
                now: now
            )
        )
    }

    func testGlobalPollingIntervalStillThrottlesDueChecks() {
        let source = source(id: "new", interval: 5)
        let overview = overview(sources: [source], runs: [])

        XCTAssertFalse(
            ATMCollectionSchedulePolicy.shouldRun(
                overview,
                lastAttemptAt: now.addingTimeInterval(-60),
                now: now
            )
        )
        XCTAssertTrue(
            ATMCollectionSchedulePolicy.shouldRun(
                overview,
                lastAttemptAt: now.addingTimeInterval(-300),
                now: now
            )
        )
    }

    private func overview(
        sources: [ATMCollectionSource],
        runs: [ATMCollectionRun]
    ) -> ATMCollectionOverview {
        ATMCollectionOverview(
            enabled: true,
            intervalMinutes: 5,
            lookbackMinutes: 60,
            model: "test",
            connectorHealth: [],
            summary: .empty,
            sources: sources,
            runs: runs,
            items: [],
            digests: []
        )
    }

    private func source(id: String, interval: Int) -> ATMCollectionSource {
        ATMCollectionSource(
            id: id,
            connector: "test",
            kind: "group",
            externalID: id,
            name: id,
            project: nil,
            excludePattern: nil,
            instruction: nil,
            knowledgeCollection: nil,
            strategy: "tasks",
            decisionUnit: "window",
            intervalMinutes: interval,
            priority: "P2",
            enabled: true,
            muted: false,
            createdAt: 1,
            updatedAt: 1
        )
    }

    private func run(sourceID: String, startedAt: Int64) -> ATMCollectionRun {
        ATMCollectionRun(
            id: "run-\(sourceID)-\(startedAt)",
            connector: "test",
            sourceID: sourceID,
            status: "succeeded",
            startedAt: startedAt,
            finishedAt: startedAt + 1,
            fetchedCount: 0,
            analyzedCount: 0,
            createdCount: 0,
            appendedCount: 0,
            insightCount: 0,
            ignoredCount: 0,
            failedCount: 0,
            error: nil
        )
    }
}
