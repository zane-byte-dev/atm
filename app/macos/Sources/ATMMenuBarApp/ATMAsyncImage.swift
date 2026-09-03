import Foundation
import ImageIO
import SwiftUI
import UniformTypeIdentifiers

/// Only the small, decoded bitmap crosses back to the view. File inspection and
/// ImageIO decoding run on the loader's executor and a detached worker.
struct ATMAsyncThumbnail: View {
    let url: URL
    let width: CGFloat
    let height: CGFloat
    var contentMode: ContentMode = .fill
    /// Optional model revision for a source whose URL is reused. Stored Todo
    /// attachments already receive unique paths on import.
    var revision: AnyHashable? = nil

    @Environment(\.displayScale) private var displayScale
    @State private var loaded: LoadedThumbnail?

    private struct Request: Equatable {
        let url: URL
        let pixelWidth: Int
        let pixelHeight: Int
        let fill: Bool
        let revision: AnyHashable?
    }

    private struct LoadedThumbnail {
        let request: Request
        let image: CGImage
    }

    private var request: Request {
        Request(
            url: url.standardizedFileURL,
            pixelWidth: ATMThumbnailLoader.pixelBucket(Int(ceil(width * displayScale))),
            pixelHeight: ATMThumbnailLoader.pixelBucket(Int(ceil(height * displayScale))),
            fill: contentMode == .fill,
            revision: revision
        )
    }

