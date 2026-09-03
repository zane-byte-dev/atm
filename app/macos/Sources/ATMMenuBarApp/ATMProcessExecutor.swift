import Darwin
import Foundation

/// Runs CLI work entirely off the main actor. Process termination and pipe
/// readiness wake this executor; short commands no longer wait for a poll tick.
enum ATMProcessExecutor {
    static func run(
        executableURL: URL,
        arguments: [String],
        standardInput: Data?,
        timeout: TimeInterval
    ) async throws -> ATMCommandProcessResult {
        let execution = ATMProcessExecution(
            executableURL: executableURL,
            arguments: arguments,
            standardInput: standardInput,
            timeout: timeout
        )
        return try await withTaskCancellationHandler {
            do {
                let result = try await withCheckedThrowingContinuation { continuation in
                    execution.start(continuation)
                }
                try Task.checkCancellation()
                return result
            } catch {
                try Task.checkCancellation()
                throw error
            }
        } onCancel: {
            execution.cancel()
        }
    }
}

/// All mutable state, including launch/cancel races, belongs to one utility
/// queue. Nonblocking pipes also let timeout/cancel interrupt a child that stops
/// reading a large request, without occupying a cooperative executor thread.
private final class ATMProcessExecution: @unchecked Sendable {
    private let queue = DispatchQueue(label: "atm.command.process", qos: .utility)
    private let process = Process()
    private let stdout = Pipe()
    private let stderr = Pipe()
    private let stdin: Pipe?
    private let input: Data?
    private let arguments: [String]
    private let timeout: TimeInterval

    private var continuation: CheckedContinuation<ATMCommandProcessResult, Error>?
    private var outputSource: DispatchSourceRead?
    private var errorSource: DispatchSourceRead?
    private var inputSource: DispatchSourceWrite?
    private var timeoutWork: DispatchWorkItem?
    private var killWork: DispatchWorkItem?
    private var output = Data()
    private var errorOutput = Data()
    private var inputOffset = 0
    private var status: Int32?
    private var failure: Error?
    private var cancelled = false
    private var finished = false

    init(executableURL: URL, arguments: [String], standardInput: Data?, timeout: TimeInterval) {
        self.arguments = arguments
        self.input = standardInput
        self.stdin = standardInput.map { _ in Pipe() }
        self.timeout = timeout
        process.executableURL = executableURL
        process.arguments = arguments
        process.standardOutput = stdout
        process.standardError = stderr
        process.standardInput = stdin
        var environment = ProcessInfo.processInfo.environment
        let commonPath = "/usr/local/bin:/opt/homebrew/bin:\(FileManager.default.homeDirectoryForCurrentUser.path)/.local/bin"
        environment["PATH"] = commonPath + ":" + (environment["PATH"] ?? "")
        environment["ATM_SKIP_LOCAL_NOTIFICATION"] = "1"
        process.environment = environment
    }

    func start(_ continuation: CheckedContinuation<ATMCommandProcessResult, Error>) {
        queue.async {
            self.continuation = continuation
            guard !self.cancelled else {
                self.finish(.failure(CancellationError()))
                return
            }
            self.process.terminationHandler = { [weak self] process in
                let status = process.terminationStatus
                guard let self else { return }
                self.queue.async { self.didTerminate(status: status) }
            }
            do {
                try self.process.run()
            } catch {
                self.finish(.failure(error))
                return
            }

            // Only the child owns these ends after launch. Closing our copies
            // makes EOF reflect the child, including commands that exit at once.
            try? self.stdout.fileHandleForWriting.close()
            try? self.stderr.fileHandleForWriting.close()
            try? self.stdin?.fileHandleForReading.close()

            self.outputSource = self.readSource(self.stdout.fileHandleForReading, isError: false)
            self.errorSource = self.readSource(self.stderr.fileHandleForReading, isError: true)
            self.startInput()
            let timeoutWork = DispatchWorkItem { [weak self] in
                guard let self, !self.finished else { return }
                self.stop(with: ATMCommandError.timedOut(arguments: self.arguments, seconds: self.timeout))
            }
            self.timeoutWork = timeoutWork
            self.queue.asyncAfter(deadline: .now() + max(0, self.timeout), execute: timeoutWork)
        }
    }

    func cancel() {
        queue.async {
            self.cancelled = true
            guard self.continuation != nil, !self.finished else { return }
            self.stop(with: CancellationError())
        }
    }

    private func readSource(_ handle: FileHandle, isError: Bool) -> DispatchSourceRead {
        makeNonblocking(handle)
        let source = DispatchSource.makeReadSource(fileDescriptor: handle.fileDescriptor, queue: queue)
        source.setEventHandler { [self] in drain(handle, isError: isError) }
        source.setCancelHandler { try? handle.close() }
        source.resume()
        return source
    }

