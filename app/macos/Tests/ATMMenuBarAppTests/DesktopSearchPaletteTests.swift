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
