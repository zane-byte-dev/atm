import AppKit
import SwiftUI

enum ATMTodoListPreferences {
    static let showDroppedKey = "ATMShowDroppedTodos"
    static let defaultShowsDropped = false
}

struct DesktopSettingsView: View {
    private enum SettingsTab: String, CaseIterable, Identifiable {
        case general
        case notch
        case todo
        case connectors

        var id: String { rawValue }

        var title: String {
            switch self {
            case .general: return "通用"
            case .notch: return "刘海"
            case .todo: return "Todo"
            case .connectors: return "连接器"
            }
        }

        var systemImage: String {
            switch self {
            case .general: return "slider.horizontal.3"
            case .notch: return "rectangle.topthird.inset.filled"
            case .todo: return "checklist"
            case .connectors: return "link"
            }
        }

        var subtitle: String {
            switch self {
            case .general: return "主题、字号与快捷键"
            case .notch: return "状态提醒与声音反馈"
            case .todo: return "任务列表与默认行为"
            case .connectors: return "自动收集与外部来源"
            }
        }
    }

    @ObservedObject var store: ATMDataStore
    @ObservedObject private var appearance = ATMAppearance.shared
    @ObservedObject private var hotKeys = ATMGlobalHotKeyManager.shared
    @State private var selectedTab: SettingsTab = .general
    @AppStorage(ATMTodoListPreferences.showDroppedKey)
    private var showsDropped = ATMTodoListPreferences.defaultShowsDropped
    @AppStorage(ATMGlobalHotKeyPreferences.enabledKey)
    private var globalHotKeyEnabled = ATMGlobalHotKeyPreferences.defaultEnabled
    @AppStorage(ATMGlobalHotKeyPreferences.hotKeyKey)
    private var globalHotKeyValue = ATMGlobalHotKeyPreferences.defaultHotKey.storageValue
    @AppStorage(ATMGlobalHotKeyPreferences.targetKey)
    private var globalHotKeyTarget = ATMGlobalHotKeyPreferences.defaultTarget.rawValue
    @AppStorage(ATMAgentNotchPreferences.enabledKey)
    private var agentNotchEnabled = ATMAgentNotchPreferences.defaultEnabled
    @AppStorage(ATMAgentNotchPreferences.retentionKey)
    private var agentNotchRetention = ATMAgentNotchRetention.default.rawValue
    @AppStorage(ATMAgentNotchPreferences.notificationDwellKey)
    private var agentNotchNotificationDwell = ATMAgentNotchNotificationDwell.default.rawValue
    @AppStorage(ATMAgentNotchPreferences.screenSelectionKey)
    private var agentNotchScreenSelection = ATMAgentNotchScreenSelection.default.rawValue
    @AppStorage(ATMAgentNotchPreferences.stripAlignmentKey)
    private var agentNotchStripAlignment = ATMAgentNotchStripAlignment.default.rawValue
    @State private var availableScreens: [ATMNotchScreenOption] = ATMNotchScreenOption.current()
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
        HSplitView {
            settingsSidebar
                .frame(minWidth: 240, idealWidth: 270, maxWidth: 310)

            settingsContent
                .frame(minWidth: 580, maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(ATMTheme.listPane)
    }

    private var settingsSidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 3) {
                Text("工作台")
                    .font(ATMFont.caption)
                    .foregroundStyle(ATMTheme.secondary)
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
                ForEach(SettingsTab.allCases) { tab in
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

    private func settingsSidebarRow(_ tab: SettingsTab) -> some View {
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
                case .notch:
                    notchSettings
                case .todo:
                    todoSettings
                case .connectors:
                    connectorSettings
                }
            }
        }
        .background(ATMTheme.listPane)
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

