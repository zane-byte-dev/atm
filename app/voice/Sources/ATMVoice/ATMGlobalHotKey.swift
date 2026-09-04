import AppKit
import Carbon.HIToolbox

/// Four-char code identifying ATM's hot keys inside the process-wide Carbon hot
/// key table, so the shared event handler ignores registrations made by anyone
/// else linked into this process.
private let atmHotKeySignature = OSType(0x41_54_4D_4B) // 'ATMK'

/// Which of ATM's global shortcuts a Carbon registration belongs to.
///
/// The raw values are the ids Carbon hands back in the event, so they are part of
/// the wire format between `register` and the C callback: stable, distinct, and
/// never reused for a different meaning.
enum ATMHotKeyAction: UInt32, CaseIterable {
    /// Hold to dictate; releasing writes the transcript into the focused app.
    case voiceInput = 2
    /// Bare ⎋, held only while dictation is running. See `registerTransient`.
    case cancelVoice = 3
}

/// A global shortcut: one virtual key code plus its modifier mask.
///
/// Kept as a plain value type — free of AppKit windows and Carbon refs — because
/// the parts that are easy to get wrong are parsing what came back from
/// UserDefaults and rendering the combination a person can recognise, and both
/// need to be testable without a running status item.
struct ATMHotKey: Equatable {
    /// Shift is recordable but never sufficient on its own: ⇧ plus a letter is
    /// just typing a capital, and stealing it globally would break text entry
    /// everywhere.
    static let supportedModifiers: NSEvent.ModifierFlags = [.command, .control, .option, .shift]

    let keyCode: UInt16
    let modifiers: NSEvent.ModifierFlags

    init(keyCode: UInt16, modifiers: NSEvent.ModifierFlags) {
        self.keyCode = keyCode
        self.modifiers = modifiers.intersection(Self.supportedModifiers)
    }

    /// A combination macOS will actually let us hold globally. Anything without
    /// ⌘, ⌃ or ⌥ collides with plain typing.
    var isValid: Bool {
        !modifiers.intersection([.command, .control, .option]).isEmpty
    }

    /// `<modifier raw value>:<key code>`. A single string so the setting fits the
    /// `@AppStorage` types the rest of the settings screen already uses; an
    /// unreadable value parses back to nil rather than to a wrong shortcut.
    var storageValue: String {
        "\(modifiers.rawValue):\(keyCode)"
    }

    init?(storageValue: String) {
        let parts = storageValue.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2,
              let rawModifiers = UInt(parts[0]),
              let keyCode = UInt16(parts[1]) else {
            return nil
        }
        self.init(keyCode: keyCode, modifiers: NSEvent.ModifierFlags(rawValue: rawModifiers))
    }

    /// Modifier symbols in the order macOS prints them (⌃⌥⇧⌘), then the key, so
    /// the setting reads the same way a menu item would.
    var displayString: String {
        var text = ""
        if modifiers.contains(.control) { text += "⌃" }
        if modifiers.contains(.option) { text += "⌥" }
        if modifiers.contains(.shift) { text += "⇧" }
        if modifiers.contains(.command) { text += "⌘" }
        return text + Self.keyLabel(for: keyCode)
    }

    var carbonModifiers: UInt32 {
        var value: UInt32 = 0
        if modifiers.contains(.command) { value |= UInt32(cmdKey) }
        if modifiers.contains(.option) { value |= UInt32(optionKey) }
        if modifiers.contains(.control) { value |= UInt32(controlKey) }
        if modifiers.contains(.shift) { value |= UInt32(shiftKey) }
        return value
    }

    /// Names the physical key. Deliberately a fixed table rather than the event's
    /// characters: the shortcut is registered by key code, so the label has to
    /// stay stable while a dead key, a non-Latin input source or a ⌥-modified
    /// character would each report something different.
    static func keyLabel(for keyCode: UInt16) -> String {
        keyLabels[keyCode] ?? "Key \(keyCode)"
    }

