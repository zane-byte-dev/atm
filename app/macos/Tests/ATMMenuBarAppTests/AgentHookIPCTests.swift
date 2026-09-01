import XCTest
@testable import ATMMenuBarApp

final class AgentHookIPCTests: XCTestCase {
    func testMethodVocabularyAndRequestHaveNoArgvEscapeHatch() throws {
        XCTAssertEqual(ATMAgentHookIPCCommand.install.arguments, ["_ipc", "agent.hook.install"])
        XCTAssertEqual(ATMAgentHookIPCCommand.status.arguments, ["_ipc", "agent.hook.status"])
        XCTAssertEqual(ATMAgentHookIPCCommand.uninstall.arguments, ["_ipc", "agent.hook.uninstall"])

        let data = try JSONEncoder().encode(ATMAgentHookRequest(source: "claude"))
        let request = try XCTUnwrap(
            JSONSerialization.jsonObject(with: data) as? [String: Any]
        )
        XCTAssertEqual(request["source"] as? String, "claude")
        XCTAssertNil(request["arguments"])
        XCTAssertNil(request["argv"])
        XCTAssertNil(request["action"])
    }

    /// Runs the three settings operations against the release-check's real Go
    /// binary. The temporary HOME makes the write reversible while still proving
    /// that Go's service response crosses the typed envelope and decodes in Swift.
    func testRealGoAgentHookLifecycleDecodesInSwift() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-agent-hook-ipc-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }

        let request = try JSONEncoder().encode(ATMAgentHookRequest(source: "claude"))
        let installed = try runGoIPC(
            executable: executable, verb: "agent.hook.install", home: home, input: request
        )
        XCTAssertEqual(installed.status, 0, installed.stderr)
        let installReport = try decode(installed.stdout)
        XCTAssertEqual(installReport.sources.map(\.source), ["claude"])
        XCTAssertFalse(installReport.sources[0].added.isEmpty)

        let status = try runGoIPC(
            executable: executable, verb: "agent.hook.status", home: home, input: request
        )
        XCTAssertEqual(status.status, 0, status.stderr)
        let statusReport = try decode(status.stdout)
        XCTAssertFalse(statusReport.sources[0].installed.isEmpty)
        XCTAssertTrue(statusReport.sources[0].missing.isEmpty)

        let uninstalled = try runGoIPC(
            executable: executable, verb: "agent.hook.uninstall", home: home, input: request
        )
        XCTAssertEqual(uninstalled.status, 0, uninstalled.stderr)
        let uninstallReport = try decode(uninstalled.stdout)
        XCTAssertFalse(uninstallReport.sources[0].removed.isEmpty)
    }

    private func decode(_ data: Data) throws -> ATMAgentHookReport {
        try JSONDecoder().decode(
            ATMIPCEnvelope<ATMAgentHookReport>.self,
            from: data
        ).data
    }

    private struct CLIResult {
        let status: Int32
        let stdout: Data
        let stderr: String
    }

    private func runGoIPC(
        executable: String,
        verb: String,
        home: URL,
        input: Data
    ) throws -> CLIResult {
        let process = Process()
        let stdout = Pipe()
        let stderr = Pipe()
        let stdin = Pipe()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = ["_ipc", verb]
        process.standardOutput = stdout
        process.standardError = stderr
        process.standardInput = stdin
        process.currentDirectoryURL = home
        var environment = ProcessInfo.processInfo.environment
        environment["HOME"] = home.path
        environment["ATM_SKIP_LOCAL_NOTIFICATION"] = "1"
        process.environment = environment
        try process.run()
        stdin.fileHandleForWriting.write(input)
        try stdin.fileHandleForWriting.close()
        process.waitUntilExit()
        return CLIResult(
            status: process.terminationStatus,
            stdout: stdout.fileHandleForReading.readDataToEndOfFile(),
            stderr: String(
                data: stderr.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8
            ) ?? ""
        )
    }
}
