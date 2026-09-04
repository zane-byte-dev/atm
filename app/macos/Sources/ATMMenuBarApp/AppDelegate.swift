import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuItemValidation {
    private var statusBarController: StatusBarController?
    private var transitionController: ATMLegacyTransitionController?
    private var legacyLease: ATMLegacyRuntimeLease?

    func applicationDidFinishLaunching(_ notification: Notification) {
        // First thing: whether the previous run ended cleanly can only be answered
        // before anything else touches the marker.
        ATMLog.recordStartup()
        // This must precede StatusBarController: its stored properties eagerly
        // construct ATMDataStore. A stopped Go instance still owns the backend.
        if ATMRuntimeHandover.mode != .legacy {
            NSApp.setActivationPolicy(.accessory)
            transitionController = ATMLegacyTransitionController(mode: ATMRuntimeHandover.mode)
            return
        }
        // Go takes the same lifetime lease before starting presence or jobs.
        // Lock before constructing Store, then recheck the persisted selection.
        legacyLease = try? ATMLegacyRuntimeLease(directory: ATMRuntimeHandover.dataDirectory)
        guard legacyLease != nil, ATMRuntimeHandover.mode == .legacy else {
            legacyLease = nil
            NSApp.setActivationPolicy(.accessory)
            transitionController = ATMLegacyTransitionController(mode: .web)
            return
        }
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

    /// 「前往 → 任务 / 收集 / …」(⌘1–⌘6)。菜单项只带一个下标 tag，分区的顺序由
    /// `ATMDesktopSection.allCases` 定，跟侧栏共用同一个顺序。
    @MainActor @objc func selectSectionFromMenu(_ sender: Any?) {
        guard let item = sender as? NSMenuItem,
              ATMDesktopSection.allCases.indices.contains(item.tag)
        else { return }
        statusBarController?.selectSection(ATMDesktopSection.allCases[item.tag])
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
        transitionController?.stop()
        legacyLease = nil
        ATMLog.recordCleanExit()
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { statusBarController?.openDesktop(); transitionController?.openWeb() }
        return true
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }
}
