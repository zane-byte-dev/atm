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
final class VoxCaretAppleSpeechTranscriber: VoxCaretSpeechTranscribing {
    let displayName = "Apple Speech"
    let providesPartialResults = true

    private(set) var inputLevel: Float = 0

    private var recognizer: SFSpeechRecognizer?
    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private let audioSink = VoxCaretSpeechAudioBufferSink()
    private var inputTapInstalled = false
    private var latestTranscript = ""
    private var committedTranscript = ""
    private var currentSegmentTranscript = ""
    private var isFinishing = false
    private var segmentGeneration = 0
    private var consecutiveSegmentErrors = 0
    private var partialResultHandler: ((String) -> Void)?
    private var failureHandler: ((Error) -> Void)?
    private var finalResultStream: AsyncStream<String>?
    private var finalResultContinuation: AsyncStream<String>.Continuation?

    func requestAuthorization() async throws {
        // Already answered: no dialog is coming, so no deadline is needed either.
        switch SFSpeechRecognizer.authorizationStatus() {
        case .authorized:
            break
        case .notDetermined:
            let answer = await VoxCaretAuthorizationRequest.run { completion in
                SFSpeechRecognizer.requestAuthorization { completion($0 == .authorized) }
            }
            guard let answer else { throw VoxCaretSpeechTranscriberError.authorizationTimedOut }
            guard answer else { throw VoxCaretSpeechTranscriberError.speechRecognitionDenied }
        default:
            throw VoxCaretSpeechTranscriberError.speechRecognitionDenied
        }
        try await VoxCaretMicrophoneAuthorization.request()
    }

    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws {
        cancel()
        let language = VoxCaretInputPreferences.language
        guard let recognizer = SFSpeechRecognizer(locale: language.locale), recognizer.isAvailable else {
            throw VoxCaretSpeechTranscriberError.recognizerUnavailable
        }
        self.recognizer = recognizer

        latestTranscript = ""
        committedTranscript = ""
        currentSegmentTranscript = ""
        isFinishing = false
        consecutiveSegmentErrors = 0
        partialResultHandler = onPartialResult
        failureHandler = onFailure
        let finalResults = AsyncStream<String>.makeStream()
        finalResultStream = finalResults.stream
        finalResultContinuation = finalResults.continuation

        let engine = AVAudioEngine()
        if VoxCaretInputPreferences.onDeviceOnly {
            // Checked rather than set-and-hope: with the flag on and the language
            // unsupported, the recognizer fails partway through instead of at the
            // start, which reads as dictation being broken.
            guard recognizer.supportsOnDeviceRecognition else {
                throw VoxCaretSpeechTranscriberError.onDeviceRecognitionUnavailable
            }
        }

        beginRecognitionSegment()

        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            throw VoxCaretSpeechTranscriberError.invalidAudioInput
        }

        inputNode.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            self?.audioSink.append(buffer)
            let level = VoxCaretAudioLevel.rms(of: buffer)
            Task { @MainActor [weak self] in self?.inputLevel = level }
        }
        inputTapInstalled = true

        engine.prepare()
        do {
            try engine.start()
        } catch {
            inputNode.removeTap(onBus: 0)
            inputTapInstalled = false
            recognitionTask?.cancel()
            clearRecognitionObjects()
            throw error
        }

        audioEngine = engine
    }

    func finish() async throws -> String {
        isFinishing = true
        stopAudioCapture()
        recognitionRequest?.endAudio()

        // A fixed delay routinely loses the last few words on a busy machine or a
        // network-backed recognizer. Wait for Speech's final result, but keep a hard
        // deadline so a recognizer that never closes cannot strand dictation.
        if let finalResultStream,
           let final = await VoxCaretFinalResultWaiter.first(
               in: finalResultStream,
               timeout: .seconds(2)
           ) {
            latestTranscript = final
        }
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
        committedTranscript = ""
        currentSegmentTranscript = ""
        inputLevel = 0
    }

    /// Apple's recognizer is allowed to finalize after a pause even though the hot key
    /// is still held. A recognition request cannot accept audio after that point, so a
    /// long utterance is a chain of short Speech tasks over one uninterrupted mic tap.
    /// The accumulated transcript is emitted on every callback, keeping live insertion
    /// stable across those otherwise invisible segment boundaries.
    private func beginRecognitionSegment() {
        guard let recognizer, !isFinishing else { return }

        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        request.contextualStrings = VoxCaretInputPreferences.contextualTerms
        request.addsPunctuation = true
        request.requiresOnDeviceRecognition = VoxCaretInputPreferences.onDeviceOnly

        segmentGeneration += 1
        let currentSegment = segmentGeneration
        recognitionRequest = request
        audioSink.replace(with: request)
        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor [weak self] in
                self?.handleRecognitionCallback(
                    result: result,
                    error: error,
                    segment: currentSegment
                )
            }
        }
    }

    private func handleRecognitionCallback(
        result: SFSpeechRecognitionResult?,
        error: Error?,
        segment: Int
    ) {
        guard segment == segmentGeneration else { return }

        if let text = result?.bestTranscription.formattedString, !text.isEmpty {
            currentSegmentTranscript = text
            latestTranscript = VoxCaretTranscriptSegments.merge(
                committedTranscript,
                currentSegmentTranscript
            )
            consecutiveSegmentErrors = 0
            partialResultHandler?(latestTranscript)
        }

        if result?.isFinal == true {
            commitCurrentSegment()
            if isFinishing {
                finishFinalResultStream()
            } else {
                VoxCaretLog.lifecycle("apple_speech_segment_rotated")
                beginRecognitionSegment()
            }
            return
        }

        guard let error else { return }
        if isFinishing {
            // Ending audio commonly arrives as a cancellation error. The best partial
            // is still a valid result and must not turn a normal key release red.
            commitCurrentSegment()
            finishFinalResultStream()
            return
        }

        // A network-backed task can also end transiently without a final callback.
        // Preserve what it heard and retry twice while the microphone stays open;
        // persistent service failures still surface instead of spinning forever.
        commitCurrentSegment()
        consecutiveSegmentErrors += 1
        if consecutiveSegmentErrors <= 2 {
            VoxCaretLog.lifecycle("apple_speech_segment_recovered")
            beginRecognitionSegment()
        } else {
            finalResultContinuation?.finish()
            failureHandler?(error)
        }
    }

    private func commitCurrentSegment() {
        guard !currentSegmentTranscript.isEmpty else { return }
        committedTranscript = VoxCaretTranscriptSegments.merge(
            committedTranscript,
            currentSegmentTranscript
        )
        currentSegmentTranscript = ""
        latestTranscript = committedTranscript
    }

    private func finishFinalResultStream() {
        if !latestTranscript.isEmpty {
            finalResultContinuation?.yield(latestTranscript)
        }
        finalResultContinuation?.finish()
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
        segmentGeneration += 1
        audioSink.replace(with: nil)
        finalResultContinuation?.finish()
        finalResultContinuation = nil
        finalResultStream = nil
        partialResultHandler = nil
        failureHandler = nil
        recognizer = nil
        recognitionRequest = nil
        recognitionTask = nil
    }
}

