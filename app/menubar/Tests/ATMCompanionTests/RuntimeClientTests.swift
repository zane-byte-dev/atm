import XCTest
@testable import ATMCompanion

final class RuntimeClientTests: XCTestCase {
    func testOnlyExplicitLoopbackOriginCanReceiveControlToken() {
        XCTAssertNotNil(RuntimeInstance.validateOrigin("http://127.0.0.1:47321"))
        for value in ["https://example.com", "http://localhost:47321", "http://127.0.0.1:47321@evil.test", "http://user@127.0.0.1:47321", "http://127.0.0.1:47321/path", "http://127.0.0.1:47321?token=x", "http://127.0.0.1:47321#x", "http://127.0.0.1"] {
            XCTAssertNil(RuntimeInstance.validateOrigin(value), value)
        }
    }
    func testNotificationWireContractIncludesWithdrawAndStableIDs() throws {
        let json = #"{"snapshot":{"active_count":2,"attention_count":1},"feed":{"cursor":4,"notifications":[{"sequence":4,"id":"agent:one:attention","kind":"attention","action":"withdraw","title":"","object_id":"t1"}]}}"#
        let state = try JSONDecoder().decode(CompanionState.self, from: Data(json.utf8))
        XCTAssertEqual(state.snapshot.activeCount, 2)
        XCTAssertEqual(state.feed?.notifications?.first?.action, "withdraw")
        XCTAssertEqual(state.feed?.notifications?.first?.objectID, "t1")
    }
    func testMissingRuntimeReturnsActionableOfflineStateWithoutCreatingFiles() {
        let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        XCTAssertThrowsError(try RuntimeClient(dataDirectory: path).instance())
        XCTAssertFalse(FileManager.default.fileExists(atPath: path.path))
    }

    func testNotificationDestinationUsesKindAndRejectsUnknownObjectRoutes() {
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "guard", objectID: "ap_1"), .web(.home))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "collection_new", objectID: "ci_1"), .web(.collection))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "todo_review", objectID: "t9"), .web(.task("t9")))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "attention", objectID: "session:1"), .web(.agent("session:1")))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "attention", objectID: "t123"), .web(.agent("t123")))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "completed", objectID: "session:2"), .web(.agent("session:2")))
        XCTAssertEqual(CompanionNotificationDestination.resolve(kind: "unknown", objectID: "ap_1"), .web(.home))
    }

    func testInstanceRecordMustMatchSelectedDataDirectory() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let validJSON = "{\"schema_version\":1,\"origin\":\"http://127.0.0.1:47321\",\"instance_id\":\"instance_1\",\"data_dir\":\"\(directory.path)\"}"
        let instance = try JSONDecoder().decode(RuntimeInstance.self, from: Data(validJSON.utf8))
        XCTAssertTrue(instance.isValid(for: directory))

        let wrong = try JSONDecoder().decode(RuntimeInstance.self, from: Data(validJSON.replacingOccurrences(of: directory.path, with: directory.appendingPathComponent("other").path).utf8))
        XCTAssertFalse(wrong.isValid(for: directory))

        for invalidJSON in [
            validJSON.replacingOccurrences(of: "\"schema_version\":1", with: "\"schema_version\":2"),
            validJSON.replacingOccurrences(of: "\"instance_1\"", with: "\"\""),
            validJSON.replacingOccurrences(of: "\"instance_1\"", with: "\"bad instance\""),
            validJSON.replacingOccurrences(of: "\"instance_1\"", with: "\"\(String(repeating: "x", count: 129))\""),
        ] {
            let invalid = try JSONDecoder().decode(RuntimeInstance.self, from: Data(invalidJSON.utf8))
            XCTAssertFalse(invalid.isValid(for: directory), invalidJSON)
        }
    }

    func testControlTokenBoundsAndBusyCodeAreMachineReadable() {
        XCTAssertFalse(RuntimeClient.isValidControlToken(String(repeating: "x", count: 31)))
        XCTAssertTrue(RuntimeClient.isValidControlToken(String(repeating: "x", count: 32)))
        XCTAssertTrue(RuntimeClient.isValidControlToken(String(repeating: "x", count: 1024)))
        XCTAssertFalse(RuntimeClient.isValidControlToken(String(repeating: "x", count: 1025)))
        XCTAssertEqual(RuntimeClient.clientError(code: "busy", message: "this operation is already queued or running").localizedDescription, "已有同步进行中")
    }

    func testDisabledNotificationReadStillAdvancesCursorWithoutReplayingHistory() throws {
        let feed = try JSONDecoder().decode(NotificationFeed.self, from: Data(#"{"notifications":[],"cursor":42}"#.utf8))
        XCTAssertEqual(feed.advancedCursor(from: 7), 42)
        XCTAssertEqual(feed.advancedCursor(from: 99), 99)
    }
}
