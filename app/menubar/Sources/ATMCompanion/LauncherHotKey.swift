import AppKit
import Carbon.HIToolbox

/// Only the Web launcher exists in this process. No voice or Escape registration.
@MainActor
final class LauncherHotKey {
    static let shared = LauncherHotKey()
    var onOpen: (() -> Void)?
    private var key: EventHotKeyRef?
    private var handler: EventHandlerRef?
    func start() -> Bool {
        stop()
        var event = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        let installed = InstallEventHandler(GetEventDispatcherTarget(), { _, event, _ in
            guard let event else { return OSStatus(eventNotHandledErr) }
            var id = EventHotKeyID()
            guard GetEventParameter(event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID), nil, MemoryLayout<EventHotKeyID>.size, nil, &id) == noErr,
                  id.signature == 0x41544D43, id.id == 1 else { return OSStatus(eventNotHandledErr) }
            Task { @MainActor in LauncherHotKey.shared.onOpen?() }
            return noErr
        }, 1, &event, nil, &handler)
        guard installed == noErr else { return false }
        let id = EventHotKeyID(signature: 0x41544D43, id: 1)
        let code = UserDefaults.standard.integer(forKey: "LauncherKeyCode")
        let modifiers = UserDefaults.standard.integer(forKey: "LauncherModifiers")
        let keyCode = code == 0 ? UInt32(kVK_ANSI_A) : UInt32(clamping: code)
        let allowed = UInt32(cmdKey | optionKey | controlKey | shiftKey)
        let flags = modifiers == 0 ? UInt32(cmdKey | optionKey) : UInt32(clamping: modifiers) & allowed
        guard flags & UInt32(cmdKey | optionKey | controlKey) != 0 else { return false }
        return RegisterEventHotKey(keyCode, flags, id, GetEventDispatcherTarget(), 0, &key) == noErr
    }
    func stop() {
        if let key { UnregisterEventHotKey(key); self.key = nil }
        if let handler { RemoveEventHandler(handler); self.handler = nil }
    }
}
