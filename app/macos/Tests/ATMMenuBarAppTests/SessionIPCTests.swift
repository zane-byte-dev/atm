import XCTest
@testable import ATMMenuBarApp

final class SessionIPCTests: XCTestCase {
    private func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func capturingRunner(stdout: String) throws -> (ATMCommandRunner, URL, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-session-ipc-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        let request = directory.appendingPathComponent("request.json")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try """
        #!/bin/sh
        /bin/cat > \(shellQuoted(request.path))
        /usr/bin/printf '%s' \(shellQuoted(stdout))
        """.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return (try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path]), directory, request)
    }

    func testSessionMethodVocabularyAndRequestsHaveNoArgvEscapeHatch() throws {
        XCTAssertEqual(ATMSessionIPCCommand.list.arguments, ["_ipc", "session.list"])
        XCTAssertEqual(ATMSessionIPCCommand.search.arguments, ["_ipc", "session.search"])
        XCTAssertEqual(ATMSessionIPCCommand.show.arguments, ["_ipc", "session.show"])
        XCTAssertEqual(ATMSessionIPCCommand.timeline.arguments, ["_ipc", "session.timeline"])

        let list = try object(JSONEncoder().encode(ATMSessionListRequest(
            agent: "codex",
            project: "atm",
            includeAll: true,
            order: "desc",
            limit: 200,
            offset: 20
        )))
        XCTAssertEqual(list["agent"] as? String, "codex")
        XCTAssertEqual(list["all"] as? Bool, true)
        XCTAssertEqual(list["offset"] as? Int, 20)
        XCTAssertNil(list["arguments"])
        XCTAssertNil(list["action"])

        let show = try object(JSONEncoder().encode(ATMSessionShowRequest(
            sessionID: "s1",
            includeThinking: true,
            last: 4,
            maxChars: 6000
        )))
        XCTAssertEqual(show["session_id"] as? String, "s1")
        XCTAssertEqual(show["include_thinking"] as? Bool, true)
        XCTAssertEqual(show["max_chars"] as? Int, 6000)
        XCTAssertNil(show["argv"])
    }

    @MainActor
    func testDataStoreSessionSearchUsesTypedRequestAndKeepsDedupingSessions() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-session-search","verb":"session.search","data":{"keyword":"deploy","total":2,"returned":2,"truncated":false,"limit":200,"matches":[{"id":"full-1","short_id":"s1","agent":"codex","project":"atm","created_at":"2026-08-20T05:00:00Z","role":"user","content":"deploy one"},{"id":"full-1","short_id":"s1","agent":"codex","project":"atm","created_at":"2026-08-20T05:00:00Z","role":"assistant","content":"deploy two"}],"meta":{}}}"#
        let (runner, directory, requestURL) = try capturingRunner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeSessionIPCClient: {
            ATMSessionIPCClient(runner: runner)
        })

        let hits = try await store.searchSessions("  deploy  ")

        XCTAssertEqual(hits.map(\.shortID), ["s1"])
        let request = try object(Data(contentsOf: requestURL))
        XCTAssertEqual(request["keyword"] as? String, "deploy")
        XCTAssertEqual(request["limit"] as? Int, 200)
        XCTAssertEqual(request["snippet"] as? Int, 400)
    }

    @MainActor
    func testDataStoreSessionListShowAndTimelineUseTypedResponses() async throws {
        let listResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-session-list","verb":"session.list","data":{"sessions":[{"id":"full-1","short_id":"s1","agent":"codex","project":"atm","created_at":"2026-08-20T05:00:00Z","q_count":1,"summary":"Typed session"}],"total":1,"days":1,"all":true,"offset":0,"limit":200,"meta":{}}}"#
        let (listRunner, listDirectory, listRequestURL) = try capturingRunner(stdout: listResponse)
        defer { try? FileManager.default.removeItem(at: listDirectory) }
        let listStore = ATMDataStore(makeSessionIPCClient: {
            ATMSessionIPCClient(runner: listRunner)
        })
        listStore.loadIndexedSessions(reset: true, agent: "codex", project: "atm")
        await waitUntil { !listStore.isLoadingIndexedSessions }
        XCTAssertEqual(listStore.indexedSessions.map(\.id), ["full-1"])
        let listRequest = try object(Data(contentsOf: listRequestURL))
        XCTAssertEqual(listRequest["all"] as? Bool, true)
        XCTAssertEqual(listRequest["order"] as? String, "desc")
        XCTAssertEqual(listRequest["limit"] as? Int, ATMDataStore.indexedSessionPageSize)

        let showResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-session-show","verb":"session.show","data":{"id":"full-1","agent":"codex","project":"atm","qa":[{"turn":1,"q":"Question","a":"Answer"}],"tools":{"exec_command":1},"total_turns":1,"returned_turns":1,"truncated":false,"meta":{}}}"#
        let (showRunner, showDirectory, showRequestURL) = try capturingRunner(stdout: showResponse)
        defer { try? FileManager.default.removeItem(at: showDirectory) }
        let showStore = ATMDataStore(makeSessionIPCClient: {
            ATMSessionIPCClient(runner: showRunner)
        })
        let text = try await showStore.sessionTranscript("s1")
        XCTAssertTrue(text.contains("Q: Question"))
        XCTAssertTrue(text.contains("A: Answer"))
        let showRequest = try object(Data(contentsOf: showRequestURL))
        XCTAssertEqual(showRequest["session_id"] as? String, "s1")
        XCTAssertEqual(showRequest["include_thinking"] as? Bool, false)

        let timelineResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-session-timeline","verb":"session.timeline","data":{"events":[{"kind":"message","role":"user","content":"Question","ts":1787202000}],"meta":{}}}"#
        let (timelineRunner, timelineDirectory, timelineRequestURL) = try capturingRunner(stdout: timelineResponse)
        defer { try? FileManager.default.removeItem(at: timelineDirectory) }
        let timelineStore = ATMDataStore(makeSessionIPCClient: {
            ATMSessionIPCClient(runner: timelineRunner)
        })
        timelineStore.loadSessionRead("s1", mode: .timeline)
        await waitUntil { !timelineStore.isLoadingSessionRead("s1", mode: .timeline) }
        XCTAssertEqual(timelineStore.sessionTimeline("s1")?.first?.content, "Question")
        let timelineRequest = try object(Data(contentsOf: timelineRequestURL))
        XCTAssertEqual(timelineRequest["session_id"] as? String, "s1")
    }

    func testRealGoSessionJSONDecodesInSwift() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let fixtureRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-session-contract-\(UUID().uuidString)", isDirectory: true)
        let project = fixtureRoot.appendingPathComponent(".claude/projects/-tmp-atm", isDirectory: true)
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: fixtureRoot) }

        let sessionID = "0123456789abcdef"
        let transcript = project.appendingPathComponent("\(sessionID).jsonl")
        try [
            #"{"type":"ai-title","timestamp":"2026-08-20T05:00:00Z","aiTitle":"Typed IPC session"}"#,
            #"{"type":"user","timestamp":"2026-08-20T05:00:01Z","message":{"content":[{"type":"text","text":"Session contract question"}]}}"#,
            #"{"type":"assistant","timestamp":"2026-08-20T05:00:02Z","message":{"id":"msg_contract","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"Session contract answer"}]}}"#,
        ].joined(separator: "\n").appending("\n").write(to: transcript, atomically: true, encoding: .utf8)

        let sync = try runGoCommand(
            executable: executable,
            arguments: ["sync"],
            home: fixtureRoot,
            input: Data()
        )
        XCTAssertEqual(sync.status, 0, sync.stderr)

        let list = try runGoIPC(
            executable: executable,
            verb: "session.list",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMSessionListRequest(includeAll: true, order: "desc", limit: 10))
        )
        XCTAssertEqual(list.status, 0, list.stderr)
        let listData = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMSessionListResponse>.self,
            from: list.stdout
        ).data
        XCTAssertEqual(listData.sessions.map(\.id), [sessionID])

        let search = try runGoIPC(
            executable: executable,
            verb: "session.search",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMSessionSearchRequest(keyword: "contract", limit: 10))
        )
        XCTAssertEqual(search.status, 0, search.stderr)
        let searchData = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMSessionSearchResult>.self,
            from: search.stdout
        ).data
        XCTAssertEqual(searchData.matches.first?.shortID, "01234567")

        let show = try runGoIPC(
            executable: executable,
            verb: "session.show",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMSessionShowRequest(sessionID: sessionID))
        )
        XCTAssertEqual(show.status, 0, show.stderr)
        let showData = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMSessionTranscript>.self,
            from: show.stdout
        ).data
        XCTAssertEqual(showData.turns.first?.answer, "Session contract answer")

        let timeline = try runGoIPC(
            executable: executable,
            verb: "session.timeline",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMSessionTimelineRequest(sessionID: sessionID))
        )
        XCTAssertEqual(timeline.status, 0, timeline.stderr)
        let timelineData = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMSessionTimelineResponse>.self,
            from: timeline.stdout
        ).data
        XCTAssertTrue(timelineData.events.contains { $0.kind == "request" })
    }

    @MainActor
    private func waitUntil(_ condition: @escaping @MainActor () -> Bool) async {
        for _ in 0..<200 {
            if condition() { return }
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTFail("timed out waiting for async DataStore update")
    }

    private func object(_ data: Data) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
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
        try runGoCommand(
            executable: executable,
            arguments: ["_ipc", verb],
            home: home,
            input: input
        )
    }

    private func runGoCommand(
        executable: String,
        arguments: [String],
        home: URL,
        input: Data
    ) throws -> CLIResult {
        let process = Process()
        let stdout = Pipe()
        let stderr = Pipe()
        let stdin = Pipe()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
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
                data: stderr.fileHandleForReading.readDataToEndOfFile(),
                encoding: .utf8
            ) ?? ""
        )
    }
}
