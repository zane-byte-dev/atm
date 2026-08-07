import AppKit
import Combine
import SwiftUI

@MainActor
final class StatusBarController {
    private let store = ATMDataStore()
    private let desktopNavigation = ATMDesktopNavigation()
    private let statusItem: NSStatusItem
    private let panel: FloatingPanel
    private var desktopWindow: NSWindow?
    private var agentNotchController: ATMAgentNotchController?
    private var outsideClickMonitor: Any?
    private var cancellables = Set<AnyCancellable>()

    init() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        panel = FloatingPanel(size: NSSize(width: 360, height: 420))
        configureStatusItem()
        bindAppearance()
        configurePanel()
        bindStore()
        ATMNotificationManager.shared.start { [weak self] in
            Task { @MainActor in self?.openDesktop() }
        }
        store.start()
        agentNotchController = ATMAgentNotchController(
            store: store,
            onOpenSession: { [weak self] session in
                self?.openAgentSession(session)
            },
            onOpenAgents: { [weak self] session in
                self?.openAgents(session: session)
            },
            onOpenSettings: { [weak self] in
                self?.openDesktop(section: .settings)
            }
        )
        if ProcessInfo.processInfo.environment["ATM_OPEN_PANEL"] == "1" {
            DispatchQueue.main.async { [weak self] in self?.openPanel() }
        }
        ATMGlobalHotKeyManager.shared.onPressed = { [weak self] action in
            switch action {
            case .launcher:
                self?.handleGlobalHotKey()
            case .voiceInput:
                ATMVoiceInputCoordinator.shared.hotKeyPressed()
            case .cancelVoice:
                ATMVoiceInputCoordinator.shared.cancel()
            }
        }
        ATMGlobalHotKeyManager.shared.onReleased = { action in
            // Only dictation cares about the key coming back up; the launcher toggles
            // on the way down.
            guard action == .voiceInput else { return }
            ATMVoiceInputCoordinator.shared.hotKeyReleased()
        }
        ATMGlobalHotKeyManager.shared.start()
    }

    func stop() {
        // Before the hot key manager goes away: an in-flight recording holds the
        // microphone and a transient ⎋ registration, and both are its to release.
        ATMVoiceInputCoordinator.shared.cancel()
        ATMGlobalHotKeyManager.shared.stop()
        agentNotchController?.stop()
        store.stop()
        stopOutsideClickMonitor()
    }

    private func configureStatusItem() {
        guard let button = statusItem.button else { return }
        button.image = ATMBrandAssets.menuBarImage()
        button.imagePosition = .imageLeading
        button.font = .monospacedDigitSystemFont(ofSize: 12.5, weight: .semibold)
        button.target = self
        button.action = #selector(statusItemClicked)
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
    }

    private func configurePanel() {
        panel.onDismiss = { [weak self] in self?.closePanel() }
        panel.host(
            QuickPanelView(
                store: store,
                close: { [weak self] in self?.closePanel() },
                openDesktop: { [weak self] todo in
                    self?.closePanel()
                    self?.openDesktop(todo: todo)
                }
            )
        )
    }

    /// `showAddTodo` is assigned on every open, not just when true: the add card
    /// is owned by the navigation object, so closing the desktop window with ⌘W
    /// while it is up would leave the flag set and pop the card again the next
    /// time someone merely opens the main window.
    func openDesktop(
        todo: ATMTodo? = nil,
        showAddTodo: Bool = false,
        section: ATMDesktopSection = .tasks,
        agentSessionID: String? = nil
    ) {
        desktopNavigation.section = section
        if let todo { desktopNavigation.selectedTodoID = todo.id }
        if let agentSessionID { desktopNavigation.selectedAgentID = agentSessionID }
        desktopNavigation.showAddTodo = showAddTodo

        let window: NSWindow
        if let desktopWindow {
            window = desktopWindow
        } else {
            let content = DesktopContentView(store: store, navigation: desktopNavigation)
            let created = NSWindow(
                contentRect: NSRect(x: 0, y: 0, width: 1440, height: 960),
                styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                backing: .buffered,
                defer: false
            )
            created.title = Bundle.main.bundleURL.pathExtension == "app" ? "ATM" : "ATM Dev"
            created.titleVisibility = .hidden
            created.titlebarAppearsTransparent = true
            created.minSize = NSSize(width: 1120, height: 780)
            created.isReleasedWhenClosed = false
            created.setFrameAutosaveName("ATMDesktopWindowV3")
            created.contentViewController = NSHostingController(rootView: content)
            created.center()
            desktopWindow = created
            window = created
        }

        store.refresh(sync: true)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func openAgents(session: ATMLiveSession?) {
        openDesktop(section: .agents, agentSessionID: session?.id)
    }

    private func openAgentSession(_ session: ATMLiveSession) {
        do {
            try ATMAgentSessionLauncher.open(session)
        } catch {
            // Prefer activating the known host terminal over bouncing into ATM's
            // own Agents pane. Opening the desktop is a last resort for sessions
            // with no host metadata at all.
            if let bundle = session.terminalApp?
                .trimmingCharacters(in: .whitespacesAndNewlines),
               !bundle.isEmpty {
                try? ATMAgentSessionLauncher.activateApplication(bundleIdentifier: bundle)
                return
            }
            openAgents(session: session)
        }
    }

    private func bindStore() {
        store.$dashboardState
            .sink { [weak self] state in
                guard let button = self?.statusItem.button else { return }
                let snapshot = state.snapshot
                let quota = state.quota
                var title = snapshot.menuBarTitle
                if !title.isEmpty, let suffix = quota.menuBarSuffix {
                    title += " · \(suffix)"
                }
                let tooltip = [snapshot.menuBarTooltip, quota.tooltipText]
                    .compactMap { $0 }
                    .joined(separator: " · ")
                button.title = title.isEmpty ? "" : " \(title)"
                button.toolTip = tooltip
                button.setAccessibilityLabel("ATM")
                button.setAccessibilityValue(tooltip)
            }
            .store(in: &cancellables)
    }

    private func bindAppearance() {
        ATMAppearance.shared.$themeMode
            .removeDuplicates()
            .sink { mode in
                // nil means inherit the current macOS appearance and keep
                // following future system changes.
                NSApp.appearance = mode.nsAppearance
            }
            .store(in: &cancellables)
    }

    @objc private func statusItemClicked() {
        if NSApp.currentEvent?.type == .rightMouseUp {
            showContextMenu()
        } else {
            toggleQuickPanel()
        }
    }

    /// The global shortcut and the status item share one gesture: press once to
    /// glance, press again to get out of the way. A shortcut that only ever
    /// opened would leave the panel stranded over whatever the person went back
    /// to, since the panel closes on an outside *click*, not on a keystroke.
    private func toggleQuickPanel() {
        if panel.isVisible {
            closePanel()
        } else {
            openPanel()
        }
    }

    private func handleGlobalHotKey() {
        switch ATMGlobalHotKeyPreferences.target {
        case .desktop:
            toggleDesktop()
        case .quickPanel:
            toggleQuickPanel()
        }
    }

    /// Press again to put ATM away, the same way the quick panel behaves. Hiding
    /// only applies while ATM is the app in front and this window is the one being
    /// looked at: from another app the same keystroke has to raise the window, or
    /// the shortcut would silently hide a window nobody could see.
    ///
    /// Keeps whichever section was last open — the shortcut is "come back to what
    /// I was doing", while the menu's 打开 ATM 主窗口 keeps the 任务 default.
    private func toggleDesktop() {
        if let window = desktopWindow, window.isVisible, NSApp.isActive, window.isKeyWindow {
            window.orderOut(nil)
            return
        }
        openDesktop(section: desktopNavigation.section)
    }

    private func openPanel() {
        guard let button = statusItem.button else { return }
        // Recreate the lightweight view tree so every glance starts at NOW,
        // instead of preserving a previous scroll position deep in analytics.
        configurePanel()
        store.refresh(sync: true)
        panel.anchor(to: button)
        panel.orderFrontRegardless()
        panel.makeKey()
        NSApp.activate(ignoringOtherApps: true)
        startOutsideClickMonitor()
    }

    private func closePanel() {
        panel.orderOut(nil)
        stopOutsideClickMonitor()
    }

    private func showContextMenu() {
        store.refresh(sync: true)
        statusItem.menu = ATMStatusMenu.make(target: self)
        statusItem.button?.performClick(nil)
        statusItem.menu = nil
    }

    // Internal, not private: `ATMStatusMenu` forms the selectors.
    @objc func openFromMenu() { openDesktop() }
    /// Right-click is the only add-task entry that works without first pulling
    /// the quick panel open.
    @objc func addTodoFromMenu() { openDesktop(showAddTodo: true) }
    /// ⌘N，来自主菜单，所以在主窗口的任何页签和快速面板上都成立。停在哪个页签就在那儿
    /// 弹卡：卡片本来就覆盖整个窗口，先把人拽到「任务」再弹，等于顺手换掉了他正在看的
    /// 东西——提交后才会跳到新建的任务上。面板上按的话先把面板收起来，输入卡在主窗口。
    @objc func addTodoFromShortcut() {
        closePanel()
        openDesktop(showAddTodo: true, section: desktopNavigation.section)
    }
    @objc func openQuickPanelFromMenu() { openPanel() }
    @objc func syncFromMenu() { store.refresh(sync: true) }
    @objc func quitFromMenu() { NSApp.terminate(nil) }

    private func startOutsideClickMonitor() {
        guard outsideClickMonitor == nil else { return }
        outsideClickMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            Task { @MainActor in self?.closePanel() }
        }
    }

    private func stopOutsideClickMonitor() {
        if let outsideClickMonitor {
            NSEvent.removeMonitor(outsideClickMonitor)
            self.outsideClickMonitor = nil
        }
    }
}
