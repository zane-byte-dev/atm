import AppKit
import ApplicationServices
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
enum ATMTextInjector {
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

        var errorDescription: String? {
            switch self {
            case .targetApplicationUnavailable:
                return "开始录音时所在的应用已经关掉了。"
            case .pasteboardWriteFailed:
                return "写不进系统剪贴板。"
            }
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
        let snapshot = ATMPasteboardSnapshot(pasteboard: pasteboard)
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
            ATMLog.failure("voice_input_paste_untrusted", error: "AXIsProcessTrusted == false")
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
            try await ATMInjectionGate.perform(
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
        await ATMPasteKeySequence.pause(milliseconds: 320)

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
        await ATMPasteKeySequence.perform(
            keyDown: { keyDown.post(tap: .cghidEventTap) },
            keyUp: { keyUp.post(tap: .cghidEventTap) }
        )
    }
}

@MainActor
enum ATMInjectionGate {
    // Testable sequencing around the activation suspension point. A cancellation
    // or focus switch during that gap must never dispatch a keystroke.
    static func perform(activate: () -> Void, waitUntilReady: () async throws -> Void,
                        isCurrent: () -> Bool, isTargetFocused: () -> Bool,
                        paste: () async -> Void) async throws {
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        activate()
        try await waitUntilReady()
        guard isCurrent(), !Task.isCancelled else { throw CancellationError() }
        guard isTargetFocused() else { throw ATMTextInjector.Failure.targetApplicationUnavailable }
        await paste()
    }
}

@MainActor
enum ATMPasteKeySequence {
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
private struct ATMPasteboardSnapshot {
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