                        HStack(alignment: .top, spacing: 14) {
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
                        .frame(width: 420)
                    }
                }

                card {
                    VStack(alignment: .leading, spacing: 10) {
                        Text("预览")
                            .font(ATMFont.font(.caption, weight: .semibold))
                            .foregroundStyle(ATMTheme.secondary)
                        // Rendered by the same view the real content uses, so the
                        // preview cannot drift from what the setting actually does.
                        ATMMarkdownContentView(source: Self.previewSample)
                    }
                }

                card { globalHotKeySection }

                Spacer(minLength: 0)
            }
            .padding(24)
            .frame(maxWidth: 980, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// The global shortcut lives in 通用 rather than in a tab of its own: it is a
    /// single app-wide binding, and the only other place it could sit — 刘海 — is
    /// about pushed events rather than about opening ATM by hand.
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
               case .unavailable(let hotKey) = hotKeys.registration {
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

    /// The 刘海 tab: everything about the menu-bar notch experience — the strip
    /// itself and its placement, the event-push hooks that feed it, and the
    /// state-change sounds it plays.
    private var notchSettings: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card { agentNotchSection }

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

    /// The notch's own preferences: on/off plus the four Ping-Island-style
    /// customizations — which screen it lives on, how it sits on a notchless
    /// screen, how long finished sessions linger, and how long a card stays up.
    @ViewBuilder
    private var agentNotchSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("刘海 Agent")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("在屏幕顶部显示活跃与最近的 Agent。绿色表示活跃、灰色表示最近；悬停直接展开完整会话列表，点击可固定并直接回到来源。")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                Toggle("显示", isOn: $agentNotchEnabled)
                    .labelsHidden()
                    .toggleStyle(.switch)
                    .controlSize(.small)
            }

            if agentNotchEnabled {
                Divider()

                agentNotchScreenPicker

                agentNotchSettingRow(
                    title: "无刘海屏位置",
                    detail: "没有物理刘海的屏幕降级为顶部悬浮条时的停靠位置；物理刘海屏始终居中对齐相机区域。"
                ) {
                    Picker("无刘海屏位置", selection: $agentNotchStripAlignment) {
                        ForEach(ATMAgentNotchStripAlignment.allCases) { alignment in
                            Text(alignment.label).tag(alignment.rawValue)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 240)
                }

                agentNotchSettingRow(
                    title: "最近会话保留",
                    detail: "会话结束后在刘海里保留多久，超时自动隐藏。“需要你”的会话不受此限制，会一直显示到你处理。"
                ) {
                    Picker("最近会话保留", selection: $agentNotchRetention) {
                        ForEach(ATMAgentNotchRetention.allCases) { retention in
                            Text(retention.label).tag(retention.rawValue)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 300)
                }

                agentNotchSettingRow(
                    title: "通知停留",
                    detail: "“已完成 / 需要你”的卡片弹出后自动收起前停留多久。选“手动收起”则一直显示，直到点击别处或来了新事件。"
                ) {
                    Picker("通知停留", selection: $agentNotchNotificationDwell) {
                        ForEach(ATMAgentNotchNotificationDwell.allCases) { dwell in
                            Text(dwell.label).tag(dwell.rawValue)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 300)
                }

                Text("没有活跃或最近会话时自动隐藏。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .onAppear { availableScreens = ATMNotchScreenOption.current() }
        .onReceive(
            NotificationCenter.default.publisher(
                for: NSApplication.didChangeScreenParametersNotification
            )
        ) { _ in
            availableScreens = ATMNotchScreenOption.current()
        }
    }

    private var agentNotchScreenPicker: some View {
        agentNotchSettingRow(
            title: "显示屏",
            detail: "选择刘海挂在哪块屏幕上。“自动”优先带物理刘海的屏，其次主屏；指定的外接屏拔掉后自动回退到“自动”。"
        ) {
            Picker("显示屏", selection: $agentNotchScreenSelection) {
                Text("自动").tag(ATMAgentNotchScreenSelection.automatic.rawValue)
                Text("主屏").tag(ATMAgentNotchScreenSelection.main.rawValue)
                if !availableScreens.isEmpty {
                    Divider()
                    ForEach(availableScreens) { screen in
                        Text(screen.label)
                            .tag(ATMAgentNotchScreenSelection.display(screen.displayID).rawValue)
                    }
                }
            }
            .labelsHidden()
            .frame(width: 240)
        }
    }

    /// One labeled control row inside the notch card: title + explanatory copy on
    /// the left, the control trailing. Keeps the four pickers visually uniform.
    private func agentNotchSettingRow(
        title: String,
        detail: String,
        @ViewBuilder control: () -> some View
    ) -> some View {
        HStack(alignment: .top, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(ATMFont.font(.body, weight: .medium))
                Text(detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 12)
            control()
        }
    }

    /// Hook wiring for the notch.
    ///
    /// Without hooks the notch has to infer "this session needs you" by
    /// keyword-matching the agent's last message, which cannot see a tool call
    /// blocked on a permission prompt at all. This section is how that inference
    /// gets replaced by the agent telling us directly.
    @ViewBuilder
    private var agentHookSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Agent 事件推送")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text("装上 hook 后，Agent 卡在授权或等你输入的那一刻会直接推给刘海，不再靠每 3 秒扫一遍会话记录去猜。装的都是只上报的 hook，不会拦住工具调用，也不会改 Agent 的行为。")
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
            return "事件通道未启动（刘海或 Agent 工作台打开后自动监听）"
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
                    // so in-notch approval would mean two prompts racing.
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
            .atmWorkspaceCard()
    }

    private func collectionSettingsRelativeTime(_ timestamp: Int64) -> String {
        let elapsed = max(Int(Date().timeIntervalSince1970) - Int(timestamp), 0)
        if elapsed < 60 { return "刚刚" }
        if elapsed < 3_600 { return "\(elapsed / 60) 分钟前" }
        if elapsed < 86_400 { return "\(elapsed / 3_600) 小时前" }
        return "\(elapsed / 86_400) 天前"
    }
}

/// A pluggable screen the notch can be pinned to, resolved from the live
/// `NSScreen` list. Identified by CoreGraphics display id so the tag survives a
/// reshuffle when a monitor is plugged or unplugged.
struct ATMNotchScreenOption: Identifiable {
    let displayID: CGDirectDisplayID
    let label: String

    var id: CGDirectDisplayID { displayID }

    static func current() -> [ATMNotchScreenOption] {
        let screens = NSScreen.screens
        return screens.enumerated().compactMap { index, screen in
            guard let displayID = screen.atmDisplayID else { return nil }
            let notch = screen.safeAreaInsets.top > 0 ? " · 刘海屏" : ""
            let main = screen == NSScreen.main ? " · 主屏" : ""
            let base = screen.localizedName.isEmpty
                ? "显示器 \(index + 1)"
                : screen.localizedName
            return ATMNotchScreenOption(displayID: displayID, label: "\(base)\(notch)\(main)")
        }
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

                HStack(spacing: 5) {
                    Text(mode.label)
                        .font(ATMFont.font(.body, weight: isSelected ? .semibold : .regular))
                    if isSelected {
                        Image(systemName: "checkmark.circle.fill")
                            .font(ATMFont.font(.caption, weight: .semibold))
                            .foregroundStyle(ATMTheme.accent)
                    }
                }
                .foregroundStyle(ATMTheme.primary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(mode.label)主题")
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}

private struct ATMThemePreview: View {
    let mode: ATMThemeMode

    var body: some View {
        HStack(spacing: 0) {
            if mode == .system {
                previewHalf(isDark: false, isSplit: true)
                previewHalf(isDark: true, isSplit: true)
            } else {
                previewHalf(isDark: mode == .dark, isSplit: false)
            }
        }
        .frame(width: 184, height: 108)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    private func previewHalf(isDark: Bool, isSplit: Bool) -> some View {
        let palette = ATMThemePreviewPalette(isDark: isDark)

        return HStack(spacing: 0) {
            palette.canvas
                .overlay(palette.line.opacity(0.055))
                .frame(width: isSplit ? 20 : 38)

            VStack(alignment: .leading, spacing: 7) {
                Capsule()
                    .fill(palette.line.opacity(0.72))
                    .frame(width: 45, height: 5)

                VStack(alignment: .leading, spacing: 7) {
                    Capsule()
                        .fill(palette.line.opacity(0.55))
                        .frame(width: 35, height: 5)
                    Capsule()
                        .fill(palette.line.opacity(0.34))
                        .frame(maxWidth: .infinity)
                        .frame(height: 4)
                    Capsule()
                        .fill(palette.line.opacity(0.34))
                        .frame(width: 54, height: 4)
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(palette.surface)
                .clipShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
            }
            .padding(10)
            .background(palette.canvas)
        }
    }
}

/// Resolves AppKit's dynamic semantic colors against an explicit appearance.
/// Without this, a dark preview rendered while the app is light would silently
/// inherit the light window appearance.
private struct ATMThemePreviewPalette {
    let canvas: Color
    let surface: Color
    let line: Color

    init(isDark: Bool) {
        guard let appearance = NSAppearance(named: isDark ? .darkAqua : .aqua) else {
            canvas = Color(nsColor: .windowBackgroundColor)
            surface = Color(nsColor: .controlBackgroundColor)
            line = Color(nsColor: .labelColor)
            return
        }
        canvas = Self.resolve(.windowBackgroundColor, appearance: appearance)
        surface = Self.resolve(.controlBackgroundColor, appearance: appearance)
        line = Self.resolve(.labelColor, appearance: appearance)
    }

    private static func resolve(_ color: NSColor, appearance: NSAppearance) -> Color {
        var resolved = color
        appearance.performAsCurrentDrawingAppearance {
            resolved = color.usingColorSpace(.sRGB) ?? color
        }
        return Color(nsColor: resolved)
    }
}
