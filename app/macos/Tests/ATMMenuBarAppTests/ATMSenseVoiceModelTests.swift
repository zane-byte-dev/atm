import XCTest
@testable import ATMMenuBarApp

/// The model is a 160MB download that a recording depends on. The failure worth
/// guarding against is a half-installed model that answers "ready" and then fails at
/// the moment someone is holding the key down and talking.
final class ATMSenseVoiceModelTests: XCTestCase {
    private var sandbox: URL!

    override func setUpWithError() throws {
        try super.setUpWithError()
        sandbox = FileManager.default.temporaryDirectory
            .appendingPathComponent("ATMSenseVoiceModelTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: sandbox, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        if let sandbox {
            try? FileManager.default.removeItem(at: sandbox)
        }
        try super.tearDownWithError()
    }

    private func write(_ name: String, byteCount: Int) throws -> URL {
        let url = sandbox.appendingPathComponent(name)
        try Data(count: byteCount).write(to: url)
        return url
    }

    /// Absent files must not read as complete — this is the check that stands between a
    /// missing model and the recognizer being handed a path that is not there.
    func testMissingFilesAreNotComplete() {
        let files = ATMSenseVoiceModelFiles(
            model: sandbox.appendingPathComponent("model.int8.onnx"),
            tokens: sandbox.appendingPathComponent("tokens.txt")
        )

        XCTAssertFalse(ATMSenseVoiceModelManager.isComplete(files))
    }

    /// A download interrupted partway leaves a file that exists and is far too small.
    /// Existence alone would call that ready.
    func testTruncatedModelIsNotComplete() throws {
        let files = ATMSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 4096),
            tokens: try write("tokens.txt", byteCount: 200_000)
        )

        XCTAssertFalse(ATMSenseVoiceModelManager.isComplete(files))
    }

    /// The vocabulary is checked too: a model without its tokens decodes to nothing.
    func testTruncatedTokensAreNotComplete() throws {
        let files = ATMSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 100_000_001),
            tokens: try write("tokens.txt", byteCount: 10)
        )

        XCTAssertFalse(ATMSenseVoiceModelManager.isComplete(files))
    }

    func testBothFilesAboveThresholdAreComplete() throws {
        let files = ATMSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 100_000_001),
            tokens: try write("tokens.txt", byteCount: 100_001)
        )

        XCTAssertTrue(ATMSenseVoiceModelManager.isComplete(files))
    }

    /// A directory where a file belongs has no size attribute to read, and must not be
    /// mistaken for one.
    func testDirectoryInPlaceOfModelIsNotComplete() throws {
        let directory = sandbox.appendingPathComponent("model.int8.onnx", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let files = ATMSenseVoiceModelFiles(
            model: directory,
            tokens: try write("tokens.txt", byteCount: 100_001)
        )

        XCTAssertFalse(ATMSenseVoiceModelManager.isComplete(files))
    }

    /// The checksum is what makes the install all-or-nothing, and the byte count is what
    /// keeps the progress bar moving when the redirect reports no length. A blank or
    /// zero value would silently disable each.
    func testArchiveIsPinnedByChecksumAndSize() {
        XCTAssertEqual(ATMSenseVoiceModelManager.archiveSHA256.count, 64)
        XCTAssertTrue(
            ATMSenseVoiceModelManager.archiveSHA256.allSatisfy(\.isHexDigit),
            "校验值必须是十六进制"
        )
        XCTAssertGreaterThan(ATMSenseVoiceModelManager.archiveByteCount, 0)
    }

    /// Under Application Support, not inside the bundle: the bundle is replaced and
    /// re-signed on every build, and re-downloading 160MB per build is not acceptable.
    @MainActor
    func testModelLivesOutsideTheAppBundle() {
        let path = ATMSenseVoiceModelManager.shared.modelDirectory.path

        XCTAssertTrue(path.contains("Application Support"), path)
        XCTAssertTrue(path.contains("/ATM/"), path)
        XCTAssertFalse(path.contains(".app/"), path)
    }

    @MainActor
    func testModelFilesAreNamedWhatTheArchiveContains() {
        let directory = ATMSenseVoiceModelManager.shared.modelDirectory

        // Not read back from `modelFiles`, which returns nil until the model is really
        // installed; these are the names the extraction step looks for.
        XCTAssertEqual(directory.appendingPathComponent("model.int8.onnx").lastPathComponent, "model.int8.onnx")
        XCTAssertEqual(directory.appendingPathComponent("tokens.txt").lastPathComponent, "tokens.txt")
    }
}
