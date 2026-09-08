import XCTest
@testable import VoxCaret

final class VoxCaretSenseVoiceModelLifecycleTests: XCTestCase {
    private var sandbox: URL!

    override func setUpWithError() throws {
        sandbox = FileManager.default.temporaryDirectory
            .appendingPathComponent("SenseVoiceLifecycleTests-\(UUID())", isDirectory: true)
        try FileManager.default.createDirectory(at: sandbox, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: sandbox)
    }

    @MainActor
    func testStartingDownloadReservesOperationBeforeItsTaskRuns() {
        let download = ControlledModelDownload()
        var clientCount = 0
        let manager = VoxCaretSenseVoiceModelManager(
            applicationSupport: sandbox,
            dependencies: .init(
                makeDownloader: { clientCount += 1; return download },
                prepareArchive: { _, _ in throw TestError.unexpectedPreparation },
                unload: {}
            )
        )

        manager.downloadModel()
        manager.downloadModel()

        XCTAssertEqual(clientCount, 1)
        XCTAssertEqual(manager.state, .downloading(0))
        manager.cancelDownload()
        XCTAssertEqual(download.cancelCount, 1)
        XCTAssertEqual(manager.state, .missing)
    }

    @MainActor
    func testCancelledDownloadCannotChangeRetryProgressOrClearItsCancellationHandle() async throws {
        let first = ControlledModelDownload()
        let retry = ControlledModelDownload()
        var clients = [first, retry]
        let manager = VoxCaretSenseVoiceModelManager(
            applicationSupport: sandbox,
            dependencies: .init(
                makeDownloader: { clients.removeFirst() },
                prepareArchive: { _, _ in throw TestError.unexpectedPreparation },
                unload: {}
            )
        )

        manager.downloadModel()
        await fulfillment(of: [first.started], timeout: 2)
        manager.cancelDownload()
        manager.downloadModel()
        await fulfillment(of: [retry.started], timeout: 2)
        retry.reportProgress(0.4)
        try await eventually { manager.state == .downloading(0.4) }

        // Simulate a URLSession delegate reporting progress and an error after
        // cancellation, while another download owns the progress card.
        first.reportProgress(0.9)
        first.finish(throwing: URLError(.cancelled))
        try await eventually { !first.archiveExists }
        XCTAssertEqual(manager.state, .downloading(0.4))

        manager.cancelDownload()
        XCTAssertEqual(retry.cancelCount, 1, "Old completion must not clear the retry's handle")
        XCTAssertEqual(manager.state, .missing)
        retry.finish(throwing: CancellationError())
        try await eventually { !retry.archiveExists }
    }

    @MainActor
    func testCancelledPreparationCannotOverwriteSuccessfulRetry() async throws {
        let first = ControlledModelDownload()
        let retry = ControlledModelDownload()
        let firstPreparation = ControlledModelPreparation(marker: "cancelled")
        let retryPreparation = ControlledModelPreparation(marker: "retry")
        var clients = [first, retry]
        let manager = VoxCaretSenseVoiceModelManager(
            applicationSupport: sandbox,
            dependencies: .init(
                makeDownloader: { clients.removeFirst() },
                prepareArchive: { archive, staging in
                    let preparation = first.archive == archive ? firstPreparation : retryPreparation
                    return try await preparation.run(staging: staging)
                },
                unload: {}
            )
        )

        manager.downloadModel()
        await fulfillment(of: [first.started], timeout: 2)
        first.finish()
        await fulfillment(of: [firstPreparation.started], timeout: 2)
        XCTAssertEqual(manager.state, .installing)
        manager.cancelDownload()

        manager.downloadModel()
        await fulfillment(of: [retry.started], timeout: 2)
        retry.finish()
        await fulfillment(of: [retryPreparation.started], timeout: 2)
        XCTAssertNotEqual(firstPreparation.staging, retryPreparation.staging)
        XCTAssertEqual(
            retryPreparation.staging?.deletingLastPathComponent(),
            manager.modelDirectory.deletingLastPathComponent()
        )
        retryPreparation.finish()
        try await eventually { manager.state == .ready }

        // The first extractor deliberately ignores cancellation and finishes last.
        firstPreparation.finish()
        try await eventually { !firstPreparation.stagingExists }
        XCTAssertEqual(manager.state, .ready)
        XCTAssertEqual(manager.source, .voxCaret)
        XCTAssertEqual(
            try String(contentsOf: manager.modelDirectory.appendingPathComponent("origin.txt")),
            "retry"
        )
        XCTAssertFalse(first.archiveExists)
        XCTAssertFalse(retry.archiveExists)
        XCTAssertFalse(retryPreparation.stagingExists)
    }

