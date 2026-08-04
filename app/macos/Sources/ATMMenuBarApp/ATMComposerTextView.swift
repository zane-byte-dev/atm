import AppKit
import SwiftUI

/// Whether the composer should show its placeholder.
///
/// Chinese (and other) IMEs keep pre-edit text as marked text before commit.
/// That marked range can leave the SwiftUI `String` binding empty, so the
/// placeholder must also consult `hasMarkedText`.
enum ATMComposerPlaceholderPolicy {
    static func shouldShow(stringIsEmpty: Bool, hasMarkedText: Bool) -> Bool {
        stringIsEmpty && !hasMarkedText
    }
}

/// A multi-line text box where Return submits and Shift/Option+Return inserts a
/// newline. `TextEditor` cannot do this on macOS 13 -- it swallows Return, and
/// `onKeyPress` needs macOS 14 -- so the capture happens in the text view's
/// command dispatch instead.
///
/// Placeholder is drawn by AppKit (not a SwiftUI overlay) so it disappears as
/// soon as IME composition starts, not only after the characters are committed.
struct ATMComposerTextView: NSViewRepresentable {
    @Binding var text: String
    var placeholder: String = ""
    var font: NSFont = ATMFont.nsFont(.body)
    /// Take focus once, when the view first appears.
    var autoFocus = false
    /// Called when Return is pressed without a modifier. Nil disables submission.
    var onSubmit: (() -> Void)?

    func makeCoordinator() -> Coordinator {
        Coordinator(text: $text, onSubmit: onSubmit)
    }

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSScrollView()
        scrollView.drawsBackground = false
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.borderType = .noBorder
        scrollView.autoresizingMask = [.width, .height]

        let textView = ATMComposerNSTextView()
        textView.delegate = context.coordinator
        textView.minSize = .zero
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.textContainer?.containerSize = NSSize(
            width: 0,
            height: CGFloat.greatestFiniteMagnitude
        )
        textView.textContainer?.widthTracksTextView = true
        textView.string = text
        textView.font = font
        textView.placeholderString = placeholder
        textView.isRichText = false
        textView.allowsUndo = true
        textView.drawsBackground = false
        textView.textContainerInset = NSSize(width: 5, height: 8)
        // The composer is a form field, not a document: smart substitutions would
        // rewrite quotes and dashes inside task titles.
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false

        scrollView.documentView = textView

        if autoFocus {
            DispatchQueue.main.async {
                textView.window?.makeFirstResponder(textView)
            }
        }
        return scrollView
    }

    func updateNSView(_ nsView: NSScrollView, context: Context) {
        context.coordinator.text = $text
        context.coordinator.onSubmit = onSubmit
        guard let textView = nsView.documentView as? ATMComposerNSTextView else { return }
        textView.font = font
        textView.placeholderString = placeholder
        // Never replace the storage while an IME is composing — that clears
        // marked text and re-shows the placeholder mid-pinyin.
        guard !textView.hasMarkedText() else { return }
        if textView.string != text {
            textView.string = text
            textView.needsDisplay = true
        }
    }

    @MainActor
    final class Coordinator: NSObject, NSTextViewDelegate {
        var text: Binding<String>
        var onSubmit: (() -> Void)?

        init(text: Binding<String>, onSubmit: (() -> Void)?) {
            self.text = text
            self.onSubmit = onSubmit
        }

        func textDidChange(_ notification: Notification) {
            guard let textView = notification.object as? NSTextView else { return }
            // During IME composition the storage may already include marked text;
            // keep the binding in sync so suggestions update, but placeholder
            // visibility is owned by ATMComposerNSTextView.
            text.wrappedValue = textView.string
            textView.needsDisplay = true
        }

        func textView(_ textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            guard selector == #selector(NSResponder.insertNewline(_:)), let onSubmit else { return false }
            let modifiers = NSApp.currentEvent?.modifierFlags ?? []
            if modifiers.contains(.shift) || modifiers.contains(.option) {
                textView.insertNewlineIgnoringFieldEditor(nil)
                return true
            }
            onSubmit()
            return true
        }
    }
}

/// NSTextView that draws a form-style placeholder and hides it for marked text.
final class ATMComposerNSTextView: NSTextView {
    var placeholderString: String = "" {
        didSet {
            if oldValue != placeholderString {
                needsDisplay = true
            }
        }
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        drawPlaceholderIfNeeded()
    }

    override func didChangeText() {
        super.didChangeText()
        needsDisplay = true
    }

    override func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        super.setMarkedText(string, selectedRange: selectedRange, replacementRange: replacementRange)
        needsDisplay = true
    }

    override func unmarkText() {
        super.unmarkText()
        needsDisplay = true
    }

    private func drawPlaceholderIfNeeded() {
        guard ATMComposerPlaceholderPolicy.shouldShow(
            stringIsEmpty: string.isEmpty,
            hasMarkedText: hasMarkedText()
        ), !placeholderString.isEmpty else { return }

        let inset = textContainerInset
        let origin = NSPoint(x: inset.width + 4, y: inset.height + 2)
        let size = NSSize(
            width: max(0, bounds.width - origin.x - inset.width),
            height: max(0, bounds.height - origin.y - inset.height)
        )
        let attrs: [NSAttributedString.Key: Any] = [
            .font: font ?? ATMFont.nsFont(.body),
            .foregroundColor: NSColor.secondaryLabelColor,
        ]
        (placeholderString as NSString).draw(
            with: NSRect(origin: origin, size: size),
            options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine],
            attributes: attrs
        )
    }
}
