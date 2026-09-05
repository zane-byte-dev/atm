import AppKit
import ApplicationServices
import Carbon.HIToolbox
import Foundation

/// Puts transcribed text where the cursor was when someone started talking.
///
/// Via the pasteboard and a synthesized ⌘V rather than the Accessibility API's
/// `AXUIElementSetAttributeValue`: setting a text field's value directly replaces
/// what is already in it, works in roughly half of real apps, and does nothing at all
/// in a terminal or an Electron window. A paste goes wherever typing would go.
///
/// The pasteboard is borrowed, not taken — its previous contents are restored
/// afterwards, because dictating a sentence should not cost someone the thing they
/// copied a minute ago.
@MainActor
enum VoxCaretTextInjector {
    /// What happened, rather than success-or-throw: "the text is on the clipboard but
    /// we could not paste it" is neither, and it is the outcome the overlay most needs
    /// to be able to explain.
    enum Outcome: Equatable {
        /// Pasted into the target application.
        case injected
        /// Accessibility permission is missing, so the text was left on the clipboard
        /// for the person to paste themselves.
        case copiedToPasteboardOnly
    }

    enum Failure: LocalizedError {
        case targetApplicationUnavailable
        case pasteboardWriteFailed
        case livePreviewUnavailable

        var errorDescription: String? {
            switch self {
            case .targetApplicationUnavailable:
                return "开始录音时所在的应用已经关掉了。"
            case .pasteboardWriteFailed:
                return "写不进系统剪贴板。"
            case .livePreviewUnavailable:
                return "实时预览的位置发生了变化，已停止继续写入。"
            }
        }
    }

    struct LiveReplacementPlan: Equatable {
        let selectionLength: Int
        let replacement: String

        init(previous: String, replacement: String) {
            selectionLength = previous.count
            self.replacement = replacement
        }
    }

    /// Writes `text` into `application`, or falls back to leaving it on the clipboard.
    ///
    /// Never prompts for Accessibility permission: `AXIsProcessTrusted` is checked
    /// without the prompt option because the prompt is a modal dialog that would steal
    /// focus from the very app being pasted into. The settings screen is where
    /// permission gets requested, at a moment when interrupting is fine.
    static func inject(_ text: String, into application: NSRunningApplication?, isCurrent: @MainActor () -> Bool = { !Task.isCancelled }) async throws -> Outcome {
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        let pasteboard = NSPasteboard.general
        let snapshot = VoxCaretPasteboardSnapshot(pasteboard: pasteboard)
        pasteboard.clearContents()
        guard pasteboard.setString(text, forType: .string) else {
            snapshot.restore(to: pasteboard)
            throw Failure.pasteboardWriteFailed
        }
        let injectedChangeCount = pasteboard.changeCount

        guard AXIsProcessTrusted() else {
            // Deliberately leaves the text on the clipboard and does not restore the
            // snapshot: without the paste, the clipboard is the only copy of what was
            // just said, and losing it would lose the recording.
            VoxCaretLog.failure("voice_input_paste_untrusted", error: "AXIsProcessTrusted == false")
            return .copiedToPasteboardOnly
        }
        guard let application, !application.isTerminated else {
            snapshot.restore(to: pasteboard)
            throw Failure.targetApplicationUnavailable
        }

        // The overlay is a non-activating panel, so the target app usually never lost
        // focus — but a recording that outlives a Space switch or a click elsewhere
        // would otherwise paste into the wrong window.
        do {
            try await VoxCaretInjectionGate.perform(
                activate: { application.activate(options: [.activateAllWindows]) },
                waitUntilReady: { try await Task.sleep(for: .milliseconds(140)) },
                isCurrent: isCurrent,
                isTargetFocused: { NSWorkspace.shared.frontmostApplication?.processIdentifier == application.processIdentifier },
                paste: { await postPasteKeystroke() }
            )
        } catch {
            if pasteboard.changeCount == injectedChangeCount { snapshot.restore(to: pasteboard) }
            throw error
        }
        // Once a keypress was posted, let the target consume the borrowed
        // clipboard even if the recording task is cancelled during the hold.
        await VoxCaretPasteKeySequence.pause(milliseconds: 320)

        // Only restore if nothing else wrote in the meantime: an app that copies
        // something as part of handling the paste owns the clipboard now, and putting
        // the old contents back would undo its work.
        if pasteboard.changeCount == injectedChangeCount {
            snapshot.restore(to: pasteboard)
        }
        return .injected
    }

    /// Asks for Accessibility permission, showing macOS's own prompt. Called from
    /// settings only — see `inject`.
    static func requestAccessibilityPermission() {
        _ = AXIsProcessTrustedWithOptions(
            [kAXTrustedCheckOptionPrompt.takeUnretainedValue(): true] as CFDictionary
        )
    }

