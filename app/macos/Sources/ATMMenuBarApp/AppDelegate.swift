import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
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
