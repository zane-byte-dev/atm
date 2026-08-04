import AppKit
import XCTest
@testable import ATMMenuBarApp

/// The clipboard shortcuts only work because these menu items exist: macOS
/// dispatches ⌘V and friends as menu key equivalents, so dropping an item here
/// silently breaks pasting in every text field again.
final class ATMMainMenuTests: XCTestCase {
    private func editMenu() throws -> NSMenu {
        let main = ATMMainMenu.make(appName: "ATM")
        return try XCTUnwrap(main.item(withTitle: "编辑")?.submenu)
    }

    func testEditMenuBindsStandardClipboardShortcuts() throws {
        let edit = try editMenu()
        let expected: [(String, Selector, String, NSEvent.ModifierFlags)] = [
            ("撤销", Selector(("undo:")), "z", .command),
            ("重做", Selector(("redo:")), "z", [.command, .shift]),
            ("剪切", #selector(NSText.cut(_:)), "x", .command),
            ("复制", #selector(NSText.copy(_:)), "c", .command),
            ("粘贴", #selector(NSText.paste(_:)), "v", .command),
            ("粘贴为纯文本", #selector(NSTextView.pasteAsPlainText(_:)), "v", [.command, .option, .shift]),
            ("全选", #selector(NSText.selectAll(_:)), "a", .command),
        ]

        for (title, action, key, modifiers) in expected {
            let item = try XCTUnwrap(edit.item(withTitle: title), "缺少编辑菜单项：\(title)")
            XCTAssertEqual(item.action, action, "\(title) 绑定了错误的 action")
            XCTAssertEqual(item.keyEquivalent, key, "\(title) 快捷键错误")
            XCTAssertEqual(item.keyEquivalentModifierMask, modifiers, "\(title) 修饰键错误")
            // nil target is what sends the action down the responder chain to the
            // focused field editor.
            XCTAssertNil(item.target, "\(title) 不应绑定固定 target")
        }
    }

    func testMainMenuHasAppAndWindowSections() {
        let main = ATMMainMenu.make(appName: "ATM Dev")
        XCTAssertEqual(main.items.map(\.title), ["ATM Dev", "编辑", "窗口"])
        XCTAssertEqual(main.item(withTitle: "ATM Dev")?.submenu?.item(withTitle: "退出 ATM Dev")?.keyEquivalent, "q")
        XCTAssertEqual(main.item(withTitle: "窗口")?.submenu?.item(withTitle: "关闭")?.keyEquivalent, "w")
    }

    /// ⌘S saves knowledge, ⌘K opens search, ⌘N adds a task — the menu must not
    /// steal them from the views that own them.
    func testMainMenuDoesNotClaimViewLevelShortcuts() throws {
        var claimed: Set<String> = []
        for section in ATMMainMenu.make(appName: "ATM").items {
            for item in section.submenu?.items ?? [] where !item.keyEquivalent.isEmpty {
                if item.keyEquivalentModifierMask == .command { claimed.insert(item.keyEquivalent) }
            }
        }
        XCTAssertFalse(claimed.contains("s"))
        XCTAssertFalse(claimed.contains("k"))
        XCTAssertFalse(claimed.contains("n"))
    }
}
