import XCTest
@testable import ATMMenuBarApp

final class ATMDesktopLayoutTests: XCTestCase {
    func testNavigationSidebarUsesCompactExpandedWidth() {
        XCTAssertEqual(ATMDesktopLayout.expandedSidebarWidth, 160)
        XCTAssertEqual(ATMDesktopLayout.collapsedSidebarWidth, 58)
    }
}
