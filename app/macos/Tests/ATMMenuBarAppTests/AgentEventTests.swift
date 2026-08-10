import XCTest
@testable import ATMMenuBarApp

final class AgentEventTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func session(
        tool: String = "Claude Code",
        sessionID: String,
        resumeID: String? = nil,
        cwd: String? = nil,
        lastAnswer: String? = nil,
        ageSeconds: Int = 30
    ) -> ATMLiveSession {
        ATMLiveSession(
            tool: tool,
            sessionID: sessionID,
            resumeID: resumeID,
            project: "atm",
            cwd: cwd,
            ageSeconds: ageSeconds,
            lastAnswer: lastAnswer
        )
    }

    private func signal(
        reason: String = "permission_prompt",
        tool: String? = "Bash",
        source: String = "claude",
        receivedAt: Date? = nil
    ) -> ATMAgentAttentionSignal {
        ATMAgentAttentionSignal(
            reason: reason,
            tool: tool,
            text: nil,
            source: source,
            receivedAt: receivedAt ?? now
        )
    }

    // MARK: - Envelope decoding

    func testDecodesTheEnvelopeTheCLIWrites() {
        let line = Data("""
        {"v":1,"source":"claude","event":"attention","session_id":"abc","cwd":"/w",\
        "tool":"Bash","reason":"permission_prompt","text":"needs permission",\
        "at":"2026-08-03T10:00:00+08:00"}
        """.utf8)

        guard let event = ATMAgentEventDecoder.decode(line) else {
            return XCTFail("failed to decode a well-formed envelope")
        }
        XCTAssertEqual(event.event, .attention)
        XCTAssertEqual(event.source, "claude")
        XCTAssertEqual(event.sessionID, "abc")
        XCTAssertEqual(event.tool, "Bash")
        XCTAssertEqual(event.reason, "permission_prompt")
        XCTAssertEqual(event.joinCandidates, ["abc", "/w"])
    }

    func testRejectsUnknownVersionsAndGarbage() {
        // A future schema might reuse these field names with different meanings,
        // so acting on it would be a guess.
        XCTAssertNil(ATMAgentEventDecoder.decode(Data(
            #"{"v":2,"source":"claude","event":"attention","session_id":"abc"}"#.utf8
        )))
        XCTAssertNil(ATMAgentEventDecoder.decode(Data(
            #"{"v":1,"source":"claude","event":"teleported","session_id":"abc"}"#.utf8
        )))
        XCTAssertNil(ATMAgentEventDecoder.decode(Data("not json".utf8)))
    }

    // MARK: - Framing

    func testSplitLinesKeepsPartialLinesBuffered() {
        // A single socket read can land mid-line; the remainder has to survive
        // until the rest of the bytes arrive.
        let (lines, remainder) = ATMAgentEventDecoder.splitLines(Data("{\"a\":1}\n{\"b\":2}\n{\"c\":".utf8))
        XCTAssertEqual(lines.map { String(decoding: $0, as: UTF8.self) }, ["{\"a\":1}", "{\"b\":2}"])
        XCTAssertEqual(String(decoding: remainder, as: UTF8.self), "{\"c\":")
    }

    func testSplitLinesSkipsBlankLines() {
        let (lines, remainder) = ATMAgentEventDecoder.splitLines(Data("\n\n{\"a\":1}\n".utf8))
        XCTAssertEqual(lines.count, 1)
        XCTAssertTrue(remainder.isEmpty)
    }

    // MARK: - Join

    func testClaudeSessionIDJoinsDirectly() {
        let signals = ["claude-session-uuid": signal()]
        let matched = ATMAgentAttentionJoin.signal(
            for: session(sessionID: "claude-session-uuid"),
            in: signals,
            now: now
        )
        XCTAssertEqual(matched?.reason, "permission_prompt")
    }

    func testCodexJoinsOnResumeIDBecauseItsSessionIDIsTruncated() {
        // The Codex parser truncates its session id to eight characters and
        // keeps the full thread id, which is what the hook reports, in resumeID.
        let signals = ["7b2c1d9e-full-thread-id": signal(source: "codex")]
        let matched = ATMAgentAttentionJoin.signal(
            for: session(tool: "Codex", sessionID: "7b2c1d9e", resumeID: "7b2c1d9e-full-thread-id"),
            in: signals,
            now: now
        )
        XCTAssertEqual(matched?.source, "codex")
    }

    func testCwdIsTheLastResort() {
        let signals = ["/Users/tester/mox/atm": signal(reason: "idle_prompt")]
        let matched = ATMAgentAttentionJoin.signal(
            for: session(sessionID: "unrelated", cwd: "/Users/tester/mox/atm"),
            in: signals,
            now: now
        )
        XCTAssertEqual(matched?.reason, "idle_prompt")
    }

    func testSessionIDWinsOverCwdWhenBothMatch() {
        let signals = [
            "abc": signal(reason: "permission_prompt"),
            "/w": signal(reason: "idle_prompt"),
        ]
        let matched = ATMAgentAttentionJoin.signal(
            for: session(sessionID: "abc", cwd: "/w"),
            in: signals,
            now: now
        )
        XCTAssertEqual(matched?.reason, "permission_prompt")
    }

    func testUnmatchedSessionsGetNoSignal() {
        XCTAssertNil(ATMAgentAttentionJoin.signal(
            for: session(sessionID: "abc", cwd: "/w"),
            in: ["other": signal()],
            now: now
        ))
    }

    func testJoinKeysAreOrderedAndDeduplicated() {
        let keys = ATMAgentAttentionJoin.joinKeys(
            for: session(sessionID: "same", resumeID: "same", cwd: "  ")
        )
        XCTAssertEqual(keys, ["same"])
    }

    // MARK: - TTL

    func testSignalExpiresAfterItsTimeToLive() {
        let stale = signal(receivedAt: now.addingTimeInterval(-ATMAgentAttentionSignal.timeToLive - 1))
        XCTAssertFalse(stale.isLive(at: now))
        // An expired signal must not keep a row orange: hooks are best effort,
        // and a crashed CLI never sends the clearing event.
        XCTAssertNil(ATMAgentAttentionJoin.signal(
            for: session(sessionID: "abc"),
            in: ["abc": stale],
            now: now
        ))
    }

    func testSignalIsLiveJustInsideItsTimeToLive() {
        let fresh = signal(receivedAt: now.addingTimeInterval(-ATMAgentAttentionSignal.timeToLive + 1))
        XCTAssertTrue(fresh.isLive(at: now))
    }

    // MARK: - Merge onto a snapshot

    func testMergeStampsOnlyMatchingSessions() {
        let sessions = [
            session(sessionID: "waiting"),
            session(sessionID: "busy"),
        ]
        let merged = ATMAgentAttentionJoin.merge(
            sessions,
            signals: ["waiting": signal()],
            now: now
        )
        XCTAssertNotNil(merged[0].attentionSignal)
        XCTAssertNil(merged[1].attentionSignal)
    }

    func testLiveStatusAppliesSignalsOnEverySnapshot() {
        // The poller replaces the whole session array each refresh, so the
        // overlay has to be re-applied or it would be wiped every 3 seconds.
        let status = ATMLiveStatus(sessions: [session(sessionID: "abc")], time: "12:00:00")
        let applied = status.applyingAttentionSignals(["abc": signal()], now: now)
        XCTAssertEqual(applied.sessions.first?.attentionSignal?.reason, "permission_prompt")
        XCTAssertEqual(applied.time, "12:00:00")
    }

    func testApplyingNoSignalsLeavesTheSnapshotAlone() {
        let status = ATMLiveStatus(sessions: [session(sessionID: "abc")], time: "12:00:00")
        XCTAssertNil(status.applyingAttentionSignals([:], now: now).sessions.first?.attentionSignal)
    }

    // MARK: - Presence

    func testHookSignalMakesASessionNeedAttentionWithoutAnyKeywords() {
        // The case the old heuristic could never catch: a tool call blocked on a
        // permission prompt writes no assistant text at all.
        var blocked = session(sessionID: "abc", lastAnswer: "正在运行测试", ageSeconds: 600)
        XCTAssertFalse(blocked.needsUserAttention)
        XCTAssertEqual(blocked.presenceState, .recent)

        blocked.attentionSignal = signal()
        XCTAssertTrue(blocked.needsUserAttention)
        XCTAssertEqual(blocked.presenceState, .attention)
        XCTAssertTrue(blocked.needsUserText.contains("等待授权"))
        XCTAssertTrue(blocked.needsUserText.contains("Bash"))
    }

    func testKeywordHeuristicStillCoversAgentsWithoutHooks() {
        let guessed = session(tool: "Copilot", sessionID: "abc", lastAnswer: "请确认后我再继续")
        XCTAssertNil(guessed.attentionSignal)
        XCTAssertTrue(guessed.matchesAttentionKeywords)
        XCTAssertTrue(guessed.needsUserAttention)
    }

    func testDisplayReasonCoversTheMatchersWeInstall() {
        XCTAssertEqual(signal(reason: "permission_prompt").displayReason, "等待授权")
        XCTAssertEqual(signal(reason: "permission_request").displayReason, "等待授权")
        XCTAssertEqual(signal(reason: "idle_prompt").displayReason, "等待输入")
        XCTAssertEqual(signal(reason: "agent_needs_input").displayReason, "需要补充信息")
        XCTAssertEqual(signal(reason: "ask_user_question").displayReason, "等待选择")
        XCTAssertEqual(signal(reason: "settled").displayReason, "等待下一步")
        XCTAssertEqual(signal(reason: "something_new").displayReason, "需要你")
    }

    // MARK: - Bus lifecycle

    @MainActor
    func testAttentionIsSetThenClearedByTheAgentMovingOn() {
        let bus = ATMAgentEventBus()
        let attention = ATMAgentEvent(
            version: 1, source: "claude", event: .attention, sessionID: "abc",
            cwd: "/w", tool: "Bash", reason: "permission_prompt", text: nil, at: nil
        )
        XCTAssertTrue(bus.apply(attention, now: now))
        XCTAssertNotNil(bus.signals["abc"])
        // Recorded under cwd too, so an agent whose hook session id does not
        // match the parser's can still be joined.
        XCTAssertNotNil(bus.signals["/w"])

        let started = ATMAgentEvent(
            version: 1, source: "claude", event: .started, sessionID: "abc",
            cwd: "/w", tool: nil, reason: nil, text: nil, at: nil
        )
        XCTAssertTrue(bus.apply(started, now: now))
        XCTAssertTrue(bus.signals.isEmpty)
    }

    @MainActor
    func testCompletedAndSessionEndAlsoClearAttention() {
        for clearing in [ATMAgentEvent.Kind.completed, .sessionEnd] {
            let bus = ATMAgentEventBus()
            bus.apply(ATMAgentEvent(
                version: 1, source: "claude", event: .attention, sessionID: "abc",
                cwd: nil, tool: nil, reason: "idle_prompt", text: nil, at: nil
            ), now: now)
            bus.apply(ATMAgentEvent(
                version: 1, source: "claude", event: clearing, sessionID: "abc",
                cwd: nil, tool: nil, reason: nil, text: nil, at: nil
            ), now: now)
            XCTAssertTrue(bus.signals.isEmpty, "\(clearing) should clear attention")
        }
    }

    @MainActor
    func testResumedClearsAttentionBecauseTheTurnIsNotOverYet() {
        // The case the notch used to get wrong: you answer a permission prompt,
        // the agent goes back to work, and neither `started` nor `completed`
        // fires for the rest of the turn. Without `resumed` the row stays orange
        // until the ten-minute TTL.
        let bus = ATMAgentEventBus()
        bus.apply(ATMAgentEvent(
            version: 1, source: "claude", event: .attention, sessionID: "abc",
            cwd: "/w", tool: "Bash", reason: "permission_prompt", text: nil, at: nil
        ), now: now)
        XCTAssertFalse(bus.signals.isEmpty)

        let resumed = ATMAgentEvent(
            version: 1, source: "claude", event: .resumed, sessionID: "abc",
            cwd: "/w", tool: "Bash", reason: nil, text: nil, at: nil
        )
        XCTAssertTrue(bus.apply(resumed, now: now))
        XCTAssertTrue(bus.signals.isEmpty)
    }

    @MainActor
    func testResumedDoesNotHandTurnStateToHooks() {
        // A tool hook firing says nothing about whether the turn-end hook does.
        // Crediting it would silence snapshot diffing for an agent whose `Stop`
        // never arrives, which is Grok Build today: its completion card and
        // chime would both disappear rather than improve.
        let bus = ATMAgentEventBus()
        bus.apply(ATMAgentEvent(
            version: 1, source: "grokbuild", event: .resumed, sessionID: "g1",
            cwd: "/w", tool: "shell", reason: nil, text: nil, at: nil
        ), now: now)
        XCTAssertFalse(bus.isHookBacked(session(tool: "Grok Build", sessionID: "g1", cwd: "/w")))
        XCTAssertFalse(bus.isHookAuthoritative(session(tool: "Grok Build", sessionID: "g1", cwd: "/w")))
        // Still counts as the hooks being alive, which is what slows polling.
        XCTAssertEqual(bus.lastEventAt, now)

        bus.apply(ATMAgentEvent(
            version: 1, source: "grokbuild", event: .completed, sessionID: "g1",
            cwd: "/w", tool: nil, reason: nil, text: nil, at: nil
        ), now: now)
        XCTAssertTrue(bus.isHookBacked(session(tool: "Grok Build", sessionID: "g1", cwd: "/w")))
    }

    func testOnlyResumedSkipsTheSnapshotRefresh() {
        // `resumed` fires once per tool call. Letting it re-run `atm session
        // status` would pull the effective poll interval down to the debounce
        // window for as long as an agent is working.
        XCTAssertFalse(ATMAgentEvent.Kind.resumed.mayChangeSnapshot)
        for kind in [ATMAgentEvent.Kind.sessionStart, .started, .attention, .completed, .sessionEnd] {
            XCTAssertTrue(kind.mayChangeSnapshot, "\(kind) needs a fresh snapshot to be visible")
        }
    }

    @MainActor
    func testSessionStartDoesNotClaimAttention() {
        let bus = ATMAgentEventBus()
        let event = ATMAgentEvent(
            version: 1, source: "claude", event: .sessionStart, sessionID: "abc",
            cwd: nil, tool: nil, reason: nil, text: nil, at: nil
        )
        XCTAssertFalse(bus.apply(event, now: now))
        XCTAssertTrue(bus.signals.isEmpty)
        // It still counts as the hooks being alive, which is what slows polling.
        XCTAssertEqual(bus.lastEventAt, now)
    }

    private func hookReport(
        source: String,
        installed: [String],
        error: String? = nil
    ) -> ATMAgentHookReport {
        let errorField = error.map { ",\"error\":\"\($0)\"" } ?? ""
        let names = installed.map { "\"\($0)\"" }.joined(separator: ",")
        let json = """
        {"socket_path":"/Users/tester/.atm/notch.sock","sources":[
          {"source":"\(source)","path":"/c","installed":[\(names)]\(errorField)}
        ]}
        """
        return try! JSONDecoder().decode(ATMAgentHookReport.self, from: Data(json.utf8))
    }

    func testAnInstalledStopHookOwnsTheStateBeforeTheFirstEventArrives() {
        // The launch window: ATM starts while a Claude conversation is mid-turn,
        // so no event has arrived for it yet. Its hooks still own the state —
        // otherwise text diffing declares the turn finished on every reply.
        let claude = session(tool: "Claude Code", sessionID: "abc")
        XCTAssertTrue(ATMAgentHookAuthority.isAuthoritative(
            session: claude,
            seenSessionKeys: [],
            report: hookReport(source: "claude", installed: ["SessionStart", "UserPromptSubmit", "Stop"]),
            isListening: true
        ))
    }

    func testAgentsWithoutHooksKeepTheHeuristic() {
        let copilot = session(tool: "Copilot", sessionID: "abc")
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: copilot,
            seenSessionKeys: [],
            report: hookReport(source: "claude", installed: ["Stop"]),
            isListening: true
        ))
    }

    func testStateIsNotHandedToHooksThatCannotReachUs() {
        let claude = session(tool: "Claude Code", sessionID: "abc")
        let installed = hookReport(source: "claude", installed: ["Stop"])
        // Socket down: nothing can deliver, so guessing beats going dark.
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: claude, seenSessionKeys: [], report: installed, isListening: false
        ))
        // Never asked the CLI what is installed.
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: claude, seenSessionKeys: [], report: nil, isListening: true
        ))
        // Registered, but the agent's config could not be read.
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: claude,
            seenSessionKeys: [],
            report: hookReport(source: "claude", installed: ["Stop"], error: "settings.json is not valid JSON"),
            isListening: true
        ))
        // Stop missing: only the notification matchers got installed.
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: claude,
            seenSessionKeys: [],
            report: hookReport(source: "claude", installed: ["Notification(idle_prompt)"]),
            isListening: true
        ))
    }

    func testAReceivedEventStillGrantsAuthorityWithoutAnyReport() {
        // Pi has no config file to inspect; its events are the only evidence.
        let pi = session(tool: "Pi", sessionID: "pi-1")
        XCTAssertTrue(ATMAgentHookAuthority.isAuthoritative(
            session: pi, seenSessionKeys: ["pi-1"], report: nil, isListening: true
        ))
    }

    func testToolNamesMapOntoHookSources() {
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Claude Code"), "claude")
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Codex"), "codex")
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Pi"), "pi")
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Grok Build"), "grokbuild")
        // The IDE and the CLI read the same ~/.qoder/settings.json.
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Qoder"), "qoder")
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Qoder CLI"), "qoder")
        XCTAssertNil(ATMAgentHookSource.source(forTool: "Copilot"))
        // A different product with its own runtime: claiming Qoder's install
        // covers it would hand its state to hooks that never fire.
        XCTAssertNil(ATMAgentHookSource.source(forTool: "QoderWork"))
    }

    func testQoderKeepsTranscriptInferenceUntilARealHookEventArrives() {
        // Qoder reads its settings once at launch, so a hook installed into
        // ~/.qoder/settings.json does not fire until the app restarts. Trusting
        // the file would leave every Qoder session with no source of state at all.
        let qoder = session(
            tool: "Qoder",
            sessionID: "1af727db",
            resumeID: "1af727db-91c1-4d8d-87f5-fcf4ad264c62"
        )
        let installed = hookReport(
            source: "qoder",
            installed: ["SessionStart", "UserPromptSubmit", "Stop", "SessionEnd", "Notification"]
        )
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: qoder, seenSessionKeys: [], report: installed, isListening: true
        ))
        // The hook reports the full session id, which is why the parser keeps it
        // as resumeID: this is the join that grants authority.
        XCTAssertTrue(ATMAgentHookAuthority.isAuthoritative(
            session: qoder,
            seenSessionKeys: ["1af727db-91c1-4d8d-87f5-fcf4ad264c62"],
            report: installed,
            isListening: true
        ))
    }

    @MainActor
    func testAQoderStopEventTakesOverTheTurnStateOfBothRowsForThatSession() {
        // One Qoder conversation is reported twice — once from the SQLite client
        // database, once from the transcript directory — and both rows carry the
        // same full session id. A single Stop must cover both, or the row that
        // missed out keeps inferring completions from text and the island prompts
        // twice per turn.
        let bus = ATMAgentEventBus()
        bus.apply(ATMAgentEvent(
            version: 1, source: "qoder", event: .completed,
            sessionID: "1af727db-91c1-4d8d-87f5-fcf4ad264c62", cwd: "/Users/tester/mox/atm",
            tool: nil, reason: nil, text: "done", at: nil
        ), now: now)

        for tool in ["Qoder", "Qoder CLI"] {
            let row = session(
                tool: tool,
                sessionID: "1af727db",
                resumeID: "1af727db-91c1-4d8d-87f5-fcf4ad264c62"
            )
            XCTAssertTrue(bus.isHookAuthoritative(row), "\(tool) should be hook-owned")
        }
    }

    func testGrokKeepsTranscriptInferenceUntilARealHookEventArrives() {
        // Installing Grok lifecycle hooks must not silence the text path before
        // the first envelope is seen — Grok's Stop hook is not always observed.
        let grok = session(tool: "Grok Build", sessionID: "g1")
        let installed = hookReport(
            source: "grokbuild",
            installed: ["SessionStart", "UserPromptSubmit", "Stop", "SessionEnd", "Notification"]
        )
        XCTAssertFalse(ATMAgentHookAuthority.isAuthoritative(
            session: grok, seenSessionKeys: [], report: installed, isListening: true
        ))
        XCTAssertTrue(ATMAgentHookAuthority.isAuthoritative(
            session: grok, seenSessionKeys: ["g1"], report: installed, isListening: true
        ))
    }

    @MainActor
    func testHookAuthorityIsPerSessionNotPerDirectory() {
        // Two agents in one repo share a cwd. A Codex event must not sign the
        // unhooked copilot session next to it over to the hook path, or that
        // session loses its only source of state.
        let bus = ATMAgentEventBus()
        bus.apply(ATMAgentEvent(
            version: 1, source: "codex", event: .completed,
            sessionID: "019fc64f-thread", cwd: "/Users/tester/mox/atm",
            tool: nil, reason: nil, text: "done", at: nil
        ), now: now)

        let hookedSession = session(
            tool: "Codex",
            sessionID: "019fc64f",
            resumeID: "019fc64f-thread",
            cwd: "/Users/tester/mox/atm"
        )
        let neighbour = session(
            tool: "Copilot",
            sessionID: "copilot-1",
            cwd: "/Users/tester/mox/atm"
        )

        XCTAssertTrue(bus.isHookAuthoritative(hookedSession))
        XCTAssertFalse(bus.isHookAuthoritative(neighbour))
        // The looser predicate behind sound suppression is unchanged: a cwd match
        // still counts there.
        XCTAssertTrue(bus.isHookBacked(neighbour))
    }

    @MainActor
    func testEventsWithoutAnyIdentifierAreIgnored() {
        let bus = ATMAgentEventBus()
        let event = ATMAgentEvent(
            version: 1, source: "claude", event: .attention, sessionID: nil,
            cwd: nil, tool: nil, reason: "idle_prompt", text: nil, at: nil
        )
        XCTAssertFalse(bus.apply(event, now: now))
        XCTAssertTrue(bus.signals.isEmpty)
    }

    @MainActor
    func testPurgeDropsExpiredSignals() {
        let bus = ATMAgentEventBus()
        bus.apply(ATMAgentEvent(
            version: 1, source: "claude", event: .attention, sessionID: "abc",
            cwd: nil, tool: nil, reason: "idle_prompt", text: nil, at: nil
        ), now: now)
        bus.purgeExpired(now: now.addingTimeInterval(ATMAgentAttentionSignal.timeToLive + 1))
        XCTAssertTrue(bus.signals.isEmpty)
    }

    // MARK: - Sound

    func testEventKindsMapOntoSounds() {
        XCTAssertEqual(ATMAgentEvent.Kind.attention.soundEvent, .attentionRequired)
        XCTAssertEqual(ATMAgentEvent.Kind.completed.soundEvent, .taskCompleted)
        XCTAssertEqual(ATMAgentEvent.Kind.started.soundEvent, .processingStarted)
        // Lifecycle bookkeeping is not worth a chime.
        XCTAssertNil(ATMAgentEvent.Kind.sessionStart.soundEvent)
        XCTAssertNil(ATMAgentEvent.Kind.sessionEnd.soundEvent)
    }

    private func soundSession(
        sessionID: String,
        input: String,
        result: String,
        answer: String = "正在处理",
        tool: String = "Claude Code",
        cwd: String? = nil
    ) -> ATMLiveSession {
        ATMLiveSession(
            tool: tool,
            sessionID: sessionID,
            project: "atm",
            cwd: cwd,
            ageSeconds: 1,
            lastQuestion: input,
            lastAnswer: answer,
            latestResult: result,
            activityState: "active"
        )
    }

    func testHookBackedSessionsDoNotAlsoChimeFromSnapshotDiffing() {
        // The event path already played this sound; diffing the next snapshot
        // must not play it a second time.
        var tracker = ATMAgentSoundTransitionTracker()
        let hooked: (ATMLiveSession) -> Bool = { $0.sessionID == "hooked" }

        let baseline = soundSession(sessionID: "hooked", input: "one", result: "old")
        XCTAssertNil(tracker.nextEvent(for: [baseline], hookBacked: hooked))

        let progressed = soundSession(sessionID: "hooked", input: "two", result: "new")
        XCTAssertNil(tracker.nextEvent(for: [progressed], hookBacked: hooked))
    }

    func testUnhookedSessionsStillChimeFromSnapshotDiffing() {
        var tracker = ATMAgentSoundTransitionTracker()
        let hooked: (ATMLiveSession) -> Bool = { $0.sessionID == "hooked" }

        let baseline = soundSession(sessionID: "plain", input: "one", result: "old")
        XCTAssertNil(tracker.nextEvent(for: [baseline], hookBacked: hooked))

        let progressed = soundSession(sessionID: "plain", input: "two", result: "old")
        XCTAssertEqual(tracker.nextEvent(for: [progressed], hookBacked: hooked), .processingStarted)
    }

    func testLosingHookCoverageDoesNotReplayASuppressedTransition() {
        // Hook coverage can lapse (the agent exits, the hook is uninstalled). The
        // suppressed transition must not then fire late as if it were new.
        var tracker = ATMAgentSoundTransitionTracker()
        nonisolated(unsafe) var isHooked = true
        let hooked: (ATMLiveSession) -> Bool = { _ in isHooked }

        let baseline = soundSession(sessionID: "abc", input: "one", result: "old")
        XCTAssertNil(tracker.nextEvent(for: [baseline], hookBacked: hooked))

        let progressed = soundSession(sessionID: "abc", input: "two", result: "new")
        XCTAssertNil(tracker.nextEvent(for: [progressed], hookBacked: hooked))

        isHooked = false
        XCTAssertNil(tracker.nextEvent(for: [progressed], hookBacked: hooked))
    }

    @MainActor
    func testBusRemembersWhichSessionsAreHookBacked() {
        let bus = ATMAgentEventBus()
        XCTAssertFalse(bus.isHookBacked(session(sessionID: "abc")))

        bus.apply(ATMAgentEvent(
            version: 1, source: "claude", event: .sessionStart, sessionID: "abc",
            cwd: "/w", tool: nil, reason: nil, text: nil, at: nil
        ), now: now)

        XCTAssertTrue(bus.isHookBacked(session(sessionID: "abc")))
        // Matching by cwd too, so an agent whose hook session id differs from the
        // parser's still counts as covered.
        XCTAssertTrue(bus.isHookBacked(session(sessionID: "other", cwd: "/w")))
        XCTAssertFalse(bus.isHookBacked(session(sessionID: "unrelated", cwd: "/elsewhere")))
    }

    @MainActor
    func testEveryAppliedEventIsPublishedSoTheSnapshotGetsRefreshed() {
        // A `completed` event on a session with no pending attention signal
        // changes nothing in the overlay but still means the snapshot is stale.
        let bus = ATMAgentEventBus()
        var published: [ATMAgentEvent.Kind] = []
        let cancellable = bus.didApplyEvent.sink { published.append($0.event) }
        defer { cancellable.cancel() }

        bus.apply(ATMAgentEvent(
            version: 1, source: "claude", event: .completed, sessionID: "abc",
            cwd: nil, tool: nil, reason: nil, text: "done", at: nil
        ), now: now)

        XCTAssertEqual(published, [.completed])
    }

    // MARK: - Hook status decoding

    func testDecodesHookStatusReportFromTheCLI() throws {
        let json = """
        {"socket_path":"/Users/tester/.atm/notch.sock","sources":[
          {"source":"claude","path":"/Users/tester/.claude/settings.json",
           "installed":["Stop"],"missing":["SessionStart"],
           "conflicts":["PermissionRequest: /Users/tester/.ping-island/bin/ping-island-bridge --source claude"]},
          {"source":"pi","manual":"复制扩展到 ~/.pi/agent/extensions/"}
        ]}
        """
        let report = try JSONDecoder().decode(ATMAgentHookReport.self, from: Data(json.utf8))
        XCTAssertEqual(report.socketPath, "/Users/tester/.atm/notch.sock")
        XCTAssertEqual(report.sources.count, 2)

        let claude = report.sources[0]
        XCTAssertEqual(claude.displayName, "Claude Code")
        XCTAssertEqual(claude.installed, ["Stop"])
        XCTAssertEqual(claude.missing, ["SessionStart"])
        XCTAssertFalse(claude.isFullyInstalled, "a source with missing hooks is not fully installed")
        XCTAssertEqual(claude.conflicts.count, 1)

        let pi = report.sources[1]
        XCTAssertEqual(pi.displayName, "Pi")
        XCTAssertNotNil(pi.manual)
        // The app cannot verify a file it did not write, so Pi never claims to be
        // installed from here.
        XCTAssertFalse(pi.isFullyInstalled)
    }

    func testFullyInstalledRequiresNoMissingHooksAndNoError() throws {
        let json = """
        {"socket_path":"/tmp/s.sock","sources":[
          {"source":"claude","path":"/p","installed":["Stop","SessionStart"]}
        ]}
        """
        let report = try JSONDecoder().decode(ATMAgentHookReport.self, from: Data(json.utf8))
        XCTAssertTrue(report.sources[0].isFullyInstalled)
    }

    // MARK: - Poll interval

    func testPollingSlowsDownOnlyWhileHooksAreFresh() {
        let fast = ATMLiveStatusRefreshPolicy.interval
        let slow = ATMLiveStatusRefreshPolicy.hookBackedInterval
        XCTAssertGreaterThan(slow, fast)

        XCTAssertEqual(
            ATMLiveStatusRefreshPolicy.interval(lastHookEventAt: nil, now: now),
            fast
        )
        XCTAssertEqual(
            ATMLiveStatusRefreshPolicy.interval(lastHookEventAt: now, now: now),
            slow
        )
        // Hooks uninstalled or the agent gone: fall back to scraping promptly.
        XCTAssertEqual(
            ATMLiveStatusRefreshPolicy.interval(
                lastHookEventAt: now.addingTimeInterval(-ATMLiveStatusRefreshPolicy.hookFreshness - 1),
                now: now
            ),
            fast
        )
    }

    // MARK: - Socket path

    func testSocketPathDefaultsUnderAtmHomeAndHonoursTheOverride() {
        XCTAssertEqual(
            ATMAgentEventListener.defaultSocketPath(environment: [:], home: "/Users/tester"),
            "/Users/tester/.atm/notch.sock"
        )
        XCTAssertEqual(
            ATMAgentEventListener.defaultSocketPath(
                environment: ["ATM_NOTCH_SOCKET": "/tmp/custom.sock"],
                home: "/Users/tester"
            ),
            "/tmp/custom.sock"
        )
        // Must stay inside sockaddr_un for a realistically long home directory.
        let path = ATMAgentEventListener.defaultSocketPath(
            environment: [:],
            home: "/Users/a-fairly-long-account-name"
        )
        XCTAssertLessThanOrEqual(path.utf8.count, ATMAgentEventListener.maximumPathLength)
    }
}
