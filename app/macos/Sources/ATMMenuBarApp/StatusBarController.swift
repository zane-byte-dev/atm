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
    private var agentAttentionNotifier: ATMAgentAttentionNotifier?
    private var approvalPresenter: ATMApprovalPresenter?
    private var approvalArrivalCancellable: AnyCancellable?
    private var outsideClickMonitor: Any?
    private var cancellables = Set<AnyCancellable>()

    init() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        panel = FloatingPanel(size: NSSize(width: 360, height: 420))
        configureStatusItem()
        bindAppearance()
        configurePanel()
        bindStore()
        ATMNotificationManager.shared.start(
            onOpen: { [weak self] route in
                Task { @MainActor in self?.handleNotificationRoute(route) }
            },
            onGuardDecision: { [weak self] approvalID, approve in
                // A banner button is the decision itself. Routed to the store rather
                // than opening anything, so approving from the banner never requires
                // the window.
                Task { @MainActor in self?.store.decideApproval(id: approvalID, approve: approve) }
            }
        )
        store.start()
        agentAttentionNotifier = ATMAgentAttentionNotifier(store: store)
        approvalPresenter = ATMApprovalPresenter(store: store)
        approvalArrivalCancellable = store.approvalArrivals
            .sink { [weak self] arrivals in
                self?.approvalPresenter?.present(arrived: arrivals.arrived, pending: arrivals.pending)
            }
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
        agentAttentionNotifier?.stop()
        store.stop()
        stopOutsideClickMonitor()
    }

    var canNavigateBack: Bool {
        desktopWindow?.isVisible == true && desktopNavigation.canGoBack
    }

    var canNavigateForward: Bool {
        desktopWindow?.isVisible == true && desktopNavigation.canGoForward
    }

    /// 主菜单「前往 → 任务 / 收集 / …」(⌘1–⌘6)。跟侧栏按钮走同一套副作用，
    /// 否则用快捷键切到知识页会停在上一次的目录快照上。
    func selectSection(_ section: ATMDesktopSection) {
        openDesktop(section: section)
        if section == .knowledge {
            store.refreshKnowledgeCatalog()
        }
    }

    func navigateBack() {
        guard canNavigateBack else { return }
        desktopNavigation.goBack()
    }

    func navigateForward() {
        guard canNavigateForward else { return }
        desktopNavigation.goForward()
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
                },
                openUsage: { [weak self] in
                    self?.closePanel()
                    self?.openDesktop(section: .usage)
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
        if let todo { desktopNavigation.selectedTodoID = todo.id }
        if let agentSessionID {
            desktopNavigation.selectedAgentID = agentSessionID
            desktopNavigation.selectedAgentRunTodoID = nil
        }
        desktopNavigation.section = section
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

    /// Where a click on a delivered notification lands.
    ///
    /// An agent banner goes to the agent's own terminal, not to ATM: the whole
    /// point of 等待授权 is that there is a prompt somewhere waiting for a
    /// keystroke, and ATM cannot answer it. `openAgentSession` already falls back
    /// to the Agents pane for a session with no host metadata.
    private func handleNotificationRoute(_ route: ATMNotificationRoute) {
        switch route {
        case .agentSession(let sessionID):
            guard let session = store.snapshot.liveStatus.sessions
                .first(where: { $0.id == sessionID })
            else {
                // The session is gone from the snapshot — its terminal is the one
                // thing we can no longer find. Show the pane instead of nothing.
                openAgents(session: nil)
                return
            }
            openAgentSession(session)
        case .todo(let todoID):
            // `ATMNowSnapshot` keeps todos in per-status buckets with no combined
            // list, and a notification can name a todo in any of them.
            let work = store.snapshot.work
            let buckets = [work.open, work.working, work.waiting, work.review, work.blocked, work.due]
            guard let todo = buckets.lazy.flatMap({ $0 }).first(where: { $0.id == todoID }) else {
                openDesktop()
                return
            }
            openDesktop(todo: todo)
        case .guardApproval:
            // Clicking the banner opens the window that can actually hold the
            // decision, not the transient menu-bar panel.
            approvalPresenter?.openManually()
        case .collection(let itemID):
            if let itemID { desktopNavigation.revealCollectionItem(itemID) }
            openDesktop(section: .collection)
        case .app:
            openDesktop()
        }
    }

    private func bindStore() {
        Publishers.CombineLatest(store.$dashboardState, store.$collectionOverview)
            .sink { [weak self] state, collection in
                guard let button = self?.statusItem.button else { return }
                let snapshot = state.snapshot
                let quota = state.quota
                var title = snapshot.menuBarTitle
                let unread = collection.summary.unreadCount ?? 0
                if unread > 0 {
                    title = "新收集 \(unread)" + (title.isEmpty ? "" : " · \(title)")
                }
                if !title.isEmpty, let suffix = quota.menuBarSuffix {
                    title += " · \(suffix)"
                }
                let collectionTooltip = unread > 0 ? "\(unread) 条新收集待查看" : nil
                let tooltip = [collectionTooltip, snapshot.menuBarTooltip, quota.tooltipText]
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
        // `FloatingPanel` is deliberately a nonactivating panel that can join
        // every Space. Activating the whole app here also raises ATM's main
        // window; when that window belongs to another Space, handing focus to
        // Screenshot can make macOS switch there and back. The panel can become
        // key and handle its controls without activating its owning app.
        panel.makeKey()
        setStatusItemHighlighted(true)
        startOutsideClickMonitor()
    }

    private func closePanel() {
        panel.orderOut(nil)
        setStatusItemHighlighted(false)
        stopOutsideClickMonitor()
    }

    /// A custom panel does not drive the status button's pressed appearance the
    /// way an `NSMenu` does. Reapply on the next run-loop turn as well, after the
    /// mouse-up tracking loop has finished clearing AppKit's transient highlight.
    private func setStatusItemHighlighted(_ highlighted: Bool) {
        statusItem.button?.highlight(highlighted)
        guard highlighted else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self, self.panel.isVisible else { return }
            self.statusItem.button?.highlight(true)
        }
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
