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
        XCTAssertEqual(main.items.map(\.title), ["ATM Dev", "文件", "编辑", "窗口"])
        XCTAssertEqual(main.item(withTitle: "ATM Dev")?.submenu?.item(withTitle: "退出 ATM Dev")?.keyEquivalent, "q")
        XCTAssertEqual(main.item(withTitle: "窗口")?.submenu?.item(withTitle: "关闭")?.keyEquivalent, "w")
    }

    /// ⌘S saves knowledge and ⌘K opens search: both belong to something that is on
    /// screen whatever the selected section is — the focused editor, the permanent
    /// sidebar — so the menu must not steal them from the views that own them.
    func testMainMenuDoesNotClaimViewLevelShortcuts() throws {
        XCTAssertFalse(commandShortcuts().contains("s"))
        XCTAssertFalse(commandShortcuts().contains("k"))
    }

    /// ⌘N is the exception, and the menu owning it is the point: the add-task card
    /// covers the whole window, but the shortcut used to hang off 任务's 新建
    /// button, so it died the moment someone was reading 收集 or 知识. A menu key
    /// equivalent holds in every section and in the quick panel.
    func testFileMenuOwnsTheAddTaskShortcut() throws {
        let file = try XCTUnwrap(ATMMainMenu.make(appName: "ATM").item(withTitle: "文件")?.submenu)
        let item = try XCTUnwrap(file.item(withTitle: "新建任务"), "缺少「新建任务」菜单项")
        XCTAssertEqual(item.keyEquivalent, "n")
        XCTAssertEqual(item.keyEquivalentModifierMask, .command)
        XCTAssertEqual(item.action, #selector(AppDelegate.newTodoFromMenu(_:)))
        // nil target so AppKit walks the responder chain out to NSApp's delegate:
        // the menu is installed before the status bar controller exists.
        XCTAssertNil(item.target)
    }

    /// The nil target only works because AppKit's search for a responder ends at
    /// NSApp's delegate. Nothing else in the app is reachable that way, so this is
    /// the one place where losing it would leave a menu item that greys itself out.
    @MainActor
    func testTheAddTaskActionReachesTheAppDelegate() {
        let application = NSApplication.shared
        let previous = application.delegate
        let delegate = AppDelegate()
        application.delegate = delegate
        addTeardownBlock { @MainActor in application.delegate = previous }

        let target = application.target(forAction: #selector(AppDelegate.newTodoFromMenu(_:)))
        XCTAssertTrue(target as? AppDelegate === delegate)
    }

    private func commandShortcuts() -> Set<String> {
        var claimed: Set<String> = []
        for section in ATMMainMenu.make(appName: "ATM").items {
            for item in section.submenu?.items ?? [] where !item.keyEquivalent.isEmpty {
                if item.keyEquivalentModifierMask == .command { claimed.insert(item.keyEquivalent) }
            }
        }
        return claimed
    }
}
