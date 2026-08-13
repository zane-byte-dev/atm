import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuItemValidation {
    private var statusBarController: StatusBarController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        // First thing: whether the previous run ended cleanly can only be answered
        // before anything else touches the marker.
        ATMLog.recordStartup()
        NSApp.setActivationPolicy(.regular)
        // Must happen before any window opens: the Edit menu is what makes
        // ⌘V / ⌘C / ⌘X / ⌘A / ⌘Z work inside the app's text fields.
        ATMMainMenu.install()
        let controller = StatusBarController()
        statusBarController = controller

        let environment = ProcessInfo.processInfo.environment
        if environment["ATM_OPEN_PANEL"] != "1", environment["ATM_MENU_ONLY"] != "1" {
            DispatchQueue.main.async { controller.openDesktop() }
        }
    }

    /// 主菜单「文件 → 新建任务」(⌘N)。菜单项 target 是 nil，AppKit 找不到响应者时会
    /// 落到 NSApp 的 delegate，也就是这里 —— 菜单安装在 StatusBarController 建立之前，
    /// 所以不给菜单项绑固定 target，由这层转交。
    @MainActor @objc func newTodoFromMenu(_ sender: Any?) {
        statusBarController?.addTodoFromShortcut()
    }

    /// Main-menu actions keep history shortcuts active regardless of which
    /// SwiftUI control currently owns keyboard focus.
    @MainActor @objc func navigateBackFromMenu(_ sender: Any?) {
        statusBarController?.navigateBack()
    }

    @MainActor @objc func navigateForwardFromMenu(_ sender: Any?) {
        statusBarController?.navigateForward()
    }

    @MainActor func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        switch menuItem.action {
        case #selector(navigateBackFromMenu(_:)):
            return statusBarController?.canNavigateBack == true
        case #selector(navigateForwardFromMenu(_:)):
            return statusBarController?.canNavigateForward == true
        default:
            return true
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        statusBarController?.stop()
        ATMLog.recordCleanExit()
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { statusBarController?.openDesktop() }
        return true
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
}
