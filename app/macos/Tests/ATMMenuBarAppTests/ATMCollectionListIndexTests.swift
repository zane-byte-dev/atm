import Combine
import XCTest
@testable import ATMMenuBarApp

final class ATMCollectionListIndexTests: XCTestCase {
    private func item(
        _ id: String, action: String = "create", todoID: String? = nil,
        sourceID: String = "s1", time: Int = 10, archived: Bool = false, readAt: Int = 0
    ) throws -> ATMCollectionItem {
        var json: [String: Any] = [
            "id": id, "source_id": sourceID, "connector": "c", "fingerprint": id,
            "message_ids": [id], "action": action, "status": "processed",
            "created_at": time, "updated_at": time, "read_at": readAt,
            "archived_at": archived ? 50 : 0,
        ]
        json["todo_id"] = todoID
        return try JSONDecoder().decode(ATMCollectionItem.self, from: JSONSerialization.data(withJSONObject: json))
    }

    private func source(_ id: String) throws -> ATMCollectionSource {
        let json: [String: Any] = [
            "id": id, "connector": "c", "kind": "group", "external_id": id,
            "priority": "P2", "enabled": true, "created_at": 1, "updated_at": 1,
        ]
        return try JSONDecoder().decode(ATMCollectionSource.self, from: JSONSerialization.data(withJSONObject: json))
    }

    func testIndexPreservesSupplementFamiliesOrderingAndUnreadSemantics() throws {
        let items = try [
            item("create", todoID: "t1", readAt: 10),
            item("z-late", action: "append", todoID: "t1", time: 30),
            item("b-early", action: "append", todoID: "t1", time: 20),
            item("a-early", action: "append", todoID: "t1", time: 20, readAt: 10),
            item("standalone", action: "append", todoID: "external"),
            item("archived-create", todoID: "t2", archived: true),
            item("active-append", action: "append", todoID: "t2"),
            item("archived-append", action: "append", todoID: "t2", archived: true),
            item("no-todo", action: "append"),
        ]
        let index = ATMCollectionListIndex(items: items)
        XCTAssertEqual(Set(index.itemsByID.keys), Set(ATMCollectionItemGrouping.visibleItems(items).map(\.id)))
        XCTAssertEqual(index.supplementsByItemID["create"]?.map(\.id), ["a-early", "b-early", "z-late"])
        XCTAssertEqual(index.unreadCountsByItemID["create"], 2)
        XCTAssertEqual(index.supplementsByItemID["archived-create"]?.map(\.id), ["archived-append"])
        XCTAssertNotNil(index.itemsByID["active-append"])
        for visible in index.itemsByID.values {
            XCTAssertEqual(index.supplementsByItemID[visible.id], ATMCollectionItemGrouping.supplements(for: visible, in: items))
            XCTAssertEqual(index.unreadCountsByItemID[visible.id], ATMCollectionItemGrouping.unreadCount(for: visible, in: items))
        }
    }

    func testSourceOrderUnknownRecordsAndSelectionMatchPresentation() throws {
        let items = try [
            item("s1-record"),
            item("orphan", sourceID: "deleted"),
            item("closed", archived: true),
            item("s2-record", sourceID: "s2"),
        ]
        let index = ATMCollectionListIndex(
            items: items, sources: try [source("s1"), source("s2")],
            sourceOrder: ATMManualOrder.encode(["s2", "s1"])
        )
        XCTAssertEqual(index.flattenedItems.map(\.id), ["s2-record", "s1-record", "orphan", "closed"])
        XCTAssertEqual(index.primaryItemsBySource["s1"]?.map(\.id), ["s1-record"])
        XCTAssertEqual(index.unknownItems.map(\.id), ["orphan"])
        XCTAssertEqual(index.selectedItem(id: nil, flat: true, showingIgnored: false)?.id, "s2-record")
        XCTAssertEqual(index.selectedItem(id: nil, flat: false, showingIgnored: false)?.id, "s1-record")
        XCTAssertEqual(index.selectedItem(id: "closed", flat: false, showingIgnored: false)?.id, "s1-record")
        XCTAssertEqual(index.selectedItem(id: "closed", flat: false, showingIgnored: true)?.id, "closed")
        XCTAssertEqual(index.selectedItem(id: "closed", flat: true, showingIgnored: false)?.id, "closed")
    }

    func testCacheIgnoresUnchangedSnapshotButUpdatesReadStateWithSameIDs() throws {
        let cache = ATMCollectionListIndexStore()
        var publications = 0
        let subscription = cache.$index.sink { _ in publications += 1 }
        let unread = try item("record", todoID: "t1")
        cache.update(items: [unread], sources: [], sourceOrder: "")
        let afterInitial = publications
        cache.update(items: [unread], sources: [], sourceOrder: "")
        XCTAssertEqual(publications, afterInitial)
        XCTAssertEqual(cache.index.unreadCountsByItemID["record"], 1)

        let read = try item("record", todoID: "t1", readAt: 20)
        cache.update(items: [read], sources: [], sourceOrder: "")
        XCTAssertEqual(publications, afterInitial + 1)
        XCTAssertEqual(cache.index.unreadCountsByItemID["record"], 0)
        withExtendedLifetime(subscription) {}
    }
}
