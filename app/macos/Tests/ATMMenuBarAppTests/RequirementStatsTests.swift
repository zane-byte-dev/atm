import Foundation
import XCTest
@testable import ATMMenuBarApp

final class RequirementStatsTests: XCTestCase {
    func testSummaryAndBucketsUseCompletionCalendarDates() throws {
        let snapshot = makeSnapshot(completions: [
            completion("t1", "2026-08-21", project: "atm"),
            completion("t2", "2026-08-20", project: "atm"),
            completion("t3", "2026-08-05", project: "other"),
            completion("t4", "2026-07-31", project: "other"),
        ])
        let now = try XCTUnwrap(ATMUsageDateAxis.date(from: "2026-08-21 12:00"))

        let summary = snapshot.requirementSummary(now: now)
        XCTAssertEqual(summary.today, 1)
        XCTAssertEqual(summary.thisWeek, 2)
        XCTAssertEqual(summary.thisMonth, 3)

        let days = snapshot.requirementBuckets(granularity: .day, now: now)
        XCTAssertEqual(days.count, 14)
        XCTAssertEqual(days.last?.completed, 1)

        let projects = snapshot.requirementProjects(granularity: .month, now: now)
        XCTAssertEqual(projects.map(\.project), ["atm", "other"])
        XCTAssertEqual(projects.map(\.completed), [2, 1])

        let filteredSummary = snapshot.requirementSummary(project: "other", now: now)
        XCTAssertEqual(filteredSummary.today, 0)
        XCTAssertEqual(filteredSummary.thisWeek, 0)
        XCTAssertEqual(filteredSummary.thisMonth, 1)

        let filteredDays = snapshot.requirementBuckets(granularity: .day, project: "atm", now: now)
        XCTAssertEqual(filteredDays.last?.completed, 1)
        XCTAssertEqual(snapshot.requirementProjectOptions(), ["atm", "other"])
        XCTAssertEqual(
            snapshot.recentTodoCompletions(project: "other").map(\.todoID),
            ["t3", "t4"]
        )
    }

    private func completion(
        _ id: String,
        _ date: String,
        project: String
    ) -> ATMTodoCompletion {
        ATMTodoCompletion(
            todoID: id,
            title: id,
            project: project,
            priority: "P1",
            creator: "me",
            createdDate: "2026-08-01",
            completedDate: date,
            completedTS: 0
        )
    }

    private func makeSnapshot(completions: [ATMTodoCompletion]) -> ATMDashboardSnapshot {
        ATMDashboardSnapshot(
            work: .empty,
            dayStats: [],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            todoCompletions: completions,
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )
    }
}
