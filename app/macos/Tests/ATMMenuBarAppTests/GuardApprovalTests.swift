import XCTest

@testable import ATMMenuBarApp

final class GuardApprovalTests: XCTestCase {

    // MARK: - Socket decoding

    /// The most important assertion in this file: adding approval requests to the
    /// notch socket must not change how a line from an already-installed hook is
    /// read. Those hooks are in the user's own agent configs and are not upgraded
    /// alongside ATM.
    func testALineWithNoTypeIsStillAnAgentEvent() throws {
        let line = Data(
            #"{"v":1,"event":"attention","session_id":"s1","source":"claude","reason":"permission_prompt"}"#
                .utf8)
        guard case .agent(let event)? = ATMAgentEventDecoder.decodeMessage(line) else {
            return XCTFail("a typeless line no longer decodes as an agent event")
        }
        XCTAssertEqual(event.sessionID, "s1")
    }

    func testAGuardRequestDecodesIntoItsOwnCase() throws {
        let line = Data(
            #"{"v":1,"type":"guard_request","id":"ap_1","tool":"dws","label":"发送钉钉消息","target":"cid1","title":"上线","body":"已发布","cwd":"/w","agent":"claude","expires_at":123}"#
                .utf8)
        guard case .guardRequest(let request)? = ATMAgentEventDecoder.decodeMessage(line) else {
            return XCTFail("guard request did not decode")
        }
        XCTAssertEqual(request.id, "ap_1")
        XCTAssertEqual(request.target, "cid1")
        XCTAssertEqual(request.body, "已发布")
        XCTAssertTrue(request.isSupported)
    }

