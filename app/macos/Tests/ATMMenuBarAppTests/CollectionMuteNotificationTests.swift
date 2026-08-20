import XCTest

@testable import ATMMenuBarApp

/// Muting a source is a promise about one thing only — no banner — so these tests
/// pin both halves: a muted source's run raises nothing, and everything else
/// about the source is unchanged.
final class CollectionMuteNotificationTests: XCTestCase {
    private func makeOverview(mutedSecondSource: Bool) throws -> ATMCollectionOverview {
        let muted = mutedSecondSource ? "true" : "false"
        let data = Data(
            """
            {
              "enabled":true,"interval_minutes":5,"lookback_minutes":60,
              "model":"deepseek-v4-flash","connector_health":[],
              "summary":{"sources":2,"enabled_sources":2,"fetched_today":4,"created_today":2,
                         "appended_today":0,"insight_today":1,"ignored_today":0,"failed_today":0,
                         "followups":0,"followups_closed":0,"retry_stopped":0,"unread_count":3},
              "sources":[{"id":"cs-loud","connector":"example","kind":"group","external_id":"g1",
                          "name":"要紧群","priority":"P1","enabled":true,
                          "created_at":1,"updated_at":1},
                         {"id":"cs-quiet","connector":"example","kind":"group","external_id":"g2",
                          "name":"吵闹群","priority":"P2","enabled":true,"muted":\(muted),
                          "created_at":1,"updated_at":1}],
              "runs":[{"id":"cr-loud","connector":"example","source_id":"cs-loud",
                       "status":"succeeded","started_at":10,"finished_at":11,
                       "fetched_count":2,"analyzed_count":2,"created_count":1,"appended_count":0,
                       "insight_count":0,"ignored_count":0,"failed_count":0},
                      {"id":"cr-quiet","connector":"example","source_id":"cs-quiet",
                       "status":"succeeded","started_at":12,"finished_at":13,
                       "fetched_count":2,"analyzed_count":2,"created_count":1,"appended_count":0,
                       "insight_count":1,"ignored_count":0,"failed_count":0}],
              "items":[],"digests":[]
            }
            """.utf8
        )
        return try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
    }

    func testMutedSourceIsLeftOutOfTheBannerCounts() throws {
        let overview = try makeOverview(mutedSecondSource: true)
        XCTAssertEqual(overview.sources.first?.notifiesDesktop, true)
        XCTAssertEqual(overview.sources.last?.notifiesDesktop, false)
        // Muting changes nothing about collection or unread: those are the signals
        // that still have to say the work arrived.
        XCTAssertEqual(overview.sources.last?.enabled, true)
        XCTAssertEqual(overview.summary.unreadCount, 3)

        let payload = ATMCollectionNotificationPayload.make(
            runs: overview.runs,
            sources: overview.sources
        )
        XCTAssertEqual(payload?.body, "新增 1 · 补充 0 · 结论 0 · 失败 0")
    }

    func testAllMutedRunsRaiseNoBannerAtAll() throws {
        let overview = try makeOverview(mutedSecondSource: true)
        let quietRuns = overview.runs.filter { $0.sourceID == "cs-quiet" }
        XCTAssertNil(
            ATMCollectionNotificationPayload.make(runs: quietRuns, sources: overview.sources)
        )
    }

    func testUnmutedSourcesStillCountAndUnknownSourcesStillNotify() throws {
        let overview = try makeOverview(mutedSecondSource: false)
        // A source with no `muted` key at all is an older database, not a mute.
        XCTAssertEqual(overview.sources.first?.notifiesDesktop, true)
        XCTAssertEqual(
            ATMCollectionNotificationPayload.make(runs: overview.runs, sources: overview.sources)?
                .body,
            "新增 2 · 补充 0 · 结论 1 · 失败 0"
        )
        // A run whose source is gone (deleted after the run, or never scoped) must
        // still be announced: "unknown" is not "muted".
        let muted = try makeOverview(mutedSecondSource: true)
        XCTAssertEqual(
            ATMCollectionNotificationPayload.make(runs: muted.runs, sources: [])?.body,
            "新增 2 · 补充 0 · 结论 1 · 失败 0"
        )
    }

    func testMuteUsesItsOwnTypedStateMethod() {
        XCTAssertEqual(ATMCollectionIPCCommand.setSourceMuted.verb, "collect.source.muted")
        XCTAssertNotEqual(
            ATMCollectionIPCCommand.setSourceMuted.verb,
            ATMCollectionIPCCommand.setSourceEnabled.verb
        )
    }

