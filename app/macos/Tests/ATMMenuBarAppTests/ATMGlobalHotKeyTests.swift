import AppKit
import Carbon.HIToolbox
import XCTest
@testable import ATMMenuBarApp

/// The global shortcut is registered by key code through Carbon, so nothing about
/// it is visible until someone presses keys in another app. The two places it can
/// break silently are serialization — a preference that reads back as a different
/// combination, or as none — and the label, which is the only way a person can
/// tell what is currently bound.
final class ATMGlobalHotKeyTests: XCTestCase {
    func testStorageValueRoundTripsEveryModifier() {
        let hotKey = ATMHotKey(
            keyCode: UInt16(kVK_ANSI_K),
            modifiers: [.command, .control, .option, .shift]
        )

        let restored = ATMHotKey(storageValue: hotKey.storageValue)

        XCTAssertEqual(restored, hotKey)
        XCTAssertEqual(restored?.displayString, "⌃⌥⇧⌘K")
    }

    /// Modifier flags carry device-dependent bits and Caps Lock / numeric-pad
    /// state that Carbon cannot register. Two values that differ only in those
    /// bits have to compare equal, or every read of the preference would look like
    /// a change and re-register the hot key.
    func testUnsupportedModifierBitsAreDropped() {
        let noisy = ATMHotKey(
            keyCode: UInt16(kVK_Space),
            modifiers: [.command, .capsLock, .numericPad, .function]
        )

        XCTAssertEqual(noisy, ATMHotKey(keyCode: UInt16(kVK_Space), modifiers: .command))
        XCTAssertEqual(noisy.displayString, "⌘Space")
    }

    func testMalformedStorageValueIsRejectedRatherThanGuessed() {
        for raw in ["", "⌘A", "1048576", "1048576:", ":0", "abc:0", "1048576:0:2", "-1:0"] {
            XCTAssertNil(ATMHotKey(storageValue: raw), "\(raw) 不应解析成快捷键")
        }
    }

    func testShortcutWithoutCommandControlOrOptionIsInvalid() {
        XCTAssertFalse(ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: []).isValid)
        XCTAssertFalse(ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: .shift).isValid)
        XCTAssertTrue(ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: .command).isValid)
        XCTAssertTrue(ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: .control).isValid)
        XCTAssertTrue(ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: .option).isValid)
    }

    func testCarbonModifiersMapEachFlagToItsOwnBit() {
        XCTAssertEqual(ATMHotKey(keyCode: 0, modifiers: .command).carbonModifiers, UInt32(cmdKey))
        XCTAssertEqual(ATMHotKey(keyCode: 0, modifiers: .option).carbonModifiers, UInt32(optionKey))
        XCTAssertEqual(ATMHotKey(keyCode: 0, modifiers: .control).carbonModifiers, UInt32(controlKey))
        XCTAssertEqual(ATMHotKey(keyCode: 0, modifiers: .shift).carbonModifiers, UInt32(shiftKey))
        XCTAssertEqual(
            ATMHotKey(keyCode: 0, modifiers: [.command, .option]).carbonModifiers,
            UInt32(cmdKey) | UInt32(optionKey)
        )
        XCTAssertEqual(ATMHotKey(keyCode: 0, modifiers: []).carbonModifiers, 0)
    }

    /// An unnamed key code must still render as something a person can act on:
    /// non-ANSI layouts and extra keys are not in the table.
    func testUnknownKeyCodeFallsBackToItsNumber() {
        XCTAssertEqual(ATMHotKey(keyCode: 250, modifiers: .command).displayString, "⌘Key 250")
    }

    func testDefaultShortcutIsRegistrableAndSurvivesStorage() {
        let fallback = ATMGlobalHotKeyPreferences.defaultHotKey

        XCTAssertTrue(fallback.isValid)
        XCTAssertEqual(fallback.displayString, "⌥⌘A")
        XCTAssertEqual(ATMHotKey(storageValue: fallback.storageValue), fallback)
    }

    /// A corrupted or shortcut-less preference has to resolve to the default
    /// rather than to nothing: losing the shortcut is indistinguishable from the
    /// feature being broken.
    func testResolvedPreferenceFallsBackToTheDefault() {
        let defaults = UserDefaults.standard
        let previous = defaults.string(forKey: ATMGlobalHotKeyPreferences.hotKeyKey)
        defer {
            if let previous {
                defaults.set(previous, forKey: ATMGlobalHotKeyPreferences.hotKeyKey)
            } else {
                defaults.removeObject(forKey: ATMGlobalHotKeyPreferences.hotKeyKey)
            }
        }

        defaults.set("not-a-hotkey", forKey: ATMGlobalHotKeyPreferences.hotKeyKey)
        XCTAssertEqual(ATMGlobalHotKeyPreferences.hotKey, ATMGlobalHotKeyPreferences.defaultHotKey)

        // Stored, but missing the modifier Carbon requires.
        defaults.set(
            ATMHotKey(keyCode: UInt16(kVK_ANSI_A), modifiers: .shift).storageValue,
            forKey: ATMGlobalHotKeyPreferences.hotKeyKey
        )
        XCTAssertEqual(ATMGlobalHotKeyPreferences.hotKey, ATMGlobalHotKeyPreferences.defaultHotKey)

        let custom = ATMHotKey(keyCode: UInt16(kVK_ANSI_J), modifiers: [.control, .option])
        defaults.set(custom.storageValue, forKey: ATMGlobalHotKeyPreferences.hotKeyKey)
        XCTAssertEqual(ATMGlobalHotKeyPreferences.hotKey, custom)
    }

    /// The shortcut opens the main window unless someone asked for the quick
    /// panel: "quickly open ATM" means the window the work happens in, and an
    /// unset or stale target must not silently mean the 340pt panel.
    func testTargetDefaultsToTheMainWindow() {
        let defaults = UserDefaults.standard
        let previous = defaults.string(forKey: ATMGlobalHotKeyPreferences.targetKey)
        defer {
            if let previous {
                defaults.set(previous, forKey: ATMGlobalHotKeyPreferences.targetKey)
            } else {
                defaults.removeObject(forKey: ATMGlobalHotKeyPreferences.targetKey)
            }
        }

        XCTAssertEqual(ATMGlobalHotKeyPreferences.defaultTarget, .desktop)

        defaults.removeObject(forKey: ATMGlobalHotKeyPreferences.targetKey)
        XCTAssertEqual(ATMGlobalHotKeyPreferences.target, .desktop)

        defaults.set("panel-from-a-future-build", forKey: ATMGlobalHotKeyPreferences.targetKey)
        XCTAssertEqual(ATMGlobalHotKeyPreferences.target, .desktop)

        defaults.set(ATMGlobalHotKeyTarget.quickPanel.rawValue, forKey: ATMGlobalHotKeyPreferences.targetKey)
        XCTAssertEqual(ATMGlobalHotKeyPreferences.target, .quickPanel)
    }
}
