import AppKit
import SwiftUI
import ServiceManagement

@main
struct ATMVoiceApp: App {
    @NSApplicationDelegateAdaptor(VoiceDelegate.self) private var delegate
    var body: some Scene {
        WindowGroup("ATM Voice") { VoiceSettings().frame(minWidth: 620, minHeight: 640) }
            .windowResizability(.contentMinSize)
    }
}

@MainActor
final class VoiceDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    func applicationDidFinishLaunching(_ notification: Notification) {
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.button?.image = NSImage(systemSymbolName: "waveform", accessibilityDescription: "ATM Voice")
        let menu = NSMenu()
        let settings = menu.addItem(withTitle: "语音设置…", action: #selector(openSettings), keyEquivalent: ",")
        settings.target = self
        let copy = menu.addItem(withTitle: "复制最近转写", action: #selector(copyLast), keyEquivalent: "")
        copy.target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: "退出 ATM Voice", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        item.menu = menu
        statusItem = item
        let keys = ATMGlobalHotKeyManager.shared
        keys.onPressed = { action in
            switch action {
            case .voiceInput: ATMVoiceInputCoordinator.shared.hotKeyPressed()
            case .cancelVoice: ATMVoiceInputCoordinator.shared.cancel()
            }
        }
        keys.onReleased = { action in
            if action == .voiceInput { ATMVoiceInputCoordinator.shared.hotKeyReleased() }
        }
        keys.start()
    }
    func applicationWillTerminate(_ notification: Notification) {
        ATMVoiceInputCoordinator.shared.cancel()
        ATMGlobalHotKeyManager.shared.stop()
        ATMSenseVoiceModelManager.shared.cancelDownload()
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    @objc private func openSettings() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.canBecomeMain }) { window.makeKeyAndOrderFront(nil) }
    }
    @objc private func copyLast() {
        let text = ATMVoiceInputCoordinator.shared.lastTranscript
        guard !text.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

struct VoiceSettings: View {
    @AppStorage(ATMVoiceInputPreferences.hotKeyEnabledKey) private var enabled = true
    @AppStorage(ATMVoiceInputPreferences.hotKeyKey) private var hotKey = ATMVoiceInputPreferences.defaultHotKey.storageValue
    @AppStorage(ATMVoiceInputPreferences.engineKey) private var engine = ATMVoiceRecognitionEngine.senseVoice.rawValue
    @AppStorage(ATMVoiceInputPreferences.languageKey) private var language = ATMVoiceInputLanguage.auto.rawValue
    @AppStorage(ATMVoiceInputPreferences.onDeviceOnlyKey) private var localOnly = false
    @AppStorage(ATMVoiceInputPreferences.removeTrailingPeriodKey) private var removePeriod = false
    @AppStorage(ATMVoiceInputPreferences.dictionaryKey) private var dictionary = ""
    @AppStorage(LegacyVoiceImport.versionKey) private var importVersion = 0
    @ObservedObject private var model = ATMSenseVoiceModelManager.shared
    @ObservedObject private var keys = ATMGlobalHotKeyManager.shared
    @ObservedObject private var coordinator = ATMVoiceInputCoordinator.shared
    @State private var permission = ATMVoicePermissionSnapshot.current()
    @State private var importing = false
    @State private var importMessage = ""
    @State private var importDomain = LegacyVoiceImport.domains[0]
    @State private var loginEnabled = SMAppService.mainApp.status == .enabled

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("ATM Voice").font(.largeTitle.bold())
                    Text("按住快捷键说话，松开后写入刚才的应用。无需 ATM 服务。").foregroundStyle(.secondary)
                }
                GroupBox("快捷键") {
                    VStack(alignment: .leading, spacing: 10) {
                        Toggle("启用全局语音输入", isOn: $enabled)
                        Toggle("登录时启动 ATM Voice", isOn: Binding(get: { loginEnabled }, set: { value in
                            do {
                                if value { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
                                loginEnabled = SMAppService.mainApp.status == .enabled
                            } catch { importMessage = error.localizedDescription }
                        }))
                        ATMHotKeyRecorder(hotKey: Binding(get: { ATMHotKey(storageValue: hotKey) ?? ATMVoiceInputPreferences.defaultHotKey }, set: { hotKey = $0.storageValue }), isEnabled: enabled)
                        if case .unavailable = keys.registration(for: .voiceInput) {
                            Text("快捷键被其他应用占用。请关闭旧 ATM 的语音输入，或选择不同快捷键。").foregroundStyle(.orange)
                            Button("重新注册") { keys.stop(); keys.start() }
                        }
                        Text("录音中按 Esc 取消；每次最长 60 秒。最近转写只保存在本次进程内。").font(.footnote).foregroundStyle(.secondary)
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
                GroupBox("识别") {
                    VStack(alignment: .leading, spacing: 12) {
                        Picker("首选引擎", selection: $engine) { ForEach(ATMVoiceRecognitionEngine.allCases) { Text($0.label).tag($0.rawValue) } }
                        Picker("语言", selection: $language) { ForEach(ATMVoiceInputLanguage.allCases) { Text($0.label).tag($0.rawValue) } }
                        Toggle("Apple Speech 仅在本机识别", isOn: $localOnly)
                        Text(engineDescription).font(.footnote).foregroundStyle(.secondary)
                        HStack {
                            Text(modelDescription)
                            Spacer()
                            switch model.state {
                            case .downloading(let progress): ProgressView(value: progress).frame(width: 120); Button("取消") { model.cancelDownload() }
                            case .installing: ProgressView().controlSize(.small)
                            case .ready: Button("移除独立模型", role: .destructive) { model.deleteModel() }.disabled(coordinator.state.isActive)
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
                        Button("重新检查") { permission = .current() }
                        Text("首次按住快捷键时申请录音权限；缺少辅助功能权限时，转写会保留在剪贴板，可手动粘贴。").font(.footnote).foregroundStyle(.secondary)
                    }.padding(8)
                }
                if importVersion == 0 {
                    GroupBox("从旧 ATM 导入") {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("复制语音偏好与完整模型，不包含任务、会话或转写历史；旧数据保留。").font(.footnote).foregroundStyle(.secondary)
                            Picker("来源", selection: $importDomain) {
                                Text("ATM 正式版").tag(LegacyVoiceImport.domains[0])
                                Text("ATM Dev").tag(LegacyVoiceImport.domains[1])
                            }
                            HStack { Button("导入设置与模型") { importLegacy() }.disabled(importing || coordinator.state.isActive); Button("跳过") { importVersion = 1 }.disabled(importing); if importing { ProgressView().controlSize(.small) } }
                            if !importMessage.isEmpty { Text(importMessage).font(.footnote).foregroundStyle(.secondary) }
                        }.padding(8)
                    }
                } else if !importMessage.isEmpty { Text(importMessage).font(.footnote).foregroundStyle(.secondary) }
            }.padding(28)
        }.onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in permission = .current(); loginEnabled = SMAppService.mainApp.status == .enabled }
    }

    private var engineDescription: String {
        if engine == ATMVoiceRecognitionEngine.senseVoice.rawValue, model.isModelReady { return "当前使用 SenseVoice 本地模型；识别音频不发送到网络。" }
        let prefix = engine == ATMVoiceRecognitionEngine.senseVoice.rawValue ? "SenseVoice 模型未就绪，当前回退到 Apple Speech。" : "当前使用 Apple Speech。"
        return prefix + (localOnly ? "已限定本机识别；不支持的语言会报错，不转为远程识别。" : "未限定本机识别，Apple 可能通过网络处理音频。")
    }
    private var modelDescription: String {
        switch model.state {
        case .missing: return "本地模型未下载（约 160 MB）"
        case .downloading: return "下载中"
        case .installing: return "校验并安装中"
        case .ready: return "独立本地模型已就绪"
        case .failed(let message): return message
        }
    }
    private func permissionRow(_ name: String, _ status: ATMVoicePermissions.Status, _ pane: ATMVoicePermissions.Pane) -> some View {
        HStack { Text(name); Spacer(); Text(status == .granted ? "已授权" : (status == .denied ? "未授权" : "尚未请求")).foregroundStyle(.secondary); Button("系统设置") { ATMVoicePermissions.openSystemSettings(pane) } }
    }
    private func importLegacy() {
        importing = true
        let preferences = LegacyVoiceImport.preferences(in: importDomain)
        let source = LegacyVoiceImport.legacyModelDirectory
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
