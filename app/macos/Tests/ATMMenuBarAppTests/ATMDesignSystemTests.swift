import XCTest
@testable import ATMMenuBarApp

final class ATMDesignSystemTests: XCTestCase {
    func testSpacingUsesFourPointRhythm() {
        XCTAssertEqual(ATMSpacing.xSmall, 4)
        XCTAssertEqual(ATMSpacing.small, 8)
        XCTAssertEqual(ATMSpacing.medium, 12)
        XCTAssertEqual(ATMSpacing.large, 16)
        XCTAssertEqual(ATMSpacing.xLarge, 24)
        XCTAssertEqual(ATMSpacing.xxLarge, 32)
    }

    func testRadiusHasOnlyFourSemanticLevels() {
        XCTAssertEqual(ATMRadius.control, 6)
        XCTAssertEqual(ATMRadius.row, 10)
        XCTAssertEqual(ATMRadius.panel, 12)
        XCTAssertEqual(ATMRadius.feature, 16)
    }

    func testGroupedNavigatorMatchesVisualContract() {
        XCTAssertEqual(ATMGroupedNavigatorMetrics.headerHeight, 64)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.groupHeight, 32)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.rowMinHeight, 64)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.rowCornerRadius, 10)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.selectedFillOpacity, 0.08)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.headerHorizontalInset, 20)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.contentHorizontalInset, 12)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.contentVerticalInset, 8)
        XCTAssertEqual(ATMGroupedNavigatorMetrics.groupSpacing, 4)
    }

    func testWorkspaceNavigatorWidthContract() {
        XCTAssertEqual(ATMWorkspaceLayout.navigatorDefaultWidth, 336)
        XCTAssertEqual(ATMWorkspaceLayout.navigatorMinWidth, 300)
        XCTAssertEqual(ATMWorkspaceLayout.navigatorMaxWidth, 420)
        XCTAssertGreaterThan(
            ATMWorkspaceLayout.navigatorDefaultWidth,
            ATMWorkspaceLayout.navigatorMinWidth
        )
        XCTAssertLessThan(
            ATMWorkspaceLayout.navigatorDefaultWidth,
            ATMWorkspaceLayout.navigatorMaxWidth
        )
    }

    func testReadingColumnKeepsReadableLineLength() {
        XCTAssertEqual(ATMWorkspaceLayout.readingColumnMaxWidth, 900)
        XCTAssertGreaterThan(
            ATMWorkspaceLayout.readingDetailMinWidth,
            ATMWorkspaceLayout.objectDetailMinWidth
        )
    }
}
