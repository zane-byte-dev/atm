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
        XCTAssertNil(ATMAgentHookSource.source(forTool: "Copilot"))
        XCTAssertNil(ATMAgentHookSource.source(forTool: "QoderWork"))
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

    func testHookedSessionsNeverCompleteFromSnapshotDiffing() {
        // The bug this guards: Codex writes `commentary` while it works, so its
        // latest reply changes several times inside one turn. Read as a result,
        // every one of those looked like a finished turn — while the `Stop` hook
        // that actually ends the turn had not fired yet.
        var tracker = ATMAgentCompletionTransitionTracker()
        let hooked: (ATMLiveSession) -> Bool = { $0.sessionID == "hooked" }

        let baseline = soundSession(
            sessionID: "hooked",
            input: "一个长任务",
            result: "我先定位对应的组件",
            answer: "我先定位对应的组件"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline], hookBacked: hooked))
        XCTAssertTrue(tracker.completedSessionIDs.isEmpty)

        let stillWorking = soundSession(
            sessionID: "hooked",
            input: "一个长任务",
            result: "样式已经改完，接下来跑测试",
            answer: "样式已经改完，接下来跑测试"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [stillWorking], hookBacked: hooked))
        XCTAssertTrue(tracker.newlyCompletedSessionIDs.isEmpty)
        XCTAssertTrue(tracker.completedSessionIDs.isEmpty)
    }

    func testUnhookedSessionsStillCompleteFromSnapshotDiffing() {
        // Same directory as a hooked session, no hooks of its own: the heuristic
        // is all it has, so it must keep working.
        var tracker = ATMAgentCompletionTransitionTracker()
        let hooked: (ATMLiveSession) -> Bool = { $0.sessionID == "hooked" }

        let baseline = soundSession(
            sessionID: "plain",
            input: "one",
            result: "old",
            answer: "old",
            tool: "Copilot",
            cwd: "/Users/tester/mox/atm"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline], hookBacked: hooked))

        let completed = soundSession(
            sessionID: "plain",
            input: "one",
            result: "new result",
            answer: "new result",
            tool: "Copilot",
            cwd: "/Users/tester/mox/atm"
        )
        XCTAssertEqual(
            tracker.nextCompletedSession(in: [completed], hookBacked: hooked)?.id,
            completed.id
        )
        XCTAssertTrue(tracker.completedSessionIDs.contains(completed.id))
    }

    func testHookedSessionIsNotSeededAsCompletedWhilePriming() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let existing = soundSession(
            sessionID: "hooked",
            input: "one",
            result: "already done",
            answer: "already done"
        )
        XCTAssertNil(
            tracker.nextCompletedSession(in: [existing], hookBacked: { _ in true })
        )
        XCTAssertTrue(tracker.isPrimed)
        XCTAssertTrue(tracker.completedSessionIDs.isEmpty)
    }

    func testLosingHookCoverageDoesNotReplayASuppressedCompletion() {
        var tracker = ATMAgentCompletionTransitionTracker()
        nonisolated(unsafe) var isHooked = true
        let hooked: (ATMLiveSession) -> Bool = { _ in isHooked }

        let baseline = soundSession(sessionID: "abc", input: "one", result: "old", answer: "old")
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline], hookBacked: hooked))

        let completed = soundSession(sessionID: "abc", input: "one", result: "new", answer: "new")
        XCTAssertNil(tracker.nextCompletedSession(in: [completed], hookBacked: hooked))

        isHooked = false
        XCTAssertNil(tracker.nextCompletedSession(in: [completed], hookBacked: hooked))
    }

    func testCompletionReminderPrimesSilentlyThenReturnsTheFinishedSession() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let baseline = soundSession(sessionID: "abc", input: "one", result: "old")
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline]))

        let completed = soundSession(sessionID: "abc", input: "one", result: "new result")
        XCTAssertEqual(tracker.nextCompletedSession(in: [completed])?.id, completed.id)
        XCTAssertNil(tracker.nextCompletedSession(in: [completed]))
    }

    func testEmptyPlaceholderDoesNotPrimeOrReplayHistoricalCompletions() {
        var tracker = ATMAgentCompletionTransitionTracker()
        XCTAssertNil(tracker.nextCompletedSession(in: []))
        XCTAssertFalse(tracker.isPrimed)

        let existing = soundSession(
            sessionID: "abc",
            input: "one",
            result: "already done",
            answer: "already done"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [existing]))
        XCTAssertTrue(tracker.isPrimed)
        XCTAssertEqual(tracker.completedSessionIDs, [existing.id])
        XCTAssertTrue(tracker.newlyCompletedSessionIDs.isEmpty)
    }

    func testInitialSnapshotDoesNotTreatPreviousResultAsCurrentCompletion() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let working = soundSession(
            sessionID: "abc",
            input: "new request",
            result: "previous final answer",
            answer: "正在处理新一轮请求"
        )

        XCTAssertNil(tracker.nextCompletedSession(in: [working]))
        XCTAssertFalse(tracker.completedSessionIDs.contains(working.id))
        XCTAssertEqual(
            ATMAgentNotchSessionState.resolve(
                session: working,
                completedSessionIDs: tracker.completedSessionIDs
            ),
            .working
        )
    }

    func testRepeatedInputStillClearsAStaleCompletion() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let completed = soundSession(
            sessionID: "abc",
            input: "same request",
            result: "finished",
            answer: "finished"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [completed]))
        XCTAssertTrue(tracker.completedSessionIDs.contains(completed.id))

        let working = soundSession(
            sessionID: "abc",
            input: "same request",
            result: "finished",
            answer: "正在重新处理"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [working]))
        XCTAssertTrue(tracker.startedSessionIDs.contains(working.id))
        XCTAssertFalse(tracker.completedSessionIDs.contains(working.id))
    }

    func testCompletionReminderDoesNotReplayAResultAfterSessionTemporarilyDisappears() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let baseline = soundSession(sessionID: "abc", input: "one", result: "old")
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline]))

        let completed = soundSession(sessionID: "abc", input: "one", result: "new result")
        XCTAssertEqual(tracker.nextCompletedSession(in: [completed])?.id, completed.id)
        XCTAssertNil(tracker.nextCompletedSession(in: []))
        XCTAssertNil(tracker.nextCompletedSession(in: [completed]))
    }

    func testCompletionReminderIgnoresUnobservedBindings() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let baseline = soundSession(sessionID: "abc", input: "one", result: "old")
        XCTAssertNil(tracker.nextCompletedSession(in: [baseline]))

        let unobserved = ATMLiveSession(
            tool: "Claude Code",
            sessionID: "abc",
            project: "atm",
            ageSeconds: 1,
            lastQuestion: "one",
            latestResult: "new result",
            activityState: "unobserved"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [unobserved]))
    }

    func testNewInputMovesACompletedSessionBackToWorking() {
        var tracker = ATMAgentCompletionTransitionTracker()
        let completed = soundSession(
            sessionID: "abc",
            input: "one",
            result: "old",
            answer: "old"
        )
        XCTAssertNil(tracker.nextCompletedSession(in: [completed]))
        XCTAssertTrue(tracker.completedSessionIDs.contains(completed.id))
        XCTAssertEqual(
            ATMAgentNotchSessionState.resolve(
                session: completed,
                completedSessionIDs: tracker.completedSessionIDs
            ),
            .completed
        )

        let working = soundSession(sessionID: "abc", input: "two", result: "old")
        XCTAssertNil(tracker.nextCompletedSession(in: [working]))
        XCTAssertTrue(tracker.startedSessionIDs.contains(working.id))
        XCTAssertFalse(tracker.completedSessionIDs.contains(working.id))
        XCTAssertEqual(
            ATMAgentNotchSessionState.resolve(
                session: working,
                completedSessionIDs: tracker.completedSessionIDs
            ),
            .working
        )
    }

    func testAttentionStateOutranksCompletedState() {
        var attention = soundSession(sessionID: "abc", input: "one", result: "done")
        attention.attentionSignal = ATMAgentAttentionSignal(
            reason: "idle_prompt",
            tool: nil,
            text: nil,
            source: "codex",
            receivedAt: now
        )
        XCTAssertEqual(
            ATMAgentNotchSessionState.resolve(
                session: attention,
                completedSessionIDs: [attention.id]
            ),
            .attention
        )
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

    // MARK: - Hover

    /// Geometry matching a 14" notched Mac: the compact strip sits at the top
    /// centre, and the expanded panel is wider and taller around the same centre.
    private var compactStrip: CGRect { CGRect(x: 590, y: 988, width: 332, height: 34) }
    private var expandedPanel: CGRect { CGRect(x: 456, y: 672, width: 600, height: 350) }

    func testHoverRegionWhileCompactIsTheStripPulledInFromItsEdges() {
        let region = ATMAgentNotchHover.region(
            presentation: .compact,
            compactFrame: compactStrip,
            panelFrame: compactStrip
        )
        // Where the expanded panel *would* be must not trigger anything: the
        // cursor is over the user's own windows there.
        XCTAssertFalse(region.contains(CGPoint(x: 756, y: 800)))
        // The middle of the strip triggers.
        XCTAssertTrue(region.contains(CGPoint(x: compactStrip.midX, y: compactStrip.midY)))
        // The outer edges do not: that is where the menu bar's own items live, and
        // reaching for one of those should not open the panel.
        XCTAssertFalse(region.contains(CGPoint(x: compactStrip.minX + 2, y: compactStrip.midY)))
        XCTAssertFalse(region.contains(CGPoint(x: compactStrip.maxX - 2, y: compactStrip.midY)))
        // Full height is kept: the strip is only 34pt tall, so there is nothing to
        // give away vertically.
        XCTAssertEqual(region.height, compactStrip.height)
        XCTAssertEqual(
            region.width,
            compactStrip.width - 2 * ATMAgentNotchHover.horizontalTriggerInset
        )
    }

    func testTriggerInsetNeverCollapsesANarrowStrip() {
        // The fallback strip on a screen with no notch is much narrower; insetting
        // by a fixed amount must not leave an empty or inverted rect.
        let narrow = CGRect(x: 0, y: 0, width: 100, height: 38)
        let region = ATMAgentNotchHover.triggerFrame(compactFrame: narrow)
        XCTAssertGreaterThanOrEqual(region.width, 80)
        XCTAssertTrue(region.contains(CGPoint(x: narrow.midX, y: narrow.midY)))
    }

    func testExpandedRegionUsesTheFullStripSoTheEdgesStopBeingACliff() {
        // Once open, the inset is dropped: sliding along the strip towards the edge
        // must not read as leaving.
        let region = ATMAgentNotchHover.region(
            presentation: .hoverExpanded,
            compactFrame: compactStrip,
            panelFrame: expandedPanel
        )
        XCTAssertTrue(region.contains(CGPoint(x: compactStrip.minX + 2, y: compactStrip.midY)))
    }

    func testHoverRegionWhileExpandedCoversTheWholePanel() {
        let region = ATMAgentNotchHover.region(
            presentation: .hoverExpanded,
            compactFrame: compactStrip,
            panelFrame: expandedPanel
        )
        // Moving down into the session list has to keep it open.
        XCTAssertTrue(region.contains(CGPoint(x: 756, y: 800)))
        // And the strip itself stays inside the region even though the panel
        // frame alone would already cover it.
        XCTAssertTrue(region.contains(CGPoint(x: 600, y: 1_000)))
    }

    func testCompactRegionIsAlwaysContainedInTheExpandedRegion() {
        // If it were not, expanding could drop the cursor outside the new region
        // and collapse immediately — an oscillation by construction.
        let expandedRegion = ATMAgentNotchHover.region(
            presentation: .hoverExpanded,
            compactFrame: compactStrip,
            panelFrame: expandedPanel
        )
        XCTAssertTrue(expandedRegion.contains(compactStrip))
    }

    func testOpensOnlyFromCompactAndOnlyWithTheCursorPresent() {
        XCTAssertTrue(ATMAgentNotchHover.shouldOpen(presentation: .compact, cursorIsInRegion: true))
        // The dwell elapsed but the cursor already left: a fast pass across the
        // strip must not leave a panel open behind it.
        XCTAssertFalse(ATMAgentNotchHover.shouldOpen(presentation: .compact, cursorIsInRegion: false))
        // Already open, or pinned: nothing to do.
        for presentation in [
            ATMAgentNotchPresentation.hoverExpanded, .sessionList, .notification,
        ] {
            XCTAssertFalse(
                ATMAgentNotchHover.shouldOpen(presentation: presentation, cursorIsInRegion: true),
                "\(presentation) should not re-open"
            )
        }
    }

    func testTheCursorActuallyBeingInsideVetoesEveryClose() {
        // This is the check that makes the open/collapse flapping impossible: the
        // window resize used to report a false hover exit while the cursor had not
        // moved at all, and acting on it collapsed the panel right back onto the
        // cursor, which re-opened it.
        for presentation in ATMAgentNotchPresentation.allCases {
            XCTAssertFalse(
                ATMAgentNotchHover.shouldClose(
                    presentation: presentation,
                    cursorIsInRegion: true,
                    dismissesNotificationOnExit: true
                ),
                "\(presentation) must not close while the cursor is inside"
            )
        }
    }

    func testClosesHoverExpandedOnceTheCursorIsGone() {
        XCTAssertTrue(ATMAgentNotchHover.shouldClose(
            presentation: .hoverExpanded,
            cursorIsInRegion: false,
            dismissesNotificationOnExit: false
        ))
    }

    func testAPinnedSessionListNeverClosesFromHover() {
        // It was opened by a click, so only an explicit dismissal or an outside
        // click should close it.
        XCTAssertFalse(ATMAgentNotchHover.shouldClose(
            presentation: .sessionList,
            cursorIsInRegion: false,
            dismissesNotificationOnExit: true
        ))
    }

    func testNotificationClosesOnExitOnlyAfterItsTimerElapsed() {
        // Still counting down: leaving must not cut the notification short.
        XCTAssertFalse(ATMAgentNotchHover.shouldClose(
            presentation: .notification,
            cursorIsInRegion: false,
            dismissesNotificationOnExit: false
        ))
        // Timer already fired while the cursor was inside, so it was held open
        // for reading; now that the cursor left it may go.
        XCTAssertTrue(ATMAgentNotchHover.shouldClose(
            presentation: .notification,
            cursorIsInRegion: false,
            dismissesNotificationOnExit: true
        ))
    }

    func testHoverDelaysGiveTransitAChanceToPassAndJitterAChanceToReturn() {
        XCTAssertGreaterThan(ATMAgentNotchHover.openDelay, 0)
        XCTAssertGreaterThan(ATMAgentNotchHover.closeDelay, 0)
        // The close grace must outlast the resize animation's own settling, or a
        // spurious exit mid-animation could still win.
        XCTAssertGreaterThanOrEqual(ATMAgentNotchHover.closeDelay, ATMAgentNotchHover.openDelay)
        // Both short enough to feel immediate rather than laggy.
        XCTAssertLessThan(ATMAgentNotchHover.openDelay, 0.4)
        XCTAssertLessThan(ATMAgentNotchHover.closeDelay, 0.4)
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
