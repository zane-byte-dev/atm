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

    private var downloader: VoxCaretSenseVoiceDownloadClient?
    private var downloadTask: Task<Void, Never>?
    private var resolvedModelFiles: VoxCaretSenseVoiceModelFiles?

    private init() {
        refreshState()
    }

    /// Under Application Support rather than inside the bundle: the bundle is
    /// replaced wholesale on every build and re-signed ad hoc, and re-downloading
    /// 160MB per build is not a thing to ask of anyone.
    var modelDirectory: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return base
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

    /// Re-reads the disk. Skipped while a download is running: the files are not
    /// there yet by definition, and answering "missing" would throw away the
    /// progress the download card is showing.
    func refreshState() {
        guard !isBusy else { return }
        if Self.isComplete(managedModelFiles) {
            resolvedModelFiles = managedModelFiles
            source = .voxCaret
            state = .ready
            return
        }
        if let discovered = Self.discoverCompatibleLegacyModel(
            applicationSupport: FileManager.default.urls(
                for: .applicationSupportDirectory,
                in: .userDomainMask
            )[0]
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
        guard !isBusy else { return }
        downloadTask = Task { [weak self] in
            await self?.performDownload()
        }
    }

    func cancelDownload() {
        downloader?.cancel()
        downloader = nil
        downloadTask?.cancel()
        downloadTask = nil
        state = .missing
        refreshState()
    }

    func deleteModel() {
        cancelDownload()
        // Unload before removing the files: the recognizer holds the ONNX file open,
        // and deleting underneath it would leave a live session reading a path that
        // no longer exists.
        Task { await VoxCaretSenseVoiceEngine.shared.unload() }
        do {
            if FileManager.default.fileExists(atPath: modelDirectory.path) {
                try FileManager.default.removeItem(at: modelDirectory)
            }
            refreshState()
        } catch {
            state = .failed(error.localizedDescription)
            VoxCaretLog.failure("sense_voice_model_delete_failed", error: error.localizedDescription)
        }
    }

    private var isBusy: Bool {
        switch state {
        case .downloading, .installing: return true
        case .missing, .ready, .failed: return false
        }
    }

    private func performDownload() async {
        let archive = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoxCaret-SenseVoice-\(UUID().uuidString).tar.bz2")
        let client = VoxCaretSenseVoiceDownloadClient()
        downloader = client
        state = .downloading(0)

        do {
            try await client.download(from: Self.archiveURL, to: archive) { [weak self] progress in
                Task { @MainActor in
                    // Only while still downloading: a late progress callback must not
                    // drag the card back out of 安装中.
                    guard let self, case .downloading = self.state else { return }
                    self.state = .downloading(progress)
                }
            }
            try Task.checkCancellation()
            state = .installing
            let destination = modelDirectory
            // Hashing 160MB and shelling out to tar both block; off the main actor so
            // the progress card and the rest of the app keep drawing.
            try await Task.detached(priority: .userInitiated) {
                try Self.verifyArchive(at: archive)
                try Self.extractArchive(at: archive, to: destination)
            }.value
            state = .missing
            refreshState()
            VoxCaretLog.lifecycle("sense_voice_model_installed")
        } catch is CancellationError {
            state = .missing
            refreshState()
        } catch {
            state = .failed(error.localizedDescription)
            VoxCaretLog.failure("sense_voice_model_install_failed", error: error.localizedDescription)
        }

        try? FileManager.default.removeItem(at: archive)
        downloader = nil
        downloadTask = nil
    }

    /// Streamed in 4MB chunks rather than read whole: the archive is 160MB and this
    /// runs on a machine that is also holding the app's UI.
    nonisolated private static func verifyArchive(at url: URL) throws {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        while autoreleasepool(invoking: {
            let data = try? handle.read(upToCount: 4 * 1024 * 1024)
            guard let data, !data.isEmpty else { return false }
            hasher.update(data: data)
            return true
        }) {}
        let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()
        guard digest == archiveSHA256 else { throw VoxCaretSenseVoiceModelError.checksumMismatch }
    }

    nonisolated private static func extractArchive(at archive: URL, to destination: URL) throws {
        let fileManager = FileManager.default
        let staging = fileManager.temporaryDirectory
            .appendingPathComponent("VoxCaret-SenseVoice-install-\(UUID().uuidString)", isDirectory: true)
        try fileManager.createDirectory(at: staging, withIntermediateDirectories: true)
        defer { try? fileManager.removeItem(at: staging) }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
        process.arguments = ["-xjf", archive.path, "-C", staging.path]
        try process.run()
        process.waitUntilExit()
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

        try fileManager.createDirectory(
            at: destination.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        if fileManager.fileExists(atPath: destination.path) {
            try fileManager.removeItem(at: destination)
        }
        try fileManager.moveItem(at: extracted, to: destination)
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
private final class VoxCaretSenseVoiceDownloadClient: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Void, Error>?
    private var destination: URL?
    private var progress: (@Sendable (Double) -> Void)?
    private var session: URLSession?
    private var task: URLSessionDownloadTask?

    func download(
        from source: URL,
        to destination: URL,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                lock.lock()
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
        task?.cancel()
        let continuation = self.continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(throwing: CancellationError())
        session?.invalidateAndCancel()
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
        progress?(min(1, Double(totalBytesWritten) / Double(expected)))
    }

    func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        do {
            guard let destination else { throw VoxCaretSenseVoiceModelError.downloadFailed }
            try? FileManager.default.removeItem(at: destination)
            // Must happen inside this callback: URLSession deletes the temporary
            // file as soon as it returns.
            try FileManager.default.moveItem(at: location, to: destination)
        } catch {
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
        lock.unlock()
        if let error {
            continuation?.resume(throwing: error)
        } else {
            continuation?.resume()
        }
        session?.finishTasksAndInvalidate()
    }
}
