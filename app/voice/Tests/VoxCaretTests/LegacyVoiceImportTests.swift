import XCTest
@testable import VoxCaret

final class LegacyVoiceImportTests: XCTestCase {
    func testOnlyValidatedVoicePreferencesCanCrossDomains() {
        let imported = LegacyVoiceImport.validated([
            "ATMVoiceInputHotKeyEnabled": true,
            "ATMVoiceInputEngine": "senseVoice",
            "ATMVoiceInputLanguage": "invalid",
            "ATMVoiceInputOnDeviceOnly": "false",
            "ATMVoiceInputHotKey": "0:49",
            "ATMVoiceInputDictionary": "ATM=ATM",
            "LastTranscript": "sensitive text",
            "ATMDataDirectory": "/another/workspace",
        ])
        XCTAssertEqual(Set(imported.keys), ["VoxCaretInputHotKeyEnabled", "VoxCaretInputEngine", "VoxCaretInputDictionary"])
        XCTAssertFalse(LegacyVoiceImport.preferenceKeys.contains("LastTranscript"))
    }

    func testEveryLegacySourceHasAStableDomainAndModelLocation() {
        XCTAssertEqual(LegacyVoiceImport.sources.first?.domain, "dev.zanebyte.atm.voice")
        XCTAssertEqual(Set(LegacyVoiceImport.sources.map(\.domain)).count, LegacyVoiceImport.sources.count)

        for source in LegacyVoiceImport.sources {
            let path = LegacyVoiceImport.modelDirectory(for: source.domain)?.path ?? ""
            XCTAssertTrue(path.contains("Application Support"), path)
            XCTAssertTrue(path.hasSuffix("SenseVoiceSmall-int8-2024-07-17"), path)
        }
        XCTAssertNil(LegacyVoiceImport.modelDirectory(for: "unknown.product"))
    }

    func testModelCopyPublishesCompleteFilesAndPreservesSource() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let source = root.appendingPathComponent("legacy")
        let destination = root.appendingPathComponent("voice/model")
        try FileManager.default.createDirectory(at: source, withIntermediateDirectories: true)
        try Data("onnx bytes".utf8).write(to: source.appendingPathComponent("model.int8.onnx"))
        try Data("token bytes".utf8).write(to: source.appendingPathComponent("tokens.txt"))
        let valid: (VoxCaretSenseVoiceModelFiles) -> Bool = { FileManager.default.fileExists(atPath: $0.model.path) && FileManager.default.fileExists(atPath: $0.tokens.path) }
        XCTAssertTrue(try LegacyVoiceImport.copyModel(from: source, to: destination, validate: valid))
        XCTAssertEqual(try Data(contentsOf: source.appendingPathComponent("model.int8.onnx")), try Data(contentsOf: destination.appendingPathComponent("model.int8.onnx")))
        XCTAssertFalse(try LegacyVoiceImport.copyModel(from: source, to: destination, validate: valid))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: destination.deletingLastPathComponent().path), ["model"])
    }

    func testIncompleteAndSymlinkModelsNeverPublish() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let source = root.appendingPathComponent("legacy")
        let destination = root.appendingPathComponent("new")
        try FileManager.default.createDirectory(at: source, withIntermediateDirectories: true)
        try Data("model".utf8).write(to: source.appendingPathComponent("model.int8.onnx"))
        XCTAssertThrowsError(try LegacyVoiceImport.copyModel(from: source, to: destination))
        try FileManager.default.createSymbolicLink(at: source.appendingPathComponent("tokens.txt"), withDestinationURL: source.appendingPathComponent("model.int8.onnx"))
        XCTAssertThrowsError(try LegacyVoiceImport.copyModel(from: source, to: destination, validate: { _ in true }))
        XCTAssertFalse(FileManager.default.fileExists(atPath: destination.path))
    }

    func testVoicePackageHasOnlyVoiceHotKeyActions() {
        XCTAssertEqual(Set(VoxCaretHotKeyAction.allCases.map(\.rawValue)), [2, 3])
    }

    @MainActor func testCancelledInjectionStopsBeforeAccessingPasteboardOrTarget() async {
        do {
            _ = try await VoxCaretTextInjector.inject("must not paste", into: nil, isCurrent: { false })
            XCTFail("Cancelled recording must never inject")
        } catch is CancellationError { } catch { XCTFail("Unexpected \(error)") }
    }
}
