import XCTest
@testable import ATMMenuBarApp

final class MemoryIPCTests: XCTestCase {
    private func shellQuoted(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'"
    }

    private func capturingRunner(stdout: String) throws -> (ATMCommandRunner, URL, URL) {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-memory-ipc-\(UUID().uuidString)", isDirectory: true)
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

    func testMemoryMethodVocabularyAndRequestsHaveNoArgvEscapeHatch() throws {
        XCTAssertEqual(ATMMemoryIPCCommand.recall.arguments, ["_ipc", "memory.recall"])
        XCTAssertEqual(ATMMemoryIPCCommand.supersede.arguments, ["_ipc", "memory.supersede"])

        let recall = try object(JSONEncoder().encode(ATMMemoryRecallRequest(
            query: "alias",
            scope: nil,
            limit: 200
        )))
        XCTAssertEqual(Set(recall.keys), ["query", "limit"])
        XCTAssertEqual(recall["query"] as? String, "alias")
        XCTAssertNil(recall["arguments"])

        let supersede = try object(JSONEncoder().encode(ATMMemorySupersedeRequest(
            targetID: "memory:1",
            scope: "global",
            content: "new fact",
            tags: ["service"],
            source: "session:s1"
        )))
        XCTAssertEqual(Set(supersede.keys), ["target_id", "scope", "content", "tags", "source"])
        XCTAssertEqual(supersede["target_id"] as? String, "memory:1")
        XCTAssertNil(supersede["file"])
        XCTAssertNil(supersede["argv"])
    }

    @MainActor
    func testDataStoreMemoryRecallUsesTypedRequestAndDecodesHits() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-memory-recall","verb":"memory.recall","data":{"hits":[{"id":"memory:1","scope":"global","content":"hub alias","tags":["alias"],"created_at":"2026-08-20T05:00:00Z","score":0.7,"source":"memory","metadata":{"source":"session:s1"}}]}}"#
        let (runner, directory, requestURL) = try capturingRunner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeMemoryIPCClient: {
            ATMMemoryIPCClient(runner: runner)
        })

        let hits = try await store.memories(query: "  alias  ")

        XCTAssertEqual(hits.map(\.id), ["memory:1"])
        XCTAssertEqual(hits.first?.content, "hub alias")
        let request = try object(Data(contentsOf: requestURL))
        XCTAssertEqual(request["query"] as? String, "alias")
        XCTAssertEqual(request["limit"] as? Int, 200)
        XCTAssertNil(request["scope"])
    }

    @MainActor
    func testDataStoreMemorySupersedeSendsCompleteReplacementIntent() async throws {
        let response = #"{"envelope_version":1,"protocol_version":1,"request_id":"ipc-memory-supersede","verb":"memory.supersede","data":{"event":{"id":"memory:2"}}}"#
        let (runner, directory, requestURL) = try capturingRunner(stdout: response)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = ATMDataStore(makeMemoryIPCClient: {
            ATMMemoryIPCClient(runner: runner)
        })
        let memory = try JSONDecoder().decode(
            ATMMemoryHit.self,
            from: Data(#"{"id":"memory:1","scope":"global","content":"old","tags":["alias","service"],"created_at":"2026-08-20T04:00:00Z","score":1,"source":"memory","metadata":{"source":"session:s1"}}"#.utf8)
        )

        try await store.supersedeMemory(memory, content: "new fact")

        let request = try object(Data(contentsOf: requestURL))
        XCTAssertEqual(request["target_id"] as? String, "memory:1")
        XCTAssertEqual(request["scope"] as? String, "global")
        XCTAssertEqual(request["content"] as? String, "new fact")
        XCTAssertEqual(request["tags"] as? [String], ["alias", "service"])
        XCTAssertEqual(request["source"] as? String, "session:s1")
    }

    private func object(_ data: Data) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }
}
