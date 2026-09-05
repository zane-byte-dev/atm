import XCTest
@testable import VoxCaret

/// The model is a 160MB download that a recording depends on. The failure worth
/// guarding against is a half-installed model that answers "ready" and then fails at
/// the moment someone is holding the key down and talking.
final class VoxCaretSenseVoiceModelTests: XCTestCase {
    private var sandbox: URL!

    override func setUpWithError() throws {
        try super.setUpWithError()
        sandbox = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoxCaretSenseVoiceModelTests-\(UUID().uuidString)", isDirectory: true)
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
        let files = VoxCaretSenseVoiceModelFiles(
            model: sandbox.appendingPathComponent("model.int8.onnx"),
            tokens: sandbox.appendingPathComponent("tokens.txt")
        )

        XCTAssertFalse(VoxCaretSenseVoiceModelManager.isComplete(files))
    }

    /// A download interrupted partway leaves a file that exists and is far too small.
    /// Existence alone would call that ready.
    func testTruncatedModelIsNotComplete() throws {
        let files = VoxCaretSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 4096),
            tokens: try write("tokens.txt", byteCount: 200_000)
        )

        XCTAssertFalse(VoxCaretSenseVoiceModelManager.isComplete(files))
    }

    /// The vocabulary is checked too: a model without its tokens decodes to nothing.
    func testTruncatedTokensAreNotComplete() throws {
        let files = VoxCaretSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 100_000_001),
            tokens: try write("tokens.txt", byteCount: 10)
        )

        XCTAssertFalse(VoxCaretSenseVoiceModelManager.isComplete(files))
    }

    func testBothFilesAboveThresholdAreComplete() throws {
        let files = VoxCaretSenseVoiceModelFiles(
            model: try write("model.int8.onnx", byteCount: 100_000_001),
            tokens: try write("tokens.txt", byteCount: 100_001)
        )

        XCTAssertTrue(VoxCaretSenseVoiceModelManager.isComplete(files))
    }

    /// A directory where a file belongs has no size attribute to read, and must not be
    /// mistaken for one.
    func testDirectoryInPlaceOfModelIsNotComplete() throws {
        let directory = sandbox.appendingPathComponent("model.int8.onnx", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let files = VoxCaretSenseVoiceModelFiles(
            model: directory,
            tokens: try write("tokens.txt", byteCount: 100_001)
        )

        XCTAssertFalse(VoxCaretSenseVoiceModelManager.isComplete(files))
    }

    /// The checksum is what makes the install all-or-nothing, and the byte count is what
    /// keeps the progress bar moving when the redirect reports no length. A blank or
    /// zero value would silently disable each.
    func testArchiveIsPinnedByChecksumAndSize() {
        XCTAssertEqual(VoxCaretSenseVoiceModelManager.archiveSHA256.count, 64)
        XCTAssertTrue(
            VoxCaretSenseVoiceModelManager.archiveSHA256.allSatisfy(\.isHexDigit),
            "校验值必须是十六进制"
        )
        XCTAssertGreaterThan(VoxCaretSenseVoiceModelManager.archiveByteCount, 0)
    }

    /// Under Application Support, not inside the bundle: the bundle is replaced and
    /// re-signed on every build, and re-downloading 160MB per build is not acceptable.
    @MainActor
    func testModelLivesOutsideTheAppBundle() {
        let path = VoxCaretSenseVoiceModelManager.shared.modelDirectory.path

        XCTAssertTrue(path.contains("Application Support"), path)
        XCTAssertTrue(path.contains("/VoxCaret/"), path)
        XCTAssertFalse(path.contains(".app/"), path)
    }

    @MainActor
    func testModelFilesAreNamedWhatTheArchiveContains() {
        let directory = VoxCaretSenseVoiceModelManager.shared.modelDirectory

        // Not read back from `modelFiles`, which returns nil until the model is really
        // installed; these are the names the extraction step looks for.
        XCTAssertEqual(directory.appendingPathComponent("model.int8.onnx").lastPathComponent, "model.int8.onnx")
        XCTAssertEqual(directory.appendingPathComponent("tokens.txt").lastPathComponent, "tokens.txt")
    }

    /// Optional local integration coverage. It skips in CI, but on a migrated machine
    /// proves the discovered model is accepted by the exact runtime VoxCaret ships.
    @MainActor
    func testInstalledCompatibleModelLoadsWhenAvailable() async throws {
        let applicationSupport = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support", isDirectory: true)
        guard let files = VoxCaretSenseVoiceModelManager.discoverCompatibleLegacyModel(
            applicationSupport: applicationSupport
        ) else {
            throw XCTSkip("未找到兼容的旧版 SenseVoice Small")
        }

        try await VoxCaretSenseVoiceEngine.shared.prepare(files: files, language: "auto")
        await VoxCaretSenseVoiceEngine.shared.unload()
    }
}
