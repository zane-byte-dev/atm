import AppKit

/// The right-click menu on the status bar icon. Built by a static factory rather
/// than inline in `StatusBarController` so the item set is reachable from tests —
/// this menu is the only entry point that works while the quick panel is closed,
/// so losing an item here is invisible until someone reaches for it.
enum ATMStatusMenu {
    /// Every item targets `target` explicitly: the status item's menu is shown
    /// outside any window, so there is no responder chain to fall back on.
    static func make(target: AnyObject) -> NSMenu {
        let menu = NSMenu()
        menu.addItem(item("打开 ATM 主窗口", #selector(StatusBarController.openFromMenu), target: target))
        menu.addItem(item("添加任务", #selector(StatusBarController.addTodoFromMenu), target: target, key: "n"))
        menu.addItem(item("打开快速面板", #selector(StatusBarController.openQuickPanelFromMenu), target: target))
        // One escape hatch: auto-sync covers launch / 5-minute cadence / open.
        // Keep a single forced path for stale indexes without a second "refresh".
        menu.addItem(item("同步并刷新", #selector(StatusBarController.syncFromMenu), target: target, key: "s"))
        menu.addItem(.separator())
        menu.addItem(item("退出 ATM", #selector(StatusBarController.quitFromMenu), target: target, key: "q"))
        return menu
    }

    private static func item(
        _ title: String,
        _ action: Selector,
        target: AnyObject,
        key: String = ""
    ) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: key)
        item.target = target
        return item
    }
}
