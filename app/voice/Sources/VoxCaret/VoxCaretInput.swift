import AppKit
import AVFoundation
import Carbon.HIToolbox
import Foundation
import Speech

/// Whether this process is a real `.app`.
///
/// `swift run` produces a bare executable — ad-hoc signed, with the Info.plist linked
/// into `__TEXT,__info_plist`, but not a bundle and not registered with Launch
/// Services. Several system services refuse to work in that shape, and each one fails
/// differently: notifications throw, and TCC's permission dialogs simply never appear,
/// leaving the authorization callback pending forever.
///
/// One place to ask, so a new caller inherits the answer instead of rediscovering the
/// failure mode.
enum VoxCaretAppBundle {
    static let isBundled = Bundle.main.bundleURL.pathExtension == "app"
}

/// The boundary between "get audio, give me text" and everything else about
/// dictation.
///
/// Two engines sit behind it — Apple's Speech framework and a local SenseVoice
/// model — and they differ in a way that reaches the screen: Apple streams partial
/// results while you talk, SenseVoice only produces text once the audio stops. That
/// is why `start` takes a partial-result callback that one implementation simply
/// never calls, rather than each engine owning its own display.
///
/// Nothing above this protocol — the hot key, the overlay, the injection step —
/// knows which engine is running.
@MainActor
protocol VoxCaretSpeechTranscribing: AnyObject {
    /// Shown in the overlay while recording, because a silent fallback from
    /// SenseVoice to Apple Speech would otherwise be invisible.
    var displayName: String { get }

    /// Streams the microphone level, 0...1, for the engines that give no partial
    /// text. Real RMS rather than an animation: a flat bar means the wrong input
    /// device is selected, which is worth being able to see.
    var inputLevel: Float { get }

    /// Whether `start` can publish partial text while recording.
    var providesPartialResults: Bool { get }

    func requestAuthorization() async throws
    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws
    func finish() async throws -> String
    func cancel()

    /// Loads whatever is expensive to load. Called when the key goes down, in
    /// parallel with audio capture, so a model load overlaps with the person
    /// speaking instead of delaying the result.
    func prepare() async
}

extension VoxCaretSpeechTranscribing {
    var inputLevel: Float { 0 }
    var providesPartialResults: Bool { false }
    func prepare() async {}
}

enum VoxCaretSpeechTranscriberError: LocalizedError {
    case requiresAppBundle
    case authorizationTimedOut
    case microphoneDenied
    case speechRecognitionDenied
    case recognizerUnavailable
    case onDeviceRecognitionUnavailable
    case invalidAudioInput

    var errorDescription: String? {
        switch self {
        case .requiresAppBundle:
            return "语音输入需要以 .app 运行。`swift run` 是裸可执行文件，系统弹不出麦克风授权框。改用 app/voice/Scripts/build-app.sh。"
        case .authorizationTimedOut:
            return "等授权等超时了。如果没看到系统弹窗，多半是这个进程不是正规 .app；否则到「系统设置 → 隐私与安全性」里手动允许。"
        case .microphoneDenied:
            return "没有麦克风权限。到「系统设置 → 隐私与安全性 → 麦克风」里允许 VoxCaret 使用麦克风。"
        case .speechRecognitionDenied:
            return "没有语音识别权限。到「系统设置 → 隐私与安全性 → 语音识别」里允许 VoxCaret。"
        case .recognizerUnavailable:
            return "系统语音识别当前不可用。"
        case .onDeviceRecognitionUnavailable:
            return "所选语言不支持设备端识别，请关掉「仅使用本地识别」或换一种语言。"
        case .invalidAudioInput:
            return "没有找到可用的音频输入设备。"
        }
    }
}

/// One `Bool` answer out of a callback-style authorization API, with a deadline.
///
/// Both `AVCaptureDevice.requestAccess` and `SFSpeechRecognizer.requestAuthorization`
/// promise a callback and deliver one in every case Apple documents — but "the dialog
/// never appeared" is not one of those cases, and there the callback simply never
/// comes. Without a deadline that leaves dictation parked in 正在打开麦克风… with no
/// way to tell it apart from a person who has not clicked the dialog yet.
///
/// Answers `nil` on timeout rather than throwing, so callers decide what a missing
/// answer means.
enum VoxCaretAuthorizationRequest {
    /// Long enough for someone to actually find and click the dialog, short enough that
    /// a dialog which never appeared stops looking like a hang.
    static let timeout: Duration = .seconds(45)

    /// The completion is `@Sendable` because the system frameworks call it from
    /// whatever queue they please.
    static func run(
        timeout: Duration = Self.timeout,
        _ request: @escaping (@escaping @Sendable (Bool) -> Void) -> Void
    ) async -> Bool? {
        let box = Box()
        return await withCheckedContinuation { continuation in
            box.arm(continuation)
            request { granted in box.settle(granted) }
            Task {
                try? await Task.sleep(for: timeout)
                box.settle(nil)
            }
        }
    }

