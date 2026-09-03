import AppKit
import XCTest
@testable import ATMMenuBarApp

/// The growing title field is the one part of the edit form whose behaviour is not
/// visible in a diff: the height comes from AppKit's layout, and a measurement that
/// never fires (or never settles) reads exactly like the single-line box it
/// replaced. These drive the text view directly, without a window.
final class ATMComposerTextViewTests: XCTestCase {
    private let font = ATMFont.nsFont(.body, weight: .medium)
    private let inset = NSSize(width: 6, height: 6)

    private func makeTextView(width: CGFloat = 560) -> ATMComposerNSTextView {
        let textView = ATMComposerNSTextView.make(font: font, textInset: inset)
        textView.frame = NSRect(x: 0, y: 0, width: width, height: 40)
        return textView
    }

    func testMeasuredHeightGrowsWithWrappedLines() {
        let textView = makeTextView()
        var reported: [CGFloat] = []
        textView.onMeasuredHeight = { reported.append($0) }

        textView.string = "改标题"
        textView.reportMeasuredHeight()
        let single = reported.last ?? 0
        XCTAssertGreaterThan(single, 0, "一行文本也要报出高度，否则框永远停在初始值")

        // A title long enough to wrap several times at this width — the case the
        // single-line field scrolled sideways instead of showing.
        textView.string = String(repeating: "我发现我现在创建任务大部分都是直接写任务的标题。", count: 6)
        textView.reportMeasuredHeight()
        let wrapped = reported.last ?? 0
        XCTAssertGreaterThan(wrapped, single * 2, "换行后的高度必须明显高于一行")
    }

    func testMeasuredHeightIsReportedOncePerDistinctValue() {
        let textView = makeTextView()
        var reportCount = 0
        textView.onMeasuredHeight = { _ in reportCount += 1 }

        textView.string = "一样的标题"
        textView.reportMeasuredHeight()
        let afterFirst = reportCount
        XCTAssertEqual(afterFirst, 1)

        // Same text, same width: re-measuring must stay silent, or the frame it
        // drives bounces back through layout forever.
        textView.reportMeasuredHeight()
        textView.reportMeasuredHeight()
        XCTAssertEqual(reportCount, afterFirst)
    }

    func testNarrowerWidthReportsATallerBox() {
        let wide = makeTextView(width: 560)
        let narrow = makeTextView(width: 200)
        let title = String(repeating: "任务标题很长需要换行。", count: 4)
        var wideHeight: CGFloat = 0
        var narrowHeight: CGFloat = 0
        wide.onMeasuredHeight = { wideHeight = $0 }
        narrow.onMeasuredHeight = { narrowHeight = $0 }

        wide.string = title
        narrow.string = title
        wide.reportMeasuredHeight()
        narrow.reportMeasuredHeight()

        XCTAssertGreaterThan(narrowHeight, wideHeight, "窄面板里同样的标题要占更多行")
    }

    func testBoundedMeasurementStopsAtVisibleLinesAndShrinksAfterDeletion() {
        let textView = makeTextView(width: 200)
        let ceiling = NSLayoutManager().defaultLineHeight(for: font) * 4
        textView.maximumMeasuredHeight = ceiling
        var measured: CGFloat = 0
        textView.onMeasuredHeight = { measured = $0 }
        textView.string = String(repeating: "这是需要滚动查看的长输入。\n", count: 10_000)
        textView.reportMeasuredHeight()

        XCTAssertEqual(measured, ceiling, accuracy: 0.5)
        XCTAssertLessThan(
            textView.layoutManager?.firstUnlaidCharacterIndex() ?? Int.max,
            (textView.string as NSString).length,
            "仅测前四行不应强制全文布局"
        )

        textView.string = "短标题"
        textView.reportMeasuredHeight()
        XCTAssertGreaterThan(measured, 0)
        XCTAssertLessThan(measured, ceiling)
    }

    func testBoundedMeasurementShrinksWhenWidthGrowsAndIgnoresHeightOnlyResize() {
        let textView = makeTextView(width: 120)
        let ceiling = NSLayoutManager().defaultLineHeight(for: font) * 3
        textView.maximumMeasuredHeight = ceiling
        var reports: [CGFloat] = []
        textView.onMeasuredHeight = { reports.append($0) }
        textView.string = "一段在窄输入框中占多行的任务标题"
        textView.reportMeasuredHeight()
        XCTAssertEqual(reports.last ?? 0, ceiling, accuracy: 0.5)

        textView.setFrameSize(NSSize(width: 800, height: 40))
        XCTAssertLessThan(reports.last ?? ceiling, ceiling)
        let count = reports.count
        textView.setFrameSize(NSSize(width: 800, height: 100))
        XCTAssertEqual(reports.count, count)
    }

    func testSingleLineFieldKeepsPastedNewlinesOutOfTheStorage() {
        let textView = makeTextView()
        textView.allowsNewlines = false

        textView.insertText("第一行\n第二行\n\n第三行", replacementRange: NSRange(location: 0, length: 0))

        // Folded on the way in, so the binding and the storage never disagree about
        // what the title holds.
        XCTAssertEqual(textView.string, "第一行 第二行 第三行")
    }

    func testMultiLineFieldStillAcceptsNewlines() {
        let textView = makeTextView()

        textView.insertText("标题\n细节", replacementRange: NSRange(location: 0, length: 0))

		// The add sheet keeps the complete requirement block as the description.
        XCTAssertEqual(textView.string, "标题\n细节")
    }

	func testImageAwareComposerInterceptsPasteBeforeTextStorage() {
		let textView = makeTextView()
		var intercepted = false
		textView.onPasteImages = { pasteboard in
			intercepted = pasteboard === NSPasteboard.general
			return true
		}

		textView.paste(nil)

		XCTAssertTrue(intercepted)
		XCTAssertTrue(textView.string.isEmpty)
	}
}
