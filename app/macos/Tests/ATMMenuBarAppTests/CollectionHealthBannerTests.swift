import XCTest

@testable import ATMMenuBarApp

/// A banner over the Collection workspace is a card you have to dismiss by hand, so
/// it outlives the condition it describes. These pin which conditions earn one.
final class CollectionHealthBannerTests: XCTestCase {

    /// The health objects go through the real decoder — that is where the new fields
    /// live and where a rename on the Go side would bite. The overview around them is
    /// built directly, so the fixture stays about health rather than about every
    /// unrelated field `collect status` happens to emit.
    private func overview(_ healthJSON: String) throws -> ATMCollectionOverview {
        let health = try JSONDecoder().decode(
            [ATMCollectionConnectorHealth].self, from: Data(healthJSON.utf8))
        return ATMCollectionOverview(
            enabled: true, intervalMinutes: 5, lookbackMinutes: 60, model: "m",
            connectorHealth: health, summary: .empty,
            sources: [], runs: [], items: [], digests: [])
    }

    /// The case that started this: one business error between successes. The
    /// connector is already retrying, and it succeeded again five minutes later.
    func testAFlakyConnectorRaisesNoBanner() throws {
        let overview = try overview(
            """
            [{"connector":"dingtalk","status":"flaky","error":"business error: success=false",
              "checked_at":100,"consecutive_failures":1,"recent_runs":23,"recent_failures":2}]
            """)
        XCTAssertNil(
            ATMCollectionWorkspaceNotice.banner(for: overview),
            "a connector that recovers on its own must not raise a card")
        let health = overview.connectorHealth[0]
        XCTAssertFalse(health.needsAttention)
        // It is not hidden either — the rate is stated where the connector is listed.
        XCTAssertEqual(health.transientNote, "最近 23 次里失败 2 次，会自动重试")
        XCTAssertEqual(health.statusLabel, "偶发失败")
    }

    func testARecoveredConnectorRaisesNoBannerButSaysItHiccupped() throws {
        let overview = try overview(
            """
            [{"connector":"dingtalk","status":"ready","checked_at":100,
              "recent_runs":20,"recent_failures":1}]
            """)
        XCTAssertNil(ATMCollectionWorkspaceNotice.banner(for: overview))
        XCTAssertEqual(overview.connectorHealth[0].transientNote, "最近 20 次里失败过 1 次，已恢复")
    }

    func testACleanConnectorSaysNothingAtAll() throws {
        let overview = try overview(
            #"[{"connector":"dingtalk","status":"ready","checked_at":100,"recent_runs":20}]"#)
        XCTAssertNil(ATMCollectionWorkspaceNotice.banner(for: overview))
        XCTAssertNil(overview.connectorHealth[0].transientNote)
    }

    func testAConnectorThatStoppedRecoveringRaisesABanner() throws {
        let overview = try overview(
            """
            [{"connector":"dingtalk","status":"error","error":"business error",
              "checked_at":100,"consecutive_failures":3,"recent_runs":10,"recent_failures":3}]
            """)
        let banner = ATMCollectionWorkspaceNotice.banner(for: overview)
        XCTAssertNotNil(banner)
        XCTAssertTrue(banner?.contains("dingtalk") == true, banner ?? "")
        XCTAssertTrue(banner?.contains("business error") == true, banner ?? "")
        XCTAssertTrue(overview.connectorHealth[0].needsAttention)
    }

    /// A login that expired will not fix itself, so it earns a banner on the first
    /// failure rather than after a streak.
    func testAnExpiredLoginRaisesABannerImmediately() throws {
        for status in ["auth_required", "permission_required"] {
            let overview = try overview(
                """
                [{"connector":"dingtalk","status":"\(status)","error":"not_authenticated",
                  "checked_at":100,"consecutive_failures":1,"recent_runs":20,"recent_failures":1}]
                """)
            XCTAssertNotNil(
                ATMCollectionWorkspaceNotice.banner(for: overview),
                "\(status) must not wait for a second failure")
            XCTAssertNil(
                overview.connectorHealth[0].transientNote,
                "\(status) is not a hiccup and must not be described as one")
        }
    }

    func testAnUnverifiedConnectorRaisesNoBanner() throws {
        let overview = try overview(
            #"[{"connector":"slack","status":"not_checked"}]"#)
        XCTAssertNil(ATMCollectionWorkspaceNotice.banner(for: overview))
        XCTAssertTrue(overview.connectorHealth[0].isUnverified)
    }

    /// One broken connector must not hide another, and a working one must not be
    /// dragged into the banner by its neighbour.
    func testOnlyTheBrokenConnectorsAppearInTheBanner() throws {
        let overview = try overview(
            """
            [{"connector":"dingtalk","status":"ready","checked_at":100,"recent_runs":20},
             {"connector":"slack","status":"error","error":"boom","checked_at":100,
              "consecutive_failures":4,"recent_runs":4,"recent_failures":4}]
            """)
        let banner = ATMCollectionWorkspaceNotice.banner(for: overview)
        XCTAssertTrue(banner?.contains("slack") == true, banner ?? "")
        XCTAssertFalse(banner?.contains("dingtalk") == true, banner ?? "")
    }

    /// The fields are optional so an older CLI's output still decodes; absent must
    /// not be read as a failure streak.
    func testAbsentCountsDecodeAndMeanNoStreak() throws {
        let overview = try overview(
            #"[{"connector":"dingtalk","status":"ready","checked_at":100}]"#)
        let health = overview.connectorHealth[0]
        XCTAssertNil(health.consecutiveFailures)
        XCTAssertNil(health.transientNote)
        XCTAssertFalse(health.needsAttention)
    }
}
