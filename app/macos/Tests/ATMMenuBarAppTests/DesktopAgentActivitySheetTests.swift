import XCTest
@testable import ATMMenuBarApp

final class DesktopAgentActivitySheetTests: XCTestCase {
    func testFullActivitySheetExposesUpdatesAndTranscriptTabs() {
        XCTAssertEqual(ATMAgentActivitySheetTab.allCases, [.updates, .transcript])
        XCTAssertEqual(ATMAgentActivitySheetTab.updates.title, "执行动态")
        XCTAssertEqual(ATMAgentActivitySheetTab.transcript.title, "完整对话")
        XCTAssertEqual(ATMAgentActivitySheetTab.updates.systemImage, "clock.arrow.circlepath")
        XCTAssertEqual(ATMAgentActivitySheetTab.transcript.systemImage, "text.bubble")
    }
}