    private static let keyLabels: [UInt16: String] = Dictionary(
        uniqueKeysWithValues: [
            (kVK_ANSI_A, "A"), (kVK_ANSI_B, "B"), (kVK_ANSI_C, "C"), (kVK_ANSI_D, "D"),
            (kVK_ANSI_E, "E"), (kVK_ANSI_F, "F"), (kVK_ANSI_G, "G"), (kVK_ANSI_H, "H"),
            (kVK_ANSI_I, "I"), (kVK_ANSI_J, "J"), (kVK_ANSI_K, "K"), (kVK_ANSI_L, "L"),
            (kVK_ANSI_M, "M"), (kVK_ANSI_N, "N"), (kVK_ANSI_O, "O"), (kVK_ANSI_P, "P"),
            (kVK_ANSI_Q, "Q"), (kVK_ANSI_R, "R"), (kVK_ANSI_S, "S"), (kVK_ANSI_T, "T"),
            (kVK_ANSI_U, "U"), (kVK_ANSI_V, "V"), (kVK_ANSI_W, "W"), (kVK_ANSI_X, "X"),
            (kVK_ANSI_Y, "Y"), (kVK_ANSI_Z, "Z"),
            (kVK_ANSI_0, "0"), (kVK_ANSI_1, "1"), (kVK_ANSI_2, "2"), (kVK_ANSI_3, "3"),
            (kVK_ANSI_4, "4"), (kVK_ANSI_5, "5"), (kVK_ANSI_6, "6"), (kVK_ANSI_7, "7"),
            (kVK_ANSI_8, "8"), (kVK_ANSI_9, "9"),
            (kVK_ANSI_Minus, "-"), (kVK_ANSI_Equal, "="), (kVK_ANSI_LeftBracket, "["),
            (kVK_ANSI_RightBracket, "]"), (kVK_ANSI_Backslash, "\\"), (kVK_ANSI_Semicolon, ";"),
            (kVK_ANSI_Quote, "'"), (kVK_ANSI_Comma, ","), (kVK_ANSI_Period, "."),
            (kVK_ANSI_Slash, "/"), (kVK_ANSI_Grave, "`"),
            (kVK_Space, "Space"), (kVK_Return, "↩"), (kVK_Tab, "⇥"), (kVK_Escape, "⎋"),
            (kVK_Delete, "⌫"), (kVK_ForwardDelete, "⌦"),
            (kVK_LeftArrow, "←"), (kVK_RightArrow, "→"), (kVK_UpArrow, "↑"), (kVK_DownArrow, "↓"),
            (kVK_Home, "↖"), (kVK_End, "↘"), (kVK_PageUp, "⇞"), (kVK_PageDown, "⇟"),
            (kVK_F1, "F1"), (kVK_F2, "F2"), (kVK_F3, "F3"), (kVK_F4, "F4"),
            (kVK_F5, "F5"), (kVK_F6, "F6"), (kVK_F7, "F7"), (kVK_F8, "F8"),
            (kVK_F9, "F9"), (kVK_F10, "F10"), (kVK_F11, "F11"), (kVK_F12, "F12"),
        ].map { (UInt16($0.0), $0.1) }
    )
}

/// The storage behind one shortcut a person can bind: which UserDefaults keys hold
/// it, what it falls back to, and how to read it back.
///
/// Two shortcuts now share this — opening ATM and holding to dictate — and the part
/// worth writing once is the fallback rule, not the pair of key names.
struct ATMHotKeyBinding {
    let enabledKey: String
    let hotKeyKey: String
    let defaultEnabled: Bool
    let defaultHotKey: ATMHotKey

    var isEnabled: Bool {
        let defaults = UserDefaults.standard
        guard defaults.object(forKey: enabledKey) != nil else { return defaultEnabled }
        return defaults.bool(forKey: enabledKey)
    }

    /// Falls back to the default rather than to "no shortcut": a value we cannot
    /// parse means a corrupted preference, and silently losing the shortcut looks
    /// exactly like the feature being broken.
    var hotKey: ATMHotKey {
        guard let raw = UserDefaults.standard.string(forKey: hotKeyKey),
              let hotKey = ATMHotKey(storageValue: raw),
              hotKey.isValid else {
            return defaultHotKey
        }
        return hotKey
    }
}

