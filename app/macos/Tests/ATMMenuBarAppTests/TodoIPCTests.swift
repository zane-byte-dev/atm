import XCTest
@testable import ATMMenuBarApp

final class TodoIPCTests: XCTestCase {
    private func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func capturingRunner(stdout: String) throws -> (ATMCommandRunner, URL, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-todo-ipc-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        let request = directory.appendingPathComponent("request.json")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try """
        #!/bin/sh
        if [ ! -s \(shellQuoted(request.path)) ]; then
          /bin/cat > \(shellQuoted(request.path))
        else
          /bin/cat > /dev/null
        fi
        /usr/bin/printf '%s' \(shellQuoted(stdout))
        """.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return (try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path]), directory, request)
    }

    func testTodoMethodVocabularyAndRequestsHaveNoArgvEscapeHatch() throws {
        XCTAssertEqual(ATMTodoIPCCommand.list.arguments, ["_ipc", "todo.list"])
        XCTAssertEqual(ATMTodoIPCCommand.show.arguments, ["_ipc", "todo.show"])
        XCTAssertEqual(ATMTodoIPCCommand.document.arguments, ["_ipc", "todo.doc"])
        XCTAssertEqual(ATMTodoIPCCommand.create.arguments, ["_ipc", "todo.create"])
		XCTAssertEqual(ATMTodoIPCCommand.suggestTitle.arguments, ["_ipc", "todo.title"])
		XCTAssertEqual(ATMTodoIPCCommand.suggestTitle.timeout, 45)
        XCTAssertEqual(ATMTodoIPCCommand.update.arguments, ["_ipc", "todo.update"])
        XCTAssertEqual(ATMTodoIPCCommand.refine.arguments, ["_ipc", "todo.refine"])
        XCTAssertEqual(ATMTodoIPCCommand.refine.timeout, 180)

        let list = try object(JSONEncoder().encode(ATMTodoListRequest(
            status: "all", query: "typed", limit: 200, offset: 20
        )))
        XCTAssertEqual(list["status"] as? String, "all")
        XCTAssertEqual(list["query"] as? String, "typed")
        XCTAssertEqual(list["limit"] as? Int, 200)
        XCTAssertEqual(list["offset"] as? Int, 20)
        XCTAssertNil(list["argv"])
        XCTAssertNil(list["action"])

        let create = try object(JSONEncoder().encode(ATMTodoCreateRequest(draft: ATMTodoDraft(
            text: "Typed create\n\nBody", project: "atm", priority: "P0",
            imagePaths: ["/tmp/screenshot.png"]
        ))))
		XCTAssertEqual(create["title"] as? String, "Typed create Body")
		XCTAssertEqual(create["description"] as? String, "Typed create\n\nBody")
        XCTAssertEqual(create["image_paths"] as? [String], ["/tmp/screenshot.png"])
        for forbidden in ["status", "creator", "source", "on_done", "arguments", "action"] {
            XCTAssertNil(create[forbidden], "create unexpectedly exposed \(forbidden)")
        }

        let update = try object(JSONEncoder().encode(ATMTodoUpdateRequest(
            todoID: "t8", description: "", priority: "P2"
        )))
        XCTAssertEqual(update["todo_id"] as? String, "t8")
        XCTAssertEqual(update["description"] as? String, "")
        XCTAssertEqual(update["priority"] as? String, "P2")
        XCTAssertNil(update["title"])
        XCTAssertNil(update["argv"])

        let document = try object(JSONEncoder().encode(ATMTodoIDRequest(todoID: "t8")))
        XCTAssertEqual(document["todo_id"] as? String, "t8")
        XCTAssertNil(document["initialize"])

        let refine = try object(JSONEncoder().encode(ATMTodoRefineRequest(
            todoID: " t8 ", allowSplit: false, maxChildren: 2,
            hint: "  补上验收标准  ", dryRun: true
        )))
        XCTAssertEqual(refine["todo_id"] as? String, "t8")
        XCTAssertEqual(refine["allow_split"] as? Bool, false)
        XCTAssertEqual(refine["max_children"] as? Int, 2)
        XCTAssertEqual(refine["hint"] as? String, "补上验收标准")
        XCTAssertEqual(refine["dry_run"] as? Bool, true)
        for forbidden in ["argv", "action", "timeout", "model", "executor"] {
            XCTAssertNil(refine[forbidden], "refine unexpectedly exposed \(forbidden)")
        }
    }

    @MainActor
    func testDataStoreTodoReadPathsUseTypedResponses() async throws {
        let listResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-list","verb":"todo.list","data":[{"id":"t7","title":"Typed search","priority":"P1","status":"open","project":"atm","created":"2026-08-20"}]}"#
        let (listRunner, listDirectory, listRequestURL) = try capturingRunner(stdout: listResponse)
        defer { try? FileManager.default.removeItem(at: listDirectory) }
        let listStore = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: listRunner) })

        let matches = try await listStore.searchTodos("  typed  ")

        XCTAssertEqual(matches.map(\.id), ["t7"])
        let listRequest = try object(Data(contentsOf: listRequestURL))
        XCTAssertEqual(listRequest["status"] as? String, "all")
        XCTAssertEqual(listRequest["query"] as? String, "typed")

        let docResponse = ###"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-doc","verb":"todo.doc","data":{"exists":true,"content":"## 进展\n- [2026-08-20 13:00] Typed document loaded\n"}}"###
        let (docRunner, docDirectory, docRequestURL) = try capturingRunner(stdout: docResponse)
        defer { try? FileManager.default.removeItem(at: docDirectory) }
        let docStore = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: docRunner) })
        docStore.loadProgress(for: "t7")
        await waitUntil { !docStore.isLoadingProgress(for: "t7") }
        XCTAssertEqual(docStore.progress(for: "t7").first?.text, "Typed document loaded")
        XCTAssertEqual(try object(Data(contentsOf: docRequestURL))["todo_id"] as? String, "t7")

        let showResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-show","verb":"todo.show","data":{"todo":{"id":"t7","title":"Typed search","priority":"P1","status":"open","created":"2026-08-20"},"sessions":[{"session_id":"session-1","short_id":"s1","agent":"codex","project":"atm"}]}}"#
        let (showRunner, showDirectory, showRequestURL) = try capturingRunner(stdout: showResponse)
        defer { try? FileManager.default.removeItem(at: showDirectory) }
        let showStore = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: showRunner) })
        showStore.loadBoundSessions(for: "t7")
        await waitUntil { !showStore.isLoadingBoundSessions(for: "t7") }
        XCTAssertEqual(showStore.boundSessions(for: "t7").map(\.sessionID), ["session-1"])
        XCTAssertEqual(try object(Data(contentsOf: showRequestURL))["todo_id"] as? String, "t7")
    }

    @MainActor
    func testDataStoreTodoCreateAndEditUseTypedRequests() async throws {
        let createResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-create","verb":"todo.create","data":{"id":"t8","title":"Typed create","description":"Body","priority":"P0","status":"open","project":"atm","creator":"me","created":"2026-08-20"}}"#
        let (createRunner, createDirectory, createRequestURL) = try capturingRunner(stdout: createResponse)
        defer { try? FileManager.default.removeItem(at: createDirectory) }
        let createStore = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: createRunner) })
        var selectedID: String?
        createStore.addTodo(
            ATMTodoDraft(text: "Typed create\n\nBody", project: "atm", priority: "P0")
        ) { selectedID = $0 }
        await waitUntil { selectedID != nil }
        XCTAssertEqual(selectedID, "t8")
        XCTAssertEqual(createStore.allTodos.first?.id, "t8")
		let titleRequest = try object(Data(contentsOf: createRequestURL))
		XCTAssertEqual(titleRequest["description"] as? String, "Typed create\n\nBody")
		XCTAssertNil(titleRequest["status"])
		XCTAssertNil(titleRequest["creator"])

        let original = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(#"{"id":"t8","title":"Typed create","priority":"P0","status":"open","created":"2026-08-20"}"#.utf8)
        )
        let updateResponse = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-update","verb":"todo.update","data":{"id":"t8","title":"Typed edit","description":"More context","priority":"P2","status":"review","project":"atm","source":"menu bar","created":"2026-08-20"}}"#
        let (updateRunner, updateDirectory, updateRequestURL) = try capturingRunner(stdout: updateResponse)
        defer { try? FileManager.default.removeItem(at: updateDirectory) }
        let updateStore = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: updateRunner) })
        updateStore.editTodo(original, edit: ATMTodoEdit(
            title: "  Typed edit  ", description: "  More context  ", priority: "P2",
            project: " atm ", status: "review", wakeCondition: "", reviewAt: "", source: " menu bar "
        ))
        await waitUntil { !updateStore.isActing }
        let updateRequest = try object(Data(contentsOf: updateRequestURL))
        XCTAssertEqual(updateRequest["todo_id"] as? String, "t8")
        XCTAssertEqual(updateRequest["title"] as? String, "Typed edit")
        XCTAssertEqual(updateRequest["description"] as? String, "More context")
        XCTAssertEqual(updateRequest["status"] as? String, "review")
        XCTAssertNil(updateRequest["action"])
    }

    @MainActor
    func testDataStoreTodoRefineUsesTypedRequestAndTracksUnchanged() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-todo-refine","verb":"todo.refine","data":{"todo":{"id":"t8","title":"Already clear","priority":"P1","status":"open","created":"2026-08-20"},"complexity":"simple","title_changed":false,"description_changed":false,"split":false,"children":[],"dry_run":false,"changed":false}}"#
        let (runner, directory, requestURL) = try capturingRunner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeTodoIPCClient: { ATMTodoIPCClient(runner: runner) })

        store.refineTodo(id: " t8 ", hint: "  补上验收标准  ")
        await waitUntil { !store.refiningTodoIDs.contains("t8") }

        XCTAssertTrue(store.refineUnchangedTodoIDs.contains("t8"))
        XCTAssertNil(store.refineErrorByTodoID["t8"] ?? nil)
        let request = try object(Data(contentsOf: requestURL))
        XCTAssertEqual(request["todo_id"] as? String, "t8")
        XCTAssertEqual(request["allow_split"] as? Bool, true)
        XCTAssertEqual(request["max_children"] as? Int, 5)
        XCTAssertEqual(request["hint"] as? String, "补上验收标准")
        XCTAssertEqual(request["dry_run"] as? Bool, false)
        XCTAssertNil(request["argv"])
    }

    /// Lifecycle over typed IPC, against the real Go binary. These five used to be
    /// fork/exec argv; what has to hold now is that each transition actually lands
    /// in the database, that Work's human-acceptance and confirmation rules still
    /// apply through this transport, and that the Swift response types decode.
    func testRealGoTodoLifecycleOverTypedIPC() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-todo-lifecycle-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }

        let encoder = JSONEncoder()
        let create = try runGoIPC(
            executable: executable, verb: "todo.create", home: home,
            input: encoder.encode(ATMTodoCreateRequest(draft: ATMTodoDraft(
                text: "Lifecycle over IPC", project: "atm", priority: "P1"
            )))
        )
        XCTAssertEqual(create.status, 0, create.stderr)
        let created = try JSONDecoder().decode(ATMIPCEnvelope<ATMTodo>.self, from: create.stdout).data
        XCTAssertEqual(created.status, "open")

        let started = try runGoIPC(
            executable: executable, verb: "todo.start", home: home,
            input: encoder.encode(ATMTodoStartRequest(todoID: created.id, reopenReason: nil))
        )
        XCTAssertEqual(started.status, 0, started.stderr)
        XCTAssertEqual(
            try JSONDecoder().decode(ATMIPCEnvelope<ATMTodo>.self, from: started.stdout).data.status,
            "in_progress"
        )

        // `_ipc` presents a human actor, so acceptance is allowed here; an Agent
        // calling as an agent still has to use submit.
        let done = try runGoIPC(
            executable: executable, verb: "todo.done", home: home,
            input: encoder.encode(ATMTodoDoneRequest(
                todoID: created.id,
                reason: "关键路径与 IPC 生命周期回归已验证通过"
            ))
        )
        XCTAssertEqual(done.status, 0, done.stderr)
        XCTAssertEqual(
            try JSONDecoder().decode(ATMIPCEnvelope<ATMTodo>.self, from: done.stdout).data.status,
            "done"
        )

        let archived = try runGoIPC(
            executable: executable, verb: "todo.archive", home: home,
            input: encoder.encode(ATMTodoRetentionRequest(created.id))
        )
        XCTAssertEqual(archived.status, 0, archived.stderr)
        XCTAssertEqual(
            try JSONDecoder().decode(
                ATMIPCEnvelope<ATMTodoRetentionResponse>.self, from: archived.stdout
            ).data.moved,
            [created.id]
        )
        XCTAssertTrue(try todoList(executable: executable, home: home, status: "all").isEmpty)

        let restored = try runGoIPC(
            executable: executable, verb: "todo.restore", home: home,
            input: encoder.encode(ATMTodoRetentionRequest(created.id))
        )
        XCTAssertEqual(restored.status, 0, restored.stderr)
        XCTAssertEqual(
            try JSONDecoder().decode(
                ATMIPCEnvelope<ATMTodoRetentionResponse>.self, from: restored.stdout
            ).data.moved,
            [created.id]
        )
        XCTAssertEqual(
            try todoList(executable: executable, home: home, status: "all").map(\.id), [created.id]
        )

        // Deletion without confirmation is refused, and refusing it must not
        // delete anything.
        let unconfirmed = try runGoIPC(
            executable: executable, verb: "todo.delete", home: home,
            input: Data("{\"todo_id\":\"\(created.id)\",\"confirmed\":false}".utf8)
        )
        // Read the envelope's error half as raw JSON: the typed envelope only
        // surfaces `data`, and a refusal deliberately carries none.
        let refusal = try object(unconfirmed.stdout)
        XCTAssertEqual(
            (refusal["error"] as? [String: Any])?["code"] as? String,
            "forbidden",
            unconfirmed.stderr
        )
        XCTAssertEqual(
            try todoList(executable: executable, home: home, status: "all").map(\.id), [created.id]
        )

        let deleted = try runGoIPC(
            executable: executable, verb: "todo.delete", home: home,
            input: encoder.encode(ATMTodoDeleteRequest(created.id))
        )
        XCTAssertEqual(deleted.status, 0, deleted.stderr)
        XCTAssertEqual(
            try JSONDecoder().decode(
                ATMIPCEnvelope<ATMTodoDeleteResponse>.self, from: deleted.stdout
            ).data.deleted,
            [created.id]
        )
        XCTAssertTrue(try todoList(executable: executable, home: home, status: "all").isEmpty)
    }

    func testRealGoTodoJSONDecodesInSwiftIncludingWorkingAndTrashLists() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let fixtureRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-todo-contract-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: fixtureRoot, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: fixtureRoot) }

        let create = try runGoIPC(
            executable: executable,
            verb: "todo.create",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoCreateRequest(draft: ATMTodoDraft(
                text: "Cross-language Todo\n\nDecoded from real Go", project: "atm", priority: "P0"
            )))
        )
        XCTAssertEqual(create.status, 0, create.stderr)
        let created = try JSONDecoder().decode(ATMIPCEnvelope<ATMTodo>.self, from: create.stdout).data
        XCTAssertEqual(created.id, "t1")
        XCTAssertEqual(created.creator, "me")
        XCTAssertEqual(created.status, "open")

        let working = try todoList(executable: executable, home: fixtureRoot, status: "all")
        XCTAssertEqual(working.map(\.id), [created.id])

        let show = try runGoIPC(
            executable: executable, verb: "todo.show", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoIDRequest(todoID: created.id))
        )
        XCTAssertEqual(show.status, 0, show.stderr)
        let detail = try JSONDecoder().decode(ATMIPCEnvelope<ATMTodoDetail>.self, from: show.stdout).data
        XCTAssertEqual(detail.todo?.id, created.id)

        let document = try runGoIPC(
            executable: executable, verb: "todo.doc", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoIDRequest(todoID: created.id))
        )
        XCTAssertEqual(document.status, 0, document.stderr)
        let doc = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMTodoDocumentResponse>.self, from: document.stdout
        ).data
        XCTAssertTrue(doc.exists)
        XCTAssertTrue(doc.content?.contains("Decoded from real Go") == true)

        let update = try runGoIPC(
            executable: executable, verb: "todo.update", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoUpdateRequest(
                todoID: created.id, title: "Cross-language updated", source: "macOS App"
            ))
        )
        XCTAssertEqual(update.status, 0, update.stderr)
        let updated = try JSONDecoder().decode(ATMIPCEnvelope<ATMTodo>.self, from: update.stdout).data
        XCTAssertEqual(updated.title, "Cross-language updated")
        XCTAssertEqual(updated.source, "macOS App")

        let modelServer = try startRefineModelServer(proposal: [
            "title": "Cross-language refined",
            "description": "Goal: decode the typed refinement and project its document.",
            "complexity": "simple",
            "plan": "",
            "reason": "One independently verifiable deliverable",
            "children": [],
        ])
        defer { stopRefineModelServer(modelServer) }
        let modelEnvironment = [
            "ATM_TEXT_MODEL_API_KEY": "test-key",
            "ATM_TEXT_MODEL_BASE_URL": modelServer.baseURL,
            "ATM_TEXT_MODEL_MODEL": "contract-test-model",
        ]

        let dryRun = try runGoIPC(
            executable: executable, verb: "todo.refine", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoRefineRequest(
                todoID: created.id, allowSplit: false, maxChildren: 2,
                hint: "Preserve the typed wire shape", dryRun: true
            )),
            environmentOverrides: modelEnvironment
        )
        XCTAssertEqual(dryRun.status, 0, dryRun.stderr)
        let proposed = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMTodoRefineResponse>.self, from: dryRun.stdout
        ).data
        XCTAssertTrue(proposed.dryRun)
        XCTAssertTrue(proposed.changed)
        XCTAssertTrue(proposed.children.isEmpty)
        XCTAssertTrue(proposed.proposal?.children.isEmpty == true)
        XCTAssertEqual(proposed.proposedTitle, "Cross-language refined")

        let applyRefine = try runGoIPC(
            executable: executable, verb: "todo.refine", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoRefineRequest(
                todoID: created.id, allowSplit: false, maxChildren: 2,
                hint: "Preserve the typed wire shape", dryRun: false
            )),
            environmentOverrides: modelEnvironment
        )
        XCTAssertEqual(applyRefine.status, 0, applyRefine.stderr)
        let refined = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMTodoRefineResponse>.self, from: applyRefine.stdout
        ).data
        XCTAssertFalse(refined.dryRun)
        XCTAssertTrue(refined.changed)
        XCTAssertTrue(refined.children.isEmpty)
        XCTAssertEqual(refined.todo.title, "Cross-language refined")

        let refinedDocument = try runGoIPC(
            executable: executable, verb: "todo.doc", home: fixtureRoot,
            input: JSONEncoder().encode(ATMTodoIDRequest(todoID: created.id))
        )
        XCTAssertEqual(refinedDocument.status, 0, refinedDocument.stderr)
        let projected = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMTodoDocumentResponse>.self, from: refinedDocument.stdout
        ).data
        XCTAssertTrue(projected.content?.contains("Cross-language refined") == true)
        XCTAssertTrue(projected.content?.contains("模型整理") == true)

        let trash = try runGoCommand(
            executable: executable, arguments: ["todo", "trash", created.id],
            home: fixtureRoot, input: Data()
        )
        XCTAssertEqual(trash.status, 0, trash.stderr)
        XCTAssertTrue(try todoList(executable: executable, home: fixtureRoot, status: "all").isEmpty)
        let trashed = try todoList(executable: executable, home: fixtureRoot, status: "trashed")
        XCTAssertEqual(trashed.map(\.id), [created.id])
        XCTAssertEqual(trashed.first?.title, "Cross-language refined")
    }

    private func todoList(executable: String, home: URL, status: String) throws -> [ATMTodo] {
        let result = try runGoIPC(
            executable: executable, verb: "todo.list", home: home,
            input: JSONEncoder().encode(ATMTodoListRequest(status: status))
        )
        XCTAssertEqual(result.status, 0, result.stderr)
        return try JSONDecoder().decode(ATMIPCEnvelope<[ATMTodo]>.self, from: result.stdout).data
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
        input: Data,
        environmentOverrides: [String: String] = [:]
    ) throws -> CLIResult {
        try runGoCommand(
            executable: executable,
            arguments: ["_ipc", verb],
            home: home,
            input: input,
            environmentOverrides: environmentOverrides
        )
    }

    private func runGoCommand(
        executable: String,
        arguments: [String],
        home: URL,
        input: Data,
        environmentOverrides: [String: String] = [:]
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
        for (key, value) in environmentOverrides {
            environment[key] = value
        }
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

    private struct RefineModelServer {
        let process: Process
        let directory: URL
        let baseURL: String
    }

    private func startRefineModelServer(proposal: [String: Any]) throws -> RefineModelServer {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-refine-model-\(UUID().uuidString)", isDirectory: true)
        let scriptURL = directory.appendingPathComponent("server.py")
        let responseURL = directory.appendingPathComponent("response.json")
        let portURL = directory.appendingPathComponent("port.txt")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)

        let proposalData = try JSONSerialization.data(withJSONObject: proposal, options: [.sortedKeys])
        let content = try XCTUnwrap(String(data: proposalData, encoding: .utf8))
        let responseData = try JSONSerialization.data(withJSONObject: [
            "choices": [[
                "message": ["content": content],
                "finish_reason": "stop",
            ]],
        ])
        try responseData.write(to: responseURL)
        try """
        import http.server
        import pathlib
        import sys

        response = pathlib.Path(sys.argv[1]).read_bytes()

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                self.rfile.read(length)
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(response)))
                self.end_headers()
                self.wfile.write(response)

            def log_message(self, format, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        pathlib.Path(sys.argv[2]).write_text(str(server.server_port))
        server.serve_forever()
        """.write(to: scriptURL, atomically: true, encoding: .utf8)

        let process = Process()
        let stderr = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/python3")
        process.arguments = [scriptURL.path, responseURL.path, portURL.path]
        process.standardOutput = Pipe()
        process.standardError = stderr
        try process.run()

        for _ in 0..<300 {
            if let rawPort = try? String(contentsOf: portURL, encoding: .utf8),
               let port = Int(rawPort.trimmingCharacters(in: .whitespacesAndNewlines)) {
                return RefineModelServer(
                    process: process, directory: directory, baseURL: "http://127.0.0.1:\(port)"
                )
            }
            if !process.isRunning { break }
            Thread.sleep(forTimeInterval: 0.01)
        }

        if process.isRunning {
            process.terminate()
        }
        process.waitUntilExit()
        let message = String(
            data: stderr.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8
        ) ?? ""
        try? FileManager.default.removeItem(at: directory)
        throw NSError(
            domain: "TodoIPCTests", code: 1,
            userInfo: [NSLocalizedDescriptionKey: "refine model server failed to start: \(message)"]
        )
    }

    private func stopRefineModelServer(_ server: RefineModelServer) {
        if server.process.isRunning {
            server.process.terminate()
            server.process.waitUntilExit()
        }
        try? FileManager.default.removeItem(at: server.directory)
    }
}
