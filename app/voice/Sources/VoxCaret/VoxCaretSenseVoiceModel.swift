import CryptoKit
import Foundation

struct VoxCaretSenseVoiceModelFiles: Sendable, Equatable {
    let model: URL
    let tokens: URL
}

enum VoxCaretSenseVoiceModelState: Equatable {
    case missing
    case downloading(Double)
    case installing
    case deleting
    case ready
    case failed(String)
}

enum VoxCaretSenseVoiceModelSource: Equatable {
    case voxCaret
    case compatibleLegacy

    var isManagedByVoxCaret: Bool { self == .voxCaret }
}

enum VoxCaretSenseVoiceModelError: LocalizedError {
    case downloadFailed
    case checksumMismatch
    case extractionFailed
    case incompleteModel

    var errorDescription: String? {
        switch self {
        case .downloadFailed: return "SenseVoice 模型下载失败。"
        case .checksumMismatch: return "SenseVoice 模型校验失败，请重新下载。"
        case .extractionFailed: return "SenseVoice 模型解压失败。"
        case .incompleteModel: return "SenseVoice 模型文件不完整，请重新下载。"
        }
    }
}

protocol VoxCaretSenseVoiceDownloading: Sendable {
    func download(
        from source: URL,
        to destination: URL,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws
    func cancel()
}

struct VoxCaretSenseVoiceModelDependencies {
    var makeDownloader: () -> any VoxCaretSenseVoiceDownloading
    /// Produces verified files inside the operation's private staging directory.
    /// Only the manager may commit those files to the shared model directory.
    var prepareArchive: @Sendable (URL, URL) async throws -> URL
    var unload: @Sendable () async -> Void

    static let live = Self(
        makeDownloader: { VoxCaretSenseVoiceDownloadClient() },
        prepareArchive: { archive, staging in
            let worker = Task.detached(priority: .userInitiated) {
                try VoxCaretSenseVoiceModelManager.verifyArchive(at: archive)
                return try VoxCaretSenseVoiceModelManager.extractArchive(at: archive, staging: staging)
            }
            return try await withTaskCancellationHandler {
                try await worker.value
            } onCancel: {
                worker.cancel()
            }
        },
        unload: { await VoxCaretSenseVoiceEngine.shared.unload() }
    )
}

/// Owns the on-disk SenseVoice model: whether it is there, downloading it, and
/// throwing it away.
///
/// The model is a 160MB archive that expands to a ~230MB int8 ONNX file, so it is
/// not shipped inside the app — dictation works on Apple Speech until someone opts
/// in to the download.
///
/// The install is deliberately all-or-nothing: verify the archive's checksum, expand
/// into a staging directory, check the files that came out, and only then move the
/// whole directory into place. A download interrupted halfway leaves nothing behind,
/// because a half-written model would read as "ready" and then fail at the moment
/// someone is trying to speak.
@MainActor
final class VoxCaretSenseVoiceModelManager: ObservableObject {
    static let shared = VoxCaretSenseVoiceModelManager()

    static let archiveURL = URL(
        string: "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-int8-2024-07-17.tar.bz2"
    )!
    nonisolated static let archiveSHA256 = "7d1efa2138a65b0b488df37f8b89e3d91a60676e416f515b952358d83dfd347e"
    /// Read from the download delegate, which runs off the main actor.
    nonisolated static let archiveByteCount: Int64 = 163_002_883

    @Published private(set) var state: VoxCaretSenseVoiceModelState = .missing
    @Published private(set) var source: VoxCaretSenseVoiceModelSource?

    private enum Operation: Equatable {
        case download(UUID)
        case deletion(UUID)
    }

    private let applicationSupport: URL
    private let dependencies: VoxCaretSenseVoiceModelDependencies
    private var operation: Operation?
    private var downloader: (any VoxCaretSenseVoiceDownloading)?
    private var operationTask: Task<Void, Never>?
    private var resolvedModelFiles: VoxCaretSenseVoiceModelFiles?

    init(
        applicationSupport: URL = FileManager.default.urls(
            for: .applicationSupportDirectory, in: .userDomainMask
        )[0],
        dependencies: VoxCaretSenseVoiceModelDependencies = .live
    ) {
        self.applicationSupport = applicationSupport
        self.dependencies = dependencies
        refreshState()
    }