    @MainActor
    func testDeletionWaitsForUnloadAndRejectsAllReentry() async throws {
        let unload = ControlledModelUnload()
        let download = ControlledModelDownload()
        var clientCount = 0
        let manager = VoxCaretSenseVoiceModelManager(
            applicationSupport: sandbox,
            dependencies: .init(
                makeDownloader: { clientCount += 1; return download },
                prepareArchive: { _, _ in throw TestError.unexpectedPreparation },
                unload: { await unload.run() }
            )
        )
        try writeCompleteModel(at: manager.modelDirectory, marker: "managed")
        manager.refreshState()
        XCTAssertTrue(manager.isModelReady)

        manager.deleteModel()
        XCTAssertEqual(manager.state, .deleting)
        XCTAssertNil(manager.modelFiles, "New recognizers must not reopen a model being deleted")
        await fulfillment(of: [unload.started], timeout: 2)
        manager.cancelDownload()
        manager.deleteModel()
        manager.downloadModel()
        manager.refreshState()
        XCTAssertEqual(manager.state, .deleting)
        XCTAssertEqual(clientCount, 0)
        XCTAssertTrue(FileManager.default.fileExists(atPath: manager.modelDirectory.path))

        await unload.finish()
        try await eventually { manager.state == .missing }
        XCTAssertFalse(FileManager.default.fileExists(atPath: manager.modelDirectory.path))
        let unloadCalls = await unload.calls
        XCTAssertEqual(unloadCalls, 1)
        manager.downloadModel()
        XCTAssertEqual(clientCount, 1, "Completion should release the operation reservation")
        manager.cancelDownload()
    }

    @MainActor
    func testDeletionCancelsPendingInstallAndPreservesCompatibleModel() async throws {
        let download = ControlledModelDownload()
        let preparation = ControlledModelPreparation(marker: "cancelled")
        let unload = ControlledModelUnload()
        let manager = VoxCaretSenseVoiceModelManager(
            applicationSupport: sandbox,
            dependencies: .init(
                makeDownloader: { download },
                prepareArchive: { _, staging in try await preparation.run(staging: staging) },
                unload: { await unload.run() }
            )
        )
        manager.downloadModel()
        await fulfillment(of: [download.started], timeout: 2)
        download.finish()
        await fulfillment(of: [preparation.started], timeout: 2)
        manager.deleteModel()
        await fulfillment(of: [unload.started], timeout: 2)
        preparation.finish()
        try await eventually { !preparation.stagingExists }
        XCTAssertEqual(manager.state, .deleting)
        XCTAssertFalse(FileManager.default.fileExists(atPath: manager.modelDirectory.path))

        let compatibleDirectory = sandbox.appendingPathComponent(
            "ATM Voice/VoiceModels/SenseVoiceSmall-int8-2024-07-17", isDirectory: true
        )
        try writeCompleteModel(at: compatibleDirectory, marker: "legacy")
        await unload.finish()
        try await eventually { manager.state == .ready }
        XCTAssertEqual(manager.source, .compatibleLegacy)
        XCTAssertEqual(manager.modelFiles?.model.deletingLastPathComponent(), compatibleDirectory)
        XCTAssertTrue(FileManager.default.fileExists(atPath: compatibleDirectory.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: manager.modelDirectory.path))
    }

