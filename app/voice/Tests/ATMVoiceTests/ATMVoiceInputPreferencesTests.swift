import AppKit
import Carbon.HIToolbox
import XCTest
@testable import ATMVoice

/// Dictation reads its settings on every key-down, from UserDefaults, by string key.
/// The failures that would be invisible are a stale or garbled value resolving to
/// something plausible-but-wrong — listening in the wrong language, or losing the
/// shortcut entirely — and the two engines silently disagreeing about which language
/// was chosen.
final class ATMVoiceInputPreferencesTests: XCTestCase {
    private var restore: [String: Any?] = [:]

    override func setUp() {
        super.setUp()
        let defaults = UserDefaults.standard
        for key in [
            ATMVoiceInputPreferences.engineKey,
            ATMVoiceInputPreferences.languageKey,
            ATMVoiceInputPreferences.hotKeyKey,
            ATMVoiceInputPreferences.hotKeyEnabledKey,
            ATMVoiceInputPreferences.dictionaryKey,
        ] {
            restore[key] = defaults.object(forKey: key)
            defaults.removeObject(forKey: key)
        }
    }

    override func tearDown() {
        let defaults = UserDefaults.standard
        for (key, value) in restore {
            if let value {
                defaults.set(value, forKey: key)
            } else {
                defaults.removeObject(forKey: key)
            }
        }
        restore = [:]
        super.tearDown()
    }

    // MARK: - Engine

    /// SenseVoice by default even though its model is not downloaded yet: the router
    /// falls back to Apple Speech until it is, so this default costs nothing and means
    /// dictation gets better the moment someone downloads the model.
    func testEngineDefaultsToSenseVoice() {
        XCTAssertEqual(ATMVoiceInputPreferences.defaultEngine, .senseVoice)
        XCTAssertEqual(ATMVoiceInputPreferences.engine, .senseVoice)
    }

    func testUnknownEngineFallsBackRatherThanBeingGuessed() {
        UserDefaults.standard.set("whisper-from-a-future-build", forKey: ATMVoiceInputPreferences.engineKey)
        XCTAssertEqual(ATMVoiceInputPreferences.engine, .senseVoice)

        UserDefaults.standard.set(
            ATMVoiceRecognitionEngine.appleSpeech.rawValue,
            forKey: ATMVoiceInputPreferences.engineKey
        )
        XCTAssertEqual(ATMVoiceInputPreferences.engine, .appleSpeech)
    }

    // MARK: - Language

    func testLanguageDefaultsToAutoAndRejectsJunk() {
        XCTAssertEqual(ATMVoiceInputPreferences.language, .auto)

        UserDefaults.standard.set("kl-GL", forKey: ATMVoiceInputPreferences.languageKey)
        XCTAssertEqual(ATMVoiceInputPreferences.language, .auto)

        UserDefaults.standard.set(
            ATMVoiceInputLanguage.cantonese.rawValue,
            forKey: ATMVoiceInputPreferences.languageKey
        )
        XCTAssertEqual(ATMVoiceInputPreferences.language, .cantonese)
    }

    /// One picker drives two engines. If a case gained an Apple locale without a
    /// SenseVoice code (or the reverse), the setting would say 中文 while one engine
    /// listened for something else — and nothing on screen would show it.
    func testEveryLanguageMapsToBothEngines() {
        for language in ATMVoiceInputLanguage.allCases {
            XCTAssertFalse(language.senseVoiceCode.isEmpty, "\(language) 缺少 SenseVoice 语言码")
            XCTAssertFalse(language.label.isEmpty, "\(language) 缺少界面名")

            switch language {
            case .auto:
                XCTAssertEqual(language.senseVoiceCode, "auto")
                XCTAssertEqual(language.locale.identifier, Locale.current.identifier)
            default:
                // The raw value is the Apple locale identifier, so a case whose locale
                // does not round-trip is a typo in the enum.
                XCTAssertEqual(language.locale.identifier, language.rawValue, "\(language) 的 Locale 不一致")
                XCTAssertNotEqual(language.senseVoiceCode, "auto", "\(language) 不该退回 auto")
            }
        }
    }