    /// Replaces the text most recently previewed at the target insertion point.
    /// Unlike the final injector this never activates an app and never leaves partial
    /// text on the clipboard: losing focus disables streaming and the final transcript
    /// falls back to the normal safe path.
    static func replaceLivePreview(
        _ previousText: String,
        with replacement: String,
        in application: NSRunningApplication?,
        isCurrent: @MainActor () -> Bool = { !Task.isCancelled }
    ) async throws -> Outcome {
        guard previousText != replacement else { return .injected }
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        guard AXIsProcessTrusted() else { return .copiedToPasteboardOnly }
        guard let application,
              !application.isTerminated,
              NSWorkspace.shared.frontmostApplication?.processIdentifier == application.processIdentifier
        else { throw Failure.targetApplicationUnavailable }

        let plan = LiveReplacementPlan(previous: previousText, replacement: replacement)
        let pasteboard = NSPasteboard.general
        let snapshot = VoxCaretPasteboardSnapshot(pasteboard: pasteboard)
        var injectedChangeCount: Int?

        if !plan.replacement.isEmpty {
            pasteboard.clearContents()
            guard pasteboard.setString(plan.replacement, forType: .string) else {
                snapshot.restore(to: pasteboard)
                throw Failure.pasteboardWriteFailed
            }
            injectedChangeCount = pasteboard.changeCount
        }

        // Selection and replacement form one indivisible edit. Re-check immediately
        // before it begins; once characters are selected we must always complete the
        // paste/delete so the target is not left with a live selection.
        guard isCurrent(), !Task.isCancelled,
              NSWorkspace.shared.frontmostApplication?.processIdentifier == application.processIdentifier
        else {
            if let injectedChangeCount, pasteboard.changeCount == injectedChangeCount {
                snapshot.restore(to: pasteboard)
            }
            throw CancellationError()
        }

        await postSelectTrailingCharacters(plan.selectionLength)
        if plan.replacement.isEmpty {
            await postDeleteSelection()
        } else {
            await postPasteKeystroke()
        }
        await VoxCaretPasteKeySequence.pause(milliseconds: 180)

        if let injectedChangeCount, pasteboard.changeCount == injectedChangeCount {
            snapshot.restore(to: pasteboard)
        }
        return .injected
    }

    /// Synthesizes ⌘V at the HID level, which is the layer where an app cannot tell it
    /// from a real keypress.
    private static func postPasteKeystroke() async {
        let source = CGEventSource(stateID: .hidSystemState)
        let vKeyCode = CGKeyCode(0x09) // kVK_ANSI_V
        guard let keyDown = CGEvent(keyboardEventSource: source, virtualKey: vKeyCode, keyDown: true),
              let keyUp = CGEvent(keyboardEventSource: source, virtualKey: vKeyCode, keyDown: false) else {
            return
        }
        keyDown.flags = .maskCommand
        keyUp.flags = .maskCommand
        await VoxCaretPasteKeySequence.perform(
            keyDown: { keyDown.post(tap: .cghidEventTap) },
            keyUp: { keyUp.post(tap: .cghidEventTap) }
        )
    }

    private static func postSelectTrailingCharacters(_ count: Int) async {
        guard count > 0 else { return }
        let source = CGEventSource(stateID: .hidSystemState)
        guard let keyDown = CGEvent(
            keyboardEventSource: source,
            virtualKey: CGKeyCode(kVK_LeftArrow),
            keyDown: true
        ), let keyUp = CGEvent(
            keyboardEventSource: source,
            virtualKey: CGKeyCode(kVK_LeftArrow),
            keyDown: false
        ) else { return }
        keyDown.flags = .maskShift
        keyUp.flags = .maskShift
        for _ in 0..<count {
            keyDown.post(tap: .cghidEventTap)
            keyUp.post(tap: .cghidEventTap)
        }
        await VoxCaretPasteKeySequence.pause(milliseconds: 35)
    }

    private static func postDeleteSelection() async {
        let source = CGEventSource(stateID: .hidSystemState)
        guard let keyDown = CGEvent(
            keyboardEventSource: source,
            virtualKey: CGKeyCode(kVK_Delete),
            keyDown: true
        ), let keyUp = CGEvent(
            keyboardEventSource: source,
            virtualKey: CGKeyCode(kVK_Delete),
            keyDown: false
        ) else { return }
        await VoxCaretPasteKeySequence.perform(
            keyDown: { keyDown.post(tap: .cghidEventTap) },
            keyUp: { keyUp.post(tap: .cghidEventTap) }
        )
    }
}

@MainActor
enum VoxCaretInjectionGate {
    // Testable sequencing around the activation suspension point. A cancellation
    // or focus switch during that gap must never dispatch a keystroke.
    static func perform(activate: () -> Void, waitUntilReady: () async throws -> Void,
                        isCurrent: () -> Bool, isTargetFocused: () -> Bool,
                        paste: () async -> Void) async throws {
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        activate()
        try await waitUntilReady()
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        guard isTargetFocused() else { throw VoxCaretTextInjector.Failure.targetApplicationUnavailable }
        await paste()
    }
}

@MainActor
enum VoxCaretPasteKeySequence {
    static func perform(keyDown: @MainActor () -> Void, keyUp: @MainActor () -> Void) async {
        keyDown()
        defer { keyUp() }
        // The delay must yield the main actor, and key-up must still follow a
        // complete hold if dictation is cancelled after key-down. A continuation
        // deliberately makes this tiny key sequence indivisible by cancellation.
        await pause(milliseconds: 50)
    }

    static func pause(milliseconds: Int) async {
        await withCheckedContinuation { continuation in
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(milliseconds)) {
                continuation.resume()
            }
        }
    }
}

/// A copy of everything on the pasteboard, deep enough to put back.
///
/// `pasteboardItems` hands out references the pasteboard invalidates on the next
/// `clearContents()`, so each item's data is copied out per type rather than held.
private struct VoxCaretPasteboardSnapshot {
    private let items: [NSPasteboardItem]

    init(pasteboard: NSPasteboard) {
        items = (pasteboard.pasteboardItems ?? []).map { source in
            let copy = NSPasteboardItem()
            for type in source.types {
                if let data = source.data(forType: type) {
                    copy.setData(data, forType: type)
                }
            }
            return copy
        }
    }

    func restore(to pasteboard: NSPasteboard) {
        pasteboard.clearContents()
        guard !items.isEmpty else { return }
        pasteboard.writeObjects(items)
    }
}
