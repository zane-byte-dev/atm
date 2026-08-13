import AppKit
import XCTest
@testable import ATMMenuBarApp

final class FloatingPanelTests: XCTestCase {
    func testQuickPanelTouchesBottomEdgeOfMenuBarButton() {
        let button = NSRect(x: 1200, y: 900, width: 28, height: 24)
        let panel = NSSize(width: 360, height: 420)
        let visibleFrame = NSRect(x: 0, y: 0, width: 1440, height: 900)

        let origin = FloatingPanel.anchoredOrigin(
            buttonRect: button,
            panelSize: panel,
            visibleFrame: visibleFrame
        )

        XCTAssertEqual(origin.y + panel.height, button.minY, accuracy: 0.001)
    }

    func testQuickPanelKeepsHorizontalScreenMargin() {
        let button = NSRect(x: 1430, y: 900, width: 10, height: 24)
        let panel = NSSize(width: 360, height: 420)
        let visibleFrame = NSRect(x: 0, y: 0, width: 1440, height: 900)

        let origin = FloatingPanel.anchoredOrigin(
            buttonRect: button,
            panelSize: panel,
            visibleFrame: visibleFrame
        )

        XCTAssertEqual(origin.x + panel.width, visibleFrame.maxX - 10, accuracy: 0.001)
    }
}