    var body: some View {
        let request = self.request
        Group {
            if let loaded, loaded.request == request {
                Image(decorative: loaded.image, scale: displayScale)
                    .resizable()
                    .aspectRatio(contentMode: contentMode)
            } else {
                Image(systemName: "photo")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(width: width, height: height)
        .clipped()
        .task(id: request) {
            let image = await ATMThumbnailLoader.shared.image(
                for: request.url,
                pixelWidth: request.pixelWidth,
                pixelHeight: request.pixelHeight,
                fill: request.fill
            )
            guard !Task.isCancelled else { return }
            loaded = image.map { LoadedThumbnail(request: request, image: $0) }
        }
    }
}

/// Actor isolation keeps filesystem work away from MainActor. Entries include
/// source metadata, so replacing a file at the same path does not reuse an old
/// bitmap. Both completed bitmaps and duplicate pending decodes are shared.
actor ATMThumbnailLoader {
    static let shared = ATMThumbnailLoader()

    typealias Decoder = @Sendable (URL, Int, Int, Bool) -> CGImage?

    private struct Key: Hashable {
        let path: String
        let modified: Date?
        let created: Date?
        let fileSize: Int
        let pixelWidth: Int
        let pixelHeight: Int
        let fill: Bool
    }

    private struct Entry {
        let image: CGImage
        let cost: Int
        var access: UInt64
    }

    private struct Pending {
        let id: UUID
        let task: Task<CGImage?, Never>
        var consumers: Set<UUID>
    }

    private let cacheCostLimit: Int
    private let cacheCountLimit: Int
    private let decoder: Decoder
    private var cache: [Key: Entry] = [:]
    private var pending: [Key: Pending] = [:]
    private var cachedCost = 0
    private var access: UInt64 = 0

    init(
        cacheCostLimit: Int = 32 * 1024 * 1024,
        cacheCountLimit: Int = 256,
        decoder: @escaping Decoder = { @Sendable url, width, height, fill in
            ATMThumbnailLoader.decode(url, width, height, fill)
        }
    ) {
        self.cacheCostLimit = max(0, cacheCostLimit)
        self.cacheCountLimit = max(0, cacheCountLimit)
        self.decoder = decoder
    }

    func image(for url: URL, pixelWidth: Int, pixelHeight: Int, fill: Bool = true) async -> CGImage? {
        guard !Task.isCancelled,
              let key = key(for: url, pixelWidth: pixelWidth, pixelHeight: pixelHeight, fill: fill)
        else { return nil }
        access &+= 1
        if var entry = cache[key] {
            entry.access = access
            cache[key] = entry
            return entry.image
        }
        let consumer = UUID()
        let work: Pending
        if var existing = pending[key] {
            existing.consumers.insert(consumer)
            pending[key] = existing
            work = existing
        } else {
            let decoder = decoder
            work = Pending(
                id: UUID(),
                task: Task.detached(priority: .utility) {
                    await ATMThumbnailDecodeExecutor.image {
                        decoder(URL(fileURLWithPath: key.path), key.pixelWidth, key.pixelHeight, key.fill)
                    }
                },
                consumers: [consumer]
            )
            pending[key] = work
        }
        let image = await withTaskCancellationHandler {
            await work.task.value
        } onCancel: {
            Task { await self.cancelConsumer(consumer, key: key, pendingID: work.id) }
        }
        // Another caller may have already completed this shared request.
        if pending[key]?.id == work.id {
            pending.removeValue(forKey: key)
            if let image,
               self.key(for: url, pixelWidth: pixelWidth, pixelHeight: pixelHeight, fill: fill) == key {
                insert(image, for: key)
            }
        }
        guard !Task.isCancelled,
              self.key(for: url, pixelWidth: pixelWidth, pixelHeight: pixelHeight, fill: fill) == key
        else { return nil }
        return image
    }

    private func cancelConsumer(_ consumer: UUID, key: Key, pendingID: UUID) {
        guard var work = pending[key], work.id == pendingID else { return }
        work.consumers.remove(consumer)
        if work.consumers.isEmpty {
            pending.removeValue(forKey: key)
            work.task.cancel()
        } else {
            pending[key] = work
        }
    }

    /// Small window resizes reuse decoded pixels and the same SwiftUI task.
    /// Rounding upward retains enough resolution for every size in a bucket.
    nonisolated static func pixelBucket(_ value: Int) -> Int {
        let clamped = min(4096, max(1, value))
        return ((clamped + 31) / 32) * 32
    }

    private func key(for url: URL, pixelWidth: Int, pixelHeight: Int, fill: Bool) -> Key? {
        guard url.isFileURL,
              let values = try? URL(fileURLWithPath: url.path).resourceValues(forKeys: [
                .isRegularFileKey, .contentModificationDateKey, .creationDateKey, .fileSizeKey,
              ]), values.isRegularFile == true else { return nil }
        return Key(
            path: url.standardizedFileURL.path,
            modified: values.contentModificationDate,
            created: values.creationDate,
            fileSize: values.fileSize ?? 0,
            pixelWidth: Self.pixelBucket(pixelWidth),
            pixelHeight: Self.pixelBucket(pixelHeight),
            fill: fill
        )
    }

    private func insert(_ image: CGImage, for key: Key) {
        let cost = image.bytesPerRow * image.height
        guard cost <= cacheCostLimit, cacheCountLimit > 0 else { return }
        if let previous = cache.removeValue(forKey: key) { cachedCost -= previous.cost }
        while !cache.isEmpty && (cachedCost + cost > cacheCostLimit || cache.count >= cacheCountLimit) {
            guard let oldest = cache.min(by: { $0.value.access < $1.value.access }) else { break }
            cachedCost -= oldest.value.cost
            cache.removeValue(forKey: oldest.key)
        }
        access &+= 1
        cache[key] = Entry(image: image, cost: cost, access: access)
        cachedCost += cost
    }

    nonisolated static func decode(_ url: URL, _ width: Int, _ height: Int, _ fill: Bool) -> CGImage? {
        autoreleasepool {
            guard let source = CGImageSourceCreateWithURL(
                url as CFURL, [kCGImageSourceShouldCache: false] as CFDictionary
            ) else { return nil }
            let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any]
            var sourceWidth = (properties?[kCGImagePropertyPixelWidth] as? NSNumber)?.doubleValue ?? Double(width)
            var sourceHeight = (properties?[kCGImagePropertyPixelHeight] as? NSNumber)?.doubleValue ?? Double(height)
            let orientation = (properties?[kCGImagePropertyOrientation] as? NSNumber)?.intValue ?? 1
            if (5...8).contains(orientation) { swap(&sourceWidth, &sourceHeight) }
            guard sourceWidth > 0, sourceHeight > 0 else { return nil }
            let horizontalScale = Double(width) / sourceWidth
            let verticalScale = Double(height) / sourceHeight
            let scale = fill ? max(horizontalScale, verticalScale) : min(horizontalScale, verticalScale)
            let maxPixelSize = Int(min(4096, max(1, ceil(max(sourceWidth, sourceHeight) * min(1, scale)))))
            return CGImageSourceCreateThumbnailAtIndex(source, 0, [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,
                kCGImageSourceShouldCacheImmediately: true,
                kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
            ] as CFDictionary)
        }
    }
}

private enum ATMThumbnailDecodeExecutor {
    // ImageIO's synchronous decode cannot be interrupted once it starts. A
    // serial utility queue bounds that work during rapid navigation; cancelled
    // requests waiting in the queue never begin decoding.
    private static let queue = DispatchQueue(label: "dev.atm.thumbnail-decode", qos: .utility)

