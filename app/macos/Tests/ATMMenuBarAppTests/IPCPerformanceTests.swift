import Darwin
import XCTest
@testable import ATMMenuBarApp

final class IPCPerformanceTests: XCTestCase {
    private struct ConvertedPayload: Decodable {
        let displayName: String
        let exactId: Int64
    }

    private struct WirePayload: Decodable {
        let displayName: String
        enum CodingKeys: String, CodingKey { case displayName = "display_name" }
    }

    private func script(_ body: String) throws -> (ATMCommandRunner, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-ipc-performance-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let executable = directory.appendingPathComponent("atm")
        try ("#!/bin/sh\n" + body + "\n").write(to: executable, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
        return (try ATMCommandRunner(environment: ["ATM_EXECUTABLE": executable.path]), directory)
    }

    private func response(_ json: String, status: Int32 = 0) throws -> (ATMCommandRunner, URL) {
        let quoted = "'" + json.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
        return try script("/usr/bin/printf '%s' \(quoted)\n/usr/bin/printf '%s' legacy-error >&2\nexit \(status)")
    }

    func testTypedPayloadUsesSelectedKeyStrategyAndPreservesLargeInteger() async throws {
        let json = #"{"protocol_version":1,"envelope_version":1,"request_id":"p1","verb":"example.read","data":{"display_name":"ATM","exact_id":9007199254740993}}"#
        let (runner, directory) = try response(json)
        defer { try? FileManager.default.removeItem(at: directory) }
        let client = ATMIPCClient(runner: runner)

        let converted = try await client.call(ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read"))
        XCTAssertEqual(converted.displayName, "ATM")
        XCTAssertEqual(converted.exactId, 9_007_199_254_740_993)

        let wire = try await client.call(ATMIPCMethod<ATMIPCNoRequest, WirePayload>(
            "example.read", responseKeyDecoding: .useDefault
        ))
        XCTAssertEqual(wire.displayName, "ATM")
    }

    func testProtocolMismatchWinsOverChangedEnvelopeAndPayloadShapes() async throws {
        let json = #"{"protocol_version":2,"envelope_version":{"new":"shape"},"request_id":[],"verb":[],"error":[],"data":[]}"#
        let (runner, directory) = try response(json, status: 1)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMIPCClient(runner: runner).call(
                ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read")
            )
            XCTFail("expected protocol mismatch")
        } catch let error as ATMIPCProtocolMismatch {
            XCTAssertEqual(error.cliVersion, 2)
        }

        XCTAssertThrowsError(try JSONDecoder().decode(ATMIPCEnvelope<WirePayload>.self, from: Data(json.utf8))) {
            XCTAssertTrue($0 is ATMIPCProtocolMismatch)
        }
    }

    func testEnvelopeMismatchWinsOverChangedVerbAndErrorShapes() async throws {
        let json = #"{"protocol_version":1,"envelope_version":2,"request_id":[],"verb":[],"error":[],"data":[]}"#
        let (runner, directory) = try response(json, status: 1)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMIPCClient(runner: runner).call(
                ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read")
            )
            XCTFail("expected envelope mismatch")
        } catch let error as ATMIPCEnvelopeVersionMismatch {
            XCTAssertEqual(error.cliVersion, 2)
        }
    }

