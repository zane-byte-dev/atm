import AppKit
import Foundation

enum VoxCaretInputState: Equatable {
    case idle
    case requestingPermission
    /// Carries the partial transcript, which is empty for engines that produce none.
    case recording(String)
    case processing
    case cancelling
    case success(String)
    case failed(String)

    var isRecording: Bool {
        if case .recording = self { return true }
        return false
    }

    var isActive: Bool {
        switch self {
        case .requestingPermission, .recording, .processing, .cancelling: return true
        case .idle, .success, .failed: return false
        }
    }
}

/// Runs one dictation from key-down to pasted text.
///
/// The order matters more than any single step: the frontmost application is captured
/// before the microphone opens, because by the time there is text to paste the answer
/// to "where was the cursor" has to already be known. Everything after that is a state
/// machine whose only job is to never leave the microphone open or the overlay stuck.
///
/// `generation` guards every async step. A recording can be cancelled, superseded, or
/// timed out while a permission prompt or a model load is still in flight, and a stale
/// continuation resuming into a live recording is the failure mode that would be
/// hardest to see: the audio would be right and the text would go somewhere else.
@MainActor
final class VoxCaretInputCoordinator: ObservableObject {
    static let shared = VoxCaretInputCoordinator()

    @Published private(set) var state: VoxCaretInputState = .idle
    /// Kept after the fact so settings can offer it for copying — the only way back to
    /// a transcript whose paste failed.
    @Published private(set) var lastTranscript = ""
    @Published private(set) var lastOutcome: VoxCaretTextInjector.Outcome?
    @Published private(set) var activeEngineName = ""
    @Published private(set) var showsLevelMeter = false
    @Published private(set) var inputLevel: Float = 0

    /// A router is scoped to one recording. A cancelled `finish()` may still be
    /// unwinding while the next recording starts; sharing the same router (and its
    /// Apple/SenseVoice instances) lets that stale cleanup cancel the new session.
    private let transcriberSessions: VoxCaretTranscriberSessions
    private var targetApplication: NSRunningApplication?
    private var operationTask: Task<Void, Never>?
    private var levelTask: Task<Void, Never>?
    private var watchdogTask: Task<Void, Never>?
    private var liveInsertionSession: VoxCaretLiveInsertionSession?
    /// A release that arrived before `start` finished. Holding the key for less time
    /// than the permission check takes is entirely normal, and dropping the release
    /// would leave the microphone open.
    private var releaseRequested = false
    private var generation = 0

    init(
        makeTranscriber: @escaping @MainActor () -> VoxCaretSpeechTranscribing = {
            VoxCaretTranscriberRouter()
        }
    ) {
        transcriberSessions = VoxCaretTranscriberSessions(makeTranscriber: makeTranscriber)
    }

    // MARK: - Hot key entry points

    func hotKeyPressed() {
        guard !state.isActive else { return }
        // Checked before anything else, because the failure it prevents does not look
        // like a failure: with no `.app` bundle, TCC never shows the microphone dialog
        // and never calls back, so dictation would sit in 正在打开麦克风… forever.
        // There is a deadline behind this too, but saying so up front is the difference
        // between an answer and a 45-second wait.
        guard VoxCaretAppBundle.isBundled else {
            VoxCaretLog.failure(
                "voice_input_requires_app_bundle",
                fields: ["bundle": Bundle.main.bundleURL.lastPathComponent]
            )
            generation += 1
            transition(to: .failed(VoxCaretSpeechTranscriberError.requiresAppBundle.localizedDescription))
            scheduleReset(after: .seconds(8), generation: generation)
            return
        }
        beginRecording()
    }

    func hotKeyReleased() {
        releaseRequested = true
        guard state.isRecording else { return }
        finishRecordingSoon()
    }

    /// ⎋ during a recording: stop, keep nothing, paste nothing.
    func cancel() {
        generation += 1
        let currentGeneration = generation
        let liveSession = liveInsertionSession
        liveInsertionSession = nil
        operationTask?.cancel()
        operationTask = nil
        teardown()
        transcriberSessions.cancelCurrent()
        guard let liveSession, liveSession.hasRenderedText else {
            transition(to: .idle)
            return
        }
        transition(to: .cancelling)
        operationTask = Task { [weak self] in
            await liveSession.rollback()
            guard let self, currentGeneration == self.generation else { return }
            self.transition(to: .idle)
        }
    }

    // MARK: - Recording

