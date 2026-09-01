import SwiftUI
import UniformTypeIdentifiers
import XCTest
@testable import ATMMenuBarApp

final class ATMManualOrderTests: XCTestCase {
    private struct Item: Equatable {
        let id: String
    }

    func testStoredOrderWinsAndNewValuesAppendInFallbackOrder() {
        let values = [Item(id: "inbox"), Item(id: "alpha"), Item(id: "beta")]
        let stored = ATMManualOrder.encode(["beta", "deleted", "inbox"])

        XCTAssertEqual(
            ATMManualOrder.ordered(values, stored: stored, id: \.id),
            [Item(id: "beta"), Item(id: "inbox"), Item(id: "alpha")]
        )
    }

    func testInvalidAndDuplicateStoredIDsAreReconciled() {
        XCTAssertEqual(
            ATMManualOrder.reconciledIDs(
                stored: "[\"b\",\"b\",\"missing\"]",
                fallback: ["a", "b", "c"]
            ),
            ["b", "a", "c"]
        )
        XCTAssertEqual(
            ATMManualOrder.reconciledIDs(stored: "not-json", fallback: ["a", "b"]),
            ["a", "b"]
        )
    }

    /// A catalog that repeats an ID is malformed, but ordering it must degrade to
    /// dropping the duplicate rather than trapping the whole app.
    func testDuplicateValueIDsCollapseInsteadOfTrapping() {
        let values = [Item(id: "a"), Item(id: "b"), Item(id: "a")]
        XCTAssertEqual(
            ATMManualOrder.ordered(values, stored: "", id: \.id),
            [Item(id: "a"), Item(id: "b")]
        )
    }

    func testMovingDownPlacesItemAfterEnteredRow() {
        let moved = ATMManualOrder.moving(
            "a",
            over: "c",
            stored: "",
            fallback: ["a", "b", "c", "d"]
        )
        XCTAssertEqual(ATMManualOrder.decode(moved), ["b", "c", "a", "d"])
    }

    func testMovingUpPlacesItemBeforeEnteredRow() {
        let moved = ATMManualOrder.moving(
            "d",
            over: "b",
            stored: "",
            fallback: ["a", "b", "c", "d"]
        )
        XCTAssertEqual(ATMManualOrder.decode(moved), ["a", "d", "b", "c"])
    }

    /// The name-sorted fallback has to stay dynamic until a real move happens:
    /// persisting it on a no-op would freeze the sort behind the user's back.
    func testUnresolvableMoveLeavesStoredOrderUntouched() {
        XCTAssertEqual(
            ATMManualOrder.moving("gone", over: "b", stored: "", fallback: ["a", "b"]),
            ""
        )
        XCTAssertEqual(
            ATMManualOrder.moving("a", over: "gone", stored: "", fallback: ["a", "b"]),
            ""
        )
        let stored = ATMManualOrder.encode(["b", "a"])
        XCTAssertEqual(
            ATMManualOrder.moving("a", over: "a", stored: stored, fallback: ["a", "b"]),
            stored
        )
    }

    /// The row ID has to ride along as plain text and nothing more exotic: the
    /// drop side only ever matches `public.utf8-plain-text`, and a provider it
    /// cannot match is a row no drop target accepts — which is how this gesture
    /// dies, silently and completely.
    ///
    /// Deliberately asserts nothing about ATM being able to recognise its own
    /// drag. Every marker tried so far survives a provider built here and is gone
    /// by the time a real drop reads it, so a test asserting otherwise passes
    /// while the app cannot be dragged at all. See `ATMManualOrder.itemProvider`.
    func testItemProviderCarriesTheRowIDAsPlainTextOnly() {
        let provider = ATMManualOrder.itemProvider(for: "inbox")
        XCTAssertEqual(provider.registeredTypeIdentifiers, [UTType.utf8PlainText.identifier])
        XCTAssertTrue(provider.canLoadObject(ofClass: NSString.self))
    }

    @MainActor
    func testDropIsRefusedUntilARowStartsTheDrag() {
        var dragged: String?
        let delegate = ATMManualOrderDropDelegate(
            targetID: "b",
            draggedID: Binding(get: { dragged }, set: { dragged = $0 }),
            move: { _, _ in XCTFail("no drag is in flight") }
        )

        XCTAssertFalse(delegate.acceptsDrop, "no ATM drag is in flight")
        XCTAssertNil(delegate.pendingMoveSource)
    }

    @MainActor
    func testDropEnteredMovesTheDraggedRowButNotItself() {
        var dragged: String? = "a"
        let binding = Binding(get: { dragged }, set: { dragged = $0 })
        var moves: [(String, String)] = []

        let overOther = ATMManualOrderDropDelegate(
            targetID: "b",
            draggedID: binding,
            move: { moves.append(($0, $1)) }
        )
        XCTAssertTrue(overOther.acceptsDrop)
        XCTAssertEqual(overOther.pendingMoveSource, "a")

        let overSelf = ATMManualOrderDropDelegate(
            targetID: "a",
            draggedID: binding,
            move: { moves.append(($0, $1)) }
        )
        XCTAssertTrue(overSelf.acceptsDrop)
        XCTAssertNil(overSelf.pendingMoveSource, "hovering the dragged row is a no-op")
    }
}
