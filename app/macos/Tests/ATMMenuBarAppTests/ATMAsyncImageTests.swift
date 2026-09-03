import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers
import XCTest
@testable import ATMMenuBarApp

final class ATMAsyncImageTests: XCTestCase {
    func testThumbnailDownsamplesAndInvalidatesAfterFileReplacement() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("image.png")
        try imageData(width: 800, height: 400).write(to: url)
        let loader = ATMThumbnailLoader()

        let first = await loader.image(for: url, pixelWidth: 80, pixelHeight: 40, fill: false)
        XCTAssertEqual(first?.width, 96)
        XCTAssertEqual(first?.height, 48)

        try imageData(width: 400, height: 800).write(to: url, options: .atomic)
        try FileManager.default.setAttributes(
            [.modificationDate: Date().addingTimeInterval(10)], ofItemAtPath: url.path
        )
        let second = await loader.image(for: url, pixelWidth: 80, pixelHeight: 40, fill: false)
        XCTAssertEqual(second?.width, 32)
        XCTAssertEqual(second?.height, 64)
        // Fill needs enough pixels along both axes, including a portrait image
        // inside a landscape cell; simply taking max(width, height) is too small.
        let filled = await loader.image(for: url, pixelWidth: 80, pixelHeight: 40, fill: true)
        XCTAssertEqual(filled?.width, 96)
        XCTAssertEqual(filled?.height, 192)
    }

    func testConcurrentRequestsShareDecodeAndCostLimitEvictsOldest() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let firstURL = directory.appendingPathComponent("first.png")
        let secondURL = directory.appendingPathComponent("second.png")
        try Data([1]).write(to: firstURL)
        try Data([2]).write(to: secondURL)
        let fixture = try makeImage(width: 32, height: 16)
        let decoder = CountingDecoder(image: fixture)
        let loader = ATMThumbnailLoader(
            cacheCostLimit: fixture.bytesPerRow * fixture.height,
            decoder: { _, _, _, _ in decoder.decode() }
        )

        await withTaskGroup(of: Void.self) { group in
            for _ in 0..<12 {
                group.addTask {
                    _ = await loader.image(for: firstURL, pixelWidth: 32, pixelHeight: 16)
                }
            }
        }
        XCTAssertEqual(decoder.count, 1)
        _ = await loader.image(for: secondURL, pixelWidth: 32, pixelHeight: 16)
        XCTAssertEqual(decoder.count, 2)
        _ = await loader.image(for: firstURL, pixelWidth: 32, pixelHeight: 16)
        XCTAssertEqual(decoder.count, 3, "A second bitmap must evict the first when the byte budget holds only one")
    }

    func testRemovedSourceDoesNotReturnCachedImage() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("image.png")
        try imageData(width: 100, height: 50).write(to: url)
        let loader = ATMThumbnailLoader()
        let first = await loader.image(for: url, pixelWidth: 50, pixelHeight: 25)
        XCTAssertNotNil(first)
        try FileManager.default.removeItem(at: url)
        let missing = await loader.image(for: url, pixelWidth: 50, pixelHeight: 25)
        XCTAssertNil(missing)
    }

    func testNearbyResizeRequestsReuseDecodedPixels() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("image.png")
        try Data([1]).write(to: url)
        let decoder = CountingDecoder(image: try makeImage(width: 96, height: 64))
        let loader = ATMThumbnailLoader(decoder: { _, _, _, _ in decoder.decode() })
        _ = await loader.image(for: url, pixelWidth: 70, pixelHeight: 50)
        _ = await loader.image(for: url, pixelWidth: 90, pixelHeight: 60)
        XCTAssertEqual(decoder.count, 1)
        _ = await loader.image(for: url, pixelWidth: 97, pixelHeight: 60)
        XCTAssertEqual(decoder.count, 2, "Crossing a size bucket requests enough pixels for the larger cell")
    }

    func testCancelledThumbnailRequestDoesNotDecode() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("image.png")
        try Data([1]).write(to: url)
        let decoder = CountingDecoder(image: try makeImage(width: 96, height: 64))
        let loader = ATMThumbnailLoader(decoder: { _, _, _, _ in decoder.decode() })
        let task = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            return await loader.image(for: url, pixelWidth: 96, pixelHeight: 64)
        }
        let cancelled = await task.value
        XCTAssertNil(cancelled)
        XCTAssertEqual(decoder.count, 0)
        let active = await loader.image(for: url, pixelWidth: 96, pixelHeight: 64)
        XCTAssertNotNil(active)
        XCTAssertEqual(decoder.count, 1)
    }

    func testPastedImageWritesPNGWithPrivatePermissions() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = try await ATMPastedImageWriter.writePNG(
            data: imageData(width: 120, height: 60, type: .tiff), directory: directory
        )
        let source = try XCTUnwrap(CGImageSourceCreateWithURL(url as CFURL, nil))
        XCTAssertEqual(CGImageSourceGetType(source) as String?, UTType.png.identifier)
        let image = try XCTUnwrap(CGImageSourceCreateImageAtIndex(source, 0, nil))
        XCTAssertEqual(image.width, 120)
        XCTAssertEqual(image.height, 60)
        let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
        XCTAssertEqual((attributes[.posixPermissions] as? NSNumber)?.intValue, 0o600)
    }

    func testInvalidAndCancelledPasteLeaveNoFiles() async throws {
        let directory = try makeDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMPastedImageWriter.writePNG(data: Data("invalid image".utf8), directory: directory)
            XCTFail("Invalid data should fail")
        } catch ATMPastedImageWriter.WriteError.invalidImage {
        }
        let data = try imageData(width: 512, height: 512)
        let task = Task {
            // Cancel deterministically before calling the writer rather than
            // depending on racing a fast image conversion in the test runner.
            withUnsafeCurrentTask { $0?.cancel() }
            return try await ATMPastedImageWriter.writePNG(data: data, directory: directory)
        }
        do {
            _ = try await task.value
            XCTFail("Cancelled conversion should fail")
        } catch is CancellationError {
        }
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: directory.path), [])
    }

    private func makeDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("atm-images-test-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    private func makeImage(width: Int, height: Int) throws -> CGImage {
        let context = try XCTUnwrap(CGContext(
            data: nil, width: width, height: height, bitsPerComponent: 8,
            bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ))
        context.setFillColor(CGColor(red: 0.2, green: 0.5, blue: 0.8, alpha: 1))
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        return try XCTUnwrap(context.makeImage())
    }

    private func imageData(width: Int, height: Int, type: UTType = .png) throws -> Data {
        let data = NSMutableData()
        let destination = try XCTUnwrap(CGImageDestinationCreateWithData(data, type.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, try makeImage(width: width, height: height), nil)
        XCTAssertTrue(CGImageDestinationFinalize(destination))
        return data as Data
    }
}

private final class CountingDecoder: @unchecked Sendable {
    private let lock = NSLock()
    private let image: CGImage
    private var storedCount = 0

    init(image: CGImage) { self.image = image }

    var count: Int { lock.withLock { storedCount } }

    func decode() -> CGImage {
        lock.withLock { storedCount += 1 }
        return image
    }
}
