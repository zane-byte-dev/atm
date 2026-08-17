import CoreGraphics
import XCTest
@testable import ATMMenuBarApp

final class ATMDesktopSidebarLayoutTests: XCTestCase {
    func testExpandedWidthIsClampedAndRounded() {
        XCTAssertEqual(
            ATMDesktopLayout.resolvedExpandedSidebarWidth(ATMDesktopLayout.expandedSidebarWidth),
            ATMDesktopLayout.expandedSidebarWidth
        )
        XCTAssertEqual(ATMDesktopLayout.resolvedExpandedSidebarWidth(80), 132)
        XCTAssertEqual(ATMDesktopLayout.resolvedExpandedSidebarWidth(400), 280)
        XCTAssertEqual(ATMDesktopLayout.resolvedExpandedSidebarWidth(172.6), 173)
    }

    func testInvalidStoredWidthFallsBackToDefault() {
        XCTAssertEqual(
            ATMDesktopLayout.resolvedExpandedSidebarWidth(.nan),
            ATMDesktopLayout.expandedSidebarWidth
        )
        XCTAssertEqual(
            ATMDesktopLayout.resolvedExpandedSidebarWidth(.infinity),
            ATMDesktopLayout.expandedSidebarWidth
        )
    }

    func testDraggingThroughThresholdCollapsesSidebar() {
        XCTAssertFalse(
            ATMDesktopLayout.sidebarIsCollapsed(
                at: ATMDesktopLayout.sidebarCollapseThreshold + 1,
                wasCollapsed: false
            )
        )
        XCTAssertTrue(
            ATMDesktopLayout.sidebarIsCollapsed(
                at: ATMDesktopLayout.sidebarCollapseThreshold,
                wasCollapsed: false
            )
        )
        XCTAssertTrue(ATMDesktopLayout.sidebarIsCollapsed(at: .nan, wasCollapsed: false))
    }

    func testCollapsedSidebarUsesHysteresisBeforeExpanding() {
        XCTAssertTrue(
            ATMDesktopLayout.sidebarIsCollapsed(
                at: ATMDesktopLayout.sidebarCollapseThreshold + 1,
                wasCollapsed: true
            )
        )
        XCTAssertFalse(
            ATMDesktopLayout.sidebarIsCollapsed(
                at: ATMDesktopLayout.sidebarExpansionThreshold,
                wasCollapsed: true
            )
        )
    }

    func testCollapsedWidthLeavesEnoughDragDistanceBeforeExpansion() {
        XCTAssertLessThan(
            ATMDesktopLayout.collapsedSidebarWidth,
            ATMDesktopLayout.sidebarCollapseThreshold
        )
        XCTAssertLessThan(
            ATMDesktopLayout.sidebarCollapseThreshold,
            ATMDesktopLayout.sidebarExpansionThreshold
        )
        XCTAssertLessThan(
            ATMDesktopLayout.sidebarExpansionThreshold,
            ATMDesktopLayout.minimumExpandedSidebarWidth
        )
    }

    func testSidebarWidthUsesDedicatedDefaultsKey() {
        XCTAssertEqual(ATMDesktopLayout.sidebarWidthDefaultsKey, "ATMDesktopSidebarWidth")
    }
}
