import XCTest
@testable import ATMMenuBarApp

final class ATMTaskPresentationTests: XCTestCase {
    private func todo(
        _ id: String,
        title: String = "Task",
        status: String = "open",
        project: String = "atm",
        created: String = "2026-09-03",
        closed: String? = nil,
        doneTS: Int64? = nil
    ) throws -> ATMTodo {
        var value: [String: Any] = [
            "id": id, "title": title, "status": status, "priority": "P2",
            "project": project, "created": created,
        ]
        if let closed { value["closed"] = closed }
        if let doneTS { value["done_ts"] = doneTS }
        return try JSONDecoder().decode(ATMTodo.self, from: JSONSerialization.data(withJSONObject: value))
    }

    private var today: Date {
        Calendar.current.date(from: DateComponents(year: 2026, month: 9, day: 3, hour: 12))!
    }

    func testSelectionsReuseDerivedCollectionsUntilTaskVersionChanges() throws {
        let cache = ATMTaskPresentationCache()
        var reads = 0
        let tasks = try [todo("t1"), todo("t2", status: "review")]
        let source = { reads += 1; return tasks }
        let first = cache.presentation(version: 1, now: today, todos: source, archived: { [] })
        for _ in 0..<100 {
            let result = cache.presentation(version: 1, now: today.addingTimeInterval(60), todos: source, archived: { [] })
            XCTAssertEqual(result.flattenedTodos.map(\.id), first.flattenedTodos.map(\.id))
            XCTAssertEqual(result.todosByID["t1"]?.id, "t1")
        }
        XCTAssertEqual(reads, 1, "Selection and unrelated publications must not reread or sort tasks")
        XCTAssertEqual(cache.rebuildCount, 1)

        let updated = try todo("t1", title: "Updated title", status: "in_progress")
        let next = cache.presentation(version: 2, now: today, todos: { [updated] }, archived: { [] })
        XCTAssertEqual(cache.rebuildCount, 2)
        XCTAssertEqual(next.todosByID["t1"]?.title, "Updated title")
        XCTAssertEqual(next.groups.map(\.id), ["working"])
        XCTAssertNil(next.todosByID["t2"])
    }

    func testDayBoundaryMovesCompletionToHistoryWithoutTaskMutation() throws {
        let cache = ATMTaskPresentationCache()
        let task = try todo("t9", status: "done", closed: "2026-08-28")
        let first = cache.presentation(version: 1, now: today, todos: { [task] }, archived: { [] })
        XCTAssertEqual(first.groups.map(\.id), ["done"])
        let tomorrow = Calendar.current.date(byAdding: .day, value: 1, to: today)!
        let next = cache.presentation(version: 1, now: tomorrow, todos: { [task] }, archived: { [] })
        XCTAssertEqual(next.groups.map(\.id), ["history"])
        XCTAssertEqual(cache.rebuildCount, 2)
    }

    func testCompletionOrderingAndArchiveIndexSurviveBucketSplit() throws {
        let tasks = try [
            todo("t99", status: "done", closed: "2026-09-03"),
            todo("t100", status: "done", closed: "2026-09-03"),
            todo("t2", status: "done", closed: "2026-09-03", doneTS: 200),
            todo("t8", status: "done", closed: "2026-08-20"),
            todo("t11", status: "review"),
        ]
        let archived = try [todo("t7", status: "done")]
        let value = ATMTaskPresentation(todos: tasks, archived: archived, now: today)
        XCTAssertEqual(value.groups.map(\.id), ["review", "done", "history", "archive"])
        XCTAssertEqual(value.groups.first { $0.id == "done" }?.todos.map(\.id), ["t2", "t100", "t99"])
        XCTAssertEqual(value.flattenedTodos.map(\.id), ["t11", "t2", "t100", "t99", "t8", "t7"])
        XCTAssertEqual(value.managedGroups.map(\.id), ["review", "working", "open", "done", "history", "archive"])
        XCTAssertEqual(value.archivedIDs, ["t7"])
    }

    func testOverlappingRestoreSnapshotsPreferCurrentTaskForSelection() throws {
        let current = try todo("t1", title: "Restored", status: "open")
        let archived = try todo("t1", title: "Archived", status: "done")
        let value = ATMTaskPresentation(todos: [current], archived: [archived], now: today)
        XCTAssertEqual(value.todosByID["t1"]?.title, "Restored")
    }

    func testProjectCandidatesCacheRecencyAndInvalidateWithTaskVersion() throws {
        let tasks = try [
            todo("t1", project: "old", created: "2026-08-01"),
            todo("t2", project: "atm"),
            todo("t3", project: "atm", created: "2026-09-01"),
        ]
        let cache = ATMTodoProjectCache()
        var reads = 0
        let source = { reads += 1; return tasks }
        for text in ["", "atm next", "old project urgent P1"] {
            let projects = cache.projects(version: 1, todos: source)
            XCTAssertEqual(
                ATMTodoSuggestion.infer(text: text, knownProjects: projects.ranked, liveProject: nil),
                ATMTodoSuggestion.infer(text: text, todos: tasks)
            )
        }
        XCTAssertEqual(reads, 1)
        XCTAssertEqual(cache.ranked, ["atm", "old"])
        XCTAssertEqual(cache.alphabetical, ["atm", "old"])
        let newest = try todo("t4", project: "new", created: "2026-09-04")
        _ = cache.projects(version: 2, todos: { tasks + [newest] })
        XCTAssertEqual(cache.ranked.first, "new")
        XCTAssertEqual(
            ATMTodoSuggestion.infer(text: "", knownProjects: cache.ranked, liveProject: "mox-atm").project,
            "atm"
        )
    }

