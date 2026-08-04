import AppKit

/// The app boots from a plain `NSApplication` with no nib, so nothing installs a
/// main menu for free — and on macOS the clipboard shortcuts live in the menu,
/// not in the text views. `⌘V` / `⌘C` / `⌘X` / `⌘A` / `⌘Z` are dispatched as menu
/// key equivalents, so without an Edit menu every input in the app (task title,
/// description, knowledge editor, search field) silently ignored them.
///
/// Every item targets nil so AppKit routes the action down the responder chain to
/// whichever field editor is first responder, which is also what keeps the items
/// correctly greyed out when no text view is focused.
enum ATMMainMenu {
    static func install(into application: NSApplication = .shared) {
        application.mainMenu = make(appName: appName)
    }

    /// Bundle name, so the dev build shows "ATM Dev" like its window title does.
    static var appName: String {
        let info = Bundle.main.infoDictionary
        let name = (info?["CFBundleDisplayName"] ?? info?["CFBundleName"]) as? String
        guard let name, !name.isEmpty else { return "ATM" }
        return name
    }

    static func make(appName: String) -> NSMenu {
        let main = NSMenu()
        main.addItem(submenu(title: appName, items: appItems(appName: appName)))
        main.addItem(submenu(title: "编辑", items: editItems()))
        main.addItem(submenu(title: "窗口", items: windowItems()))
        return main
    }

    private static func appItems(appName: String) -> [NSMenuItem] {
        [
            item("关于 \(appName)", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""),
            .separator(),
            item("隐藏 \(appName)", #selector(NSApplication.hide(_:)), "h"),
            item("隐藏其他", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]),
            item("显示全部", #selector(NSApplication.unhideAllApplications(_:)), ""),
            .separator(),
            item("退出 \(appName)", #selector(NSApplication.terminate(_:)), "q"),
        ]
    }

    private static func editItems() -> [NSMenuItem] {
        [
            // `undo:` / `redo:` have no public Swift selector: the field editor
            // picks them up through NSResponder's undo manager forwarding.
            item("撤销", Selector(("undo:")), "z"),
            item("重做", Selector(("redo:")), "z", [.command, .shift]),
            .separator(),
            item("剪切", #selector(NSText.cut(_:)), "x"),
            item("复制", #selector(NSText.copy(_:)), "c"),
            item("粘贴", #selector(NSText.paste(_:)), "v"),
            item("粘贴为纯文本", #selector(NSTextView.pasteAsPlainText(_:)), "v", [.command, .option, .shift]),
            item("删除", #selector(NSText.delete(_:)), ""),
            .separator(),
            item("全选", #selector(NSText.selectAll(_:)), "a"),
        ]
    }

    private static func windowItems() -> [NSMenuItem] {
        // No `NSApp.windowsMenu`: the quick panel is a glance, not a document, so
        // an auto-populated window list would only add noise. `performClose:` and
        // `performMiniaturize:` reach the key window through the responder chain.
        [
            item("最小化", #selector(NSWindow.performMiniaturize(_:)), "m"),
            item("关闭", #selector(NSWindow.performClose(_:)), "w"),
        ]
    }

    private static func submenu(title: String, items: [NSMenuItem]) -> NSMenuItem {
        let entry = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        let menu = NSMenu(title: title)
        items.forEach { menu.addItem($0) }
        entry.submenu = menu
        return entry
    }

    private static func item(
        _ title: String,
        _ action: Selector,
        _ keyEquivalent: String,
        _ modifiers: NSEvent.ModifierFlags = .command
    ) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: keyEquivalent)
        item.keyEquivalentModifierMask = modifiers
        return item
    }
}
