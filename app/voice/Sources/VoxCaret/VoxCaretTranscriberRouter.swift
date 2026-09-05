import Foundation

/// Picks the engine for one recording and then gets out of the way.
///
/// The choice is made in `requestAuthorization` — the first step of every recording —
/// and held for the rest of it, so a preference edited mid-sentence cannot swap the
/// engine out from under the audio that is already being captured.
///
/// SenseVoice is the default even before its model exists, and this is where that
/// works: without the model the router quietly uses Apple Speech. Quiet, not hidden —
/// the overlay names the engine that actually ran, and the settings screen shows the
/// model as not downloaded. Refusing to record would be the worse answer for someone
/// who just wants to dictate a sentence.
@MainActor
final class VoxCaretTranscriberRouter: VoxCaretSpeechTranscribing {
    private let apple: VoxCaretSpeechTranscribing
    private let senseVoice: VoxCaretSpeechTranscribing
    private var active: VoxCaretSpeechTranscribing?

    init(
        apple: VoxCaretSpeechTranscribing? = nil,
        senseVoice: VoxCaretSpeechTranscribing? = nil
    ) {
        self.apple = apple ?? VoxCaretAppleSpeechTranscriber()
        self.senseVoice = senseVoice ?? VoxCaretSenseVoiceTranscriber()
    }

    /// The engine actually in use once a recording has started, and the preferred one
    /// before that — never a promise the fallback might break.
    var displayName: String {
        active?.displayName ?? resolved().displayName
    }

    var inputLevel: Float {
        active?.inputLevel ?? 0
    }

    /// Whether partial text will arrive, so the overlay can decide between showing a
    /// transcript and showing a level meter instead of inferring it from an empty
    /// string.
    var providesPartialResults: Bool {
        (active ?? resolved()).providesPartialResults
    }

    func requestAuthorization() async throws {
        let selected = resolved()
        active = selected
        try await selected.requestAuthorization()
    }

    func prepare() async {
        await (active ?? resolved()).prepare()
    }

    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws {
        guard let active else {
            throw VoxCaretSpeechTranscriberError.recognizerUnavailable
        }
        try active.start(onPartialResult: onPartialResult, onFailure: onFailure)
    }

    func finish() async throws -> String {
        guard let active else { return "" }
        defer { self.active = nil }
        return try await active.finish()
    }

    func cancel() {
        active?.cancel()
        active = nil
    }

    private func resolved() -> VoxCaretSpeechTranscribing {
        guard VoxCaretTranscriberRouting.shouldUseSenseVoice(
            preferredEngine: VoxCaretInputPreferences.engine,
            liveInsertionEnabled: VoxCaretInputPreferences.liveInsertionEnabled,
            senseVoiceModelReady: VoxCaretSenseVoiceModelManager.shared.isModelReady
        ) else {
            return apple
        }
        return senseVoice
    }
}

enum VoxCaretTranscriberRouting {
    static func shouldUseSenseVoice(
        preferredEngine: VoxCaretRecognitionEngine,
        liveInsertionEnabled: Bool,
        senseVoiceModelReady: Bool
    ) -> Bool {
        !liveInsertionEnabled && preferredEngine == .senseVoice && senseVoiceModelReady
    }
}