    private func drain(_ handle: FileHandle, isError: Bool) {
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        // Bound a readiness turn so a continuously writing child cannot starve
        // the timeout, cancellation or the other stream on this serial queue.
        for _ in 0..<16 {
            let count = buffer.withUnsafeMutableBytes {
                Darwin.read(handle.fileDescriptor, $0.baseAddress, $0.count)
            }
            if count > 0 {
                if isError {
                    errorOutput.append(contentsOf: buffer.prefix(count))
                } else {
                    output.append(contentsOf: buffer.prefix(count))
                }
            } else if count < 0, errno == EINTR {
                continue
            } else if count < 0, errno == EAGAIN || errno == EWOULDBLOCK {
                return
            } else {
                closeReadSource(isError: isError)
                completeIfReady()
                return
            }
        }
    }

    private func startInput() {
        guard let input, let handle = stdin?.fileHandleForWriting else { return }
        guard !input.isEmpty else {
            try? handle.close()
            return
        }
        makeNonblocking(handle)
        // An early child exit must produce EPIPE, never terminate the app.
        _ = fcntl(handle.fileDescriptor, F_SETNOSIGPIPE, 1)
        let source = DispatchSource.makeWriteSource(fileDescriptor: handle.fileDescriptor, queue: queue)
        source.setEventHandler { [self] in
            for _ in 0..<16 where inputOffset < input.count {
                let count = input.withUnsafeBytes {
                    Darwin.write(
                        handle.fileDescriptor,
                        $0.baseAddress!.advanced(by: inputOffset),
                        min(64 * 1024, $0.count - inputOffset)
                    )
                }
                if count > 0 {
                    inputOffset += count
                } else if count < 0, errno == EINTR {
                    continue
                } else if count < 0, errno == EAGAIN || errno == EWOULDBLOCK {
                    return
                } else {
                    // The child may intentionally ignore stdin. Its exit status
                    // and IPC envelope remain the authority for command errors.
                    closeInputSource()
                    return
                }
            }
            if inputOffset == input.count { closeInputSource() }
        }
        source.setCancelHandler { try? handle.close() }
        inputSource = source
        source.resume()
    }

    private func makeNonblocking(_ handle: FileHandle) {
        let flags = fcntl(handle.fileDescriptor, F_GETFL)
        _ = fcntl(handle.fileDescriptor, F_SETFL, flags | O_NONBLOCK)
    }

    private func didTerminate(status: Int32) {
        guard !finished else { return }
        self.status = status
        killWork?.cancel()
        killWork = nil
        closeInputSource()
        completeIfReady()
    }

    private func stop(with error: Error) {
        guard !finished else { return }
        if failure == nil { failure = error }
        timeoutWork?.cancel()
        timeoutWork = nil
        if process.isRunning {
            process.terminate()
            if killWork == nil {
                let work = DispatchWorkItem { [weak self] in
                    guard let self, !self.finished, self.process.isRunning else { return }
                    kill(self.process.processIdentifier, SIGKILL)
                }
                killWork = work
                queue.asyncAfter(deadline: .now() + 0.5, execute: work)
            }
        }
        // Interrupted output is discarded. Cancel the descriptors as well so a
        // descendant retaining the pipes cannot leave background readers alive.
        closeInputSource()
        closeReadSource(isError: false)
        closeReadSource(isError: true)
        completeIfReady()
    }

    private func closeInputSource() {
        inputSource?.setEventHandler {}
        inputSource?.cancel()
        inputSource = nil
    }

    private func closeReadSource(isError: Bool) {
        let source = isError ? errorSource : outputSource
        source?.setEventHandler {}
        source?.cancel()
        if isError { errorSource = nil } else { outputSource = nil }
    }

    private func completeIfReady() {
        guard let status, outputSource == nil, errorSource == nil else { return }
        if let failure {
            finish(.failure(failure))
        } else {
            finish(.success(ATMCommandProcessResult(
                standardOutput: output,
                standardError: errorOutput,
                terminationStatus: status
            )))
        }
    }

    private func finish(_ result: Result<ATMCommandProcessResult, Error>) {
        guard !finished else { return }
        finished = true
        timeoutWork?.cancel()
        killWork?.cancel()
        timeoutWork = nil
        killWork = nil
        process.terminationHandler = nil
        // If launch failed, no dispatch sources took ownership of the handles.
        if case .failure = result, status == nil {
            try? stdout.fileHandleForReading.close()
            try? stdout.fileHandleForWriting.close()
            try? stderr.fileHandleForReading.close()
            try? stderr.fileHandleForWriting.close()
            try? stdin?.fileHandleForReading.close()
            try? stdin?.fileHandleForWriting.close()
        }
        continuation?.resume(with: result)
        continuation = nil
    }
}
