import Carbon.HIToolbox
import XCTest
@testable import ATMMenuBarApp

/// Two shortcuts now share one Carbon event handler, and the id in the event is the
/// only thing that says which one fired. Nothing about that is visible until someone
/// presses keys in another app, so the parts that can be checked here are the id table
/// and the preference wiring behind it.
final class ATMVoiceHotKeyRegistrationTests: XCTestCase {
    /// The raw values travel through Carbon and come back in the event, so they are a
    /// wire format: two actions sharing one would route dictation's key-down to the
    /// launcher.
    func testActionIdentifiersAreDistinct() {
        let ids = ATMHotKeyAction.allCases.map(\.rawValue)

        XCTAssertEqual(ids.count, Set(ids).count)
        XCTAssertEqual(ATMHotKeyAction.launcher.rawValue, 1)
        XCTAssertEqual(ATMHotKeyAction.voiceInput.rawValue, 2)
        XCTAssertEqual(ATMHotKeyAction.cancelVoice.rawValue, 3)
    }

    /// 0 is what an uninitialised `EventHotKeyID` carries, so no action may claim it —
    /// otherwise a malformed event would resolve to a real shortcut.
    func testNoActionUsesZero() {
        XCTAssertNil(ATMHotKeyAction(rawValue: 0))
    }

    /// Every bindable action must resolve to storage; the transient one must not, because
    /// `apply()` uses exactly that to decide what it reconciles from preferences.
    func testOnlyBindableActionsHaveStorage() {
        XCTAssertNotNil(ATMHotKeyAction.launcher.binding)
        XCTAssertNotNil(ATMHotKeyAction.voiceInput.binding)
        XCTAssertNil(ATMHotKeyAction.cancelVoice.binding)
    }

    func testEachBindableActionResolvesToItsOwnKeysAndDefault() {
        let launcher = ATMHotKeyAction.launcher.binding
        let voice = ATMHotKeyAction.voiceInput.binding

        XCTAssertEqual(launcher?.hotKeyKey, ATMGlobalHotKeyPreferences.hotKeyKey)
        XCTAssertEqual(launcher?.defaultHotKey, ATMGlobalHotKeyPreferences.defaultHotKey)
        XCTAssertEqual(voice?.hotKeyKey, ATMVoiceInputPreferences.hotKeyKey)
        XCTAssertEqual(voice?.defaultHotKey, ATMVoiceInputPreferences.defaultHotKey)
        XCTAssertNotEqual(launcher?.hotKeyKey, voice?.hotKeyKey)
    }

    /// Both default to on: a dictation shortcut nobody knows to enable is a feature
    /// nobody finds.
    func testBothShortcutsDefaultToEnabled() {
        XCTAssertTrue(ATMGlobalHotKeyPreferences.defaultEnabled)
        XCTAssertTrue(ATMVoiceInputPreferences.defaultEnabled)
    }

    /// The shared fallback rule, exercised through the binding rather than through the
    /// per-feature accessors: a corrupted value resolves to the default, because losing
    /// the shortcut looks exactly like the feature being broken.
    func testBindingFallsBackToDefaultOnUnreadableValue() {
        let defaults = UserDefaults.standard
        let key = "ATMHotKeyBindingTestKey"
        let previous = defaults.string(forKey: key)
        defer {
            if let previous { defaults.set(previous, forKey: key) } else { defaults.removeObject(forKey: key) }
        }

        let fallback = ATMHotKey(keyCode: UInt16(kVK_ANSI_K), modifiers: [.command, .option])
        let binding = ATMHotKeyBinding(
            enabledKey: "ATMHotKeyBindingTestEnabledKey",
            hotKeyKey: key,
            defaultEnabled: true,
            defaultHotKey: fallback
        )

        defaults.removeObject(forKey: key)
        XCTAssertEqual(binding.hotKey, fallback)

        defaults.set("garbage", forKey: key)
        XCTAssertEqual(binding.hotKey, fallback)

        // Parses but is not registrable: no modifier Carbon accepts.
        defaults.set(ATMHotKey(keyCode: UInt16(kVK_ANSI_K), modifiers: .shift).storageValue, forKey: key)
        XCTAssertEqual(binding.hotKey, fallback)

        let custom = ATMHotKey(keyCode: UInt16(kVK_ANSI_L), modifiers: .control)
        defaults.set(custom.storageValue, forKey: key)
        XCTAssertEqual(binding.hotKey, custom)
    }

    /// An absent value means "not configured yet", which for both shortcuts means on.
    /// `bool(forKey:)` alone answers false, so the binding has to check existence first.
    func testAbsentEnabledKeyMeansDefaultNotFalse() {
        let defaults = UserDefaults.standard
        let key = "ATMHotKeyBindingTestEnabledOnly"
        let previous = defaults.object(forKey: key)
        defer {
            if let previous { defaults.set(previous, forKey: key) } else { defaults.removeObject(forKey: key) }
        }

        let binding = ATMHotKeyBinding(
            enabledKey: key,
            hotKeyKey: "ATMHotKeyBindingTestEnabledOnlyHotKey",
            defaultEnabled: true,
            defaultHotKey: ATMVoiceInputPreferences.defaultHotKey
        )

        defaults.removeObject(forKey: key)
        XCTAssertTrue(binding.isEnabled)

        defaults.set(false, forKey: key)
        XCTAssertFalse(binding.isEnabled)
    }
}