    func testLargeHistorySelectionReusesSingleBuild() throws {
        let tasks = try (1...2_000).map { index in
            try todo(
                "t\(index)",
                status: index.isMultiple(of: 10) ? "in_progress" : "done",
                closed: index.isMultiple(of: 3) ? "2026-09-02" : "2026-08-01",
                doneTS: Int64(index)
            )
        }
        let cache = ATMTaskPresentationCache()
        let started = Date()
        let expected = cache.presentation(version: 1, now: today, todos: { tasks }, archived: { [] })
        let coldSeconds = Date().timeIntervalSince(started)
        let selectionStart = Date()
        for index in 1...2_000 {
            let value = cache.presentation(version: 1, now: today, todos: {
                XCTFail("Selecting cached tasks must not rebuild completed history")
                return tasks
            }, archived: { [] })
            XCTAssertEqual(value.todosByID["t\(index)"]?.id, "t\(index)")
        }
        let selectionSeconds = Date().timeIntervalSince(selectionStart)
        XCTAssertEqual(cache.rebuildCount, 1)
        XCTAssertEqual(expected.visibleTodos.count, 2_000)
        print("ATM task cache: 2,000 rows cold \(coldSeconds)s; 2,000 cached selections \(selectionSeconds)s; rebuilds \(cache.rebuildCount)")
    }

    func testArchiveFirstDoesNotChooseDefaultBeforeWorkArrives() throws {
        let archived = try todo("t330", status: "review")
        let archiveOnly = ATMTaskPresentation(todos: [], archived: [archived], now: today)
        let beforeWork = ATMTaskSelection.resolve(
            currentID: nil, in: archiveOnly, workSettled: false, archivesSettled: true
        )
        XCTAssertNil(beforeWork, "A fast archive response must not select or expand an archive group")

        let active = try todo("t355", status: "open")
        let loaded = ATMTaskPresentation(todos: [active], archived: [archived], now: today)
        let afterWork = ATMTaskSelection.resolve(
            currentID: beforeWork, in: loaded, workSettled: true, archivesSettled: true
        )
        XCTAssertEqual(afterWork, "t355", "Current work takes precedence even over an archived review task")
    }

    func testUserCanSelectLoadedArchiveWhileInitialWorkIsPending() throws {
        let archived = try todo("t330", status: "done")
        let archiveOnly = ATMTaskPresentation(todos: [], archived: [archived], now: today)
        let explicit = ATMTaskSelection.resolve(
            currentID: "t330", in: archiveOnly, workSettled: false, archivesSettled: true
        )
        XCTAssertEqual(explicit, "t330")

        let active = try todo("t355", status: "in_progress")
        let loaded = ATMTaskPresentation(todos: [active], archived: [archived], now: today)
        XCTAssertEqual(
            ATMTaskSelection.resolve(currentID: explicit, in: loaded, workSettled: true, archivesSettled: true),
            "t330", "Arrival of work must not override a user's intervening choice"
        )
    }

    func testWorkFirstPreservesArchiveDeepLinkUntilArchiveArrives() throws {
        let active = try todo("t355", status: "in_progress")
        let workOnly = ATMTaskPresentation(todos: [active], archived: [], now: today)
        let pending = ATMTaskSelection.resolve(
            currentID: "t330", in: workOnly, workSettled: true, archivesSettled: false
        )
        XCTAssertEqual(pending, "t330", "An unresolved link may belong to the unfinished archive request")
        let archived = try todo("t330", status: "done")
        let loaded = ATMTaskPresentation(todos: [active], archived: [archived], now: today)
        XCTAssertEqual(
            ATMTaskSelection.resolve(currentID: pending, in: loaded, workSettled: true, archivesSettled: true),
            "t330"
        )
        XCTAssertEqual(
            ATMTaskSelection.resolve(currentID: "t355", in: workOnly, workSettled: false, archivesSettled: false),
            "t355", "An already available explicit selection never waits on readiness flags"
        )
    }

    func testEmptyWorkCompletionAllowsArchiveDefaultWithoutChangedIDs() throws {
        let archived = try todo("t330", status: "done")
        let archiveOnly = ATMTaskPresentation(todos: [], archived: [archived], now: today)
        XCTAssertNil(ATMTaskSelection.resolve(
            currentID: nil, in: archiveOnly, workSettled: false, archivesSettled: true
        ))
        // The task IDs are unchanged: the initial-work settled notification is
        // what makes this selection possible for a successful empty response.
        XCTAssertEqual(ATMTaskSelection.resolve(
            currentID: nil, in: archiveOnly, workSettled: true, archivesSettled: true
        ), "t330")
    }

    func testMissingDeepLinkFallsBackAfterBothInitialReadsSettle() throws {
        let active = try todo("t355", status: "in_progress")
        let workOnly = ATMTaskPresentation(todos: [active], archived: [], now: today)
        XCTAssertEqual(ATMTaskSelection.resolve(
            currentID: "t999", in: workOnly, workSettled: true, archivesSettled: false
        ), "t999")
        XCTAssertEqual(ATMTaskSelection.resolve(
            currentID: "t999", in: workOnly, workSettled: true, archivesSettled: true
        ), "t355")
        let empty = ATMTaskPresentation(todos: [], archived: [], now: today)
        XCTAssertNil(ATMTaskSelection.resolve(
            currentID: "t999", in: empty, workSettled: true, archivesSettled: true
        ))
    }
}
