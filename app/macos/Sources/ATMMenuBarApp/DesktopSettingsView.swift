import AppKit
import SwiftUI

enum ATMTodoListPreferences {
    static let showDroppedKey = "ATMShowDroppedTodos"
    static let defaultShowsDropped = false
}

enum ATMSettingsTab: String, CaseIterable, Identifiable {
    case general
    case shortcuts
    case voice
    case notify
    case todo
    case connectors

    var id: String { rawValue }

    var title: String {
        switch self {
        case .general: return "通用"
        case .shortcuts: return "快捷键"
        case .voice: return "语音"
        case .notify: return "通知"
        case .todo: return "Todo"
        case .connectors: return "连接器"
        }
    }

    var systemImage: String {
        switch self {
        case .general: return "slider.horizontal.3"
        case .shortcuts: return "keyboard"
        case .voice: return "waveform"
        case .notify: return "bell"
        case .todo: return "checklist"
        case .connectors: return "link"
        }
    }

    var subtitle: String {
        switch self {
        case .general: return "主题、字号与应用信息"
        case .shortcuts: return "查看全部快捷键并改绑"
        case .voice: return "识别、文本与权限"
        case .notify: return "通知、Hook 与声音反馈"
        case .todo: return "任务列表与默认行为"
        case .connectors: return "自动收集与外部来源"
        }
    }
}

struct DesktopSettingsView: View {

    @ObservedObject var store: ATMDataStore
    @ObservedObject private var appearance = ATMAppearance.shared
    @ObservedObject private var hotKeys = ATMGlobalHotKeyManager.shared
    @State private var selectedTab: ATMSettingsTab = .general
    @AppStorage(ATMTodoListPreferences.showDroppedKey)
    private var showsDropped = ATMTodoListPreferences.defaultShowsDropped
    @AppStorage(ATMGlobalHotKeyPreferences.enabledKey)
    private var globalHotKeyEnabled = ATMGlobalHotKeyPreferences.defaultEnabled
    @AppStorage(ATMGlobalHotKeyPreferences.hotKeyKey)
    private var globalHotKeyValue = ATMGlobalHotKeyPreferences.defaultHotKey.storageValue
    @AppStorage(ATMGlobalHotKeyPreferences.targetKey)
    private var globalHotKeyTarget = ATMGlobalHotKeyPreferences.defaultTarget.rawValue
    @ObservedObject private var voiceInput = ATMVoiceInputCoordinator.shared
    @ObservedObject private var senseVoiceModel = ATMSenseVoiceModelManager.shared
    @AppStorage(ATMVoiceInputPreferences.hotKeyEnabledKey)
    private var voiceHotKeyEnabled = ATMVoiceInputPreferences.defaultEnabled
    @AppStorage(ATMVoiceInputPreferences.hotKeyKey)
    private var voiceHotKeyValue = ATMVoiceInputPreferences.defaultHotKey.storageValue
    @AppStorage(ATMVoiceInputPreferences.engineKey)
    private var voiceEngine = ATMVoiceInputPreferences.defaultEngine.rawValue
    @AppStorage(ATMVoiceInputPreferences.languageKey)
    private var voiceLanguage = ATMVoiceInputPreferences.defaultLanguage.rawValue
    @AppStorage(ATMVoiceInputPreferences.onDeviceOnlyKey)
    private var voiceOnDeviceOnly = false
    @AppStorage(ATMVoiceInputPreferences.removeTrailingPeriodKey)
    private var voiceRemoveTrailingPeriod = false
    @AppStorage(ATMVoiceInputPreferences.dictionaryKey)
    private var voiceDictionary = ""
    /// Permissions are read on appear rather than observed: TCC has no change
    /// notification, and the trip to System Settings and back always passes through
    /// this view becoming visible again.
    @State private var voicePermissions = ATMVoicePermissionSnapshot.current()
    @AppStorage(ATMAgentAttentionNotifyPreferences.enabledKey)
    private var agentAttentionNotifyEnabled = ATMAgentAttentionNotifyPreferences.defaultEnabled
    @AppStorage(ATMAgentSoundPreferences.enabledKey)
    private var agentSoundsEnabled = ATMAgentSoundPreferences.defaultEnabled
    @AppStorage(ATMAgentSoundPreferences.volumeKey)
    private var agentSoundVolume = ATMAgentSoundPreferences.defaultVolume
    @AppStorage(ATMAgentSoundPreferences.enabledKey(for: .processingStarted))
    private var processingStartedSoundEnabled = ATMAgentSoundEvent.processingStarted.defaultEnabled
    @AppStorage(ATMAgentSoundPreferences.soundKey(for: .processingStarted))
    private var processingStartedSound = ATMAgentSoundEvent.processingStarted.defaultSound.rawValue
    @AppStorage(ATMAgentSoundPreferences.enabledKey(for: .attentionRequired))
    private var attentionRequiredSoundEnabled = ATMAgentSoundEvent.attentionRequired.defaultEnabled
    @AppStorage(ATMAgentSoundPreferences.soundKey(for: .attentionRequired))
    private var attentionRequiredSound = ATMAgentSoundEvent.attentionRequired.defaultSound.rawValue
    @AppStorage(ATMAgentSoundPreferences.enabledKey(for: .taskCompleted))
    private var taskCompletedSoundEnabled = ATMAgentSoundEvent.taskCompleted.defaultEnabled
    @AppStorage(ATMAgentSoundPreferences.soundKey(for: .taskCompleted))
    private var taskCompletedSound = ATMAgentSoundEvent.taskCompleted.defaultSound.rawValue

