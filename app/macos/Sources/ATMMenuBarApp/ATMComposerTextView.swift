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

/// Text for a field that holds exactly one line.
///
/// A task title is written as one line everywhere it is read -- list rows, the
/// `--title` argument, the detail header -- so pasting a multi-line blob into
/// one should flatten rather than smuggle line breaks through. Runs of blank
/// lines collapse to a single space instead of a gap of spaces.
enum ATMComposerText {
    static func singleLine(_ text: String) -> String {
        text.split(whereSeparator: \.isNewline).joined(separator: " ")
    }
}

/// How tall a composer that grows with its content should be: never shorter
/// than `minLines`, never taller than `maxLines` -- past that it scrolls.
///
/// Separate from the view so the clamping is testable without a window.
enum ATMComposerHeight {
    /// - Parameter contentHeight: laid-out height of the text alone, insets excluded.
    static func clamped(
        contentHeight: CGFloat,
        lineHeight: CGFloat,
        minLines: Int,
        maxLines: Int,
        verticalInset: CGFloat
    ) -> CGFloat {
        let lower = max(1, minLines)
        let upper = max(lower, maxLines)
        let bounded = min(max(contentHeight, lineHeight * CGFloat(lower)), lineHeight * CGFloat(upper))
        return (bounded + verticalInset * 2).rounded(.up)
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
    /// False turns the box into a one-line field: the keyboard can never insert a
    /// newline (so Return only ever submits) and pasted line breaks fold to spaces.
    var allowsNewlines = true
    /// Inner padding. Fields sized to one or two lines want less of it than the
    /// add sheet's tall box.
    var textInset = NSSize(width: 5, height: 8)
    /// Laid-out height of the text, insets excluded, reported whenever the content
    /// or the available width changes. Drives `ATMGrowingTextField`.
    var onMeasuredHeight: ((CGFloat) -> Void)?
    /// Called when Return is pressed without a modifier. Nil disables submission.
    var onSubmit: (() -> Void)?

    func makeCoordinator() -> Coordinator {
        Coordinator(text: $text, allowsNewlines: allowsNewlines, onSubmit: onSubmit)
    }

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSScrollView()
        scrollView.drawsBackground = false
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.borderType = .noBorder
        scrollView.autoresizingMask = [.width, .height]

        let textView = ATMComposerNSTextView.make(font: font, textInset: textInset)
        textView.delegate = context.coordinator
        textView.string = text
        textView.placeholderString = placeholder
        textView.allowsNewlines = allowsNewlines
        textView.onMeasuredHeight = onMeasuredHeight

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
        context.coordinator.allowsNewlines = allowsNewlines
        context.coordinator.onSubmit = onSubmit
        guard let textView = nsView.documentView as? ATMComposerNSTextView else { return }
        textView.font = font
        textView.placeholderString = placeholder
        textView.allowsNewlines = allowsNewlines
        textView.onMeasuredHeight = onMeasuredHeight
        textView.textContainerInset = textInset
        // Never replace the storage while an IME is composing — that clears
        // marked text and re-shows the placeholder mid-pinyin.
        guard !textView.hasMarkedText() else { return }
        if textView.string != text {
            textView.string = text
            textView.needsDisplay = true
            // Assigning `string` skips `didChangeText`, so a value that arrives from
            // the binding (opening the edit form) still has to remeasure.
            textView.reportMeasuredHeight()
        }
    }

    @MainActor
    final class Coordinator: NSObject, NSTextViewDelegate {
        var text: Binding<String>
        var allowsNewlines: Bool
        var onSubmit: (() -> Void)?

        init(text: Binding<String>, allowsNewlines: Bool, onSubmit: (() -> Void)?) {
            self.text = text
            self.allowsNewlines = allowsNewlines
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
            guard selector == #selector(NSResponder.insertNewline(_:)) else { return false }
            // A one-line field swallows Return whether or not it can submit —
            // letting it through would put a line break in a title.
            guard allowsNewlines else {
                onSubmit?()
                return true
            }
            guard let onSubmit else { return false }
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
    /// The one place the composer's text view is configured — wrapping and inset
    /// decide what `reportMeasuredHeight` returns, so a test measuring the growing
    /// field has to start from the same object the representable builds.
    static func make(font: NSFont, textInset: NSSize) -> ATMComposerNSTextView {
        let textView = ATMComposerNSTextView()
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
        textView.font = font
        textView.isRichText = false
        textView.allowsUndo = true
        textView.drawsBackground = false
        textView.textContainerInset = textInset
        // The composer is a form field, not a document: smart substitutions would
        // rewrite quotes and dashes inside task titles.
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false
        return textView
    }

    var placeholderString: String = "" {
        didSet {
            if oldValue != placeholderString {
                needsDisplay = true
            }
        }
    }

    /// See `ATMComposerTextView.allowsNewlines`.
    var allowsNewlines = true

    /// See `ATMComposerTextView.onMeasuredHeight`.
    var onMeasuredHeight: ((CGFloat) -> Void)?

    private var lastMeasuredHeight: CGFloat = -1
    private var isMeasuring = false

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        drawPlaceholderIfNeeded()
    }

