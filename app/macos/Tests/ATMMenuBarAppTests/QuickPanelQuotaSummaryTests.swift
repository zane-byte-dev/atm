import XCTest
@testable import ATMMenuBarApp

final class QuickPanelQuotaSummaryTests: XCTestCase {
    func testZeroPercentMeansUnusedInsteadOfExhausted() {
        let summary = ATMQuickQuotaSummary(entries: [
            entry("antigravity-1", title: "Antigravity 5h", percent: 0),
            entry("antigravity-2", title: "Antigravity 1w", percent: 0),
            entry("codex", title: "Codex", percent: 41),
            entry("grok", title: "Grok", percent: 0),
            entry("idealab", title: "IdeaLab", percent: 44),
        ])

        XCTAssertEqual(summary.usedCount, 2)
        XCTAssertEqual(summary.unusedCount, 3)
        XCTAssertEqual(summary.statusText, "2 个使用中 · 3 个未使用")
        XCTAssertEqual(summary.highlightedEntries.map(\.compactTitle), ["Codex", "IdeaLab"])
        XCTAssertEqual(summary.remainderText, "其他 3 个 未使用")
    }

    func testRoundedZeroPercentStaysUnusedInCompactCopy() {
        let summary = ATMQuickQuotaSummary(entries: [
            entry("tiny", title: "Tiny", percent: 0.4),
            entry("active", title: "Active", percent: 0.5),
        ])

        XCTAssertEqual(summary.usedCount, 1)
        XCTAssertEqual(summary.unusedCount, 1)
        XCTAssertEqual(summary.highlightedEntries.first?.roundedPercent, 1)
        XCTAssertEqual(summary.remainderText, "其他 1 个 未使用")
    }

    func testAllUnusedCopyDoesNotCallEveryEntryOther() {
        let summary = ATMQuickQuotaSummary(entries: [
            entry("first", title: "First", percent: 0),
            entry("second", title: "Second", percent: 0),
        ])

        XCTAssertTrue(summary.highlightedEntries.isEmpty)
        XCTAssertEqual(summary.remainderText, "全部 2 个 未使用")
    }

    func testUnavailableProviderRemainsVisibleAndIsNotCountedAsUnused() throws {
        let data = Data(
            """
            {
              "claude":{"provider_cards":[{
                "id":"daily","provider":"idealab","title":"MO计划-专项AK","period":"今日",
                "observed_at":"2026-08-21T09:22:50Z","metrics":[],
                "unavailable":true,"unavailable_reason":"empty"
              }]},
              "codex":{"primary":{"used_percent":41,"window_minutes":10080,
                "resets_at":1788143239,"resets_in":"6d18h"}},
              "grokbuild":{"primary":{"used_percent":3,"window_minutes":10080,
                "resets_at":1788143239,"resets_in":"6d18h"}}
            }
            """.utf8
        )
        let quota = try JSONDecoder().decode(ATMQuotaSnapshot.self, from: data)
        let summary = ATMQuickQuotaSummary(quota: quota)
        let idealab = try XCTUnwrap(summary.entries.first { $0.compactTitle == "Idealab" })

        XCTAssertNil(idealab.usedPercent)
        XCTAssertEqual(idealab.compactValueText, "暂无数据")
        XCTAssertEqual(summary.unusedCount, 0)
        XCTAssertEqual(summary.unavailableEntries.count, 1)
        XCTAssertEqual(summary.highlightedEntries.map(\.compactTitle), ["Codex", "Idealab"])
        XCTAssertEqual(summary.statusText, "2 个使用中 · 1 个暂无数据")
    }

    private func entry(_ id: String, title: String, percent: Double) -> ATMQuickQuotaEntry {
        ATMQuickQuotaEntry(
            id: id,
            agent: id,
            title: title,
            compactTitle: title,
            usedPercent: percent,
            help: title,
            unavailableText: nil
        )
    }
}
