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
    private let copyFinalText: @MainActor (String) throws -> VoxCaretTextInjector.Outcome

    private(set) var renderedText = ""
    private(set) var latestTranscript = ""
    private var pendingText: String?
    private var worker: Task<Void, Never>?
    private var finalReplacement: Task<VoxCaretTextInjector.Outcome, Error>?
    private var unavailable = false

    init(
        target: VoxCaretLiveTextTarget?,
        isCurrent: @escaping @MainActor () -> Bool
    ) {
        replace = { previous, replacement in
            guard let target else { throw VoxCaretTextInjector.Failure.livePreviewUnavailable }
            return try await VoxCaretTextInjector.replaceLivePreview(
                previous,
                with: replacement,
                target: target,
                isCurrent: isCurrent
            )
        }
        rollbackReplace = { previous, _ in
            guard let target else { throw VoxCaretTextInjector.Failure.livePreviewUnavailable }
            return try await VoxCaretTextInjector.replaceLivePreview(
                previous,
                with: target.originalSelection,
                target: target,
                isCurrent: { true }
            )
        }
        debounce = .milliseconds(140)
        copyFinalText = VoxCaretTextInjector.copyForChangedTarget
    }

    init(
        debounce: Duration,
        replace: @escaping Replacer,
        rollbackReplace: Replacer? = nil,
        copyFinalText: @escaping @MainActor (String) throws -> VoxCaretTextInjector.Outcome = VoxCaretTextInjector.copyForChangedTarget
    ) {
        self.replace = replace
        self.rollbackReplace = rollbackReplace ?? replace
        self.debounce = debounce
        self.copyFinalText = copyFinalText
    }

    func submit(_ text: String) {
        latestTranscript = text
        guard !unavailable, !text.isEmpty, text != renderedText else { return }
        pendingText = text
        startWorkerIfNeeded()
    }

    /// Reconciles the currently visible preview with the final cleaned transcript.
    /// Even when no partial reached the field, the first insertion uses the anchor
    /// captured at key-down. An unverifiable target falls back to an explicit copy.
    func finish(with finalText: String) async throws -> VoxCaretTextInjector.Outcome {
        pendingText = nil
        let activeWorker = worker
        await activeWorker?.value
        try Task.checkCancellation()
        latestTranscript = finalText
        let replacement = Task { @MainActor in
            try Task.checkCancellation()
            if !unavailable {
                do {
                    let outcome = try await replace(renderedText, finalText)
                    if outcome == .injected {
                        // Record a confirmed paste even if Esc arrived during its
                        // key hold. Rollback waits for this task before removing it.
                        renderedText = finalText
                        return outcome
                    }
                } catch is CancellationError {
                    throw CancellationError()
                } catch {
                    unavailable = true
                }
            }
            try Task.checkCancellation()
            return try copyFinalText(finalText)
        }
        finalReplacement = replacement
        defer { finalReplacement = nil }
        return try await withTaskCancellationHandler {
            try await replacement.value
        } onCancel: {
            replacement.cancel()
        }
    }

    /// Waits for dispatched edits, then restores only the verified preview span.
    /// False means the location changed; callers retain the text for manual recovery.
    @discardableResult
    func rollback() async -> Bool {
        pendingText = nil
        let activeWorker = worker
        await activeWorker?.value
        let activeFinal = finalReplacement
        _ = try? await activeFinal?.value
        guard !renderedText.isEmpty else { return !unavailable }

        do {
            let outcome = try await rollbackReplace(renderedText, "")
            if outcome == .injected { renderedText = ""; return true }
        } catch {
            VoxCaretLog.failure("voice_live_preview_rollback_failed", error: error.localizedDescription)
        }
        return false
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
                case .copiedToPasteboardOnly, .copiedForChangedTarget:
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
