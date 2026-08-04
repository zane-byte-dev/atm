import XCTest
@testable import ATMMenuBarApp

/// Exercises the real socket, end to end across both languages: the Swift
/// listener binds, the Go `atm agent hook` binary connects and writes, and the
/// app decodes what arrives.
///
/// The unit tests cover framing and joining with plain byte buffers. What only a
/// real socket can catch is the part that is easy to get subtly wrong and
/// impossible to notice from tests on either side alone: the `sockaddr_un`
/// layout, the path length ceiling, the file mode, and the two independent
/// implementations of "where does the socket live".
final class AgentEventListenerIntegrationTests: XCTestCase {
    /// Socket paths must fit `sockaddr_un.sun_path`, and the default temp
    /// directory on macOS is long enough on its own to blow the limit.
    private func shortTemporaryDirectory() throws -> String {
        let path = "/tmp/atm-notch-test-\(ProcessInfo.processInfo.processIdentifier)-\(UUID().uuidString.prefix(8))"
        try FileManager.default.createDirectory(atPath: path, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(atPath: path) }
        return path
    }

    /// The repository's built CLI, or nil when it has not been built yet.
    ///
    /// Anchored on this file's own location rather than the working directory,
    /// which differs between `swift test` and Xcode.
    private func atmBinary() -> String? {
        // …/app/macos/Tests/ATMMenuBarAppTests/<this file> → repo root is 4 up.
        var directory = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        for _ in 0..<4 {
            directory = directory.deletingLastPathComponent()
        }
        let binary = directory.appendingPathComponent("bin/atm").path
        return FileManager.default.isExecutableFile(atPath: binary) ? binary : nil
    }

    private func runHook(
        binary: String,
        socketPath: String,
        payload: String,
        arguments: [String]
    ) throws -> (status: Int32, stdout: String, stderr: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: binary)
        process.arguments = ["agent", "hook"] + arguments
        var environment = ProcessInfo.processInfo.environment
        environment["ATM_NOTCH_SOCKET"] = socketPath
        process.environment = environment

