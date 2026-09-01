import XCTest
@testable import ATMMenuBarApp

/// Covers the channel that replaced the notch: which agent moments are allowed to
/// interrupt someone, and how the banner is shaped and retired.
final class AgentAttentionNotifyTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func session(
        sessionID: String,
        tool: String = "Claude Code",
        project: String = "atm",
        lastAnswer: String? = nil,
        summary: String? = nil,
        activityState: String = "active",
        bindingState: String = "bound",
        signal: ATMAgentAttentionSignal? = nil
    ) -> ATMLiveSession {
        var built = ATMLiveSession(
            tool: tool,
            sessionID: sessionID,
            project: project,
            summary: summary,
            ageSeconds: 5,
            lastAnswer: lastAnswer,
            activityState: activityState,
            bindingState: bindingState
        )
        built.attentionSignal = signal
        return built
    }

    private func signal(reason: String = "permission_prompt") -> ATMAgentAttentionSignal {
        ATMAgentAttentionSignal(
            reason: reason,
            tool: "Bash",
            text: nil,
            source: "claude",
            receivedAt: now
        )
    }

    // MARK: - What may interrupt

    /// The narrowing that makes notifications tolerable. `presenceState` reads
    /// `.attention` for three different reasons; only one of them means an agent
    /// is actually blocked and waiting.
    func testOnlyAHookSignalIsAllowedToRaiseABanner() {
        let hooked = session(sessionID: "hooked", signal: signal())
        XCTAssertTrue(hooked.needsHookAttention)

        // The keyword heuristic fires on text the agent merely *wrote*. Fine as a
        // row in a list, wrong as a banner.
        let guessed = session(sessionID: "guessed", lastAnswer: "好的，请确认这个方案")
        XCTAssertTrue(guessed.needsAnyAttention, "启发式仍然计入菜单栏计数")
        XCTAssertFalse(guessed.needsHookAttention, "但不能弹通知")

        // A binding inconsistency is a data problem, not an agent waiting on you.
        let unbound = session(sessionID: "unbound", bindingState: "stale")
        XCTAssertTrue(unbound.needsAnyAttention)
        XCTAssertFalse(unbound.needsHookAttention)
    }

    func testUnobservedSessionsAreInvisibleToBothChannels() {
        let hidden = session(
            sessionID: "hidden",
            activityState: "unobserved",
            signal: signal()
        )
        XCTAssertFalse(hidden.needsHookAttention)
        XCTAssertFalse(hidden.needsAnyAttention)
    }

    // MARK: - Diffing snapshots into banners

    func testBannerIsRaisedOnceAndWithdrawnWhenTheAgentMovesOn() {
        var tracker = ATMAgentAttentionTracker()
        let waiting = session(sessionID: "abc", signal: signal())

        let first = tracker.next(for: [waiting])
        XCTAssertEqual(first.post.map(\.id), [waiting.id])
        XCTAssertTrue(first.withdraw.isEmpty)

        // Same snapshot again: the person has already been told.
        XCTAssertTrue(tracker.next(for: [waiting]).isEmpty)

        // The agent got its answer and carried on.
        let resumed = session(sessionID: "abc")
        let cleared = tracker.next(for: [resumed])
        XCTAssertTrue(cleared.post.isEmpty)
        XCTAssertEqual(cleared.withdraw, [waiting.id])

        // And it stays withdrawn rather than being retired twice.
        XCTAssertTrue(tracker.next(for: [resumed]).isEmpty)
    }

    /// A terminal can be closed while its agent is mid-prompt, which drops the row
    /// from the snapshot outright rather than clearing its signal. The banner still
    /// has to go: nothing is waiting on the other end any more.
    func testBannerIsWithdrawnWhenTheSessionVanishesEntirely() {
        var tracker = ATMAgentAttentionTracker()
        let waiting = session(sessionID: "abc", signal: signal())
        XCTAssertEqual(tracker.next(for: [waiting]).post.count, 1)

        let change = tracker.next(for: [])
        XCTAssertEqual(change.withdraw, [waiting.id])
    }

    func testASecondAgentBlockingGetsItsOwnBanner() {
        var tracker = ATMAgentAttentionTracker()
        let first = session(sessionID: "first", signal: signal())
        XCTAssertEqual(tracker.next(for: [first]).post.map(\.id), [first.id])

        let second = session(sessionID: "second", tool: "Codex", signal: signal(reason: "idle_prompt"))
        let change = tracker.next(for: [first, second])
        XCTAssertEqual(change.post.map(\.id), [second.id], "已经通知过的不重发")
        XCTAssertTrue(change.withdraw.isEmpty)
    }

    /// A blocked agent that is *replaced* by a different blocked agent in one step
    /// must both retire the old banner and raise the new one.
    func testOneSnapshotCanBothPostAndWithdraw() {
        var tracker = ATMAgentAttentionTracker()
        let leaving = session(sessionID: "leaving", signal: signal())
        _ = tracker.next(for: [leaving])

        let arriving = session(sessionID: "arriving", signal: signal())
        let change = tracker.next(for: [arriving])
        XCTAssertEqual(change.post.map(\.id), [arriving.id])
        XCTAssertEqual(change.withdraw, [leaving.id])
    }

    // MARK: - Banner content

    func testBannerNamesTheProjectTheAgentAndTheReasonTheAgentGave() {
        let payload = ATMAgentAttentionNotificationPayload.make(
            session: session(
                sessionID: "abc",
                summary: "重构刘海为通知机制",
                signal: signal()
            )
        )
        XCTAssertEqual(payload?.title, "ATM · atm")
        XCTAssertEqual(payload?.subtitle, "Claude 等待授权")
        XCTAssertEqual(payload?.body, "重构刘海为通知机制")
    }

    func testReasonCopyTracksWhatTheHookActuallyReported() {
        let idle = ATMAgentAttentionNotificationPayload.make(
            session: session(sessionID: "abc", signal: signal(reason: "idle_prompt"))
        )
        XCTAssertEqual(idle?.subtitle, "Claude 等待输入")

        let choice = ATMAgentAttentionNotificationPayload.make(
            session: session(sessionID: "abc", signal: signal(reason: "ask_user_question"))
        )
        XCTAssertEqual(choice?.subtitle, "Claude 等待选择")
    }

    func testTitleDegradesToBarePrefixWithoutAProject() {
        let payload = ATMAgentAttentionNotificationPayload.make(
            session: session(sessionID: "abc", project: "  ", signal: signal())
        )
        XCTAssertEqual(payload?.title, "ATM")
    }

    func testNoSignalMeansNoPayloadAtAll() {
        XCTAssertNil(
            ATMAgentAttentionNotificationPayload.make(
                session: session(sessionID: "abc", lastAnswer: "请确认")
            )
        )
    }

    /// Stable rather than UUID-suffixed, unlike the todo notifications: an agent
    /// can re-signal the same block, and the second delivery must replace the
    /// first instead of stacking. It is also what makes withdrawal possible.
    func testIdentifierIsStablePerSessionSoARepeatSignalReplacesIt() {
        let sessionID = "Claude Code:abc"
        XCTAssertEqual(
            ATMNotificationManager.agentAttentionIdentifier(sessionID: sessionID),
            ATMNotificationManager.agentAttentionIdentifier(sessionID: sessionID)
        )
        XCTAssertNotEqual(
            ATMNotificationManager.agentAttentionIdentifier(sessionID: sessionID),
            ATMNotificationManager.agentAttentionIdentifier(sessionID: "Codex:abc")
        )
    }

    // MARK: - Where a click lands

    func testAnAgentBannerRoutesToItsSessionRatherThanToATM() {
        XCTAssertEqual(
            ATMNotificationRoute.from(userInfo: [
                "session_id": "Claude Code:abc",
                "event": "agent_attention",
            ]),
            .agentSession("Claude Code:abc")
        )
    }

    func testTodoBannersKeepTheirOwnRoute() {
        XCTAssertEqual(
            ATMNotificationRoute.from(userInfo: ["todo_id": "t65", "event": "review"]),
            .todo("t65")
        )
    }

    func testUnrecognizedOrEmptyPayloadsJustOpenTheApp() {
        XCTAssertEqual(ATMNotificationRoute.from(userInfo: [:]), .app)
        XCTAssertEqual(
            ATMNotificationRoute.from(userInfo: ["event": "collection"]),
            .collection(itemID: nil)
        )
        XCTAssertEqual(
            ATMNotificationRoute.from(userInfo: [
                "event": "collection",
                "collection_item_id": "ci-new",
            ]),
            .collection(itemID: "ci-new")
        )
        XCTAssertEqual(ATMNotificationRoute.from(userInfo: ["session_id": ""]), .app)
    }

    // MARK: - The glanceable count

    func testMenuBarLeadsWithTheAttentionCountAndOmitsItAtZero() {
        let quiet = snapshot(sessions: [session(sessionID: "quiet")])
        XCTAssertEqual(quiet.attentionSessionCount, 0)
        XCTAssertEqual(quiet.menuBarTitle, "0 · 1.0M")

        // Counted generously on purpose: the heuristic-only session is included
        // here even though it never raised a banner.
        let busy = snapshot(sessions: [
            session(sessionID: "hooked", signal: signal()),
            session(sessionID: "guessed", lastAnswer: "请确认一下"),
            session(sessionID: "quiet"),
        ])
        XCTAssertEqual(busy.attentionSessionCount, 2)
        XCTAssertEqual(busy.menuBarTitle, "需要你 2 · 0 · 1.0M")
    }

    private func snapshot(sessions: [ATMLiveSession]) -> ATMDashboardSnapshot {
        ATMDashboardSnapshot(
            work: .empty,
            dayStats: [
                ATMDayStats(
                    date: "2026-08-10",
                    sessions: 1,
                    queries: 1,
                    inputTokens: 1_000_000,
                    outputTokens: 0,
                    costUSD: 0
                ),
            ],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            rangeData: [:],
            liveStatus: ATMLiveStatus(sessions: sessions, time: "12:00"),
            currentSession: nil,
            refreshedAt: now
        )
    }
}
