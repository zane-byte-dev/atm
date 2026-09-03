import AppKit
import SwiftUI
import XCTest
@testable import ATMMenuBarApp

final class DesktopSearchPaletteTests: XCTestCase {
    func testSearchResultPolicyKeepsSectionsCompact() {
        XCTAssertEqual(ATMSearchResultPolicy.top(Array(0..<20)), Array(0..<6))
        XCTAssertEqual(ATMSearchResultPolicy.top([1, 2]), [1, 2])
    }

    func testSearchSelectionMovesAndClampsToAvailableResults() {
        XCTAssertEqual(ATMSearchSelection.movedIndex(current: 0, resultCount: 4, step: 1), 1)
        XCTAssertEqual(ATMSearchSelection.movedIndex(current: 3, resultCount: 4, step: 1), 3)
        XCTAssertEqual(ATMSearchSelection.movedIndex(current: 0, resultCount: 4, step: -1), 0)
        XCTAssertEqual(ATMSearchSelection.movedIndex(current: 2, resultCount: 4, step: -1), 1)
        XCTAssertEqual(ATMSearchSelection.movedIndex(current: 7, resultCount: 0, step: 1), 0)
    }

    func testProgressiveSearchCompletesEachDomainWithoutHidingOtherPendingDomains() {
        var progress = ATMSearchProgress()
        let request = progress.begin(query: "ATM")

        XCTAssertTrue(progress.complete(.tasks, requestID: request, error: nil))
        XCTAssertEqual(progress.resultQueries[.tasks], "ATM")
        XCTAssertFalse(progress.pending.contains(.tasks))
        XCTAssertTrue(progress.isSearching, "快速任务结果到达后，慢搜索域仍应显示加载状态")
        XCTAssertTrue(progress.complete(.documents, requestID: request, error: "查询失败"))
        XCTAssertEqual(progress.errorMessage, "知识：查询失败")
        XCTAssertTrue(progress.isSearching)
        progress.complete(.memories, requestID: request, error: nil)
        progress.complete(.sessions, requestID: request, error: nil)
        XCTAssertFalse(progress.isSearching)
    }

    func testProgressiveSearchRetainsResultQueryAndRejectsOlderRetryResults() {
        var progress = ATMSearchProgress()
        let first = progress.begin(query: "旧查询")
        progress.complete(.tasks, requestID: first, error: nil)
        let second = progress.begin(query: "新查询")
        XCTAssertEqual(progress.previousQuery(for: .tasks), "旧查询")
        XCTAssertFalse(progress.complete(.sessions, requestID: first, error: nil))
        XCTAssertNil(progress.resultQueries[.sessions])

        let retry = progress.begin(query: "新查询")
        XCTAssertFalse(progress.accepts(second, query: "新查询"))
        XCTAssertFalse(progress.complete(.tasks, requestID: second, error: nil))
        XCTAssertTrue(progress.complete(.tasks, requestID: retry, error: nil))
        XCTAssertNil(progress.previousQuery(for: .tasks))
        XCTAssertEqual(progress.resultQueries[.tasks], "新查询")

        progress.begin(query: "")
        XCTAssertFalse(progress.isSearching)
        XCTAssertTrue(progress.resultQueries.isEmpty)
        XCTAssertFalse(progress.complete(.memories, requestID: retry, error: nil))
    }

    func testProgressiveResultsPreserveKeyboardSelectionWhenEarlierDomainArrives() {
        XCTAssertEqual(
            ATMSearchSelection.reconciledIndex(
                current: 1, selectedAnchor: "session:s2", anchors: ["task:t1", "session:s1", "session:s2"]
            ),
            2
        )
        XCTAssertEqual(
            ATMSearchSelection.reconciledIndex(current: 5, selectedAnchor: "removed", anchors: ["task:t1"]),
            0
        )
    }

    @MainActor
    func testSearchTextFieldRoutesArrowReturnAndEscapeCommands() {
        var query = "ATM"
        var focused = true
        var moves: [Int] = []
        var submitCount = 0
        var cancelCount = 0
        let field = ATMSearchTextField(
            text: Binding(get: { query }, set: { query = $0 }),
            isFocused: Binding(get: { focused }, set: { focused = $0 }),
            placeholder: "搜索",
            onMove: { moves.append($0) },
            onSubmit: { submitCount += 1 },
            onCancel: { cancelCount += 1 }
        )
        let coordinator = field.makeCoordinator()
        let control = NSTextField()
        let editor = NSTextView()

        XCTAssertTrue(coordinator.control(
            control,
            textView: editor,
            doCommandBy: #selector(NSResponder.moveDown(_:))
        ))
        XCTAssertTrue(coordinator.control(
            control,
            textView: editor,
            doCommandBy: #selector(NSResponder.moveUp(_:))
        ))
        XCTAssertTrue(coordinator.control(
            control,
            textView: editor,
            doCommandBy: #selector(NSResponder.insertNewline(_:))
        ))
        XCTAssertTrue(coordinator.control(
            control,
            textView: editor,
            doCommandBy: #selector(NSResponder.cancelOperation(_:))
        ))
        XCTAssertFalse(coordinator.control(
            control,
            textView: editor,
            doCommandBy: #selector(NSResponder.moveLeft(_:))
        ))

        XCTAssertEqual(moves, [1, -1])
        XCTAssertEqual(submitCount, 1)
        XCTAssertEqual(cancelCount, 1)
    }
}