extension ATMHotKeyAction {
    /// The preference this action reads, or nil when nothing about it is bindable.
    ///
    /// `cancelVoice` has none on purpose: it is ⎋, ATM owns it, and it only exists
    /// while dictation is running — see `ATMGlobalHotKeyManager.registerTransient`.
    var binding: ATMHotKeyBinding? {
        switch self {
        case .voiceInput: return ATMVoiceInputPreferences.hotKeyBinding
        case .cancelVoice: return nil
        }
    }
}

/// Holds ATM's system-wide shortcuts.
///
/// Uses Carbon's `RegisterEventHotKey` rather than an `NSEvent` global monitor:
/// the monitor form of keyboard events requires Input Monitoring permission and
/// only observes — it cannot stop the keystroke from also reaching whatever app
/// is in front. A registered hot key needs no permission prompt and is consumed
/// by us, which is what a launcher shortcut has to do.
///
/// One manager rather than one per shortcut: the Carbon event handler is installed
/// against the process-wide dispatcher target and filters by our signature, so a
/// second manager would see the first one's events and both would have to agree on
/// who owns which id. Keeping the id table in one place removes that question.
@MainActor
final class ATMGlobalHotKeyManager: ObservableObject {
    static let shared = ATMGlobalHotKeyManager()

    /// Surfaced so the settings screen can tell "off" apart from "you picked a
    /// combination something else already owns" — a failed registration is
    /// otherwise indistinguishable from the shortcut simply not working.
    enum Registration: Equatable {
        case off
        case active(ATMHotKey)
        case unavailable(ATMHotKey)
    }

    @Published private(set) var registrations: [ATMHotKeyAction: Registration] = [:]

    /// Set by `StatusBarController`; the manager knows nothing about panels or
    /// microphones. `onReleased` only fires for shortcuts whose whole point is how
    /// long you hold them — the launcher ignores it.
    var onPressed: ((ATMHotKeyAction) -> Void)?
    var onReleased: ((ATMHotKeyAction) -> Void)?

    private var refs: [ATMHotKeyAction: EventHotKeyRef] = [:]
    private var eventHandler: EventHandlerRef?
    private var defaultsObserver: NSObjectProtocol?

    func registration(for action: ATMHotKeyAction) -> Registration {
        registrations[action] ?? .off
    }

