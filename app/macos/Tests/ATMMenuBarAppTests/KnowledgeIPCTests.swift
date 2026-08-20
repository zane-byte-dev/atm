import XCTest
@testable import ATMMenuBarApp

final class KnowledgeIPCTests: XCTestCase {
    private func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func capturingRunner(stdout: String) throws -> (ATMCommandRunner, URL, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-knowledge-ipc-\(UUID().uuidString)", isDirectory: true)
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

    func testKnowledgeMethodVocabularyHasNoArgvEscapeHatch() {
        XCTAssertEqual(ATMKnowledgeIPCCommand.catalog.arguments, ["_ipc", "knowledge.catalog"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.query.arguments, ["_ipc", "knowledge.query"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.document.arguments, ["_ipc", "knowledge.document.get"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.governance.arguments, ["_ipc", "knowledge.governance"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.saveDocument.arguments, ["_ipc", "knowledge.document.save"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.deleteDocument.arguments, ["_ipc", "knowledge.document.delete"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.importDocument.arguments, ["_ipc", "knowledge.document.import"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.saveCollection.arguments, ["_ipc", "knowledge.collection.save"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.deleteCollection.arguments, ["_ipc", "knowledge.collection.delete"])
        XCTAssertEqual(ATMKnowledgeIPCCommand.feedback.arguments, ["_ipc", "knowledge.feedback"])
    }

    func testDocumentSaveEncodesExactlyOneTypedVariant() throws {
        let draft = ATMKnowledgeDraft(
            title: "Typed save",
            collection: "notes",
            domains: [],
            tags: ["ipc"],
            projects: ["atm"],
            content: "Body"
        )
        let create = try XCTUnwrap(
            JSONSerialization.jsonObject(
                with: JSONEncoder().encode(ATMKnowledgeDocumentSaveRequest.create(draft))
            ) as? [String: Any]
        )
        XCTAssertEqual(Set(create.keys), ["create"])
        let payload = try XCTUnwrap(create["create"] as? [String: Any])
        XCTAssertEqual(payload["title"] as? String, "Typed save")
        XCTAssertEqual(payload["content"] as? String, "Body")
        XCTAssertEqual(payload["domains"] as? [String], [])
        XCTAssertNil(create["content"])
        XCTAssertNil(create["metadata"])

        let metadata = try XCTUnwrap(
            JSONSerialization.jsonObject(
                with: JSONEncoder().encode(ATMKnowledgeDocumentSaveRequest.metadata(
                    documentID: "document:1",
                    collection: "research",
                    tags: []
                ))
            ) as? [String: Any]
        )
        XCTAssertEqual(Set(metadata.keys), ["metadata"])
        let metadataPayload = try XCTUnwrap(metadata["metadata"] as? [String: Any])
        XCTAssertEqual(metadataPayload["document_id"] as? String, "document:1")
        XCTAssertEqual(metadataPayload["collection"] as? String, "research")
        XCTAssertEqual(metadataPayload["tags"] as? [String], [])
    }

    @MainActor
    func testDataStoreKnowledgeBrowseUsesTypedQueryAndKeepsDedupingRows() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-knowledge-query","verb":"knowledge.query","data":{"documents":[{"document_id":"document:1","title":"One","collection":"notes","status":"active"},{"document_id":"document:1","title":"Duplicate","collection":"notes","status":"active"}]}}"#
        let (runner, directory, requestURL) = try capturingRunner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeKnowledgeIPCClient: {
            ATMKnowledgeIPCClient(runner: runner)
        })

        let documents = try await store.knowledgeDocuments(collectionID: "notes", status: "active")

        XCTAssertEqual(documents.map(\.documentID), ["document:1"])
        let request = try XCTUnwrap(
            JSONSerialization.jsonObject(with: Data(contentsOf: requestURL)) as? [String: Any]
        )
        XCTAssertEqual(request["collection"] as? String, "notes")
        XCTAssertEqual(request["status"] as? String, "active")
        XCTAssertNil(request["text"])
        XCTAssertNil(request["session_id"])
    }

    func testRealGoKnowledgeJSONDecodesInSwift() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let fixtureRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-knowledge-contract-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: fixtureRoot, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: fixtureRoot) }

        let draft = ATMKnowledgeDraft(
            title: "跨语言知识",
            collection: "notes",
            domains: ["architecture"],
            tags: ["ipc"],
            projects: ["atm"],
            content: "Real Go JSON decoded by Swift."
        )
        let saved = try runGoIPC(
            executable: executable,
            verb: "knowledge.document.save",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMKnowledgeDocumentSaveRequest.create(draft))
        )
        XCTAssertEqual(saved.status, 0, saved.stderr)
        let document = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMKnowledgeDocument>.self,
            from: saved.stdout
        ).data
        XCTAssertEqual(document.metadata.title, "跨语言知识")
        XCTAssertEqual(document.metadata.tags, ["ipc"])
        XCTAssertEqual(document.collection, "notes")

        let feedback = try runGoIPC(
            executable: executable,
            verb: "knowledge.feedback",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMKnowledgeFeedbackRequest(
                documentID: document.metadata.id,
                sessionID: "swift-contract-session",
                outcome: "adopted",
                note: "decoded"
            ))
        )
        XCTAssertEqual(feedback.status, 0, feedback.stderr)
        let receipt = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMKnowledgeFeedbackReceipt>.self,
            from: feedback.stdout
        ).data
        XCTAssertEqual(receipt.documentID, document.metadata.id)
        XCTAssertEqual(receipt.sessionID, "swift-contract-session")

        let query = try runGoIPC(
            executable: executable,
            verb: "knowledge.query",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMKnowledgeQueryRequest(
                text: "Real Go JSON",
                collection: nil,
                status: "active",
                sessionID: nil,
                limit: 20
            ))
        )
        XCTAssertEqual(query.status, 0, query.stderr)
        let result = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMKnowledgeQueryResponse>.self,
            from: query.stdout
        ).data
        XCTAssertEqual(result.documents.map(\.documentID), [document.metadata.id])
        XCTAssertNotNil(result.documents.first?.score)
    }

    func testRealGoMemoryJSONDecodesInSwift() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        let fixtureRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-memory-contract-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: fixtureRoot, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: fixtureRoot) }

        let remembered = try runGoCommand(
            executable: executable,
            arguments: [
                "memory", "remember", "original memory bridge", "--scope", "global",
                "--tag", "ipc", "--source", "session:seed", "--json",
            ],
            home: fixtureRoot,
            input: Data()
        )
        XCTAssertEqual(remembered.status, 0, remembered.stderr)
        let target = try JSONDecoder().decode(ATMMemoryMutationReceipt.self, from: remembered.stdout)

        let recalled = try runGoIPC(
            executable: executable,
            verb: "memory.recall",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMMemoryRecallRequest(
                query: "memory bridge",
                scope: nil,
                limit: 20
            ))
        )
        XCTAssertEqual(recalled.status, 0, recalled.stderr)
        let recallResult = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMMemoryRecallResponse>.self,
            from: recalled.stdout
        ).data
        XCTAssertEqual(recallResult.hits.map(\.id), [target.id])

        let superseded = try runGoIPC(
            executable: executable,
            verb: "memory.supersede",
            home: fixtureRoot,
            input: JSONEncoder().encode(ATMMemorySupersedeRequest(
                targetID: target.id,
                scope: "global",
                content: "replacement memory bridge",
                tags: ["ipc", "typed"],
                source: "session:replacement"
            ))
        )
        XCTAssertEqual(superseded.status, 0, superseded.stderr)
        let supersedeResult = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMMemorySupersedeResponse>.self,
            from: superseded.stdout
        ).data
        XCTAssertFalse(supersedeResult.event.id.isEmpty)
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