    /// Under Application Support rather than inside the bundle: the bundle is
    /// replaced wholesale on every build and re-signed ad hoc, and re-downloading
    /// 160MB per build is not a thing to ask of anyone.
    var modelDirectory: URL {
        applicationSupport
            .appendingPathComponent("VoxCaret", isDirectory: true)
            .appendingPathComponent("VoiceModels", isDirectory: true)
            .appendingPathComponent("SenseVoiceSmall-int8-2024-07-17", isDirectory: true)
    }

    private var managedModelFiles: VoxCaretSenseVoiceModelFiles {
        VoxCaretSenseVoiceModelFiles(
            model: modelDirectory.appendingPathComponent("model.int8.onnx"),
            tokens: modelDirectory.appendingPathComponent("tokens.txt")
        )
    }

    var modelFiles: VoxCaretSenseVoiceModelFiles? {
        guard let resolvedModelFiles, Self.isComplete(resolvedModelFiles) else { return nil }
        return resolvedModelFiles
    }

    var isModelReady: Bool { modelFiles != nil }

    /// Re-reads the disk only while idle, preserving operation progress and
    /// keeping a model scheduled for deletion unavailable to new recognizers.
    func refreshState() {
        guard !isBusy else { return }
        if Self.isComplete(managedModelFiles) {
            resolvedModelFiles = managedModelFiles
            source = .voxCaret
            state = .ready
            return
        }
        if let discovered = Self.discoverCompatibleLegacyModel(
            applicationSupport: applicationSupport
        ) {
            resolvedModelFiles = discovered
            source = .compatibleLegacy
            state = .ready
            VoxCaretLog.lifecycle("sense_voice_model_reused_from_compatible_install")
            return
        }
        resolvedModelFiles = nil
        source = nil
        state = .missing
    }

    func downloadModel() {
        guard operation == nil else { return }
        let id = UUID()
        let client = dependencies.makeDownloader()
        // Reserve synchronously so two calls in the same actor turn cannot start
        // parallel downloads before their tasks begin running.
        operation = .download(id)
        downloader = client
        state = .downloading(0)
        operationTask = Task { [weak self] in
            await self?.performDownload(id: id, client: client)
        }
    }

    func cancelDownload() {
        guard case .download = operation else { return }
        let client = downloader
        let task = operationTask
        // Invalidate first. Even a downloader that finishes or reports progress
        // after cancellation no longer owns the state or installation destination.
        operation = nil
        downloader = nil
        operationTask = nil
        client?.cancel()
        task?.cancel()
        refreshState()
    }

    func deleteModel() {
        if case .deletion = operation { return }
        cancelDownload()
        let id = UUID()
        operation = .deletion(id)
        resolvedModelFiles = nil
        source = nil
        state = .deleting
        operationTask = Task { [weak self] in
            await self?.performDeletion(id: id)
        }
    }

    private func performDeletion(id: UUID) async {
        // Keep this operation reserved across the await: no new download or
        // model lookup may race the engine while it releases the ONNX file.
        await dependencies.unload()
        guard operation == .deletion(id) else { return }
        do {
            if FileManager.default.fileExists(atPath: modelDirectory.path) {
                try FileManager.default.removeItem(at: modelDirectory)
            }
            finishOperation(.deletion(id))
            refreshState()
        } catch {
            finishOperation(.deletion(id))
            state = .failed(error.localizedDescription)
            VoxCaretLog.failure("sense_voice_model_delete_failed", error: error.localizedDescription)
        }
    }

    private var isBusy: Bool { operation != nil }

    private func finishOperation(_ completed: Operation) {
        guard operation == completed else { return }
        operation = nil
        downloader = nil
        operationTask = nil
    }

