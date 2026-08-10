import AppKit
import SwiftUI

/// Read-only viewer for one whole session transcript.
///
/// A single SwiftUI `Text` with `.textSelection(.enabled)` lays out the entire
/// string before it can draw the first line, and a bound session's transcript
/// routinely runs tens of thousands of characters — enough that opening the
/// sheet stalls and every scroll after it stutters. `NSTextView` lays out only
/// what the viewport needs, and brings ⌘F and whole-document selection with it.
struct ATMTranscriptTextView: NSViewRepresentable {
    let text: String
    var font: NSFont = .monospacedSystemFont(ofSize: ATMFont.Tier.body.size, weight: .regular)
    var insets = NSSize(width: 16, height: 14)
    var accessibilityLabel = "会话完整对话"
    var scrollsToEndOnUpdate = false

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let textView = NSTextView()
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = false
        textView.drawsBackground = false
        textView.textContainerInset = insets
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.minSize = .zero
        textView.maxSize = NSSize(
            width: CGFloat.greatestFiniteMagnitude,
            height: CGFloat.greatestFiniteMagnitude
        )
        textView.textContainer?.widthTracksTextView = true
        textView.textContainer?.containerSize = NSSize(
            width: 0,
            height: CGFloat.greatestFiniteMagnitude
        )
        // A transcript is a document, not a field: finding a phrase in it is the
        // main reason to open one this long.
        textView.usesFindBar = true
        textView.isIncrementalSearchingEnabled = true
        textView.font = font
        textView.textColor = .labelColor
        textView.setAccessibilityLabel(accessibilityLabel)
        // TextKit 1 lays the whole document out up front unless told otherwise;
        // TextKit 2 (the default from macOS 14) is already viewport-driven, and
        // reading `layoutManager` on it would silently downgrade the stack.
        if textView.textLayoutManager == nil {
            textView.layoutManager?.allowsNonContiguousLayout = true
        }
        apply(text, to: textView, coordinator: context.coordinator)

        let scrollView = NSScrollView()
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.borderType = .noBorder
        scrollView.drawsBackground = false
        scrollView.documentView = textView
        if scrollsToEndOnUpdate {
            scrollToEnd(textView)
        }
        return scrollView
    }

    func updateNSView(_ nsView: NSScrollView, context: Context) {
        guard let textView = nsView.documentView as? NSTextView else { return }
        if textView.font != font {
            textView.font = font
        }
        textView.setAccessibilityLabel(accessibilityLabel)
        // Compared against the coordinator's copy rather than `textView.string`:
        // SwiftUI re-runs this on unrelated redraws, and diffing a megabyte of
        // transcript out of the text storage each time is the cost we came to avoid.
        guard context.coordinator.appliedText != text else { return }
        apply(text, to: textView, coordinator: context.coordinator)
        if scrollsToEndOnUpdate {
            scrollToEnd(textView)
        } else {
            textView.scroll(.zero)
        }
    }

    private func apply(_ text: String, to textView: NSTextView, coordinator: Coordinator) {
        textView.string = text
        // `string` replaces the storage wholesale, which drops the font on some
        // macOS versions; re-stating it is cheaper than building an attributed copy.
        textView.font = font
        coordinator.appliedText = text
    }

    private func scrollToEnd(_ textView: NSTextView) {
        let end = (textView.string as NSString).length
        textView.scrollRangeToVisible(NSRange(location: end, length: 0))
    }

    final class Coordinator {
        var appliedText: String?
    }
}
