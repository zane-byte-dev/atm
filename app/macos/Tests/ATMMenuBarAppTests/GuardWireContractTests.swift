import XCTest
@testable import ATMMenuBarApp

/// The bytes in this fixture were captured from the real CLI writing to the real
/// socket, not hand-written. It is the only assertion that both sides of the wire
/// agree on field names; everything else tests one side against its own idea of
/// the other.
final class GuardWireContractTests: XCTestCase {
    func testTheCLIsActualEnvelopeDecodes() throws {
        let line = Data(#"{"v":1,"type":"guard_request","id":"ap_96afc3ca67","tool":"dws","label":"发送钉钉消息","target":"cid1","title":"上线","body":"已发布到预发","cwd":"/Users/mj/mox/atm","agent":"claude","expires_at":1787020266}"#.utf8)
        guard case .guardRequest(let request)? = ATMAgentEventDecoder.decodeMessage(line) else {
            return XCTFail("the CLI's own envelope did not decode")
        }
        XCTAssertEqual(request.tool, "dws")
        XCTAssertEqual(request.label, "发送钉钉消息")
        XCTAssertEqual(request.target, "cid1")
        XCTAssertEqual(request.title, "上线")
        XCTAssertEqual(request.body, "已发布到预发")
        XCTAssertFalse(request.id.isEmpty)
        XCTAssertNotNil(request.expiresAt)
        XCTAssertNotNil(request.cwd)
        XCTAssertEqual(request.agent, "claude")

        // Every field the CLI sends must be one the app reads: a field it silently
        // ignores is a field the banner is missing.
        let raw = try JSONSerialization.jsonObject(with: line) as! [String: Any]
        let known: Set<String> = ["v", "type", "id", "tool", "label", "target", "title",
                                  "body", "cwd", "agent", "expires_at"]
        XCTAssertTrue(Set(raw.keys).subtracting(known).isEmpty,
                      "the CLI sends fields the app drops: \(Set(raw.keys).subtracting(known))")
    }

    /// The read paths the settings pane now uses, run against the real Go binary.
    ///
    /// The captured fixture below still pins `atm guard status --json`, which the
    /// CLI keeps emitting as a bare array. Over `_ipc` the same data arrives
    /// wrapped in the service's result struct, so both shapes need their own
    /// guarantee — a decoder that silently produced an empty list would show every
    /// tool as ungated.
    func testRealGoGuardReadPathsDecodeInSwift() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-guard-ipc-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }

        let encoder = JSONEncoder()
        let status = try runGoIPC(
            executable: executable, verb: "guard.status", home: home,
            input: encoder.encode(ATMGuardStatusRequest())
        )
        XCTAssertEqual(status.status, 0, status.stderr)
        let states = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMGuardStatusResponse>.self, from: status.stdout
        ).data.states
        // Guard ships built-in tools, so an empty list means the decode dropped
        // them rather than that there is nothing to gate.
        XCTAssertFalse(states.isEmpty)
        XCTAssertFalse(states.contains { $0.tool.isEmpty })

        let rules = try runGoIPC(
            executable: executable, verb: "guard.rule.list", home: home,
            input: encoder.encode(ATMGuardRuleListRequest())
        )
        XCTAssertEqual(rules.status, 0, rules.stderr)
        let decoded = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMGuardRuleListResponse>.self, from: rules.stdout
        ).data.rules
        XCTAssertFalse(decoded.isEmpty)
        XCTAssertFalse(decoded.contains { $0.id.isEmpty || $0.tool.isEmpty })

        // The decision path must stay refused over this transport: a gate an Agent
        // can open for itself is not a gate.
        let approve = try runGoIPC(
            executable: executable, verb: "guard.approve", home: home,
            input: Data(#"{"id":"ap_whatever"}"#.utf8)
        )
        XCTAssertNotEqual(approve.status, 0, "guard.approve must not be a desktop method")
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
        let stdout = Pipe(), stderr = Pipe(), stdin = Pipe()
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

    /// Captured from `atm guard status --json`. Pins the field names the settings
    /// pane reads: a rename on the Go side would otherwise surface as a tool that
    /// silently reports itself not installed.
    func testStatusJSONFromTheCLIDecodes() throws {
        let json = Data(
            """
            [{"tool":"a1","bin_path":"/Users/x/.local/bin/a1",
              "real_path":"/Users/x/.local/bin/a1-atm-real",
              "installed":true,"clobbered":false,"rules":1}]
            """.utf8)
        let tools = try JSONDecoder().decode([ATMGuardTool].self, from: json)
        XCTAssertEqual(tools.count, 1)
        XCTAssertTrue(tools[0].installed)
        XCTAssertTrue(tools[0].isHealthy)
        XCTAssertEqual(tools[0].realPath, "/Users/x/.local/bin/a1-atm-real")

        // shadowed_by is absent when nothing bypasses the gate, and its absence must
        // not read as "shadowed by empty string".
        XCTAssertFalse(tools[0].isShadowed)
    }

    /// Captured from `atm guard rule list --json`.
    func testRuleListJSONFromTheCLIDecodes() throws {
        let json = Data(
            """
            [{"tool":"dws","id":"chat-send","label":"发送钉钉消息",
              "path":["chat","message","send"],
              "target_flags":["--group","--user"],"body_flags":["--text"],
              "enabled":true,"builtin":true,"overridden":false}]
            """.utf8)
        let rules = try JSONDecoder().decode([ATMGuardRule].self, from: json)
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0].matcherText, "chat message send")
        XCTAssertEqual(rules[0].originText, "内置")
        XCTAssertFalse(rules[0].isDeletable)
        XCTAssertEqual(rules[0].id, "dws/chat-send")
    }
}
