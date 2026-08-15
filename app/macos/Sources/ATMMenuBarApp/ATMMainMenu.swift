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
        main.addItem(submenu(title: "文件", items: fileItems()))
        main.addItem(submenu(title: "编辑", items: editItems()))
        main.addItem(submenu(title: "前往", items: navigationItems()))
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

    /// ⌘N 是这里唯一一条业务快捷键，而它必须归菜单：加任务卡是覆盖整个窗口的浮层，
    /// 快捷键却曾经挂在「任务」页那个「新建」按钮上——切到收集或知识就按不动了，得先
    /// 换回任务页才能记一条。菜单键等价在任何页签、以及快速面板上都成立，顺带把这一下
    /// 快捷键写在人看得见的地方。⌘S / ⌘K 仍归各自的视图：保存归当前编辑器、搜索归
    /// 常驻侧栏，两者本来就不因页签而消失。
    private static func fileItems() -> [NSMenuItem] {
        [item("新建任务", #selector(AppDelegate.newTodoFromMenu(_:)), "n")]
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

    /// Browser-style history shortcuts. These live in the main menu so they
    /// continue to work while a text field or Markdown editor owns focus; the
    /// app delegate validates each item against the live navigation stacks.
    ///
    /// 分区切换（⌘1–⌘6）同理归菜单：侧栏那五个按钮此前只能点，没有键盘入口，而分区
    /// 是这个 App 里跳得最频繁的一层。顺序跟侧栏一致（设置在最后，它在侧栏底部）。
    /// `tag` 带的是 `ATMDesktopSection.allCases` 的下标，由 app delegate 转回分区。
    private static func navigationItems() -> [NSMenuItem] {
        var items = ATMDesktopSection.allCases.enumerated().map { index, section in
            let entry = item(
                section.title,
                #selector(AppDelegate.selectSectionFromMenu(_:)),
                String(index + 1)
            )
            entry.tag = index
            return entry
        }
        items.append(.separator())
        items.append(item("后退", #selector(AppDelegate.navigateBackFromMenu(_:)), "["))
        items.append(item("前进", #selector(AppDelegate.navigateForwardFromMenu(_:)), "]"))
        return items
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
