import XCTest
@testable import ATMMenuBarApp

final class ATMDesktopLayoutTests: XCTestCase {
    func testNavigationSidebarUsesCompactExpandedWidth() {
        XCTAssertEqual(ATMDesktopLayout.expandedSidebarWidth, 160)
        XCTAssertEqual(ATMDesktopLayout.collapsedSidebarWidth, 58)
    }

    func testDetailHeaderTabsAndContentShareTheSameLeadingEdge() {
        XCTAssertEqual(ATMDetailLayout.surfaceHorizontalInset, ATMDetailLayout.horizontalPadding)
        XCTAssertEqual(ATMDetailLayout.surfaceVerticalInset, ATMSpacing.medium)
        XCTAssertEqual(ATMDetailLayout.tabsHorizontalPadding, ATMDetailLayout.horizontalPadding)
        XCTAssertEqual(ATMDetailLayout.contentMaxWidth, 880)
    }

    func testCollectionSourceSheetKeepsACompactViewport() {
        XCTAssertEqual(CollectionSourceEditorLayout.sheetWidth, 560)
        XCTAssertEqual(CollectionSourceEditorLayout.sheetHeight(isNewSource: true), 560)
        XCTAssertEqual(CollectionSourceEditorLayout.sheetHeight(isNewSource: false), 520)
        XCTAssertGreaterThanOrEqual(CollectionSourceEditorLayout.advancedTriggerMinimumHeight, 44)
        XCTAssertGreaterThanOrEqual(CollectionSourceEditorLayout.choiceCardMinimumHeight, 44)
    }

    @MainActor
    func testNavigationHistoryRestoresSectionAndDetailSelection() {
        let navigation = ATMDesktopNavigation()

        navigation.selectedTodoID = "t1"
        navigation.selectedAgentID = "session-1"
        navigation.selectedAgentRunTodoID = "t1"
        navigation.section = .agents

        XCTAssertTrue(navigation.canGoBack)
        XCTAssertFalse(navigation.canGoForward)

        navigation.goBack()
        XCTAssertEqual(navigation.section, .tasks)
        XCTAssertEqual(navigation.selectedTodoID, "t1")
        // The task detail itself is also a destination; one more back returns
        // to the initial unselected task page.
        XCTAssertTrue(navigation.canGoBack)
        XCTAssertTrue(navigation.canGoForward)

        navigation.goForward()
        XCTAssertEqual(navigation.section, .agents)
        XCTAssertEqual(navigation.selectedAgentID, "session-1")
        XCTAssertEqual(navigation.selectedAgentRunTodoID, "t1")
        XCTAssertTrue(navigation.canGoBack)
        XCTAssertFalse(navigation.canGoForward)
    }

    @MainActor
    func testNavigationHistoryRestoresTaskListMode() {
        let navigation = ATMDesktopNavigation()
        navigation.taskListMode = .archive
        navigation.selectedTodoID = "t-deleted"
        navigation.section = .collection

        navigation.goBack()
        XCTAssertEqual(navigation.section, .tasks)
        XCTAssertEqual(navigation.taskListMode, .archive)
        XCTAssertEqual(navigation.selectedTodoID, "t-deleted")
    }

    @MainActor
    func testNewNavigationClearsForwardHistory() {
        let navigation = ATMDesktopNavigation()
        navigation.section = .usage
        navigation.section = .settings

        navigation.goBack()
        XCTAssertEqual(navigation.section, .usage)
        XCTAssertTrue(navigation.canGoForward)

        navigation.section = .collection
        XCTAssertFalse(navigation.canGoForward)

        navigation.goBack()
        XCTAssertEqual(navigation.section, .usage)
    }

    @MainActor
    func testCollectionNotificationRevealRepeatsForTheSameRecord() {
        let navigation = ATMDesktopNavigation()

        navigation.revealCollectionItem("ci1")
        XCTAssertEqual(navigation.selectedCollectionItemID, "ci1")
        XCTAssertEqual(navigation.collectionItemRevealRequest, 1)

        navigation.revealCollectionItem("ci1")
        XCTAssertEqual(navigation.collectionItemRevealRequest, 2)
    }
}