    private static let previewSample = """
        ## 迁移方案

        字号收敛成一层语义 token 后，下次微调只改一处。正文、次要说明和 `mono` 的 ID
        各自有固定档位，不再靠散落的字面量维持层级。

        - 任务描述、知识文档、共享记忆跟随此设置
        - 侧边栏、列表行、表格等界面文字保持固定
        """

    var body: some View {
        // The settings sidebar holds four short rows and no user content, so it
        // starts narrower than the task / knowledge lists: the space is worth more
        // to the settings forms on the right.
        ATMSplitColumn(
            id: "settings",
            defaultWidth: 232,
            minWidth: 208,
            maxWidth: 310,
            detailMinWidth: 560
        ) {
            settingsSidebar
        } detail: {
            settingsContent
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(ATMTheme.listPane)
    }

    private var settingsSidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 3) {
                Text("设置")
                    .font(ATMFont.font(.title2, weight: .bold))
                Text("应用偏好与外部连接")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }
            .padding(.horizontal, 20)
            .padding(.top, 24)
            .padding(.bottom, 18)

            Divider()

            VStack(spacing: 4) {
                ForEach(ATMSettingsTab.allCases) { tab in
                    Button {
                        selectedTab = tab
                    } label: {
                        settingsSidebarRow(tab)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(8)

            Spacer(minLength: 0)
        }
        .background(ATMTheme.listPane)
    }

    private func settingsSidebarRow(_ tab: ATMSettingsTab) -> some View {
        let isSelected = selectedTab == tab

        return HStack(spacing: 12) {
            Image(systemName: tab.systemImage)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(isSelected ? ATMTheme.accent : ATMTheme.secondary)
                .frame(width: 30, height: 30)
                .background(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .fill(isSelected ? ATMTheme.accent.opacity(0.12) : ATMTheme.surface)
                )

            VStack(alignment: .leading, spacing: 2) {
                Text(tab.title)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                Text(tab.subtitle)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }

            Spacer(minLength: 4)

            Image(systemName: "chevron.right")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(ATMTheme.secondary.opacity(0.65))
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .contentShape(Rectangle())
        .atmRowSurface(isSelected: isSelected)
    }

    private var settingsContent: some View {
        VStack(spacing: 0) {
            HStack(spacing: 13) {
                Image(systemName: selectedTab.systemImage)
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(ATMTheme.accent)
                    .frame(width: 36, height: 36)
                    .background(
                        RoundedRectangle(cornerRadius: 11, style: .continuous)
                            .fill(ATMTheme.accent.opacity(0.12))
                    )

                VStack(alignment: .leading, spacing: 3) {
                    Text(selectedTab.title)
                        .font(ATMFont.font(.title2, weight: .bold))
                    Text(selectedTab.subtitle)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }

                Spacer()
            }
            .padding(.horizontal, 24)
            .frame(height: 86)
            .background(ATMTheme.elevated)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(ATMTheme.border)
                    .frame(height: 1)
            }

            Group {
                switch selectedTab {
                case .general:
                    generalSettings
                case .shortcuts:
                    shortcutSettings
                case .voice:
                    voiceSettings
                case .notify:
                    notifySettings
                case .todo:
                    todoSettings
                case .connectors:
                    connectorSettings
                }
            }
        }
        // 设置内容是阅读/编辑画布，和任务、Agent 的详情栏一样保持清晰；
        // 冷中性色 listPane 只属于左侧分类抽屉。
        .background(ATMTheme.elevated)
    }

    private var generalSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card {
                    VStack(alignment: .leading, spacing: 12) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("主题")
                                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                            Text("应用到主窗口、菜单和菜单栏浮层。选择“跟随系统”后，ATM 会随 macOS 外观自动切换。")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }

                        LazyVGrid(
                            columns: [
                                // Never below the preview's fixed 184pt artwork,
                                // or a narrow column would clip it again.
                                GridItem(.adaptive(minimum: 184, maximum: 220), spacing: 14)
                            ],
                            alignment: .leading,
                            spacing: 14
                        ) {
                            ForEach(ATMThemeMode.allCases) { mode in
                                ATMThemeChoiceButton(
                                    mode: mode,
                                    isSelected: appearance.themeMode == mode
                                ) {
                                    appearance.themeMode = mode
                                }
                            }
                        }
                    }
                }

                card {
                    VStack(alignment: .leading, spacing: 12) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("正文字号")
                                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                            Text("只影响长文阅读区：任务描述、任务进展、知识文档、共享记忆、Agent 会话回复。侧边栏、列表、表格等界面文字保持固定尺寸。")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }

                        Picker("正文字号", selection: $appearance.contentTextSize) {
                            ForEach(ATMContentTextSize.allCases) { size in
                                Text("\(size.label) · \(Int(size.pointSize))pt").tag(size)
                            }
                        }
                        .labelsHidden()
                        .pickerStyle(.segmented)
                        .frame(maxWidth: 520)

                        Divider()
                            .padding(.vertical, 4)

                        Text("预览")
                            .font(ATMFont.font(.caption, weight: .semibold))
                            .foregroundStyle(ATMTheme.secondary)
                        // Rendered by the same view the real content uses, so the
                        // preview cannot drift from what the setting actually does.
                        ATMMarkdownContentView(source: Self.previewSample)
                    }
                }

                card { aboutSection }

                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var aboutSection: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text("关于 ATM")
                .font(ATMFont.font(.bodyLarge, weight: .semibold))
            Text(appVersionLabel)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var appVersionLabel: String {
        let info = Bundle.main.infoDictionary
        guard let version = info?["CFBundleShortVersionString"] as? String, !version.isEmpty else {
            return "开发版"
        }
        guard let build = info?["CFBundleVersion"] as? String, !build.isEmpty else {
            return "版本 \(version)"
        }
        return "版本 \(version) · 构建 \(build)"
    }

    // MARK: - 快捷键

    private var shortcutSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card { globalHotKeySection }
                card { voiceShortcutSection }

                ForEach(ATMShortcutCatalog.groups) { group in
                    card { shortcutReferenceGroup(group) }
                }

                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func shortcutReferenceGroup(_ group: ATMShortcutGroup) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 4) {
                Text(group.title)
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                Text(group.detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.bottom, 8)

            ForEach(Array(group.shortcuts.enumerated()), id: \.element.id) { index, shortcut in
                if index > 0 { Divider() }
                HStack(alignment: .center, spacing: 18) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(shortcut.title)
                            .font(ATMFont.font(.body, weight: .medium))
                        Text(shortcut.detail)
                            .font(ATMFont.caption)
                            .foregroundStyle(ATMTheme.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    Spacer(minLength: 12)

                    Text(shortcut.keys)
                        .font(ATMFont.mono(.footnote, .semibold))
                        .foregroundStyle(ATMTheme.primary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(
                            ATMTheme.controlFill,
                            in: RoundedRectangle(cornerRadius: 7, style: .continuous)
                        )
                        .overlay {
                            RoundedRectangle(cornerRadius: 7, style: .continuous)
                                .stroke(ATMTheme.border, lineWidth: 1)
                        }
                        .accessibilityLabel("快捷键 \(shortcut.keys)")
                }
                .padding(.vertical, 9)
            }
        }
    }

    private var globalHotKeySection: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("全局快捷键")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("在任何应用中按一次呼出 ATM，再按一次收起。默认 ⌥⌘A；点击下方按钮后按新的组合即可改绑。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Toggle("启用全局快捷键", isOn: $globalHotKeyEnabled)
                    .labelsHidden()
                    .toggleStyle(.switch)
            }

            HStack(spacing: 14) {
                ATMHotKeyRecorder(hotKey: globalHotKeyBinding, isEnabled: globalHotKeyEnabled)

                Picker("呼出", selection: $globalHotKeyTarget) {
                    ForEach(ATMGlobalHotKeyTarget.allCases) { target in
                        Text(target.label).tag(target.rawValue)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .frame(width: 200)
                .disabled(!globalHotKeyEnabled)
            }

            // A combination another app already owns cannot be registered, and the
            // only symptom is a shortcut that does nothing — so say so here rather
            // than leaving it to be discovered by pressing keys.
            if globalHotKeyEnabled,
               case .unavailable(let hotKey) = hotKeys.registration(for: .launcher) {
                Text("\(hotKey.displayString) 已被系统或其他应用占用，请换一个组合。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// `@AppStorage` only holds the serialized form; an unreadable value falls
    /// back to the default so the recorder always has something to show.
    private var globalHotKeyBinding: Binding<ATMHotKey> {
        Binding(
            get: { ATMHotKey(storageValue: globalHotKeyValue) ?? ATMGlobalHotKeyPreferences.defaultHotKey },
            set: { globalHotKeyValue = $0.storageValue }
        )
    }

    // MARK: - 语音

    /// The 语音 tab owns recognition and text handling. Its configurable trigger
    /// lives with every other binding in 快捷键 so there is one place to discover
    /// and change keyboard behavior.
    private var voiceSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card { voiceEngineSection }
                card { voiceTextSection }
                card { voicePermissionSection }
                if !voiceInput.lastTranscript.isEmpty {
                    card { voiceLastTranscriptSection }
                }
                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .onAppear { voicePermissions = ATMVoicePermissionSnapshot.current() }
    }

    private var voiceShortcutSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("语音输入快捷键")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("在任何应用里按住不放开始说话，松手把文字写进当前光标处；说错了按 ⎋ 取消，不会写入任何东西。默认 ⌥Space。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Toggle("启用语音输入", isOn: $voiceHotKeyEnabled)
                    .labelsHidden()
                    .toggleStyle(.switch)
            }

            ATMHotKeyRecorder(
                hotKey: voiceHotKeyBinding,
                isEnabled: voiceHotKeyEnabled,
                defaultHotKey: ATMVoiceInputPreferences.defaultHotKey
            )

            if voiceHotKeyEnabled,
               case .unavailable(let hotKey) = hotKeys.registration(for: .voiceInput) {
                Text("\(hotKey.displayString) 已被系统或其他应用占用（⌥Space 常被输入法拿走），请换一个组合。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var voiceEngineSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text("识别引擎")
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                Text(ATMVoiceRecognitionEngine(rawValue: voiceEngine)?.detail ?? "")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Picker("识别引擎", selection: $voiceEngine) {
                ForEach(ATMVoiceRecognitionEngine.allCases) { engine in
                    Text(engine.label).tag(engine.rawValue)
                }
            }
            .labelsHidden()
            .pickerStyle(.segmented)
            .frame(maxWidth: 420)

            if voiceEngine == ATMVoiceRecognitionEngine.senseVoice.rawValue {
                senseVoiceModelCard
            }

            Divider()

            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("识别语言")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("同时决定两个引擎听哪种语言。「自动」对 SenseVoice 是真的自动判别，对 Apple Speech 是跟随系统语言。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Picker("识别语言", selection: $voiceLanguage) {
                    ForEach(ATMVoiceInputLanguage.allCases) { language in
                        Text(language.label).tag(language.rawValue)
                    }
                }
                .labelsHidden()
                .frame(width: 150)
            }

            if voiceEngine == ATMVoiceRecognitionEngine.appleSpeech.rawValue {
                Divider()
                HStack(alignment: .top, spacing: 16) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("仅使用本地识别")
                            .font(ATMFont.font(.body, weight: .semibold))
                        Text("音频不离开这台机器。部分语言不支持设备端识别，那种情况下开着这个开关会直接报错而不是偷偷联网。")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 12)
                    Toggle("仅使用本地识别", isOn: $voiceOnDeviceOnly)
                        .labelsHidden()
                        .toggleStyle(.switch)
                }
            }
        }
    }

    /// The model is 160MB, so its state is the one thing on this page worth a card of
    /// its own — "为什么识别的是 Apple Speech" is almost always answered here.
    @ViewBuilder
    private var senseVoiceModelCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch senseVoiceModel.state {
            case .missing:
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("模型未下载")
                            .font(ATMFont.font(.body, weight: .semibold))
                            .foregroundStyle(ATMTheme.warning)
                        Text("下载前语音输入自动使用 Apple Speech。约 160MB，解压后约 230MB。")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    Spacer(minLength: 8)
                    Button("下载模型") { senseVoiceModel.downloadModel() }
                }
            case .downloading(let progress):
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text("正在下载模型 · \(Int(progress * 100))%")
                            .font(ATMFont.font(.body, weight: .semibold))
                        Spacer(minLength: 8)
                        Button("取消") { senseVoiceModel.cancelDownload() }
                    }
                    ProgressView(value: progress)
                }
            case .installing:
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text("正在校验并解压…")
                        .font(ATMFont.body)
                }
            case .ready:
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("模型已就绪")
                            .font(ATMFont.font(.body, weight: .semibold))
                            .foregroundStyle(ATMTheme.success)
                        Text(senseVoiceModel.modelDirectory.path)
                            .font(ATMFont.mono(.caption))
                            .foregroundStyle(ATMTheme.secondary)
                            .textSelection(.enabled)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    Spacer(minLength: 8)
                    Button("删除模型") { senseVoiceModel.deleteModel() }
                }
            case .failed(let message):
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("模型未就绪")
                            .font(ATMFont.font(.body, weight: .semibold))
                            .foregroundStyle(ATMTheme.danger)
                        Text(message)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 8)
                    Button("重新下载") { senseVoiceModel.downloadModel() }
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private var voiceTextSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("去掉句尾句号")
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text("口述多半落在聊天框、提交信息、提示词里，识别器补的那个句号每次都要手删一下。只去一个，「等一下。。。」保持原样。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Toggle("去掉句尾句号", isOn: $voiceRemoveTrailingPeriod)
                    .labelsHidden()
                    .toggleStyle(.switch)
            }

            Divider()

            VStack(alignment: .leading, spacing: 8) {
                Text("替换词典")
                    .font(ATMFont.font(.body, weight: .semibold))
                Text("一行一条 `原词 => 替换词`，`→` 和 `=` 也认，`#` 开头是注释。专有名词两侧都会喂给 Apple Speech 当上下文提示，让它更可能一次就说对。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                TextEditor(text: $voiceDictionary)
                    .font(ATMFont.mono(.body))
                    .frame(height: 120)
                    .padding(6)
                    .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(ATMTheme.border, lineWidth: 1)
                    )
                    .frame(maxWidth: 520)

                Text("当前有效 \(ATMVoiceTextCleanup.parseReplacements(voiceDictionary).count) 条")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
    }

    /// Three permissions, three different failure modes. Listed together because
    /// "dictation did nothing" is otherwise indistinguishable between them — and
    /// because ATM is signed ad hoc, so 辅助功能 genuinely does lapse after a rebuild.
    private var voicePermissionSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("权限")
                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                Text("辅助功能是用来模拟一次 ⌘V 的。没有它语音输入仍然可用，只是文字停在剪贴板上，要自己按 ⌘V。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            // Above the three rows, because in this mode none of them can be acted on:
            // 打开设置 leads to a list that will not contain this process.
            if !ATMAppBundle.isBundled {
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(ATMTheme.warning)
                        .font(.system(size: 12))
                    Text("当前不是以 .app 运行（`swift run` 的裸可执行文件），系统弹不出授权框，语音输入用不了。改用 `Scripts/run-dev-app.sh`，下面这几行状态在那之后才有意义。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ATMTheme.warningFill, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            }

            voicePermissionRow(
                title: "麦克风",
                detail: "录音的前提。",
                status: voicePermissions.microphone,
                pane: .microphone
            )
            voicePermissionRow(
                title: "语音识别",
                detail: "只有 Apple Speech 需要；SenseVoice 不用。",
                status: voicePermissions.speechRecognition,
                pane: .speechRecognition
            )
            voicePermissionRow(
                title: "辅助功能",
                detail: "自动粘贴到当前应用。",
                status: voicePermissions.accessibility,
                pane: .accessibility
            )

            HStack(spacing: 10) {
                Button("重新检查") { voicePermissions = ATMVoicePermissionSnapshot.current() }
                if voicePermissions.accessibility != .granted {
                    Button("请求辅助功能权限") {
                        ATMTextInjector.requestAccessibilityPermission()
                    }
                }
            }
            .font(ATMFont.footnote)
        }
    }

    private func voicePermissionRow(
        title: String,
        detail: String,
        status: ATMVoicePermissions.Status,
        pane: ATMVoicePermissions.Pane
    ) -> some View {
        HStack(spacing: 12) {
            Image(systemName: status == .granted ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                .foregroundStyle(status == .granted ? ATMTheme.success : ATMTheme.warning)
                .font(.system(size: 13))

            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(ATMFont.font(.body, weight: .medium))
                Text(detail)
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
            }

            Spacer(minLength: 8)

            Text(statusLabel(status))
                .font(ATMFont.caption)
                .foregroundStyle(status == .granted ? ATMTheme.success : ATMTheme.warning)

            if status != .granted {
                Button("打开设置") { ATMVoicePermissions.openSystemSettings(pane) }
                    .buttonStyle(.link)
                    .font(ATMFont.footnote)
            }
        }
        .padding(.vertical, 2)
    }

    private func statusLabel(_ status: ATMVoicePermissions.Status) -> String {
        switch status {
        case .granted: return "已授权"
        case .denied: return "已拒绝"
        case .notDetermined: return "未询问"
        }
    }

    /// The last transcript, kept for copying. The reason it exists: when the paste
    /// fails, this is the only remaining copy of what was said.
    private var voiceLastTranscriptSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("上一次转写")
                    .font(ATMFont.font(.body, weight: .semibold))
                Spacer(minLength: 8)
                Button("复制") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(voiceInput.lastTranscript, forType: .string)
                }
                .font(ATMFont.footnote)
            }
            Text(voiceInput.lastTranscript)
                .font(ATMFont.body)
                .foregroundStyle(ATMTheme.secondary)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var voiceHotKeyBinding: Binding<ATMHotKey> {
        Binding(
            get: { ATMHotKey(storageValue: voiceHotKeyValue) ?? ATMVoiceInputPreferences.defaultHotKey },
            set: { voiceHotKeyValue = $0.storageValue }
        )
    }

    /// The 通知 tab: how ATM interrupts you at all — the banner it raises when an
    /// agent is blocked, the event-push hooks that are the only signal trusted to
    /// raise it, and the state-change sounds that cover everything not worth a
    /// banner.
    private var notifySettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card { agentAttentionNotifySection }

                card { agentHookSection }

                card {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("Agent 提示音")
                                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                                Text("默认使用 Ping Island 的 8-bit 提示音；按状态变化播放一次，不会跟随每 3 秒刷新重复响。")
                                    .font(ATMFont.footnote)
                                    .foregroundStyle(ATMTheme.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            Spacer(minLength: 12)
                            Toggle("启用提示音", isOn: $agentSoundsEnabled)
                                .labelsHidden()
                                .toggleStyle(.switch)
                                .controlSize(.small)
                        }

                        HStack(spacing: 12) {
                            Image(systemName: "speaker.fill")
                                .foregroundStyle(ATMTheme.secondary)
                            Slider(value: $agentSoundVolume, in: 0...1)
                                .frame(maxWidth: 260)
                            Image(systemName: "speaker.wave.3.fill")
                                .foregroundStyle(ATMTheme.secondary)
                            Text("\(Int(agentSoundVolume * 100))%")
                                .font(ATMFont.mono(.caption, .medium))
                                .foregroundStyle(ATMTheme.secondary)
                                .frame(width: 40, alignment: .trailing)
                        }
                        .disabled(!agentSoundsEnabled)

                        Divider()

                        agentSoundEventRow(
                            event: .processingStarted,
                            isEnabled: $processingStartedSoundEnabled,
                            selectedSound: $processingStartedSound
                        )
                        agentSoundEventRow(
                            event: .attentionRequired,
                            isEnabled: $attentionRequiredSoundEnabled,
                            selectedSound: $attentionRequiredSound
                        )
                        agentSoundEventRow(
                            event: .taskCompleted,
                            isEnabled: $taskCompletedSoundEnabled,
                            selectedSound: $taskCompletedSound
                        )
                    }
                }

                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func agentSoundEventRow(
        event: ATMAgentSoundEvent,
        isEnabled: Binding<Bool>,
        selectedSound: Binding<String>
    ) -> some View {
        HStack(alignment: .center, spacing: 14) {
            Toggle("", isOn: isEnabled)
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(.mini)

            VStack(alignment: .leading, spacing: 2) {
                Text(event.title)
                    .font(ATMFont.font(.body, weight: .medium))
                Text(event.subtitle)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
            }

            Spacer(minLength: 12)

            Picker(event.title, selection: selectedSound) {
                ForEach(ATMAgentSound.allCases) { sound in
                    Text(sound.title).tag(sound.rawValue)
                }
            }
            .labelsHidden()
            .frame(width: 210)

            Button {
                let sound = ATMAgentSound(rawValue: selectedSound.wrappedValue)
                    ?? event.defaultSound
                ATMAgentSoundPlayer.shared.preview(sound, volume: Float(agentSoundVolume))
            } label: {
                Image(systemName: "play.fill")
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .frame(width: 24, height: 24)
            }
            .buttonStyle(.borderless)
            .help("预听\(event.title)提示音")
            .disabled(!agentSoundsEnabled || !isEnabled.wrappedValue)
        }
        .disabled(!agentSoundsEnabled)
    }

    private var connectorSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack {
                            VStack(alignment: .leading, spacing: 4) {
                                Text("自动收集")
                                    .font(ATMFont.font(.bodyLarge, weight: .semibold))
                                Text("ATM 在菜单栏常驻期间按间隔读取已启用来源，并为新事项创建 Todo。")
                                    .font(ATMFont.footnote)
                                    .foregroundStyle(ATMTheme.secondary)
                            }
                            Spacer()
                            Toggle(
                                "启用",
                                isOn: Binding(
                                    get: { store.collectionOverview.enabled },
                                    set: { store.setCollectionEnabled($0) }
                                )
                            )
                            .toggleStyle(.switch)
                            .labelsHidden()
                        }

                        Divider()

                        if store.collectionOverview.connectorHealth.isEmpty {
                            Label("尚未配置连接器", systemImage: "link.badge.plus")
                                .foregroundStyle(ATMTheme.secondary)
                        } else {
                            ForEach(store.collectionOverview.connectorHealth, id: \.connector) { health in
                                HStack {
                                    Text(health.connector)
                                        .font(ATMFont.mono(.footnote))
                                        .frame(width: 80, alignment: .leading)
                                    Label(health.statusLabel, systemImage: health.statusIcon)
                                        .foregroundStyle(ATMTheme.collectionHealthColor(health.status))
                                        .font(ATMFont.font(.body, weight: .medium))
                                    Spacer()
                                    if let checkedAt = health.checkedAt {
                                        Text("检测于 \(collectionSettingsRelativeTime(checkedAt))")
                                            .font(ATMFont.caption)
                                            .foregroundStyle(ATMTheme.secondary)
                                    } else {
                                        Text("请立即收集一次")
                                            .font(ATMFont.caption)
                                            .foregroundStyle(ATMTheme.secondary)
                                    }
                                }
                                if let healthError = health.error, !healthError.isEmpty {
                                    Text(healthError)
                                        .font(ATMFont.footnote)
                                        .foregroundStyle(ATMTheme.secondary)
                                        .textSelection(.enabled)
                                        .padding(.leading, 80)
                                }
                            }
                        }

                        Stepper(
                            "采集间隔：\(store.collectionOverview.intervalMinutes) 分钟",
                            value: Binding(
                                get: { store.collectionOverview.intervalMinutes },
                                set: { store.setCollectionInterval($0) }
                            ),
                            in: 1...60
                        )
                        .font(ATMFont.body)

                        HStack {
                            Text("分类模型")
                                .foregroundStyle(ATMTheme.secondary)
                                .frame(width: 80, alignment: .leading)
                            Text(store.collectionOverview.modelCommand)
                                .font(ATMFont.mono(.footnote))
                                .textSelection(.enabled)
                        }
                    }
                }

                card {
                    VStack(alignment: .leading, spacing: 12) {
                        HStack {
                            Text("钉钉来源")
                                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                            Spacer()
                            Button("刷新") { store.refreshCollection() }
                                .controlSize(.small)
                        }
                        if store.collectionOverview.sources.isEmpty {
                            Text("尚未配置来源。请在主窗口“收集”页添加群聊或联系人。")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                        } else {
                            ForEach(store.collectionOverview.sources) { source in
                                HStack {
                                    Image(systemName: source.symbolName)
                                        .frame(width: 20)
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(source.displayName)
                                            .font(ATMFont.font(.body, weight: .medium))
                                        Text(source.project?.isEmpty == false ? source.project! : "未映射项目")
                                            .font(ATMFont.caption)
                                            .foregroundStyle(ATMTheme.secondary)
                                        Text(source.effectiveStrategy == "observe"
                                            ? "只沉淀知识 · 每 \(source.effectiveIntervalMinutes) 分钟"
                                            : "任务提取 · 每 \(source.effectiveIntervalMinutes) 分钟")
                                            .font(ATMFont.caption)
                                            .foregroundStyle(ATMTheme.secondary)
                                        if let exclusion = source.excludePattern, !exclusion.isEmpty {
                                            Text("排除：\(exclusion)")
                                                .font(ATMFont.caption)
                                                .foregroundStyle(ATMTheme.secondary)
                                                .lineLimit(1)
                                        }
                                    }
                                    Spacer()
                                    Toggle(
                                        "启用",
                                        isOn: Binding(
                                            get: { source.enabled },
                                            set: { store.setCollectionSource(source, enabled: $0) }
                                        )
                                    )
                                    .toggleStyle(.switch)
                                    .labelsHidden()
                                    .controlSize(.small)
                                }
                                if source.id != store.collectionOverview.sources.last?.id { Divider() }
                            }
                        }
                    }
                }

                if let error = store.collectionErrorMessage, !error.isEmpty {
                    card {
                        Label(error, systemImage: "exclamationmark.triangle.fill")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.warning)
                            .textSelection(.enabled)
                    }
                }
            }
            .padding(24)
        }
        .onAppear { store.refreshCollection() }
    }

    private var todoSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card {
                    VStack(alignment: .leading, spacing: 12) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("任务列表")
                                .font(ATMFont.font(.bodyLarge, weight: .semibold))
                            Text("控制关闭态任务在桌面任务列表中的显示方式。")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                        }

                        Toggle("显示已废弃", isOn: $showsDropped)
                            .toggleStyle(.switch)
                            .controlSize(.small)
                            .font(ATMFont.font(.body, weight: .medium))

                        Text("关闭后仅从任务列表隐藏已放弃任务，不会删除任务或修改状态。")
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }

                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// The one interruption ATM allows itself.
    ///
    /// A single switch on purpose. There is no per-reason granularity and no
    /// quiet-hours of ATM's own: Notification Center already runs 专注模式 and
    /// Do Not Disturb, and a second set of rules here could only disagree with
    /// the first.
    @ViewBuilder
    private var agentAttentionNotifySection: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Agent 需要你时通知")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("Agent 卡在等待授权、等待输入或等待选择时发一条系统通知，点击直接跳到它所在的终端；Agent 继续往下走之后通知自动撤回。遵循系统的专注模式与「勿扰」。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Toggle("通知", isOn: $agentAttentionNotifyEnabled)
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .controlSize(.small)
            }

            Divider()

            Text("只在下面的 hook 报出确切原因时触发。没有 hook 的 agent 靠关键词猜「是不是在等你」，那种推测会误报，所以它只计入菜单栏的「需要你」计数和 Agent 页，不发通知。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)

            Text("跑完一轮不发通知——那不是被挡住，用提示音就够。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// Hook wiring: the event source behind every notification above.
    ///
    /// Without hooks ATM has to infer "this session needs you" by keyword-matching
    /// the agent's last message, which cannot see a tool call blocked on a
    /// permission prompt at all. This section is how that inference gets replaced
    /// by the agent telling us directly.
    @ViewBuilder
    private var agentHookSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Agent 事件推送")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("装上 hook 后，Agent 卡在授权或等你输入的那一刻会立刻上报，不再靠每 3 秒扫一遍会话记录去猜——上面的通知也只信这个通道。装的都是只上报的 hook，不会拦住工具调用，也不会改 Agent 的行为。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                if store.isUpdatingAgentHooks {
                    ProgressView().controlSize(.small)
                }
            }

            HStack(spacing: 10) {
                Circle()
                    .fill(store.agentEvents.isListening ? ATMTheme.success : ATMTheme.secondary.opacity(0.5))
                    .frame(width: 7, height: 7)
                Text(socketStatusText)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            }

            if let error = store.agentEvents.startupError {
                Text(error)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }

            if let report = store.agentHookReport {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(report.sources) { source in
                        agentHookRow(source)
                    }
                }
            }

            if let error = store.agentHookErrorMessage {
                Text(error)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }

            HStack(spacing: 10) {
                Button("安装 hook") { store.installAgentHooks() }
                    .disabled(store.isUpdatingAgentHooks)
                Button("移除") { store.uninstallAgentHooks() }
                    .disabled(store.isUpdatingAgentHooks)
                Button("重新检测") { store.loadAgentHookStatus() }
                    .disabled(store.isUpdatingAgentHooks)
                Spacer()
            }
            .controlSize(.small)
        }
        .onAppear { store.loadAgentHookStatus() }
    }

    private var socketStatusText: String {
        guard store.agentEvents.isListening else {
            // Since the attention notifier owns the poll for the whole lifetime of
            // the app, the listener is up from launch. Seeing this now means it
            // failed to bind, not that some window is closed.
            return "事件通道未启动——socket 绑定失败，通知将退回每 3 秒扫描会话记录"
        }
        let path = store.agentEvents.socketPath ?? store.agentHookReport?.socketPath ?? ""
        if let lastEvent = store.agentEvents.lastEventAt {
            let seconds = Int(Date().timeIntervalSince(lastEvent))
            return "正在监听 \(path) · 最近一次事件 \(seconds) 秒前"
        }
        return "正在监听 \(path) · 尚未收到事件"
    }

    @ViewBuilder
    private func agentHookRow(_ source: ATMAgentHookSource) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: source.isFullyInstalled ? "checkmark.circle.fill" : "circle.dashed")
                .foregroundStyle(source.isFullyInstalled ? ATMTheme.success : ATMTheme.secondary)
                .font(ATMFont.caption)
            VStack(alignment: .leading, spacing: 2) {
                Text(source.displayName)
                    .font(ATMFont.font(.body, weight: .medium))
                if let manual = source.manual {
                    Text(manual)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                } else if let error = source.error {
                    Text(error)
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                } else if source.missing.isEmpty {
                    Text("已接入 \(source.installed.count) 个事件")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                } else {
                    Text("待接入：\(source.missing.joined(separator: "、"))")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ForEach(source.conflicts, id: \.self) { conflict in
                    // Worth surfacing: another tool already answers this event,
                    // so both would prompt for the same moment.
                    Text("另一个工具已占用 \(conflict)")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 8)
        }
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                ATMTheme.elevated,
                in: RoundedRectangle(cornerRadius: 12, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(ATMTheme.border, lineWidth: 1)
            }
    }

    private func collectionSettingsRelativeTime(_ timestamp: Int64) -> String {
        let elapsed = max(Int(Date().timeIntervalSince1970) - Int(timestamp), 0)
        if elapsed < 60 { return "刚刚" }
        if elapsed < 3_600 { return "\(elapsed / 60) 分钟前" }
        if elapsed < 86_400 { return "\(elapsed / 3_600) 小时前" }
        return "\(elapsed / 86_400) 天前"
    }
}

