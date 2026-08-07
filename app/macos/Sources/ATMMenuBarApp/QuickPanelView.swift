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
                VStack(spacing: 0) {
                    if let error = store.errorMessage {
                        banner(error, icon: "exclamationmark.triangle.fill", color: ATMTheme.danger)
                            .padding(.bottom, 8)
                    }
                    indexHealthBanner
                    usageSection
                    sectionDivider
                    if !store.snapshot.work.needsAction.isEmpty {
                        attentionSection
                        sectionDivider
                    }
                    workingSection
                }
                .padding(.horizontal, 10)
                .padding(.top, 10)
                .padding(.bottom, 8)
            }
            actionBar
        }
        .background(ATMTheme.canvas.opacity(0.96))
        .ignoresSafeArea()
        .frame(minWidth: 360, minHeight: 320)
        .atmHidesScrollBars()
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
                sectionTitle("用量", icon: "chart.bar.fill", color: ATMTheme.accent)
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
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Text(NumberFormat.compact(usage.totalTokens))
                        .font(ATMFont.rounded(.metric, .black))
                        .foregroundStyle(ATMTheme.primary)
                    Text("tokens")
                        .font(ATMFont.font(.caption, weight: .medium))
                        .foregroundStyle(ATMTheme.secondary)
                    Spacer()
                    VStack(alignment: .trailing, spacing: 1) {
                        Text(NumberFormat.currency(usage.costUSD))
                            .font(ATMFont.mono(.bodyLarge, .bold))
                            .foregroundStyle(ATMTheme.accent)
                        Text("\(usage.sessions) 个会话")
                            .font(ATMFont.font(.micro, weight: .medium))
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .help("按模型定价估算的费用 · \(usage.sessions) 个会话")
                }

                HStack(spacing: 0) {
                    usageMetric("输入 + 缓存", NumberFormat.compact(usage.inputTokens))
                    Divider().frame(height: 30)
                    usageMetric("输出", NumberFormat.compact(usage.outputTokens))
                    Divider().frame(height: 30)
                    usageMetric(
                        "缓存命中",
                        NumberFormat.percent(usage.cacheHitRate),
                        valueColor: ATMTheme.cacheHitColor(usage.cacheHitRate)
                    )
                }
                .padding(.top, 2)

                if !store.quota.isEmpty {
                    Divider().padding(.vertical, 2)
                    ForEach(store.quota.cards) { card in
                        quotaRow(agent: card.agent, window: card.window)
                    }
                    ForEach(store.quota.providerCards) { card in
                        ForEach(card.payload.metrics) { metric in
                            providerQuotaRow(card: card, metric: metric)
                        }
                    }
                }
            }
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// One rate-limit window, compact enough for the quick panel: agent, window
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

    private func providerQuotaRow(
        card: ATMProviderQuotaCard,
        metric: ATMProviderQuotaMetric
    ) -> some View {
        let url = card.payload.linkURL
        return quotaPercentRow(
            agent: card.agent,
            title: "\(card.providerLabel) \(metric.label)",
            percent: metric.usedPercent,
            help: "\(ATMAgentDisplay.name(card.agent)) · \(card.providerLabel) · "
                + "\(card.payload.title)：\(metric.valueText)（\(String(format: "%.1f", metric.usedPercent))%）"
                + (url == nil ? "" : " · 点击打开"),
            url: url
        )
    }

    /// `url` is the page behind the reading, when the provider named one. Built-in
    /// rate-limit windows have no such page and stay unclickable.
    @ViewBuilder
    private func quotaPercentRow(
        agent: String,
        title: String,
        percent: Double,
        help: String,
        url: URL? = nil
    ) -> some View {
        if let url {
            Button {
                // The panel is transient and the browser is about to take focus,
                // so it goes away first — same as opening the desktop window.
                close()
                NSWorkspace.shared.open(url)
            } label: {
                quotaPercentRowBody(agent: agent, title: title, percent: percent, help: help)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            quotaPercentRowBody(agent: agent, title: title, percent: percent, help: help)
        }
    }

    private func quotaPercentRowBody(
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
        quickCard(
            "工作中",
            icon: "hammer",
            badge: store.snapshot.work.working.isEmpty ? nil : "\(store.snapshot.work.working.count)"
        ) {
            if store.snapshot.work.working.isEmpty {
                empty("当前没有工作中的任务")
            } else {
                ForEach(store.snapshot.work.working) { todo in
                    quickTodoRow(todo, caption: "工作中")
                }
            }
        }
    }

    private var attentionSection: some View {
        quickCard(
            "需处理",
            icon: "exclamationmark.circle.fill",
            badge: "\(store.snapshot.work.needsAction.count)",
            iconColor: ATMTheme.warning
        ) {
            ForEach(store.snapshot.work.needsAction) { todo in
                quickTodoRow(todo, caption: attentionCaption(todo), showsActions: false)
            }
        }
    }

    /// Capturing a task is the one write the panel offers, so it sits next to the
    /// only other action rather than inside the glance content. The composer itself is
    /// the desktop window's — the compact panel has no room for the project/priority
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
            // ⌘N 归主菜单「文件 → 新建任务」，面板不再自己声明一遍：菜单键等价先被
            // 匹配，两个声明里这一个永远不会触发，只会看着像还有人管。

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
        .padding(.horizontal, 8)
        .padding(.vertical, 7)
        .background(.regularMaterial)
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
                Menu {
                    ForEach(ATMTodoStatusActions.items(for: todo)) { item in
                        Button {
                            store.perform(item.action, on: todo)
                        } label: {
                            Label(item.title, systemImage: item.systemImage)
                        }
                    }
                } label: {
                    ATMIconMenuLabel(
                        systemImage: "ellipsis",
                        help: "任务操作",
                        chrome: .bare,
                        isEnabled: !store.isActing,
                        side: 24,
                        iconTier: .caption
                    )
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .disabled(store.isActing)
            } else {
                Image(systemName: "chevron.right")
                    .font(ATMFont.font(.micro, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
            }
        }
        // Same menu as the task list, minus 编辑任务: the edit form only exists in
        // the desktop detail pane, and the quick panel is not where you would go
        // looking for it.
        .atmRightClickMenu { ATMTodoMenu.entries(for: todo, store: store) }
    }

    private func attentionCaption(_ todo: ATMTodo) -> String {
        let status: String
        switch todo.status {
        case "review": status = "待验收"
        case "blocked": status = "阻塞"
        case "waiting": status = "到期"
        default: status = "需处理"
        }
        // Status only: quickTodoRow appends the project itself, so returning it
        // here printed it twice ("待验收 · atm · atm").
        return status
    }

    private func quickCard<Content: View>(
        _ title: String,
        icon: String,
        badge: String? = nil,
        iconColor: Color = ATMTheme.secondary,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 7) {
                sectionTitle(title, icon: icon, color: iconColor)
                Spacer(minLength: 8)
                if let badge {
                    Text(badge)
                        .font(ATMFont.mono(.micro, .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                        .padding(.horizontal, 7)
                        .frame(minHeight: 18)
                        .background(ATMTheme.controlFill, in: Capsule())
                }
            }
            content()
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var sectionDivider: some View {
        Divider()
            .padding(.horizontal, 4)
            .opacity(0.7)
    }

    private func sectionTitle(_ title: String, icon: String, color: Color) -> some View {
        HStack(spacing: 7) {
            Image(systemName: icon)
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(color)
                .frame(width: 16)
            Text(title)
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
        }
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
        .padding(.horizontal, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func banner(_ text: String, icon: String, color: Color) -> some View {
        Label(text, systemImage: icon)
            .font(ATMFont.font(.caption, weight: .medium))
            .foregroundStyle(color)
            // A banner that carries an instruction has to show all of it; the
            // version-mismatch message ends in the command to run.
            .fixedSize(horizontal: false, vertical: true)
            .multilineTextAlignment(.leading)
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