    /// Guarantees exactly one `resume`: the callback and the deadline race, and resuming
    /// a continuation twice traps.
    private final class Box: @unchecked Sendable {
        private let lock = NSLock()
        private var continuation: CheckedContinuation<Bool?, Never>?
        private var settled = false

        func arm(_ continuation: CheckedContinuation<Bool?, Never>) {
            lock.lock()
            self.continuation = continuation
            lock.unlock()
        }

        func settle(_ value: Bool?) {
            lock.lock()
            guard !settled else {
                lock.unlock()
                return
            }
            settled = true
            let continuation = self.continuation
            self.continuation = nil
            lock.unlock()
            continuation?.resume(returning: value)
        }
    }
}

/// Which recognizer transcribes the audio.
enum VoxCaretRecognitionEngine: String, CaseIterable, Identifiable {
    case senseVoice
    case appleSpeech

    var id: String { rawValue }

    var label: String {
        switch self {
        case .senseVoice: return "SenseVoice Small（本地）"
        case .appleSpeech: return "Apple Speech"
        }
    }

    var detail: String {
        switch self {
        case .senseVoice:
            return "离线本地模型，中文与中英混说更准，说完才出结果。需要先下载约 160MB 模型。"
        case .appleSpeech:
            return "系统自带，边说边出字，无需下载。默认可能把音频送到 Apple 服务器，可开启「仅使用本地识别」。"
        }
    }
}

/// What dictation writes into, per language. One choice drives both engines: Apple
/// wants a `Locale`, SenseVoice wants its own short code, and letting them drift
/// apart would mean the setting says 中文 while one of them listens for English.
enum VoxCaretInputLanguage: String, CaseIterable, Identifiable {
    case auto
    case chinese = "zh-CN"
    case english = "en-US"
    case japanese = "ja-JP"
    case korean = "ko-KR"
    case cantonese = "yue-Hant-HK"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .auto: return "自动"
        case .chinese: return "中文"
        case .english: return "English"
        case .japanese: return "日本語"
        case .korean: return "한국어"
        case .cantonese: return "粤语"
        }
    }

    /// `.current` for 自动: Apple's recognizer has no auto-detect, so the closest
    /// honest answer is the language the system is already set to.
    var locale: Locale {
        switch self {
        case .auto: return .current
        default: return Locale(identifier: rawValue)
        }
    }

    var senseVoiceCode: String {
        switch self {
        case .auto: return "auto"
        case .chinese: return "zh"
        case .english: return "en"
        case .japanese: return "ja"
        case .korean: return "ko"
        case .cantonese: return "yue"
        }
    }
}

/// One rewrite applied to the transcript, and fed to Apple's recognizer as a
/// contextual hint so the right spelling gets a better chance of winning outright.
struct VoxCaretReplacement: Equatable {
    let source: String
    let target: String
}

/// Where dictation's settings live. Same shape as `VoxCaretGlobalHotKeyPreferences` and
/// `VoxCaretAgentNotchPreferences`: raw UserDefaults keys the settings screen binds to
/// with `@AppStorage`, plus resolved accessors for everyone else.
enum VoxCaretInputPreferences {
    static let hotKeyEnabledKey = "VoxCaretInputHotKeyEnabled"
    static let hotKeyKey = "VoxCaretInputHotKey"
    static let engineKey = "VoxCaretInputEngine"
    static let languageKey = "VoxCaretInputLanguage"
    static let onDeviceOnlyKey = "VoxCaretInputOnDeviceOnly"
    static let removeTrailingPeriodKey = "VoxCaretInputRemoveTrailingPeriod"
    static let dictionaryKey = "VoxCaretInputDictionary"
    static let liveInsertionEnabledKey = "VoxCaretLiveInsertionEnabled"
    static let rightCommandHoldEnabledKey = "VoxCaretRightCommandHoldEnabled"

    static let defaultEnabled = true
    static let defaultEngine = VoxCaretRecognitionEngine.senseVoice
    static let defaultLanguage = VoxCaretInputLanguage.auto

    /// ⌥Space. Not ⌘-anything: this key is held down for the length of a sentence,
    /// and ⌥ is the modifier least likely to be doing something else while held.
    /// The cost is macOS's own ⌥Space (a non-breaking space), which is a fair trade
    /// for the shortcut this feature is built around — and it is rebindable.
    static let defaultHotKey = VoxCaretHotKey(keyCode: UInt16(kVK_Space), modifiers: [.option])

    static let hotKeyBinding = VoxCaretHotKeyBinding(
        enabledKey: hotKeyEnabledKey,
        hotKeyKey: hotKeyKey,
        defaultEnabled: defaultEnabled,
        defaultHotKey: defaultHotKey
    )

