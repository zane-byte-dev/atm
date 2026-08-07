import AppKit
import Carbon.HIToolbox

/// Four-char code identifying ATM's hot keys inside the process-wide Carbon hot
/// key table, so the shared event handler ignores registrations made by anyone
/// else linked into this process.
private let atmHotKeySignature = OSType(0x41_54_4D_4B) // 'ATMK'
private let atmGlobalHotKeyID: UInt32 = 1

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

/// What the global shortcut brings up. The main window is the default: "quickly
/// open ATM" means the app people actually work in, and the quick panel is a
/// glance at five numbers — reasonable to bind, but not what someone reaching for
/// a shortcut usually wants.
enum ATMGlobalHotKeyTarget: String, CaseIterable, Identifiable {
    case desktop
    case quickPanel

    var id: String { rawValue }

    var label: String {
        switch self {
        case .desktop: return "主窗口"
        case .quickPanel: return "快速面板"
        }
    }
}

/// Where the global shortcut lives. Same shape as `ATMAgentNotchPreferences`: raw
/// UserDefaults keys the settings screen binds to with `@AppStorage`, plus
/// resolved accessors for the non-SwiftUI callers.
enum ATMGlobalHotKeyPreferences {
    static let enabledKey = "ATMGlobalHotKeyEnabled"
    static let hotKeyKey = "ATMGlobalHotKey"
    static let targetKey = "ATMGlobalHotKeyTarget"
    static let defaultEnabled = true
    static let defaultTarget = ATMGlobalHotKeyTarget.desktop

    /// ⌥⌘A: unassigned by macOS, and the letter matches the app rather than a
    /// position on the keyboard. Anyone whose muscle memory disagrees can change
    /// it, which is the point of the setting.
    static let defaultHotKey = ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: [.command, .option])

    static var isEnabled: Bool {
        let defaults = UserDefaults.standard
        guard defaults.object(forKey: enabledKey) != nil else { return defaultEnabled }
        return defaults.bool(forKey: enabledKey)
    }

    static var target: ATMGlobalHotKeyTarget {
        guard let raw = UserDefaults.standard.string(forKey: targetKey),
              let target = ATMGlobalHotKeyTarget(rawValue: raw) else {
            return defaultTarget
        }
        return target
    }

    /// Falls back to the default rather than to "no shortcut": a value we cannot
    /// parse means a corrupted preference, and silently losing the shortcut looks
    /// exactly like the feature being broken.
    static var hotKey: ATMHotKey {
        guard let raw = UserDefaults.standard.string(forKey: hotKeyKey),
              let hotKey = ATMHotKey(storageValue: raw),
              hotKey.isValid else {
            return defaultHotKey
        }
        return hotKey
    }
}

/// Holds the system-wide shortcut that brings ATM to the front.
///
/// Uses Carbon's `RegisterEventHotKey` rather than an `NSEvent` global monitor:
/// the monitor form of keyboard events requires Input Monitoring permission and
/// only observes — it cannot stop the keystroke from also reaching whatever app
/// is in front. A registered hot key needs no permission prompt and is consumed
/// by us, which is what a launcher shortcut has to do.
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

    @Published private(set) var registration: Registration = .off

    /// Set by `StatusBarController`; the manager knows nothing about panels.
    var onTrigger: (() -> Void)?

    private var hotKeyRef: EventHotKeyRef?
    private var eventHandler: EventHandlerRef?
    private var defaultsObserver: NSObjectProtocol?

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
        unregister()
        if let eventHandler {
            RemoveEventHandler(eventHandler)
            self.eventHandler = nil
        }
    }

    /// Brings the live registration in line with the stored preference. Safe to
    /// call on every UserDefaults change: an unchanged shortcut is left alone, and
    /// a combination that already failed is not retried until it changes, so a
    /// conflict cannot turn into a registration loop.
    func apply() {
        guard ATMGlobalHotKeyPreferences.isEnabled else {
            unregister()
            registration = .off
            return
        }
        let desired = ATMGlobalHotKeyPreferences.hotKey
        // Already settled, either way. Re-registering an unchanged shortcut would
        // churn on every unrelated preference write, and retrying one that a
        // different app owns would loop; turning the setting off and on again is
        // the deliberate way to retry.
        if registration == .active(desired) || registration == .unavailable(desired) {
            return
        }
        register(desired)
    }

    private func register(_ hotKey: ATMHotKey) {
        unregister()
        guard hotKey.isValid else {
            registration = .off
            return
        }
        var ref: EventHotKeyRef?
        let identifier = EventHotKeyID(signature: atmHotKeySignature, id: atmGlobalHotKeyID)
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
            registration = .unavailable(hotKey)
            ATMLog.failure(
                "global_hotkey_register_failed",
                error: "OSStatus \(status)",
                fields: ["hotkey": hotKey.displayString]
            )
            return
        }
        hotKeyRef = ref
        registration = .active(hotKey)
    }

    private func unregister() {
        guard let hotKeyRef else { return }
        UnregisterEventHotKey(hotKeyRef)
        self.hotKeyRef = nil
    }

    private func installEventHandler() {
        guard eventHandler == nil else { return }
        var spec = EventTypeSpec(
            eventClass: OSType(kEventClassKeyboard),
            eventKind: UInt32(kEventHotKeyPressed)
        )
        let status = InstallEventHandler(
            GetEventDispatcherTarget(),
            atmGlobalHotKeyEventHandler,
            1,
            &spec,
            nil,
            &eventHandler
        )
        if status != noErr {
            ATMLog.failure("global_hotkey_handler_install_failed", error: "OSStatus \(status)")
        }
    }

    fileprivate func handleHotKeyPressed() {
        onTrigger?()
    }
}

/// Carbon calls back through a bare C function pointer, so it cannot capture the
/// manager. Only one hot key is registered under our signature, so resolving the
/// shared manager is unambiguous.
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
          identifier.id == atmGlobalHotKeyID else {
        return OSStatus(eventNotHandledErr)
    }
    Task { @MainActor in ATMGlobalHotKeyManager.shared.handleHotKeyPressed() }
    return noErr
}
