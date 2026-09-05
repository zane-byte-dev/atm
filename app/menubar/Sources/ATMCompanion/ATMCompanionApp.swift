import AppKit
import ServiceManagement
import SwiftUI
import UserNotifications

@main
struct ATMCompanionApp {
    @MainActor static func main() {
        let app = NSApplication.shared
        let delegate = CompanionDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.accessory)
        withExtendedLifetime(delegate) { app.run() }
    }
}

@MainActor
final class CompanionDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    private var item: NSStatusItem!
    private var pollTask: Task<Void, Never>?
    private var refreshInFlight = false
    private var refreshAgain = false
    private var connected = false
    private var syncing = false
    private var message = "连接中…"
    private var active = 0
    private var attention = 0
    private var todos: CompanionTodos?
    private var quota: CompanionQuota?
    private var todayTokens: TodayTokenMenuState = .loading
    private var lastInstance = UserDefaults.standard.string(forKey: "NotificationInstance") ?? ""
    private var cursor = UInt64(UserDefaults.standard.string(forKey: "NotificationCursor") ?? "") ?? 0
    private let clientID = UUID().uuidString
    private var launcherActive = false
    private var soundWindow: NSWindow?
    private var notifications = UserDefaults.standard.object(forKey: "NativeNotifications") as? Bool ?? true
    private var launcher = UserDefaults.standard.object(forKey: "LauncherEnabled") as? Bool ?? true

    private var client: RuntimeClient {
        let configured = UserDefaults.standard.string(forKey: "ATMDataDirectory")
        let directory = configured.map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".atm")
        return RuntimeClient(dataDirectory: directory)
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.image = ATMCompanionBrandAssets.menuBarImage()
        item.button?.imagePosition = .imageLeading
        item.button?.font = .monospacedDigitSystemFont(ofSize: 12.5, weight: .semibold)
        UNUserNotificationCenter.current().delegate = self
        UNUserNotificationCenter.current().setNotificationCategories([])
        LauncherHotKey.shared.onOpen = { [weak self] in self?.openWeb() }
        if launcher { launcherActive = LauncherHotKey.shared.start() }
        updateStatusItem()
        rebuildMenu()
        startPolling()
    }

    func applicationWillTerminate(_ notification: Notification) {
        pollTask?.cancel()
        LauncherHotKey.shared.stop()
    }

    private func startPolling() {
        pollTask?.cancel()
        pollTask = Task { [weak self] in
            if self?.notifications == true {
                let center = UNUserNotificationCenter.current()
                let settings = await center.notificationSettings()
                if settings.authorizationStatus == .notDetermined {
                    _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
                }
            }
            while !Task.isCancelled {
                await self?.refresh()
                let retrySeconds = self?.connected == false ? 2 : 10
                do { try await Task.sleep(for: .seconds(retrySeconds)) } catch { return }
            }
        }
    }

    private func refresh() async {
        guard !refreshInFlight else {
            refreshAgain = true
            return
        }
        refreshInFlight = true
        defer {
            refreshInFlight = false
            if refreshAgain {
                refreshAgain = false
                Task { @MainActor [weak self] in await self?.refresh() }
            }
        }

        do {
            let runtime = client
            let instance = try runtime.instance()
            let instanceChanged = lastInstance != instance.instanceID
            let requestCursor = instanceChanged ? 0 : cursor
            let center = UNUserNotificationCenter.current()
            let settings = await center.notificationSettings()
            let granted = settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional
            let result = try await runtime.state(
                instance: instance,
                clientID: clientID,
                after: requestCursor,
                enabled: notifications && granted
            )
            guard !Task.isCancelled else { return }

            if instanceChanged {
                lastInstance = instance.instanceID
                cursor = 0
            }
            active = result.snapshot.activeCount
            attention = result.snapshot.attentionCount
            todos = result.todos
            quota = result.quota
            todayTokens = TodayTokenMenuPresentation.resolve(
                today: result.todayUsage,
                legacyQuick: result.legacyQuick
            )
            connected = true
            message = CompanionMenuPresentation.serviceTitle(active: active, attention: attention)

            if notifications && granted, let currentIDs = result.attentionNotificationIDs {
                await reconcileAttentionNotifications(validIDs: Set(currentIDs), center: center)
            }
            if let feed = result.feed {
                if notifications && granted {
                    for entry in feed.notifications ?? [] {
                        guard !Task.isCancelled else { return }
                        if entry.sequence <= cursor { continue }
                        if entry.action == "withdraw" {
                            center.removeDeliveredNotifications(withIdentifiers: [entry.id])
                            center.removePendingNotificationRequests(withIdentifiers: [entry.id])
                        } else if entry.action == "post" {
                            let content = UNMutableNotificationContent()
                            content.title = entry.title
                            content.subtitle = entry.subtitle ?? ""
                            content.body = entry.body ?? ""
                            content.userInfo = ["kind": entry.kind]
                            if let objectID = entry.objectID { content.userInfo["object_id"] = objectID }
                            try await center.add(UNNotificationRequest(identifier: entry.id, content: content, trigger: nil))
                            guard !Task.isCancelled else { return }
                            if entry.kind == "completed" || entry.kind == "todo_completed" {
                                ATMAgentSoundPlayer.shared.play(.taskCompleted)
                            } else {
                                ATMAgentSoundPlayer.shared.play(.attentionRequired)
                            }
                        }
                        cursor = entry.sequence
                        saveCursor()
                        try await runtime.acknowledge(
                            instance: instance,
                            clientID: clientID,
                            sequence: entry.sequence
                        )
                    }
                }
                cursor = feed.advancedCursor(from: cursor)
                saveCursor()
                if notifications && granted, cursor > 0 {
                    try await runtime.acknowledge(instance: instance, clientID: clientID, sequence: cursor)
                }
            }
        } catch {
            guard !Task.isCancelled else { return }
            let hasSnapshot = connected
                || todos != nil
                || quota != nil
                || TodayTokenMenuPresentation.statusBarValue(todayTokens) != nil
            connected = false
            message = hasSnapshot ? "正在重新连接… · 显示上次数据" : error.localizedDescription
        }

        updateStatusItem()
        rebuildMenu()
    }

    private func reconcileAttentionNotifications(
        validIDs: Set<String>,
        center: UNUserNotificationCenter
    ) async {
        let delivered = await center.deliveredNotifications()
        let pending = await center.pendingNotificationRequests()
        let deliveredIDs = delivered.compactMap { notification in
            notification.request.content.userInfo["kind"] as? String == "attention"
                ? notification.request.identifier : nil
        }
        let pendingIDs = pending.compactMap { request in
            request.content.userInfo["kind"] as? String == "attention"
                ? request.identifier : nil
        }
        let staleIDs = Set(deliveredIDs + pendingIDs).subtracting(validIDs)
        guard !staleIDs.isEmpty else { return }
        center.removeDeliveredNotifications(withIdentifiers: Array(staleIDs))
        center.removePendingNotificationRequests(withIdentifiers: Array(staleIDs))
    }

    private func saveCursor() {
        UserDefaults.standard.set(lastInstance, forKey: "NotificationInstance")
        UserDefaults.standard.set(String(cursor), forKey: "NotificationCursor")
    }

    private func updateStatusItem() {
        let summary = CompanionMenuPresentation.statusBarTitle(
            attention: attention,
            quota: quota,
            todayTokens: todayTokens
        )
        let hasSummary = todos != nil
            || quota != nil
            || TodayTokenMenuPresentation.statusBarValue(todayTokens) != nil
        let summaryTooltip = CompanionMenuPresentation.statusBarTooltip(
                active: active,
                attention: attention,
                todos: todos,
                quota: quota,
                todayTokens: todayTokens
            )
        let tooltip = !hasSummary ? message : connected ? summaryTooltip : message + "\n" + summaryTooltip
        item.button?.title = summary.isEmpty ? "" : " \(summary)"
        item.button?.toolTip = tooltip
        item.button?.setAccessibilityLabel("ATM")
        item.button?.setAccessibilityValue(tooltip)
    }

    private func rebuildMenu() {
        let menu = NSMenu()
        addDisabled(message, to: menu)
        let today = menu.addItem(
            withTitle: TodayTokenMenuPresentation.title(todayTokens),
            action: nil,
            keyEquivalent: ""
        )
        today.isEnabled = false
        today.image = NSImage(systemSymbolName: "chart.bar.fill", accessibilityDescription: nil)
        today.toolTip = TodayTokenMenuPresentation.detail(todayTokens)
        menu.addItem(.separator())

        addSummarySections(to: menu)
        menu.addItem(.separator())

        add("打开 ATM", #selector(openWebAction), to: menu)
        add("新增任务", #selector(newTaskFromMenu), to: menu, keyEquivalent: "n")
        let sync = add(
            syncing ? "正在同步…" : "同步并刷新",
            #selector(syncFromMenu),
            to: menu,
            keyEquivalent: "s"
        )
        sync.isEnabled = !syncing

        let settings = NSMenu()
        add("原生通知", #selector(toggleNotifications), to: settings, checked: notifications)
        add("通知声音…", #selector(openSoundSettings), to: settings)
        add(
            launcher && !launcherActive ? "全局打开 ATM ⌥⌘A（被占用）" : "全局打开 ATM ⌥⌘A",
            #selector(toggleLauncher),
            to: settings,
            checked: launcher
        )
        add(
            "登录时启动菜单栏",
            #selector(toggleLogin),
            to: settings,
            checked: SMAppService.mainApp.status == .enabled
        )
        add("选择 ATM 数据目录…", #selector(chooseDataDirectory), to: settings)
        let importMenu = NSMenu()
        for (title, domain) in [
            ("从 ATM 正式版导入", "dev.zanebyte.atm.menubar"),
            ("从 ATM Dev 导入", "dev.zanebyte.atm.menubar.dev"),
        ] {
            let entry = importMenu.addItem(
                withTitle: title,
                action: #selector(importLegacy(_:)),
                keyEquivalent: ""
            )
            entry.target = self
            entry.representedObject = domain
        }
        let importEntry = settings.addItem(
            withTitle: "导入旧菜单栏偏好",
            action: nil,
            keyEquivalent: ""
        )
        importEntry.submenu = importMenu
        let settingsEntry = menu.addItem(withTitle: "设置", action: nil, keyEquivalent: "")
        settingsEntry.submenu = settings
        menu.addItem(.separator())
        let quitEntry = add("退出 ATM Menu", #selector(quit), to: menu, keyEquivalent: "q")
        quitEntry.toolTip = "后台服务继续运行"
        item.menu = menu
    }

    private func addSummarySections(to menu: NSMenu) {
        let taskHeader = menu.addItem(
            withTitle: CompanionMenuPresentation.taskHeader(todos),
            action: nil,
            keyEquivalent: ""
        )
        taskHeader.isEnabled = false
        taskHeader.toolTip = todos?.error
        if let error = todos?.error, !error.isEmpty {
            taskHeader.toolTip = error
        } else {
            let rows = CompanionMenuPresentation.taskRows(todos)
            if !rows.isEmpty {
                for row in rows {
                    addRoute(
                        row.title,
                        detail: row.detail,
                        route: row.route,
                        symbol: "checklist",
                        to: menu
                    )
                }
                if let total = todos?.total, total > rows.count {
                    addRoute(
                        "另外 \(total - rows.count) 个任务…",
                        detail: "在 Web 中查看全部",
                        route: .tasks,
                        symbol: "ellipsis.circle",
                        to: menu
                    )
                }
            }
        }

        menu.addItem(.separator())
        let quotaHeader = menu.addItem(
            withTitle: CompanionMenuPresentation.quotaHeader(quota),
            action: nil,
            keyEquivalent: ""
        )
        quotaHeader.isEnabled = false
        quotaHeader.toolTip = quota?.error
        if let error = quota?.error, !error.isEmpty {
            quotaHeader.toolTip = error
        } else {
            let rows = CompanionMenuPresentation.quotaRows(quota)
            if !rows.isEmpty {
                for row in rows {
                    addRoute(
                        "\(row.title)    \(row.detail)",
                        detail: row.detail,
                        route: row.route,
                        symbol: "chart.bar.xaxis",
                        to: menu
                    )
                }
                let hiddenCount = max(0, (quota?.windows.count ?? 0) - rows.count)
                if hiddenCount > 0 || quota?.truncated == true {
                    addRoute(
                        quota?.truncated == true ? "查看全部额度…" : "另外 \(hiddenCount) 个额度窗口…",
                        detail: "在 Web 中查看全部",
                        route: .usage(agent: nil),
                        symbol: "ellipsis.circle",
                        to: menu
                    )
                }
            }
        }
    }

    @discardableResult
    private func add(
        _ title: String,
        _ selector: Selector,
        to menu: NSMenu,
        checked: Bool? = nil,
        keyEquivalent: String = ""
    ) -> NSMenuItem {
        let entry = menu.addItem(withTitle: title, action: selector, keyEquivalent: keyEquivalent)
        entry.target = self
        if let checked { entry.state = checked ? .on : .off }
        return entry
    }

    private func addDisabled(_ title: String, detail: String? = nil, to menu: NSMenu) {
        let entry = menu.addItem(withTitle: title, action: nil, keyEquivalent: "")
        entry.isEnabled = false
        entry.toolTip = detail
    }

    private func addRoute(
        _ title: String,
        detail: String?,
        route: RuntimeRoute,
        symbol: String,
        to menu: NSMenu
    ) {
        let entry = menu.addItem(withTitle: title, action: #selector(openRoute(_:)), keyEquivalent: "")
        entry.target = self
        entry.representedObject = MenuRouteTarget(route)
        entry.toolTip = detail
        entry.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
    }

    @objc private func openWebAction() { openWeb() }
    @objc private func newTaskFromMenu() { openWeb(route: .newTask) }

    private func openWeb(route: RuntimeRoute = .home) {
        Task { @MainActor in
            do {
                NSWorkspace.shared.open(try await client.openURL(route: route))
            } catch {
                showError(error.localizedDescription)
            }
        }
    }

    @objc private func openRoute(_ sender: NSMenuItem) {
        guard let target = sender.representedObject as? MenuRouteTarget else { return }
        openWeb(route: target.route)
    }

    @objc private func syncFromMenu() {
        guard !syncing else { return }
        syncing = true
        rebuildMenu()
        Task { @MainActor in
            do {
                let runtime = client
                let instance = try runtime.instance()
                _ = try await runtime.synchronize(instance: instance, idempotencyKey: UUID().uuidString)
            } catch {
                if error.localizedDescription != RuntimeClient.ClientError.busy.localizedDescription {
                    showError(error.localizedDescription)
                }
            }
            syncing = false
            await refresh()
        }
    }

    @objc private func openSoundSettings() {
        if soundWindow == nil {
            let window = NSWindow(
                contentRect: NSRect(x: 0, y: 0, width: 460, height: 250),
                styleMask: [.titled, .closable],
                backing: .buffered,
                defer: false
            )
            window.title = "ATM 通知声音"
            window.isReleasedWhenClosed = false
            window.contentView = NSHostingView(rootView: NotificationSoundSettings())
            window.center()
            soundWindow = window
        }
        NSApp.activate(ignoringOtherApps: true)
        soundWindow?.makeKeyAndOrderFront(nil)
    }

    @objc private func toggleNotifications() {
        notifications.toggle()
        UserDefaults.standard.set(notifications, forKey: "NativeNotifications")
        Task { @MainActor in
            if notifications {
                do {
                    _ = try await UNUserNotificationCenter.current().requestAuthorization(
                        options: [.alert, .sound, .badge]
                    )
                } catch {
                    showError(error.localizedDescription)
                }
            }
            rebuildMenu()
            startPolling()
        }
    }

    @objc private func toggleLauncher() {
        launcher.toggle()
        UserDefaults.standard.set(launcher, forKey: "LauncherEnabled")
        LauncherHotKey.shared.stop()
        launcherActive = launcher ? LauncherHotKey.shared.start() : false
        rebuildMenu()
    }

    @objc private func toggleLogin() {
        do {
            if SMAppService.mainApp.status == .enabled {
                try SMAppService.mainApp.unregister()
            } else {
                try SMAppService.mainApp.register()
            }
        } catch {
            showError(error.localizedDescription)
        }
        rebuildMenu()
    }

    @objc private func chooseDataDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.message = "选择包含 runtime 目录的 ATM 数据目录"
        if panel.runModal() == .OK, let url = panel.url {
            UserDefaults.standard.set(url.path, forKey: "ATMDataDirectory")
            lastInstance = ""
            cursor = 0
            active = 0
            attention = 0
            todos = nil
            quota = nil
            todayTokens = .loading
            message = "连接中…"
            updateStatusItem()
            rebuildMenu()
            startPolling()
        }
    }

    @objc private func importLegacy(_ sender: NSMenuItem) {
        guard let domain = sender.representedObject as? String,
              ["dev.zanebyte.atm.menubar", "dev.zanebyte.atm.menubar.dev"].contains(domain) else { return }
        let values = UserDefaults.standard.persistentDomain(forName: domain) ?? [:]
        if let enabled = values["ATMAgentAttentionNotifyEnabled"] as? Bool {
            notifications = enabled
            UserDefaults.standard.set(enabled, forKey: "NativeNotifications")
        }
        if let enabled = values[ATMAgentSoundPreferences.enabledKey] as? Bool {
            UserDefaults.standard.set(enabled, forKey: ATMAgentSoundPreferences.enabledKey)
        }
        if let volume = values[ATMAgentSoundPreferences.volumeKey] as? Double {
            UserDefaults.standard.set(min(max(volume, 0), 1), forKey: ATMAgentSoundPreferences.volumeKey)
        }
        for event in ATMAgentSoundEvent.allCases {
            let enabledKey = ATMAgentSoundPreferences.enabledKey(for: event)
            let soundKey = ATMAgentSoundPreferences.soundKey(for: event)
            if let enabled = values[enabledKey] as? Bool {
                UserDefaults.standard.set(enabled, forKey: enabledKey)
            }
            if let raw = values[soundKey] as? String, ATMAgentSound(rawValue: raw) != nil {
                UserDefaults.standard.set(raw, forKey: soundKey)
            }
        }
        if let enabled = values["ATMGlobalHotKeyEnabled"] as? Bool {
            launcher = enabled
            UserDefaults.standard.set(enabled, forKey: "LauncherEnabled")
        }
        if let raw = values["ATMGlobalHotKey"] as? String {
            let parts = raw.split(separator: ":")
            if parts.count == 2, let modifiers = UInt(parts[0]), let code = UInt16(parts[1]) {
                let flags = NSEvent.ModifierFlags(rawValue: modifiers)
                var carbon = 0
                if flags.contains(.command) { carbon |= 256 }
                if flags.contains(.shift) { carbon |= 512 }
                if flags.contains(.option) { carbon |= 2048 }
                if flags.contains(.control) { carbon |= 4096 }
                if carbon & (256 | 2048 | 4096) != 0 {
                    UserDefaults.standard.set(carbon, forKey: "LauncherModifiers")
                    UserDefaults.standard.set(Int(code), forKey: "LauncherKeyCode")
                }
            }
        }
        LauncherHotKey.shared.stop()
        launcherActive = launcher ? LauncherHotKey.shared.start() : false
        rebuildMenu()
        startPolling()
    }

    @objc private func quit() { NSApp.terminate(nil) }

    private func showError(_ message: String) {
        let alert = NSAlert()
        alert.messageText = "ATM"
        alert.informativeText = message
        alert.runModal()
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let objectID = response.notification.request.content.userInfo["object_id"] as? String
        let kind = response.notification.request.content.userInfo["kind"] as? String
        Task { @MainActor in
            switch CompanionNotificationDestination.resolve(kind: kind, objectID: objectID) {
            case .web(let route): self.openWeb(route: route)
            }
            completionHandler()
        }
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound, .list])
    }
}

private final class MenuRouteTarget: NSObject {
    let route: RuntimeRoute
    init(_ route: RuntimeRoute) { self.route = route }
}
