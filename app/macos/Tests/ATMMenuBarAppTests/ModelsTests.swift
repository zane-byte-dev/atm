import Combine
import XCTest
@testable import ATMMenuBarApp

final class ModelsTests: XCTestCase {
    private func encodedObject<Value: Encodable>(_ value: Value) throws -> [String: Any] {
        let data = try JSONEncoder().encode(value)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    func testAIDayContractAndCommands() throws {
        let data = Data("""
        {"schema_version":3,"day":"2026-08-15","state":"ready","timezone":"Asia/Shanghai",
         "features":{"session_count":2,"event_count":8,"turn_count":3,"tool_calls":4,"source_count":2,
          "input_tokens":100,"output_tokens":50,"cache_create_tokens":10,"cache_read_tokens":20,
          "generation_seconds":6,"active_seconds":6,"foreground_seconds":6,"background_seconds":0,
          "semantic_counts":{"directive":2},"modality_counts":{"code":4}},
         "concept":{"id":"code_architect","title":"代码架构师","explanation":"代码与工具构成主旋律。",
          "tags":["grid","growth"],"evidence":[{"metric":"code_events","value":4,"unit":"events"},
          {"metric":"tool_calls","value":4,"unit":"calls"}],"confidence":0.72,
          "evidence_strength":0.45,"origin":"user_corrected","computed_id":"deep_collaboration",
          "computed_title":"深度共创"},
         "badge":{"id":"code_architect","name":"代码架构师","description":"代码与工具构成主旋律。",
          "family":"grid","kind":"growth","level":1,"unlocked":true,"qualified_days":1,
          "qualified_dates":["2026-08-15"],"next_level_days":7,"progress":0,"score":0.88,
          "evidence":[{"metric":"code_events","value":4,"unit":"events"}]},
         "candidates":[{"id":"deep_collaboration","name":"深度共创","description":"多轮互动。",
          "family":"crystal","kind":"growth","level":0,"unlocked":false,"qualified_days":0,
          "qualified_dates":[],"next_level_days":3,"progress":0,"score":0.5}],
         "baseline_days":12,"provisional":true,
         "coverage":{"complete":false,"expected_sources":3,"present_sources":2,
          "missing_sources":["claude"],"data_through":1755400000},
         "feedback":{"day":"2026-08-15","verdict":"corrected","corrected_badge_id":"code_architect","updated_at":2},
         "generated_at":1,"engine_version":3}
        """.utf8)
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let result = try decoder.decode(ATMAIDayResult.self, from: data)
        XCTAssertEqual(result.badge?.id, "code_architect")
        XCTAssertEqual(result.features.totalTokens, 180)
        // Cache reads track context size, not work, so they stay out of the figure
        // shown as "how much happened today".
        XCTAssertEqual(result.features.workTokens, 160)
        XCTAssertEqual(result.badge?.qualifiedDates, ["2026-08-15"])
        XCTAssertTrue(result.isProvisional)
        XCTAssertEqual(result.coverage?.complete, false)
        XCTAssertEqual(result.coverage?.missingSources, ["claude"])
        XCTAssertEqual(result.feedback?.verdict, "corrected")
        XCTAssertEqual(result.candidates?.count, 1)
        // A correction records the user's judgement without erasing the engine's.
        XCTAssertTrue(result.concept?.isUserCorrected == true)
        XCTAssertEqual(result.concept?.computedTitle, "深度共创")
        XCTAssertEqual(result.concept?.strength, 0.45)
        XCTAssertLessThan(result.concept?.confidence ?? 1, 1)
    }

    func testAIDayUsesOnlyTheEightTypedIPCMethods() {
        XCTAssertEqual(
            [
                ATMAIDayCommand.snapshot.arguments,
                ATMAIDayCommand.show.arguments,
                ATMAIDayCommand.feedback.arguments,
                ATMAIDayCommand.setSource.arguments,
                ATMAIDayCommand.deleteSource.arguments,
                ATMAIDayCommand.setPrivacy.arguments,
                ATMAIDayCommand.deleteData.arguments,
                ATMAIDayCommand.exportData.arguments,
            ],
            [
                ["_ipc", "day.snapshot"],
                ["_ipc", "day.show"],
                ["_ipc", "day.feedback"],
                ["_ipc", "day.source.set"],
                ["_ipc", "day.source.delete"],
                ["_ipc", "day.privacy.set"],
                ["_ipc", "day.data.delete"],
                ["_ipc", "day.data.export"],
            ]
        )
        XCTAssertEqual(ATMAIDayCommand.snapshot.timeout, 60)
        XCTAssertEqual(ATMAIDayCommand.feedback.timeout, 60)
        XCTAssertEqual(ATMAIDayCommand.exportData.timeout, 60)
    }

    func testAIDayIPCRequestsUseTheGoContractKeysAndOmitAbsentFields() throws {
        let feedback = try encodedObject(ATMAIDayFeedbackRequest(
            day: "2026-08-15",
            verdict: "corrected",
            correctedBadgeID: "code_architect",
            semanticLabels: ["directive"]
        ))
        XCTAssertEqual(Set(feedback.keys), [
            "day", "verdict", "corrected_badge_id", "semantic_labels",
        ])
        XCTAssertEqual(feedback["corrected_badge_id"] as? String, "code_architect")

        let clear = try encodedObject(ATMAIDayFeedbackRequest(day: "2026-08-15", clear: true))
        XCTAssertEqual(Set(clear.keys), ["day", "clear"])
        XCTAssertEqual(clear["clear"] as? Bool, true)

        let source = try encodedObject(ATMAIDaySourceSetRequest(
            source: "codex",
            enabled: false,
            semanticEnabled: false
        ))
        XCTAssertEqual(Set(source.keys), ["source", "enabled", "semantic_enabled"])
        XCTAssertEqual(source["semantic_enabled"] as? Bool, false)

        let privacy = try encodedObject(ATMAIDayPrivacyPatch(retentionDays: 30))
        XCTAssertEqual(Set(privacy.keys), ["retention_days"])
        XCTAssertNil(privacy["semantic_enabled"])

        let deletion = try encodedObject(ATMAIDayDeleteRequest(all: true, confirmed: true))
        XCTAssertEqual(Set(deletion.keys), ["all", "confirmed"])
        XCTAssertEqual(deletion["confirmed"] as? Bool, true)

        let sourceDeletion = try encodedObject(
            ATMAIDaySourceDeleteRequest(source: "codex", confirmed: true)
        )
        XCTAssertEqual(Set(sourceDeletion.keys), ["source", "confirmed"])
    }

    /// The app ships separately from `atm`, so an older CLI must degrade to the
    /// previous contract rather than fail the whole pane.
    func testAIDayResultDecodesWithoutTheNewProvenanceFields() throws {
        let data = Data("""
        {"schema_version":2,"day":"2026-08-15","state":"ready","timezone":"Asia/Shanghai",
         "features":{"session_count":1,"event_count":2,"turn_count":2,"tool_calls":0,"source_count":1,
          "input_tokens":10,"output_tokens":5,"cache_create_tokens":0,"cache_read_tokens":0,
          "generation_seconds":1,"active_seconds":1,"foreground_seconds":1,"background_seconds":0,
          "semantic_counts":{},"modality_counts":{}},
         "concept":{"id":"deep_collaboration","title":"深度共创","explanation":"多轮互动。",
          "tags":["crystal","growth"],"evidence":[],"confidence":0.6},
         "baseline_days":0,"generated_at":1,"engine_version":2}
        """.utf8)
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        let result = try decoder.decode(ATMAIDayResult.self, from: data)
        XCTAssertFalse(result.isProvisional)
        XCTAssertFalse(result.concept?.isUserCorrected == true)
        XCTAssertEqual(result.concept?.strength, 0)
        XCTAssertNil(result.coverage)
        XCTAssertNil(result.feedback)
    }

    func testAIDayTokenFormattingIsReadable() {
        XCTAssertEqual(ATMAIDayFormat.tokens(17_214_880), "17.2M")
        XCTAssertEqual(ATMAIDayFormat.tokens(2_400), "2.4K")
        XCTAssertEqual(ATMAIDayFormat.tokens(860), "860")
    }

    /// The timer must tick faster than the staleness threshold. Ticking at exactly
    /// the threshold looks equivalent but always misses: `lastRefreshed` is stamped
    /// when the first load finishes, seconds after the timer starts, so the tick at
    /// 420s is a few seconds short, skips, and the first automatic refresh lands at
    /// roughly 14 minutes instead of 7.
    func testAutoRefreshTickIsShorterThanTheStalenessThreshold() {
        XCTAssertLessThan(ATMAIDayStore.refreshCheckInterval, ATMAIDayStore.autoRefreshInterval)
        let loaded = Date()
        // The moment a tick would fire if the two were equal.
        let atThreshold = loaded.addingTimeInterval(ATMAIDayStore.autoRefreshInterval - 5)
        XCTAssertFalse(ATMAIDayStore.shouldRefresh(lastRefreshed: loaded, now: atThreshold))
        // With a shorter tick, the next check after that is still well inside one
        // threshold period, so the refresh is at most one tick late.
        let nextTick = atThreshold.addingTimeInterval(ATMAIDayStore.refreshCheckInterval)
        XCTAssertTrue(ATMAIDayStore.shouldRefresh(lastRefreshed: loaded, now: nextTick))
        XCTAssertLessThan(
            nextTick.timeIntervalSince(loaded),
            ATMAIDayStore.autoRefreshInterval * 2,
            "the first automatic refresh must not slip to two full periods"
        )
        XCTAssertTrue(ATMAIDayStore.shouldRefresh(lastRefreshed: nil, now: loaded))
    }

    /// A metric with no label falls through to its raw internal name. Both maps
    /// are pinned: the share card used to keep its own English copy, so a metric
    /// added to the engine could be readable in the app and raw on the card.
    func testEveryEngineMetricHasALabel() {
        for metric in ATMAIDayLabels.knownMetrics {
            XCTAssertTrue(
                ATMAIDayLabels.hasLabels(for: metric),
                "metric \(metric) is missing a label in the app or on the share card, and would be shown raw"
            )
        }
        XCTAssertEqual(ATMAIDayLabels.evidence("generation_seconds"), "生成秒数")
        XCTAssertEqual(ATMAIDayLabels.evidence("work_tokens"), "有效 Token")
        XCTAssertEqual(ATMAIDayLabels.compact("generation_seconds"), "seconds")
        XCTAssertEqual(ATMAIDayLabels.compact("work_tokens"), "tokens")
    }

    func testEvidenceValuesFormatTokensAndPercentages() {
        let tokens = ATMAIDayEvidence(metric: "work_tokens", value: 2_412_000, unit: "tokens", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.value(tokens), "2.4M")
        let share = ATMAIDayEvidence(metric: "acceptance_share", value: 25, unit: "%", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.value(share), "25%")
        let plain = ATMAIDayEvidence(metric: "turn_count", value: 41, unit: "turns", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.value(plain), "41 turns")
    }

    /// 分享卡上单位由下面那行标签说，数字本身不带单位；百分号必须留着。
    func testCompactEvidenceValuesDropUnitsButKeepPercentSign() {
        let tokens = ATMAIDayEvidence(metric: "work_tokens", value: 2_412_000, unit: "tokens", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.compactValue(tokens), "2.4M")
        let share = ATMAIDayEvidence(metric: "acceptance_share", value: 25, unit: "%", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.compactValue(share), "25%")
        let plain = ATMAIDayEvidence(metric: "turn_count", value: 41, unit: "turns", comparison: nil)
        XCTAssertEqual(ATMAIDayLabels.compactValue(plain), "41")
    }

    func testComparisonSpellsOutThePercentileWindow() {
        XCTAssertEqual(ATMAIDayLabels.comparison("recent_p75"), "近 30 日 P75")
        XCTAssertEqual(ATMAIDayLabels.comparison("baseline"), "baseline")
    }

    func testGlyphNormalizationGivesEveryAssetTheSameOpticalSizeAndCenter() {
        // 占画布 50%、偏左上的图形。
        let small = ATMAIDayGlyphMetrics(center: CGPoint(x: 0.40, y: 0.30), extent: 0.50)
        // 占画布 80%、几乎居中的图形。
        let large = ATMAIDayGlyphMetrics(center: CGPoint(x: 0.51, y: 0.55), extent: 0.80)
        let size: CGFloat = 200
        let target: CGFloat = 0.64

        // 画布放大到让图形长边正好占 target：小图形要放得更大。
        XCTAssertEqual(small.canvasSide(in: size, target: target), 256, accuracy: 0.01)
        XCTAssertEqual(large.canvasSide(in: size, target: target), 160, accuracy: 0.01)

        // 归一化后两者的图形长边都是 128pt = 200 × 0.64。
        for metrics in [small, large] {
            let rendered = metrics.extent * metrics.canvasSide(in: size, target: target)
            XCTAssertEqual(rendered, size * target, accuracy: 0.01)
        }

        // 位移把图形光学中心搬到正中：偏左上的往右下推，几乎居中的几乎不动。
        let smallShift = small.canvasOffset(in: size, target: target)
        XCTAssertEqual(smallShift.width, 25.6, accuracy: 0.01)
        XCTAssertEqual(smallShift.height, 51.2, accuracy: 0.01)
        let largeShift = large.canvasOffset(in: size, target: target)
        XCTAssertEqual(largeShift.width, -1.6, accuracy: 0.01)
        XCTAssertEqual(largeShift.height, -8.0, accuracy: 0.01)
    }

    func testGlyphNormalizationFallsBackOnAnEmptyCanvas() {
        // 几乎全透明的图会把缩放推到无穷大，宁可原样画。
        let blank = ATMAIDayGlyphMetrics(center: CGPoint(x: 0.5, y: 0.5), extent: 0)
        XCTAssertEqual(blank.canvasSide(in: 120, target: 0.64), 120, accuracy: 0.01)
        XCTAssertEqual(ATMAIDayGlyphMetrics.untrimmed.canvasSide(in: 120, target: 1), 120, accuracy: 0.01)
    }

    /// 拴住测量的 y 方向。位图上下文的行序搞反不会「少校正一点」——它会把偏移量加倍，
    /// 而只比较「校正后各图是否一致」的测试发现不了，因为量和校正都在同一个翻转空间里。
    /// 所以这里对的是外部独立量出的包围盒（PIL，alpha > 8，362×362 画布）。
    func testBadgeGlyphMetricsMatchAnIndependentMeasurement() throws {
        // first_draft_accepted 的图形明显靠上：包围盒 (67, 2, 270, 204)，从顶边算起。
        let high = try XCTUnwrap(ATMAIDayBadgeAssets.asset(named: "first_draft_accepted")).metrics
        XCTAssertEqual(high.center.y, 0.284, accuracy: 0.02, "图形靠上，光学中心必须在 0.5 以上方")
        XCTAssertEqual(high.center.x, 0.466, accuracy: 0.02)
        XCTAssertEqual(high.extent, 0.561, accuracy: 0.02)

        // deep_collaboration 反过来，明显靠下：包围盒 (56, 158, 337, 302)。
        let low = try XCTUnwrap(ATMAIDayBadgeAssets.asset(named: "deep_collaboration")).metrics
        XCTAssertEqual(low.center.y, 0.635, accuracy: 0.02, "图形靠下，光学中心必须在 0.5 以下方")
        XCTAssertEqual(low.extent, 0.776, accuracy: 0.02)

        XCTAssertLessThan(high.center.y, low.center.y, "两枚徽章的垂直方向不能被量反")
    }

    /// 这一组图原本的光学大小从占画布 49% 到 80%（差 1.6 倍），垂直中心跨了近三分之一个
    /// 底座高。归一化之后必须全部对齐——这就是「图标看起来都居中却又都差一点」的修复。
    func testEveryBadgeAssetNormalizesToTheSameRenderedGlyph() throws {
        let ids = [
            "autopilot", "deep_collaboration", "model_conductor", "visual_director",
            "code_architect", "quality_inspector", "follow_up", "detail_microscope",
            "generalist", "hard_to_fool", "first_draft_accepted", "streak",
        ]
        let size: CGFloat = 200
        let target: CGFloat = 0.64
        var rawExtents: [CGFloat] = []

        for id in ids {
            let metrics = try XCTUnwrap(ATMAIDayBadgeAssets.asset(named: id), "缺少徽章资源 \(id)").metrics
            rawExtents.append(metrics.extent)

            // 归一化后的图形长边处处相等。
            let rendered = metrics.extent * metrics.canvasSide(in: size, target: target)
            XCTAssertEqual(rendered, size * target, accuracy: 0.5, "\(id) 的光学大小没有对齐")

            // 图形极点落在圆内：半径 0.5，包围盒角最远 target/2 × √2。
            let halfDiagonal = target / 2 * 2.0.squareRoot()
            XCTAssertLessThan(halfDiagonal, 0.5, "图形会伸出圆形底座")

            // 校正后的光学中心就是底座正中。
            let shift = metrics.canvasOffset(in: size, target: target)
            let side = metrics.canvasSide(in: size, target: target)
            let centeredX = (metrics.center.x - 0.5) * side + shift.width
            let centeredY = (metrics.center.y - 0.5) * side + shift.height
            XCTAssertEqual(centeredX, 0, accuracy: 0.01, "\(id) 水平没居中")
            XCTAssertEqual(centeredY, 0, accuracy: 0.01, "\(id) 垂直没居中")
        }

        // 顺带证明这些资源确实是不齐的——哪天资源重新导出成统一留白，这条会失败，
        // 那时候归一化就成了纯粹的多余，可以删。
        let spread = try XCTUnwrap(rawExtents.max()) - XCTUnwrap(rawExtents.min())
        XCTAssertGreaterThan(spread, 0.2, "资源的原始留白已经统一了，归一化可以去掉")
    }

    func testAtlasGuideReportsDistanceToNextLevel() {
        let badge = ATMAIDayBadge(
            id: "code_architect", name: "代码架构师", description: "", family: "grid", kind: "growth",
            level: 1, unlocked: true, qualifiedDays: 4, qualifiedDates: [], nextLevelDays: 7,
            progress: 0.57, lastQualified: nil, cooldownUntil: nil, score: nil, evidence: nil
        )
        XCTAssertEqual(ATMAIDayAtlasGuide.nextStep(badge), "距 L2 还差 3 天")
    }

    private struct CLIResult {
        let status: Int32
        let stdout: Data
        let stderr: String
    }

    private func runCLI(
        executable: String,
        arguments: [String],
        home: URL,
        standardInput: Data? = nil
    ) throws -> CLIResult {
        let process = Process()
        let stdout = Pipe()
        let stderr = Pipe()
        let stdin = standardInput == nil ? nil : Pipe()
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
        if let standardInput, let stdin {
            stdin.fileHandleForWriting.write(standardInput)
            try? stdin.fileHandleForWriting.close()
        }
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

    func testCollectionOverviewDecodesConnectorAuditContract() throws {
        let data = Data(
            """
            {
              "enabled":true,"interval_minutes":5,"lookback_minutes":60,
              "model":"deepseek-v4-flash",
              "connector_health":[{"connector":"example","status":"ready","checked_at":110}],
              "summary":{"sources":1,"enabled_sources":1,"fetched_today":3,
                         "created_today":1,"appended_today":1,"insight_today":0,
                         "ignored_today":1,"failed_today":0,"unread_count":1,
                         "settleable_count":2},
              "sources":[{"id":"cs1","connector":"example","kind":"channel",
                          "external_id":"channel-1","name":"产品反馈","project":"atm",
                          "exclude_pattern":"机器人通知",
                          "strategy":"observe","interval_minutes":60,
                          "priority":"P1","enabled":true,"created_at":10,"updated_at":11},
                         {"id":"cs2","connector":"example","kind":"bot",
                          "external_id":"bot-1","name":"发布通知",
                          "strategy":"tasks","decision_unit":"message","interval_minutes":15,
                          "priority":"P2","enabled":true,"created_at":12,"updated_at":13}],
              "runs":[{"id":"cr1","connector":"example","source_id":"cs1",
                       "status":"succeeded","started_at":100,"finished_at":110,
                       "fetched_count":3,"analyzed_count":3,"created_count":1,
                       "appended_count":1,"insight_count":0,"ignored_count":1,"failed_count":0}],
              "items":[{"id":"ci1","source_id":"cs1","connector":"example",
                        "conversation_id":"channel-1","fingerprint":"fp","message_ids":["m1"],
                        "sender":"测试发送人","occurred_at":99,"raw_context":"想做自动收集",
                        "action":"create","title":"实现自动收集","item_type":"requirement",
                        "project":"atm","priority":"P1","reason":"明确需求","confidence":0.95,
                        "todo_id":"t1","todo_status":"open",
                        "status":"processed","read_at":0,"created_at":100,"updated_at":101},
                       {"id":"ci2","source_id":"cs1","connector":"example",
                        "conversation_id":"channel-1","fingerprint":"fp2","message_ids":["m2"],
                        "action":"create","title":"修好部署脚本","item_type":"bug",
                        "todo_id":"t2","todo_status":"done","todo_archived":true,
                        "status":"processed","read_at":103,"created_at":102,"updated_at":103},
                       {"id":"ci3","source_id":"cs1","connector":"example",
                        "conversation_id":"channel-1","fingerprint":"fp3","message_ids":["m3"],
                        "action":"create","title":"没有回流状态的旧记录",
                        "todo_id":"t3","status":"processed","created_at":104,"updated_at":105},
                       {"id":"ci4","source_id":"cs1","connector":"example",
                        "conversation_id":"channel-1","fingerprint":"fp4","message_ids":["m4"],
                        "action":"insight","title":"手动了结的结论","archived_at":106,
                        "status":"processed","read_at":106,"created_at":106,"updated_at":106}],
              "digests":[]
            }
            """.utf8
        )
        let overview = try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
        XCTAssertTrue(overview.enabled)
        XCTAssertEqual(overview.summary.createdToday, 1)
        XCTAssertEqual(overview.summary.unreadCount, 1)
        XCTAssertEqual(overview.summary.settleableCount, 2)
        XCTAssertEqual(overview.items.first?.id, "ci1")
        XCTAssertEqual(overview.sources.first?.externalID, "channel-1")
        XCTAssertEqual(overview.sources.first?.excludePattern, "机器人通知")
        XCTAssertEqual(overview.sources.first?.effectiveStrategy, "observe")
        // A source saved before the column existed decodes as the window
        // behaviour it actually had; a notification feed carries its own.
        XCTAssertEqual(overview.sources.first?.effectiveDecisionUnit, "window")
        XCTAssertEqual(overview.sources.last?.effectiveDecisionUnit, "message")
        XCTAssertEqual(overview.sources.last?.symbolName, "cpu")
        XCTAssertEqual(overview.sources.first?.effectiveIntervalMinutes, 60)
        XCTAssertEqual(overview.latestRun?.id, "cr1")
        XCTAssertEqual(overview.latestRun(for: "cs1")?.id, "cr1")
        XCTAssertEqual(overview.latestSuccessfulRun(for: "cs1")?.id, "cr1")
        XCTAssertNil(overview.latestRun(for: "cs2"))
        XCTAssertEqual(overview.connectorHealth.first?.status, "ready")
        XCTAssertEqual(overview.items.first?.todoID, "t1")
        XCTAssertEqual(overview.items.first?.messageIDs, ["m1"])
        XCTAssertEqual(overview.items.first?.isUnread, true)
        // A record is only settled once the Todo it filed is: the open one stays in
        // the main list, the finished one folds away with the insights and noise.
        XCTAssertEqual(overview.items.first?.todoClosed, false)
        XCTAssertEqual(overview.items.first?.shouldCollapseInCollection, false)
        XCTAssertEqual(overview.items[1].todoClosed, true)
        XCTAssertEqual(overview.items[1].shouldCollapseInCollection, true)
        XCTAssertEqual(overview.items[1].todoArchived, true)
        XCTAssertEqual(overview.items[1].isUnread, false)
        // Written before the CLI derived the Todo's state: absent, not closed.
        XCTAssertNil(overview.items[2].todoStatus)
        XCTAssertNil(overview.items[2].todoArchived)
        XCTAssertEqual(overview.items[2].shouldCollapseInCollection, false)
        XCTAssertTrue(overview.items[3].isArchived)
        XCTAssertTrue(overview.items[3].shouldCollapseInCollection)
        XCTAssertFalse(overview.items[3].isUnread)
        XCTAssertEqual(ATMCollectionIPCCommand.run.timeout, 300)
        XCTAssertEqual(ATMCollectionIPCCommand.reprocessItem.timeout, 180)
        XCTAssertEqual(ATMIPCCommand.checkTextModel.timeout, 45)
        let notification = ATMCollectionNotificationPayload.make(runs: overview.runs)
        XCTAssertEqual(notification?.subtitle, "有新的收集待查看")
        XCTAssertEqual(notification?.body, "新增 1 · 补充 1 · 结论 0 · 失败 0")
    }

    func testCollectionManualRunIsScopedToOneSource() {
        XCTAssertEqual(ATMCollectionIPCCommand.run.verb, "collect.run")
    }

    func testCollectionItemStateUsesTypedIPCMethods() {
        XCTAssertEqual(ATMCollectionIPCCommand.setItemsRead.verb, "collect.item.read")
        XCTAssertEqual(ATMCollectionIPCCommand.setItemsArchived.verb, "collect.item.archive")
        XCTAssertEqual(ATMCollectionIPCCommand.deleteItems.verb, "collect.item.delete")
    }

    func testOnlyAcknowledgedUnsavedInsightsAreSettleableFromAGroup() throws {
        let data = Data(
            """
            [
              {"id":"ready","source_id":"s","connector":"c","fingerprint":"f1",
               "message_ids":["m1"],"action":"insight","status":"processed",
               "read_at":10,"created_at":10,"updated_at":10},
              {"id":"unread","source_id":"s","connector":"c","fingerprint":"f2",
               "message_ids":["m2"],"action":"insight","status":"processed",
               "read_at":0,"created_at":10,"updated_at":10},
              {"id":"saved","source_id":"s","connector":"c","fingerprint":"f3",
               "message_ids":["m3"],"action":"insight","knowledge_document_id":"k1",
               "status":"processed","read_at":10,"created_at":10,"updated_at":10},
              {"id":"task","source_id":"s","connector":"c","fingerprint":"f4",
               "message_ids":["m4"],"action":"create","todo_id":"t1",
               "status":"processed","read_at":10,"created_at":10,"updated_at":10}
            ]
            """.utf8
        )
        let items = try JSONDecoder().decode([ATMCollectionItem].self, from: data)
        XCTAssertEqual(items.filter(\.isSettleableConclusion).map(\.id), ["ready"])
    }

    func testCollectionGroupsTodoSupplementsUnderTheCreateRecord() throws {
        let data = Data(
            """
            [
              {"id":"create","source_id":"s","connector":"c","fingerprint":"f1",
               "message_ids":["m1"],"action":"create","todo_id":"t1","status":"processed",
               "read_at":10,"created_at":10,"updated_at":10},
              {"id":"late","source_id":"s","connector":"c","fingerprint":"f2",
               "message_ids":["m2"],"action":"append","todo_id":"t1","summary":"第二次补充",
               "occurred_at":30,"status":"processed","read_at":0,"created_at":30,"updated_at":30},
              {"id":"early","source_id":"s","connector":"c","fingerprint":"f3",
               "message_ids":["m3"],"action":"append","todo_id":"t1","summary":"第一次补充",
               "occurred_at":20,"status":"processed","created_at":20,"updated_at":20},
              {"id":"standalone","source_id":"s","connector":"c","fingerprint":"f4",
               "message_ids":["m4"],"action":"append","todo_id":"external-todo","status":"processed",
               "created_at":40,"updated_at":40},
              {"id":"archived-create","source_id":"s","connector":"c","fingerprint":"f5",
               "message_ids":["m5"],"action":"create","todo_id":"t2","status":"processed",
               "archived_at":50,"created_at":50,"updated_at":50},
              {"id":"active-append","source_id":"s","connector":"c","fingerprint":"f6",
               "message_ids":["m6"],"action":"append","todo_id":"t2","status":"processed",
               "created_at":60,"updated_at":60}
            ]
            """.utf8
        )
        let items = try JSONDecoder().decode([ATMCollectionItem].self, from: data)
        XCTAssertEqual(
            ATMCollectionItemGrouping.visibleItems(items).map(\.id),
            ["create", "standalone", "archived-create", "active-append"]
        )
        XCTAssertEqual(
            ATMCollectionItemGrouping.supplements(for: items[0], in: items).map(\.id),
            ["early", "late"]
        )
        XCTAssertEqual(ATMCollectionItemGrouping.unreadCount(for: items[0], in: items), 1)
        XCTAssertTrue(ATMCollectionItemGrouping.supplements(for: items[3], in: items).isEmpty)
        XCTAssertTrue(ATMCollectionItemGrouping.supplements(for: items[4], in: items).isEmpty)
    }

    func testCollectionWorkspaceNoticeOnlyShowsUnattributedFailures() {
        XCTAssertEqual(
            ATMCollectionWorkspaceNotice.message(shared: "删除来源失败", sourceErrors: [:]),
            "删除来源失败"
        )
        // 采集失败已经显示在那个来源的“采集状态”卡片里，横幅不再重复一遍。
        XCTAssertNil(
            ATMCollectionWorkspaceNotice.message(
                shared: "连接器不可用",
                sourceErrors: ["cs1": "连接器不可用"]
            )
        )
        XCTAssertNil(ATMCollectionWorkspaceNotice.message(shared: "   ", sourceErrors: [:]))
        XCTAssertNil(ATMCollectionWorkspaceNotice.message(shared: nil, sourceErrors: [:]))
    }

    func testCollectionItemTypesExplainClassifierVocabulary() {
        XCTAssertEqual(ATMCollectionItemType.resolve("requirement").title, "需求")
        XCTAssertEqual(ATMCollectionItemType.resolve("bug").title, "缺陷")
        XCTAssertEqual(ATMCollectionItemType.resolve("investigation").title, "排查")
        XCTAssertEqual(ATMCollectionItemType.resolve("follow_up").title, "跟进")
        XCTAssertEqual(ATMCollectionItemType.resolve("conversation").title, "讨论")
        XCTAssertEqual(ATMCollectionItemType.resolve("future_type"), .unknown)
        XCTAssertEqual(ATMCollectionItemType.resolve(nil), .unknown)

        for itemType in ATMCollectionItemType.allCases {
            XCTAssertFalse(itemType.title.isEmpty)
            XCTAssertFalse(itemType.systemImage.isEmpty)
        }
    }

    /// 原文的行格式来自 collector 的 formatMessageContext：
    /// `[新消息] 2026-08-06 15:04:05 [张三] 内容`。详情栏把它当聊天渲染，所以这里盯住
    /// 三件在真实数据里一定会出现的事：同一个人连说几句要并成一块、正文自己的换行不能
    /// 被当成新消息、分界线只画在第一条新消息前面。
    func testCollectionTranscriptGroupsMessagesAndMarksWhereFreshBegins() {
        let transcript = ATMCollectionTranscript.parse(
            """
            [上下文] 2026-08-06 15:04:05 [张三] 昨天那个导出还是超时
            [上下文] 2026-08-06 15:04:30 [张三] 大概 30s 就断了
            [新消息] 2026-08-06 15:12:00 [李四] 我看下
            应该是分页没生效
            [新消息] 2026-08-06 15:12:40 [王五] 那我先回滚

            """
        )

        XCTAssertNil(transcript.fallback)
        XCTAssertEqual(transcript.blocks.map(\.sender), ["张三", "李四", "王五"])
        XCTAssertEqual(transcript.blocks.map(\.isFresh), [false, true, true])
        // 秒被丢掉，日期只在开头和跨天时出现：逐行重复的时分秒是这段原文最大的噪声。
        XCTAssertEqual(transcript.blocks.map(\.time), ["08-06 15:04", "15:12", "15:12"])
        // 原文以换行结尾时，末尾空行不能变成消息尾部的空白。
        XCTAssertEqual(transcript.blocks[2].lines, ["那我先回滚"])
        XCTAssertEqual(transcript.blocks[0].lines, ["昨天那个导出还是超时", "大概 30s 就断了"])
        // 正文里的换行留在同一条消息里，不会伪装成又一条。
        XCTAssertEqual(transcript.blocks[1].lines, ["我看下\n应该是分页没生效"])
        // 分界线只有一条，画在第一块新消息前面。
        XCTAssertEqual(transcript.blocks.map(\.startsFresh), [false, true, false])
    }

    func testCollectionTranscriptWithoutMarkersIsAllFreshAndHasNoDivider() {
        // 按需分析没有标记前缀：每一行都参与了判断，没有「上面只是背景」这回事。
        let transcript = ATMCollectionTranscript.parse(
            """
            2026-08-06 15:04:05 [张三] 分析下这段
            2026-08-07 09:00:00 [张三] 顺便看看昨天的
            """
        )

        XCTAssertEqual(transcript.blocks.map(\.isFresh), [true, true])
        XCTAssertEqual(transcript.blocks.map(\.startsFresh), [false, false])
        // 同一个人跨天说话要断开，并且补上日期——只留时分会读成两分钟前的事。
        XCTAssertEqual(transcript.blocks.map(\.time), ["08-06 15:04", "08-07 09:00"])
    }

    func testCollectionTranscriptFallsBackToRawTextAndHandlesEmpty() {
        let freeform = "连接器直出的自由文本\n没有时间戳"
        let transcript = ATMCollectionTranscript.parse(freeform)

        XCTAssertTrue(transcript.blocks.isEmpty)
        XCTAssertEqual(transcript.fallback, freeform)

        XCTAssertEqual(ATMCollectionTranscript.parse(nil), .empty)
        XCTAssertEqual(ATMCollectionTranscript.parse("   \n "), .empty)
    }

    func testCollectionCandidatesDecodeAndBuildAddArguments() throws {
        let data = Data(
            """
            {"candidates":[
              {"kind":"channel","external_id":"channel-example","name":"示例发布频道","detail":"由示例连接器返回"},
              {"kind":"issue","external_id":"owner/repo#42","name":"示例问题","detail":"公开仓库"}
            ]}
            """.utf8
        )
        let list = try JSONDecoder().decode(ATMCollectionCandidateList.self, from: data)
        XCTAssertEqual(list.candidates.count, 2)
        XCTAssertFalse(list.candidates[0].isGroup)
        // A channel reads as a room, a bot as a machine, and an unknown kind
        // still gets a glyph rather than a blank slot.
        XCTAssertEqual(list.candidates[0].symbolName, "person.3.fill")
        XCTAssertEqual(collectionKindSymbol("bot"), "cpu")
        XCTAssertEqual(list.candidates[1].symbolName, "person.fill")
        XCTAssertEqual(collectionKindSymbol(nil), "person.fill")
        XCTAssertEqual(list.candidates[0].detail, "由示例连接器返回")
        XCTAssertFalse(list.candidates[1].isGroup)
        XCTAssertEqual(ATMCollectionSourceTarget.candidate(list.candidates[1]).kind, "issue")
        XCTAssertEqual(ATMCollectionSourceTarget.candidate(list.candidates[1]).value, "owner/repo#42")
        XCTAssertEqual(ATMCollectionSourceTarget.identifier(kind: "channel", externalID: " channel-2 ").kind, "channel")
        XCTAssertEqual(ATMCollectionSourceTarget.identifier(kind: "channel", externalID: " channel-2 ").value, "channel-2")
        XCTAssertTrue(ATMCollectionSourceTarget.identifier(kind: "channel", externalID: "   ").value.isEmpty)
        XCTAssertEqual(ATMCollectionIPCCommand.searchSources.timeout, 45)
    }

    /// The add sheet only knows the connector and whatever the person has picked
    /// or typed; these two rules decide what `collect source add` is handed, and
    /// what the footer says while it cannot be handed anything.
    func testCollectionSourceIdentityResolvesWhatGetsSaved() {
        var identity = ATMCollectionSourceIdentity()
        XCTAssertNil(identity.target)
        XCTAssertEqual(identity.blockReason, "请先选择连接器")

        identity.connector = " DingTalk "
        XCTAssertEqual(identity.trimmedConnector, "dingtalk")
        XCTAssertEqual(identity.blockReason, "请搜索并选择一个来源")

        // A picked candidate wins over the manual fields: it carries the kind the
        // connector itself uses, which is not something anyone should retype.
        identity.manualKind = "group"
        identity.externalID = "typed-id"
        identity.selection = ATMCollectionCandidate(
            kind: "bot", externalID: "bot-1", name: "Code助手", detail: nil
        )
        XCTAssertEqual(identity.target?.kind, "bot")
        XCTAssertEqual(identity.target?.value, "bot-1")
        XCTAssertNil(identity.blockReason)

        // Switching to manual entry ignores the stale candidate.
        identity.manualEntry = true
        XCTAssertEqual(identity.target?.kind, "group")
        XCTAssertEqual(identity.target?.value, "typed-id")
        identity.externalID = "   "
        XCTAssertNil(identity.target)
        XCTAssertEqual(identity.blockReason, "请填写来源类型和来源 ID")

        // Editing pins the identity: the upsert keys on it, so a save that let it
        // drift would create a second source instead of updating this one.
        identity.locked = .identifier(kind: "group", externalID: "chat-9")
        XCTAssertTrue(identity.isEditing)
        XCTAssertEqual(identity.target?.kind, "group")
        XCTAssertEqual(identity.target?.value, "chat-9")
        XCTAssertNil(identity.blockReason)
    }

    func testCollectionVocabularyNamesKindsAndConnectorHealth() {
        XCTAssertEqual(collectionKindLabel("group"), "群聊")
        XCTAssertEqual(collectionKindLabel("open_dingtalk_id"), "联系人")
        XCTAssertEqual(collectionKindLabel("bot"), "机器人")
        XCTAssertEqual(collectionKindLabel("all"), "来源")
        XCTAssertEqual(collectionKindLabel(nil), "来源")
        // A kind ATM has never seen belongs to the connector, so it prints as
        // itself rather than as "未知".
        XCTAssertEqual(collectionKindLabel("issue"), "issue")

        // The search filter is discovery only; every case has to survive the trip
        // to `collect source search --kind`.
        XCTAssertEqual(ATMCollectionSearchKind.allCases.map(\.rawValue), ["all", "group", "user", "bot"])
        for kind in ATMCollectionSearchKind.allCases {
            XCTAssertFalse(kind.label.isEmpty)
            XCTAssertFalse(kind.systemImage.isEmpty)
        }

        let ready = ATMCollectionConnectorHealth(
            connector: "example", status: "ready", error: nil, checkedAt: 110
        )
        XCTAssertEqual(ready.statusLabel, "可用")
        XCTAssertFalse(ready.isUnverified)
        let unchecked = ATMCollectionConnectorHealth(
            connector: "example", status: "not_checked", error: nil, checkedAt: nil
        )
        XCTAssertEqual(unchecked.statusLabel, "尚未检测")
        XCTAssertTrue(unchecked.isUnverified)
        XCTAssertEqual(
            ATMCollectionConnectorHealth(
                connector: "example", status: "auth_required", error: "not_authenticated", checkedAt: 9
            ).statusIcon,
            "person.crop.circle.badge.exclamationmark"
        )
    }

    func testCollectionHistoryDecodesReadOnlyConversation() throws {
        let data = Data(
            """
            {"source":{"connector":"example","kind":"channel","external_id":"channel-1",
                       "name":"示例发布频道"},
             "messages":[
               {"id":"m1","conversation_id":"cid1","sender":"测试发布人","created_at":1785417000,"content":"发布完毕"},
               {"id":"m2","conversation_id":"cid1","created_at":1785417100,"content":"[图片]"}
             ],
             "synced":2}
            """.utf8
        )
        let history = try JSONDecoder().decode(ATMCollectionHistory.self, from: data)
        XCTAssertEqual(history.source?.displayName, "示例发布频道")
        XCTAssertNil(history.source?.id)
        XCTAssertEqual(history.synced, 2)
        XCTAssertNil(history.stale)
        XCTAssertEqual(history.messages.count, 2)
        XCTAssertEqual(history.messages[0].sender, "测试发布人")
        XCTAssertFalse(history.messages[0].time.isEmpty)
        // A message without a sender still renders; only the label is missing.
        XCTAssertNil(history.messages[1].sender)
        XCTAssertEqual(ATMCollectionIPCCommand.history.timeout, 45)
        XCTAssertEqual(ATMCollectionIPCCommand.history.verb, "collect.history")

        // A read served off disk because the connector was unreachable must be labelled, so
        // the sheet can say so instead of passing it off as current.
        let staleData = Data(
            """
            {"source":{"connector":"example","kind":"channel","external_id":"channel-1","name":"示例发布频道"},
             "messages":[{"id":"m1","conversation_id":"cid1","created_at":1785417000,"content":"发布完毕"}],
             "synced":0,"stale":true,"error":"connector returned an error: not_authenticated"}
            """.utf8
        )
        let stale = try JSONDecoder().decode(ATMCollectionHistory.self, from: staleData)
        XCTAssertEqual(stale.stale, true)
        XCTAssertEqual(stale.synced, 0)
        XCTAssertEqual(stale.messages.count, 1)
        XCTAssertNotNil(stale.error)
    }

    func testProjectFolderResolverPrefersNewestExistingBinding() throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-project-resolver-\(UUID().uuidString)", isDirectory: true)
        let older = root.appendingPathComponent("older", isDirectory: true)
        let newer = root.appendingPathComponent("newer", isDirectory: true)
        try FileManager.default.createDirectory(at: older, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: newer, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let todo = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t1","title":"Task","priority":"P1","status":"open","project":"demo","created":"2026-07-18"}"#.utf8
            )
        )
        let bindings = [
            ATMTodoSessionBinding(cwd: older.path, boundAt: 100),
            ATMTodoSessionBinding(cwd: newer.path, boundAt: 200),
        ]

        XCTAssertEqual(
            ATMProjectFolderResolver.resolve(todo: todo, bindings: bindings, homeDirectory: root),
            newer.standardizedFileURL
        )
    }

