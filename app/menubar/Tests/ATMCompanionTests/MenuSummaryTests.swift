import XCTest
@testable import ATMCompanion

final class MenuSummaryTests: XCTestCase {
    @MainActor
    func testMenuBarBrandImageIsATemplateAtNativeSize() {
        let image = ATMCompanionBrandAssets.menuBarImage(scale: 2)

        XCTAssertTrue(image.isTemplate)
        XCTAssertEqual(image.size.width, 18)
        XCTAssertEqual(image.size.height, 18)
    }

    func testServiceTitleOmitsZeroAttention() {
        XCTAssertEqual(
            CompanionMenuPresentation.serviceTitle(active: 10, attention: 0),
            "服务运行中 · 10 个会话"
        )
        XCTAssertEqual(
            CompanionMenuPresentation.serviceTitle(active: 2, attention: 3),
            "服务运行中 · 2 个会话 · 3 项待处理"
        )
    }

    func testCompanionPayloadDecodesTaskStatesAndQuotaWindows() throws {
        let json = #"""
        {
          "snapshot":{"active_count":2,"attention_count":1},
          "todos":{"items":[
            {"id":"t12","title":"Review it","status":"review","priority":"p0","project":"atm","review_at":"2026-09-04T00:00:00Z","menu_state":"review"},
            {"id":"t13","title":"Waiting","status":"in_progress","priority":"p1","wake_condition":"CI passes","menu_state":"waiting"}
          ],"total":2,"truncated":false},
          "quota":{"source":"cache","generated_at":"2026-09-04T00:00:00Z","windows":[
            {"agent":"codex","window_minutes":300,"used_percent":37,"remaining_percent":63,"resets_at":1788487200,"observed_at":"2026-09-04T00:00:00Z","stale":false,"reset_elapsed":false}
          ],"truncated":false}
        }
        """#
        let state = try JSONDecoder().decode(CompanionState.self, from: Data(json.utf8))

        XCTAssertEqual(state.todos?.items[0].menuState, "review")
        XCTAssertEqual(state.todos?.items[1].wakeCondition, "CI passes")
        XCTAssertEqual(state.quota?.windows[0].remainingPercent, 63)
        XCTAssertEqual(CompanionMenuPresentation.taskRows(state.todos).map(\.detail), ["待验收 · atm", "进行中 · 等待 · 未分项目"])
    }

    func testResetElapsedQuotaIsUnknownInsteadOfZero() throws {
        let quota = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":82,"remaining_percent":18,"resets_at":1,"observed_at":"2026-09-01T00:00:00Z","stale":true,"reset_elapsed":true"#)
        let row = try XCTUnwrap(CompanionMenuPresentation.quotaRows(quota).first)

        XCTAssertEqual(row.detail, "重置后待更新 · 上次 82%")
        XCTAssertFalse(row.detail.contains("已用 0%"))
        XCTAssertFalse(row.detail.contains("分钟后重置"))
        XCTAssertFalse(row.detail.contains("小时后重置"))
        XCTAssertFalse(row.detail.contains("天后重置"))
        XCTAssertFalse(row.detail.contains("记录较旧"))
    }

    func testRealZeroQuotaRemainsAHealthyReading() throws {
        let quota = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":0,"remaining_percent":100,"resets_at":4102444800,"observed_at":"2026-09-04T00:00:00Z","stale":false,"reset_elapsed":false"#)
        let row = try XCTUnwrap(CompanionMenuPresentation.quotaRows(quota, now: Date(timeIntervalSince1970: 4_000_000_000)).first)

        XCTAssertTrue(row.detail.hasPrefix("已用 0%"))
    }

    func testQuotaSummaryShowsAtMostThreeRows() throws {
        let json = #"""
        {"source":"cache","generated_at":"now","windows":[
          {"agent":"codex","window_minutes":300,"used_percent":10,"stale":false,"reset_elapsed":false},
          {"agent":"codex","window_minutes":10080,"used_percent":20,"stale":false,"reset_elapsed":false},
          {"agent":"claude","window_minutes":300,"used_percent":30,"stale":false,"reset_elapsed":false},
          {"agent":"claude","window_minutes":10080,"used_percent":40,"stale":false,"reset_elapsed":false}
        ],"truncated":false}
        """#
        let quota = try JSONDecoder().decode(CompanionQuota.self, from: Data(json.utf8))

        XCTAssertEqual(CompanionMenuPresentation.quotaRows(quota).count, 3)
    }

    func testStatusBarSummaryIsBoundedAndUsesTightestLiveWindow() throws {
        let quotaJSON = #"""
        {"source":"cache","generated_at":"now","windows":[
          {"agent":"codex","window_minutes":300,"used_percent":87,"remaining_percent":13,"resets_at":4102444800,"observed_at":"now","stale":false,"reset_elapsed":false},
          {"agent":"claude","window_minutes":300,"used_percent":91,"remaining_percent":9,"resets_at":1,"observed_at":"old","stale":true,"reset_elapsed":true}
        ],"truncated":false}
        """#
        let quota = try JSONDecoder().decode(CompanionQuota.self, from: Data(quotaJSON.utf8))
        let todosJSON = #"{"items":[{"id":"t1","title":"x","status":"review","priority":"p0","menu_state":"review"}],"total":4,"truncated":false}"#
        let todos = try JSONDecoder().decode(CompanionTodos.self, from: Data(todosJSON.utf8))

        let title = CompanionMenuPresentation.statusBarTitle(
            attention: 2,
            quota: quota,
            todayTokens: .value(387_000_000)
        )
        XCTAssertEqual(title, "待 2 · Codex 87% · 387M")
        XCTAssertLessThanOrEqual(title.count, 42)
        XCTAssertEqual(
            CompanionMenuPresentation.statusBarTooltip(
                active: 3,
                attention: 2,
                todos: todos,
                quota: quota,
                todayTokens: .value(387_000_000)
            ),
            "3 个活跃会话 · 2 项待处理 · 4 个当前任务 · Codex 5 小时已用 87% · 今日 Token 387M"
        )
    }

    func testStatusBarOnlyShowsCurrentQuotaAtLegacyWarningThreshold() throws {
        let below = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":74.9,"remaining_percent":25.1,"resets_at":4102444800,"observed_at":"now","stale":false,"reset_elapsed":false"#)
        XCTAssertEqual(CompanionMenuPresentation.statusBarTitle(attention: 0, quota: below, todayTokens: .loading), "")

        let warning = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":75,"remaining_percent":25,"resets_at":4102444800,"observed_at":"now","stale":false,"reset_elapsed":false"#)
        XCTAssertEqual(CompanionMenuPresentation.statusBarTitle(attention: 0, quota: warning, todayTokens: .loading), "Codex 75%")

        let expired = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":99,"remaining_percent":1,"resets_at":1,"observed_at":"old","stale":true,"reset_elapsed":true"#)
        XCTAssertEqual(CompanionMenuPresentation.statusBarTitle(attention: 0, quota: expired, todayTokens: .loading), "")

        let stale = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":90,"remaining_percent":10,"resets_at":4102444800,"observed_at":"old","stale":true,"reset_elapsed":false"#)
        XCTAssertEqual(CompanionMenuPresentation.statusBarTitle(attention: 0, quota: stale, todayTokens: .loading), "")
        XCTAssertTrue(CompanionMenuPresentation.quotaRows(stale).first?.detail.contains("记录较旧") == true)
    }

    func testAllTaskMenuStatesAndMissingProjectAreVisible() throws {
        let json = #"""
        {"items":[
          {"id":"t1","title":"review","status":"review","priority":"p0","project":"atm","menu_state":"review"},
          {"id":"t2","title":"blocked","status":"blocked","priority":"p1","project":"atm","menu_state":"blocked"},
          {"id":"t3","title":"due","status":"in_progress","priority":"p2","project":"atm","menu_state":"due"},
          {"id":"t4","title":"waiting","status":"in_progress","priority":"p3","project":"atm","menu_state":"waiting"},
          {"id":"t5","title":"working","status":"in_progress","priority":"","project":"","menu_state":"working"}
        ],"total":5,"truncated":false}
        """#
        let todos = try JSONDecoder().decode(CompanionTodos.self, from: Data(json.utf8))
        XCTAssertEqual(CompanionMenuPresentation.taskRows(todos).map(\.detail), [
            "待验收 · atm", "已阻塞 · atm", "到期需处理 · atm", "进行中 · 等待 · atm", "进行中 · 未分项目",
        ])
    }

    func testStatusBarDefaultsToTokenWithoutTaskOrSessionCounts() {
        XCTAssertEqual(
            CompanionMenuPresentation.statusBarTitle(attention: 0, quota: nil, todayTokens: .value(387_000_000)),
            "387M"
        )
        XCTAssertEqual(
            CompanionMenuPresentation.statusBarTitle(attention: 3, quota: nil, todayTokens: .value(387_000_000)),
            "待 3 · 387M"
        )
        XCTAssertEqual(
            CompanionMenuPresentation.statusBarTooltip(
                active: 0,
                attention: 0,
                todos: nil,
                quota: nil,
                todayTokens: .value(0)
            ),
            "0 个活跃会话 · 0 项待处理 · 今日 Token 0"
        )
        XCTAssertFalse(
            CompanionMenuPresentation.statusBarTooltip(
                active: 1,
                attention: 0,
                todos: nil,
                quota: nil,
                todayTokens: .unavailable("用量暂不可用")
            ).contains("Token")
        )
    }

    func testStatusBarLimitPreservesQuotaWarning() throws {
        let quota = try decodeQuota(window: #""agent":"codex","window_minutes":300,"used_percent":92,"remaining_percent":8,"resets_at":4102444800,"observed_at":"now","stale":false,"reset_elapsed":false"#)

        let title = CompanionMenuPresentation.statusBarTitle(
            attention: 123456789,
            quota: quota,
            todayTokens: .value(387_000_000)
        )

        XCTAssertLessThanOrEqual(title.count, 42)
        XCTAssertTrue(title.hasSuffix("Codex 92% · 387M"))
    }

    func testHeadersDistinguishLoadingEmptyAndSectionError() throws {
        XCTAssertEqual(CompanionMenuPresentation.taskHeader(nil), "当前任务 · 加载中")
        XCTAssertEqual(CompanionMenuPresentation.quotaHeader(nil), "Agent 额度 · 加载中")

        let emptyTodos = try JSONDecoder().decode(CompanionTodos.self, from: Data(#"{"items":[],"total":0,"truncated":false}"#.utf8))
        XCTAssertEqual(CompanionMenuPresentation.taskHeader(emptyTodos), "当前任务 · 无")
        let errorTodos = try JSONDecoder().decode(CompanionTodos.self, from: Data(#"{"items":[],"total":0,"truncated":false,"error":"任务暂不可用"}"#.utf8))
        XCTAssertEqual(CompanionMenuPresentation.taskHeader(errorTodos), "当前任务 · 暂不可用")

        let emptyQuota = try JSONDecoder().decode(CompanionQuota.self, from: Data(#"{"source":"","generated_at":"","windows":[],"truncated":false}"#.utf8))
        XCTAssertEqual(CompanionMenuPresentation.quotaHeader(emptyQuota), "Agent 额度 · 暂无数据")
        let errorQuota = try JSONDecoder().decode(CompanionQuota.self, from: Data(#"{"source":"","generated_at":"","windows":[],"truncated":false,"error":"额度暂不可用"}"#.utf8))
        XCTAssertEqual(CompanionMenuPresentation.quotaHeader(errorQuota), "Agent 额度 · 暂不可用")
    }

    func testRoutesStayOnTicketOriginAndKeepTicketFragment() {
        let ticket = URL(string: "http://127.0.0.1:47321/#ticket=secret")!
        XCTAssertEqual(RuntimeRoute.tasks.applying(to: ticket).absoluteString, "http://127.0.0.1:47321/tasks#ticket=secret")
        XCTAssertEqual(RuntimeRoute.newTask.applying(to: ticket).absoluteString, "http://127.0.0.1:47321/tasks?new=1#ticket=secret")
        XCTAssertEqual(RuntimeRoute.collection.applying(to: ticket).absoluteString, "http://127.0.0.1:47321/collection#ticket=secret")
        XCTAssertEqual(RuntimeRoute.task("t356").applying(to: ticket).absoluteString, "http://127.0.0.1:47321/tasks/t356#ticket=secret")
        XCTAssertEqual(RuntimeRoute.usage(agent: "codex").applying(to: ticket).absoluteString, "http://127.0.0.1:47321/usage?agent=codex#ticket=secret")
        XCTAssertEqual(RuntimeRoute.task("../../bad").applying(to: ticket), ticket)
    }

    private func decodeQuota(window: String) throws -> CompanionQuota {
        let json = "{\"source\":\"cache\",\"generated_at\":\"now\",\"windows\":[{\(window)}],\"truncated\":false}"
        return try JSONDecoder().decode(CompanionQuota.self, from: Data(json.utf8))
    }
}

final class TodayTokenMenuPresentationTests: XCTestCase {
    func testTopLevelTodayUsageDistinguishesZeroAndARealValue() throws {
        let zero = try decodeState(today: #"{"total_tokens":0,"sessions":0,"queries":0}"#)
        let zeroState = TodayTokenMenuPresentation.resolve(today: zero.todayUsage, legacyQuick: zero.legacyQuick)
        XCTAssertEqual(zeroState, .value(0))
        XCTAssertEqual(TodayTokenMenuPresentation.title(zeroState), "今日 Token · 0")

        let used = try decodeState(today: #"{"total_tokens":12345,"sessions":3,"queries":9}"#)
        let usedState = TodayTokenMenuPresentation.resolve(today: used.todayUsage, legacyQuick: used.legacyQuick)
        XCTAssertEqual(usedState, .value(12_345))
        XCTAssertEqual(TodayTokenMenuPresentation.title(usedState), "今日 Token · 12.3K")
    }

    func testTodayUsageErrorAndLoadingHaveClearMenuStates() throws {
        XCTAssertEqual(TodayTokenMenuPresentation.title(.loading), "今日 Token · 加载中…")
        let state = try decodeState(today: #"{"total_tokens":0,"sessions":0,"queries":0,"error":"用量暂不可用"}"#)
        let resolved = TodayTokenMenuPresentation.resolve(today: state.todayUsage, legacyQuick: state.legacyQuick)
        XCTAssertEqual(resolved, .unavailable("用量暂不可用"))
        XCTAssertEqual(TodayTokenMenuPresentation.title(resolved), "今日 Token · 暂不可用")
        XCTAssertEqual(TodayTokenMenuPresentation.detail(resolved), "用量暂不可用")
    }

    func testLegacyQuickTodayRemainsReadableDuringRuntimeHandover() throws {
        let json = #"""
        {"snapshot":{"active_count":0,"attention_count":0},"quick":{"usage":{"ranges":{"today":{"total_tokens":9876}}}}}
        """#
        let state = try JSONDecoder().decode(CompanionState.self, from: Data(json.utf8))
        let resolved = TodayTokenMenuPresentation.resolve(today: state.todayUsage, legacyQuick: state.legacyQuick)
        XCTAssertEqual(TodayTokenMenuPresentation.title(resolved), "今日 Token · 9.9K")
    }

    private func decodeState(today: String) throws -> CompanionState {
        let json = "{\"snapshot\":{\"active_count\":0,\"attention_count\":0},\"today_usage\":\(today)}"
        return try JSONDecoder().decode(CompanionState.self, from: Data(json.utf8))
    }
}