    func start() {
        installEventHandler()
        apply()
        // The setting is written through @AppStorage, which does not notify
        // anyone but SwiftUI. UserDefaults' own change notification is the only
        // signal that reaches back here.
        defaultsObserver = NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: UserDefaults.standard,
            queue: .main
        ) { _ in
            Task { @MainActor in ATMGlobalHotKeyManager.shared.apply() }
        }
    }

    func stop() {
        if let defaultsObserver {
            NotificationCenter.default.removeObserver(defaultsObserver)
            self.defaultsObserver = nil
        }
        for action in ATMHotKeyAction.allCases {
            unregister(action)
        }
        registrations = [:]
        if let eventHandler {
            RemoveEventHandler(eventHandler)
            self.eventHandler = nil
        }
    }

    /// Brings the live registrations in line with the stored preferences. Safe to
    /// call on every UserDefaults change: an unchanged shortcut is left alone, and
    /// a combination that already failed is not retried until it changes, so a
    /// conflict cannot turn into a registration loop.
    func apply() {
        for action in ATMHotKeyAction.allCases {
            guard let binding = action.binding else { continue }
            guard binding.isEnabled else {
                unregister(action)
                registrations[action] = .off
                continue
            }
            let desired = binding.hotKey
            let current = registration(for: action)
            // Already settled, either way. Re-registering an unchanged shortcut
            // would churn on every unrelated preference write, and retrying one
            // that a different app owns would loop; turning the setting off and on
            // again is the deliberate way to retry.
            if current == .active(desired) || current == .unavailable(desired) {
                continue
            }
            register(action, desired)
        }
    }

    /// Takes a shortcut ATM owns outright for as long as something is running,
    /// bypassing both the preference table and `ATMHotKey.isValid`.
    ///
    /// `isValid` demands ⌘, ⌃ or ⌥ because a bare letter bound forever would fight
    /// ordinary typing. Bare ⎋ held only while dictation is recording is the case
    /// that rule exists to allow: "press ⎋ to cancel" has to be true from the very
    /// first recording, and the alternative — `NSEvent.addGlobalMonitorForEvents` —
    /// needs Accessibility permission, so it would be a lie exactly when someone is
    /// still staring at the permission prompt.
    func registerTransient(_ action: ATMHotKeyAction, hotKey: ATMHotKey) {
        register(action, hotKey, requireModifiers: false)
    }

    func unregisterTransient(_ action: ATMHotKeyAction) {
        unregister(action)
        registrations[action] = .off
    }

    private func register(_ action: ATMHotKeyAction, _ hotKey: ATMHotKey, requireModifiers: Bool = true) {
        unregister(action)
        if requireModifiers, !hotKey.isValid {
            registrations[action] = .off
            return
        }
        var ref: EventHotKeyRef?
        let identifier = EventHotKeyID(signature: atmHotKeySignature, id: action.rawValue)
        let status = RegisterEventHotKey(
            UInt32(hotKey.keyCode),
            hotKey.carbonModifiers,
            identifier,
            GetEventDispatcherTarget(),
            0,
            &ref
        )
        guard status == noErr, let ref else {
            // Almost always `eventHotKeyExistsErr`: another app holds it.
            registrations[action] = .unavailable(hotKey)
            ATMLog.failure(
                "global_hotkey_register_failed",
                error: "OSStatus \(status)",
                fields: ["hotkey": hotKey.displayString, "action": String(describing: action)]
            )
            return
        }
        refs[action] = ref
        registrations[action] = .active(hotKey)
    }

    private func unregister(_ action: ATMHotKeyAction) {
        guard let ref = refs.removeValue(forKey: action) else { return }
        UnregisterEventHotKey(ref)
    }

    /// Both kinds, in one handler. Release is what makes hold-to-dictate possible:
    /// Carbon reports it for the same registration, so nothing has to watch the
    /// keyboard to notice a key coming back up.
    private func installEventHandler() {
        guard eventHandler == nil else { return }
        var specs = [
            EventTypeSpec(
                eventClass: OSType(kEventClassKeyboard),
                eventKind: UInt32(kEventHotKeyPressed)
            ),
            EventTypeSpec(
                eventClass: OSType(kEventClassKeyboard),
                eventKind: UInt32(kEventHotKeyReleased)
            ),
        ]
        let status = InstallEventHandler(
            GetEventDispatcherTarget(),
            atmGlobalHotKeyEventHandler,
            specs.count,
            &specs,
            nil,
            &eventHandler
        )
        if status != noErr {
            ATMLog.failure("global_hotkey_handler_install_failed", error: "OSStatus \(status)")
        }
    }

    fileprivate func handle(action: ATMHotKeyAction, isPressed: Bool) {
        if isPressed {
            onPressed?(action)
        } else {
            onReleased?(action)
        }
    }
}

/// Carbon calls back through a bare C function pointer, so it cannot capture the
/// manager. Every hot key under our signature belongs to the shared manager, so
/// resolving it is unambiguous; the id says which shortcut and the event kind says
/// whether the key went down or came back up.
private func atmGlobalHotKeyEventHandler(
    _ callRef: EventHandlerCallRef?,
    _ event: EventRef?,
    _ userData: UnsafeMutableRawPointer?
) -> OSStatus {
    guard let event else { return OSStatus(eventNotHandledErr) }
    var identifier = EventHotKeyID()
    let status = GetEventParameter(
        event,
        EventParamName(kEventParamDirectObject),
        EventParamType(typeEventHotKeyID),
        nil,
        MemoryLayout<EventHotKeyID>.size,
        nil,
        &identifier
    )
    guard status == noErr,
          identifier.signature == atmHotKeySignature,
          let action = ATMHotKeyAction(rawValue: identifier.id) else {
        return OSStatus(eventNotHandledErr)
    }
    let kind = GetEventKind(event)
    guard kind == UInt32(kEventHotKeyPressed) || kind == UInt32(kEventHotKeyReleased) else {
        return OSStatus(eventNotHandledErr)
    }
    let isPressed = kind == UInt32(kEventHotKeyPressed)
    Task { @MainActor in
        ATMGlobalHotKeyManager.shared.handle(action: action, isPressed: isPressed)
    }
    return noErr
}