    func testNewResultNotificationNamesContentAndLocatesItsRecord() throws {
        let data = Data(
            """
            {
              "enabled":true,"interval_minutes":5,"lookback_minutes":60,"model":"test",
              "connector_health":[],
              "summary":{"sources":1,"enabled_sources":1,"fetched_today":2,"created_today":1,
                         "appended_today":1,"insight_today":0,"ignored_today":0,"failed_today":0,
                         "unread_count":2},
              "sources":[{"id":"cs-product","connector":"example","kind":"group",
                          "external_id":"g1","name":"产品反馈群","priority":"P1","enabled":true,
                          "created_at":1,"updated_at":1}],
              "runs":[{"id":"cr-new","connector":"example","source_id":"cs-product",
                       "status":"succeeded","started_at":100,"finished_at":110,
                       "fetched_count":2,"analyzed_count":2,"created_count":1,"appended_count":1,
                       "insight_count":0,"ignored_count":0,"failed_count":0}],
              "items":[{"id":"ci-create","source_id":"cs-product","connector":"example",
                        "fingerprint":"fp1","message_ids":["m1"],"action":"create",
                        "title":"通知展示具体的收集内容","todo_id":"t1","status":"processed",
                        "created_at":101,"updated_at":102},
                       {"id":"ci-append","source_id":"cs-product","connector":"example",
                        "fingerprint":"fp2","message_ids":["m2"],"action":"append",
                        "title":"点击后定位到对应记录","todo_id":"t1","status":"processed",
                        "created_at":103,"updated_at":104}],
              "digests":[]
            }
            """.utf8
        )
        let overview = try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
        let payloads = ATMCollectionNotificationPayload.makeResults(
            runs: overview.runs,
            items: overview.items,
            sources: overview.sources
        )

        XCTAssertEqual(payloads.count, 2)
        XCTAssertEqual(payloads[0].subtitle, "产品反馈群 · 新任务")
        XCTAssertEqual(payloads[0].body, "通知展示具体的收集内容")
        XCTAssertEqual(payloads[0].itemID, "ci-create")
        XCTAssertEqual(payloads[1].subtitle, "产品反馈群 · 任务补充")
        XCTAssertEqual(payloads[1].body, "点击后定位到对应记录")
        XCTAssertEqual(payloads[1].itemID, "ci-create")
    }

    func testAppendNotificationDoesNotLocateAnArchivedCreateRecord() throws {
        let data = Data(
            """
            {
              "enabled":true,"interval_minutes":5,"lookback_minutes":60,"model":"test",
              "connector_health":[],
              "summary":{"sources":1,"enabled_sources":1,"fetched_today":1,"created_today":0,
                         "appended_today":1,"insight_today":0,"ignored_today":0,"failed_today":0,
                         "unread_count":1},
              "sources":[{"id":"cs-product","connector":"example","kind":"group",
                          "external_id":"g1","name":"产品反馈群","priority":"P1","enabled":true,
                          "created_at":1,"updated_at":1}],
              "runs":[{"id":"cr-new","connector":"example","source_id":"cs-product",
                       "status":"succeeded","started_at":100,"finished_at":110,
                       "fetched_count":1,"analyzed_count":1,"created_count":0,"appended_count":1,
                       "insight_count":0,"ignored_count":0,"failed_count":0}],
              "items":[{"id":"ci-archived-create","source_id":"cs-product","connector":"example",
                        "fingerprint":"fp1","message_ids":["m1"],"action":"create",
                        "title":"已经了结的主记录","todo_id":"t1","status":"processed",
                        "archived_at":50,"created_at":1,"updated_at":50},
                       {"id":"ci-active-append","source_id":"cs-product","connector":"example",
                        "fingerprint":"fp2","message_ids":["m2"],"action":"append",
                        "title":"后来收到的新补充","todo_id":"t1","status":"processed",
                        "created_at":103,"updated_at":104}],
              "digests":[]
            }
            """.utf8
        )
        let overview = try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
        let payloads = ATMCollectionNotificationPayload.makeResults(
            runs: overview.runs,
            items: overview.items,
            sources: overview.sources
        )

        XCTAssertEqual(payloads.count, 1)
        XCTAssertEqual(payloads[0].itemID, "ci-active-append")
    }
}
