import AVFoundation
import Foundation
import SherpaOnnx

enum ATMSenseVoiceTranscriberError: LocalizedError {
    case modelMissing
    case modelLoadFailed
    case noAudio
    case recognitionFailed

    var errorDescription: String? {
        switch self {
        case .modelMissing: return "SenseVoice 模型还没下载，到「设置 → 语音」下载。"
        case .modelLoadFailed: return "SenseVoice 模型加载失败，请删除模型后重新下载。"
        case .noAudio: return "没有录到有效语音，检查一下麦克风。"
        case .recognitionFailed: return "SenseVoice 没有产出识别结果。"
        }
    }
}

/// Dictation through a local SenseVoice Small model.
///
/// Offline in both senses: no network, and no streaming. The model transcribes a
/// finished utterance rather than a growing one, so there is no partial text to
/// show — the overlay falls back to a level meter. What that buys is markedly better
/// Chinese and mixed Chinese/English than Apple Speech, with audio that never leaves
/// the machine.
///
/// The batch nature has a useful consequence: inference only happens on release, so
/// loading the model can overlap with the person speaking. See `prepare`.
@MainActor
final class ATMSenseVoiceTranscriber: ATMSpeechTranscribing {
    let displayName = "SenseVoice Small"

    private(set) var inputLevel: Float = 0

    private var audioEngine: AVAudioEngine?
    private var inputTapInstalled = false
    private var accumulator = ATMSenseVoiceAudioAccumulator()

    func requestAuthorization() async throws {
        try await ATMMicrophoneAuthorization.request()
        guard ATMSenseVoiceModelManager.shared.isModelReady else {
            throw ATMSenseVoiceTranscriberError.modelMissing
        }
    }

    /// Loads the ONNX session. Called at key-down, concurrently with capture: a cold
    /// load costs about a second, and a second spent while someone is still talking
    /// costs nothing at all.
    func prepare() async {
        guard let files = ATMSenseVoiceModelManager.shared.modelFiles else { return }
        try? await ATMSenseVoiceEngine.shared.prepare(
            files: files,
            language: ATMVoiceInputPreferences.language.senseVoiceCode
        )
    }

    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws {
        cancel()
        guard ATMSenseVoiceModelManager.shared.modelFiles != nil else {
            throw ATMSenseVoiceTranscriberError.modelMissing
        }

        let engine = AVAudioEngine()
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            throw ATMSpeechTranscriberError.invalidAudioInput
        }

        let accumulator = ATMSenseVoiceAudioAccumulator()
        self.accumulator = accumulator
        input.installTap(onBus: 0, bufferSize: 2048, format: format) { [weak self] buffer, _ in
            accumulator.append(buffer, sampleRate: format.sampleRate)
            let level = ATMAudioLevel.rms(of: buffer)
            Task { @MainActor [weak self] in self?.inputLevel = level }
        }
        inputTapInstalled = true

        engine.prepare()
        do {
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            inputTapInstalled = false
            throw error
        }
        audioEngine = engine
    }

    func finish() async throws -> String {
        stopCapture()
        let audio = accumulator.snapshot()
        // Under a tenth of a second is a mis-press, not an utterance. Recognising it
        // would return noise, and noise gets pasted.
        guard audio.samples.count >= max(1, Int(audio.sampleRate / 10)) else {
            throw ATMSenseVoiceTranscriberError.noAudio
        }
        guard let files = ATMSenseVoiceModelManager.shared.modelFiles else {
            throw ATMSenseVoiceTranscriberError.modelMissing
        }
        return try await ATMSenseVoiceEngine.shared.transcribe(
            samples: audio.samples,
            sampleRate: Int(audio.sampleRate),
            files: files,
            language: ATMVoiceInputPreferences.language.senseVoiceCode
        )
    }

    func cancel() {
        stopCapture()
        accumulator.reset()
        inputLevel = 0
    }

    private func stopCapture() {
        audioEngine?.stop()
        if inputTapInstalled, let input = audioEngine?.inputNode {
            input.removeTap(onBus: 0)
        }
        inputTapInstalled = false
        audioEngine = nil
    }
}

/// Collects microphone samples for the length of one utterance.
///
/// The audio tap runs on a real-time thread that must not block, so it appends under
/// a plain lock and nothing else. Sample rate is recorded alongside because it comes
/// from whatever input device is selected, not from an assumption.
private final class ATMSenseVoiceAudioAccumulator: @unchecked Sendable {
    private let lock = NSLock()
    private var samples: [Float] = []
    private var sampleRate: Double = 16_000

    func append(_ buffer: AVAudioPCMBuffer, sampleRate: Double) {
        guard let channels = buffer.floatChannelData, buffer.frameLength > 0 else { return }
        let count = Int(buffer.frameLength)
        lock.lock()
        self.sampleRate = sampleRate
        samples.append(contentsOf: UnsafeBufferPointer(start: channels[0], count: count))
        lock.unlock()
    }