    private func beginRecording() {
        generation += 1
        let currentGeneration = generation
        let transcriber = transcriberSessions.begin()
        releaseRequested = false
        lastOutcome = nil
        liveInsertionSession = nil
        // Before the microphone opens: this is the app the text belongs in.
        targetApplication = NSWorkspace.shared.frontmostApplication
        VoxCaretGlobalHotKeyManager.shared.registerTransient(
            .cancelVoice,
            hotKey: VoxCaretInputPreferences.cancelHotKey
        )
        transition(to: .requestingPermission)

        operationTask?.cancel()
        operationTask = Task { [weak self] in
            guard let self else { return }
            do {
                // The only suspension point before the microphone opens, and the only
                // place the recording can be superseded from under us. `cancel()` has
                // already stopped the engine by the time we get back here, so bailing
                // out is all that is left to do.
                try await transcriber.requestAuthorization()
                guard currentGeneration == generation else { return }

                activeEngineName = transcriber.displayName
                showsLevelMeter = !transcriber.providesPartialResults
                // Everything from here to the end of the task is synchronous, so the
                // recording is fully set up before anything else can observe it.
                try transcriber.start(
                    onPartialResult: { [weak self] text in
                        self?.handlePartialResult(text, generation: currentGeneration)
                    },
                    onFailure: { [weak self] error in
                        self?.fail(error.localizedDescription, generation: currentGeneration)
                    }
                )
                if transcriber.providesPartialResults, VoxCaretInputPreferences.liveInsertionEnabled {
                    liveInsertionSession = VoxCaretLiveInsertionSession(
                        application: targetApplication,
                        isCurrent: { currentGeneration == self.generation && !Task.isCancelled }
                    )
                }
                transition(to: .recording(""))
                startLevelUpdates(
                    transcriber: transcriber,
                    generation: currentGeneration
                )
                startWatchdog(generation: currentGeneration)
                // Loading a local model overlaps with the person speaking rather than
                // delaying the result — the whole reason it is not done at launch.
                Task { await transcriber.prepare() }

                if releaseRequested {
                    // They let go faster than the microphone opened.
                    finishRecordingSoon()
                }
            } catch is CancellationError {
                return
            } catch {
                fail(error.localizedDescription, generation: currentGeneration)
            }
        }
    }

    private func finishRecordingSoon() {
        guard let transcriber = transcriberSessions.active else { return }
        let currentGeneration = generation
        operationTask?.cancel()
        operationTask = Task { [weak self] in
            await self?.finishRecording(
                with: transcriber,
                generation: currentGeneration
            )
        }
    }

    private func finishRecording(
        with transcriber: VoxCaretSpeechTranscribing,
        generation currentGeneration: Int
    ) async {
        guard currentGeneration == generation, state.isRecording else { return }
        stopLevelUpdates()
        stopWatchdog()
        transition(to: .processing)

        let raw: String
        do {
            raw = try await transcriber.finish()
        } catch {
            fail(error.localizedDescription, generation: currentGeneration)
            return
        }
        guard currentGeneration == generation else { return }

        let transcript = VoxCaretTextCleanup.process(raw)
        guard !transcript.isEmpty else {
            fail("没有识别到内容，再说一次。", generation: currentGeneration)
            return
        }

        do {
            let liveOutcome = try await liveInsertionSession?.finish(with: transcript)
            let outcome: VoxCaretTextInjector.Outcome
            if let liveOutcome {
                outcome = liveOutcome
            } else {
                outcome = try await VoxCaretTextInjector.inject(transcript, into: targetApplication, isCurrent: {
                    currentGeneration == self.generation && !Task.isCancelled
                })
            }
            guard currentGeneration == generation else { return }
            lastTranscript = transcript
            lastOutcome = outcome
            liveInsertionSession = nil
            transcriberSessions.clearIfCurrent(transcriber)
            teardown()
            transition(to: .success(transcript))
            // Long enough to read a short sentence and confirm what landed, short
            // enough not to sit over someone's work.
            scheduleReset(after: .milliseconds(1200), generation: currentGeneration)
        } catch is CancellationError {
            return
        } catch {
            guard currentGeneration == generation else { return }
            // The transcript survived the recognizer but not the paste; keep it so it
            // can still be copied out of settings.
            lastTranscript = transcript
            fail(error.localizedDescription, generation: currentGeneration)
        }
    }

    // MARK: - Watchdogs