    func testSenseVoiceCodesAreDistinctPerLanguage() {
        let codes = ATMVoiceInputLanguage.allCases.map(\.senseVoiceCode)
        XCTAssertEqual(codes.count, Set(codes).count, "两种语言映射到了同一个 SenseVoice 码")
    }

    // MARK: - Hot key

    /// ⌥Space is what this feature is built around: held for the length of a sentence,
    /// so it needs a modifier that is not doing anything else while held down.
    func testDefaultHotKeyIsOptionSpaceAndIsRegistrable() {
        let hotKey = ATMVoiceInputPreferences.defaultHotKey

        XCTAssertEqual(hotKey.displayString, "⌥Space")
        XCTAssertTrue(hotKey.isValid)
        XCTAssertEqual(ATMHotKey(storageValue: hotKey.storageValue), hotKey)
    }

    func testCorruptedHotKeyFallsBackToTheDefault() {
        UserDefaults.standard.set("not-a-hotkey", forKey: ATMVoiceInputPreferences.hotKeyKey)
        XCTAssertEqual(ATMVoiceInputPreferences.hotKey, ATMVoiceInputPreferences.defaultHotKey)

        // Parses, but Carbon cannot register it.
        UserDefaults.standard.set(
            ATMHotKey(keyCode: UInt16(kVK_Space), modifiers: .shift).storageValue,
            forKey: ATMVoiceInputPreferences.hotKeyKey
        )
        XCTAssertEqual(ATMVoiceInputPreferences.hotKey, ATMVoiceInputPreferences.defaultHotKey)
    }

    /// The two shortcuts must not share storage. Rebinding dictation while the launcher
    /// silently changed too would be a bug nobody would think to look for here.
    func testVoiceShortcutUsesOnlyVoiceStorage() {
        XCTAssertNotEqual(ATMVoiceInputPreferences.hotKeyKey, "ATMGlobalHotKey")
        XCTAssertNotEqual(ATMVoiceInputPreferences.hotKeyEnabledKey, "ATMGlobalHotKeyEnabled")

        let custom = ATMHotKey(keyCode: UInt16(kVK_ANSI_J), modifiers: [.control, .option])
        UserDefaults.standard.set(custom.storageValue, forKey: ATMVoiceInputPreferences.hotKeyKey)

        XCTAssertEqual(ATMVoiceInputPreferences.hotKey, custom)
    }

    /// ⎋ deliberately fails `isValid` — a bare key would fight ordinary typing if it
    /// were bindable. It is only ever held while a recording is running, through
    /// `registerTransient`, which is the one path allowed to skip that rule.
    func testCancelHotKeyIsBareEscapeAndIsNotBindable() {
        let cancel = ATMVoiceInputPreferences.cancelHotKey

        XCTAssertEqual(cancel.keyCode, UInt16(kVK_Escape))
        XCTAssertTrue(cancel.modifiers.isEmpty)
        XCTAssertFalse(cancel.isValid)
    }

    // MARK: - Wiring

    /// Both sides of every rewrite are handed to Apple's recognizer as context: the
    /// recognizer has to produce something recognisable before a rewrite can fire.
    func testContextualTermsIncludeBothSidesOfEveryRule() {
        UserDefaults.standard.set("阿童木 => ATM\n派 => Pi", forKey: ATMVoiceInputPreferences.dictionaryKey)

        XCTAssertEqual(ATMVoiceInputPreferences.contextualTerms, ["阿童木", "ATM", "派", "Pi"])
    }

    func testMissingDictionaryYieldsNoRulesRatherThanCrashing() {
        UserDefaults.standard.removeObject(forKey: ATMVoiceInputPreferences.dictionaryKey)

        XCTAssertTrue(ATMVoiceInputPreferences.replacements.isEmpty)
        XCTAssertTrue(ATMVoiceInputPreferences.contextualTerms.isEmpty)
    }
}