    override func didChangeText() {
        super.didChangeText()
        needsDisplay = true
        reportMeasuredHeight()
    }

    /// Width drives wrapping, so the height has to be recomputed when the pane is
    /// resized — not only when the text changes.
    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        reportMeasuredHeight()
    }

    override func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        super.setMarkedText(string, selectedRange: selectedRange, replacementRange: replacementRange)
        needsDisplay = true
    }

    override func unmarkText() {
        super.unmarkText()
        needsDisplay = true
    }

    /// Typing, pasting and drag-and-drop all land here, so folding newlines at this
    /// one point keeps them out of the storage entirely — the binding and the text
    /// view never disagree about what the field holds.
    override func insertText(_ string: Any, replacementRange: NSRange) {
        guard !allowsNewlines else {
            super.insertText(string, replacementRange: replacementRange)
            return
        }
        let plain = (string as? NSAttributedString)?.string ?? string as? String
        guard let plain else {
            super.insertText(string, replacementRange: replacementRange)
            return
        }
        super.insertText(ATMComposerText.singleLine(plain), replacementRange: replacementRange)
    }

    /// Reports the laid-out text height, ignoring repeats so a caller that turns
    /// the value into a frame does not bounce back through here forever.
    ///
    /// `ensureLayout` can resize a vertically-resizable text view, which lands back
    /// in `setFrameSize`, so the pass also guards against re-entering itself.
    func reportMeasuredHeight() {
        guard !isMeasuring else { return }
        guard let onMeasuredHeight, let container = textContainer, let manager = layoutManager else { return }
        isMeasuring = true
        defer { isMeasuring = false }
        manager.ensureLayout(for: container)
        let height = manager.usedRect(for: container).height
        guard abs(height - lastMeasuredHeight) > 0.5 else { return }
        lastMeasuredHeight = height
        onMeasuredHeight(height)
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

/// A one-line-by-default field that wraps and grows with what is typed, up to
/// `maxLines`, and scrolls after that.
///
/// `TextField` was the wrong shape for a task title: most titles here are a
/// whole sentence, and a single-line field showed one window of it at a time
/// with the rest scrolled off sideways — you had to arrow through the text to
/// find the word you meant to fix. Newlines stay out (see `allowsNewlines`), so
/// this is still one line of content, just one that is fully visible.
struct ATMGrowingTextField: View {
    @Binding private var text: String
    private let placeholder: String
    private let font: NSFont
    private let autoFocus: Bool
    private let minLines: Int
    private let maxLines: Int
    private let textInset: NSSize
    private let onSubmit: (() -> Void)?

    /// Laid-out text height as last reported by the text view; 0 until the first
    /// layout, which the `minLines` floor covers.
    @State private var contentHeight: CGFloat = 0

    init(
        text: Binding<String>,
        placeholder: String = "",
        font: NSFont = ATMFont.nsFont(.body),
        autoFocus: Bool = false,
        minLines: Int = 1,
        maxLines: Int = 6,
        textInset: NSSize = NSSize(width: 6, height: 6),
        onSubmit: (() -> Void)? = nil
    ) {
        _text = text
        self.placeholder = placeholder
        self.font = font
        self.autoFocus = autoFocus
        self.minLines = minLines
        self.maxLines = maxLines
        self.textInset = textInset
        self.onSubmit = onSubmit
    }

    var body: some View {
        ATMComposerTextView(
            text: $text,
            placeholder: placeholder,
            font: font,
            autoFocus: autoFocus,
            allowsNewlines: false,
            textInset: textInset,
            onMeasuredHeight: { measured in
                // The report can arrive from the text view's own layout pass, and
                // assigning SwiftUI state there is a re-entrant update.
                DispatchQueue.main.async { contentHeight = measured }
            },
            onSubmit: onSubmit
        )
        .frame(
            height: ATMComposerHeight.clamped(
                contentHeight: contentHeight,
                lineHeight: NSLayoutManager().defaultLineHeight(for: font),
                minLines: minLines,
                maxLines: maxLines,
                verticalInset: textInset.height
            )
        )
    }
}
