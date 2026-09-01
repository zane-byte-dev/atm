import Darwin
import Foundation

/// Accepts newline-delimited `ATMAgentEvent` JSON on a unix domain socket.
///
/// Written against POSIX sockets rather than `NWListener` because this listener
/// has three requirements Network.framework does not give directly: unlink a
/// stale socket file left by a crashed app, own the file mode (0600 — the events
/// carry prompt and reply text), and bind one exact path that the `atm` CLI
/// computes independently.
final class ATMAgentEventListener {
    /// Longest socket path `sockaddr_un.sun_path` accepts on Darwin, minus the
    /// terminator. Exceeding it fails with a bare EINVAL, so it is checked up
    /// front and reported as a real reason.
    static let maximumPathLength = 103

    enum StartError: LocalizedError {
        case pathTooLong(String)
        case socketFailed(errno: Int32)
        case bindFailed(path: String, errno: Int32)
        case listenFailed(errno: Int32)

        var errorDescription: String? {
            switch self {
            case .pathTooLong(let path):
                return "Socket 路径超过 \(ATMAgentEventListener.maximumPathLength) 字节上限：\(path)"
            case .socketFailed(let code):
                return "创建 socket 失败：\(String(cString: strerror(code)))"
            case .bindFailed(let path, let code):
                return "绑定 \(path) 失败：\(String(cString: strerror(code)))"
            case .listenFailed(let code):
                return "监听失败：\(String(cString: strerror(code)))"
            }
        }
    }

    /// Environment override, matching `agentevent.SocketEnvVar` on the Go side.
    static let socketPathEnvironmentKey = "ATM_NOTCH_SOCKET"

    static func defaultSocketPath(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        home: String = FileManager.default.homeDirectoryForCurrentUser.path
    ) -> String {
        if let override = environment[socketPathEnvironmentKey], !override.isEmpty {
            return override
        }
        return (home as NSString)
            .appendingPathComponent(".atm")
            .appending("/notch.sock")
    }

    private let path: String
    private let queue = DispatchQueue(label: "com.atm.agent-event-listener")
    private let onEvent: @Sendable (ATMAgentEvent) -> Void
    private let onGuardRequest: @Sendable (ATMGuardRequest) -> Void
    private var listenDescriptor: Int32 = -1
    private var acceptSource: DispatchSourceRead?
    private var connections: [Int32: ATMAgentEventConnection] = [:]

    /// `onGuardRequest` defaults to a no-op so a caller that only cares about
    /// agent events — which is every caller but the one that raises approval
    /// banners — stays unchanged.
    init(
        path: String,
        onEvent: @escaping @Sendable (ATMAgentEvent) -> Void,
        onGuardRequest: @escaping @Sendable (ATMGuardRequest) -> Void = { _ in }
    ) {
        self.path = path
        self.onEvent = onEvent
        self.onGuardRequest = onGuardRequest
    }

    var socketPath: String { path }

    func start() throws {
        guard path.utf8.count <= Self.maximumPathLength else {
            throw StartError.pathTooLong(path)
        }

        let directory = (path as NSString).deletingLastPathComponent
        // 0700: the socket carries conversation text, so keep the whole
        // directory off-limits to other local users.
        try? FileManager.default.createDirectory(
            atPath: directory,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )

        // A socket file left behind by a previous run would make bind fail with
        // EADDRINUSE even though nothing is listening. Removing it is safe: a
        // live listener in another process keeps working on its own descriptor,
        // and hooks fail closed (they exit 0 silently) if they lose the path.
        unlink(path)

        let descriptor = socket(AF_UNIX, SOCK_STREAM, 0)
        guard descriptor >= 0 else { throw StartError.socketFailed(errno: errno) }

        // Non-blocking is mandatory, not an optimization: the accept handler
        // drains the backlog in a loop, and on a blocking socket the call after
        // the last pending connection would park the listener queue forever
        // instead of returning EAGAIN.
        setNonBlocking(descriptor)

        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let pathBytes = Array(path.utf8)
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.copyBytes(from: pathBytes)
            buffer[pathBytes.count] = 0
        }
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)

        let bound = withUnsafePointer(to: &address) { pointer -> Int32 in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
                Darwin.bind(descriptor, sockaddrPointer, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard bound == 0 else {
            let code = errno
            close(descriptor)
            throw StartError.bindFailed(path: path, errno: code)
        }

        chmod(path, 0o600)

        guard listen(descriptor, 16) == 0 else {
            let code = errno
            close(descriptor)
            unlink(path)
            throw StartError.listenFailed(errno: code)
        }

        listenDescriptor = descriptor
        let source = DispatchSource.makeReadSource(fileDescriptor: descriptor, queue: queue)
        source.setEventHandler { [weak self] in self?.acceptPending() }
        source.setCancelHandler { close(descriptor) }
        acceptSource = source
        source.resume()
    }

    func stop() {
        acceptSource?.cancel()
        acceptSource = nil
        listenDescriptor = -1
        queue.sync {
            for connection in connections.values {
                connection.cancel()
            }
            connections.removeAll()
        }
        unlink(path)
    }

    deinit {
        acceptSource?.cancel()
    }

    private func acceptPending() {
        // One readable event can cover several pending connections, so drain
        // until the backlog is empty instead of accepting a single one. The
        // socket is non-blocking, so EAGAIN is how "backlog empty" arrives.
        while true {
            let incoming = accept(listenDescriptor, nil, nil)
            if incoming < 0 {
                if errno == EINTR { continue }
                return
            }
            setNonBlocking(incoming)
            let connection = ATMAgentEventConnection(
                descriptor: incoming,
                queue: queue,
                onEvent: onEvent,
                onGuardRequest: onGuardRequest,
                onClose: { [weak self] descriptor in
                    self?.connections.removeValue(forKey: descriptor)
                }
            )
            connections[incoming] = connection
            connection.resume()
        }
    }
}