/// Visual theme selector modeled after Enchanted's appearance cards. A button is
/// used instead of a Picker so each option can show the interface it represents.
private struct ATMThemeChoiceButton: View {
    let mode: ATMThemeMode
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 8) {
                ATMThemePreview(mode: mode)
                    .overlay {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .stroke(
                                isSelected ? ATMTheme.accent : ATMTheme.border,
                                lineWidth: isSelected ? 2 : 1
                            )
                    }

                Text(mode.label)
                    .font(ATMFont.font(.body, weight: isSelected ? .semibold : .regular))
                    .foregroundStyle(ATMTheme.primary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity)
        .accessibilityLabel("\(mode.label)主题")
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}

private struct ATMThemePreview: View {
    let mode: ATMThemeMode

    private static let designWidth: CGFloat = 184
    private static let designHeight: CGFloat = 108
    private static let resourceBundle: Bundle = {
        let name = "ATMMenuBarApp_ATMMenuBarApp.bundle"
        let candidates = [
            Bundle.main.resourceURL,
            Bundle.main.bundleURL,
            Bundle.main.executableURL?.deletingLastPathComponent(),
        ]
        for baseURL in candidates.compactMap({ $0 }) {
            if let bundle = Bundle(url: baseURL.appendingPathComponent(name, isDirectory: true)) {
                return bundle
            }
        }
        return Bundle.module
    }()

    private static let images: [ATMThemeMode: NSImage] = {
        Dictionary(uniqueKeysWithValues: ATMThemeMode.allCases.compactMap { mode in
            let resourceName: String
            switch mode {
            case .system: resourceName = "theme-system@2x"
            case .light: resourceName = "theme-light@2x"
            case .dark: resourceName = "theme-dark@2x"
            }
            guard let url = resourceBundle.url(
                forResource: resourceName,
                withExtension: "png",
                subdirectory: "ThemePreviews"
            ) ?? resourceBundle.url(forResource: resourceName, withExtension: "png"),
                let image = NSImage(contentsOf: url)
            else {
                return nil
            }
            return (mode, image)
        })
    }()

    var body: some View {
        Group {
            if let image = Self.images[mode] {
                Image(nsImage: image)
                    .resizable()
                    .interpolation(.high)
                    .aspectRatio(contentMode: .fill)
            } else {
                Color(nsColor: .controlBackgroundColor)
            }
        }
        .frame(width: Self.designWidth, height: Self.designHeight)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
    }
}