/// Thread-safe handoff between AVAudioEngine's real-time tap and Speech requests.
/// Replacing a request takes only the duration of assigning one reference; audio keeps
/// flowing while Apple finalizes a segment and the next segment starts.
private final class VoxCaretSpeechAudioBufferSink: @unchecked Sendable {
    private let lock = NSLock()
    private var request: SFSpeechAudioBufferRecognitionRequest?

    func replace(with request: SFSpeechAudioBufferRecognitionRequest?) {
        lock.lock()
        self.request = request
        lock.unlock()
    }

    func append(_ buffer: AVAudioPCMBuffer) {
        lock.lock()
        let request = self.request
        lock.unlock()
        request?.append(buffer)
    }
}

/// Joins Speech segments without adding spaces between CJK characters or repeating
/// the short overlap Apple sometimes carries into the next segment.
enum VoxCaretTranscriptSegments {
    static func merge(_ committed: String, _ incoming: String) -> String {
        let left = committed.trimmingCharacters(in: .whitespacesAndNewlines)
        let right = incoming.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !left.isEmpty else { return right }
        guard !right.isEmpty else { return left }
        if right.hasPrefix(left) { return right }
        if left.hasSuffix(right) { return left }

        let leftCharacters = Array(left)
        let rightCharacters = Array(right)
        let maximumOverlap = min(24, leftCharacters.count, rightCharacters.count)
        var overlap = 0
        if maximumOverlap > 0 {
            for length in stride(from: maximumOverlap, through: 1, by: -1) {
                if leftCharacters.suffix(length).elementsEqual(rightCharacters.prefix(length)) {
                    overlap = length
                    break
                }
            }
        }

        let remainder = String(rightCharacters.dropFirst(overlap))
        guard !remainder.isEmpty else { return left }
        let needsSpace = left.last?.isASCIIWordCharacter == true
            && remainder.first?.isASCIIWordCharacter == true
            && overlap == 0
        return left + (needsSpace ? " " : "") + remainder
    }
}

private extension Character {
    var isASCIIWordCharacter: Bool {
        unicodeScalars.allSatisfy { scalar in
            scalar.isASCII && (CharacterSet.alphanumerics.contains(scalar) || scalar == "_")
        }
    }
}

/// Waits for the final callback without allowing Speech to hold the recording open
/// forever. Kept independent of Speech so the callback/timeout race is testable.
enum VoxCaretFinalResultWaiter {
    static func first(in stream: AsyncStream<String>, timeout: Duration) async -> String? {
        await withTaskGroup(of: String?.self) { group in
            group.addTask {
                var iterator = stream.makeAsyncIterator()
                return await iterator.next()
            }
            group.addTask {
                do {
                    try await Task.sleep(for: timeout)
                    return nil
                } catch {
                    return nil
                }
            }
            let value = await group.next() ?? nil
            group.cancelAll()
            return value
        }
    }
}

/// Microphone consent, asked the same way by both engines.
enum VoxCaretMicrophoneAuthorization {
    static func request() async throws {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized:
            return
        case .notDetermined:
            // The only branch that shows a dialog, and so the only one that can hang
            // waiting for a dialog that never appears.
            let answer = await VoxCaretAuthorizationRequest.run { completion in
                AVCaptureDevice.requestAccess(for: .audio, completionHandler: completion)
            }
            guard let answer else { throw VoxCaretSpeechTranscriberError.authorizationTimedOut }
            guard answer else { throw VoxCaretSpeechTranscriberError.microphoneDenied }
        default:
            // Denied or restricted: asking again does nothing, macOS only shows the
            // prompt once. The error text points at System Settings instead.
            throw VoxCaretSpeechTranscriberError.microphoneDenied
        }
    }
}

/// Loudness of one audio buffer, 0...1, for the overlay's level meter.
enum VoxCaretAudioLevel {
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
