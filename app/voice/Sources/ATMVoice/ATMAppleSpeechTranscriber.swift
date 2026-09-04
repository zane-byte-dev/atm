import AVFoundation
import Foundation
import Speech

/// Dictation through Apple's Speech framework.
///
/// The engine that always works: nothing to download, and it streams partial text
/// while someone is still talking, so the overlay can show the sentence forming.
/// The trade is that unless "仅使用本地识别" is on, macOS may send the audio to
/// Apple's servers, and its Chinese punctuation is weaker than SenseVoice's.
@MainActor
final class ATMAppleSpeechTranscriber: ATMSpeechTranscribing {
    let displayName = "Apple Speech"

    private(set) var inputLevel: Float = 0

    private var recognizer: SFSpeechRecognizer?
    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private var inputTapInstalled = false
    private var latestTranscript = ""
    private var isFinishing = false

    func requestAuthorization() async throws {
        // Already answered: no dialog is coming, so no deadline is needed either.
        switch SFSpeechRecognizer.authorizationStatus() {
        case .authorized:
            break
        case .notDetermined:
            let answer = await ATMAuthorizationRequest.run { completion in
                SFSpeechRecognizer.requestAuthorization { completion($0 == .authorized) }
            }
            guard let answer else { throw ATMSpeechTranscriberError.authorizationTimedOut }
            guard answer else { throw ATMSpeechTranscriberError.speechRecognitionDenied }
        default:
            throw ATMSpeechTranscriberError.speechRecognitionDenied
        }
        try await ATMMicrophoneAuthorization.request()
    }

    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws {
        cancel()
        let language = ATMVoiceInputPreferences.language
        guard let recognizer = SFSpeechRecognizer(locale: language.locale), recognizer.isAvailable else {
            throw ATMSpeechTranscriberError.recognizerUnavailable
        }
        self.recognizer = recognizer

        latestTranscript = ""
        isFinishing = false

        let engine = AVAudioEngine()
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        request.contextualStrings = ATMVoiceInputPreferences.contextualTerms
        if ATMVoiceInputPreferences.onDeviceOnly {
            // Checked rather than set-and-hope: with the flag on and the language
            // unsupported, the recognizer fails partway through instead of at the
            // start, which reads as dictation being broken.
            guard recognizer.supportsOnDeviceRecognition else {
                throw ATMSpeechTranscriberError.onDeviceRecognitionUnavailable
            }
            request.requiresOnDeviceRecognition = true
        }

        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            throw ATMSpeechTranscriberError.invalidAudioInput
        }

        inputNode.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            request.append(buffer)
            let level = ATMAudioLevel.rms(of: buffer)
            Task { @MainActor [weak self] in self?.inputLevel = level }
        }
        inputTapInstalled = true

        engine.prepare()
        do {
            try engine.start()
        } catch {
            inputNode.removeTap(onBus: 0)
            inputTapInstalled = false
            throw error
        }

        audioEngine = engine
        recognitionRequest = request
        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor [weak self] in
                guard let self else { return }
                if let text = result?.bestTranscription.formattedString {
                    self.latestTranscript = text
                    onPartialResult(text)
                }
                // Ending the audio makes the task report cancellation; that is the
                // normal end of a recording, not a failure to show anyone.
                if let error, !self.isFinishing {
                    onFailure(error)
                }
            }
        }
    }

    func finish() async throws -> String {
        isFinishing = true
        stopAudioCapture()
        recognitionRequest?.endAudio()

        // Give Speech a short window to turn the last audio buffers into a final
        // result — without it the tail of the sentence is routinely lost.
        try? await Task.sleep(for: .milliseconds(350))
        recognitionTask?.cancel()
        clearRecognitionObjects()
        return latestTranscript
    }

    func cancel() {
        isFinishing = true
        stopAudioCapture()
        recognitionRequest?.endAudio()
        recognitionTask?.cancel()
        clearRecognitionObjects()
        latestTranscript = ""
        inputLevel = 0
    }

    private func stopAudioCapture() {
        audioEngine?.stop()
        if inputTapInstalled, let inputNode = audioEngine?.inputNode {
            inputNode.removeTap(onBus: 0)
        }
        inputTapInstalled = false
        audioEngine = nil
    }

    private func clearRecognitionObjects() {
        recognizer = nil
        recognitionRequest = nil
        recognitionTask = nil
    }
}

/// Microphone consent, asked the same way by both engines.
enum ATMMicrophoneAuthorization {
    static func request() async throws {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized:
            return
        case .notDetermined:
            // The only branch that shows a dialog, and so the only one that can hang
            // waiting for a dialog that never appears.
            let answer = await ATMAuthorizationRequest.run { completion in
                AVCaptureDevice.requestAccess(for: .audio, completionHandler: completion)
            }
            guard let answer else { throw ATMSpeechTranscriberError.authorizationTimedOut }
            guard answer else { throw ATMSpeechTranscriberError.microphoneDenied }
        default:
            // Denied or restricted: asking again does nothing, macOS only shows the
            // prompt once. The error text points at System Settings instead.
            throw ATMSpeechTranscriberError.microphoneDenied
        }
    }
}

/// Loudness of one audio buffer, 0...1, for the overlay's level meter.
enum ATMAudioLevel {
    /// Root mean square scaled so ordinary speech fills most of the bar.
    ///
    /// Raw RMS of speech sits around 0.02–0.15, which would barely move a meter, so
    /// the value is boosted and clamped. This drives a rough visual only — it is
    /// never used to decide anything about the audio.
    static func rms(of buffer: AVAudioPCMBuffer) -> Float {
        guard let channel = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return 0 }
        let count = Int(buffer.frameLength)
        var sum: Float = 0
        for index in 0..<count {
            let sample = channel[index]
            sum += sample * sample
        }
        let level = (sum / Float(count)).squareRoot()
        return min(1, level * 8)
    }
}