    func testAnUnknownTypeIsDroppedRatherThanGuessedAt() {
        let line = Data(#"{"v":1,"type":"something_new","id":"x"}"#.utf8)
        XCTAssertNil(ATMAgentEventDecoder.decodeMessage(line))
    }

    func testAGuardRequestFromANewerBuildIsRefused() {
        let line = Data(#"{"v":2,"type":"guard_request","id":"ap_1","tool":"dws"}"#.utf8)
        XCTAssertNil(
            ATMAgentEventDecoder.decodeMessage(line),
            "a newer envelope may mean something different by the same field names")
    }

    func testGuardRequestsSurviveFramingAcrossChunkBoundaries() {
        let first = Data(#"{"v":1,"type":"guard_request","id":"ap_1","tool":"dws"}"#.utf8)
        let second = Data(#"{"v":1,"type":"guard_request","id":"ap_2","tool":"a1"}"#.utf8)
        var buffer = Data()
        buffer.append(first)
        buffer.append(0x0A)
        buffer.append(second)  // no trailing newline: still in flight
        let split = ATMAgentEventDecoder.splitLines(buffer)
        XCTAssertEqual(split.lines.count, 1)
        XCTAssertFalse(split.remainder.isEmpty)
        guard case .guardRequest(let request)? = ATMAgentEventDecoder.decodeMessage(split.lines[0])
        else {
            return XCTFail("first line did not decode")
        }
        XCTAssertEqual(request.id, "ap_1")
    }

    // MARK: - List decoding

    func testApprovalListDecodesWhatTheCLIEmits() throws {
        let json = Data(
            """
            [{"id":"ap_1","dedup_key":"k","tool":"dws","rule_id":"chat-send",
              "real_bin":"/x/dws-atm-real","argv":["chat","message","send"],
              "cwd":"/w","env_agent":"claude","label":"发送钉钉消息",
              "preview_target":"cid1","preview_title":"上线","preview_body":"已发布到预发",
              "status":"pending","attach_count":2,"requested_at":100,"expires_at":1900,
              "effective_status":"pending"}]
            """.utf8)
        let approvals = try JSONDecoder().decode([ATMGuardApproval].self, from: json)
        XCTAssertEqual(approvals.count, 1)
        let approval = approvals[0]
        XCTAssertEqual(approval.actionLine, "发送钉钉消息 → cid1")
        XCTAssertTrue(approval.isPending)
        XCTAssertEqual(approval.attachCount, 2)
    }

    /// The CLI reports a pending row past its expiry as expired without writing.
    /// The UI has to believe that field, not `status`.
    func testEffectiveStatusWinsOverStoredStatus() throws {
        let json = Data(
            """
            [{"id":"ap_1","tool":"dws","status":"pending","effective_status":"expired",
              "requested_at":1,"expires_at":2}]
            """.utf8)
        let approval = try JSONDecoder().decode([ATMGuardApproval].self, from: json)[0]
        XCTAssertFalse(approval.isPending, "an expired request must not offer buttons")
        XCTAssertEqual(approval.state, "expired")
    }

    func testARunningRequestIsReportedAsUnknownOutcome() throws {
        let json = Data(
            #"[{"id":"ap_1","tool":"dws","status":"running","requested_at":1,"expires_at":2}]"#.utf8)
        let approval = try JSONDecoder().decode([ATMGuardApproval].self, from: json)[0]
        XCTAssertTrue(approval.isExecutingWithUnknownOutcome)
        XCTAssertFalse(approval.isPending, "nothing can be decided about a request already executing")
    }

    func testActionLineFallsBackToTheToolWhenThereIsNoLabel() throws {
        let json = Data(
            #"[{"id":"ap_1","tool":"dws","status":"pending","requested_at":1,"expires_at":2}]"#.utf8)
        let approval = try JSONDecoder().decode([ATMGuardApproval].self, from: json)[0]
        XCTAssertEqual(approval.actionLine, "dws")
    }

    // MARK: - Banner copy

    private func request(
        label: String? = "发送钉钉消息",
        target: String? = "cid1",
        title: String? = "上线",
        body: String? = "已发布到预发"
    ) -> ATMGuardRequest {
        ATMGuardRequest(
            version: 1, id: "ap_1", tool: "dws", label: label, target: target,
            title: title, body: body, cwd: "/w", agent: "claude", expiresAt: 1900)
    }

    /// Approving from the banner sends the message, so the banner must show what
    /// would go out — not a summary of it.
    func testBannerShowsWhoItReachesAndWhatItSays() {
        let payload = ATMGuardApprovalPayload.make(request: request())
        XCTAssertEqual(payload.title, "ATM · 待授权")
        XCTAssertEqual(payload.subtitle, "发送钉钉消息 → cid1")
        XCTAssertTrue(payload.body.contains("已发布到预发"), payload.body)
        XCTAssertTrue(payload.body.contains("上线"), "the message title was dropped: \(payload.body)")
    }

    func testBannerWithoutATargetStillNamesTheAction() {
        let payload = ATMGuardApprovalPayload.make(request: request(target: nil))
        XCTAssertEqual(payload.subtitle, "发送钉钉消息")
    }

    func testBannerWithNoReadableMessageSaysWhatApprovingWouldDo() {
        let payload = ATMGuardApprovalPayload.make(request: request(title: nil, body: nil))
        XCTAssertFalse(payload.body.isEmpty, "an empty banner asks for a decision with no information")
    }

    func testTheSameRequestKeepsOneStableBannerIdentifier() {
        let first = ATMNotificationManager.guardApprovalIdentifier("ap_1")
        let second = ATMNotificationManager.guardApprovalIdentifier("ap_1")
        XCTAssertEqual(first, second, "a changing identifier would stack banners instead of replacing")
        XCTAssertNotEqual(first, ATMNotificationManager.guardApprovalIdentifier("ap_2"))
    }

    // MARK: - Routing

    func testApprovalRoutingTakesPrecedenceOverASessionRoute() {
        // A banner carries both keys only if something regresses, but if it ever
        // does, an approval must not be routed to an agent's terminal — ATM is the
        // only place this decision can be made.
        let route = ATMNotificationRoute.from(userInfo: [
            "approval_id": "ap_1", "session_id": "s1",
        ])
        XCTAssertEqual(route, .guardApproval("ap_1"))
    }

    func testEmptyApprovalIDFallsThroughToTheOtherRoutes() {
        let route = ATMNotificationRoute.from(userInfo: ["approval_id": "", "session_id": "s1"])
        XCTAssertEqual(route, .agentSession("s1"))
    }

    func testTheTwoBannerActionsHaveDistinctIdentifiers() {
        XCTAssertNotEqual(ATMGuardApprovalActions.approve, ATMGuardApprovalActions.deny)
        XCTAssertFalse(ATMGuardApprovalActions.category.isEmpty)
    }

    // MARK: - Banner reconciliation

    private func approval(_ id: String, status: String = "pending") -> ATMGuardApproval {
        let json = Data(
            """
            {"id":"\(id)","tool":"dws","status":"\(status)","effective_status":"\(status)",
             "requested_at":1,"expires_at":9999}
            """.utf8)
        // Decoding rather than constructing keeps the fixture honest about which
        // fields the CLI actually supplies.
        return try! JSONDecoder().decode(ATMGuardApproval.self, from: json)
    }

    func testFirstLoadRaisesNoBanners() {
        let (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: nil, approvals: [approval("ap_1"), approval("ap_2")])
        XCTAssertTrue(diff.post.isEmpty, "launching with a backlog must not produce a pile of banners")
        XCTAssertTrue(diff.withdraw.isEmpty)
        XCTAssertEqual(notified, ["ap_1", "ap_2"])
    }

    func testANewPendingRequestIsRaisedOnce() {
        var (_, notified) = ATMGuardApprovalNotifyDiff.next(notified: nil, approvals: [])
        var diff: ATMGuardApprovalNotifyDiff
        (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notified, approvals: [approval("ap_1")])
        XCTAssertEqual(diff.post.map(\.id), ["ap_1"])

        // Same list again: already notified, so nothing more.
        (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notified, approvals: [approval("ap_1")])
        XCTAssertTrue(diff.post.isEmpty)
        XCTAssertTrue(diff.withdraw.isEmpty)
        XCTAssertEqual(notified, ["ap_1"])
    }

    /// Covers the case nothing pushes: the request was decided in a terminal, or it
    /// expired. A stale banner with live buttons is worse than no banner.
    func testABannerIsPulledBackWhenTheRequestStopsBeingPending() {
        var (_, notified) = ATMGuardApprovalNotifyDiff.next(notified: nil, approvals: [])
        (_, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notified, approvals: [approval("ap_1"), approval("ap_2")])

        var diff: ATMGuardApprovalNotifyDiff
        (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notified,
            approvals: [approval("ap_1", status: "denied"), approval("ap_2")])
        XCTAssertEqual(diff.withdraw, ["ap_1"])
        XCTAssertTrue(diff.post.isEmpty)
        XCTAssertEqual(notified, ["ap_2"])

        // Disappearing from the list entirely counts too.
        (diff, notified) = ATMGuardApprovalNotifyDiff.next(notified: notified, approvals: [])
        XCTAssertEqual(diff.withdraw, ["ap_2"])
        XCTAssertTrue(notified.isEmpty)
    }

    func testARunningRequestNeitherRaisesNorKeepsABanner() {
        var (_, notified) = ATMGuardApprovalNotifyDiff.next(notified: nil, approvals: [])
        var diff: ATMGuardApprovalNotifyDiff
        (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notified, approvals: [approval("ap_1", status: "running")])
        XCTAssertTrue(diff.post.isEmpty, "a request already executing offers no decision")
        XCTAssertTrue(notified.isEmpty)
    }

    // MARK: - Command construction

    func testDecisionArgvNamesThePanelAsTheDecidingSurface() {
        XCTAssertEqual(
            ATMCommandBuilder.guardDecision(id: "ap_1", approve: true),
            ["guard", "approve", "ap_1", "--by", "panel", "--json"])
        XCTAssertEqual(
            ATMCommandBuilder.guardDecision(id: "ap_1", approve: false),
            ["guard", "deny", "ap_1", "--by", "panel", "--json"])
    }

    func testListArgvAsksOnlyForPendingRequests() {
        XCTAssertEqual(
            ATMCommandBuilder.guardList(),
            ["guard", "list", "--status", "pending", "--limit", "50", "--json"])
    }

    /// Approving runs the gated command. The default 15s ceiling would terminate a
    /// slow send partway and report failure for a message that in fact went out.
    func testApprovingGetsALongerTimeoutThanAnOrdinaryCommand() {
        let approveTimeout = ATMCommandPolicy.timeout(for: ["guard", "approve", "ap_1"])
        XCTAssertGreaterThan(approveTimeout, ATMCommandPolicy.timeout(for: ["guard", "list"]))
        XCTAssertGreaterThanOrEqual(approveTimeout, 60)
    }
}
