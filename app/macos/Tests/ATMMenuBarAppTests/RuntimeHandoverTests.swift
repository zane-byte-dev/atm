import Darwin
import XCTest
@testable import ATMMenuBarApp

final class RuntimeHandoverTests: XCTestCase {
    func testStoppedGoOwnerStillDisablesLegacyStore() throws {
        let suite = "ATMRuntimeHandoverTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }
        let marker = root.appendingPathComponent("presence-owner.json")
        XCTAssertEqual(ATMRuntimeHandover.resolve(environment: [:], defaults: defaults, marker: marker), .legacy)
        try Data(#"{"owner":"go","running":false,"expires_at":"2000-01-01T00:00:00Z"}"#.utf8).write(to: marker)
        XCTAssertEqual(ATMRuntimeHandover.resolve(environment: [:], defaults: defaults, marker: marker), .web)
        XCTAssertEqual(ATMRuntimeHandover.resolve(environment: ["ATM_VOICE_ONLY":"1"], defaults: defaults, marker: marker), .voiceOnly)
    }
    func testLegacyLeaseIsExclusiveBeforeStoreConstruction() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        var first: ATMLegacyRuntimeLease? = try ATMLegacyRuntimeLease(directory: root)
        XCTAssertThrowsError(try ATMLegacyRuntimeLease(directory: root))
        first = nil
        let second = try ATMLegacyRuntimeLease(directory: root)
        withExtendedLifetime(second) { XCTAssertNil(first) }
    }
    func testSecondListenerCannotUnlinkFirstAndStopIsOwnershipSafe() throws {
        let root = "/tmp/atm-owner-\(UUID().uuidString.prefix(8))"
        try FileManager.default.createDirectory(atPath: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: root) }
        let path = root + "/notch.sock"
        let first = ATMAgentEventListener(path: path) { _ in }
        try first.start()
        defer { first.stop() }
        let second = ATMAgentEventListener(path: path) { _ in }
        XCTAssertThrowsError(try second.start())
        second.stop()
        XCTAssertTrue(FileManager.default.fileExists(atPath: path))
        // Simulate a new socket path replacing ours after an external transition.
        try FileManager.default.moveItem(atPath: path, toPath: path + ".old")
        try Data("foreign".utf8).write(to: URL(fileURLWithPath: path))
        first.stop()
        XCTAssertEqual(try String(contentsOfFile: path), "foreign")
    }
}
