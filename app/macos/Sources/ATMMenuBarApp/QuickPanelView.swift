import SwiftUI

struct QuickPanelView: View {
    @ObservedObject var store: ATMDataStore
    let close: () -> Void
    let openDesktop: (ATMTodo?) -> Void
    let addTodo: () -> Void

    @State private var metricsRange: ATMMetricsRange = .today

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(showsIndicators: false) {
                VStack(spacing: 8) {
                    if let error = store.errorMessage {
                        banner(error, icon: "exclamationmark.triangle.fill", color: ATMTheme.danger)
                    }
                    indexHealthBanner
                    usageSection
                    if !store.snapshot.work.needsAction.isEmpty {
                        attentionSection
                    }
                    workingSection
                }
                .padding(10)
            }
            actionBar
        }
        .background(ATMTheme.canvas.opacity(0.98))
        .ignoresSafeArea()
        .frame(minWidth: 340, minHeight: 290)
    }

    @ViewBuilder
    private var indexHealthBanner: some View {
        // Healthy / in-flight sync is silent — a "后台同步中" chip used to appear
        // and disappear every few minutes and made the panel jump.
        if store.isSyncing {
            EmptyView()
        } else if let health = store.snapshot.indexHealth {
            let state = health.sync
            switch state.status {
            case "fresh", "syncing":
                EmptyView()
            case "stale":
                banner(
                    "索引已过期 · \(syncAge(state.ageSeconds))，后台将自动重试",
                    icon: "clock.badge.exclamationmark",
                    color: ATMTheme.warning
                )
            case "failed":
                banner(
                    "后台同步失败\(state.lastError.isEmpty ? "" : "：\(ATMErrorText.compact(state.lastError, limit: 100))")",
                    icon: "exclamationmark.triangle.fill",
                    color: ATMTheme.danger
                )
            case "missing":
                banner("会话索引尚未建立，后台将自动同步", icon: "externaldrive.badge.questionmark", color: ATMTheme.warning)
            case "never":
                banner("会话索引尚无同步记录", icon: "clock.badge.questionmark", color: ATMTheme.warning)
            default:
                banner("会话索引状态未知", icon: "questionmark.circle", color: ATMTheme.secondary)
            }
        }
    }

    private func syncAge(_ seconds: Int64?) -> String {
        guard let seconds else { return "刚刚" }
        switch seconds {
        case ..<60: return "刚刚"
        case ..<3600: return "\(seconds / 60) 分钟前"
        case ..<86400: return "\(seconds / 3600) 小时前"
        default: return "\(seconds / 86400) 天前"
        }
    }

    private var usageSection: some View {
        let usage = store.snapshot.summary(for: metricsRange)
        return VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Label("用量", systemImage: "chart.bar.fill")
                    .font(ATMFont.mono(.footnote, .bold))
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize()
                Spacer(minLength: 6)
                // Three windows, not all seven: seven segments overflowed the panel
                // rather than shrinking. The full set of calendar periods lives in
                // the desktop window's dropdown, which has room to name them.
                Picker("用量周期", selection: $metricsRange) {
                    ForEach(ATMMetricsRange.compact) { range in
                        Text(range.compactTitle).tag(range)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .controlSize(.mini)
                .frame(width: 150)
            }

            if store.snapshot.refreshedAt == .distantPast {
                HStack(spacing: 7) {
                    ProgressView().controlSize(.small)
                    Text("正在读取用量…")
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(NumberFormat.compact(usage.totalTokens))
                        .font(ATMFont.rounded(.metric, .black))
                        .foregroundStyle(ATMTheme.primary)
                    Text("Token")
                        .font(ATMFont.mono(.caption, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                    Spacer()
                    Text(NumberFormat.currency(usage.costUSD))
                        .font(ATMFont.mono(.bodyLarge, .bold))
                        .foregroundStyle(ATMTheme.accent)
                        .help("按模型定价估算的费用")
                }

                HStack(spacing: 0) {
                    usageMetric("输入 + 缓存", NumberFormat.compact(usage.inputTokens))
                    Divider().frame(height: 28)
                    usageMetric("输出", NumberFormat.compact(usage.outputTokens))
                    Divider().frame(height: 28)
                    usageMetric(
                        "缓存命中",
                        NumberFormat.percent(usage.cacheHitRate),
                        valueColor: ATMTheme.cacheHitColor(usage.cacheHitRate)
                    )
                    Divider().frame(height: 28)
                    usageMetric("会话", "\(usage.sessions)")
                }
                .padding(.top, 2)

                if !store.quota.isEmpty {
                    Divider().padding(.vertical, 2)
                    ForEach(store.quota.cards) { card in
                        quotaRow(agent: card.agent, window: card.window)
                    }
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(ATMTheme.border, lineWidth: 1))
    }

    /// One rate-limit window, compact enough for the 300pt panel: agent, window
    /// length, a bar, the percentage, and when it resets.
    private func quotaRow(agent: String, window: ATMQuotaWindow) -> some View {
        let percent = window.displayPercent
        let label = ATMAgentDisplay.name(agent)
        return quotaPercentRow(
            agent: agent,
            title: "\(label) \(window.windowLabel)",
            percent: percent,
            help: "\(label) \(window.windowLabel) 窗口：\(String(format: "%.1f", percent))% 已用，\(window.resetText)"
        )
    }

    private func quotaPercentRow(
        agent: String,
        title: String,
        percent: Double,
        help: String
    ) -> some View {
        let displayPercent = max(0, percent)
        let color = ATMTheme.quotaColor(ATMQuotaLevel.level(forPercent: displayPercent))
        return HStack(spacing: 6) {
            ATMAgentMark(agent: agent, size: 13)
            Text(title)
                .font(ATMFont.mono(.caption, .semibold))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)
                .frame(width: 88, alignment: .leading)

            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(ATMTheme.controlFill)
                    Capsule()
                        .fill(color)
                        .frame(width: max(0, min(1, displayPercent / 100)) * proxy.size.width)
                }
            }
            .frame(height: 4)

            Text(String(format: "%.0f%%", displayPercent))
                .font(ATMFont.mono(.footnote, .bold))
                .foregroundStyle(color)
                .frame(width: 40, alignment: .trailing)
        }
        .help(help)
    }

    private var workingSection: some View {
        quickCard("工作中", icon: "hammer") {
            if store.snapshot.work.working.isEmpty {
                empty("当前没有工作中的任务")
            } else {
                ForEach(store.snapshot.work.working) { todo in
                    quickTodoRow(todo, caption: todo.lane == "work" ? "工作" : "个人")
                }
            }
        }
    }

    private var attentionSection: some View {
        quickCard("需处理", icon: "exclamationmark.circle.fill") {
            ForEach(store.snapshot.work.needsAction) { todo in
                quickTodoRow(todo, caption: attentionCaption(todo), showsActions: false)
            }
        }
    }

    /// Capturing a task is the one write the panel offers, so it sits next to the
    /// only other action rather than inside the glance content. The card itself is
    /// the desktop window's — 300pt has no room for the project/priority/lane
    /// chips, and two composers would drift apart.
    private var actionBar: some View {
        HStack(spacing: 4) {
            ATMHoverLabelButton(
                title: "添加任务",
                systemImage: "plus",
                help: "添加任务 (⌘N)，在主窗口填写",
                height: 34,
                tier: .footnote,
                action: addTodo
            )
            .keyboardShortcut("n", modifiers: .command)
            ATMHoverLabelButton(
                title: "主窗口",
                systemImage: "macwindow",
                trailingSystemImage: "arrow.up.right",
                help: "打开 ATM 主窗口",
                height: 34,
                tier: .footnote
            ) {
                openDesktop(nil)
            }
        }
        .padding(.horizontal, 2)
        .overlay(alignment: .top) { Divider().opacity(0.45) }
    }

    private func quickTodoRow(_ todo: ATMTodo, caption: String, showsActions: Bool = true) -> some View {
        HStack(spacing: 7) {
            Button {
                openDesktop(todo)
            } label: {
                VStack(alignment: .leading, spacing: 3) {
                    Text("\(todo.id) · \(todo.title)")
                        .font(ATMFont.font(.body, weight: .semibold))
                        .foregroundStyle(ATMTheme.primary)
                        .lineLimit(2)
                    Text("\(caption) · \(todo.project ?? "未分项目")")
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                }
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if showsActions {
                ForEach(ATMTodoStatusActions.primaryItems(for: todo)) { item in
                    rowAction(item.systemImage, help: item.help) {
                        store.perform(item.action, on: todo)
                    }
                }
            } else {
                Image(systemName: "chevron.right")
                    .font(ATMFont.font(.micro, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
        .contextMenu {
            Button {
                store.openTodoProjectInVSCode(todo)
            } label: {
                Label("用 VS Code 打开项目", systemImage: "chevron.left.forwardslash.chevron.right")
            }
            if ATMTodoStatusActions.showsLaunchPrompt(for: todo) {
                Button {
                    Task {
                        guard let prompt = await store.launchPrompt(for: todo) else { return }
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(prompt, forType: .string)
                    }
                } label: {
                    Label("复制启动提示", systemImage: "doc.on.doc")
                }
            }
        }
    }

    private func attentionCaption(_ todo: ATMTodo) -> String {
        let status: String
        switch todo.status {
        case "review": status = "待验收"
        case "blocked": status = "阻塞"
        case "waiting": status = "到期"
        default: status = "需处理"
        }
        return "\(status) · \(todo.project ?? "未分项目")"
    }

    private func quickCard<Content: View>(
        _ title: String,
        icon: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Label(title, systemImage: icon)
                .font(ATMFont.mono(.footnote, .bold))
                .foregroundStyle(ATMTheme.primary.opacity(0.72))
            content()
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(ATMTheme.border, lineWidth: 1))
    }

    private func usageMetric(
        _ label: String,
        _ value: String,
        valueColor: Color = ATMTheme.primary
    ) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(value)
                .font(ATMFont.mono(.body, .bold))
                .foregroundStyle(valueColor)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            Text(label)
                .font(ATMFont.font(.micro, weight: .medium))
                .foregroundStyle(ATMTheme.secondary)
                .lineLimit(1)
        }
        .padding(.horizontal, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func rowAction(_ icon: String, help: String, action: @escaping () -> Void) -> some View {
        ATMIconButton(
            systemImage: icon,
            help: help,
            isEnabled: !store.isActing,
            side: 24,
            iconTier: .caption,
            action: action
        )
    }

    private func banner(_ text: String, icon: String, color: Color) -> some View {
        Label(text, systemImage: icon)
            .font(ATMFont.font(.caption, weight: .medium))
            .foregroundStyle(color)
            .padding(9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(color.opacity(0.12), in: RoundedRectangle(cornerRadius: 9))
            .overlay(
                RoundedRectangle(cornerRadius: 9)
                    .stroke(color.opacity(0.20), lineWidth: 0.8)
            )
    }

    private func empty(_ text: String) -> some View {
        Text(text)
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)
            .frame(maxWidth: .infinity, minHeight: 30, alignment: .leading)
    }
}
