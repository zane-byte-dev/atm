import XCTest
@testable import ATMMenuBarApp

final class SessionTranscriptModelTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    /// The index row carries no live state, so the list has to name a session from
    /// what the transcript itself recorded, and never render a blank row.
    func testIndexedSessionTitleFallsBackFromSummaryToQuestionToShortID() throws {
        let withSummary = try decode(ATMIndexedSession.self, """
        {"id":"s1","short_id":"abc12345","agent":"grokbuild","project":"atm",
         "created_at":"2026-08-10T22:42:33+08:00","q_count":3,
         "summary":"胶囊样式改造","first_q":"详情页的 tab 要不要改"}
        """)
        XCTAssertEqual(withSummary.title, "胶囊样式改造")
        XCTAssertEqual(withSummary.qCount, 3)
        XCTAssertNotNil(withSummary.startedAt)

        let withoutSummary = try decode(ATMIndexedSession.self, """
        {"id":"s2","short_id":"def67890","agent":"codex","project":"atm",
         "created_at":"2026-08-10T22:42:33+08:00","q_count":1,"first_q":"帮我看下这个 bug"}
        """)
        XCTAssertEqual(withoutSummary.title, "帮我看下这个 bug")

        let bare = try decode(ATMIndexedSession.self, """
        {"id":"s3","short_id":"ghi13579","agent":"pi","project":"","created_at":"","q_count":0,"summary":"  "}
        """)
        XCTAssertEqual(bare.title, "ghi13579")
        XCTAssertNil(bare.startedAt)
    }

    /// A missing transcript file and an agent that stores no thinking text are
    /// different facts with different remedies, so they decode into separate flags
    /// instead of collapsing into one empty pane.
    func testTranscriptSeparatesMissingSourceFromAbsentThinking() throws {
        let absent = try decode(ATMSessionTranscript.self, """
        {"id":"s1","agent":"Claude Code","project":"atm","total_turns":2,"returned_turns":2,
         "truncated":false,"thinking_absent":true,
         "qa":[{"turn":1,"q":"改一下","a":"改完了"}]}
        """)
        XCTAssertTrue(absent.thinkingAbsent)
        XCTAssertFalse(absent.thinkingSourceMissing)
        XCTAssertEqual(absent.turns.first?.answer, "改完了")
        XCTAssertNil(absent.turns.first?.thinking)

        let missing = try decode(ATMSessionTranscript.self, """
        {"id":"s2","agent":"Codex","project":"atm","total_turns":1,"returned_turns":1,
         "truncated":false,"thinking_source_missing":true,"qa":[]}
        """)
        XCTAssertTrue(missing.thinkingSourceMissing)
        XCTAssertFalse(missing.thinkingAbsent)
        XCTAssertTrue(missing.turns.isEmpty)

        // A payload with neither flag must not imply either problem.
        let plain = try decode(ATMSessionTranscript.self, """
        {"id":"s3","agent":"Pi","project":"atm","qa":[{"turn":1,"q":"想什么","a":"想好了","thinking":"先看代码"}]}
        """)
        XCTAssertFalse(plain.thinkingAbsent)
        XCTAssertFalse(plain.thinkingSourceMissing)
        XCTAssertEqual(plain.turns.first?.thinking, "先看代码")
    }

    /// The timeline is the only view where spend is attributable, so request rows
    /// must keep their usage while message rows stay usage-free.
    func testTimelineDistinguishesMessagesFromModelRequests() throws {
        let entries = try decode([ATMSessionTimelineEntry].self, """
        [{"kind":"message","role":"user","content":"帮我改一下","ts":1786351513},
         {"kind":"request","model":"grok-4.5-build","ts":1786351522,
          "input_tokens":5915,"output_tokens":163,"cache_tokens":8960,"cost_usd":0.02691}]
        """)
        XCTAssertEqual(entries.count, 2)
        XCTAssertTrue(entries[0].isMessage)
        XCTAssertNil(entries[0].costUSD)
        XCTAssertFalse(entries[1].isMessage)
        XCTAssertEqual(entries[1].model, "grok-4.5-build")
        XCTAssertEqual(entries[1].outputTokens, 163)
        XCTAssertEqual(entries[1].date.timeIntervalSince1970, 1_786_351_522, accuracy: 1)
    }

    /// Each depth is its own typed read. The brief read must be a tail rather than
    /// a prefix, and only the full read may pay for parsing the raw transcript.
    func testReadModesRequestTheDepthTheyPromise() {
        let brief = ATMSessionReadMode.brief.showRequest(sessionID: "s1")
        XCTAssertEqual(brief?.sessionID, "s1")
        XCTAssertEqual(brief?.last, ATMSessionReadMode.briefTurnCount)
        XCTAssertEqual(brief?.maxChars, ATMSessionReadMode.briefMaxChars)
        XCTAssertFalse(brief?.includeThinking ?? true)

        XCTAssertNil(ATMSessionReadMode.timeline.showRequest(sessionID: "s1"))

        let full = ATMSessionReadMode.full.showRequest(sessionID: "s1")
        XCTAssertTrue(full?.includeThinking ?? false)
        XCTAssertEqual(full?.last, 0)
    }

    /// The durable copy of the run outcome. Without it a Todo whose session has
    /// aged out of live status loses the Agent's closing message entirely.
    func testBoundSessionDecodesLatestResult() throws {
        let session = try decode(ATMBoundSession.self, """
        {"session_id":"s1","short_id":"abc12345","agent":"grokbuild","project":"atm",
         "summary":"胶囊改造","latest_result":"## 完成\\n已改好并验证","indexed":true,
         "binding_count":1,"first_bound_at":100,"bound_at":100,"queries":2,"tool_calls":3,
         "input_tokens":10,"output_tokens":5,"cost_usd":0.01}
        """)
        XCTAssertEqual(session.latestResult, "## 完成\n已改好并验证")

        let unindexed = try decode(ATMBoundSession.self, """
        {"session_id":"s2","short_id":"def67890","agent":"pi","project":"atm","indexed":false,
         "binding_count":1,"first_bound_at":1,"bound_at":1,"queries":0,"tool_calls":0,
         "input_tokens":0,"output_tokens":0,"cost_usd":0}
        """)
        XCTAssertNil(unindexed.latestResult)
    }
}