    private func performDownload(id: UUID, client: any VoxCaretSenseVoiceDownloading) async {
        let archive = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoxCaret-SenseVoice-\(id).tar.bz2")
        // Stage on the destination volume so the final move is a rename rather
        // than a potentially interruptible copy of a 230MB model across volumes.
        let staging = modelDirectory.deletingLastPathComponent()
            .appendingPathComponent(".SenseVoice-install-\(id)", isDirectory: true)
        defer {
            try? FileManager.default.removeItem(at: archive)
            try? FileManager.default.removeItem(at: staging)
        }

        do {
            try Task.checkCancellation()
            guard operation == .download(id) else { return }
            try await client.download(from: Self.archiveURL, to: archive) { [weak self] progress in
                Task { @MainActor in
                    guard let self, self.operation == .download(id),
                          case .downloading = self.state else { return }
                    self.state = .downloading(progress)
                }
            }
            try Task.checkCancellation()
            guard operation == .download(id) else { return }
            state = .installing
            let extracted = try await dependencies.prepareArchive(archive, staging)
            try Task.checkCancellation()
            guard operation == .download(id) else { return }
            // No suspension between the identity check and the commit. Detached
            // work only touches its staging directory, including after cancellation.
            try Self.commitPreparedModel(at: extracted, to: modelDirectory)
            finishOperation(.download(id))
            refreshState()
            VoxCaretLog.lifecycle("sense_voice_model_installed")
        } catch {
            guard operation == .download(id) else { return }
            finishOperation(.download(id))
            if error is CancellationError || Task.isCancelled {
                refreshState()
            } else {
                state = .failed(error.localizedDescription)
                VoxCaretLog.failure("sense_voice_model_install_failed", error: error.localizedDescription)
            }
        }
    }

    private static func commitPreparedModel(at extracted: URL, to destination: URL) throws {
        let fileManager = FileManager.default
        let files = VoxCaretSenseVoiceModelFiles(
            model: extracted.appendingPathComponent("model.int8.onnx"),
            tokens: extracted.appendingPathComponent("tokens.txt")
        )
        guard isComplete(files) else { throw VoxCaretSenseVoiceModelError.incompleteModel }
        if fileManager.fileExists(atPath: destination.path) {
            try fileManager.removeItem(at: destination)
        }
        try fileManager.moveItem(at: extracted, to: destination)
    }

    /// Streamed in 4MB chunks rather than read whole: the archive is 160MB and this
    /// runs on a machine that is also holding the app's UI.
    nonisolated fileprivate static func verifyArchive(at url: URL) throws {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        while try autoreleasepool(invoking: {
            try Task.checkCancellation()
            guard let data = try handle.read(upToCount: 4 * 1024 * 1024), !data.isEmpty else { return false }
            hasher.update(data: data)
            return true
        }) {}
        let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()
        guard digest == archiveSHA256 else { throw VoxCaretSenseVoiceModelError.checksumMismatch }
    }

    nonisolated fileprivate static func extractArchive(at archive: URL, staging: URL) throws -> URL {
        try Task.checkCancellation()
        let fileManager = FileManager.default
        try fileManager.createDirectory(at: staging, withIntermediateDirectories: true)

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
        process.arguments = ["-xjf", archive.path, "-C", staging.path]
        try process.run()
        process.waitUntilExit()
        try Task.checkCancellation()
        guard process.terminationStatus == 0 else { throw VoxCaretSenseVoiceModelError.extractionFailed }

        // The archive's top-level directory name is upstream's to change, so the
        // model is found by looking for the file we need rather than by rebuilding
        // the expected path.
        let candidates = try fileManager.contentsOfDirectory(
            at: staging,
            includingPropertiesForKeys: [.isDirectoryKey],
            options: [.skipsHiddenFiles]
        )
        guard let extracted = candidates.first(where: {
            fileManager.fileExists(atPath: $0.appendingPathComponent("model.int8.onnx").path)
        }) else {
            throw VoxCaretSenseVoiceModelError.incompleteModel
        }
        let files = VoxCaretSenseVoiceModelFiles(
            model: extracted.appendingPathComponent("model.int8.onnx"),
            tokens: extracted.appendingPathComponent("tokens.txt")
        )
        guard isComplete(files) else { throw VoxCaretSenseVoiceModelError.incompleteModel }

        return extracted
    }

    /// Size thresholds rather than a checksum of the expanded files: the point is to
    /// catch a truncated or half-moved install, and re-hashing 230MB on every launch
    /// to answer "is the model there" would cost more than it protects.
    nonisolated static func isComplete(_ files: VoxCaretSenseVoiceModelFiles) -> Bool {
        let fileManager = FileManager.default
        guard
            let modelSize = (try? fileManager.attributesOfItem(atPath: files.model.path)[.size]) as? NSNumber,
            let tokenSize = (try? fileManager.attributesOfItem(atPath: files.tokens.path)[.size]) as? NSNumber
        else { return false }
        return modelSize.int64Value > 100_000_000 && tokenSize.int64Value > 100_000
    }