    /// Ends a recording whose release event never came.
    ///
    /// Carbon owns the press/release lifecycle. This watchdog is deliberately only a
    /// duration backstop: live insertion emits synthetic Shift and Command events, and
    /// sampling global modifier flags while those are in flight can falsely conclude
    /// that Option was released and cut a sentence off in the middle.
    private func startWatchdog(generation currentGeneration: Int) {
        stopWatchdog()
        let deadline = VoxCaretInputPreferences.maximumRecordingDuration
        watchdogTask = Task { [weak self] in
            let started = ContinuousClock.now
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(120))
                guard let self, !Task.isCancelled else { return }
                guard currentGeneration == self.generation, self.state.isRecording else { return }

                if ContinuousClock.now - started > deadline {
                    VoxCaretLog.failure(
                        "voice_input_recording_timeout",
                        fields: ["limit": "\(deadline)"]
                    )
                    self.hotKeyReleased()
                    return
                }
            }
        }
    }

    private func stopWatchdog() {
        watchdogTask?.cancel()
        watchdogTask = nil
    }

    /// Polls the engine's level instead of having the audio tap publish it: the tap runs
    /// on a real-time thread, and the meter only needs to be right at screen refresh
    /// rates.
    private func startLevelUpdates(
        transcriber: VoxCaretSpeechTranscribing,
        generation currentGeneration: Int
    ) {
        stopLevelUpdates()
        levelTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self, currentGeneration == self.generation, self.state.isRecording else { return }
                self.inputLevel = transcriber.inputLevel
                try? await Task.sleep(for: .milliseconds(50))
            }
        }
    }

    private func stopLevelUpdates() {
        levelTask?.cancel()
        levelTask = nil
        inputLevel = 0
    }

    // MARK: - State

    private func handlePartialResult(_ text: String, generation currentGeneration: Int) {
        guard currentGeneration == generation, state.isRecording else { return }
        transition(to: .recording(text))
        let preview = VoxCaretTextCleanup.process(
            text,
            replacements: VoxCaretInputPreferences.replacements,
            removeTrailingPeriod: false
        )
        liveInsertionSession?.submit(preview)
    }

    private func fail(_ message: String, generation currentGeneration: Int) {
        guard currentGeneration == generation else { return }
        let liveSession = liveInsertionSession
        liveInsertionSession = nil
        transcriberSessions.cancelCurrent()
        teardown()
        VoxCaretLog.failure("voice_input_failed", error: message)
        guard let liveSession, liveSession.hasRenderedText else {
            transition(to: .failed(message))
            scheduleReset(after: .seconds(5), generation: currentGeneration)
            return
        }
        transition(to: .cancelling)
        operationTask?.cancel()
        operationTask = Task { [weak self] in
            await liveSession.rollback()
            guard let self, currentGeneration == self.generation else { return }
            self.transition(to: .failed(message))
            self.scheduleReset(after: .seconds(5), generation: currentGeneration)
        }
    }

    /// Everything that must stop whether the recording succeeded or not. ⎋ has to be
    /// given back promptly — while it is registered, no other app can use it.
    private func teardown() {
        stopLevelUpdates()
        stopWatchdog()
        VoxCaretGlobalHotKeyManager.shared.unregisterTransient(.cancelVoice)
        releaseRequested = false
    }

    private func scheduleReset(after duration: Duration, generation currentGeneration: Int) {
        operationTask?.cancel()
        operationTask = Task { [weak self] in
            try? await Task.sleep(for: duration)
            // `try?` swallows the cancellation, so a cancelled sleep returns *early*
            // rather than never finishing. Without this check, replacing one reset with
            // another at the same generation would run the first one immediately and
            // yank the overlay away before anyone had read it.
            guard !Task.isCancelled else { return }
            guard let self, currentGeneration == self.generation else { return }
            self.transition(to: .idle)
        }
    }

    private func transition(to newState: VoxCaretInputState) {
        state = newState
        if case .idle = newState {
            VoxCaretOverlayController.shared.hide()
        } else {
            VoxCaretOverlayController.shared.show(coordinator: self)
        }
    }
}

/// Owns the one transcriber associated with each recording generation.
///
/// A cancelled engine may finish unwinding after the next recording has begun. The
/// store therefore creates a fresh object for every recording and only lets matching
/// cleanup clear the current slot.
@MainActor
final class VoxCaretTranscriberSessions {
    private let makeTranscriber: @MainActor () -> VoxCaretSpeechTranscribing
    private(set) var active: VoxCaretSpeechTranscribing?

    init(makeTranscriber: @escaping @MainActor () -> VoxCaretSpeechTranscribing) {
        self.makeTranscriber = makeTranscriber
    }

    func begin() -> VoxCaretSpeechTranscribing {
        let transcriber = makeTranscriber()
        active = transcriber
        return transcriber
    }

    func cancelCurrent() {
        let transcriber = active
        active = nil
        transcriber?.cancel()
    }

    func clearIfCurrent(_ transcriber: VoxCaretSpeechTranscribing) {
        guard active === transcriber else { return }
        active = nil
    }
}
