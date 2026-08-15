import XCTest
@testable import ATMMenuBarApp

final class ATMShortcutCatalogTests: XCTestCase {
    func testSettingsExposeDedicatedShortcutTab() {
        XCTAssertEqual(
            ATMSettingsTab.allCases,
            [.general, .shortcuts, .voice, .notify, .todo, .model]
        )
        XCTAssertEqual(ATMSettingsTab.shortcuts.title, "快捷键")
        XCTAssertEqual(ATMSettingsTab.shortcuts.systemImage, "keyboard")
        XCTAssertEqual(ATMSettingsTab.notify.title, "通知")
        XCTAssertEqual(ATMSettingsTab.notify.systemImage, "bell")
        XCTAssertEqual(ATMSettingsTab.model.title, "模型")
        XCTAssertEqual(ATMSettingsTab.model.systemImage, "sparkles")
    }

    func testCatalogListsEveryApplicationShortcutWithoutDuplicateIDs() throws {
        let shortcuts = ATMShortcutCatalog.allShortcuts
        XCTAssertEqual(Set(shortcuts.map(\.id)).count, shortcuts.count)
        XCTAssertEqual(Set(ATMShortcutCatalog.groups.map(\.id)).count, ATMShortcutCatalog.groups.count)

        let expected: [String: String] = [
            "new-todo": "⌘N",
            "search": "⌘K",
            "save-knowledge": "⌘S",
            "submit-form": "⌘↩",
            "cancel": "⎋",
            "section": "⌘1–⌘6",
            "back": "⌘[",
            "forward": "⌘]",
            "minimize": "⌘M",
            "close": "⌘W",
            "hide": "⌘H",
            "hide-others": "⌥⌘H",
            "quit": "⌘Q",
            "undo": "⌘Z",
            "redo": "⇧⌘Z",
            "cut": "⌘X",
            "copy": "⌘C",
            "paste": "⌘V",
            "paste-plain": "⌥⇧⌘V",
            "select-all": "⌘A",
            "search-move": "↑ / ↓",
            "search-open": "↩",
            "search-close": "⎋",
        ]

        XCTAssertEqual(shortcuts.count, expected.count)
        for (id, keys) in expected {
            let shortcut = try XCTUnwrap(shortcuts.first { $0.id == id })
            XCTAssertEqual(shortcut.keys, keys)
            XCTAssertFalse(shortcut.title.isEmpty)
            XCTAssertFalse(shortcut.detail.isEmpty)
        }
    }
}
