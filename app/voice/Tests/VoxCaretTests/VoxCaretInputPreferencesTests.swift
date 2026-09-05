import AppKit
import Carbon.HIToolbox
import XCTest
@testable import VoxCaret

/// Dictation reads its settings on every key-down, from UserDefaults, by string key.
/// The failures that would be invisible are a stale or garbled value resolving to
/// something plausible-but-wrong — listening in the wrong language, or losing the
/// shortcut entirely — and the two engines silently disagreeing about which language
/// was chosen.
final class VoxCaretInputPreferencesTests: XCTestCase {
    private var restore: [String: Any?] = [:]

    override func setUp() {
        super.setUp()
        let defaults = UserDefaults.standard
        for key in [
            VoxCaretInputPreferences.engineKey,
            VoxCaretInputPreferences.languageKey,
            VoxCaretInputPreferences.hotKeyKey,
            VoxCaretInputPreferences.hotKeyEnabledKey,
            VoxCaretInputPreferences.dictionaryKey,
            VoxCaretInputPreferences.liveInsertionEnabledKey,
            VoxCaretInputPreferences.rightCommandHoldEnabledKey,
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
        XCTAssertEqual(VoxCaretInputPreferences.defaultEngine, .senseVoice)
        XCTAssertEqual(VoxCaretInputPreferences.engine, .senseVoice)
    }

    func testLiveInsertionDefaultsOnAndCanBeDisabled() {
        XCTAssertTrue(VoxCaretInputPreferences.liveInsertionEnabled)
        UserDefaults.standard.set(false, forKey: VoxCaretInputPreferences.liveInsertionEnabledKey)
        XCTAssertFalse(VoxCaretInputPreferences.liveInsertionEnabled)
    }

    func testLiveInsertionAlwaysRoutesToAStreamingEngine() {
        XCTAssertFalse(
            VoxCaretTranscriberRouting.shouldUseSenseVoice(
                preferredEngine: .senseVoice,
                liveInsertionEnabled: true,
                senseVoiceModelReady: true
            )
        )
        XCTAssertTrue(
            VoxCaretTranscriberRouting.shouldUseSenseVoice(
                preferredEngine: .senseVoice,
                liveInsertionEnabled: false,
                senseVoiceModelReady: true
            )
        )
        XCTAssertFalse(
            VoxCaretTranscriberRouting.shouldUseSenseVoice(
                preferredEngine: .senseVoice,
                liveInsertionEnabled: false,
                senseVoiceModelReady: false
            )
        )
    }

    func testUnknownEngineFallsBackRatherThanBeingGuessed() {
        UserDefaults.standard.set("whisper-from-a-future-build", forKey: VoxCaretInputPreferences.engineKey)
        XCTAssertEqual(VoxCaretInputPreferences.engine, .senseVoice)

        UserDefaults.standard.set(
            VoxCaretRecognitionEngine.appleSpeech.rawValue,
            forKey: VoxCaretInputPreferences.engineKey
        )
        XCTAssertEqual(VoxCaretInputPreferences.engine, .appleSpeech)
    }

    // MARK: - Language

    func testLanguageDefaultsToAutoAndRejectsJunk() {
        XCTAssertEqual(VoxCaretInputPreferences.language, .auto)

        UserDefaults.standard.set("kl-GL", forKey: VoxCaretInputPreferences.languageKey)
        XCTAssertEqual(VoxCaretInputPreferences.language, .auto)

        UserDefaults.standard.set(
            VoxCaretInputLanguage.cantonese.rawValue,
            forKey: VoxCaretInputPreferences.languageKey
        )
        XCTAssertEqual(VoxCaretInputPreferences.language, .cantonese)
    }

    /// One picker drives two engines. If a case gained an Apple locale without a
    /// SenseVoice code (or the reverse), the setting would say 中文 while one engine
    /// listened for something else — and nothing on screen would show it.
    func testEveryLanguageMapsToBothEngines() {
        for language in VoxCaretInputLanguage.allCases {
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
        let codes = VoxCaretInputLanguage.allCases.map(\.senseVoiceCode)
        XCTAssertEqual(codes.count, Set(codes).count, "两种语言映射到了同一个 SenseVoice 码")
    }

    // MARK: - Hot key

    /// ⌥Space is what this feature is built around: held for the length of a sentence,
    /// so it needs a modifier that is not doing anything else while held down.
    func testDefaultHotKeyIsOptionSpaceAndIsRegistrable() {
        let hotKey = VoxCaretInputPreferences.defaultHotKey

        XCTAssertEqual(hotKey.displayString, "⌥Space")
        XCTAssertTrue(hotKey.isValid)
        XCTAssertEqual(VoxCaretHotKey(storageValue: hotKey.storageValue), hotKey)
    }

    func testRightCommandHoldDefaultsOnAndCanBeDisabled() {
        XCTAssertTrue(VoxCaretInputPreferences.rightCommandHoldEnabled)
        UserDefaults.standard.set(false, forKey: VoxCaretInputPreferences.rightCommandHoldEnabledKey)
        XCTAssertFalse(VoxCaretInputPreferences.rightCommandHoldEnabled)
    }

    func testCorruptedHotKeyFallsBackToTheDefault() {
        UserDefaults.standard.set("not-a-hotkey", forKey: VoxCaretInputPreferences.hotKeyKey)
        XCTAssertEqual(VoxCaretInputPreferences.hotKey, VoxCaretInputPreferences.defaultHotKey)

        // Parses, but Carbon cannot register it.
        UserDefaults.standard.set(
            VoxCaretHotKey(keyCode: UInt16(kVK_Space), modifiers: .shift).storageValue,
            forKey: VoxCaretInputPreferences.hotKeyKey
        )
        XCTAssertEqual(VoxCaretInputPreferences.hotKey, VoxCaretInputPreferences.defaultHotKey)
    }

    /// The two shortcuts must not share storage. Rebinding dictation while the launcher
    /// silently changed too would be a bug nobody would think to look for here.
    func testVoiceShortcutUsesOnlyVoiceStorage() {
        XCTAssertNotEqual(VoxCaretInputPreferences.hotKeyKey, "VoxCaretGlobalHotKey")
        XCTAssertNotEqual(VoxCaretInputPreferences.hotKeyEnabledKey, "VoxCaretGlobalHotKeyEnabled")

        let custom = VoxCaretHotKey(keyCode: UInt16(kVK_ANSI_J), modifiers: [.control, .option])
        UserDefaults.standard.set(custom.storageValue, forKey: VoxCaretInputPreferences.hotKeyKey)

        XCTAssertEqual(VoxCaretInputPreferences.hotKey, custom)
    }

    /// ⎋ deliberately fails `isValid` — a bare key would fight ordinary typing if it
    /// were bindable. It is only ever held while a recording is running, through
    /// `registerTransient`, which is the one path allowed to skip that rule.
    func testCancelHotKeyIsBareEscapeAndIsNotBindable() {
        let cancel = VoxCaretInputPreferences.cancelHotKey

        XCTAssertEqual(cancel.keyCode, UInt16(kVK_Escape))
        XCTAssertTrue(cancel.modifiers.isEmpty)
        XCTAssertFalse(cancel.isValid)
    }

    // MARK: - Wiring

    /// Both sides of every rewrite are handed to Apple's recognizer as context: the
    /// recognizer has to produce something recognisable before a rewrite can fire.
    func testContextualTermsIncludeBothSidesOfEveryRule() {
        UserDefaults.standard.set("阿童木 => ATM\n派 => Pi", forKey: VoxCaretInputPreferences.dictionaryKey)

        XCTAssertEqual(VoxCaretInputPreferences.contextualTerms, ["阿童木", "ATM", "派", "Pi"])
    }

    func testMissingDictionaryYieldsNoRulesRatherThanCrashing() {
        UserDefaults.standard.removeObject(forKey: VoxCaretInputPreferences.dictionaryKey)

        XCTAssertTrue(VoxCaretInputPreferences.replacements.isEmpty)
        XCTAssertTrue(VoxCaretInputPreferences.contextualTerms.isEmpty)
    }
}