    static func image(_ decode: @escaping @Sendable () -> CGImage?) async -> CGImage? {
        let cancellation = CancellationFlag()
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                queue.async {
                    guard !cancellation.isCancelled else {
                        continuation.resume(returning: nil)
                        return
                    }
                    let image = decode()
                    continuation.resume(returning: cancellation.isCancelled ? nil : image)
                }
            }
        } onCancel: {
            cancellation.cancel()
        }
    }

    private final class CancellationFlag: @unchecked Sendable {
        private let lock = NSLock()
        private var cancelled = false

        var isCancelled: Bool { lock.withLock { cancelled } }
        func cancel() { lock.withLock { cancelled = true } }
    }
}

enum ATMPastedImageWriter {
    enum WriteError: LocalizedError {
        case invalidImage
        case encodingFailed
        case tooLarge

        var errorDescription: String? {
            switch self {
            case .invalidImage: return "无法读取剪贴板中的图片。"
            case .encodingFailed: return "无法将剪贴板图片转换为 PNG。"
            case .tooLarge: return "单张图片不能超过 10 MB。"
            }
        }
    }

    /// Pasteboard access stays on MainActor; its copied data is decoded and
    /// encoded here. Callers own the returned file and must remove it on discard.
    static func writePNG(
        data: Data,
        directory: URL = FileManager.default.temporaryDirectory
    ) async throws -> URL {
        let worker = Task.detached(priority: .userInitiated) {
            try writePNGInBackground(data: data, directory: directory)
        }
        return try await withTaskCancellationHandler {
            let url = try await worker.value
            do {
                try Task.checkCancellation()
                return url
            } catch {
                try? FileManager.default.removeItem(at: url)
                throw error
            }
        } onCancel: {
            worker.cancel()
        }
    }

    private static func writePNGInBackground(data: Data, directory: URL) throws -> URL {
        try autoreleasepool {
            try Task.checkCancellation()
            guard let source = CGImageSourceCreateWithData(
                data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary
            ), let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
                throw WriteError.invalidImage
            }
            try Task.checkCancellation()
            let url = directory.appendingPathComponent("atm-pasted-\(UUID().uuidString).png")
            var completed = false
            defer {
                if !completed { try? FileManager.default.removeItem(at: url) }
            }
            guard FileManager.default.createFile(
                atPath: url.path, contents: nil, attributes: [.posixPermissions: 0o600]
            ) else { throw WriteError.encodingFailed }
            guard let destination = CGImageDestinationCreateWithURL(
                url as CFURL, UTType.png.identifier as CFString, 1, nil
            ) else { throw WriteError.encodingFailed }
            CGImageDestinationAddImage(destination, image, nil)
            guard CGImageDestinationFinalize(destination) else { throw WriteError.encodingFailed }
            try Task.checkCancellation()
            let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
            guard (attributes[.size] as? NSNumber)?.int64Value ?? 0 <= ATMTodoImageRules.maximumBytes else {
                throw WriteError.tooLarge
            }
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
            try Task.checkCancellation()
            completed = true
            return url
        }
    }
}