        let input = Pipe(), output = Pipe(), errorPipe = Pipe()
        process.standardInput = input
        process.standardOutput = output
        process.standardError = errorPipe
        try process.run()
        input.fileHandleForWriting.write(Data(payload.utf8))
        input.fileHandleForWriting.closeFile()
        let stdout = output.fileHandleForReading.readDataToEndOfFile()
        let stderr = errorPipe.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return (
            process.terminationStatus,
            String(decoding: stdout, as: UTF8.self),
            String(decoding: stderr, as: UTF8.self)
        )
    }

    func testCLIDeliversAnEventToTheListeningApp() throws {
        guard let binary = atmBinary() else {
            throw XCTSkip("bin/atm has not been built; run `make build` in the repo root")
        }
        let socketPath = try shortTemporaryDirectory() + "/notch.sock"

        let received = expectation(description: "event received")
        // nonisolated(unsafe): only written on the listener queue before the
        // expectation is fulfilled, and only read after waiting on it.
        nonisolated(unsafe) var event: ATMAgentEvent?
        let listener = ATMAgentEventListener(path: socketPath) { incoming in
            event = incoming
            received.fulfill()
        }
        try listener.start()
        addTeardownBlock { listener.stop() }

        let result = try runHook(
            binary: binary,
            socketPath: socketPath,
            payload: #"{"hook_event_name":"Notification","session_id":"abc","cwd":"/w","message":"needs permission"}"#,
            arguments: ["--source", "claude", "--reason", "permission_prompt"]
        )
        XCTAssertEqual(result.status, 0, "stderr: \(result.stderr)")
        XCTAssertEqual(result.stdout, "", "the hook must never write to stdout")

        wait(for: [received], timeout: 5)
        XCTAssertEqual(event?.event, .attention)
        XCTAssertEqual(event?.sessionID, "abc")
        XCTAssertEqual(event?.reason, "permission_prompt")
        XCTAssertEqual(event?.tool, nil)
        XCTAssertEqual(event?.source, "claude")
    }

    func testSocketIsOwnerOnlyBecauseEventsCarryConversationText() throws {
        let socketPath = try shortTemporaryDirectory() + "/notch.sock"
        let listener = ATMAgentEventListener(path: socketPath) { _ in }
        try listener.start()
        addTeardownBlock { listener.stop() }

        let attributes = try FileManager.default.attributesOfItem(atPath: socketPath)
        let permissions = attributes[.posixPermissions] as? NSNumber
        XCTAssertEqual(permissions?.int16Value, 0o600)
    }

    func testStartReplacesAStaleSocketFileLeftByACrash() throws {
        let directory = try shortTemporaryDirectory()
        let socketPath = directory + "/notch.sock"
        // A socket file with no listener behind it: bind would fail with
        // EADDRINUSE, so start() has to unlink it first. Without this the notch
        // would stay deaf after any hard crash until the user cleaned up by hand.
        FileManager.default.createFile(atPath: socketPath, contents: Data())

        let listener = ATMAgentEventListener(path: socketPath) { _ in }
        XCTAssertNoThrow(try listener.start())
        addTeardownBlock { listener.stop() }
    }

    func testStopRemovesTheSocketSoTheNextLaunchCanBind() throws {
        let socketPath = try shortTemporaryDirectory() + "/notch.sock"
        let listener = ATMAgentEventListener(path: socketPath) { _ in }
        try listener.start()
        XCTAssertTrue(FileManager.default.fileExists(atPath: socketPath))
        listener.stop()
        XCTAssertFalse(FileManager.default.fileExists(atPath: socketPath))
    }

    func testOverlongPathFailsWithAReasonRatherThanErrno() throws {
        let directory = try shortTemporaryDirectory()
        let socketPath = directory + "/" + String(repeating: "x", count: 120) + ".sock"
        let listener = ATMAgentEventListener(path: socketPath) { _ in }
        XCTAssertThrowsError(try listener.start()) { error in
            XCTAssertTrue(
                error.localizedDescription.contains("上限"),
                "expected an explanatory message, got: \(error.localizedDescription)"
            )
        }
    }

    func testSeveralEventsOnOneConnectionAreAllDecoded() throws {
        let socketPath = try shortTemporaryDirectory() + "/notch.sock"
        let received = expectation(description: "both events received")
        received.expectedFulfillmentCount = 2
        nonisolated(unsafe) var kinds: [ATMAgentEvent.Kind] = []
        let listener = ATMAgentEventListener(path: socketPath) { incoming in
            kinds.append(incoming.event)
            received.fulfill()
        }
        try listener.start()
        addTeardownBlock { listener.stop() }

        // Two envelopes in one write, which is what a sender batching events
        // looks like on the wire.
        let payload = #"{"v":1,"source":"pi","event":"attention","session_id":"abc","reason":"settled"}"# + "\n"
            + #"{"v":1,"source":"pi","event":"started","session_id":"abc"}"# + "\n"
        try writeRaw(payload, to: socketPath)

        wait(for: [received], timeout: 5)
        XCTAssertEqual(kinds, [.attention, .started])
    }

    /// Connects as a plain client, the way the Pi TypeScript extension does
    /// without going through the `atm` binary.
    private func writeRaw(_ payload: String, to path: String) throws {
        let descriptor = socket(AF_UNIX, SOCK_STREAM, 0)
        XCTAssertGreaterThanOrEqual(descriptor, 0)
        defer { close(descriptor) }

        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.copyBytes(from: bytes)
            buffer[bytes.count] = 0
        }
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let connected = withUnsafePointer(to: &address) { pointer -> Int32 in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
                connect(descriptor, sockaddrPointer, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        XCTAssertEqual(connected, 0, "connect failed: \(String(cString: strerror(errno)))")
        let data = Array(payload.utf8)
        XCTAssertEqual(write(descriptor, data, data.count), data.count)
    }
}