    /// Reuses a model produced by our earlier app in place. Unlike third-party ONNX
    /// exports, these files contain the sherpa-onnx metadata required by this runtime.
    /// The source remains read-only and can still be copied through the migration card
    /// if the user later wants VoxCaret to own an independent copy.
    nonisolated static func discoverCompatibleLegacyModel(
        applicationSupport: URL
    ) -> VoxCaretSenseVoiceModelFiles? {
        let relativeDirectories = [
            "ATM Voice/VoiceModels/SenseVoiceSmall-int8-2024-07-17",
            "ATM/VoiceModels/SenseVoiceSmall-int8-2024-07-17",
        ]
        for relativeDirectory in relativeDirectories {
            let directory = applicationSupport.appendingPathComponent(
                relativeDirectory,
                isDirectory: true
            )
            let files = VoxCaretSenseVoiceModelFiles(
                model: directory.appendingPathComponent("model.int8.onnx"),
                tokens: directory.appendingPathComponent("tokens.txt")
            )
            if isComplete(files) { return files }
        }
        return nil
    }
}

/// A download that reports progress and can be cancelled.
///
/// `URLSession.download(from:)` gives no progress, and `bytes(for:)` would mean
/// assembling 160MB by hand, so this is the delegate form wrapped into one async
/// call.
private final class VoxCaretSenseVoiceDownloadClient: NSObject, URLSessionDownloadDelegate, VoxCaretSenseVoiceDownloading, @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Void, Error>?
    private var destination: URL?
    private var progress: (@Sendable (Double) -> Void)?
    private var session: URLSession?
    private var task: URLSessionDownloadTask?
    private var cancelled = false

    func download(
        from source: URL,
        to destination: URL,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                lock.lock()
                guard !cancelled else {
                    lock.unlock()
                    continuation.resume(throwing: CancellationError())
                    return
                }
                self.continuation = continuation
                self.destination = destination
                self.progress = progress
                let session = URLSession(configuration: .ephemeral, delegate: self, delegateQueue: nil)
                self.session = session
                let task = session.downloadTask(with: source)
                self.task = task
                lock.unlock()
                task.resume()
            }
        } onCancel: {
            self.cancel()
        }
    }

    func cancel() {
        lock.lock()
        cancelled = true
        let task = self.task
        let session = self.session
        let continuation = self.continuation
        self.continuation = nil
        self.progress = nil
        lock.unlock()
        task?.cancel()
        session?.invalidateAndCancel()
        continuation?.resume(throwing: CancellationError())
    }

    func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didWriteData bytesWritten: Int64,
        totalBytesWritten: Int64,
        totalBytesExpectedToWrite: Int64
    ) {
        // GitHub's redirect target does report a length, but fall back to the known
        // archive size rather than showing a bar that never moves.
        let expected = totalBytesExpectedToWrite > 0
            ? totalBytesExpectedToWrite
            : VoxCaretSenseVoiceModelManager.archiveByteCount
        lock.lock()
        let progress = self.progress
        lock.unlock()
        progress?(min(1, Double(totalBytesWritten) / Double(expected)))
    }

    func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        // Serialize the move with cancel(): once cancellation returns, no late
        // delegate callback can recreate an archive its owner has already removed.
        lock.lock()
        guard !cancelled, continuation != nil else {
            lock.unlock()
            return
        }
        do {
            guard let destination,
                  let response = downloadTask.response as? HTTPURLResponse,
                  (200..<300).contains(response.statusCode) else {
                throw VoxCaretSenseVoiceModelError.downloadFailed
            }
            // Must happen inside this callback: URLSession deletes the temporary
            // file as soon as it returns.
            try FileManager.default.moveItem(at: location, to: destination)
            lock.unlock()
        } catch {
            lock.unlock()
            finish(throwing: error)
        }
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        finish(throwing: error)
    }

    private func finish(throwing error: Error?) {
        lock.lock()
        let continuation = self.continuation
        self.continuation = nil
        self.progress = nil
        let session = self.session
        lock.unlock()
        if let error {
            continuation?.resume(throwing: error)
        } else {
            continuation?.resume()
        }
        session?.finishTasksAndInvalidate()
    }
}
