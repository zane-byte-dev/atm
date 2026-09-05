import AppKit
import Foundation

/// Coalesces the rapidly changing partial results from Apple Speech into a small
/// number of edits in the target field. Each edit replaces the preview written by
/// the preceding edit, so recognition corrections do not leave duplicated words.
@MainActor
final class VoxCaretLiveInsertionSession {
    typealias Replacer = @MainActor (String, String) async throws -> VoxCaretTextInjector.Outcome

    private let replace: Replacer
    private let rollbackReplace: Replacer
    private let debounce: Duration

    private(set) var renderedText = ""
    private var pendingText: String?
    private var worker: Task<Void, Never>?
    private var unavailable = false

    var hasRenderedText: Bool { !renderedText.isEmpty }

    init(
        application: NSRunningApplication?,
        isCurrent: @escaping @MainActor () -> Bool
    ) {
        replace = { previous, replacement in
            try await VoxCaretTextInjector.replaceLivePreview(
                previous,
                with: replacement,
                in: application,
                isCurrent: isCurrent
            )
        }
        rollbackReplace = { previous, replacement in
            try await VoxCaretTextInjector.replaceLivePreview(
                previous,
                with: replacement,
                in: application,
                isCurrent: { true }
            )
        }
        debounce = .milliseconds(140)
    }

    init(
        debounce: Duration,
        replace: @escaping Replacer,
        rollbackReplace: Replacer? = nil
    ) {
        self.replace = replace
        self.rollbackReplace = rollbackReplace ?? replace
        self.debounce = debounce
    }

    func submit(_ text: String) {
        guard !unavailable, !text.isEmpty, text != renderedText else { return }
        pendingText = text
        startWorkerIfNeeded()
    }

    /// Reconciles the currently visible preview with the final cleaned transcript.
    /// Returns nil when no preview ever reached the target, telling the coordinator
    /// to use its ordinary one-shot injection path.
    func finish(with finalText: String) async throws -> VoxCaretTextInjector.Outcome? {
        pendingText = nil
        let activeWorker = worker
        await activeWorker?.value

        guard !renderedText.isEmpty else { return nil }
        guard renderedText != finalText else { return .injected }

        let outcome = try await replace(renderedText, finalText)
        guard outcome == .injected else {
            throw VoxCaretTextInjector.Failure.livePreviewUnavailable
        }
        renderedText = finalText
        return .injected
    }

    /// Removes anything already previewed. Used for Esc and for recognizer failures,
    /// preserving the promise that cancellation leaves no dictated text behind.
    func rollback() async {
        pendingText = nil
        let activeWorker = worker
        await activeWorker?.value
        guard !renderedText.isEmpty else { return }

        do {
            let outcome = try await rollbackReplace(renderedText, "")
            if outcome == .injected { renderedText = "" }
        } catch {
            VoxCaretLog.failure("voice_live_preview_rollback_failed", error: error.localizedDescription)
        }
    }

    private func startWorkerIfNeeded() {
        guard worker == nil else { return }
        worker = Task { @MainActor [weak self] in
            guard let self else { return }
            // Speech can publish several hypotheses in one breath. A short debounce
            // makes the target feel live without flashing every intermediate guess.
            try? await Task.sleep(for: self.debounce)
            await self.drainPendingText()
        }
    }

    private func drainPendingText() async {
        while let next = pendingText, !unavailable {
            pendingText = nil
            do {
                let outcome = try await replace(renderedText, next)
                switch outcome {
                case .injected:
                    renderedText = next
                case .copiedToPasteboardOnly:
                    // Live previews never take over the clipboard as a fallback.
                    // The final one-shot injection still can, so no speech is lost.
                    unavailable = true
                    pendingText = nil
                }
            } catch {
                unavailable = true
                pendingText = nil
                VoxCaretLog.failure("voice_live_preview_unavailable", error: error.localizedDescription)
            }

            if pendingText != nil {
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
        worker = nil
        if pendingText != nil, !unavailable { startWorkerIfNeeded() }
    }
}
