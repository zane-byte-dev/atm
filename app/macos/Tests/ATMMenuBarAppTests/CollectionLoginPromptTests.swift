import XCTest

@testable import ATMMenuBarApp

/// An expired login is the one collection failure that waits for a person. These
/// pin the two halves of that: the workspace offers the action, and the same
/// outage stops arriving as a stream of identical failure banners.
final class CollectionLoginPromptTests: XCTestCase {
    private func overview(connectorHealth: String, runs: String = "[]") throws -> ATMCollectionOverview {
        let data = Data(
            """
            {
              "enabled":true,"interval_minutes":5,"lookback_minutes":60,"model":"test",
              "connector_health":\(connectorHealth),
              "summary":{"sources":1,"enabled_sources":1,"fetched_today":0,"created_today":0,
                         "appended_today":0,"insight_today":0,"ignored_today":0,"failed_today":1,
                         "unread_count":0},
              "sources":[{"id":"cs-product","connector":"dingtalk","kind":"group",
                          "external_id":"g1","name":"产品反馈群","priority":"P1","enabled":true,
                          "created_at":1,"updated_at":1}],
              "runs":\(runs),"items":[],"digests":[]
            }
            """.utf8
        )
        return try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
    }

    func testAStuckLoginOffersTheCommandThatEndsIt() throws {
        let overview = try overview(
            connectorHealth: """
            [{"connector":"dingtalk","status":"auth_required","error":"未登录",
              "checked_at":100,"consecutive_failures":1,"recent_runs":20,"recent_failures":1,
              "login_command":"/Users/x/bin/dws auth login"}]
            """)
        XCTAssertEqual(
            ATMCollectionWorkspaceNotice.loginPrompt(for: overview),
            ATMCollectionLoginPrompt(connector: "dingtalk", command: "/Users/x/bin/dws auth login")
        )
        XCTAssertTrue(overview.connectorHealth[0].needsCredentialAction)
    }

    /// Most connectors declare no login command, and a button that runs nothing is
    /// worse than the banner alone.
    func testAConnectorWithoutALoginCommandOffersNoButton() throws {
        let overview = try overview(
            connectorHealth: """
            [{"connector":"dingtalk","status":"auth_required","error":"未登录","checked_at":100,
              "consecutive_failures":1,"recent_runs":20,"recent_failures":1}]
            """)
        XCTAssertNil(ATMCollectionWorkspaceNotice.loginPrompt(for: overview))
        // The banner itself still has to appear: the outage is real either way.
        XCTAssertNotNil(ATMCollectionWorkspaceNotice.banner(for: overview))
    }

    /// A connector that is merely retrying must not be offered a login: nothing
    /// about its credential is in question. Neither is a permission problem, which
    /// has been per-source in practice and is not fixed by logging in again.
    func testOnlyAnExpiredLoginIsOfferedALogin() throws {
        for status in ["flaky", "permission_required", "error"] {
            let overview = try overview(
                connectorHealth: """
                [{"connector":"dingtalk","status":"\(status)","error":"business error",
                  "checked_at":100,"consecutive_failures":1,"recent_runs":20,"recent_failures":1,
                  "login_command":"/Users/x/bin/dws auth login"}]
                """)
            XCTAssertNil(ATMCollectionWorkspaceNotice.loginPrompt(for: overview), status)
            XCTAssertFalse(overview.connectorHealth[0].needsCredentialAction, status)
        }
    }

    /// The failure repeats every interval by design; the news does not. Before this,
    /// one outage sent a "收集失败" banner per source per cycle for as long as it
    /// lasted, which is how people learned to ignore them.
    func testACredentialOutageDoesNotAlsoSendGenericFailureBanners() throws {
        let runs = """
        [{"id":"cr-auth","connector":"dingtalk","source_id":"cs-product","status":"failed",
          "started_at":100,"finished_at":101,"fetched_count":0,"analyzed_count":0,
          "created_count":0,"appended_count":0,"insight_count":0,"ignored_count":0,
          "failed_count":1,"error":"未登录，请先执行 dws auth login"}]
        """
        let overview = try overview(
            connectorHealth: """
            [{"connector":"dingtalk","status":"auth_required","error":"未登录","checked_at":101,
              "consecutive_failures":1,"recent_runs":20,"recent_failures":1,
              "login_command":"/Users/x/bin/dws auth login"}]
            """,
            runs: runs)
        XCTAssertTrue(
            ATMCollectionNotificationPayload.makeResults(
                runs: overview.runs,
                items: overview.items,
                sources: overview.sources,
                credentialBlockedConnectors: ["dingtalk"]
            ).isEmpty)
        // Any other failure is still announced — this suppression is about the one
        // banner that replaces it, not about going quiet.
        XCTAssertFalse(
            ATMCollectionNotificationPayload.makeResults(
                runs: overview.runs,
                items: overview.items,
                sources: overview.sources
            ).isEmpty)
    }

    func testTheLoginBannerBodyCarriesTheConnectorsOwnComplaint() {
        XCTAssertEqual(
            ATMCollectionNotificationPayload.loginBody(detail: "  未登录，请先执行 dws auth login "),
            "未登录，请先执行 dws auth login")
        XCTAssertEqual(
            ATMCollectionNotificationPayload.loginBody(detail: "   "),
            "收集已暂停，重新登录后继续。")
    }

    /// A path with a space is ordinary here; a quote in the command would otherwise
    /// end the AppleScript literal early and run something else.
    func testTheLoginCommandSurvivesAppleScriptQuoting() {
        let script = ATMConnectorLoginLauncher.terminalScript(for: #"/Users/x/my bin/dws "auth" login"#)
        XCTAssertTrue(script.contains(#"do script "/Users/x/my bin/dws \"auth\" login""#), script)
        XCTAssertTrue(script.contains("com.apple.Terminal"), script)
        XCTAssertEqual(
            ATMConnectorLoginLauncher.appleScriptLiteral(#"a\b"#),
            #"a\\b"#)
    }
}
