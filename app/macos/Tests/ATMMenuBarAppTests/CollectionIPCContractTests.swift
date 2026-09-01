import XCTest
@testable import ATMMenuBarApp

final class CollectionIPCContractTests: XCTestCase {
    func testCollectionUsesOnlyItsDeclaredTypedMethods() {
        let methods: [(String, [String])] = [
            (ATMCollectionIPCCommand.snapshot.verb, ATMCollectionIPCCommand.snapshot.arguments),
            (ATMCollectionIPCCommand.run.verb, ATMCollectionIPCCommand.run.arguments),
            (ATMCollectionIPCCommand.history.verb, ATMCollectionIPCCommand.history.arguments),
            (ATMCollectionIPCCommand.saveSource.verb, ATMCollectionIPCCommand.saveSource.arguments),
            (ATMCollectionIPCCommand.searchSources.verb, ATMCollectionIPCCommand.searchSources.arguments),
            (ATMCollectionIPCCommand.setSourceEnabled.verb, ATMCollectionIPCCommand.setSourceEnabled.arguments),
            (ATMCollectionIPCCommand.setSourceMuted.verb, ATMCollectionIPCCommand.setSourceMuted.arguments),
            (ATMCollectionIPCCommand.deleteSource.verb, ATMCollectionIPCCommand.deleteSource.arguments),
            (ATMCollectionIPCCommand.reprocessItem.verb, ATMCollectionIPCCommand.reprocessItem.arguments),
            (ATMCollectionIPCCommand.saveConclusion.verb, ATMCollectionIPCCommand.saveConclusion.arguments),
            (ATMCollectionIPCCommand.promoteItem.verb, ATMCollectionIPCCommand.promoteItem.arguments),
            (ATMCollectionIPCCommand.correctItem.verb, ATMCollectionIPCCommand.correctItem.arguments),
            (ATMCollectionIPCCommand.revertItem.verb, ATMCollectionIPCCommand.revertItem.arguments),
            (ATMCollectionIPCCommand.setItemsRead.verb, ATMCollectionIPCCommand.setItemsRead.arguments),
            (ATMCollectionIPCCommand.setItemsArchived.verb, ATMCollectionIPCCommand.setItemsArchived.arguments),
            (ATMCollectionIPCCommand.deleteItems.verb, ATMCollectionIPCCommand.deleteItems.arguments),
        ]
        XCTAssertEqual(Set(methods.map(\.0)), [
            "collect.snapshot", "collect.run", "collect.history",
            "collect.source.save", "collect.source.search", "collect.source.enabled",
            "collect.source.muted", "collect.source.delete",
            "collect.item.reprocess", "collect.item.save_conclusion", "collect.item.promote",
            "collect.item.correct", "collect.item.revert", "collect.item.read",
            "collect.item.archive", "collect.item.delete",
        ])
        for (verb, arguments) in methods {
            XCTAssertEqual(arguments, ["_ipc", verb])
        }
    }

    func testCollectionRequestsCarryIntentInsteadOfArgv() throws {
        let encoder = JSONEncoder()
        let save = try object(encoder.encode(ATMCollectionSourceSaveRequest(
            connector: "example", kind: "group", externalID: "g1", name: "产品群",
            project: "atm", excludePattern: "bot", instruction: "只看需求",
            knowledgeCollection: "shared", strategy: "tasks", decisionUnit: "window",
            intervalMinutes: 5, priority: "P1", enabled: true
        )))
        XCTAssertEqual(save["external_id"] as? String, "g1")
        XCTAssertNil(save["arguments"])
        XCTAssertNil(save["action"])

        let read = try object(encoder.encode(ATMCollectionItemsReadRequest(
            itemIDs: ["ci1", "ci2"], all: false, read: true
        )))
        XCTAssertEqual(read["item_ids"] as? [String], ["ci1", "ci2"])
        XCTAssertEqual(read["read"] as? Bool, true)

        // 批量了结靠 `all` 而不是把台账里的 ID 抄一遍发过去：范围由 Go 判定，App 只表达
        // 意图。这里也钉住「批量只关不开」——`all` 永远配 `archived: true`。
        let settleAll = try object(encoder.encode(ATMCollectionItemsArchivedRequest(
            itemIDs: [], all: true, archived: true
        )))
        XCTAssertEqual(settleAll["all"] as? Bool, true)
        XCTAssertEqual(settleAll["archived"] as? Bool, true)
        XCTAssertEqual(settleAll["item_ids"] as? [String], [])
        let settleOne = try object(encoder.encode(ATMCollectionItemsArchivedRequest(
            itemIDs: ["ci1"], all: false, archived: false
        )))
        XCTAssertEqual(settleOne["all"] as? Bool, false)
        XCTAssertEqual(settleOne["item_ids"] as? [String], ["ci1"])

        let correction = try object(encoder.encode(ATMCollectionCorrectRequest(
            itemID: "ci1",
            correction: ATMCollectionItemCorrectionRequest(
                title: "修复发布", project: "atm", priority: "P1"
            )
        )))
        XCTAssertEqual(correction["item_id"] as? String, "ci1")
        XCTAssertNotNil(correction["correction"] as? [String: Any])
        XCTAssertNil(correction["action"])

        let sourceDelete = try object(encoder.encode(
            ATMCollectionSourceDeleteRequest(sourceID: "cs1", confirmed: true)
        ))
        let itemDelete = try object(encoder.encode(
            ATMCollectionItemsDeleteRequest(itemIDs: ["ci1"], confirmed: true)
        ))
        let revert = try object(encoder.encode(
            ATMCollectionRevertRequest(itemID: "ci1", confirmed: true)
        ))
        XCTAssertEqual(sourceDelete["confirmed"] as? Bool, true)
        XCTAssertEqual(itemDelete["confirmed"] as? Bool, true)
        XCTAssertEqual(revert["confirmed"] as? Bool, true)
    }

    func testSnapshotEnvelopeDecodesTheExistingWorkspaceModel() throws {
        let data = Data(
            #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-collect","verb":"collect.snapshot","data":{"enabled":true,"interval_minutes":5,"lookback_minutes":60,"message_retention_days":90,"model":"test","connector_health":[],"summary":{"sources":0,"enabled_sources":0,"fetched_today":0,"created_today":0,"appended_today":0,"insight_today":0,"ignored_today":0,"failed_today":0,"unread_count":0},"sources":[],"runs":[],"items":[],"digests":[]}}"#.utf8
        )
        let envelope = try JSONDecoder().decode(ATMIPCEnvelope<ATMCollectionOverview>.self, from: data)
        XCTAssertEqual(envelope.verb, "collect.snapshot")
        XCTAssertTrue(envelope.data.enabled)
        XCTAssertEqual(envelope.data.items, [])
        // 这份 payload 是「没有 settleable_count 的旧 CLI」。读成 nil 而不是 0 说明不了
        // 什么，读成 nil 的用处是「全部了结」按钮不出现——按了也没有对应的 CLI 能接。
        XCTAssertNil(envelope.data.summary.settleableCount)
    }

    private func object(_ data: Data) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }
}
