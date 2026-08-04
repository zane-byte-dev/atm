import AppKit
import Foundation

enum ATMAgentSound: String, CaseIterable, Identifiable {
    case pingIslandMenuSelect = "PingIsland.8bit_menu_select"
    case pingIslandApprovalAlert = "PingIsland.8bit_approval_alert"
    case pingIslandSubmitBlip = "PingIsland.8bit_submit_blip"
    case none = "None"
    case pop = "Pop"
    case ping = "Ping"
    case tink = "Tink"
    case glass = "Glass"
    case blow = "Blow"
    case bottle = "Bottle"
    case hero = "Hero"
    case purr = "Purr"
    case basso = "Basso"

    var id: String { rawValue }

    /// SwiftPM keeps target resources in a nested bundle. Development builds can
    /// use `Bundle.module` directly, while the hand-assembled `.app` stores that
    /// bundle under `Contents/Resources`, where a distributable app expects it.
    private static let resourceBundle: Bundle = {
        let name = "ATMMenuBarApp_ATMMenuBarApp.bundle"
        let candidates = [
            Bundle.main.resourceURL,
            Bundle.main.bundleURL,
            Bundle.main.executableURL?.deletingLastPathComponent(),
        ]
        for baseURL in candidates.compactMap({ $0 }) {
            if let bundle = Bundle(url: baseURL.appendingPathComponent(name, isDirectory: true)) {
                return bundle
            }
        }
        return Bundle.module
    }()

    var title: String {
        switch self {
        case .pingIslandMenuSelect: return "Ping Island · Menu Select"
        case .pingIslandApprovalAlert: return "Ping Island · Approval Alert"
        case .pingIslandSubmitBlip: return "Ping Island · Submit Blip"
        case .none: return "静音"
        default: return rawValue
        }
    }

    var bundledResourceName: String? {
        switch self {
        case .pingIslandMenuSelect: return "8bit_menu_select"
        case .pingIslandApprovalAlert: return "8bit_approval_alert"
        case .pingIslandSubmitBlip: return "8bit_submit_blip"
        default: return nil
        }
    }

    var bundledResourceURL: URL? {
        guard let bundledResourceName else { return nil }
        return Self.resourceBundle.url(
            forResource: bundledResourceName,
            withExtension: "wav",
            subdirectory: "Sounds"
        ) ?? Self.resourceBundle.url(forResource: bundledResourceName, withExtension: "wav")
    }

    var systemSoundName: NSSound.Name? {
        switch self {
        case .none, .pingIslandMenuSelect, .pingIslandApprovalAlert, .pingIslandSubmitBlip:
            return nil
        default:
            return NSSound.Name(rawValue)
        }
    }
}

enum ATMAgentSoundEvent: String, CaseIterable, Identifiable, Equatable {
    case processingStarted
    case attentionRequired
    case taskCompleted

    var id: String { rawValue }

    var title: String {
        switch self {
        case .processingStarted: return "开始处理"
        case .attentionRequired: return "需要介入"
        case .taskCompleted: return "处理完成"
        }
    }

    var subtitle: String {
        switch self {
        case .processingStarted: return "新会话或新的用户输入开始执行。"
        case .attentionRequired: return "Agent 等待你确认、回答或补充信息。"
        case .taskCompleted: return "Agent 产生新的完整结果，可以回来查看。"
        }
    }

    var defaultSound: ATMAgentSound {
        switch self {
        case .processingStarted: return .pingIslandMenuSelect
        case .attentionRequired: return .pingIslandApprovalAlert
        case .taskCompleted: return .pingIslandSubmitBlip
        }
    }

    var defaultEnabled: Bool {
        switch self {
        case .processingStarted: return false
        case .attentionRequired, .taskCompleted: return true
        }
    }
}

enum ATMAgentSoundPreferences {
    static let enabledKey = "ATMAgentSoundsEnabled"
    static let volumeKey = "ATMAgentSoundVolume"
    static let defaultEnabled = true
    static let defaultVolume = 0.68

    static func enabledKey(for event: ATMAgentSoundEvent) -> String {
        "ATMAgentSound.\(event.rawValue).enabled"
    }

    static func soundKey(for event: ATMAgentSoundEvent) -> String {
        "ATMAgentSound.\(event.rawValue).sound"
    }

    static func isEnabled(
        for event: ATMAgentSoundEvent,
        defaults: UserDefaults = .standard
    ) -> Bool {
        guard masterEnabled(defaults: defaults) else { return false }
        let key = enabledKey(for: event)
        guard defaults.object(forKey: key) != nil else { return event.defaultEnabled }
        return defaults.bool(forKey: key)
    }

    static func masterEnabled(defaults: UserDefaults = .standard) -> Bool {
        guard defaults.object(forKey: enabledKey) != nil else { return defaultEnabled }
        return defaults.bool(forKey: enabledKey)
    }

    static func volume(defaults: UserDefaults = .standard) -> Float {
        guard defaults.object(forKey: volumeKey) != nil else { return Float(defaultVolume) }
        return Float(min(max(defaults.double(forKey: volumeKey), 0), 1))
    }

