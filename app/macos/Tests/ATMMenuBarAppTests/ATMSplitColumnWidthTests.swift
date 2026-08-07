import CoreGraphics
import XCTest
@testable import ATMMenuBarApp

/// The workspace columns keep their width in `UserDefaults` instead of relying on
/// `HSplitView`, which forgot the divider position whenever the surrounding
/// hierarchy was rebuilt. Everything that clamps a remembered width lives in
/// `ATMSplitColumnWidth`, because a stored value can be stale, out of range, or
/// larger than the window it is restored into.
final class ATMSplitColumnWidthTests: XCTestCase {
    private let range: ClosedRange<CGFloat> = 260...420
    private let detailMin: CGFloat = 400

    private func resolve(_ requested: CGFloat, available: CGFloat = 1200) -> CGFloat {
        ATMSplitColumnWidth.resolve(
            requested: requested,
            available: available,
            range: range,
            detailMinWidth: detailMin
        )
    }

    func testWidthInsideTheRangeSurvivesUntouched() {
        XCTAssertEqual(resolve(330), 330)
    }

    func testWidthOutsideTheRangeIsClampedToIt() {
        XCTAssertEqual(resolve(80), 260)
        XCTAssertEqual(resolve(1000), 420)
    }

    /// A width remembered from a wide window must not eat the detail pane after
    /// the window is made narrow.
    func testDetailPaneKeepsItsMinimumInANarrowWindow() {
        // 700 available - 400 detail - 1 divider leaves 299 for the sidebar.
        XCTAssertEqual(resolve(420, available: 700), 299)
    }

    /// Below that, the sidebar wins: a sidebar squeezed under its minimum is
    /// unusable, while a narrow detail pane still scrolls.
    func testSidebarMinimumWinsWhenNothingFits() {
        XCTAssertEqual(resolve(420, available: 500), 260)
        XCTAssertEqual(resolve(300, available: 0), 260)
    }

    /// A drag reports fractional offsets. Passing those straight into a frame let
    /// the sidebar's own layout round differently from the width it was handed,
    /// which read as the column twitching by a point under the cursor.
    func testFractionalWidthsSnapToWholePoints() {
        XCTAssertEqual(resolve(330.4), 330)
        XCTAssertEqual(resolve(330.6), 331)
    }

    /// The window-driven ceiling is floored rather than rounded, so a fractional
    /// window width cannot round half a point off the detail pane's minimum.
    func testCeilingIsFlooredNotRounded() {
        XCTAssertEqual(resolve(420, available: 700.5), 299)
    }

    /// First layout passes and corrupt stored values both reach this function; a
    /// NaN slips through every comparison and would blank the pane.
    func testNaNInputsFallBackToTheMinimum() {
        XCTAssertEqual(resolve(.nan), 260)
        XCTAssertEqual(resolve(330, available: .nan), 260)
    }

    func testInfiniteWidthClampsLikeAnyOtherNumber() {
        XCTAssertEqual(resolve(.infinity), 420)
    }

    /// Per-pane keys: resizing the task list must not move the knowledge list.
    func testDefaultsKeysAreNamespacedPerColumn() {
        XCTAssertEqual(ATMSplitColumnWidth.defaultsKey("tasks"), "ATMSplitColumnWidth.tasks")
        XCTAssertNotEqual(
            ATMSplitColumnWidth.defaultsKey("tasks"),
            ATMSplitColumnWidth.defaultsKey("knowledge")
        )
    }
}