    func snapshot() -> (samples: [Float], sampleRate: Double) {
        lock.lock()
        defer { lock.unlock() }
        return (samples, sampleRate)
    }

    func reset() {
        lock.lock()
        samples.removeAll(keepingCapacity: false)
        lock.unlock()
    }
}

/// The loaded ONNX recognizer, and the decision of how long to keep it loaded.
///
/// An actor because the underlying handle is a C pointer that must not be used from
/// two places at once, and because the load is slow enough that callers should be
/// suspended rather than blocked.
///
/// It unloads itself after five idle minutes. ATM is a menu bar app that runs all
/// day, and holding roughly 300MB of onnxruntime session for a feature used in
/// bursts is the wrong default — but so is reloading between two sentences. Five
/// minutes keeps a dictation session hot and gives the memory back afterwards; the
/// reload that follows is hidden by `prepare` running during capture.
actor ATMSenseVoiceEngine {
    static let shared = ATMSenseVoiceEngine()

    static let idleUnloadDelay: Duration = .seconds(300)

    private var recognizer: OpaquePointer?
    /// Model paths plus language. The recognizer bakes its language in at creation,
    /// so changing the setting has to rebuild it rather than being passed per call.
    private var configurationKey = ""
    private var idleUnloadTask: Task<Void, Never>?

    func prepare(files: ATMSenseVoiceModelFiles, language: String) throws {
        let key = "\(files.model.path)|\(files.tokens.path)|\(language)"
        if recognizer != nil, key == configurationKey {
            scheduleIdleUnload()
            return
        }
        unloadNow()

        let created = files.model.path.withCString { modelPath in
            files.tokens.path.withCString { tokensPath in
                language.withCString { languageHint in
                    "cpu".withCString { provider in
                        "greedy_search".withCString { decodingMethod in
                            var senseVoice = SherpaOnnxOfflineSenseVoiceModelConfig()
                            senseVoice.model = modelPath
                            senseVoice.language = languageHint
                            // Inverse text normalization: writes numbers and dates as
                            // digits, which is what anyone dictating into a message
                            // box expects to see.
                            senseVoice.use_itn = 1

                            var model = SherpaOnnxOfflineModelConfig()
                            model.tokens = tokensPath
                            // Half the cores, capped: this runs on a machine whose
                            // main job is whatever the person is actually doing.
                            model.num_threads = Int32(
                                min(4, max(1, ProcessInfo.processInfo.activeProcessorCount / 2))
                            )
                            model.provider = provider
                            model.sense_voice = senseVoice

                            var config = SherpaOnnxOfflineRecognizerConfig()
                            config.feat_config.sample_rate = 16_000
                            config.feat_config.feature_dim = 80
                            config.model_config = model
                            config.decoding_method = decodingMethod
                            config.max_active_paths = 4
                            return SherpaOnnxCreateOfflineRecognizer(&config)
                        }
                    }
                }
            }
        }
        guard let created else { throw ATMSenseVoiceTranscriberError.modelLoadFailed }
        recognizer = created
        configurationKey = key
        scheduleIdleUnload()
    }

    func transcribe(
        samples: [Float],
        sampleRate: Int,
        files: ATMSenseVoiceModelFiles,
        language: String
    ) throws -> String {
        try prepare(files: files, language: language)
        guard let recognizer, let stream = SherpaOnnxCreateOfflineStream(recognizer) else {
            throw ATMSenseVoiceTranscriberError.recognitionFailed
        }
        defer { SherpaOnnxDestroyOfflineStream(stream) }

        // The device's real sample rate is passed through; sherpa resamples to the
        // 16kHz the features are configured for.
        samples.withUnsafeBufferPointer { buffer in
            SherpaOnnxAcceptWaveformOffline(
                stream,
                Int32(sampleRate),
                buffer.baseAddress,
                Int32(buffer.count)
            )
        }
        SherpaOnnxDecodeOfflineStream(recognizer, stream)
        guard let result = SherpaOnnxGetOfflineStreamResult(stream) else {
            throw ATMSenseVoiceTranscriberError.recognitionFailed
        }
        defer { SherpaOnnxDestroyOfflineRecognizerResult(result) }
        guard let value = result.pointee.text else {
            throw ATMSenseVoiceTranscriberError.recognitionFailed
        }
        scheduleIdleUnload()
        return String(cString: value)
    }

    func unload() {
        idleUnloadTask?.cancel()
        idleUnloadTask = nil
        unloadNow()
    }

    private func unloadNow() {
        if let recognizer { SherpaOnnxDestroyOfflineRecognizer(recognizer) }
        recognizer = nil
        configurationKey = ""
    }

    /// Restarted on every use, so the timer measures idleness rather than age.
    private func scheduleIdleUnload() {
        idleUnloadTask?.cancel()
        idleUnloadTask = Task { [weak self] in
            try? await Task.sleep(for: Self.idleUnloadDelay)
            guard !Task.isCancelled else { return }
            await self?.unloadIfIdle()
        }
    }

    private func unloadIfIdle() {
        idleUnloadTask = nil
        unloadNow()
    }
}
