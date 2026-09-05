import AppKit
import SwiftUI
import ServiceManagement

@main
struct VoxCaretApp: App {
    @NSApplicationDelegateAdaptor(VoiceDelegate.self) private var delegate
    var body: some Scene {
        WindowGroup("VoxCaret 声标") { VoiceSettings().frame(minWidth: 620, minHeight: 640) }
            .windowResizability(.contentMinSize)
    }
}

@MainActor
final class VoiceDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    func applicationDidFinishLaunching(_ notification: Notification) {
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.button?.image = VoxCaretBrand.statusIcon()
        item.button?.toolTip = VoxCaretBrand.displayName
        let menu = NSMenu()
        let brand = NSMenuItem(title: VoxCaretBrand.displayName, action: nil, keyEquivalent: "")
        brand.image = VoxCaretBrand.statusIcon()
        brand.isEnabled = false
        menu.addItem(brand)
        menu.addItem(.separator())
        let settings = menu.addItem(withTitle: "打开声标设置…", action: #selector(openSettings), keyEquivalent: ",")
        settings.target = self
        let copy = menu.addItem(withTitle: "复制最近转写", action: #selector(copyLast), keyEquivalent: "")
        copy.target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: "退出声标", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        item.menu = menu
        statusItem = item
        let keys = VoxCaretGlobalHotKeyManager.shared
        keys.onPressed = { action in
            switch action {
            case .voiceInput: VoxCaretInputCoordinator.shared.hotKeyPressed()
            case .cancelVoice: VoxCaretInputCoordinator.shared.cancel()
            }
        }
        keys.onReleased = { action in
            if action == .voiceInput { VoxCaretInputCoordinator.shared.hotKeyReleased() }
        }
        keys.start()
        VoxCaretRightCommandHoldMonitor.shared.start()
    }
    func applicationWillTerminate(_ notification: Notification) {
        VoxCaretInputCoordinator.shared.cancel()
        VoxCaretGlobalHotKeyManager.shared.stop()
        VoxCaretRightCommandHoldMonitor.shared.stop()
        VoxCaretSenseVoiceModelManager.shared.cancelDownload()
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    @objc private func openSettings() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.canBecomeMain }) { window.makeKeyAndOrderFront(nil) }
    }
    @objc private func copyLast() {
        let text = VoxCaretInputCoordinator.shared.lastTranscript
        guard !text.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

struct VoiceSettings: View {
    @AppStorage(VoxCaretInputPreferences.hotKeyEnabledKey) private var enabled = true
    @AppStorage(VoxCaretInputPreferences.hotKeyKey) private var hotKey = VoxCaretInputPreferences.defaultHotKey.storageValue
    @AppStorage(VoxCaretInputPreferences.engineKey) private var engine = VoxCaretRecognitionEngine.senseVoice.rawValue
    @AppStorage(VoxCaretInputPreferences.languageKey) private var language = VoxCaretInputLanguage.auto.rawValue
    @AppStorage(VoxCaretInputPreferences.onDeviceOnlyKey) private var localOnly = false
    @AppStorage(VoxCaretInputPreferences.removeTrailingPeriodKey) private var removePeriod = false
    @AppStorage(VoxCaretInputPreferences.dictionaryKey) private var dictionary = ""
    @AppStorage(VoxCaretInputPreferences.liveInsertionEnabledKey) private var liveInsertion = true
    @AppStorage(VoxCaretInputPreferences.rightCommandHoldEnabledKey) private var rightCommandHold = true
    @AppStorage(LegacyVoiceImport.versionKey) private var importVersion = 0
    @ObservedObject private var model = VoxCaretSenseVoiceModelManager.shared
    @ObservedObject private var keys = VoxCaretGlobalHotKeyManager.shared
    @ObservedObject private var coordinator = VoxCaretInputCoordinator.shared
    @State private var permission = VoxCaretPermissionSnapshot.current()
    @State private var importing = false
    @State private var importMessage = ""
    @State private var importDomain = LegacyVoiceImport.domains[0]
    @State private var loginEnabled = SMAppService.mainApp.status == .enabled

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                brandHeader
                GroupBox("快捷键") {
                    VStack(alignment: .leading, spacing: 10) {
                        Toggle("启用全局语音输入", isOn: $enabled)
                        Toggle("长按右 Command 直接说", isOn: $rightCommandHold)
                        Toggle("登录时启动 VoxCaret", isOn: Binding(get: { loginEnabled }, set: { value in
                            do {
                                if value { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
                                loginEnabled = SMAppService.mainApp.status == .enabled
                            } catch { importMessage = error.localizedDescription }
                        }))
                        VoxCaretHotKeyRecorder(hotKey: Binding(get: { VoxCaretHotKey(storageValue: hotKey) ?? VoxCaretInputPreferences.defaultHotKey }, set: { hotKey = $0.storageValue }), isEnabled: enabled)
                        if case .unavailable = keys.registration(for: .voiceInput) {
                            Text("快捷键被其他应用占用。请关闭旧版语音输入，或选择不同快捷键。").foregroundStyle(.orange)
                            Button("重新注册") { keys.stop(); keys.start() }
                        }
                        Text("长按右 Command 约 0.2 秒开始，短按不动作；也可使用下方备用快捷键。与闪电说同时运行时请关闭其中一个应用的右 Command 入口。")
                            .font(.footnote).foregroundStyle(.secondary)
                        Text("录音中按 Esc 取消；每次最长 5 分钟。最近转写只保存在本次进程内。").font(.footnote).foregroundStyle(.secondary)
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
                GroupBox("识别") {
                    VStack(alignment: .leading, spacing: 12) {
                        Picker("首选引擎", selection: $engine) { ForEach(VoxCaretRecognitionEngine.allCases) { Text($0.label).tag($0.rawValue) } }
                        Picker("语言", selection: $language) { ForEach(VoxCaretInputLanguage.allCases) { Text($0.label).tag($0.rawValue) } }
                        Toggle("实时输入（Apple Speech）", isOn: $liveInsertion)
                        Toggle("Apple Speech 仅在本机识别", isOn: $localOnly)
                        Text("开启时固定使用 Apple Speech，边说边改写目标输入框；关闭后按首选引擎在松手时一次写入。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        Text(engineDescription).font(.footnote).foregroundStyle(.secondary)
                        HStack {
                            Text(modelDescription)
                            Spacer()
                            switch model.state {
                            case .downloading(let progress): ProgressView(value: progress).frame(width: 120); Button("取消") { model.cancelDownload() }
                            case .installing: ProgressView().controlSize(.small)
                            case .ready:
                                if model.source?.isManagedByVoxCaret == true {
                                    Button("移除独立模型", role: .destructive) { model.deleteModel() }
                                        .disabled(coordinator.state.isActive)
                                } else {
                                    Text("零复制复用").font(.footnote).foregroundStyle(.secondary)
                                }
                            case .missing, .failed: Button("下载 SenseVoice 模型") { model.downloadModel() }
                            }
                        }
                    }.padding(8)
                }
                GroupBox("文本整理") {
                    VStack(alignment: .leading, spacing: 10) {
                        Toggle("去掉末尾句号", isOn: $removePeriod)
                        Text("词典：每行 原词=替换词").font(.footnote).foregroundStyle(.secondary)
                        TextEditor(text: $dictionary).font(.system(.body, design: .monospaced)).frame(height: 90).border(Color.secondary.opacity(0.2))
                        if !coordinator.lastTranscript.isEmpty {
                            Text(coordinator.lastTranscript).textSelection(.enabled).lineLimit(4)
                            Button("复制最近转写") { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(coordinator.lastTranscript, forType: .string) }
                        }
                    }.padding(8)
                }
                GroupBox("系统权限") {
                    VStack(alignment: .leading, spacing: 10) {
                        permissionRow("麦克风", permission.microphone, .microphone)
                        permissionRow("Apple 语音识别", permission.speechRecognition, .speechRecognition)
                        permissionRow("辅助功能（自动粘贴）", permission.accessibility, .accessibility)
                        HStack {
                            Button("重新检查") { permission = .current() }
                            if permission.accessibility != .granted {
                                Button("请求辅助功能权限") {
                                    VoxCaretTextInjector.requestAccessibilityPermission()
                                    permission = .current()
                                }
                            }
                        }
                        Text("首次按住快捷键时申请录音权限；缺少辅助功能权限时，转写会保留在剪贴板，可手动粘贴。").font(.footnote).foregroundStyle(.secondary)
                    }.padding(8)
                }
                if importVersion == 0 {
                    GroupBox("从旧版本导入") {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("复制语音偏好与完整模型，不包含任务、会话或转写历史；旧数据保留。").font(.footnote).foregroundStyle(.secondary)
                            Picker("来源", selection: $importDomain) {
                                ForEach(LegacyVoiceImport.sources) { source in
                                    Text(source.label).tag(source.domain)
                                }
                            }
                            HStack { Button("导入设置与模型") { importLegacy() }.disabled(importing || coordinator.state.isActive); Button("跳过") { importVersion = 1 }.disabled(importing); if importing { ProgressView().controlSize(.small) } }
                            if !importMessage.isEmpty { Text(importMessage).font(.footnote).foregroundStyle(.secondary) }
                        }.padding(8)
                    }
                } else if !importMessage.isEmpty { Text(importMessage).font(.footnote).foregroundStyle(.secondary) }
            }.padding(28)
        }.onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in permission = .current(); loginEnabled = SMAppService.mainApp.status == .enabled }
    }

    private var brandHeader: some View {
        HStack(spacing: 18) {
            Image(nsImage: NSApp.applicationIconImage)
                .resizable()
                .interpolation(.high)
                .frame(width: 72, height: 72)
                .shadow(color: VoxCaretTheme.indigo.opacity(0.24), radius: 10, y: 5)

            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(VoxCaretBrand.name)
                        .font(.largeTitle.bold())
                    Text(VoxCaretBrand.chineseName)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(VoxCaretTheme.accent)
                }
                Text("按住快捷键说话，松开后文字落在刚才的光标处。")
                    .foregroundStyle(.secondary)
                Text("语音 → 光标")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(VoxCaretTheme.accent)
                    .padding(.horizontal, 9)
                    .padding(.vertical, 4)
                    .background(VoxCaretTheme.accentFill, in: Capsule())
            }
            Spacer(minLength: 0)
        }
        .padding(18)
        .background(VoxCaretTheme.brandGradient, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(VoxCaretTheme.accent.opacity(0.16), lineWidth: 1)
        )
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(VoxCaretBrand.displayName)，\(VoxCaretBrand.tagline)")
    }

    private var engineDescription: String {
        if liveInsertion {
            return "实时输入已开启，当前使用 Apple Speech。" + (localOnly ? "已限定本机识别。" : "Apple 可能通过网络处理音频。")
        }
        if engine == VoxCaretRecognitionEngine.senseVoice.rawValue, model.isModelReady { return "当前使用 SenseVoice 本地模型；识别音频不发送到网络。" }
        let prefix = engine == VoxCaretRecognitionEngine.senseVoice.rawValue ? "SenseVoice 模型未就绪，当前回退到 Apple Speech。" : "当前使用 Apple Speech。"
        return prefix + (localOnly ? "已限定本机识别；不支持的语言会报错，不转为远程识别。" : "未限定本机识别，Apple 可能通过网络处理音频。")
    }
    private var modelDescription: String {
        switch model.state {
        case .missing: return "本地模型未下载（约 160 MB）"
        case .downloading: return "下载中"
        case .installing: return "校验并安装中"
        case .ready:
            return model.source == .compatibleLegacy
                ? "已复用本机兼容的 SenseVoice Small（无需下载）"
                : "独立本地模型已就绪"
        case .failed(let message): return message
        }
    }
    private func permissionRow(_ name: String, _ status: VoxCaretPermissions.Status, _ pane: VoxCaretPermissions.Pane) -> some View {
        HStack { Text(name); Spacer(); Text(status == .granted ? "已授权" : (status == .denied ? "未授权" : "尚未请求")).foregroundStyle(.secondary); Button("系统设置") { VoxCaretPermissions.openSystemSettings(pane) } }
    }
    private func importLegacy() {
        importing = true
        let preferences = LegacyVoiceImport.preferences(in: importDomain)
        guard let source = LegacyVoiceImport.modelDirectory(for: importDomain) else {
            importMessage = "无法识别旧版本来源。"
            importing = false
            return
        }
        let destination = model.modelDirectory
        Task { @MainActor in
            do {
                let copied = try await Task.detached(priority: .userInitiated) { try LegacyVoiceImport.copyModel(from: source, to: destination) }.value
                for (key, value) in preferences { UserDefaults.standard.set(value, forKey: key) }
                importVersion = 1
                model.refreshState()
                importMessage = copied ? "偏好和模型已复制，旧数据保留。" : "偏好已导入；模型无需复制，可按需下载。"
            } catch { importMessage = error.localizedDescription }
            importing = false
        }
    }
}