    func testVerbMismatchWinsOverMalformedRemoteErrorAndPayload() async throws {
        let json = #"{"protocol_version":1,"envelope_version":1,"request_id":"wrong-verb","verb":"example.other","error":[],"data":[]}"#
        let (runner, directory) = try response(json, status: 1)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMIPCClient(runner: runner).call(
                ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read")
            )
            XCTFail("expected verb mismatch")
        } catch let error as ATMIPCVerbMismatch {
            XCTAssertEqual(error.requestID, "wrong-verb")
        }
    }

    func testRemoteErrorWinsOverMalformedSuccessPayload() async throws {
        let json = #"{"protocol_version":1,"envelope_version":1,"request_id":"conflict","verb":"example.read","error":{"code":"conflict","message":"changed","retryable":true,"details":{"current_revision":4}},"data":[]}"#
        let (runner, directory) = try response(json, status: 7)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMIPCClient(runner: runner).call(
                ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read")
            )
            XCTFail("expected remote error")
        } catch let error as ATMIPCRemoteError {
            XCTAssertEqual(error.code, "conflict")
            XCTAssertEqual(error.details, .object(["current_revision": .number(4)]))
        }
    }

    func testLegacyNonzeroExitPreservesStderr() async throws {
        let (runner, directory) = try response("not-json", status: 9)
        defer { try? FileManager.default.removeItem(at: directory) }
        do {
            _ = try await ATMIPCClient(runner: runner).call(
                ATMIPCMethod<ATMIPCNoRequest, ConvertedPayload>("example.read")
            )
            XCTFail("expected command failure")
        } catch let ATMCommandError.failed(_, status, message) {
            XCTAssertEqual(status, 9)
            XCTAssertEqual(message, "legacy-error")
        }
    }

    func testSuccessStillRequiresNonNullPayload() async throws {
        for field in ["", #", "data": null"#] {
            let json = #"{"protocol_version":1,"envelope_version":1,"request_id":"empty","verb":"example.read""# + field + "}"
            let (runner, directory) = try response(json)
            defer { try? FileManager.default.removeItem(at: directory) }
            do {
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMIPCMethod<ATMIPCNoRequest, ATMJSONValue>("example.read")
                )
                XCTFail("expected missing payload failure")
            } catch is DecodingError {
                // Explicit JSON null was not a successful payload before either.
            }
        }
    }

    func testProcessDrainsLargeStreamsWhileSendingLargeInput() async throws {
        let (runner, directory) = try script("""
        /usr/bin/head -c 1048576 /dev/zero
        /usr/bin/head -c 1048576 /dev/zero >&2
        /bin/cat
        """)
        defer { try? FileManager.default.removeItem(at: directory) }
        let input = Data(repeating: 65, count: 1_048_576)
        let result = try await runner.runRaw([], standardInput: input, timeout: 5)
        XCTAssertEqual(result.standardOutput.count, 2_097_152)
        XCTAssertEqual(Data(result.standardOutput.suffix(input.count)), input)
        XCTAssertEqual(result.standardError.count, 1_048_576)
        XCTAssertEqual(result.terminationStatus, 0)
    }

    func testImmediateExitCallbacksDoNotLoseEmptyOrSmallOutput() async throws {
        let (runner, directory) = try script("/usr/bin/printf '%s' \"$1\"")
        defer { try? FileManager.default.removeItem(at: directory) }
        for index in 0..<12 {
            let expected = index.isMultiple(of: 2) ? "" : "\(index)"
            let result = try await runner.runRaw([expected], timeout: 2)
            XCTAssertEqual(String(decoding: result.standardOutput, as: UTF8.self), expected)
            XCTAssertEqual(result.terminationStatus, 0)
        }
    }

    func testTimeoutEscalatesForProcessIgnoringTermination() async throws {
        let (runner, directory) = try script("""
        trap '' TERM
        printf '%s' "$$" > "$1"
        while :; do :; done
        """)
        defer { try? FileManager.default.removeItem(at: directory) }
        let pidFile = directory.appendingPathComponent("pid")
        // Launching a new shell fixture can take several hundred milliseconds
        // under the full suite. The deadline must leave time to install the
        // signal handler; otherwise this tests early TERM, not KILL escalation.
        let timeout: TimeInterval = 3
        let started = Date()
        let task = Task { try await runner.run([pidFile.path], timeout: timeout) }
        defer { task.cancel() }
        let readyDeadline = Date().addingTimeInterval(2)
        var readyPID: Int32?
        while readyPID == nil, Date() < readyDeadline {
            if let text = try? String(contentsOf: pidFile, encoding: .utf8),
               let pid = Int32(text), pid > 0 {
                readyPID = pid
            } else {
                try await Task.sleep(nanoseconds: 10_000_000)
            }
        }
        guard let pid = readyPID else {
            task.cancel()
            _ = try? await task.value
            return XCTFail("child did not publish its PID after installing TERM ignore within the startup budget")
        }
        XCTAssertEqual(kill(pid, 0), 0, "child must be alive with TERM ignored before its deadline")
        do {
            _ = try await task.value
            XCTFail("expected timeout")
        } catch let error as ATMCommandError {
            guard case .timedOut = error else { return XCTFail("unexpected command error: \(error)") }
            let elapsed = Date().timeIntervalSince(started)
            XCTAssertGreaterThanOrEqual(elapsed, timeout + 0.45, "TERM-resistant child must receive the escalation grace period")
            XCTAssertLessThan(elapsed, timeout + 2)
        }
        XCTAssertEqual(kill(pid, 0), -1, "timeout returned before the child was reaped")
        XCTAssertEqual(errno, ESRCH)
    }

    func testCancellationEscalatesForProcessIgnoringTermination() async throws {
        let (runner, directory) = try script("""
        trap '' TERM
        /usr/bin/printf '%s' "$$" > "$1"
        while :; do :; done
        """)
        defer { try? FileManager.default.removeItem(at: directory) }
        let pidFile = directory.appendingPathComponent("pid")
        let task = Task { try await runner.run([pidFile.path], timeout: 5) }
        let launchDeadline = Date().addingTimeInterval(2)
        while !FileManager.default.fileExists(atPath: pidFile.path), Date() < launchDeadline {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        task.cancel()
        do {
            _ = try await task.value
            XCTFail("expected cancellation")
        } catch is CancellationError {}
        let pid = try XCTUnwrap(Int32(String(contentsOf: pidFile, encoding: .utf8)))
        XCTAssertEqual(kill(pid, 0), -1, "cancellation returned before the child was reaped")
        XCTAssertEqual(errno, ESRCH)
    }

    func testTimeoutCanInterruptChildThatDoesNotReadLargeInput() async throws {
        let (runner, directory) = try script("exec /bin/sleep 5")
        defer { try? FileManager.default.removeItem(at: directory) }
        let started = Date()
        do {
            _ = try await runner.run([], standardInput: Data(repeating: 0, count: 2_097_152), timeout: 0.1)
            XCTFail("expected timeout")
        } catch let error as ATMCommandError {
            guard case .timedOut = error else { return XCTFail("unexpected command error: \(error)") }
            XCTAssertLessThan(Date().timeIntervalSince(started), 3)
        }
    }
}