    static func sound(
        for event: ATMAgentSoundEvent,
        defaults: UserDefaults = .standard
    ) -> ATMAgentSound {
        guard let rawValue = defaults.string(forKey: soundKey(for: event)) else {
            return event.defaultSound
        }
        return ATMAgentSound(rawValue: rawValue) ?? event.defaultSound
    }
}

/// Maps a hook event straight onto a sound.
///
/// Preferred over the snapshot diffing below whenever a hook is installed: the
/// event says what happened, so there is no inference to get wrong and no
/// dependency on catching the change between two three-second polls.
extension ATMAgentEvent.Kind {
    var soundEvent: ATMAgentSoundEvent? {
        switch self {
        case .attention: return .attentionRequired
        case .completed: return .taskCompleted
        case .started: return .processingStarted
        // `resumed` fires once per tool call. It is a state correction, not a
        // moment worth a chime — and `started` already announced this turn.
        case .sessionStart, .sessionEnd, .resumed: return nil
        }
    }
}

/// Converts noisy three-second snapshots into at most one human-facing sound.
/// Attention wins over completion, and completion wins over processing, so one
/// refresh can never produce a stack of overlapping chimes.
///
/// Still needed after hooks: it covers the agents with no hook integration
/// (copilot, qoder, grok). For hooked sessions the event path fires first, and
/// `suppress(sessionIDs:)` keeps this from chiming a second time for the same
/// moment once the change shows up in a later snapshot.
struct ATMAgentSoundTransitionTracker {
    private var isPrimed = false
    private var previousAttentionIDs = Set<String>()
    private var previousInputBySessionID: [String: String] = [:]
    private var previousResultBySessionID: [String: String] = [:]
    private var playedCompletionKeys = Set<String>()

    /// - Parameter hookBacked: sessions whose transitions arrive as hook events.
    ///   Their state is still tracked, so that losing hook coverage later does not
    ///   replay a backlog of chimes, but they never trigger a sound from here.
    mutating func nextEvent(
        for sessions: [ATMLiveSession],
        hookBacked: (ATMLiveSession) -> Bool = { _ in false }
    ) -> ATMAgentSoundEvent? {
        let visible = sessions.filter { $0.activityState != "unobserved" }
        let soundable = Set(visible.filter { !hookBacked($0) }.map(\.id))
        let attentionIDs = Set(
            visible.filter { $0.presenceState == .attention }.map(\.id)
        )
        let inputBySessionID = Dictionary(
            uniqueKeysWithValues: visible.compactMap { session in
                session.latestUserInputText.map { (session.id, $0) }
            }
        )
        let resultBySessionID = Dictionary(
            uniqueKeysWithValues: visible.compactMap { session in
                session.latestResultText.map { (session.id, $0) }
            }
        )

        guard isPrimed else {
            isPrimed = true
            previousAttentionIDs = attentionIDs
            previousInputBySessionID = inputBySessionID
            previousResultBySessionID = resultBySessionID
            for (sessionID, result) in resultBySessionID {
                playedCompletionKeys.insert("\(sessionID)\u{1F}\(result)")
            }
            return nil
        }

        let needsAttention = !attentionIDs
            .subtracting(previousAttentionIDs)
            .intersection(soundable)
            .isEmpty
        let startedProcessing = inputBySessionID.contains { sessionID, input in
            soundable.contains(sessionID) && previousInputBySessionID[sessionID] != input
        }
        var completed = false
        for (sessionID, result) in resultBySessionID {
            guard previousResultBySessionID[sessionID] != result else { continue }
            let completionKey = "\(sessionID)\u{1F}\(result)"
            // Record the key even when suppressed, so the transition is not
            // replayed later.
            let isNew = playedCompletionKeys.insert(completionKey).inserted
            if isNew, soundable.contains(sessionID) {
                completed = true
            }
        }

        previousAttentionIDs = attentionIDs
        previousInputBySessionID = inputBySessionID
        previousResultBySessionID = resultBySessionID

        if needsAttention { return .attentionRequired }
        if completed { return .taskCompleted }
        if startedProcessing { return .processingStarted }
        return nil
    }
}

@MainActor
final class ATMAgentSoundPlayer {
    static let shared = ATMAgentSoundPlayer()

    private var activeSound: NSSound?

    private init() {}

    func play(_ event: ATMAgentSoundEvent) {
        guard ATMAgentSoundPreferences.isEnabled(for: event) else { return }
        preview(
            ATMAgentSoundPreferences.sound(for: event),
            volume: ATMAgentSoundPreferences.volume()
        )
    }

    func preview(_ sound: ATMAgentSound, volume: Float) {
        let value: NSSound?
        if let resourceURL = sound.bundledResourceURL {
            value = NSSound(contentsOf: resourceURL, byReference: false)
        } else if let soundName = sound.systemSoundName {
            value = NSSound(named: soundName)
        } else {
            value = nil
        }
        guard let value else { return }
        activeSound?.stop()
        value.volume = min(max(volume, 0), 1)
        activeSound = value.play() ? value : nil
    }
}
