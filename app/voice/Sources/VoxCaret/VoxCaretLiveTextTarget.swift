import AppKit
import ApplicationServices
import Foundation

/// Tracks the exact field, text and UTF-16 selection captured at key-down.
/// Process identity alone cannot distinguish two inputs in the same application.
@MainActor
final class VoxCaretLiveTextTarget {
    struct Snapshot: Equatable {
        let text: String
        let selection: NSRange

        var hasValidSelection: Bool {
            selection.location != NSNotFound && selection.location >= 0 && selection.length >= 0
                && selection.location <= text.utf16.count
                && selection.length <= text.utf16.count - selection.location
        }
    }

    private let read: () -> Snapshot?
    private let select: (NSRange) -> Bool
    private let anchor: Int
    let originalSelection: String
    private var expected: Snapshot
    private var renderedText = ""
    private var hasInserted = false
    private var invalidated = false

    init?(snapshot: Snapshot, read: @escaping () -> Snapshot?, select: @escaping (NSRange) -> Bool) {
        guard snapshot.hasValidSelection else { return nil }
        expected = snapshot
        anchor = snapshot.selection.location
        originalSelection = (snapshot.text as NSString).substring(with: snapshot.selection)
        self.read = read
        self.select = select
    }

    static func capture(application: NSRunningApplication?) -> VoxCaretLiveTextTarget? {
        guard AXIsProcessTrusted(), let application, !application.isTerminated else { return nil }
        let appElement = AXUIElementCreateApplication(application.processIdentifier)
        guard let element = focusedElement(in: appElement), let snapshot = snapshot(of: element) else { return nil }
        var settable: DarwinBoolean = false
        guard AXUIElementIsAttributeSettable(element, kAXSelectedTextRangeAttribute as CFString, &settable) == .success,
              settable.boolValue else { return nil }
        return VoxCaretLiveTextTarget(snapshot: snapshot, read: {
            guard !application.isTerminated,
                  NSWorkspace.shared.frontmostApplication?.processIdentifier == application.processIdentifier,
                  let focused = focusedElement(in: appElement), CFEqual(focused, element)
            else { return nil }
            return Self.snapshot(of: element)
        }, select: { selection in
            var range = CFRange(location: selection.location, length: selection.length)
            guard let value = AXValueCreate(.cfRange, &range) else { return false }
            return AXUIElementSetAttributeValue(element, kAXSelectedTextRangeAttribute as CFString, value) == .success
        })
    }

    /// `commit` posts the paste/delete immediately, then waits for the target to
    /// consume it. A failed observation permanently invalidates this recording's
    /// anchor: returning to the same coordinates later is not proof of ownership.
    func replace(previous: String, with replacement: String, isCurrent: () -> Bool,
                 commit: (String) async throws -> Void) async throws {
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        guard !invalidated, renderedText == previous, read() == expected else {
            invalidated = true
            throw VoxCaretTextInjector.Failure.livePreviewUnavailable
        }
        if hasInserted, previous == replacement { return }

        let range = hasInserted ? NSRange(location: anchor, length: previous.utf16.count) : expected.selection
        let before = expected
        let afterText = (before.text as NSString).replacingCharacters(in: range, with: replacement)
        let after = Snapshot(text: afterText, selection: NSRange(location: anchor + replacement.utf16.count, length: 0))
        guard select(range), read() == Snapshot(text: before.text, selection: range), isCurrent(), !Task.isCancelled else {
            invalidated = true
            throw VoxCaretTextInjector.Failure.livePreviewUnavailable
        }
        do {
            try await commit(replacement)
            guard read() == after else { throw VoxCaretTextInjector.Failure.livePreviewUnavailable }
            expected = after
            renderedText = replacement
            hasInserted = true
        } catch {
            invalidated = true
            throw error
        }
    }

    private static func focusedElement(in application: AXUIElement) -> AXUIElement? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(application, kAXFocusedUIElementAttribute as CFString, &value) == .success,
              let value, CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        return (value as! AXUIElement)
    }

    private static func snapshot(of element: AXUIElement) -> Snapshot? {
        var textValue: CFTypeRef?
        var rangeValue: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXValueAttribute as CFString, &textValue) == .success,
              let text = textValue as? String,
              AXUIElementCopyAttributeValue(element, kAXSelectedTextRangeAttribute as CFString, &rangeValue) == .success,
              let rangeValue, CFGetTypeID(rangeValue) == AXValueGetTypeID() else { return nil }
        let value = rangeValue as! AXValue
        var range = CFRange()
        guard AXValueGetType(value) == .cfRange, AXValueGetValue(value, .cfRange, &range) else { return nil }
        let result = Snapshot(text: text, selection: NSRange(location: range.location, length: range.length))
        return result.hasValidSelection ? result : nil
    }
}