    /// ⎋ while recording. Not stored anywhere — VoxCaret owns it only for the few
    /// seconds a recording lasts.
    static let cancelHotKey = VoxCaretHotKey(keyCode: UInt16(kVK_Escape), modifiers: [])

    /// Longest single recording. Carbon's release event normally ends it immediately;
    /// this is only a safety cap for a lost event, roomy enough for real dictation.
    static let maximumRecordingDuration: Duration = .seconds(300)

    static var isHotKeyEnabled: Bool { hotKeyBinding.isEnabled }
    static var hotKey: VoxCaretHotKey { hotKeyBinding.hotKey }

    /// SenseVoice by default even though its model is not downloaded yet: the
    /// router falls back to Apple Speech until it is, so the out-of-the-box
    /// experience works and quietly improves once someone downloads the model.
    static var engine: VoxCaretRecognitionEngine {
        guard let raw = UserDefaults.standard.string(forKey: engineKey),
              let engine = VoxCaretRecognitionEngine(rawValue: raw) else {
            return defaultEngine
        }
        return engine
    }

    static var language: VoxCaretInputLanguage {
        guard let raw = UserDefaults.standard.string(forKey: languageKey),
              let language = VoxCaretInputLanguage(rawValue: raw) else {
            return defaultLanguage
        }
        return language
    }

    static var onDeviceOnly: Bool {
        UserDefaults.standard.bool(forKey: onDeviceOnlyKey)
    }

    static var removeTrailingPeriod: Bool {
        UserDefaults.standard.bool(forKey: removeTrailingPeriodKey)
    }

    static var liveInsertionEnabled: Bool {
        guard UserDefaults.standard.object(forKey: liveInsertionEnabledKey) != nil else { return true }
        return UserDefaults.standard.bool(forKey: liveInsertionEnabledKey)
    }

    static var rightCommandHoldEnabled: Bool {
        guard UserDefaults.standard.object(forKey: rightCommandHoldEnabledKey) != nil else { return true }
        return UserDefaults.standard.bool(forKey: rightCommandHoldEnabledKey)
    }

    static var replacements: [VoxCaretReplacement] {
        VoxCaretTextCleanup.parseReplacements(UserDefaults.standard.string(forKey: dictionaryKey) ?? "")
    }

    /// Both sides of every rewrite. The source is worth hinting too: the recognizer
    /// has to produce something recognisable before a rewrite can fire at all.
    static var contextualTerms: [String] {
        replacements.flatMap { [$0.source, $0.target] }
    }
}

/// Read-only view of the three permissions dictation needs, so the settings screen
/// can show what is missing without triggering a prompt just by being opened.
enum VoxCaretPermissions {
    enum Status: Equatable {
        case granted
        case denied
        case notDetermined
    }

    static var microphone: Status {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized: return .granted
        case .notDetermined: return .notDetermined
        default: return .denied
        }
    }

    /// Only meaningful for Apple Speech; SenseVoice never asks for it.
    static var speechRecognition: Status {
        switch SFSpeechRecognizer.authorizationStatus() {
        case .authorized: return .granted
        case .notDetermined: return .notDetermined
        default: return .denied
        }
    }

    /// Accessibility has no "not determined": either this process is trusted or it
    /// is not. Checked without the prompt option so opening settings stays inert.
    static var accessibility: Status {
        AXIsProcessTrusted() ? .granted : .denied
    }

    static func openSystemSettings(_ pane: Pane) {
        guard let url = URL(string: pane.urlString) else { return }
        NSWorkspace.shared.open(url)
    }

    enum Pane: Equatable {
        case microphone
        case speechRecognition
        case accessibility

        var urlString: String {
            switch self {
            case .microphone:
                return "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
            case .speechRecognition:
                return "x-apple.systempreferences:com.apple.preference.security?Privacy_SpeechRecognition"
            case .accessibility:
                return "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
            }
        }
    }
}

/// All three permission answers read at one moment.
///
/// A value rather than three live reads because the settings screen shows them as a
/// list someone acts on: reading each one lazily during a redraw would let the rows
/// disagree with each other mid-scroll. Refreshed when the pane appears and when
/// 重新检查 is pressed — TCC has no change notification to observe.
struct VoxCaretPermissionSnapshot: Equatable {
    let microphone: VoxCaretPermissions.Status
    let speechRecognition: VoxCaretPermissions.Status
    let accessibility: VoxCaretPermissions.Status

    static func current() -> VoxCaretPermissionSnapshot {
        VoxCaretPermissionSnapshot(
            microphone: VoxCaretPermissions.microphone,
            speechRecognition: VoxCaretPermissions.speechRecognition,
            accessibility: VoxCaretPermissions.accessibility
        )
    }
}