    @MainActor
    private func eventually(_ condition: () -> Bool, file: StaticString = #filePath, line: UInt = #line) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: .seconds(2))
        while !condition(), clock.now < deadline {
            try await Task.sleep(for: .milliseconds(1))
        }
        XCTAssertTrue(condition(), "Operation did not finish", file: file, line: line)
    }
}

private enum TestError: Error {
    case unexpectedPreparation
}

/// The fake intentionally ignores cancellation, to exercise late callbacks and
/// completions rather than relying on URLSession's scheduling or the network.
private final class ControlledModelDownload: VoxCaretSenseVoiceDownloading, @unchecked Sendable {
    let started = XCTestExpectation(description: "Download started")
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Void, Error>?
    private var progress: (@Sendable (Double) -> Void)?
    private var storedArchive: URL?
    private var storedCancelCount = 0

    var cancelCount: Int { lock.withLock { storedCancelCount } }
    var archive: URL? { lock.withLock { storedArchive } }
    var archiveExists: Bool { archive.map { FileManager.default.fileExists(atPath: $0.path) } ?? false }

    func download(from source: URL, to destination: URL, progress: @escaping @Sendable (Double) -> Void) async throws {
        try Data().write(to: destination)
        try await withCheckedThrowingContinuation { continuation in
            lock.withLock {
                self.continuation = continuation
                self.progress = progress
                self.storedArchive = destination
            }
            started.fulfill()
        }
    }

    func cancel() { lock.withLock { storedCancelCount += 1 } }

    func reportProgress(_ value: Double) {
        let callback = lock.withLock { progress }
        callback?(value)
    }

    func finish(throwing error: Error? = nil) {
        let pending = lock.withLock {
            let pending = continuation
            continuation = nil
            return pending
        }
        if let error { pending?.resume(throwing: error) } else { pending?.resume() }
    }
}

private final class ControlledModelPreparation: @unchecked Sendable {
    let started = XCTestExpectation(description: "Preparation started")
    private let lock = NSLock()
    private let marker: String
    private var continuation: CheckedContinuation<URL, Error>?
    private var storedStaging: URL?
    private var extracted: URL?

    init(marker: String) { self.marker = marker }

    var staging: URL? { lock.withLock { storedStaging } }
    var stagingExists: Bool { staging.map { FileManager.default.fileExists(atPath: $0.path) } ?? false }

    func run(staging: URL) async throws -> URL {
        let extracted = staging.appendingPathComponent("upstream-model", isDirectory: true)
        try writeCompleteModel(at: extracted, marker: marker)
        return try await withCheckedThrowingContinuation { continuation in
            lock.withLock {
                self.storedStaging = staging
                self.extracted = extracted
                self.continuation = continuation
            }
            started.fulfill()
        }
    }

    func finish() {
        let pending = lock.withLock {
            let pending = continuation
            continuation = nil
            return (pending, extracted)
        }
        if let extracted = pending.1 { pending.0?.resume(returning: extracted) }
    }
}

private actor ControlledModelUnload {
    let started = XCTestExpectation(description: "Unload started")
    private var continuation: CheckedContinuation<Void, Never>?
    private(set) var calls = 0

    func run() async {
        calls += 1
        await withCheckedContinuation { continuation in
            self.continuation = continuation
            started.fulfill()
        }
    }

    func finish() {
        continuation?.resume()
        continuation = nil
    }
}

/// Sparse files exercise the real completeness and rename path without allocating
/// or downloading a model-sized payload.
private func writeCompleteModel(at directory: URL, marker: String) throws {
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    for (name, size) in [("model.int8.onnx", UInt64(100_000_001)), ("tokens.txt", UInt64(100_001))] {
        let url = directory.appendingPathComponent(name)
        _ = FileManager.default.createFile(atPath: url.path, contents: nil)
        let handle = try FileHandle(forWritingTo: url)
        defer { try? handle.close() }
        try handle.truncate(atOffset: size)
    }
    try marker.write(to: directory.appendingPathComponent("origin.txt"), atomically: true, encoding: .utf8)
}