/// Puts a descriptor in non-blocking mode, so reads and accepts report EAGAIN
/// rather than parking the shared listener queue.
private func setNonBlocking(_ descriptor: Int32) {
    let flags = fcntl(descriptor, F_GETFL, 0)
    guard flags >= 0 else { return }
    _ = fcntl(descriptor, F_SETFL, flags | O_NONBLOCK)
}

/// One accepted connection, framing newline-delimited JSON.
///
/// A sender may deliver several events on one connection, and a single read may
/// land mid-line, so bytes accumulate in a buffer and only complete lines are
/// decoded.
private final class ATMAgentEventConnection {
    /// Cap on buffered bytes for one unterminated line. A sender that never
    /// writes a newline must not be able to grow the app's memory without bound.
    private static let maximumBufferedBytes = 1 << 20

    private let descriptor: Int32
    private let onEvent: @Sendable (ATMAgentEvent) -> Void
    private let onGuardRequest: @Sendable (ATMGuardRequest) -> Void
    private let onClose: (Int32) -> Void
    private let source: DispatchSourceRead
    private var buffer = Data()
    private var isClosed = false

    init(
        descriptor: Int32,
        queue: DispatchQueue,
        onEvent: @escaping @Sendable (ATMAgentEvent) -> Void,
        onGuardRequest: @escaping @Sendable (ATMGuardRequest) -> Void,
        onClose: @escaping (Int32) -> Void
    ) {
        self.descriptor = descriptor
        self.onEvent = onEvent
        self.onGuardRequest = onGuardRequest
        self.onClose = onClose
        source = DispatchSource.makeReadSource(fileDescriptor: descriptor, queue: queue)
        source.setEventHandler { [weak self] in self?.readAvailable() }
        source.setCancelHandler { close(descriptor) }
    }

    func resume() { source.resume() }

    func cancel() {
        guard !isClosed else { return }
        isClosed = true
        source.cancel()
    }

    private func readAvailable() {
        var chunk = [UInt8](repeating: 0, count: 4096)
        let count = read(descriptor, &chunk, chunk.count)
        if count > 0 {
            buffer.append(contentsOf: chunk[0..<count])
            drainCompleteLines()
            if buffer.count > Self.maximumBufferedBytes {
                // Not a sender we recognize; drop it rather than grow forever.
                finish()
            }
            return
        }
        if count < 0, errno == EINTR || errno == EAGAIN { return }
        // Zero means the peer closed after writing; decode whatever is complete.
        drainCompleteLines()
        finish()
    }

    private func drainCompleteLines() {
        while let newlineIndex = buffer.firstIndex(of: 0x0A) {
            let line = buffer[buffer.startIndex..<newlineIndex]
            buffer.removeSubrange(buffer.startIndex...newlineIndex)
            guard !line.isEmpty else { continue }
            switch ATMAgentEventDecoder.decodeMessage(Data(line)) {
            case .agent(let event): onEvent(event)
            case .guardRequest(let request): onGuardRequest(request)
            case nil: continue
            }
        }
    }

    private func finish() {
        cancel()
        onClose(descriptor)
    }
}

/// Decoding kept separate from the socket so it can be unit tested without any
/// file descriptors.
/// What one line on the notch socket can be.
///
/// The socket carries a discriminated union rather than a sixth agent-event kind:
/// an approval request is not about a session, and a new kind would be read by
/// everything that consumes agent events — crediting a session with turn-state
/// reporting, joining a cwd, forcing a status refresh — none of which is true of
/// it. A line with no `type` is an agent event, so every hook already installed
/// keeps working byte for byte.
enum ATMNotchMessage: Equatable {
    case agent(ATMAgentEvent)
    case guardRequest(ATMGuardRequest)
}

enum ATMAgentEventDecoder {
    /// Peeks the discriminator, then decodes into the matching shape. An unknown
    /// `type` yields nil, so a newer CLI against an older app drops the line
    /// rather than misreading it.
    static func decodeMessage(_ line: Data) -> ATMNotchMessage? {
        struct Discriminator: Decodable { let type: String? }
        let kind = (try? JSONDecoder().decode(Discriminator.self, from: line))?.type
        switch kind {
        case nil, "":
            guard let event = decode(line) else { return nil }
            return .agent(event)
        case "guard_request":
            guard let request = try? JSONDecoder().decode(ATMGuardRequest.self, from: line),
                  request.isSupported
            else { return nil }
            return .guardRequest(request)
        default:
            return nil
        }
    }

    static func decode(_ line: Data) -> ATMAgentEvent? {
        guard let event = try? JSONDecoder().decode(ATMAgentEvent.self, from: line) else {
            return nil
        }
        // An unknown future version might mean something different by the same
        // field names, so ignore it instead of acting on a guess.
        guard event.isSupported else { return nil }
        return event
    }

    /// Splits a byte stream into complete lines plus the unterminated remainder.
    /// Exposed for tests that assert framing across chunk boundaries.
    static func splitLines(_ buffer: Data) -> (lines: [Data], remainder: Data) {
        var lines: [Data] = []
        var rest = buffer
        while let newlineIndex = rest.firstIndex(of: 0x0A) {
            let line = rest[rest.startIndex..<newlineIndex]
            if !line.isEmpty { lines.append(Data(line)) }
            rest.removeSubrange(rest.startIndex...newlineIndex)
        }
        return (lines, rest)
    }
}