    func testProjectFolderResolverFallsBackToMoxProjectDirectory() throws {
        let home = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-project-home-\(UUID().uuidString)", isDirectory: true)
        let project = home.appendingPathComponent("mox/demo", isDirectory: true)
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }

        let todo = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t2","title":"Task","priority":"P1","status":"open","project":"demo","created":"2026-07-18"}"#.utf8
            )
        )

        XCTAssertEqual(
            ATMProjectFolderResolver.resolve(todo: todo, bindings: [], homeDirectory: home),
            project.standardizedFileURL
        )
    }

    func testLegacyTodoShowBindingDefaultsMissingMetadata() throws {
        let data = Data(
            """
            {
              "bindings":[{"session_id":"legacy-session","todo_id":"t1"}]
            }
            """.utf8
        )
        let detail = try JSONDecoder().decode(ATMTodoDetail.self, from: data)
        XCTAssertNil(detail.bindings?.first?.cwd)
        XCTAssertEqual(detail.bindings?.first?.boundAt, 0)
    }

    /// The row shows what a session cost and how long it ran, so those fields
    /// have to survive decoding — and stay absent-tolerant, because an older CLI
    /// omits them entirely.
    func testBoundSessionDecodesEffortFieldsAndToleratesTheirAbsence() throws {
        let data = Data(
            """
            {
              "sessions":[
                {"session_id":"019fc6cb-c6ce-71e2-9b65-8f0af41c825b",
                 "indexed_id":"rollout-2026-08-03T16-44-31-019fc6cb-c6ce-71e2-9b65-8f0af41c825b",
                 "short_id":"8f0af41c","agent":"codex","project":"atm","summary":"优化 UI 设计",
                 "indexed":true,"cwd":"/Users/tester/mox/atm","binding_count":1,
                 "first_bound_at":1785746704,"bound_at":1785746704,"started_at":1785746400,
                 "last_at":1785747000,"queries":5,"tool_calls":0,
                 "input_tokens":1201163,"output_tokens":10143,"cost_usd":0.6396605},
                {"session_id":"legacy-session","short_id":"legacy01","agent":"claude",
                 "project":"atm","indexed":true,"binding_count":1,
                 "first_bound_at":100,"bound_at":100,"queries":1,"tool_calls":0}
              ]
            }
            """.utf8
        )
        let sessions = try XCTUnwrap(JSONDecoder().decode(ATMTodoDetail.self, from: data).sessions)
        XCTAssertEqual(sessions.first?.indexedID,
                       "rollout-2026-08-03T16-44-31-019fc6cb-c6ce-71e2-9b65-8f0af41c825b")
        XCTAssertEqual(sessions.first?.summary, "优化 UI 设计")
        XCTAssertEqual(sessions.first?.costUSD ?? 0, 0.6396605, accuracy: 0.000001)
        XCTAssertEqual(sessions.first?.inputTokens, 1_201_163)
        XCTAssertEqual(sessions.first?.activeSeconds, 600)

        XCTAssertNil(sessions.last?.indexedID)
        XCTAssertEqual(sessions.last?.costUSD, 0)
        XCTAssertEqual(sessions.last?.activeSeconds, 0)
    }

    func testTodoDetailDecodesExactBoundSessionsIncludingUnindexedHistory() throws {
        let data = Data(
            """
            {
              "sessions":[
                {"session_id":"indexed-session","short_id":"indexed1","agent":"codex",
                 "project":"atm","summary":"Implemented exact bindings","indexed":true,
                 "cwd":"/tmp/atm","binding_count":2,"first_bound_at":100,"bound_at":300,
                 "queries":4,"tool_calls":7},
                {"session_id":"missing-session-123","short_id":"missing-","agent":"pi",
                 "project":"atm","indexed":false,"binding_count":1,"first_bound_at":200,
                 "bound_at":200,"unbound_at":250,"reason":"done","queries":0,"tool_calls":0}
              ]
            }
            """.utf8
        )
        let detail = try JSONDecoder().decode(ATMTodoDetail.self, from: data)
        XCTAssertEqual(detail.sessions?.count, 2)
        XCTAssertEqual(detail.sessions?.first?.sessionID, "indexed-session")
        XCTAssertEqual(detail.sessions?.first?.bindingCount, 2)
        XCTAssertEqual(detail.sessions?.first?.queries, 4)
        XCTAssertTrue(detail.sessions?.first?.isActive == true)
        XCTAssertEqual(detail.sessions?.last?.sessionID, "missing-session-123")
        XCTAssertFalse(detail.sessions?.last?.indexed ?? true)
        XCTAssertEqual(detail.sessions?.last?.reason, "done")
        XCTAssertFalse(detail.sessions?.last?.isActive ?? true)
    }

    func testDashboardEnvelopeDecodesVersionedAggregateContract() throws {
        let data = Data(
            """
            {
              "schema_version":6,
              "generated_at":"2026-07-24T15:00:00+08:00",
              "work":{
                "generated_at":"2026-07-24T15:00:00+08:00",
                "open":[{"id":"t1","title":"Aggregate dashboard","priority":"P1","status":"open","created":"2026-07-24"}],
                "working":[],"waiting":[],"review":[],"blocked":[],"due":[],
                "summary":{"open":1}
              },
              "todos":[{"id":"t1","title":"Aggregate dashboard","priority":"P1","status":"open","created":"2026-07-24"}],
              "day_stats":[],"hour_stats":[],"model_day_stats":[],"model_hour_stats":[],
              "project_day_stats":[
                {"date":"2026-07-24","client":"codex","project":"atm","sessions":1,
                 "input_tokens":100,"output_tokens":10,"cache_read_tokens":40,"cost_usd":1.5}
              ],
              "project_hour_stats":[],
              "ranges":{
                "today":{"start_date":"2026-07-24","end_date":"2026-07-24",
                     "model_stats":[],"sessions":[],"skill_stats":[],
                     "project_stats":[{"project":"atm","agent":"codex","sessions":1,"queries":3,
                                       "input_tokens":100,"output_tokens":10,"cost_usd":1.5}]},
                "yesterday":{"start_date":"2026-07-23","end_date":"2026-07-23",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]},
                "this_week":{"start_date":"2026-07-20","end_date":"2026-07-26",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]},
                "last_week":{"start_date":"2026-07-13","end_date":"2026-07-19",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]},
                "this_month":{"start_date":"2026-07-01","end_date":"2026-07-31",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]},
                "last_7_days":{"start_date":"2026-07-18","end_date":"2026-07-24",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]},
                "last_30_days":{"start_date":"2026-06-25","end_date":"2026-07-24",
                     "model_stats":[],"sessions":[],"skill_stats":[],"project_stats":[]}
              },
              "live_status":{"sessions":[],"bindings":[],"time":"15:00:00"},
              "index_health":{
                "generated_at":"2026-07-24T07:00:00Z",
                "index":{"path":"/tmp/atm.db","exists":true,"schema_version":12,"indexed_sessions":0},
                "sync":{"scope":"all","status":"fresh","run_status":"succeeded","last_attempt_at":null,"last_success_at":null,"age_seconds":null,"stale_after_seconds":600,"last_error":"","last_synced_files":0}
              }
            }
            """.utf8
        )
        let envelope = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: data)
        XCTAssertEqual(envelope.schemaVersion, ATMDashboardContract.supportedSchemaVersion)
        XCTAssertEqual(envelope.todos.first?.id, "t1")
        let snapshot = envelope.makeSnapshot(refreshedAt: .distantPast)
        XCTAssertEqual(snapshot.work.summary.open, 1)
        XCTAssertEqual(snapshot.rangeData[.last30Days]?.sessions.count, 0)
        XCTAssertEqual(snapshot.indexHealth?.index.schemaVersion, 12)
        XCTAssertEqual(snapshot.projectDayStats.first?.project, "atm")
        XCTAssertEqual(snapshot.rangeData[.today]?.projectStats.first?.totalTokens, 110)
        // Every named window decodes, and each carries the boundaries the CLI
        // computed rather than leaving the app to derive them.
        XCTAssertEqual(Set(snapshot.rangeData.keys), Set(ATMMetricsRange.allCases))
        XCTAssertEqual(snapshot.rangeData[.thisMonth]?.startDate, "2026-07-01")
        XCTAssertEqual(snapshot.rangeData[.thisMonth]?.endDate, "2026-07-31")
        XCTAssertEqual(snapshot.rangeData[.yesterday]?.startDate, "2026-07-23")
    }

    /// The launch fast path asks for `--sections work`, so the envelope arrives
    /// with every statistics key empty. It has to decode into a usable task list
    /// rather than fail, because failing here means the window stays blank for
    /// the second the full snapshot takes.
    func testWorkOnlyDashboardEnvelopeDecodesWithoutStatistics() throws {
        let data = Data(
            """
            {
              "schema_version":6,
              "generated_at":"2026-07-24T15:00:00+08:00",
              "work":{
                "generated_at":"2026-07-24T15:00:00+08:00",
                "open":[{"id":"t1","title":"Paint before the charts","priority":"P1","status":"open","created":"2026-07-24"}],
                "working":[],"waiting":[],"review":[],"blocked":[],"due":[],
                "summary":{"open":1}
              },
              "todos":[{"id":"t1","title":"Paint before the charts","priority":"P1","status":"open","created":"2026-07-24"}],
              "day_stats":[],"hour_stats":[],"model_day_stats":[],"model_hour_stats":[],
              "project_day_stats":[],"project_hour_stats":[],
              "ranges":{},
              "live_status":{"sessions":[],"bindings":[],"time":"15:00:00"},
              "index_health":{
                "generated_at":"2026-07-24T07:00:00Z",
                "index":{"path":"/tmp/atm.db","exists":true,"schema_version":12,"indexed_sessions":0},
                "sync":{"scope":"all","status":"fresh","run_status":"succeeded","last_attempt_at":null,"last_success_at":null,"age_seconds":null,"stale_after_seconds":600,"last_error":"","last_synced_files":0}
              }
            }
            """.utf8
        )
        let envelope = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: data)
        // Same schema version as the full snapshot: one section of one contract,
        // not a second format the mismatch guard would have to learn.
        XCTAssertEqual(envelope.schemaVersion, ATMDashboardContract.supportedSchemaVersion)
        let snapshot = envelope.makeSnapshot(refreshedAt: .distantPast)
        XCTAssertEqual(snapshot.work.summary.open, 1)
        XCTAssertEqual(envelope.todos.first?.id, "t1")
        XCTAssertEqual(snapshot.indexHealth?.index.schemaVersion, 12)
        // An absent `ranges` still yields every key, so a statistics view reads an
        // empty window instead of finding nothing there at all.
        XCTAssertEqual(Set(snapshot.rangeData.keys), Set(ATMMetricsRange.allCases))
        XCTAssertEqual(snapshot.rangeData[.last30Days]?.sessions.count, 0)
        XCTAssertTrue(snapshot.dayStats.isEmpty)
        XCTAssertTrue(snapshot.projectDayStats.isEmpty)
    }

    func testDashboardUsesOneTypedIPCRequestForSectionAndSession() throws {
        XCTAssertEqual(ATMDashboardIPCCommand.snapshot.arguments, ["_ipc", "dashboard.snapshot"])
        let request = ATMDashboardRequest(sections: ["work"], sessionID: "s9")
        let object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as! [String: Any]
        XCTAssertEqual(object["sections"] as? [String], ["work"])
        XCTAssertEqual(object["session_id"] as? String, "s9")

        let withoutSession = try JSONSerialization.jsonObject(
            with: JSONEncoder().encode(ATMDashboardRequest(sessionID: ""))
        ) as! [String: Any]
        XCTAssertNil(withoutSession["session_id"])
    }

    func testTodaySessionsDecodeAndFilterIndependentlyFromDashboard() throws {
        let data = Data(
            """
            [{
              "session_id":"session-full","short_id":"session1","agent":"codex","project":"atm",
              "model":"gpt-5.6-sol","started_ts":1784883600,"last_ts":1784887200,
              "requests":3,"input_tokens":100,"output_tokens":10,
              "cache_create_tokens":5,"cache_read_tokens":40,
              "total_tokens":155,"cost_usd":1.5,"share":1.0
            }]
            """.utf8
        )
        let sessions = try JSONDecoder().decode([ATMSessionUsage].self, from: data)

        XCTAssertEqual(sessions.first?.shortID, "session1")
        XCTAssertEqual(sessions.first?.cacheTokens, 45)
        XCTAssertEqual(
            sessions.filtered(using: ATMUsageFilters(
                model: "gpt-5.6-sol",
                client: "Codex",
                project: "atm"
            )).count,
            1
        )
        XCTAssertTrue(sessions.filtered(using: ATMUsageFilters(project: "other")).isEmpty)
        XCTAssertEqual(
            sessions.filterOptions(dimension: .model, filters: ATMUsageFilters()),
            ["gpt-5.6-sol"]
        )
    }

    func testDashboardEnvelopeRejectsUnknownSchemaVersion() throws {
        let data = Data(#"{"schema_version":99}"#.utf8)
        XCTAssertThrowsError(try JSONDecoder().decode(ATMDashboardEnvelope.self, from: data))
    }

    /// A newer CLI than the App: the App is the side that has to move, and the
    /// message has to say so rather than describing a corrupt payload.
    func testDashboardSchemaMismatchTellsTheUserToUpdateTheApp() throws {
        let data = Data(
            #"{"schema_version":\#(ATMDashboardContract.supportedSchemaVersion + 1)}"#.utf8
        )
        do {
            _ = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: data)
            XCTFail("expected a schema mismatch")
        } catch let mismatch as ATMDashboardSchemaMismatch {
            XCTAssertEqual(mismatch.direction, .appTooOld)
            XCTAssertEqual(mismatch.cliVersion, ATMDashboardContract.supportedSchemaVersion + 1)
            XCTAssertEqual(mismatch.appVersion, ATMDashboardContract.supportedSchemaVersion)
            let summary = mismatch.summary
            XCTAssertTrue(summary.contains("App 需要更新"), summary)
            XCTAssertTrue(summary.contains("build-app.sh"), "no actionable step: \(summary)")
            // Both numbers must appear, or the user cannot tell how far apart they are.
            XCTAssertTrue(summary.contains("v\(ATMDashboardContract.supportedSchemaVersion + 1)"), summary)
            XCTAssertTrue(summary.contains("v\(ATMDashboardContract.supportedSchemaVersion)"), summary)
        }
    }

    /// An older CLI than the App: the same failure, the opposite instruction.
    func testDashboardSchemaMismatchTellsTheUserToUpdateTheCLI() throws {
        let older = ATMDashboardContract.supportedSchemaVersion - 1
        let data = Data(#"{"schema_version":\#(older)}"#.utf8)
        do {
            _ = try JSONDecoder().decode(ATMDashboardEnvelope.self, from: data)
            XCTFail("expected a schema mismatch")
        } catch let mismatch as ATMDashboardSchemaMismatch {
            XCTAssertEqual(mismatch.direction, .cliTooOld)
            let summary = mismatch.summary
            XCTAssertTrue(summary.contains("CLI 需要更新"), summary)
            XCTAssertTrue(summary.contains("install.sh"), "no actionable step: \(summary)")
            XCTAssertTrue(summary.contains("v\(older)"), summary)
        }
    }

    /// The two directions must not produce the same advice; that was the bug.
    func testDashboardSchemaMismatchDirectionsGiveDifferentAdvice() {
        let appBehind = ATMDashboardSchemaMismatch(cliVersion: 7, appVersion: 6)
        let cliBehind = ATMDashboardSchemaMismatch(cliVersion: 5, appVersion: 6)
        XCTAssertNotEqual(appBehind.recoverySuggestion, cliBehind.recoverySuggestion)
        XCTAssertNotEqual(appBehind.errorDescription, cliBehind.errorDescription)
        XCTAssertEqual(appBehind.direction, .appTooOld)
        XCTAssertEqual(cliBehind.direction, .cliTooOld)
    }

    func testQuotaDecodesWindowsAndSkipsAgentsWithoutData() throws {
        let data = Data(
            """
            {
              "codex":{
                "plan":"prolite",
                "primary":{"used_percent":44,"window_minutes":10080,"resets_at":1785635728,"resets_in":"5d19h"},
                "secondary":{"used_percent":12.5,"window_minutes":300,"resets_at":1785635728,"resets_in":"2h10m"}
              },
              "grokbuild":{
                "plan":"SuperGrok",
                "primary":{"used_percent":12,"window_minutes":10080,"resets_at":1785635728,"resets_in":"5d19h"}
              },
              "claude":null
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        XCTAssertEqual(quota.entries.map(\.agent), ["codex", "grokbuild"])
        let codex = try XCTUnwrap(quota.agents["codex"])
        XCTAssertEqual(codex.plan, "prolite")
        XCTAssertEqual(codex.windows.count, 2)
        XCTAssertEqual(codex.primary?.windowLabel, "1w")
        XCTAssertEqual(codex.secondary?.windowLabel, "5h")
        XCTAssertEqual(quota.agents["grokbuild"]?.plan, "SuperGrok")
        XCTAssertEqual(ATMAgentDisplay.name("grokbuild"), "Grok")
        XCTAssertEqual(ATMAgentDisplay.name("Grok"), "Grok")
        XCTAssertEqual(ATMAgentDisplay.monogram("claude"), "C")
        XCTAssertEqual(ATMAgentDisplay.monogram("codex"), "X")
        XCTAssertEqual(ATMAgentDisplay.monogram("pi"), "π")
        XCTAssertEqual(ATMAgentDisplay.monogram("grokbuild"), "G")
        XCTAssertEqual(ATMAgentDisplay.iconResourceName("claude"), "agent-claude")
        XCTAssertEqual(ATMAgentDisplay.iconResourceName("Claude Code"), "agent-claude")
        XCTAssertEqual(ATMAgentDisplay.iconResourceName("QoderCLI"), "agent-qoder")
        XCTAssertEqual(ATMAgentDisplay.iconResourceName("Grok"), "agent-grok")
        XCTAssertNil(ATMAgentDisplay.iconResourceName("custom-agent"))
        XCTAssertEqual(ATMAgentDisplay.systemImage("claude"), "text.bubble.fill")
        XCTAssertEqual(ATMAgentDisplay.systemImage("codex"), "chevron.left.forwardslash.chevron.right")
        XCTAssertEqual(ATMAgentDisplay.key("QoderCLI"), "qodercli")
        XCTAssertEqual(ATMAgentDisplay.key("Claude Code"), "claude")
        // Compact surfaces keep one row per window.
        XCTAssertEqual(quota.cards.map(\.agent), ["codex", "codex", "grokbuild"])
        XCTAssertEqual(Set(quota.cards.map(\.id)).count, 3)
        // The desktop groups both Codex windows into one service card instead
        // of repeating the service identity twice.
        XCTAssertEqual(quota.serviceCards.map(\.agent), ["codex", "grokbuild"])
        XCTAssertEqual(quota.serviceCards.first?.windows.map(\.windowLabel), ["1w", "5h"])
        XCTAssertEqual(quota.tightestWindow?.window.displayPercent, 44)
        XCTAssertTrue(quota.tooltipText?.contains("Grok 1w 12%") == true)
    }

    func testQuotaDecodesLiveSourceAndProducts() throws {
        // `atm quota --json` adds source/products once Grok live quota is on;
        // both are optional so older CLI output still decodes.
        let data = Data(
            """
            {
              "grokbuild":{
                "plan":"SuperGrok",
                "primary":{"used_percent":19,"window_minutes":10080,"resets_at":1785828533,"resets_in":"6d1h"},
                "source":"live",
                "products":[
                  {"product":"GrokBuild","used_percent":13},
                  {"product":"GrokImagine","used_percent":4},
                  {"product":"GrokChat","used_percent":2}
                ]
              }
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let grok = try XCTUnwrap(quota.agents["grokbuild"])
        XCTAssertEqual(grok.source, "live")
        XCTAssertEqual(grok.products?.map(\.displayName), ["Build", "Imagine", "Chat"])
        XCTAssertEqual(grok.products?.map(\.usedPercent), [13, 4, 2])
        // Products ride on the primary window's card only.
        let card = try XCTUnwrap(quota.cards.first)
        XCTAssertEqual(card.sourceLabel, "实时")
        XCTAssertEqual(card.products.count, 3)
    }

    @MainActor
    func testBundledAgentIconResourcesLoad() {
        let names = [
            "agent-claude", "agent-codex", "agent-pi", "agent-copilot",
            "agent-cursor", "agent-qoder", "agent-grok",
        ]
        for name in names {
            XCTAssertNotNil(ATMBrandAssets.agentIconImage(resourceName: name), "未加载 \(name)")
        }
    }

    func testQuotaDecodesExternalProviderCards() throws {
        let data = Data(
            """
            {
              "claude":{
                "provider_cards":[{
                  "id":"daily","provider":"example","title":"Team plan","period":"今日",
                  "observed_at":"2026-08-04T03:28:37Z","source":"browser",
                  "url":"https://example.com/account",
                  "metrics":[
                    {"id":"count","label":"每日次数","used":428,"limit":4000,"used_percent":10.7,"unit":"次"},
                    {"id":"amount","label":"每日金额","used":266.58,"limit":1200,"used_percent":22.215,"currency":"CNY","precision":2}
                  ]
                }]
              }
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        XCTAssertTrue(quota.cards.isEmpty)
        let card = try XCTUnwrap(quota.providerCards.first)
        XCTAssertEqual(card.agent, "claude")
        XCTAssertEqual(card.providerLabel, "Example")
        XCTAssertEqual(card.sourceLabel, "浏览器")
        XCTAssertEqual(card.payload.metrics.map(\.valueText), ["428 / 4.0K 次", "¥266.58 / ¥1200.00"])
        XCTAssertEqual(card.payload.linkURL?.absoluteString, "https://example.com/account")
        XCTAssertFalse(quota.isEmpty)
        XCTAssertTrue(quota.tooltipText?.contains("Claude Example 每日金额 22%") == true)
    }

    /// The card's URL becomes a click that opens the system browser, so a scheme
    /// the App must never launch has to read as "no link" rather than as a link.
    /// The CLI already refuses these; this covers a hand-edited cache file.
    func testProviderCardIgnoresANonHTTPURL() throws {
        func linkURL(_ url: String) throws -> URL? {
            let data = Data(
                """
                {"claude":{"provider_cards":[{"id":"daily","provider":"example",
                "title":"Team plan","observed_at":"2026-08-04T03:28:37Z","url":"\(url)",
                "metrics":[]}]}}
                """.utf8
            )
            let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
            return try XCTUnwrap(quota.providerCards.first).payload.linkURL
        }

        XCTAssertNil(try linkURL("file:///etc/passwd"))
        XCTAssertNil(try linkURL("javascript:alert(1)"))
        XCTAssertNil(try linkURL("/manage/"))
        XCTAssertNil(try linkURL(""))
        XCTAssertEqual(try linkURL("https://example.com/account")?.host, "example.com")
    }

    /// ATM keeps the last card a provider returned and marks it unavailable once
    /// the provider reports nothing, so a daily quota that has not been observed
    /// today leaves a card with no reading instead of a hole in the grid.
    func testQuotaKeepsAProviderCardThatHasNoReadingLeft() throws {
        let data = Data(
            """
            {
              "claude":{
                "provider_cards":[{
                  "id":"daily","provider":"example","title":"Team plan","period":"今日",
                  "observed_at":"2026-08-04T03:28:37Z","metrics":[],
                  "unavailable":true,"unavailable_reason":"empty"
                }]
              }
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let card = try XCTUnwrap(quota.providerCards.first)
        XCTAssertTrue(card.payload.isUnavailable)
        XCTAssertEqual(card.payload.unavailableText, "暂无数据")
        // The card holds its place without claiming a reading: the 额度 module
        // renders, and nothing feeds the menu bar or its tooltip.
        XCTAssertFalse(quota.isEmpty)
        XCTAssertNil(quota.menuBarSuffix)
        XCTAssertNil(quota.tooltipText)
    }

    func testProviderCardObservedLabelIsLocalAndDatesOlderObservations() throws {
        func payload(observedAt: String) throws -> ATMProviderQuotaPayload {
            let data = Data(
                """
                {"claude":{"provider_cards":[{"id":"daily","provider":"example",
                "title":"Team plan","observed_at":"\(observedAt)","metrics":[]}]}}
                """.utf8
            )
            let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
            return try XCTUnwrap(quota.providerCards.first).payload
        }

        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "MM-dd HH:mm"
        let stale = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-08-04T03:28:37Z"))
        // The timestamp is UTC, so slicing "HH:mm" out of the string reported a
        // card observed at 11:28 Beijing time as 03:28, undated.
        XCTAssertEqual(
            try payload(observedAt: "2026-08-04T03:28:37Z").observedTimeLabel,
            formatter.string(from: stale)
        )

        let today = ISO8601DateFormatter().string(from: Date())
        XCTAssertEqual(try payload(observedAt: today).observedTimeLabel.count, 5)
    }

    func testQuotaTranslatesLegacyDailyQuotaIntoProviderCard() throws {
        let data = Data(
            """
            {
              "claude":{
                "provider":"legacy",
                "daily_quota":{
                  "card_title":"Team daily plan","day":"2026-08-04",
                  "count":{"used":428,"limit":4000,"used_percent":10.7},
                  "amount":{"used":266.58,"limit":1200,"used_percent":22.215,"currency":"CNY"},
                  "observed_at":"2026-08-04T03:28:37.473Z","source":"browser"
                }
              }
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let card = try XCTUnwrap(quota.providerCards.first)
        XCTAssertEqual(card.providerLabel, "Legacy")
        XCTAssertEqual(card.payload.period, "今日")
        XCTAssertEqual(card.payload.metrics.map(\.id), ["count", "amount"])
        XCTAssertEqual(card.payload.metrics.map(\.usedPercent), [10.7, 22.215])
    }


    func testQuotaTreatsAWindowWithoutResetsInAsAlreadyReset() throws {
        // `atm quota --json` drops resets_in once the window has elapsed but
        // keeps the stale used_percent, so the app must not report the old
        // percentage as if it still applied.
        let data = Data(
            #"{"codex":{"plan":"pro","primary":{"used_percent":98,"window_minutes":10080,"resets_at":1}}}"#.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let window = try XCTUnwrap(quota.agents["codex"]?.primary)
        XCTAssertTrue(window.hasReset)
        XCTAssertEqual(window.usedPercent, 98)
        XCTAssertEqual(window.displayPercent, 0)
        XCTAssertEqual(window.resetText, "已重置")
        XCTAssertNil(quota.menuBarSuffix)
    }

    func testQuotaReachesTheMenuBarOnlyOnceItNeedsAction() throws {
        func snapshot(percent: Double) -> ATMQuotaSnapshot {
            let data = Data(
                """
                {"codex":{"plan":"pro","primary":{"used_percent":\(percent),\
                "window_minutes":10080,"resets_at":9999999999,"resets_in":"1d"}}}
                """.utf8
            )
            return try! JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        }
        XCTAssertNil(snapshot(percent: 44).menuBarSuffix)
        XCTAssertNil(snapshot(percent: 74).menuBarSuffix)
        XCTAssertEqual(snapshot(percent: 75).menuBarSuffix, "75%")
        XCTAssertEqual(snapshot(percent: 91).menuBarSuffix, "91%")
        // The tooltip has no layout cost, so it reports every window.
        XCTAssertEqual(snapshot(percent: 44).tooltipText, "配额 Codex 1w 44%")
    }

    /// The arrow is the point of the whole change: 89% resting and 89% climbing
    /// fast are the same number and opposite situations. It is one glyph, and only
    /// on a window already worth showing — the restraint that keeps the menu bar
    /// from competing with counters that are always relevant.
    func testQuotaTrendReachesTheMenuBarAsASingleArrow() throws {
        func snapshot(percent: Double, rate: Double, fullBeforeReset: Bool = false) -> ATMQuotaSnapshot {
            let data = Data(
                """
                {"codex":{"plan":"pro","primary":{"used_percent":\(percent),\
                "window_minutes":10080,"resets_at":9999999999,"resets_in":"1d",\
                "trend":{"percent_per_hour":\(rate),"samples":4,"span_minutes":120,\
                "full_at":9999999998,"full_before_reset":\(fullBeforeReset)}}}}
                """.utf8
            )
            return try! JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        }
        XCTAssertEqual(snapshot(percent: 91, rate: 12.5).menuBarSuffix, "91%↑")
        XCTAssertEqual(snapshot(percent: 91, rate: -3).menuBarSuffix, "91%↓")
        // Jitter is not movement: below the flat threshold there is no arrow, so a
        // resting quota does not read as a problem.
        XCTAssertEqual(snapshot(percent: 91, rate: 0.2).menuBarSuffix, "91%")
        XCTAssertEqual(snapshot(percent: 91, rate: -0.4).menuBarSuffix, "91%")
        // A healthy window stays out of the menu bar, trend or not.
        XCTAssertNil(snapshot(percent: 40, rate: 30).menuBarSuffix)
        // The tooltip has room, so it carries the rate.
        XCTAssertEqual(snapshot(percent: 40, rate: 12.5).tooltipText, "配额 Codex 1w 40% +12.5%/小时")
        XCTAssertEqual(snapshot(percent: 40, rate: 0.1).tooltipText, "配额 Codex 1w 40%")

        let window = try XCTUnwrap(snapshot(percent: 91, rate: 12.5, fullBeforeReset: true)
            .agents["codex"]?.primary)
        let trend = try XCTUnwrap(window.trend)
        XCTAssertTrue(trend.fullBeforeReset)
        XCTAssertEqual(trend.spanMinutes, 120)
        XCTAssertEqual(trend.samples, 4)
    }

    /// Thin history is not a trend of zero. Before two samples exist the CLI omits
    /// the key, and the app must show the plain percentage rather than "持平".
    func testQuotaWithoutHistoryShowsNoTrendAtAll() throws {
        let data = Data(
            """
            {"codex":{"plan":"pro","primary":{"used_percent":91,\
            "window_minutes":10080,"resets_at":9999999999,"resets_in":"1d"}}}
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let window = try XCTUnwrap(quota.agents["codex"]?.primary)
        XCTAssertNil(window.trend)
        XCTAssertEqual(quota.menuBarSuffix, "91%")
        XCTAssertEqual(quota.tooltipText, "配额 Codex 1w 91%")
    }

    func testQuotaLevelThresholds() {
        XCTAssertEqual(ATMQuotaLevel.level(forPercent: 0), .healthy)
        XCTAssertEqual(ATMQuotaLevel.level(forPercent: 74.9), .healthy)
        XCTAssertEqual(ATMQuotaLevel.level(forPercent: 75), .warning)
        XCTAssertEqual(ATMQuotaLevel.level(forPercent: 89.9), .warning)
        XCTAssertEqual(ATMQuotaLevel.level(forPercent: 90), .critical)
    }

    func testQuotaWindowLabelMatchesCLIFormatting() throws {
        func label(_ minutes: Int) throws -> String {
            let data = Data(
                #"{"used_percent":0,"window_minutes":\#(minutes),"resets_at":0}"#.utf8
            )
            return try JSONDecoder().decode(ATMQuotaWindow.self, from: data).windowLabel
        }
        XCTAssertEqual(try label(10080), "1w")
        XCTAssertEqual(try label(1440), "1d")
        XCTAssertEqual(try label(300), "5h")
        XCTAssertEqual(try label(45), "45m")
        XCTAssertEqual(try label(0), "窗口")
    }

    func testRealCLIContractsCoverWorkStateAndPreserveErrors() throws {
        guard let executable = ProcessInfo.processInfo.environment["ATM_CONTRACT_EXECUTABLE"],
              !executable.isEmpty else {
            throw XCTSkip("ATM_CONTRACT_EXECUTABLE is set by the release contract check")
        }
        // The fixture's Todo belongs to project atm and `session bind` now
        // refuses a working directory that resolves to a different project, so
        // the run directory has to be named after the project rather than after
        // the temp fixture. The unique parent keeps concurrent runs apart.
        let fixtureRoot = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-cli-contract-\(UUID().uuidString)", isDirectory: true)
        let home = fixtureRoot.appendingPathComponent("atm", isDirectory: true)
        let atmDirectory = home.appendingPathComponent(".atm", isDirectory: true)
        try FileManager.default.createDirectory(at: atmDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: fixtureRoot) }

        // The database is the only source of work state, so the fixture is built
        // by the same commands a user runs — including a bind that needs no
        // `--force`, which is what a user in the right directory gets.
        for arguments in [
            ["todo", "add", "Contract task", "--project", "atm"],
            ["todo", "start", "t1"],
            ["session", "bind", "t1", "--agent-session", "legacy-session"],
        ] {
            let seeded = try runCLI(executable: executable, arguments: arguments, home: home)
            XCTAssertEqual(seeded.status, 0, "\(arguments): \(seeded.stderr)")
        }

        let now = try runCLI(executable: executable, arguments: ["now", "--json"], home: home)
        XCTAssertEqual(now.status, 0, now.stderr)
        let work = try JSONDecoder().decode(ATMNowSnapshot.self, from: now.stdout)
        XCTAssertEqual(work.working.map(\.id), ["t1"])

        let status = try runCLI(
            executable: executable,
            arguments: ["session", "status", "--json"],
            home: home
        )
        XCTAssertEqual(status.status, 0, status.stderr)
        let live = try JSONDecoder().decode(ATMLiveStatus.self, from: status.stdout)
        XCTAssertEqual(live.bindings.first?.state, "bound")
        XCTAssertGreaterThan(live.bindings.first?.binding.boundAt ?? 0, 0)

        let current = try runCLI(
            executable: executable,
            arguments: [
                "session", "current", "--agent-session", "legacy-session", "--json",
            ],
            home: home
        )
        XCTAssertEqual(current.status, 0, current.stderr)
        let currentSession = try JSONDecoder().decode(ATMCurrentSession.self, from: current.stdout)
        XCTAssertTrue(currentSession.bound)
        XCTAssertEqual(currentSession.todo?.status, "in_progress")

        let show = try runCLI(
            executable: executable,
            arguments: ["todo", "show", "t1", "--json"],
            home: home
        )
        XCTAssertEqual(show.status, 0, show.stderr)
        let detail = try JSONDecoder().decode(ATMTodoDetail.self, from: show.stdout)
        XCTAssertGreaterThan(detail.bindings?.first?.boundAt ?? 0, 0)

        // Quota is read outside the dashboard snapshot, so its shape needs its
        // own guarantee. Scope the fixture to Codex: Antigravity quota comes
        // from a machine-wide loopback process and can legitimately be present
        // even with an empty HOME. With no Codex logs, the CLI reports that
        // requested agent as null rather than omitting it.
        let quotaResult = try runCLI(
            executable: executable,
            arguments: ["quota", "--agent", "codex", "--json"],
            home: home
        )
        XCTAssertEqual(quotaResult.status, 0, quotaResult.stderr)
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: quotaResult.stdout)
        XCTAssertTrue(quota.isEmpty)
        XCTAssertNil(quota.menuBarSuffix)
        XCTAssertNil(quota.tooltipText)

        // Today sessions are intentionally outside the dashboard contract and
        // stay independently decodable even when the result is empty.
        let todaySessionsResult = try runCLI(
            executable: executable,
            arguments: ["stats", "--by", "session-usage", "--days", "1", "--json"],
            home: home
        )
        XCTAssertEqual(todaySessionsResult.status, 0, todaySessionsResult.stderr)
        let todaySessions = try JSONDecoder().decode(
            [ATMSessionUsage].self,
            from: todaySessionsResult.stdout
        )
        XCTAssertTrue(todaySessions.isEmpty)

        let sync = try runCLI(executable: executable, arguments: ["sync"], home: home)
        XCTAssertEqual(sync.status, 0, sync.stderr)
        let dashboardRequest = try JSONEncoder().encode(
            ATMDashboardRequest(sessionID: "legacy-session")
        )
        let dashboard = try runCLI(
            executable: executable,
            arguments: ["_ipc", "dashboard.snapshot"],
            home: home,
            standardInput: dashboardRequest
        )
        XCTAssertEqual(dashboard.status, 0, dashboard.stderr)
        let ipcEnvelope = try JSONDecoder().decode(
            ATMIPCEnvelope<ATMDashboardEnvelope>.self,
            from: dashboard.stdout
        )
        XCTAssertEqual(ipcEnvelope.verb, "dashboard.snapshot")
        XCTAssertEqual(ipcEnvelope.data.schemaVersion, ATMDashboardContract.supportedSchemaVersion)
        XCTAssertEqual(ipcEnvelope.data.todos.map(\.id), ["t1"])
        XCTAssertTrue(ipcEnvelope.data.currentSession?.bound == true)
        XCTAssertTrue(FileManager.default.fileExists(atPath: atmDirectory.appendingPathComponent("atm.db").path))

        let missing = try runCLI(
            executable: executable,
            arguments: ["todo", "show", "missing", "--json"],
            home: home
        )
        XCTAssertNotEqual(missing.status, 0)
        XCTAssertTrue(missing.stderr.contains("todo not found: missing"))
        let appError = ATMCommandError.failed(
            arguments: ["todo", "show", "missing", "--json"],
            status: missing.status,
            message: missing.stderr
        ).localizedDescription
        XCTAssertTrue(appError.contains("todo not found: missing"))
    }

    func testParsesProgressEntriesFromDoc() {
        let content = """
        # Sample

        - **ID**: t1

        ## 需求

        do a thing

        ## 进展
        - [2026-07-14 11:32] first step done
        - [2026-07-14 12:00] second step
          continued line
        - [2026-07-15 09:37] [done] 通过 ATM 菜单栏完成

        ## 备注
        - not a progress entry
        """
        let entries = ATMTodoProgressEntry.parse(from: content)
        XCTAssertEqual(entries.count, 3)
        XCTAssertEqual(entries[0].timestamp, "2026-07-14 11:32")
        XCTAssertEqual(entries[0].text, "first step done")
        XCTAssertFalse(entries[0].isDoneMarker)
        XCTAssertEqual(entries[0].kind, .progress)
        XCTAssertEqual(entries[1].text, "second step\ncontinued line")
        XCTAssertTrue(entries[2].isDoneMarker)
        XCTAssertEqual(entries[2].text, "通过 ATM 菜单栏完成")
    }

    func testParsesProgressAndSupplementsIntoOneChronologicalTimeline() {
        let content = """
        ## 进展
        - [2026-08-03 09:47] 已完成初版实现
        - [2026-08-03 13:40] 已确认展示问题

        ## 补充
        - [2026-08-03 12:58] [钉钉采集:c72fb52497ed9c1d] 为原始版 Markdown 补充排版样式。

          来源对话：
          [新消息] 请增加 typography 排版样式

        ## 备注
        - 不应进入动态
        """

        let entries = ATMTodoProgressEntry.parse(from: content)

        XCTAssertEqual(entries.count, 3)
        XCTAssertEqual(entries.map(\.timestamp), [
            "2026-08-03 09:47", "2026-08-03 12:58", "2026-08-03 13:40",
        ])
        XCTAssertEqual(entries.map(\.kind), [.progress, .supplement, .progress])
        XCTAssertEqual(entries[1].text, "为原始版 Markdown 补充排版样式。")
        XCTAssertFalse(entries[1].text.contains("[钉钉采集:"))
        XCTAssertFalse(entries[1].text.contains("来源对话"))
        XCTAssertFalse(entries[1].text.contains("[新消息]"))
    }

    func testNewCollectorSupplementHidesIdempotencyMarker() {
        let content = """
        ## 补充
        - [2026-08-03 14:00] 增加 typography 排版样式。

          <!-- [钉钉采集:c72fb52497ed9c1d] -->
        """

        let entry = ATMTodoProgressEntry.parse(from: content).first

        XCTAssertEqual(entry?.kind, .supplement)
        XCTAssertEqual(entry?.text, "增加 typography 排版样式。")
    }

    func testProgressEntryExtractsLatestNextAction() {
        let content = """
        ## 进展
        - [2026-07-18 22:00] 结果：修复完成；证据：测试通过；下一步：重启 App 验收。
        """
        let entry = ATMTodoProgressEntry.parse(from: content).first
        XCTAssertEqual(entry?.nextAction, "重启 App 验收。")
    }

    func testDecodesNowSnapshotAndBuildsWorkLists() throws {
        let data = Data(
            """
            {
              "generated_at": "2026-07-13T15:00:00+08:00",
              "working": [
                {
                  "id": "t1", "title": "Personal focus", "priority": "P0",
                  "status": "in_progress", "project": "atm",
                  "created": "2026-07-13"
                }
              ],
              "open": [],
              "review": [],
              "blocked": [{
                "id": "t2", "title": "Blocked", "priority": "P1", "status": "blocked",
                "project": "demo", "created": "2026-07-13"
              }],
              "due": [], "waiting": [],
              "summary": {
                "open": 0, "in_progress": 1, "review": 0, "blocked": 1,
                "due": 0, "waiting": 0, "maintenance": 0
              }
            }
            """.utf8
        )

        let snapshot = try JSONDecoder().decode(ATMNowSnapshot.self, from: data)
        XCTAssertEqual(snapshot.working.first?.id, "t1")
        XCTAssertEqual(snapshot.needsAction.map(\.id), ["t2"])
        XCTAssertEqual(snapshot.summary.actionable, 1)
        XCTAssertEqual(snapshot.allTodos.count, 2)
    }

    func testDashboardComputesClientShareAndMenuTitle() throws {
        let sessions = [
            ATMSessionSummary(shortID: "1", agent: "codex", project: "atm", createdAt: "", queryCount: 1, firstQuestion: nil),
            ATMSessionSummary(shortID: "2", agent: "codex", project: "atm", createdAt: "", queryCount: 1, firstQuestion: nil),
            ATMSessionSummary(shortID: "3", agent: "pi", project: "atm", createdAt: "", queryCount: 1, firstQuestion: nil),
        ]
        let workingTodo = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                """
                {
                  "id": "t1", "title": "Improve ATM", "priority": "P1",
                  "status": "in_progress", "project": "atm", "created": "2026-07-24"
                }
                """.utf8
            )
        )
        let work = ATMNowSnapshot(
            generatedAt: "2026-07-24T14:00:00+08:00",
            open: [],
            working: [workingTodo],
            waiting: [],
            review: [],
            blocked: [],
            due: [],
            summary: ATMWorkSummary(
                open: 0, inProgress: 1, waiting: 0,
                review: 0, blocked: 0, due: 0, maintenance: 0
            )
        )
        let live = ATMLiveStatus(
            sessions: [ATMLiveSession(tool: "Codex", sessionID: "x", project: "atm", model: nil, ageSeconds: 1, firstQuestion: nil)],
            time: "15:00"
        )
        let dashboard = ATMDashboardSnapshot(
            work: work,
            dayStats: [
                ATMDayStats(
                    date: "2026-07-13",
                    sessions: 19,
                    queries: 106,
                    inputTokens: 273_000_000,
                    outputTokens: 723_122,
                    costUSD: 278.9
                ),
            ],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            rangeData: [
                .today: ATMRangeData(
                    modelStats: [
                        ATMModelStats(model: "small", sessions: 1, inputTokens: 80, outputTokens: 20, cacheReadTokens: 40, costUSD: 1),
                        ATMModelStats(model: "large", sessions: 2, inputTokens: 180, outputTokens: 20, cacheReadTokens: 150, costUSD: 2),
                    ],
                    sessions: sessions,
                    skillStats: [
                        ATMSkillStats(skill: "atm", calls: 4, sessions: 2, agents: 2),
                        ATMSkillStats(skill: "example-chat", calls: 1, sessions: 1, agents: 1),
                    ],
                    projectStats: [
                        ATMProjectStats(
                            project: "atm", agent: "codex", sessions: 2, queries: 7,
                            inputTokens: 100, outputTokens: 10, cacheReadTokens: 70, costUSD: 3
                        ),
                        ATMProjectStats(
                            project: "atm", agent: "claude", sessions: 1, queries: 5,
                            inputTokens: 90, outputTokens: 10, cacheReadTokens: 50, costUSD: 2
                        ),
                        ATMProjectStats(
                            project: "wanda", agent: "codex", sessions: 1,
                            inputTokens: 40, outputTokens: 5, cacheReadTokens: 10, costUSD: 1
                        ),
                    ]
                ),
            ],
            liveStatus: live,
            currentSession: nil,
            refreshedAt: Date()
        )

        XCTAssertEqual(dashboard.menuBarTitle, "1 · 273.7M")
        XCTAssertTrue(dashboard.menuBarTooltip.contains("1 项进行中"))
        XCTAssertTrue(dashboard.menuBarTooltip.contains("1 个 Agent 会话"))
        XCTAssertTrue(dashboard.menuBarTooltip.contains("今日用量"))
        // Healthy/missing index health stays quiet — only problems surface.
        XCTAssertFalse(dashboard.menuBarTooltip.contains("索引新鲜"))
        XCTAssertFalse(dashboard.menuBarTooltip.contains("索引状态未知"))
        XCTAssertEqual(dashboard.sortedModelStats(for: .today).map(\.model), ["large", "small"])
        XCTAssertEqual(dashboard.skillStats(for: .today).map(\.skill), ["atm", "example-chat"])
        XCTAssertEqual(dashboard.skillCallTotal(for: .today), 5)

        // The model view ranks by tokens and keeps each model's measured cache share.
        let models = dashboard.breakdown(for: .today, dimension: .model)
        XCTAssertEqual(models.map(\.label), ["large", "small"])
        XCTAssertEqual(dashboard.breakdownTokenTotal(for: .today, dimension: .model), 300)
        XCTAssertEqual(try XCTUnwrap(models.first?.cacheShare), 150.0 / 200.0, accuracy: 0.0001)

        // The client view folds every model of a client together, and counts each
        // session once instead of once per model.
        let clients = dashboard.breakdown(for: .today, dimension: .client)
        XCTAssertEqual(clients.map(\.label), ["未知客户端"])
        XCTAssertEqual(clients.first?.totalTokens, 300)
        XCTAssertEqual(clients.first?.sessions, 0)

        // The project view reads the project stats the dashboard carries, folding the
        // per-client rows of one project together.
        let projects = dashboard.breakdown(for: .today, dimension: .project)
        XCTAssertEqual(projects.map(\.label), ["atm", "wanda"])
        XCTAssertEqual(projects.first?.totalTokens, 210)
        XCTAssertEqual(projects.first?.sessions, 3)
        XCTAssertEqual(projects.first?.subtitle, "Claude · Codex")
        XCTAssertEqual(try XCTUnwrap(projects.first?.cacheShare), 120.0 / 210.0, accuracy: 0.0001)

        // Selecting a series narrows the metric cards to it, so the totals above the
        // chart never describe a wider scope than the chart does.
        let scoped = dashboard.summary(for: .today, dimension: .project, series: "atm")
        XCTAssertEqual(scoped.totalTokens, 210)
        XCTAssertEqual(scoped.outputTokens, 20)
        XCTAssertEqual(scoped.sessions, 3)
        XCTAssertEqual(scoped.costUSD, 5)
        // No series selected means the page is back to the unscoped totals.
        XCTAssertEqual(
            dashboard.summary(for: .today, dimension: .project, series: nil).totalTokens,
            dashboard.summary(for: .today).totalTokens
        )

        // The card set follows the lens. Unfiltered dimensions lead with how many
        // series they have; the model lens has no honest session count, because one
        // session can use several models.
        XCTAssertEqual(
            dashboard.usageMetrics(for: .today, lens: .total),
            [.tokens(273_723_122), .output(723_122), .cacheHitRate(0), .sessions(3), .queries(106), .cost(278.9)]
        )
        XCTAssertEqual(
            dashboard.usageMetrics(for: .today, lens: .model),
            [.seriesCount(2, "模型"), .tokens(273_723_122), .output(723_122), .cacheHitRate(0), .queries(106), .cost(278.9)]
        )
        XCTAssertEqual(
            dashboard.usageMetrics(for: .today, lens: .model, series: "large"),
            [.tokens(200), .output(20), .cacheHitRate(150.0 / 180.0), .sessions(2), .cost(2)]
        )
        // `atm stats` counts queries per project, so that card only exists here -- the
        // model and client rows have no query count to show.
        XCTAssertEqual(
            dashboard.usageMetrics(for: .today, lens: .project, series: "atm"),
            [.tokens(210), .output(20), .cacheHitRate(120.0 / 190.0), .sessions(3), .queries(12), .cost(5)]
        )
        XCTAssertFalse(
            dashboard.usageMetrics(for: .today, lens: .client, series: "未知客户端")
                .contains { if case .queries = $0 { return true } else { return false } }
        )
    }

    func testRangeSummaryAndTrendSelection() {
        let days = (1...8).map { day in
            ATMDayStats(
                date: String(format: "2026-07-%02d", day),
                sessions: day,
                queries: day,
                inputTokens: day * 10,
                outputTokens: day,
                cacheReadTokens: day * 5,
                costUSD: Double(day)
            )
        }
        let sessions = (1...3).map {
            ATMSessionSummary(shortID: "\($0)", agent: "codex", project: "atm", createdAt: "", queryCount: 1, firstQuestion: nil)
        }
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: days,
            hourStats: [
                ATMDayStats(date: "2026-07-08 14:00", sessions: 1, queries: 1, inputTokens: 20, outputTokens: 2, costUSD: 0.1),
                ATMDayStats(date: "2026-07-08 15:00", sessions: 1, queries: 1, inputTokens: 30, outputTokens: 3, costUSD: 0.2),
            ],
            modelDayStats: [
                ATMModelDayStats(date: "2026-07-01", model: "large", sessions: 1, inputTokens: 100, outputTokens: 10, cacheReadTokens: 50, costUSD: 1),
                ATMModelDayStats(date: "2026-07-02", model: "large", sessions: 1, inputTokens: 80, outputTokens: 8, cacheReadTokens: 40, costUSD: 0.8),
                ATMModelDayStats(date: "2026-07-02", model: "small", sessions: 1, inputTokens: 30, outputTokens: 3, cacheReadTokens: 10, costUSD: 0.2),
                ATMModelDayStats(date: "2026-07-08", model: "small", sessions: 1, inputTokens: 20, outputTokens: 2, cacheReadTokens: 8, costUSD: 0.1),
            ],
            modelHourStats: [
                ATMModelDayStats(date: "2026-07-08 14:00", model: "small", sessions: 1, inputTokens: 20, outputTokens: 2, cacheReadTokens: 8, costUSD: 0.1),
                ATMModelDayStats(date: "2026-07-08 15:00", model: "large", sessions: 1, inputTokens: 30, outputTokens: 3, cacheReadTokens: 10, costUSD: 0.2),
            ],
            rangeData: [
                // Windows carry their own dates now, so these exercise the date
                // filtering rather than a trailing count.
                .today: ATMRangeData(startDate: "2026-07-08", endDate: "2026-07-08", modelStats: [], sessions: []),
                .thisWeek: ATMRangeData(startDate: "2026-07-02", endDate: "2026-07-08", modelStats: [], sessions: sessions),
                .last30Days: ATMRangeData(startDate: "2026-06-09", endDate: "2026-07-08", modelStats: [], sessions: sessions),
            ],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        XCTAssertEqual(dashboard.stats(for: .today).count, 1)
        XCTAssertEqual(dashboard.stats(for: .thisWeek).count, 7)
        XCTAssertEqual(dashboard.trendStats(for: .today).count, 2)
        XCTAssertEqual(dashboard.trendStats(for: .last30Days).count, 8)
        XCTAssertEqual(dashboard.summary(for: .thisWeek).sessions, 3)
        XCTAssertEqual(dashboard.summary(for: .thisWeek).queries, 35)
        XCTAssertEqual(dashboard.summary(for: .thisWeek).totalTokens, 385)
        XCTAssertEqual(dashboard.summary(for: .thisWeek).cacheReadTokens, 175)
        XCTAssertEqual(dashboard.summary(for: .thisWeek).cacheHitRate, 0.5, accuracy: 0.0001)
        XCTAssertEqual(ATMMetricsRange.last30Days.pickerTitle, "近 30 日")
        XCTAssertEqual(ATMMetricsRange.today.tokenTrendTitle, "今日分时用量")
        // The menu bar's segmented control gets three windows and short labels;
        // anything wider overflowed the 300pt panel instead of shrinking.
        XCTAssertEqual(ATMMetricsRange.compact, [.today, .last7Days, .last30Days])
        XCTAssertEqual(ATMMetricsRange.compact.map(\.compactTitle), ["今日", "7 天", "30 天"])
        XCTAssertEqual(dashboard.seriesNames(for: .thisWeek, dimension: .model), ["large", "small"])
        let selectedLine = dashboard.lineTrendStats(for: .thisWeek, dimension: .model, selectedSeries: "small")
        XCTAssertEqual(selectedLine.count, 7)
        XCTAssertEqual(selectedLine.first?.totalTokens, 33)
        XCTAssertEqual(selectedLine[1].totalTokens, 0)
        XCTAssertEqual(selectedLine.last?.totalTokens, 22)
    }

    /// Yesterday is a single day and is labelled "昨日分时用量", so it has to be drawn
    /// in the same hour buckets today is. The hours are selected by the window's own
    /// dates: taking them wholesale would draw today's shape under yesterday's label.
    func testYesterdayTrendUsesItsOwnHourBuckets() {
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: [
                ATMDayStats(date: "2026-07-07", sessions: 2, queries: 4, inputTokens: 400, outputTokens: 40, costUSD: 4),
                ATMDayStats(date: "2026-07-08", sessions: 1, queries: 2, inputTokens: 200, outputTokens: 20, costUSD: 2),
            ],
            hourStats: [
                ATMDayStats(date: "2026-07-07 09:00", sessions: 1, queries: 2, inputTokens: 100, outputTokens: 10, costUSD: 1),
                ATMDayStats(date: "2026-07-07 10:00", sessions: 1, queries: 1, inputTokens: 300, outputTokens: 30, costUSD: 3),
                ATMDayStats(date: "2026-07-08 11:00", sessions: 1, queries: 2, inputTokens: 200, outputTokens: 20, costUSD: 2),
            ],
            modelDayStats: [
                ATMModelDayStats(date: "2026-07-07", model: "large", sessions: 2, inputTokens: 400, outputTokens: 40, cacheReadTokens: 0, costUSD: 4),
            ],
            modelHourStats: [
                ATMModelDayStats(date: "2026-07-07 09:00", model: "large", sessions: 1, inputTokens: 100, outputTokens: 10, cacheReadTokens: 0, costUSD: 1),
                ATMModelDayStats(date: "2026-07-07 10:00", model: "large", sessions: 1, inputTokens: 300, outputTokens: 30, cacheReadTokens: 0, costUSD: 3),
                ATMModelDayStats(date: "2026-07-08 11:00", model: "small", sessions: 1, inputTokens: 200, outputTokens: 20, cacheReadTokens: 0, costUSD: 2),
            ],
            projectDayStats: [
                ATMProjectDayStats(date: "2026-07-07", project: "atm", sessions: 2, inputTokens: 400, outputTokens: 40, costUSD: 4),
            ],
            projectHourStats: [
                ATMProjectDayStats(date: "2026-07-07 09:00", project: "atm", sessions: 1, inputTokens: 100, outputTokens: 10, costUSD: 1),
                ATMProjectDayStats(date: "2026-07-08 11:00", project: "wanda", sessions: 1, inputTokens: 200, outputTokens: 20, costUSD: 2),
            ],
            rangeData: [
                .today: ATMRangeData(startDate: "2026-07-08", endDate: "2026-07-08", modelStats: [], sessions: []),
                .yesterday: ATMRangeData(startDate: "2026-07-07", endDate: "2026-07-07", modelStats: [], sessions: []),
                .thisWeek: ATMRangeData(startDate: "2026-07-06", endDate: "2026-07-08", modelStats: [], sessions: []),
            ],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        XCTAssertEqual(dashboard.trendStats(for: .yesterday).map(\.date), ["2026-07-07 09:00", "2026-07-07 10:00"])
        XCTAssertEqual(dashboard.trendStats(for: .today).map(\.date), ["2026-07-08 11:00"])
        // A period keeps day buckets even though hours are in the snapshot.
        XCTAssertEqual(dashboard.trendStats(for: .thisWeek).map(\.date), ["2026-07-07", "2026-07-08"])
        // The chart's own totals: yesterday's day bucket held 440 tokens, and its two
        // hour buckets have to add up to the same number rather than to today's.
        let modelLine = dashboard.lineTrendStats(for: .yesterday, dimension: .model)
        XCTAssertEqual(modelLine.map(\.date), ["2026-07-07 09:00", "2026-07-07 10:00"])
        XCTAssertEqual(modelLine.reduce(0) { $0 + $1.totalTokens }, 440)
        XCTAssertEqual(dashboard.seriesNames(for: .yesterday, dimension: .model), ["large"])
        // Projects read their own hour rows, so yesterday's project line cannot pick
        // up today's project.
        XCTAssertEqual(dashboard.seriesNames(for: .yesterday, dimension: .project), ["atm"])
    }

    /// A single day the snapshot has no hours for -- the app is reading a window the
    /// CLI stopped sending hourly rows that far back -- falls back to its one day
    /// bucket instead of borrowing the hours of a day it can see.
    func testSingleDayWithoutHourBucketsFallsBackToDayBucket() {
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: [
                ATMDayStats(date: "2026-07-07", sessions: 2, queries: 4, inputTokens: 400, outputTokens: 40, costUSD: 4),
                ATMDayStats(date: "2026-07-08", sessions: 1, queries: 2, inputTokens: 200, outputTokens: 20, costUSD: 2),
            ],
            hourStats: [
                ATMDayStats(date: "2026-07-08 11:00", sessions: 1, queries: 2, inputTokens: 200, outputTokens: 20, costUSD: 2),
            ],
            modelDayStats: [
                ATMModelDayStats(date: "2026-07-07", model: "large", sessions: 2, inputTokens: 400, outputTokens: 40, cacheReadTokens: 0, costUSD: 4),
            ],
            modelHourStats: [
                ATMModelDayStats(date: "2026-07-08 11:00", model: "small", sessions: 1, inputTokens: 200, outputTokens: 20, cacheReadTokens: 0, costUSD: 2),
            ],
            rangeData: [
                .today: ATMRangeData(startDate: "2026-07-08", endDate: "2026-07-08", modelStats: [], sessions: []),
                .yesterday: ATMRangeData(startDate: "2026-07-07", endDate: "2026-07-07", modelStats: [], sessions: []),
            ],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        XCTAssertEqual(dashboard.trendStats(for: .yesterday).map(\.date), ["2026-07-07"])
        let modelLine = dashboard.lineTrendStats(for: .yesterday, dimension: .model)
        XCTAssertEqual(modelLine.map(\.date), ["2026-07-07"])
        XCTAssertEqual(modelLine.reduce(0) { $0 + $1.totalTokens }, 440)
        // And the axis reads the buckets, so that one tick is dated rather than 00:00.
        XCTAssertFalse(ATMUsageDateAxis.isHourly(modelLine.map(\.date)))
        XCTAssertTrue(ATMUsageDateAxis.isHourly(dashboard.trendStats(for: .today).map(\.date)))
    }

    func testUsageSeriesSwitchesBetweenModelClientAndProject() {
        let dayStats = (1...2).map { day in
            ATMDayStats(
                date: String(format: "2026-07-0%d", day),
                sessions: 1, queries: 1, inputTokens: 100, outputTokens: 10, costUSD: 1
            )
        }
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: dayStats,
            hourStats: [],
            modelDayStats: [
                ATMModelDayStats(date: "2026-07-01", client: "codex", model: "large", sessions: 1, inputTokens: 100, outputTokens: 10, cacheReadTokens: 50, costUSD: 1),
                ATMModelDayStats(date: "2026-07-01", client: "codex", model: "small", sessions: 1, inputTokens: 20, outputTokens: 2, cacheReadTokens: 5, costUSD: 0.2),
                ATMModelDayStats(date: "2026-07-02", client: "claude", model: "large", sessions: 1, inputTokens: 30, outputTokens: 3, cacheReadTokens: 10, costUSD: 0.4),
            ],
            modelHourStats: [],
            projectDayStats: [
                ATMProjectDayStats(date: "2026-07-01", client: "codex", project: "atm", sessions: 2, inputTokens: 120, outputTokens: 12, cacheReadTokens: 55, costUSD: 1.2),
                ATMProjectDayStats(date: "2026-07-02", client: "claude", project: "", sessions: 1, inputTokens: 30, outputTokens: 3, cacheReadTokens: 10, costUSD: 0.4),
            ],
            projectHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        // Model series keep the client in the label, because the same model name can
        // be served by two clients at different prices.
        XCTAssertEqual(
            dashboard.seriesNames(for: .thisWeek, dimension: .model),
            ["large · codex", "large · claude", "small · codex"]
        )
        // The client view collapses that day's two codex models into one point,
        // and uses display names (Codex/Grok) rather than raw agent ids.
        XCTAssertEqual(dashboard.seriesNames(for: .thisWeek, dimension: .client), ["Codex", "Claude"])
        let codex = dashboard.lineTrendStats(for: .thisWeek, dimension: .client, selectedSeries: "Codex")
        XCTAssertEqual(codex.count, 2)
        XCTAssertEqual(codex.first?.totalTokens, 132)
        XCTAssertEqual(codex.last?.totalTokens, 0)
        // A session with no project still has to appear somewhere.
        XCTAssertEqual(dashboard.seriesNames(for: .thisWeek, dimension: .project), ["atm", "未归类"])
        XCTAssertEqual(
            dashboard.seriesTotals(for: .thisWeek, dimension: .project, series: "atm")?.totalTokens,
            132
        )
    }

    /// The speed line is drawn from two sums, so merging models — or hours into
    /// days — has to divide totals. Averaging the per-bucket rates would let a
    /// bucket with three requests outweigh one with three hundred.
    func testSpeedSeriesDividesSumsRatherThanAveragingRates() {
        let dayStats = [
            ATMDayStats(date: "2026-07-01", sessions: 1, queries: 1, inputTokens: 100, outputTokens: 10, costUSD: 1),
        ]
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: dayStats,
            hourStats: [],
            modelDayStats: [
                // 1000 tokens in 10s (100 tok/s) and 100 tokens in 90s (1.1 tok/s).
                // Together that is 1100 tokens over 100s — 11 tok/s — where averaging
                // the two rates would claim about 51.
                ATMModelDayStats(
                    date: "2026-07-01", client: "codex", model: "fast", sessions: 1,
                    inputTokens: 100, outputTokens: 1000, cacheReadTokens: 0, costUSD: 1,
                    measuredOutputTokens: 1000, measuredDurationMS: 10_000
                ),
                ATMModelDayStats(
                    date: "2026-07-01", client: "codex", model: "slow", sessions: 1,
                    inputTokens: 100, outputTokens: 100, cacheReadTokens: 0, costUSD: 1,
                    measuredOutputTokens: 100, measuredDurationMS: 90_000
                ),
            ],
            modelHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        let fast = dashboard.seriesTotals(for: .thisWeek, dimension: .model, series: "fast · codex")
        XCTAssertEqual(fast?.tokensPerSecond ?? 0, 100, accuracy: 0.001)
        // Merged into one client: 1100 tokens over 100s, not the mean of 100 and ~1.1.
        let client = dashboard.seriesTotals(for: .thisWeek, dimension: .client, series: "Codex")
        XCTAssertEqual(client?.tokensPerSecond ?? 0, 11, accuracy: 0.001)
    }

    /// A bucket nothing could be measured in has no speed. It must stay nil so the
    /// chart leaves a gap instead of drawing a drop to zero.
    func testSpeedIsAbsentRatherThanZeroWhenNothingWasMeasured() {
        let point = ATMUsageSeriesPoint(
            date: "2026-07-01", series: "grok", sessions: 1,
            inputTokens: 100, outputTokens: 500, cacheReadTokens: 0, costUSD: 1
        )
        XCTAssertNil(point.tokensPerSecond)
        XCTAssertEqual(point.totalTokens, 600)
    }

    /// The trend chart reads its series through `filteredLineTrendStats` even
    /// with no filters. That path used to drop the measured fields, which made
    /// every bucket unmeasurable and left the 速度 reading permanently empty.
    func testFilteredTrendStatsCarryMeasuredSpeedFields() {
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: [
                ATMDayStats(date: "2026-07-01", sessions: 1, queries: 1, inputTokens: 100, outputTokens: 10, costUSD: 1),
            ],
            hourStats: [],
            modelDayStats: [
                ATMModelDayStats(
                    date: "2026-07-01", client: "grokbuild", model: "grok-4.5", sessions: 1,
                    inputTokens: 100, outputTokens: 1000, cacheReadTokens: 0, costUSD: 1,
                    measuredOutputTokens: 1000, measuredDurationMS: 10_000
                ),
            ],
            modelHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        let points = dashboard.filteredLineTrendStats(for: .thisWeek, filters: ATMUsageFilters())
        let measured = points.filter { $0.tokensPerSecond != nil }
        XCTAssertEqual(measured.count, 1)
        XCTAssertEqual(measured.first?.tokensPerSecond ?? 0, 100, accuracy: 0.001)
    }

    func testSpeedStatsCombineModelsAndWeightTurnWaits() throws {
        let data = Data(
            """
            {"models":[
              {"client":"codex","model":"gpt-5","requests":10,"sampled":8,"tokens_per_second_p50":40,
               "tokens_per_second_p90":60,"duration_p50_seconds":7,"output_tokens":800,"sampled_seconds":20},
              {"client":"claude","model":"opus","requests":4,"sampled":0,"tokens_per_second_p50":0,
               "tokens_per_second_p90":0,"duration_p50_seconds":0,"output_tokens":0,"sampled_seconds":0}
             ],
             "turns":[
              {"agent":"codex","turns":9,"wait_p50_seconds":100,"wait_p90_seconds":400},
              {"agent":"claude","turns":1,"wait_p50_seconds":200,"wait_p90_seconds":300}
             ],
             "untimed_requests":4,"out_of_window_requests":2}
            """.utf8
        )
        let speed = try JSONDecoder().decode(ATMSpeedStats.self, from: data)
        // 800 tokens over 20s; the unmeasured model contributes nothing either way.
        XCTAssertEqual(speed.tokensPerSecond() ?? 0, 40, accuracy: 0.001)
        XCTAssertEqual(speed.tokensPerSecond { $0.model == "gpt-5" } ?? 0, 40, accuracy: 0.001)
        XCTAssertNil(speed.tokensPerSecond { $0.model == "opus" })
        // Weighted by turn count: nine 100s turns and one 200s turn.
        XCTAssertEqual(speed.turnWaitSeconds() ?? 0, 110, accuracy: 0.001)
        XCTAssertEqual(speed.turnWaitSeconds { $0.agent == "claude" } ?? 0, 200, accuracy: 0.001)
    }

    /// Speed cards are only shown where the number would mean what its heading
    /// says: a project has no per-project measurement, and one turn spans models.
    func testSpeedCardsAppearOnlyWhereTheyCanBeScopedHonestly() throws {
        let speedData = Data(
            """
            {"models":[{"client":"codex","model":"gpt-5","requests":4,"sampled":4,
              "tokens_per_second_p50":50,"tokens_per_second_p90":60,"duration_p50_seconds":5,
              "output_tokens":500,"sampled_seconds":10}],
             "turns":[{"agent":"codex","turns":3,"wait_p50_seconds":90,"wait_p90_seconds":120}],
             "untimed_requests":0,"out_of_window_requests":0}
            """.utf8
        )
        let speed = try JSONDecoder().decode(ATMSpeedStats.self, from: speedData)
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: [ATMDayStats(date: "2026-07-01", sessions: 1, queries: 1, inputTokens: 100, outputTokens: 500, costUSD: 1)],
            hourStats: [],
            modelDayStats: [
                ATMModelDayStats(
                    date: "2026-07-01", client: "codex", model: "gpt-5", sessions: 1,
                    inputTokens: 100, outputTokens: 500, cacheReadTokens: 0, costUSD: 1,
                    measuredOutputTokens: 500, measuredDurationMS: 10_000
                ),
            ],
            modelHourStats: [],
            rangeData: [
                .thisWeek: ATMRangeData(
                    modelStats: [ATMModelStats(client: "codex", model: "gpt-5", sessions: 1, inputTokens: 100, outputTokens: 500, cacheReadTokens: 0, costUSD: 1)],
                    sessions: [],
                    speed: speed
                ),
            ],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        let unfiltered = dashboard.usageMetrics(for: .thisWeek, filters: ATMUsageFilters())
        XCTAssertTrue(unfiltered.contains(.throughput(50)))
        XCTAssertTrue(unfiltered.contains(.turnWait(90)))

        // Scoped to a model: the rate is that model's, but the turn is not.
        let byModel = dashboard.usageMetrics(
            for: .thisWeek,
            filters: ATMUsageFilters(model: "gpt-5")
        )
        XCTAssertTrue(byModel.contains(.throughput(50)))
        XCTAssertFalse(byModel.contains(.turnWait(90)))

        // Scoped to a project: neither number is measured per project.
        let byProject = dashboard.usageMetrics(
            for: .thisWeek,
            filters: ATMUsageFilters(project: "atm")
        )
        XCTAssertFalse(byProject.contains(.throughput(50)))
        XCTAssertFalse(byProject.contains(.turnWait(90)))
    }

    func testDurationFormatKeepsSecondsPreciseAndCompactsHours() {
        XCTAssertEqual(NumberFormat.duration(0.4), "0.4s")
        XCTAssertEqual(NumberFormat.duration(8), "8s")
        XCTAssertEqual(NumberFormat.duration(95), "1m35s")
        XCTAssertEqual(NumberFormat.duration(120), "2m")
        XCTAssertEqual(NumberFormat.duration(3_600), "1h")
        XCTAssertEqual(NumberFormat.duration(5_400), "1h30m")
    }

    func testDayStatsTotalTokens() throws {
        let data = Data(
            """
            {"date":"2026-07-13","sessions":4,"queries":8,"input_tokens":100,"output_tokens":25,"cost_usd":1.5}
            """.utf8
        )
        let stats = try JSONDecoder().decode(ATMDayStats.self, from: data)
        XCTAssertEqual(stats.totalTokens, 125)
        XCTAssertEqual(stats.cacheReadTokens, 0)
        XCTAssertEqual(NumberFormat.compact(1_250_000), "1.2M")
    }

    func testModelStatsDecodesCacheShare() throws {
        let data = Data(
            """
            {"client":"codex","model":"gpt-5.6-sol","sessions":12,"input_tokens":265934128,"output_tokens":760234,"cache_read_tokens":252000000,"cost_usd":556.1}
            """.utf8
        )
        let stats = try JSONDecoder().decode(ATMModelStats.self, from: data)
        XCTAssertEqual(stats.client, "codex")
        XCTAssertEqual(stats.displayName, "gpt-5.6-sol · codex")
        XCTAssertEqual(stats.totalTokens, 266_694_362)
        XCTAssertEqual(stats.cacheShare, Double(252_000_000) / Double(266_694_362), accuracy: 0.0001)
    }

    func testSkillStatsDecodesCLIOutput() throws {
        let data = Data(#"{"skill":"atm","calls":7,"sessions":3,"agents":2}"#.utf8)
        let stats = try JSONDecoder().decode(ATMSkillStats.self, from: data)
        XCTAssertEqual(stats.skill, "atm")
        XCTAssertEqual(stats.calls, 7)
        XCTAssertEqual(stats.sessions, 3)
        XCTAssertEqual(stats.agents, 2)
    }

    func testUsageSeriesKeepsOnlyTheTopSeriesInTheChart() {
        let dayStats = [
            ATMDayStats(date: "2026-07-13", sessions: 1, queries: 1, inputTokens: 60, outputTokens: 6, costUSD: 1),
        ]
        let modelStats = [
            ATMModelDayStats(date: "2026-07-13", model: "a", sessions: 1, inputTokens: 30, outputTokens: 3, cacheReadTokens: 10, costUSD: 0.5),
            ATMModelDayStats(date: "2026-07-13", model: "b", sessions: 1, inputTokens: 20, outputTokens: 2, cacheReadTokens: 5, costUSD: 0.3),
            ATMModelDayStats(date: "2026-07-13", model: "c", sessions: 1, inputTokens: 10, outputTokens: 1, cacheReadTokens: 2, costUSD: 0.2),
        ]
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: dayStats,
            hourStats: [],
            modelDayStats: modelStats,
            modelHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        // The filter menu still offers every series, while the chart draws the top N.
        XCTAssertEqual(dashboard.seriesNames(for: .today, dimension: .model), ["a", "b", "c"])
        let trend = dashboard.lineTrendStats(for: .today, dimension: .model, topSeriesCount: 2)
        XCTAssertEqual(trend.map(\.series), ["a", "b"])
        XCTAssertEqual(trend.first?.totalTokens, 33)
    }

    func testLiveSessionHidesTheUserInputThatIsAlreadyTheTitleWithoutBlindingTurnTrackers() throws {
        let data = Data(
            """
            {
              "tool":"Claude","session_id":"91b0","project":"atm",
              "client":"iTerm","age_seconds":9,
              "last_q":"Rework the notch state badges"
            }
            """.utf8
        )

        let session = try JSONDecoder().decode(ATMLiveSession.self, from: data)
        // No summary, so presenceTitle falls back to the question. A card that
        // rendered both would print the same sentence twice.
        XCTAssertEqual(session.presenceTitle, "Rework the notch state badges")
        XCTAssertNil(session.latestUserInputBelowTitle)
        // `ATMAgentSoundTransitionTracker` diffs this to tell one turn from the
        // next, so suppressing the card must not blank it out.
        XCTAssertEqual(session.latestUserInputText, "Rework the notch state badges")
    }

    func testActiveAgentPresenceOrderDoesNotChangeWhenActivityAgesRefresh() {
        func active(_ id: String, age: Int) -> ATMLiveSession {
            ATMLiveSession(
                tool: "Codex",
                sessionID: id,
                project: "atm",
                ageSeconds: age,
                activityState: "active"
            )
        }

        let firstPoll = ATMAgentPresenceOrdering.sorted([
            active("session-b", age: 2),
            active("session-a", age: 40),
        ])
        let nextPoll = ATMAgentPresenceOrdering.sorted([
            active("session-a", age: 1),
            active("session-b", age: 41),
        ])

        XCTAssertEqual(firstPoll.map(\.sessionID), ["session-a", "session-b"])
        XCTAssertEqual(nextPoll.map(\.sessionID), firstPoll.map(\.sessionID))
    }

    /// A bound session is titled by its Todo, not by whatever the conversation
    /// opened with. This used to also cover recovering a title from ATM's own
    /// dispatch preamble; ATM no longer writes the opening turn, so the only
    /// rule left is that the binding wins.
    func testBoundSessionPrefersDurableTodoTitleOverOpeningMessage() throws {
        let opening = """
        帮我把前进后退导航加上，顺便看下现在的路由怎么写的。
        """
        let boundData = try JSONSerialization.data(withJSONObject: [
            "tool": "Codex",
            "session_id": "bound-session",
            "project": "atm",
            "summary": "帮我把前进后退导航加上",
            "age_seconds": 3,
            "first_q": opening,
            "last_q": opening,
            "binding": [
                "session_id": "bound-session",
                "todo_id": "t252",
                "agent": "codex",
                "project": "atm",
                "bound_at": 1,
            ],
            "todo": [
                "id": "t252",
                "title": "我需要一个前进/后退功能",
                "project": "atm",
                "status": "in_progress",
            ],
        ])
        let bound = try JSONDecoder().decode(ATMLiveSession.self, from: boundData)

        XCTAssertEqual(bound.presenceTitle, "我需要一个前进/后退功能")
        XCTAssertEqual(bound.displayTitle, "我需要一个前进/后退功能")

        // Without a binding there is no durable title to prefer, so the session's
        // own text is shown as written rather than being rewritten.
        let unbound = ATMLiveSession(
            tool: "Codex",
            sessionID: "unbound-session",
            project: "atm",
            summary: "帮我把前进后退导航加上",
            ageSeconds: 3,
            firstQuestion: opening,
            lastQuestion: opening
        )
        XCTAssertEqual(unbound.presenceTitle, "帮我把前进后退导航加上")
    }

    func testLiveSessionDecodesAgentActivityAndBuildsDisplayState() throws {
        let data = Data(
            """
            {
              "tool":"Codex","session_id":"21a5890f","project":"atm",
              "resume_id":"019fc14d-58c0-7b60-ab69-2bab9f966af3",
              "client":"Codex Desktop","cwd":"/Users/tester/mox/atm",
              "model":"gpt-5.6-sol","summary":"Improve the Agent workspace",
              "age_seconds":7,"pid":"54007",
              "first_q":"# AGENTS.md instructions",
              "last_q":"Show what each Agent is doing",
              "last_a":"Inspecting the live session fields and updating the UI.",
              "latest_result":"The Agent workspace is ready.\\n\\n- Open the source session",
              "updates":["Inspecting the live session fields.","Inspecting the live session fields."],
              "tools":["rg","apply_patch"],"topics":["ATM UI","Agent status"]
            }
            """.utf8
        )

        let session = try JSONDecoder().decode(ATMLiveSession.self, from: data)
        XCTAssertEqual(session.phase, .active)
        XCTAssertEqual(session.displayTitle, "Improve the Agent workspace")
        XCTAssertEqual(session.currentObjective, "Show what each Agent is doing")
        XCTAssertEqual(session.progressText, "Inspecting the live session fields and updating the UI.")
        XCTAssertEqual(session.presenceTitle, "Improve the Agent workspace")
        XCTAssertEqual(session.latestUserInputText, "Show what each Agent is doing")
        // A summary is present, so the title is not the question: the card may
        // draw both.
        XCTAssertEqual(session.latestUserInputBelowTitle, "Show what each Agent is doing")
        XCTAssertEqual(session.latestReplyText, "Inspecting the live session fields and updating the UI.")
        XCTAssertEqual(session.latestResultText, "The Agent workspace is ready.\n\n- Open the source session")
        XCTAssertEqual(session.visibleUpdates, ["Inspecting the live session fields."])
        XCTAssertEqual(session.latestVisibleUpdate, "Inspecting the live session fields.")
        XCTAssertEqual(session.recentTools, ["rg", "apply_patch"])
        XCTAssertEqual(session.topics, ["ATM UI", "Agent status"])
        XCTAssertEqual(session.pid, "54007")
        XCTAssertEqual(session.presenceState, .active)
        XCTAssertEqual(session.resumeID, "019fc14d-58c0-7b60-ab69-2bab9f966af3")
        XCTAssertEqual(session.client, "Codex Desktop")
        XCTAssertEqual(session.cwd, "/Users/tester/mox/atm")
        XCTAssertEqual(
            ATMAgentSessionLaunchRoute.resolve(for: session),
            .codexThread(threadID: "019fc14d-58c0-7b60-ab69-2bab9f966af3")
        )

        let duplicate = ATMLiveSession(
            tool: "Codex",
            sessionID: "duplicate",
            project: "atm",
            summary: "Updating the Agent workspace",
            ageSeconds: 1,
            lastAnswer: "Updating the Agent workspace"
        )
        XCTAssertNil(duplicate.latestReplyText)
    }

    func testLiveSessionLatestVisibleUpdatePicksNewestDistinctStage() {
        let session = ATMLiveSession(
            tool: "Codex",
            sessionID: "latest-update",
            project: "atm",
            ageSeconds: 1,
            latestResult: "Implementation complete.",
            updates: [
                "Inspecting the Agent detail.",
                "Adding the overview activity card.",
                "Adding the overview activity card.",
                "Implementation complete.",
            ]
        )

        XCTAssertEqual(session.latestVisibleUpdate, "Adding the overview activity card.")
    }

    /// The Agent list hides a row's origin when it matches the column's dominant
    /// one, so these two labels have to agree exactly between the header and the
    /// rows — a whitespace-only `client` falling back differently in one of them
    /// would print `Codex Desktop · atm` on every card again.
    func testAgentOriginLabelsFallBackWhenClientOrProjectIsBlank() {
        let reported = ATMLiveSession(
            tool: "Codex",
            sessionID: "a",
            project: "atm",
            client: "Codex Desktop",
            ageSeconds: 1
        )
        XCTAssertEqual(ATMAgentDisplay.clientName(reported), "Codex Desktop")
        XCTAssertEqual(ATMAgentDisplay.projectName(reported), "atm")

        let blank = ATMLiveSession(
            tool: "Codex",
            sessionID: "b",
            project: "   ",
            client: "  ",
            ageSeconds: 1
        )
        XCTAssertEqual(ATMAgentDisplay.clientName(blank), "Codex")
        XCTAssertEqual(ATMAgentDisplay.projectName(blank), "未知项目")

        let missing = ATMLiveSession(tool: "qodercli", sessionID: "c", project: "atm", ageSeconds: 1)
        XCTAssertEqual(ATMAgentDisplay.clientName(missing), "QoderCLI")
    }

    /// Antigravity reports no TTY and no process, so before this route it fell
    /// through to "unavailable". It is a workspace editor whose summary index
    /// records the conversation's folder, which is enough to reopen the window —
    /// the same soft landing Qoder Work gets. It must be matched before the
    /// VS Code branch, since Antigravity is a VS Code fork.
    func testAgentSessionLaunchRouteOpensAntigravityAtItsWorkspace() {
        let session = ATMLiveSession(
            tool: "antigravity",
            sessionID: "9d5b8fb4-4502-45f1-ab70-8bf612c731b2",
            project: "atm",
            client: "Antigravity",
            cwd: "/Users/tester/mox/atm",
            ageSeconds: 12
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(
            route,
            .workspace(bundleIdentifier: "com.google.antigravity", path: "/Users/tester/mox/atm")
        )
        XCTAssertTrue(route.isAvailable)
        // Not exact: this reopens the workspace, not the conversation.
        XCTAssertFalse(route.isExact)
        XCTAssertTrue(route.destinationLabel.contains("Antigravity"))
    }

    /// Without a folder there is still something better than nothing: bring the
    /// editor forward rather than report the session unreachable.
    func testAgentSessionLaunchRouteFallsBackToAntigravityApplication() {
        let session = ATMLiveSession(
            tool: "antigravity",
            sessionID: "4eef93d9-80fc-4180-82ae-d9005ece9450",
            project: "",
            client: "Antigravity",
            ageSeconds: 40
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(route, .application(bundleIdentifier: "com.google.antigravity"))
        XCTAssertTrue(route.isAvailable)
    }

    func testAgentSessionLaunchRouteUsesExactTTYWhenAvailable() {
        let session = ATMLiveSession(
            tool: "Claude Code",
            sessionID: "abcd1234",
            resumeID: "abcd1234-full",
            project: "atm",
            client: "Claude Code",
            ageSeconds: 3,
            pid: "54007",
            tty: "/dev/ttys009",
            terminalApp: "com.googlecode.iterm2"
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(route, .terminal(bundleIdentifier: "com.googlecode.iterm2", tty: "ttys009"))
        XCTAssertTrue(route.isAvailable)
        XCTAssertTrue(route.isExact)
        XCTAssertEqual(route.actionTitle, "回到会话")
    }

    /// A bound session is history: its process is normally gone, so the only
    /// honest routes are "this thread can be reopened by id" (codex) or nothing.
    /// When the session does happen to still be live, the live route is exact and
    /// wins.
    func testBoundSessionRouteReopensCodexThreadsAndPrefersLiveSessions() throws {
        let sessions = try XCTUnwrap(
            JSONDecoder().decode(
                ATMTodoDetail.self,
                from: Data(
                    """
                    {
                      "sessions":[
                        {"session_id":"019fc6cb-c6ce-71e2-9b65-8f0af41c825b","short_id":"8f0af41c",
                         "agent":"codex","project":"atm","indexed":true,"binding_count":1,
                         "first_bound_at":100,"bound_at":100,"queries":1,"tool_calls":0},
                        {"session_id":"abcd1234-0000-0000-0000-00000000beef","short_id":"abcd1234",
                         "agent":"claude","project":"atm","indexed":true,"binding_count":1,
                         "first_bound_at":100,"bound_at":100,"queries":1,"tool_calls":0}
                      ]
                    }
                    """.utf8
                )
            ).sessions
        )
        let codexSession = sessions[0]
        let claudeSession = sessions[1]

        XCTAssertEqual(
            ATMAgentSessionLaunchRoute.resolve(for: codexSession, live: []),
            .codexThread(threadID: "019fc6cb-c6ce-71e2-9b65-8f0af41c825b")
        )

        let finished = ATMAgentSessionLaunchRoute.resolve(for: claudeSession, live: [])
        XCTAssertFalse(finished.isAvailable)
        XCTAssertTrue(finished.destinationLabel.contains("已经结束"))

        let live = ATMLiveSession(
            tool: "Claude Code",
            sessionID: "abcd1234",
            project: "atm",
            client: "Claude Code",
            ageSeconds: 3,
            pid: "54007",
            tty: "/dev/ttys009",
            terminalApp: "com.googlecode.iterm2"
        )
        XCTAssertEqual(
            ATMAgentSessionLaunchRoute.resolve(for: claudeSession, live: [live]),
            .terminal(bundleIdentifier: "com.googlecode.iterm2", tty: "ttys009")
        )
    }

    func testAgentSessionLaunchRouteDoesNotPretendUnknownSessionIsOpenable() {
        let session = ATMLiveSession(
            tool: "Claude Code",
            sessionID: "abcd1234",
            project: "atm",
            ageSeconds: 3
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertFalse(route.isAvailable)
        XCTAssertFalse(route.isExact)
        XCTAssertTrue(route.destinationLabel.contains("还没有采集到"))
    }

    func testAgentSessionLaunchRouteOpensGrokTerminalWhenTTYKnown() {
        let session = ATMLiveSession(
            tool: "Grok Build",
            sessionID: "019fc6b8-f793-7580-bd21-8704476912ec",
            project: "atm",
            client: "Grok Build",
            cwd: "/Users/tester/mox/atm",
            ageSeconds: 2,
            pid: "85914",
            tty: "ttys000",
            terminalApp: "com.googlecode.iterm2"
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(route, .terminal(bundleIdentifier: "com.googlecode.iterm2", tty: "ttys000"))
        XCTAssertTrue(route.isExact)
        XCTAssertEqual(route.actionTitle, "回到会话")
    }

    func testAgentSessionLaunchRouteFallsBackToTerminalCWDForGrok() {
        let session = ATMLiveSession(
            tool: "Grok Build",
            sessionID: "019fc6b8-f793-7580-bd21-8704476912ec",
            project: "atm",
            cwd: "/Users/tester/mox/atm",
            ageSeconds: 2
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(route, .workspace(bundleIdentifier: "com.apple.Terminal", path: "/Users/tester/mox/atm"))
        XCTAssertTrue(route.isAvailable)
        XCTAssertFalse(route.isExact)
        XCTAssertEqual(route.actionTitle, "打开来源")
    }

    func testAgentHookSourceMapsGrokTool() {
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "Grok Build"), "grokbuild")
        XCTAssertEqual(ATMAgentHookSource.source(forTool: "grokbuild"), "grokbuild")
        XCTAssertNil(ATMAgentHookSource.source(forTool: "Copilot"))
    }

    func testAgentSessionLaunchRouteFallsBackToKnownIDEWorkspace() {
        let session = ATMLiveSession(
            tool: "Claude Code",
            sessionID: "abcd1234",
            project: "atm",
            client: "Claude Code · VS Code",
            cwd: "/Users/tester/mox/atm",
            ageSeconds: 3
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        XCTAssertEqual(
            route,
            .workspace(bundleIdentifier: "com.microsoft.VSCode", path: "/Users/tester/mox/atm")
        )
        XCTAssertTrue(route.isAvailable)
        XCTAssertFalse(route.isExact)
        XCTAssertEqual(route.actionTitle, "打开来源")
    }

    func testLiveSessionFallsBackFromInjectedInstructionsAndDetectsIdle() throws {
        let session = ATMLiveSession(
            tool: "Codex",
            sessionID: "waiting",
            project: "atm",
            model: "codex-auto-review",
            ageSeconds: 180,
            firstQuestion: "# AGENTS.md instructions\n<INSTRUCTIONS>system context</INSTRUCTIONS>",
            lastQuestion: "Some conversation entries were omitted.",
            lastAnswer: "Review completed."
        )

        XCTAssertEqual(session.phase, .idle)
        XCTAssertEqual(session.displayTitle, "Codex 会话")
        XCTAssertEqual(session.currentObjective, "暂未提取到当前目标")
        XCTAssertFalse(session.needsUserAttention)
        XCTAssertTrue(session.needsUserText.contains("未检测到"))
        XCTAssertEqual(session.presenceState, .recent)
    }

    func testLiveSessionSkipsCodexTranscriptDeltaPlaceholder() {
        let session = ATMLiveSession(
            tool: "Codex",
            sessionID: "placeholder",
            project: "atm",
            summary: "<no retained transcript delta entries>",
            ageSeconds: 8,
            lastQuestion: "改进动态页"
        )

        XCTAssertEqual(session.displayTitle, "改进动态页")
        XCTAssertEqual(session.currentObjective, "改进动态页")
        XCTAssertEqual(session.presenceTitle, "改进动态页")
    }

    func testLiveSessionOnlyRequestsUserWhenAnswerContainsExplicitSignal() {
        let session = ATMLiveSession(
            tool: "Codex",
            sessionID: "needs-user",
            project: "atm",
            ageSeconds: 180,
            lastAnswer: "实现已经准备好，请确认是否继续发布。"
        )

        XCTAssertEqual(session.phase, .idle)
        XCTAssertTrue(session.needsUserAttention)
        XCTAssertTrue(session.needsUserText.contains("需要确认"))
        XCTAssertEqual(session.presenceState, .attention)
    }

    func testCurrentSessionDecodesAndCreatesBoundAgentState() throws {
        let data = Data(
            """
            {
              "binding": {
                "session_id":"019f73d5","todo_id":"t80","agent":"codex",
                "project":"atm","cwd":"/Users/tester/mox/atm","bound_at":1784354861
              },
              "bound":true,
              "todo":{"id":"t80","title":"ATM 持续优化迭代","project":"atm","status":"in_progress"}
            }
            """.utf8
        )
        let current = try JSONDecoder().decode(ATMCurrentSession.self, from: data)
        XCTAssertTrue(current.bound)
        XCTAssertEqual(current.state, "bound")
        XCTAssertEqual(current.binding?.todoID, "t80")
        XCTAssertEqual(current.todo?.title, "ATM 持续优化迭代")

        let fallback = ATMLiveSession(
            tool: "Codex",
            sessionID: current.binding?.sessionID ?? "",
            project: current.binding?.project ?? "",
            summary: current.todo?.title,
            ageSeconds: Int.max,
            activityState: "unobserved",
            bindingState: "bound",
            binding: current.binding,
            todo: current.todo
        )
        XCTAssertEqual(fallback.phase, .bound)
        XCTAssertTrue(fallback.progressText.contains("T80"))
    }

    func testLiveActivityBindingAndTodoStateStayIndependent() throws {
        let activeData = Data(
            """
            {
              "tool":"Codex","session_id":"019f73d5","project":"atm","age_seconds":7,
              "activity_state":"active","binding_state":"bound",
              "binding":{"session_id":"019f73d5","todo_id":"t95","agent":"codex","project":"atm","bound_at":1},
              "todo":{"id":"t95","title":"State semantics","project":"atm","status":"in_progress"}
            }
            """.utf8
        )
        let active = try JSONDecoder().decode(ATMLiveSession.self, from: activeData)
        XCTAssertEqual(active.phase, .active)
        XCTAssertEqual(active.bindingTodoID, "t95")
        XCTAssertEqual(active.todo?.status, "in_progress")

        let staleData = Data(
            """
            {
              "tool":"Codex","session_id":"019f73d5","project":"atm","age_seconds":180,
              "activity_state":"idle","binding_state":"todo_not_in_progress",
              "binding":{"session_id":"019f73d5","todo_id":"t95","agent":"codex","project":"atm","bound_at":1},
              "todo":{"id":"t95","title":"State semantics","project":"atm","status":"review"}
            }
            """.utf8
        )
        let stale = try JSONDecoder().decode(ATMLiveSession.self, from: staleData)
        XCTAssertEqual(stale.phase, .bindingIssue)
        XCTAssertTrue(stale.needsUserAttention == false)
        XCTAssertTrue(stale.needsUserText.contains("不一致"))
        XCTAssertEqual(stale.presenceState, .attention)
    }

    func testLegacyBindingDefaultsMissingMetadata() throws {
        let data = Data(
            """
            {
              "tool":"Codex","session_id":"019f73d5","project":"atm","age_seconds":7,
              "binding":{"session_id":"019f73d5","todo_id":"t95"}
            }
            """.utf8
        )

        let session = try JSONDecoder().decode(ATMLiveSession.self, from: data)
        XCTAssertEqual(session.bindingState, "bound")
        XCTAssertEqual(session.binding?.agent, "")
        XCTAssertEqual(session.binding?.project, "")
        XCTAssertEqual(session.binding?.boundAt, 0)
    }

    func testAgentSessionContextIsOptionalAndUsesKnownEnvironmentPriority() {
        XCTAssertNil(ATMAgentSessionContext.sessionID(environment: [:]))
        XCTAssertNil(ATMAgentSessionContext.sessionID(environment: ["CODEX_THREAD_ID": "  "]))
        XCTAssertEqual(
            ATMAgentSessionContext.sessionID(
                environment: [
                    "PI_SESSION_ID": "pi-1",
                    "CODEX_THREAD_ID": " codex-1 ",
                    "ATM_SESSION_ID": "atm-1",
                ]
            ),
            "atm-1"
        )
    }

    func testSearchResultAnchorsAreStableAndNamespaced() {
        XCTAssertEqual(ATMSearchResultAnchor.task("t80"), "task:t80")
        XCTAssertEqual(ATMSearchResultAnchor.session("abc"), "session:abc")
        XCTAssertEqual(ATMSearchResultAnchor.document("abc"), "document:abc")
        XCTAssertEqual(ATMSearchResultAnchor.memory("abc"), "memory:abc")
        XCTAssertNotEqual(ATMSearchResultAnchor.task("abc"), ATMSearchResultAnchor.session("abc"))
    }

    func testUsageDateAxisParsesDatesAndKeepsReadableSparseEndpoints() throws {
        let values = (20...30).map { String(format: "2026-06-%02d", $0) }
            + (1...19).map { String(format: "2026-07-%02d", $0) }
        let dates = ATMUsageDateAxis.values(values)
        XCTAssertEqual(dates.count, 7)
        XCTAssertEqual(dates.first, ATMUsageDateAxis.date(from: "2026-06-20"))
        XCTAssertEqual(dates.last, ATMUsageDateAxis.date(from: "2026-07-19"))
        XCTAssertNotNil(ATMUsageDateAxis.date(from: "2026-07-19 10:00"))

        let domain = ATMUsageDateAxis.paddedDomain(values)
        XCTAssertLessThan(domain.lowerBound, try XCTUnwrap(dates.first))
        XCTAssertGreaterThan(domain.upperBound, try XCTUnwrap(dates.last))
    }

    func testTodoCreatorLabelMatchesTheCLIRendering() throws {
        let filedByMe = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t1","title":"Filed by hand","priority":"P1","status":"open","created":"2026-08-05","creator":"me"}"#.utf8
            )
        )
        XCTAssertEqual(ATMTodoCreator.label(filedByMe.creator, ownerName: "墨水"), "墨水（我）")
        XCTAssertEqual(ATMTodoCreator.label(filedByMe.creator, ownerName: ""), "我")

        let collected = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t2","title":"Filed by collection","priority":"P1","status":"open","created":"2026-08-05","creator":"collect"}"#.utf8
            )
        )
        XCTAssertEqual(ATMTodoCreator.label(collected.creator, ownerName: "墨水"), "收集")

        let byAgent = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t3","title":"Filed by an agent","priority":"P1","status":"open","created":"2026-08-05","creator":"codex"}"#.utf8
            )
        )
        XCTAssertEqual(ATMTodoCreator.label(byAgent.creator, ownerName: "墨水"), "codex")

        // A todo from before the field existed has no creator, and the detail
        // header shows nothing rather than claiming one.
        let legacy = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t4","title":"Filed before creator existed","priority":"P1","status":"open","created":"2026-07-01"}"#.utf8
            )
        )
        XCTAssertNil(legacy.creator)
        XCTAssertNil(ATMTodoCreator.label(legacy.creator, ownerName: "墨水"))

        // The row form drops the nickname — it is the same on every row it shows
        // up on — but keeps everything that distinguishes one creator from another.
        XCTAssertEqual(ATMTodoCreator.shortLabel(filedByMe.creator), "我")
        XCTAssertEqual(ATMTodoCreator.shortLabel(collected.creator), "收集")
        XCTAssertEqual(ATMTodoCreator.shortLabel(byAgent.creator), "codex")
        XCTAssertNil(ATMTodoCreator.shortLabel(legacy.creator))

        // Icons come from the sidebar's vocabulary: a 收集-filed todo wears the
        // 收集 section's tray, an agent-filed one the Agent section's cpu.
        XCTAssertEqual(ATMTodoCreator.icon(filedByMe.creator), "person.fill")
        XCTAssertEqual(ATMTodoCreator.icon(collected.creator), ATMDesktopSection.collection.icon)
        XCTAssertEqual(ATMTodoCreator.icon(byAgent.creator), ATMDesktopSection.agents.icon)
        // No creator, no icon — a placeholder glyph would claim provenance the
        // record does not have.
        XCTAssertNil(ATMTodoCreator.icon(legacy.creator))
        XCTAssertNil(ATMTodoCreator.icon("   "))
    }

    /// The list row spends no width on the priority — it tints the id instead — so
    /// the color is the only thing carrying it there. P0 and P1 have to stay
    /// distinct from each other and from the unset/low case.
    func testTodoPriorityStyleKeepsUrgencyDistinctByColor() throws {
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: "P0"), ATMTheme.danger)
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: "P1"), ATMTheme.accent)
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: "P2"), ATMTheme.secondary)
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: "P3"), ATMTheme.secondary)
        XCTAssertNotEqual(ATMTodoPriorityStyle.color(for: "P0"), ATMTodoPriorityStyle.color(for: "P1"))
        XCTAssertNotEqual(ATMTodoPriorityStyle.color(for: "P0"), ATMTodoPriorityStyle.color(for: "P2"))
        // An unknown priority must not borrow P0's red and claim to be urgent.
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: ""), ATMTheme.secondary)
        XCTAssertEqual(ATMTodoPriorityStyle.color(for: "P9"), ATMTheme.secondary)

        // Words for the tooltip and the pickers, since the color says nothing on
        // its own to hover or to a screen reader.
        XCTAssertEqual(ATMTodoPriorityStyle.label("P0"), "P0 · 紧急")
        XCTAssertEqual(ATMTodoPriorityStyle.label("P1"), "P1 · 高")
        XCTAssertEqual(ATMTodoPriorityStyle.label("P2"), "P2 · 普通")
        XCTAssertEqual(ATMTodoPriorityStyle.label("P3"), "P3 · 低")
    }

    func testTodoProjectStyleIsStableAndUsesCategoricalColors() throws {
        let atm = ATMTodoProjectStyle.colorIndex(for: "atm")
        XCTAssertEqual(atm, ATMTodoProjectStyle.colorIndex(for: "atm"))
        XCTAssertNotEqual(atm, ATMTodoProjectStyle.colorIndex(for: "wanda"))
        XCTAssertGreaterThanOrEqual(atm, 0)
        XCTAssertLessThan(atm, ATMTheme.palette.count - 1)
    }

    func testTodoCommandArgumentsPreserveBusinessCLI() throws {
        let data = Data(
            """
            {
              "id":"t8","title":"Ship menu app","priority":"P1","status":"open",
              "project":"atm","created":"2026-07-13"
            }
            """.utf8
        )
        let todo = try JSONDecoder().decode(ATMTodo.self, from: data)

        // Lifecycle went from fork/exec argv to typed IPC. What still has to hold
        // is the payload each action sends, and the method it sends it to.
        XCTAssertEqual(ATMTodoIPCCommand.start.verb, "todo.start")
        XCTAssertEqual(ATMTodoIPCCommand.done.verb, "todo.done")
        XCTAssertEqual(ATMTodoIPCCommand.archive.verb, "todo.archive")
        XCTAssertEqual(ATMTodoIPCCommand.restore.verb, "todo.restore")
        XCTAssertEqual(ATMTodoIPCCommand.delete.verb, "todo.delete")

        let encoder = JSONEncoder()
        func payload<T: Encodable>(_ value: T) throws -> [String: Any] {
            try XCTUnwrap(JSONSerialization.jsonObject(with: encoder.encode(value)) as? [String: Any])
        }

        XCTAssertEqual(try payload(ATMTodoIDRequest(todoID: "t8"))["todo_id"] as? String, "t8")
        let completed = try payload(
            ATMTodoDoneRequest(todoID: "t8", reason: "通过 ATM 菜单栏\(todo.completionVerb)")
        )
        XCTAssertEqual(completed["todo_id"] as? String, "t8")
        XCTAssertEqual(completed["reason"] as? String, "通过 ATM 菜单栏完成")
        // Closing a todo that is waiting in review is an acceptance, and the
        // closing reason is the only place that distinction survives.
        let submitted = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t9","title":"Submitted by an agent","priority":"P1","status":"review","created":"2026-07-27"}"#.utf8
            )
        )
        XCTAssertEqual(
            try payload(
                ATMTodoDoneRequest(todoID: "t9", reason: "通过 ATM 菜单栏\(submitted.completionVerb)")
            )["reason"] as? String,
            "通过 ATM 菜单栏验收"
        )
        // Archiving is a batch use case, so a single row is a batch of one.
        XCTAssertEqual(
            try payload(ATMTodoRetentionRequest("t8"))["todo_ids"] as? [String], ["t8"]
        )
        // Permanent deletion is only offered from the archive, after the App has
        // confirmed it; the request carries that confirmation rather than implying
        // it, which is what `--yes` used to do on the command line.
        let deletion = try payload(ATMTodoDeleteRequest("t8"))
        XCTAssertEqual(deletion["todo_id"] as? String, "t8")
        XCTAssertEqual(deletion["confirmed"] as? Bool, true)
        XCTAssertEqual(submitted.completionVerb, "验收")
        XCTAssertEqual(todo.completionVerb, "完成")

        // One lifecycle set for every open todo; at the review gate only the
        // closing verb changes wording (验收), and only what the todo is already
        // in drops out. Sending it back stays plain 回到待办 — the App does not
        // ask anyone to pronounce a 验收不通过 verdict to move a task back.
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: submitted).map(\.action),
            [.start, .complete, .returnToOpen]
        )
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: submitted).map(\.title),
            ["开始", "验收", "回到待办"]
        )
        XCTAssertFalse(ATMTodoStatusActions.showsLaunchPrompt(for: submitted))
        XCTAssertTrue(ATMTodoStatusActions.showsLaunchPrompt(for: todo))
        // Already open, so 回到待办 is the one thing missing here.
        XCTAssertEqual(
            ATMTodoStatusActions.primaryItems(for: todo).map(\.action),
            [.start, .complete]
        )
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: todo).map(\.action),
            [.start, .complete]
        )
        let inProgress = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t10","title":"Working","priority":"P1","status":"in_progress","created":"2026-07-29"}"#.utf8
            )
        )
        // Already running, so 开始 drops out and the rest stays.
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: inProgress).map(\.action),
            [.complete, .returnToOpen]
        )
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: inProgress).map(\.title),
            ["标记完成", "回到待办"]
        )
        // Return-to-open is a typed todo.update; Work still owns the
        // status-change unbind rule.
        XCTAssertEqual(
            try payload(ATMTodoUpdateRequest(todoID: inProgress.id, status: "open"))["status"] as? String,
            "open"
        )
        let deferred = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                """
                {"id":"t11","title":"Waiting","priority":"P1","status":"in_progress",
                 "wake_condition":"等待发布","created":"2026-07-29"}
                """.utf8
            )
        )
        // Waiting style has the same operations as ordinary in-progress work.
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: deferred).map(\.action),
			[.complete, .returnToOpen]
        )
        XCTAssertEqual(
            ATMTodoStatusActions.primaryItems(for: deferred).map(\.title),
			["标记完成", "回到待办"]
        )
        XCTAssertEqual(
            try payload(ATMTodoUpdateRequest(todoID: deferred.id, status: "open"))["todo_id"] as? String,
            "t11"
        )
        let closed = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t12","title":"Done","priority":"P1","status":"done","created":"2026-07-29"}"#.utf8
            )
        )
        // A closed todo has exactly one way forward.
        XCTAssertEqual(
            ATMTodoStatusActions.items(for: closed).map(\.action),
            [.start]
        )
        XCTAssertEqual(
            ATMTodoStatusActions.primaryItems(for: closed).map(\.action),
            [.start]
        )
        XCTAssertEqual(try payload(ATMTodoIDRequest(todoID: closed.id))["todo_id"] as? String, "t12")

        // Handoff carries no policy and no agent: it opens Codex and stops, so
        // there is nothing for the App to choose on the user's behalf.
        XCTAssertEqual(
            ATMCommandBuilder.handoffTodo(id: "t142"),
            ["todo", "handoff", "t142", "--json"]
        )

        // `todo prompt` was merged into `todo handoff --copy`: preparing the
        // pointer and opening Codex are the same command, and --copy is the
        // branch that opens nothing.
        XCTAssertEqual(
            ATMCommandBuilder.copyTodoPointer(id: "t8"),
            ["todo", "handoff", "t8", "--copy", "--json"]
        )
    }

    func testTodoDecodesDetailFields() throws {
        let data = Data(
            """
            {
              "id":"t9","title":"Inspect details","description":"Full task context",
              "priority":"P0","status":"waiting","project":"atm",
              "tags":["maintenance"],"wake_condition":"Review completed","review_at":"2026-07-15",
              "maintenance_limit":3,"created":"2026-07-14","source":"Codex",
              "links":[{"url":"https://example.com/review","kind":"review","title":"Review"}],
              "images":[{"name":"screen.png","path":"/tmp/screen.png","media_type":"image/png","size_bytes":2048}],
              "on_done":"notify","start_ts":1783992000
            }
            """.utf8
        )

        let todo = try JSONDecoder().decode(ATMTodo.self, from: data)
        XCTAssertEqual(todo.description, "Full task context")
        XCTAssertEqual(todo.wakeCondition, "Review completed")
        XCTAssertEqual(todo.reviewAt, "2026-07-15")
        XCTAssertEqual(todo.maintenanceLimit, 3)
        XCTAssertEqual(todo.links?.first?.title, "Review")
		XCTAssertEqual(todo.images?.first?.name, "screen.png")
		XCTAssertEqual(todo.images?.first?.path, "/tmp/screen.png")
		XCTAssertEqual(todo.images?.first?.mediaType, "image/png")
		XCTAssertEqual(todo.images?.first?.sizeBytes, 2048)
        XCTAssertEqual(todo.onDone, "notify")
    }

    func testMarkdownRenderingPreservesContentAndLink() throws {
        let rendered = ATMMarkdown.render("**重点**：查看 [ATM](https://example.com) 和 `todo`")

        XCTAssertEqual(String(rendered.characters), "重点：查看 ATM 和 todo")
        let linkRange = try XCTUnwrap(String(rendered.characters).range(of: "ATM"))
        let attributedRange = try XCTUnwrap(
            Range(linkRange, in: rendered)
        )
        XCTAssertEqual(rendered[attributedRange].link, URL(string: "https://example.com"))
    }

    func testMarkdownSeparatesBareLinksAroundChinesePunctuation() {
        let source = "MR：https://code.example/review/28530828；CR：https://cd.example/cr/35028935"
        let rendered = ATMMarkdown.render(source)
        let links = rendered.runs.compactMap(\.link)

        XCTAssertEqual(String(rendered.characters), source)
        XCTAssertEqual(
            links,
            [
                URL(string: "https://code.example/review/28530828"),
                URL(string: "https://cd.example/cr/35028935"),
            ]
        )
    }

    func testMarkdownDoesNotAutolinkDomainsInsideInlineCode() {
        let source = "正式：`internal.example.test`；入口：https://public.example.test"
        let rendered = ATMMarkdown.render(source)

        XCTAssertEqual(
            String(rendered.characters),
            "正式：internal.example.test；入口：https://public.example.test"
        )
        XCTAssertEqual(
            rendered.runs.compactMap(\.link),
            [URL(string: "https://public.example.test")]
        )
    }

    func testMarkdownSplitsFencedCodeFromProse() {
        let source = """
        新版本已替换并成功重启。下次直接执行：

        ```bash
        cd /Users/tester/mox/atm && \\
        app/macos/Scripts/build-app.sh
        ```

        命令已保存。
        """

        XCTAssertEqual(
            ATMMarkdown.blocks(source),
            [
                .paragraph("新版本已替换并成功重启。下次直接执行："),
                .code(
                    language: "bash",
                    content: "cd /Users/tester/mox/atm && \\\napp/macos/Scripts/build-app.sh"
                ),
                .paragraph("命令已保存。"),
            ]
        )
        XCTAssertEqual(
            ATMMarkdown.plainSummary(source, limit: 80),
            "新版本已替换并成功重启。下次直接执行："
        )
    }

    func testMarkdownParsesBlockLevelSyntax() {
        let source = """
        ## 行为

        1. 构建 App
        2. 重新签名

        > 仅用于本机开发。

        ---

        - 支持 **粗体**
        - 支持 [链接](https://example.com)
        """

        XCTAssertEqual(
            ATMMarkdown.blocks(source),
            [
                .heading(level: 2, text: "行为"),
                .list(ordered: true, items: ["构建 App", "重新签名"]),
                .quote("仅用于本机开发。"),
                .divider,
                .list(ordered: false, items: ["支持 **粗体**", "支持 [链接](https://example.com)"]),
            ]
        )
    }

    func testMarkdownParsesKnowledgeTablesWithoutCollapsingCells() {
        let source = """
        域名按「内网 vs 对外」「正式 vs 预发」四象限：

        | 环境 | 内网域名 (internal.example.test) | 对外域名 (public.example.test) |
        |---|---|---|
        | 正式 | `internal.example.test` | `public.example.test` |
        | 预发 | `staging-internal.example.test` | `staging-public.example.test` |

        - 后续说明
        """

        XCTAssertEqual(
            ATMMarkdown.blocks(source),
            [
                .paragraph("域名按「内网 vs 对外」「正式 vs 预发」四象限："),
                .table(
                    headers: ["环境", "内网域名 (internal.example.test)", "对外域名 (public.example.test)"],
                    alignments: [.leading, .leading, .leading],
                    rows: [
                        ["正式", "`internal.example.test`", "`public.example.test`"],
                        ["预发", "`staging-internal.example.test`", "`staging-public.example.test`"],
                    ]
                ),
                .list(ordered: false, items: ["后续说明"]),
            ]
        )
    }

    func testMarkdownTableParsesAlignmentAndPipesInsideCode() {
        let source = """
        | 名称 | 数量 | 状态 |
        |:---|---:|:---:|
        | `a|b` | 2 | 正常 |
        """

        XCTAssertEqual(
            ATMMarkdown.blocks(source),
            [
                .table(
                    headers: ["名称", "数量", "状态"],
                    alignments: [.leading, .trailing, .center],
                    rows: [["`a|b`", "2", "正常"]]
                ),
            ]
        )
    }

    func testKnowledgeMarkdownRemovesFrontMatterAndDuplicateTitle() {
        let source = """
        ---
        title: 测试文章
        tags: [atm]
        ---

        # 测试文章

        ## 正文

        保留内容。
        """

        XCTAssertEqual(
            ATMMarkdown.documentBody(source, removingTitle: "测试文章"),
            "## 正文\n\n保留内容。"
        )

        XCTAssertEqual(
            ATMMarkdown.documentBody(
                "# Enchanted macOS Debug 构建与重启\n\n正文",
                removingTitle: "Enchanted：macOS Debug 构建与重启"
            ),
            "正文"
        )
    }

    func testTodoCompletionNotificationPayload() throws {
        let data = Data(
            """
            {
              "id":"t10","title":"完成原生通知","priority":"P1","status":"in_progress",
              "project":"atm","created":"2026-07-14",
              "start_ts":1783992000
            }
            """.utf8
        )
        let todo = try JSONDecoder().decode(ATMTodo.self, from: data)
        let payload = ATMNotificationPayload.todoCompleted(
            todo,
            now: Date(timeIntervalSince1970: 1_783_995_660)
        )

        XCTAssertEqual(payload.title, "ATM · atm")
        XCTAssertEqual(payload.subtitle, "t10 已完成")
        XCTAssertEqual(payload.body, "完成原生通知（1 小时 1 分钟）")
        XCTAssertEqual(payload.event, .completed)

        let submitted = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t11","title":"Submitted by an agent","priority":"P1","status":"review","created":"2026-07-27"}"#.utf8
            )
        )
        XCTAssertEqual(ATMNotificationPayload.todoCompleted(submitted).subtitle, "t11 已验收")

        let created = ATMNotificationPayload.todoCreated(todo)
        XCTAssertEqual(created.subtitle, "t10 新建")
        XCTAssertEqual(created.event, .created)

        let needsReview = ATMNotificationPayload.todoNeedsReview(submitted)
        XCTAssertEqual(needsReview.subtitle, "t11 待验收")
        XCTAssertEqual(needsReview.event, .review)
        XCTAssertEqual(needsReview.title, "ATM")
    }

    func testTodoNotificationDiffSurfacesCreateAndReviewOnly() throws {
        let existing = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t1","title":"Old","priority":"P1","status":"in_progress","created":"2026-07-14"}"#.utf8
            )
        )
        let created = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t2","title":"Agent filed","priority":"P1","status":"open","project":"atm","created":"2026-07-29"}"#.utf8
            )
        )
        let submitted = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t1","title":"Old","priority":"P1","status":"review","created":"2026-07-14"}"#.utf8
            )
        )
        let createdAsReview = try JSONDecoder().decode(
            ATMTodo.self,
            from: Data(
                #"{"id":"t3","title":"Straight to review","priority":"P0","status":"review","created":"2026-07-29"}"#.utf8
            )
        )

        XCTAssertTrue(ATMTodoNotificationDiff.events(previous: nil, current: [existing]).isEmpty)

        let previous = ATMTodoNotificationDiff.statusMap(from: [existing])
        let events = ATMTodoNotificationDiff.events(
            previous: previous,
            current: [submitted, created, createdAsReview]
        )
        XCTAssertEqual(events.map { "\($0.0.id):\($0.1.rawValue)" }, [
            "t1:review",
            "t2:created",
            "t3:created",
            "t3:review",
        ])
    }

    func testDesktopTaskQueryGroupsReviewFirstAndSearches() throws {
        let data = Data(
            """
            [
              {"id":"t1","title":"Work ATM","description":"desktop window","priority":"P1","status":"in_progress","project":"atm","created":"2026-07-14"},
              {"id":"t2","title":"Review API","priority":"P0","status":"review","project":"maigc","created":"2026-07-14"},
              {"id":"t3","title":"Wait input","priority":"P2","status":"in_progress","wake_condition":"外部输入","project":"wanda","created":"2026-07-14"}
            ]
            """.utf8
        )
        let todos = try JSONDecoder().decode([ATMTodo].self, from: data)

        XCTAssertEqual(ATMTaskQuery.apply(todos, query: "desktop").map(\.id), ["t1"])
        XCTAssertEqual(ATMTaskQuery.apply(todos, query: "MAIGC").map(\.id), ["t2"])
        // 待验收 is the human gate — list order and default selection put it first.
		XCTAssertEqual(ATMTaskQuery.groups(from: todos).map(\.id), ["review", "working"])
        XCTAssertEqual(ATMTaskQuery.groups(from: todos).map(\.title).first, "待验收")
		XCTAssertEqual(ATMTaskQuery.flattened(from: todos).map(\.id), ["t2", "t3", "t1"])
        XCTAssertEqual(ATMTaskQuery.preferredDefault(in: todos)?.id, "t2")

		let archived = try JSONDecoder().decode(
			[ATMTodo].self,
			from: Data(#"[{"id":"t9","title":"Archived","priority":"P1","status":"done","created":"2026-07-13"}]"#.utf8)
		)
		let groupsWithArchive = ATMTaskQuery.groups(from: todos, includingArchived: archived)
		XCTAssertEqual(groupsWithArchive.map(\.id), ["review", "working", "archive"])
		XCTAssertEqual(groupsWithArchive.last?.title, "已归档")
		XCTAssertEqual(groupsWithArchive.last?.todos.map(\.id), ["t9"])

        let completedFirst = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data(
                """
                [
                  {"id":"t60","title":"Old release","priority":"P0","status":"done","created":"2026-07-10","closed":"2026-07-18"},
                  {"id":"t80","title":"Current iteration","priority":"P1","status":"in_progress","created":"2026-07-18"},
                  {"id":"t79","title":"Background work","priority":"P2","status":"in_progress","created":"2026-07-17"}
                ]
                """.utf8
            )
        )
        let completionNow = try XCTUnwrap(
            ISO8601DateFormatter().date(from: "2026-07-18T12:00:00+08:00")
        )
        XCTAssertEqual(ATMTaskQuery.preferredDefault(in: completedFirst)?.id, "t80")
        XCTAssertEqual(
            ATMTaskQuery.groups(from: completedFirst, now: completionNow).map(\.id),
            ["working", "done"]
        )
        // Within a status group, newest created date comes first.
        XCTAssertEqual(
            ATMTaskQuery.groups(from: completedFirst, now: completionNow)
                .first(where: { $0.id == "working" })?.todos.map(\.id),
            ["t80", "t79"]
        )
        XCTAssertEqual(
            ATMTaskQuery.sortedByCreatedDescending(completedFirst).map(\.id),
            ["t80", "t79", "t60"]
        )

        let deferredMix = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data(
                """
                [
                  {"id":"t1","title":"Real wait","priority":"P1","status":"in_progress","wake_condition":"CI 绿了","created":"2026-07-20"},
                  {"id":"t2","title":"Timed wait","priority":"P1","status":"in_progress","review_at":"2026-07-25","created":"2026-07-21"},
                  {"id":"t3","title":"Open work","priority":"P1","status":"open","created":"2026-07-22"}
                ]
                """.utf8
            )
        )
		XCTAssertTrue(ATMTodoStatusStyle.isWaiting(deferredMix[1]))
		XCTAssertTrue(ATMTodoStatusStyle.isWaiting(deferredMix[0]))
		XCTAssertEqual(ATMTaskQuery.groups(from: deferredMix).map(\.id), ["working", "open"])
		// Waiting keeps the 进行中 label and is marked by the clock and the orange
		// tint instead, so the four-state vocabulary stays four words.
		XCTAssertEqual(ATMTodoStatusStyle.label(for: deferredMix[1]), "进行中")
		XCTAssertEqual(ATMTodoStatusStyle.icon(for: deferredMix[1]), "clock.fill")
		XCTAssertNotEqual(
			ATMTodoStatusStyle.color(for: deferredMix[1]),
			ATMTodoStatusStyle.color(forStatus: "in_progress")
		)
        XCTAssertEqual(ATMTodoStatusStyle.icon(forStatus: "review"), "person.crop.circle.badge.checkmark")
        XCTAssertEqual(ATMTodoStatusStyle.icon(forStatus: "in_progress"), "circle.dotted")
        XCTAssertNotEqual(
            ATMTodoStatusStyle.color(forStatus: "review"),
            ATMTodoStatusStyle.color(forStatus: "in_progress")
        )
        let working = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data(#"[{"id":"t1","title":"Working","priority":"P1","status":"in_progress","created":"2026-07-29"}]"#.utf8)
        )
        let deferred = try JSONDecoder().decode(
            [ATMTodo].self,
			from: Data(#"[{"id":"t2","title":"Waiting","priority":"P1","status":"in_progress","wake_condition":"发布完成","created":"2026-07-29"}]"#.utf8)
        )
        XCTAssertTrue(ATMTodoStatusStyle.usesLoadingIcon(for: working[0]))
        XCTAssertFalse(ATMTodoStatusStyle.usesLoadingIcon(for: deferred[0]))
    }

    func testTaskListPresentationFallsBackToGroupedAndToggles() {
        XCTAssertEqual(ATMNavigatorPresentation.resolve("grouped"), .grouped)
        XCTAssertEqual(ATMNavigatorPresentation.resolve("flat"), .flat)
        XCTAssertEqual(ATMNavigatorPresentation.resolve("unknown"), .grouped)
        XCTAssertEqual(ATMNavigatorPresentation.grouped.toggled, .flat)
        XCTAssertEqual(ATMNavigatorPresentation.flat.toggled, .grouped)
        XCTAssertEqual(Set([
            ATMNavigatorPresentationPreferences.tasksKey,
            ATMNavigatorPresentationPreferences.collectionKey,
            ATMNavigatorPresentationPreferences.agentsKey,
            ATMNavigatorPresentationPreferences.knowledgeKey,
        ]).count, 4)
    }

    func testComposerPlaceholderHidesDuringIMEComposition() {
        // Empty field, idle: show placeholder.
        XCTAssertTrue(ATMComposerPlaceholderPolicy.shouldShow(stringIsEmpty: true, hasMarkedText: false))
        // IME pre-edit (pinyin) with empty committed string: hide placeholder.
        XCTAssertFalse(ATMComposerPlaceholderPolicy.shouldShow(stringIsEmpty: true, hasMarkedText: true))
        // Committed text: hide regardless of marked state.
        XCTAssertFalse(ATMComposerPlaceholderPolicy.shouldShow(stringIsEmpty: false, hasMarkedText: false))
        XCTAssertFalse(ATMComposerPlaceholderPolicy.shouldShow(stringIsEmpty: false, hasMarkedText: true))
    }

    func testComposerSingleLineFoldsPastedNewlines() {
        // Nothing to fold: the common case must come back untouched, including the
        // spaces the user typed themselves.
        XCTAssertEqual(ATMComposerText.singleLine("标题栏的编辑框只有一行"), "标题栏的编辑框只有一行")
        XCTAssertEqual(ATMComposerText.singleLine(""), "")
        // Pasting a paragraph into a title lands as one line.
        XCTAssertEqual(ATMComposerText.singleLine("第一行\n第二行"), "第一行 第二行")
        XCTAssertEqual(ATMComposerText.singleLine("a\r\nb"), "a b")
        // A blank line is one separator, not a run of spaces.
        XCTAssertEqual(ATMComposerText.singleLine("a\n\n\nb"), "a b")
        // Leading and trailing breaks leave no stray space behind.
        XCTAssertEqual(ATMComposerText.singleLine("\nabc\n"), "abc")
        XCTAssertEqual(ATMComposerText.singleLine("\n\n"), "")
    }

    func testGrowingComposerHeightClampsBetweenMinAndMaxLines() {
        let line: CGFloat = 16
        let inset: CGFloat = 6
        func height(_ content: CGFloat, minLines: Int = 1, maxLines: Int = 6) -> CGFloat {
            ATMComposerHeight.clamped(
                contentHeight: content,
                lineHeight: line,
                minLines: minLines,
                maxLines: maxLines,
                verticalInset: inset
            )
        }
        // Empty (nothing measured yet) still reserves one line plus the insets.
        XCTAssertEqual(height(0), line + inset * 2)
        // Three wrapped lines: the box grows to fit them.
        XCTAssertEqual(height(line * 3), line * 3 + inset * 2)
        // Past the cap it stops growing and scrolls instead.
        XCTAssertEqual(height(line * 20), line * 6 + inset * 2)
        // A two-line floor never collapses to one.
        XCTAssertEqual(height(0, minLines: 2), line * 2 + inset * 2)
        // A max below the min is the min, not an inverted range.
        XCTAssertEqual(height(line * 20, minLines: 3, maxLines: 1), line * 3 + inset * 2)
        // Fractional layout heights round up rather than clipping the last line.
        XCTAssertEqual(height(line * 2 + 0.4), (line * 2 + 0.4 + inset * 2).rounded(.up))
    }

    func testIconButtonChromeHoverFill() {
        XCTAssertEqual(
            ATMIconButtonChrome.background(isHovered: false, isEnabled: true, chrome: .chip),
            .controlFill(opacity: 1)
        )
        XCTAssertEqual(
            ATMIconButtonChrome.background(isHovered: true, isEnabled: true, chrome: .chip),
            .primaryOverlay(opacity: 0.10)
        )
        XCTAssertEqual(
            ATMIconButtonChrome.background(isHovered: false, isEnabled: true, chrome: .bare),
            .clear
        )
        XCTAssertEqual(
            ATMIconButtonChrome.background(isHovered: true, isEnabled: true, chrome: .bare),
            .primaryOverlay(opacity: 0.07)
        )
        XCTAssertEqual(
            ATMIconButtonChrome.background(isHovered: true, isEnabled: false, chrome: .chip),
            .controlFill(opacity: 0.55)
        )
    }

    func testDesktopTaskQuerySeparatesRecentHistoryAndDropped() throws {
        let todos = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data(
                """
                [
                  {"id":"t1","title":"Newest completion","priority":"P1","status":"done","created":"2026-07-01","closed":"2026-07-31"},
                  {"id":"t2","title":"Old completion","priority":"P1","status":"done","created":"2026-07-30","closed":"2026-07-20"},
                  {"id":"t3","title":"Abandoned","priority":"P1","status":"dropped","created":"2026-07-30","closed":"2026-07-30"},
                  {"id":"t4","title":"Recent completion","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-29"},
                  {"id":"t5","title":"Timestamp completion","priority":"P1","status":"done","created":"2026-07-01","done_ts":1785384000},
                  {"id":"t6","title":"Legacy completion","priority":"P1","status":"done","created":"2026-07-19"}
                ]
                """.utf8
            )
        )
        let now = try XCTUnwrap(
            ISO8601DateFormatter().date(from: "2026-07-31T12:00:00+08:00")
        )
        let groups = ATMTaskQuery.groups(from: todos, now: now)

		XCTAssertEqual(groups.map(\.id), ["done", "history"])
		XCTAssertEqual(groups.map(\.title), ["最近完成", "完成历史"])
        XCTAssertEqual(groups.first(where: { $0.id == "done" })?.todos.map(\.id), ["t1", "t5", "t4"])
        XCTAssertEqual(groups.first(where: { $0.id == "history" })?.todos.map(\.id), ["t2", "t6"])
        XCTAssertEqual(ATMTaskQuery.completionDay(for: todos[0]), "2026-07-31")
        XCTAssertEqual(ATMTaskQuery.completionDay(for: todos[4]), "2026-07-30")
        XCTAssertEqual(ATMTaskQuery.completionDay(for: todos[5]), "2026-07-19")
        XCTAssertFalse(ATMTodoStatusStyle.usesStrikethrough(for: todos[0]))
		XCTAssertFalse(ATMTodoStatusStyle.usesStrikethrough(for: todos[2]))

        // 最近完成 is the 7-day window with no count cap: a cap used to truncate
        // mid-day, dropping the rest of that day into 完成历史.
        let volumeJSON = (1...22)
            .map { index in
                """
                {"id":"t\(100 + index)","title":"Recent \(index)","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-31"}
                """
            }
            .joined(separator: ",")
        let volume = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data("[\(volumeJSON)]".utf8)
        )
        let volumeGroups = ATMTaskQuery.groups(from: volume, now: now)
        XCTAssertEqual(volumeGroups.first(where: { $0.id == "done" })?.todos.count, 22)
        XCTAssertNil(volumeGroups.first(where: { $0.id == "history" }))
    }

    /// Within one completion day: `done_ts` decides, and ids fall back to numeric
    /// order — a string compare put t99 above t100.
    func testDesktopTaskQueryOrdersSameDayCompletionsByTimestampThenNumericID() throws {
        let todos = try JSONDecoder().decode(
            [ATMTodo].self,
            from: Data(
                """
                [
                  {"id":"t10","title":"Earlier stamp","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-31","done_ts":1785490011},
                  {"id":"t11","title":"Later stamp","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-31","done_ts":1785510835},
                  {"id":"t99","title":"No stamp, lower number","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-31"},
                  {"id":"t100","title":"No stamp, higher number","priority":"P1","status":"done","created":"2026-07-31","closed":"2026-07-31"}
                ]
                """.utf8
            )
        )

        XCTAssertEqual(
            ATMTaskQuery.sortedByCompletionDescending(todos).map(\.id),
            ["t11", "t10", "t100", "t99"]
        )
    }

    func testSyncPolicyUsesFiveMinuteCadence() {
        let now = Date(timeIntervalSince1970: 1_784_396_800)
        XCTAssertTrue(ATMSyncPolicy.shouldSync(lastAttemptAt: nil, now: now))
        XCTAssertFalse(ATMSyncPolicy.shouldSync(lastAttemptAt: now.addingTimeInterval(-299), now: now))
        XCTAssertTrue(ATMSyncPolicy.shouldSync(lastAttemptAt: now.addingTimeInterval(-300), now: now))
    }

    func testLiveStatusUsesFastVisibleCadenceAndWinsDashboardRace() {
        XCTAssertEqual(ATMLiveStatusRefreshPolicy.interval, 3)
        let dashboardStartedAt = Date(timeIntervalSince1970: 1_000)
        XCTAssertFalse(ATMLiveStatusRefreshPolicy.shouldPreserveFastStatus(
            lastAppliedAt: nil,
            dashboardRequestStartedAt: dashboardStartedAt
        ))
        XCTAssertFalse(ATMLiveStatusRefreshPolicy.shouldPreserveFastStatus(
            lastAppliedAt: dashboardStartedAt,
            dashboardRequestStartedAt: dashboardStartedAt
        ))
        XCTAssertTrue(ATMLiveStatusRefreshPolicy.shouldPreserveFastStatus(
            lastAppliedAt: dashboardStartedAt.addingTimeInterval(0.1),
            dashboardRequestStartedAt: dashboardStartedAt
        ))

        let liveStatus = ATMLiveStatus(
            sessions: [ATMLiveSession(
                tool: "Codex",
                sessionID: "live",
                project: "atm",
                ageSeconds: 1
            )],
            time: "12:00:03"
        )
        let replaced = ATMDashboardSnapshot.empty.replacingLiveStatus(liveStatus)
        XCTAssertEqual(replaced.liveStatus.sessions.map(\.sessionID), ["live"])
        XCTAssertEqual(replaced.liveStatus.time, "12:00:03")
        XCTAssertEqual(replaced.refreshedAt, .distantPast)
    }

    func testDashboardRefreshKeepsTheAttentionOverlayWhicheverStatusWins() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        func status(_ time: String) -> ATMLiveStatus {
            ATMLiveStatus(
                sessions: [ATMLiveSession(
                    tool: "Codex",
                    sessionID: "waiting",
                    project: "atm",
                    ageSeconds: 1
                )],
                time: time
            )
        }
        let signals = ["waiting": ATMAgentAttentionSignal(
            reason: "permission_prompt",
            tool: "shell",
            text: nil,
            source: "codex",
            receivedAt: now
        )]

        // The dashboard's own live_status has never seen a hook event. Landing it
        // raw is what retired a banner and then raised it again a poll later.
        let fromDashboard = ATMLiveStatusRefreshPolicy.mergedLiveStatus(
            dashboard: status("12:00:00"),
            fast: status("12:00:03"),
            preserveFast: false,
            signals: signals,
            now: now
        )
        XCTAssertEqual(fromDashboard.time, "12:00:00")
        XCTAssertEqual(fromDashboard.sessions.first?.attentionSignal?.reason, "permission_prompt")
        XCTAssertTrue(fromDashboard.sessions.allSatisfy(\.needsHookAttention))

        let fromFastPoll = ATMLiveStatusRefreshPolicy.mergedLiveStatus(
            dashboard: status("12:00:00"),
            fast: status("12:00:03"),
            preserveFast: true,
            signals: signals,
            now: now
        )
        XCTAssertEqual(fromFastPoll.time, "12:00:03")
        XCTAssertEqual(fromFastPoll.sessions.first?.attentionSignal?.reason, "permission_prompt")

        // An expired signal is dropped by the same join, so nothing has to wait
        // for the next poll to stop claiming an agent is blocked.
        let expired = ATMLiveStatusRefreshPolicy.mergedLiveStatus(
            dashboard: status("12:00:00"),
            fast: status("12:00:03"),
            preserveFast: false,
            signals: signals,
            now: now.addingTimeInterval(ATMAgentAttentionSignal.timeToLive + 1)
        )
        XCTAssertNil(expired.sessions.first?.attentionSignal)
    }

    func testAgentSoundTransitionTrackerPrimesSilentlyAndDeduplicatesSnapshots() {
        func session(
            input: String,
            result: String,
            answer: String = "正在处理"
        ) -> ATMLiveSession {
            ATMLiveSession(
                tool: "Codex",
                sessionID: "sound-session",
                project: "atm",
                summary: "测试 Agent 声音",
                ageSeconds: 1,
                lastQuestion: input,
                lastAnswer: answer,
                latestResult: result,
                activityState: "active"
            )
        }

        var tracker = ATMAgentSoundTransitionTracker()
        let baseline = session(input: "第一条输入", result: "旧结果")
        XCTAssertNil(tracker.nextEvent(for: [baseline]))
        XCTAssertNil(tracker.nextEvent(for: [baseline]))

        let processing = session(input: "第二条输入", result: "旧结果")
        XCTAssertEqual(tracker.nextEvent(for: [processing]), .processingStarted)
        XCTAssertNil(tracker.nextEvent(for: [processing]))

        let completed = session(input: "第二条输入", result: "新结果")
        XCTAssertEqual(tracker.nextEvent(for: [completed]), .taskCompleted)
        XCTAssertNil(tracker.nextEvent(for: [completed]))

        let attention = session(
            input: "第三条输入",
            result: "又一条结果",
            answer: "需要你确认后再继续"
        )
        XCTAssertEqual(tracker.nextEvent(for: [attention]), .attentionRequired)
        XCTAssertNil(tracker.nextEvent(for: [attention]))
    }

    func testAgentSoundPreferencesUseQuietDefaultsAndRespectMasterSwitch() throws {
        let suiteName = "atm-agent-sound-tests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }

        XCTAssertTrue(ATMAgentSoundPreferences.masterEnabled(defaults: defaults))
        XCTAssertFalse(ATMAgentSoundPreferences.isEnabled(
            for: .processingStarted,
            defaults: defaults
        ))
        XCTAssertTrue(ATMAgentSoundPreferences.isEnabled(
            for: .attentionRequired,
            defaults: defaults
        ))
        XCTAssertEqual(
            ATMAgentSoundPreferences.sound(for: .taskCompleted, defaults: defaults),
            .pingIslandSubmitBlip
        )

        XCTAssertEqual(ATMAgentSoundEvent.processingStarted.defaultSound, .pingIslandMenuSelect)
        XCTAssertEqual(ATMAgentSoundEvent.attentionRequired.defaultSound, .pingIslandApprovalAlert)
        XCTAssertEqual(ATMAgentSoundEvent.taskCompleted.defaultSound, .pingIslandSubmitBlip)
        XCTAssertNotNil(ATMAgentSound.pingIslandMenuSelect.bundledResourceURL)
        XCTAssertNotNil(ATMAgentSound.pingIslandApprovalAlert.bundledResourceURL)
        XCTAssertNotNil(ATMAgentSound.pingIslandSubmitBlip.bundledResourceURL)

        defaults.set(ATMAgentSound.glass.rawValue, forKey: ATMAgentSoundPreferences.soundKey(for: .attentionRequired))
        XCTAssertEqual(
            ATMAgentSoundPreferences.sound(for: .attentionRequired, defaults: defaults),
            .glass
        )

        defaults.set(false, forKey: ATMAgentSoundPreferences.enabledKey)
        XCTAssertFalse(ATMAgentSoundPreferences.isEnabled(
            for: .attentionRequired,
            defaults: defaults
        ))

        defaults.set(2.0, forKey: ATMAgentSoundPreferences.volumeKey)
        XCTAssertEqual(ATMAgentSoundPreferences.volume(defaults: defaults), 1)
    }

    func testCommandRunnerDrainsStdoutAndStderrConcurrently() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-command-runner-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        try """
        #!/bin/sh
        /usr/bin/head -c 200000 /dev/zero
        /usr/bin/head -c 200000 /dev/zero >&2
        """.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)

        let runner = try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path])
        let output = try await runner.run([])
        XCTAssertEqual(output.count, 200_000)
    }

    func testCommandRunnerTimesOutAndStopsHungProcess() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-command-timeout-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        try "#!/bin/sh\nexec /bin/sleep 5\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)

        let runner = try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path])
        do {
            _ = try await runner.run(["status"], timeout: 0.1)
            XCTFail("expected timeout")
        } catch let error as ATMCommandError {
            guard case .timedOut(let arguments, _) = error else {
                return XCTFail("unexpected command error: \(error)")
            }
            XCTAssertEqual(arguments, ["status"])
        }
    }

    func testCommandRunnerCancellationStopsProcess() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("atm-command-cancel-\(UUID().uuidString)", isDirectory: true)
        let script = directory.appendingPathComponent("atm")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        try "#!/bin/sh\nexec /bin/sleep 5\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)

        let runner = try ATMCommandRunner(environment: ["ATM_EXECUTABLE": script.path])
        let task = Task { try await runner.run(["status"], timeout: 5) }
        try await Task.sleep(nanoseconds: 100_000_000)
        task.cancel()
        do {
            _ = try await task.value
            XCTFail("expected cancellation")
        } catch is CancellationError {
            // Expected.
        }
    }

    func testErrorTextCollapsesWhitespaceAndTruncates() {
        XCTAssertEqual(ATMErrorText.compact(" first\n  second\tthird "), "first second third")
        XCTAssertEqual(ATMErrorText.compact("123456", limit: 5), "12345…")
    }

    func testErrorPresentationTurnsNetworkPayloadIntoActionableCopy() {
        let presentation = ATMErrorPresentation.resolve(
            "[NETWORK_UNREACHABLE] Cannot connect to MCP server: dial tcp: i/o timeout",
            fallbackTitle: "收集失败"
        )
        XCTAssertEqual(presentation.title, "网络连接失败")
        XCTAssertEqual(presentation.message, "无法连接到服务。请检查网络、代理或 DNS 设置后重试。")
    }

    func testErrorPresentationKeepsConciseUnknownError() {
        let presentation = ATMErrorPresentation.resolve("配置文件不存在", fallbackTitle: "加载失败")
        XCTAssertEqual(presentation, ATMErrorPresentation(title: "加载失败", message: "配置文件不存在"))
    }

    func testKnowledgeCatalogDecodesOptionalRoutingFields() throws {
        let data = Data(
            """
            [
              {"id":"atm","name":"atm","description":"","topics":["build"],"document_count":1},
              {"id":"ink","name":"个人知识","role":"primary-context","description":"长期背景", "topics":[],
               "use_when":["涉及用户本人"],"avoid_when":["公共事实"],"instructions":["区分事实与推断"],"document_count":3}
            ]
            """.utf8
        )

        let collections = try JSONDecoder().decode([ATMKnowledgeCollection].self, from: data)
        XCTAssertEqual(collections[0].id, "atm")
        XCTAssertEqual(collections[0].useWhen, [])
        XCTAssertEqual(collections[0].documentCount, 1)
        XCTAssertEqual(collections[1].role, "primary-context")
        XCTAssertEqual(collections[1].instructions, ["区分事实与推断"])
    }

    func testKnowledgeDocumentSummaryDecodesListAndSearchShapes() throws {
        let listData = Data(
            """
            {"document_id":"document:1","title":"构建手册","collection":"atm","status":"active",
             "domains":["development"],"tags":["build"],"projects":["atm"],"producer":"codex",
             "created_at":"2026-07-14T07:15:51Z","updated_at":"2026-07-15T07:15:51Z"}
            """.utf8
        )
        let searchData = Data(
            """
            {"document_id":"document:1","title":"构建手册","collection":"atm",
             "domains":["development"],"tags":["build"],"projects":["atm"],"snippet":"构建并重启","score":3.2}
            """.utf8
        )

        let listed = try JSONDecoder().decode(ATMKnowledgeDocumentSummary.self, from: listData)
        let searched = try JSONDecoder().decode(ATMKnowledgeDocumentSummary.self, from: searchData)
        XCTAssertEqual(listed.status, "active")
        XCTAssertEqual(listed.updatedAt, "2026-07-15T07:15:51Z")
        XCTAssertEqual(searched.snippet, "构建并重启")
        XCTAssertEqual(searched.score, 3.2)
        XCTAssertNil(searched.status)
    }

    func testKnowledgeDocumentDecodesMetadataAndSource() throws {
        let data = Data(
            """
            {
              "metadata": {
                "id":"document:1","schemaVersion":1,"title":"外部手册","status":"active",
                "domains":[],"tags":["cli"],"projects":["atm"],"producer":"atm-import",
                "createdAt":"2026-07-14T07:15:51Z","updatedAt":"2026-07-15T07:15:51Z",
                "source":{"type":"file","uri":"docs/guide.md","hash":"abc","importedAt":"2026-07-14T07:00:00Z"}
              },
              "collection":"atm","content":"# 外部手册\\n\\n正文"
            }
            """.utf8
        )

        let document = try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
        XCTAssertEqual(document.metadata.schemaVersion, 1)
        XCTAssertEqual(document.metadata.source?.uri, "docs/guide.md")
        XCTAssertEqual(document.metadata.source?.importedAt, "2026-07-14T07:00:00Z")
        XCTAssertEqual(document.content, "# 外部手册\n\n正文")
    }

    func testKnowledgeGovernanceModelsDecode() throws {
        let reportData = Data(
            """
            {"generated_at":"2026-07-15T05:00:00Z","stale_days":180,"documents":3,"active":2,
             "issues":[{"code":"stale","severity":"warning","document_ids":["document:1"],"collection":"atm","title":"Runbook","detail":"old","suggested_action":"review"}],
             "counts":{"stale":1}}
            """.utf8
        )
        let report = try JSONDecoder().decode(ATMKnowledgeAuditReport.self, from: reportData)
        XCTAssertEqual(report.active, 2)
        XCTAssertEqual(report.issues.first?.suggestedAction, "review")

        let qualityData = Data(
            """
            [{"document_id":"document:1","title":"Runbook","collection":"atm","retrievals":4,"adopted":2,"corrected":1,"rejected":0,"score":0.67}]
            """.utf8
        )
        let qualities = try JSONDecoder().decode([ATMKnowledgeQuality].self, from: qualityData)
        XCTAssertEqual(qualities.first?.retrievals, 4)
        XCTAssertEqual(qualities.first?.id, "document:1")

        let cleanReportData = Data(
            """
            {"generated_at":"2026-07-15T05:00:00Z","stale_days":180,"documents":89,"active":89,
             "issues":null,"counts":null}
            """.utf8
        )
        let cleanReport = try JSONDecoder().decode(ATMKnowledgeAuditReport.self, from: cleanReportData)
        XCTAssertEqual(cleanReport.issues, [])
        XCTAssertEqual(cleanReport.counts, [:])
    }

    func testDoctorReportDecodesHealthIssues() throws {
        let data = Data(
            """
            {
              "sources":[{"agent":"codex","path":"/tmp","exists":true,"files":3,"indexed_sessions":4,"status":"ok"}],
              "issues":[{"severity":"warning","domain":"usage","code":"unknown_models","subject":"codex",
                "detail":"39 request events have no model","suggestion":"update parser"}]
            }
            """.utf8
        )
        let report = try JSONDecoder().decode(ATMDoctorReport.self, from: data)
        XCTAssertEqual(report.sources.first?.indexedSessions, 4)
        XCTAssertEqual(report.issues.first?.code, "unknown_models")
    }

    func testIndexHealthReportDecodesFreshnessAndLastRun() throws {
        let data = Data(
            """
            {
              "generated_at":"2026-07-20T08:02:00Z",
              "index":{"path":"/tmp/atm.db","exists":true,"schema_version":11,"indexed_sessions":42},
              "sync":{"scope":"all","status":"fresh","run_status":"succeeded",
                "last_attempt_at":"2026-07-20T08:00:00Z","last_success_at":"2026-07-20T08:00:00Z",
                "age_seconds":120,"stale_after_seconds":600,"last_error":"","last_synced_files":3}
            }
            """.utf8
        )

        let report = try JSONDecoder().decode(ATMIndexHealthReport.self, from: data)
        XCTAssertEqual(report.index.schemaVersion, 11)
        XCTAssertEqual(report.index.indexedSessions, 42)
        XCTAssertEqual(report.sync.status, "fresh")
        XCTAssertEqual(report.sync.ageSeconds, 120)
        XCTAssertEqual(report.sync.lastSyncedFiles, 3)
    }

    func testMemoryHitDecodesMissingOptionalFields() throws {
        let data = Data(
            """
            {"id":"memory:1","scope":"global","content":"hub 指代 mm-dio-hub-service",
             "tags":["service","alias"],"created_at":"2026-07-14T01:49:43Z","score":0.24,"source":"memory"}
            """.utf8
        )

        let memory = try JSONDecoder().decode(ATMMemoryHit.self, from: data)
        XCTAssertEqual(memory.scope, "global")
        XCTAssertEqual(memory.metadata, [:])
        XCTAssertEqual(memory.source, "memory")
        XCTAssertEqual(memory.tags, ["service", "alias"])
    }

    func testSessionSearchHitDecodesCLIShape() throws {
        let data = Data(
            """
            {"short_id":"2553c50a","agent":"codex","project":"atm","created_at":"07-18 14:07",
             "role":"assistant","content":"Skill 统计已经修复"}
            """.utf8
        )

        let hit = try JSONDecoder().decode(ATMSessionSearchHit.self, from: data)
        XCTAssertEqual(hit.id, "2553c50a")
        XCTAssertEqual(hit.project, "atm")
        XCTAssertEqual(hit.content, "Skill 统计已经修复")
    }

    func testSessionSearchResultKeepsTotalBeyondReturnedPage() throws {
        let data = Data(
            """
            {"keyword":"atm","total":1110,"returned":2,"truncated":true,"limit":2,"matches":[
             {"id":"rollout-1","short_id":"2553c50a","agent":"codex","project":"atm",
              "created_at":"2026-07-18T14:07:00+08:00","role":"assistant","content":"first",
              "snippet_truncated":true},
             {"id":"rollout-2","short_id":"601cee2a","agent":"claude","project":"atm",
              "created_at":"2026-07-18T14:09:00+08:00","role":"user","content":"second"}]}
            """.utf8
        )

        let result = try JSONDecoder().decode(ATMSessionSearchResult.self, from: data)
        XCTAssertEqual(result.total, 1110)
        XCTAssertEqual(result.returned, 2)
        XCTAssertTrue(result.truncated)
        XCTAssertEqual(result.matches.count, 2)
        XCTAssertEqual(result.matches.first?.shortID, "2553c50a")
    }

    func testSessionSearchResultDefaultsCountsToPageWhenAbsent() throws {
        let data = Data(
            """
            {"matches":[{"short_id":"2553c50a","agent":"codex","project":"atm",
             "created_at":"2026-07-18T14:07:00+08:00","role":"assistant","content":"only"}]}
            """.utf8
        )

        let result = try JSONDecoder().decode(ATMSessionSearchResult.self, from: data)
        XCTAssertEqual(result.total, 1)
        XCTAssertEqual(result.returned, 1)
        XCTAssertFalse(result.truncated)
    }

    private func makeTodo(
        id: String,
        project: String?,
        created: String,
        priority: String = "P1",
        status: String = "open"
    ) throws -> ATMTodo {
        var fields = [
            "\"id\":\"\(id)\"",
            "\"title\":\"\(id)\"",
            "\"priority\":\"\(priority)\"",
            "\"status\":\"\(status)\"",
            "\"created\":\"\(created)\"",
        ]
        if let project { fields.append("\"project\":\"\(project)\"") }
        return try JSONDecoder().decode(ATMTodo.self, from: Data("{\(fields.joined(separator: ","))}".utf8))
    }

    func testTodoDraftKeepsRequirementAsDescriptionAndBuildsFallbackTitle() {
        let draft = ATMTodoDraft(
            text: "\n  收敛用量面板  \n\n按 client / project 拆视角\n还要带费用\n",
            project: " atm ",
            priority: "P1"
        )

		XCTAssertEqual(draft.title, "收敛用量面板 按 client / project 拆视角 还要带费用")
		XCTAssertEqual(draft.description, "收敛用量面板  \n\n按 client / project 拆视角\n还要带费用")
        XCTAssertEqual(draft.project, "atm")
        XCTAssertTrue(draft.isSubmittable)
        XCTAssertFalse(ATMTodoDraft(text: "   \n  ", project: "", priority: "P1").isSubmittable)
		XCTAssertEqual(
			ATMTodoDraft.fallbackTitle(from: String(repeating: "任", count: 90)).count,
			80
		)
    }

	func testTodoImageRulesAndTemporaryDraftCleanup() throws {
		let directory = FileManager.default.temporaryDirectory
			.appendingPathComponent("atm-image-rules-\(UUID().uuidString)")
		try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
		defer { try? FileManager.default.removeItem(at: directory) }
		let image = directory.appendingPathComponent("screen.png")
		try Data([0x89, 0x50, 0x4e, 0x47]).write(to: image)

		XCTAssertNil(ATMTodoImageRules.validationError(for: image, currentCount: 0))
		XCTAssertEqual(
			ATMTodoImageRules.validationError(for: image, currentCount: ATMTodoImageRules.maximumCount),
			"每个任务最多添加 10 张图片。"
		)
		let text = directory.appendingPathComponent("notes.txt")
		try Data("text".utf8).write(to: text)
		XCTAssertTrue(ATMTodoImageRules.validationError(for: text, currentCount: 0)?.contains("不支持") == true)

		let draft = ATMTodoDraft(
			text: "Cleanup pasted image",
			project: "atm",
			priority: "P1",
			imagePaths: [image.path],
			temporaryImagePaths: [image.path]
		)
		draft.cleanupTemporaryImages()
		XCTAssertFalse(FileManager.default.fileExists(atPath: image.path))
	}

    func testTodoSuggestionPrefersProjectNamedInTheText() throws {
        let todos = [
            try makeTodo(id: "t1", project: "atm", created: "2026-07-20"),
            try makeTodo(id: "t2", project: "wanda", created: "2026-07-27"),
        ]

        let suggestion = ATMTodoSuggestion.infer(text: "atm 的用量面板要拆视角", todos: todos)

        XCTAssertEqual(suggestion.project, "atm")
        XCTAssertEqual(suggestion.projectReason, "文本提到 atm")
        XCTAssertEqual(suggestion.priority, "P1")
    }

    func testTodoSuggestionFallsBackToTheLiveSessionThenRecentProject() throws {
        let todos = [
            try makeTodo(id: "t1", project: "atm", created: "2026-07-20"),
            try makeTodo(id: "t2", project: "wanda", created: "2026-07-27"),
        ]
        let live = ATMLiveSession(
            tool: "claude",
            sessionID: "s1",
            project: "mox-atm",
            ageSeconds: 30,
            activityState: "active",
            bindingState: "unbound"
        )

        // A session project spelled differently still resolves to the todo project.
        let fromSession = ATMTodoSuggestion.infer(text: "修一下详情页", todos: todos, liveSessions: [live])
        XCTAssertEqual(fromSession.project, "atm")
        XCTAssertEqual(fromSession.projectReason, "当前会话在 mox-atm")

        // With no session at all, the most recently used project wins.
        let fromHistory = ATMTodoSuggestion.infer(text: "修一下详情页", todos: todos)
        XCTAssertEqual(fromHistory.project, "wanda")
        XCTAssertEqual(ATMTodoSuggestion.infer(text: "", todos: []), .empty)
    }

    func testTodoSuggestionReadsPriorityFromTheText() {
        XCTAssertEqual(ATMTodoSuggestion.infer(text: "线上挂了，先救", todos: []).priority, "P0")
        XCTAssertEqual(ATMTodoSuggestion.infer(text: "顺手把日志清一下", todos: []).priority, "P2")
        XCTAssertEqual(ATMTodoSuggestion.infer(text: "P0 修登录", todos: []).priority, "P0")
        // "top10" contains "p1" but is not a priority.
        XCTAssertEqual(ATMTodoSuggestion.infer(text: "统计 top10 技能", todos: []).priority, "P1")
    }

    func testPaginationLimitsRenderedItemsAndClampsPage() {
        let items = Array(0..<25)

        XCTAssertEqual(ATMPagination.pageCount(itemCount: items.count, pageSize: 10), 3)
        XCTAssertEqual(ATMPagination.items(items, page: 0, pageSize: 10), Array(0..<10))
        XCTAssertEqual(ATMPagination.items(items, page: 2, pageSize: 10), Array(20..<25))
        XCTAssertEqual(ATMPagination.items(items, page: 99, pageSize: 10), Array(20..<25))
        XCTAssertEqual(ATMPagination.clampedPage(2, itemCount: 8, pageSize: 10), 0)
        XCTAssertEqual(ATMPagination.pageCount(itemCount: 0, pageSize: 10), 0)
    }

    func testMetricDisplaySeparatesValuesFromUnits() {
        XCTAssertEqual(
            ATMMetricDisplayValue.compact(241_700_000),
            ATMMetricDisplayValue(main: "241.7", unit: "M")
        )
        XCTAssertEqual(
            ATMMetricDisplayValue.compact(999),
            ATMMetricDisplayValue(main: "999", unit: "")
        )
        XCTAssertEqual(
            ATMMetricDisplayValue.percent(0.85),
            ATMMetricDisplayValue(main: "85", unit: "%")
        )
        XCTAssertEqual(
            ATMMetricDisplayValue.throughput(50.8),
            ATMMetricDisplayValue(main: "51", unit: "tok/s")
        )
        XCTAssertEqual(
            ATMMetricDisplayValue.duration(177),
            ATMMetricDisplayValue(main: "2", unit: "m 57s")
        )
    }

    func testQuotaCardSettingsOnlyAppearOnCardsThatHaveThem() throws {
        let json = """
        {"grokbuild":{"plan":"SuperGrok","source":"log","primary":{"used_percent":19,"window_minutes":10080,"resets_at":1785828533,"resets_in":"4d4h"}},
         "codex":{"plan":"prolite","primary":{"used_percent":26,"window_minutes":10080,"resets_at":1785909049,"resets_in":"5d2h"}}}
        """
        let data = Data(json.utf8)
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let byAgent = Dictionary(uniqueKeysWithValues: quota.cards.map { ($0.agent, $0) })

        XCTAssertEqual(byAgent["grokbuild"]?.settings, [.grokLiveQuota])
        XCTAssertEqual(byAgent["codex"]?.settings, [])
        // "Grok" from the usage filters names the same agent as "grokbuild".
        XCTAssertEqual(ATMQuotaCardSetting.settings(for: "Grok"), [.grokLiveQuota])
        XCTAssertEqual(ATMQuotaCardSetting.settings(for: "claude"), [])
    }

    func testUsageRenderKeyChangesOnlyForUsageInputs() {
        let quota = ATMQuotaSnapshot(agents: [:])
        let todaySessions = ATMTodaySessionsState()
        let baseline = ATMUsageRenderKey(
            refreshedAt: .distantPast,
            quota: quota,
            grokLiveQuotaEnabled: false,
            todaySessionsState: todaySessions
        )

        XCTAssertEqual(baseline, ATMUsageRenderKey(
            refreshedAt: .distantPast,
            quota: quota,
            grokLiveQuotaEnabled: false,
            todaySessionsState: todaySessions
        ))
        XCTAssertNotEqual(baseline, ATMUsageRenderKey(
            refreshedAt: Date(timeIntervalSince1970: 1),
            quota: quota,
            grokLiveQuotaEnabled: false,
            todaySessionsState: todaySessions
        ))
        XCTAssertNotEqual(baseline, ATMUsageRenderKey(
            refreshedAt: .distantPast,
            quota: quota,
            grokLiveQuotaEnabled: true,
            todaySessionsState: todaySessions
        ))
        XCTAssertNotEqual(baseline, ATMUsageRenderKey(
            refreshedAt: .distantPast,
            quota: quota,
            grokLiveQuotaEnabled: false,
            todaySessionsState: ATMTodaySessionsState(isLoading: true)
        ))
    }

    func testTodaySessionsCommandAndCachePolicyAreOnDemand() {
        XCTAssertEqual(
            ATMCommandBuilder.todaySessionUsage(),
            ["stats", "--by", "session-usage", "--days", "1", "--json"]
        )
        XCTAssertEqual(
            ATMCommandBuilder.todaySessionUsage(sessionID: "session-1"),
            [
                "stats", "--by", "session-usage", "--days", "1", "--json",
                "--agent-session", "session-1",
            ]
        )

        let loadedAt = Date(timeIntervalSince1970: 1_000)
        XCTAssertFalse(ATMTodaySessionsCachePolicy.shouldLoad(
            loadedAt: loadedAt,
            now: loadedAt.addingTimeInterval(299)
        ))
        XCTAssertTrue(ATMTodaySessionsCachePolicy.shouldLoad(
            loadedAt: loadedAt,
            now: loadedAt.addingTimeInterval(300)
        ))
        XCTAssertTrue(ATMTodaySessionsCachePolicy.shouldLoad(
            loadedAt: loadedAt,
            now: loadedAt,
            force: true
        ))
    }

    @MainActor
    func testDashboardRefreshPublishesOneAtomicState() {
        let store = ATMDataStore()
        var emissions = 0
        let subscription = store.$dashboardState
            .dropFirst()
            .sink { _ in emissions += 1 }
        var next = store.dashboardState
        next.errorMessage = "partial refresh"

        store.applyDashboardRefresh(next)

        XCTAssertEqual(emissions, 1)
        XCTAssertEqual(store.errorMessage, "partial refresh")
        withExtendedLifetime(subscription) {}
    }

    @MainActor
    func testSuccessfulArchiveImmediatelyMovesTodoOutOfDashboardState() throws {
        let todo = try makeTodo(
            id: "t161",
            project: "atm",
            created: "2026-07-31"
        )
        let store = ATMDataStore()
        var state = store.dashboardState
        state.allTodos = [todo]
        state.snapshot = ATMDashboardSnapshot(
            work: ATMNowSnapshot(
                generatedAt: "2026-07-31T12:00:00+08:00",
                open: [todo],
                working: [],
                waiting: [],
                review: [],
                blocked: [],
                due: [],
                summary: ATMWorkSummary(
                    open: 1,
                    inProgress: 0,
                    waiting: 0,
                    review: 0,
                    blocked: 0,
                    due: 0,
                    maintenance: 0
                )
            ),
            dayStats: [],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )
        store.applyDashboardRefresh(state)

        store.applySuccessfulTodoAction(.archive, on: todo)

        XCTAssertTrue(store.allTodos.isEmpty)
        XCTAssertTrue(store.snapshot.work.open.isEmpty)
        XCTAssertEqual(store.snapshot.work.summary.open, 0)
        XCTAssertEqual(store.archivedTodos.map(\.id), [todo.id])

        store.applySuccessfulTodoAction(.restore, on: todo)

        XCTAssertEqual(store.allTodos.map(\.id), [todo.id])
        XCTAssertTrue(store.archivedTodos.isEmpty)
    }

    @MainActor
    func testSuccessfulAcceptanceImmediatelyMovesReviewTodoToDone() throws {
        let todo = try makeTodo(
            id: "t162",
            project: "atm",
            created: "2026-07-31",
            status: "review"
        )
        let store = ATMDataStore()
        var state = store.dashboardState
        state.allTodos = [todo]
        state.snapshot = ATMDashboardSnapshot(
            work: ATMNowSnapshot(
                generatedAt: "2026-07-31T12:00:00+08:00",
                open: [],
                working: [],
                waiting: [],
                review: [todo],
                blocked: [],
                due: [],
                summary: ATMWorkSummary(
                    open: 0,
                    inProgress: 0,
                    waiting: 0,
                    review: 1,
                    blocked: 0,
                    due: 0,
                    maintenance: 0
                )
            ),
            dayStats: [],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            rangeData: [:],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )
        store.applyDashboardRefresh(state)

        store.applySuccessfulTodoAction(.complete, on: todo)

        XCTAssertEqual(store.allTodos.first?.status, "done")
        XCTAssertTrue(store.snapshot.work.review.isEmpty)
        XCTAssertEqual(store.snapshot.work.summary.review, 0)
    }

    /// Cost ATM guessed the rate for has to stay marked all the way to the row the
    /// usage page renders, and a row merging several models inherits the mark from
    /// any one of them: the merged total is only as sound as its weakest rate.
    func testUsageBreakdownCarriesTheEstimatedCostMark() {
        let dashboard = ATMDashboardSnapshot(
            work: .empty,
            dayStats: [],
            hourStats: [],
            modelDayStats: [],
            modelHourStats: [],
            rangeData: [
                .today: ATMRangeData(
                    modelStats: [
                        ATMModelStats(
                            client: "codex", model: "gpt-5.6-sol", sessions: 1,
                            inputTokens: 100, outputTokens: 10, cacheReadTokens: 0,
                            costUSD: 2, costEstimated: true
                        ),
                        ATMModelStats(
                            client: "codex", model: "codex-auto-review", sessions: 1,
                            inputTokens: 50, outputTokens: 5, cacheReadTokens: 0,
                            costUSD: 1, costEstimated: false
                        ),
                    ],
                    sessions: []
                ),
            ],
            liveStatus: .empty,
            currentSession: nil,
            refreshedAt: Date()
        )

        let models = dashboard.breakdown(for: .today, dimension: .model)
        XCTAssertEqual(models.count, 2)
        XCTAssertEqual(models.first(where: { $0.label == "gpt-5.6-sol" })?.costEstimated, true)
        XCTAssertEqual(models.first(where: { $0.label == "codex-auto-review" })?.costEstimated, false)

        // One client, two models, one of them estimated: the client row is marked.
        let clients = dashboard.breakdown(for: .today, dimension: .client)
        XCTAssertEqual(clients.count, 1)
        XCTAssertEqual(clients.first?.costEstimated, true)
        XCTAssertEqual(clients.first?.costUSD, 3)
    }

    /// A snapshot written before the mark existed still decodes; the rows read as
    /// exact rather than failing the whole refresh.
    func testModelStatsDefaultsToExactWhenTheMarkIsAbsent() throws {
        let json = """
        {"client":"codex","model":"gpt-5.5","sessions":1,"input_tokens":10,
         "output_tokens":2,"cache_read_tokens":0,"cost_usd":0.5}
        """
        let decoded = try JSONDecoder().decode(ATMModelStats.self, from: Data(json.utf8))
        XCTAssertFalse(decoded.costEstimated)
        XCTAssertEqual(decoded.costUSD, 0.5)
    }
}
