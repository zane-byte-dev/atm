import SwiftUI

struct QuickPanelView: View {
    @ObservedObject var store: ATMDataStore
    let close: () -> Void
    let openDesktop: (ATMTodo?) -> Void
    let openUsage: () -> Void

    @State private var metricsRange: ATMMetricsRange = .today
    @State private var isQuotaExpanded = false

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(showsIndicators: false) {
                VStack(spacing: 0) {
                    if let error = store.errorMessage {
                        banner(error, icon: "exclamationmark.triangle.fill", color: ATMTheme.danger)
                            .padding(.bottom, 8)
                    }
                    indexHealthBanner
                    // Above everything: it is the only thing here with a deadline,
                    // and the only one where not looking has an outward effect.
                    if !pendingApprovals.isEmpty || store.approvalErrorMessage != nil {
                        approvalsSection
                        sectionDivider
                    }
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
        }
        // The panel already owns a behind-window `.popover` material. Keep the
        // SwiftUI content transparent so workspace list colours cannot cover or
        // otherwise change the Menubar panel's translucency.
        .background(Color.clear)
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
                        .font(ATMFont.font(.metric, weight: .bold))
                        .foregroundStyle(ATMTheme.primary)
                    Text("Token")
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
                    quotaSummary(ATMQuickQuotaSummary(quota: store.quota))
                }
            }
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func quotaSummary(_ summary: ATMQuickQuotaSummary) -> some View {
        if !summary.entries.isEmpty {
            HStack(alignment: .top, spacing: 8) {
                Group {
                    if isQuotaExpanded {
                        VStack(spacing: 8) {
                            ForEach(summary.entries) { entry in
                                if let percent = entry.usedPercent {
                                    quotaPercentRow(
                                        agent: entry.agent,
                                        title: entry.title,
                                        percent: percent,
                                        help: entry.help
                                    )
                                } else {
                                    unavailableQuotaRow(entry)
                                }
                            }
                        }
                        .transition(.opacity.combined(with: .move(edge: .top)))
                    } else {
                        compactQuotaSummary(summary)
                            .transition(.opacity)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                Button {
                    withAnimation(.easeInOut(duration: 0.16)) {
                        isQuotaExpanded.toggle()
                    }
                } label: {
                    Image(systemName: "chevron.down")
                        .font(ATMFont.font(.micro, weight: .semibold))
                        .foregroundStyle(ATMTheme.secondary)
                        .rotationEffect(.degrees(isQuotaExpanded ? 180 : 0))
                        .frame(width: 22, height: 22)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(isQuotaExpanded ? "收起模型额度" : "展开全部模型额度")
            }
        }
    }

    private func compactQuotaSummary(_ summary: ATMQuickQuotaSummary) -> some View {
        HStack(spacing: 5) {
            ForEach(summary.highlightedEntries) { entry in
                compactQuotaChip(entry)
            }
            if let remainderText = summary.remainderText {
                Text(remainderText)
                    .font(ATMFont.mono(.micro, .medium))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.75)
                    .padding(.horizontal, 7)
                    .frame(height: 22)
                    .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 6))
            }
            Spacer(minLength: 0)
        }
    }

    private func compactQuotaChip(_ entry: ATMQuickQuotaEntry) -> some View {
        let color = entry.usedPercent.map {
            ATMTheme.quotaColor(ATMQuotaLevel.level(forPercent: $0))
        } ?? ATMTheme.warning
        return HStack(spacing: 4) {
            Text(entry.compactTitle)
                .foregroundStyle(ATMTheme.secondary)
            Text(entry.compactValueText)
                .foregroundStyle(color)
        }
        .font(ATMFont.mono(.micro, .semibold))
        .lineLimit(1)
        .minimumScaleFactor(0.72)
        .padding(.horizontal, 7)
        .frame(height: 22)
        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 6))
        .help(entry.help)
    }

    private func unavailableQuotaRow(_ entry: ATMQuickQuotaEntry) -> some View {
        Button {
            openUsage()
        } label: {
            HStack(spacing: 6) {
                ATMAgentMark(agent: entry.agent, size: 13)
                Text(entry.title)
                    .font(ATMFont.mono(.caption, .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(entry.compactValueText)
                    .font(ATMFont.mono(.footnote, .bold))
                    .foregroundStyle(ATMTheme.warning)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(entry.help)
    }

    /// The desktop usage page is the single place that explains quota and usage
    /// together. Provider-specific links remain available from their full cards
    /// there instead of making otherwise identical quick-panel rows route
    /// differently.
    private func quotaPercentRow(
        agent: String,
        title: String,
        percent: Double,
        help: String
    ) -> some View {
        Button {
            openUsage()
        } label: {
            quotaPercentRowBody(agent: agent, title: title, percent: percent, help: help)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
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
            "进行中",
            indicatorColor: ATMTheme.accent,
            badge: store.snapshot.work.working.isEmpty ? nil : "\(store.snapshot.work.working.count)"
        ) {
            if store.snapshot.work.working.isEmpty {
                empty("当前没有进行中的任务")
            } else {
                ForEach(store.snapshot.work.working) { todo in
                    quickTodoRow(todo)
                }
            }
        }
    }

    /// Requests still awaiting a decision. A request that is executing with an
    /// unknown outcome is deliberately excluded — nothing can be decided about it.
    private var pendingApprovals: [ATMGuardApproval] {
        store.pendingApprovals.filter(\.isPending)
    }

    private var approvalsSection: some View {
        quickCard(
            "待授权外发",
            indicatorColor: ATMTheme.danger,
            badge: pendingApprovals.isEmpty ? nil : "\(pendingApprovals.count)"
        ) {
            if let error = store.approvalErrorMessage {
                banner(error, icon: "exclamationmark.triangle.fill", color: ATMTheme.danger)
            }
            ForEach(pendingApprovals) { approval in
                approvalRow(approval)
            }
        }
    }

    private func approvalRow(_ approval: ATMGuardApproval) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(approval.actionLine)
                .font(ATMFont.font(.caption, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            // The message body, because approving sends it: a row that hides what
            // goes out is asking for a decision the user cannot actually make.
            if let body = approval.previewBody?.trimmingCharacters(in: .whitespacesAndNewlines),
               !body.isEmpty {
                Text(body)
                    .font(ATMFont.font(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(4)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 6) {
                Button("批准并发送") { store.decideApproval(approval, approve: true) }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                Button("拒绝") { store.decideApproval(approval, approve: false) }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                Spacer(minLength: 0)
                if let source = approval.envAgent, !source.isEmpty {
                    Text(source)
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            // Per-row, so deciding one request does not freeze the others.
            .disabled(store.isDecidingApproval(approval.id))
        }
        .padding(9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.dangerFill, in: RoundedRectangle(cornerRadius: 9))
        .overlay(
            RoundedRectangle(cornerRadius: 9)
                .stroke(ATMTheme.danger.opacity(0.20), lineWidth: 0.8)
        )
    }

    private var attentionSection: some View {
        quickCard(
            "需处理",
            indicatorColor: ATMTheme.warning,
            badge: "\(store.snapshot.work.needsAction.count)"
        ) {
            ForEach(store.snapshot.work.needsAction) { todo in
                quickTodoRow(todo, caption: attentionCaption(todo), showsActions: false)
            }
        }
    }

    private func quickTodoRow(
        _ todo: ATMTodo,
        caption: String? = nil,
        showsActions: Bool = true
    ) -> some View {
        HStack(spacing: 7) {
            Button {
                openDesktop(todo)
            } label: {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(todo.id.uppercased())
                            .font(ATMFont.mono(.caption, .medium))
                            .foregroundStyle(ATMTodoPriorityStyle.color(for: todo.priority))
                            .help(ATMTodoPriorityStyle.label(todo.priority))
                        Text(todo.title)
                            .font(ATMFont.font(.body, weight: .medium))
                            .foregroundStyle(ATMTheme.primary)
                            .lineLimit(2)
                    }
                    Text(rowMetadata(todo, caption: caption))
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
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
        // Waiting gets no caption of its own; it is shown by the orange clock on
        // the row, the same way it is in the task list.
        let status: String
        switch todo.status {
        case "review": status = "待验收"
        default: status = "需处理"
        }
        // Status only: quickTodoRow appends the project itself, so returning it
        // here printed it twice ("待验收 · atm · atm").
        return status
    }

    private func rowMetadata(_ todo: ATMTodo, caption: String?) -> String {
        var parts: [String] = []
        if let caption = caption?.trimmingCharacters(in: .whitespacesAndNewlines),
           !caption.isEmpty {
            parts.append(caption)
        }
        if let project = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines),
           !project.isEmpty {
            parts.append(project)
        } else {
            parts.append("未分项目")
        }
        return parts.joined(separator: " · ")
    }

    private func quickCard<Content: View>(
        _ title: String,
        indicatorColor: Color,
        badge: String? = nil,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 7) {
                Circle()
                    .fill(indicatorColor)
                    .frame(width: 6, height: 6)
                Text(title)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
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

/// Presentation-ready quota readings for the quick panel. Keeping the compact
/// classification separate from SwiftUI makes the important semantic explicit:
/// a displayed 0% is unused capacity, not an exhausted quota.
struct ATMQuickQuotaEntry: Identifiable, Equatable {
    let id: String
    let agent: String
    let title: String
    let compactTitle: String
    /// Nil means the provider card still exists but currently has no reading.
    /// It must not be presented as 0%: that means a healthy, unused quota.
    let usedPercent: Double?
    let help: String
    let unavailableText: String?

    var roundedPercent: Int? {
        usedPercent.map { Int(max(0, min(100, $0)).rounded()) }
    }

    var isUsed: Bool { (roundedPercent ?? 0) > 0 }
    var isUnavailable: Bool { usedPercent == nil }
    var compactValueText: String {
        roundedPercent.map { "已用 \($0)%" } ?? (unavailableText ?? "暂无数据")
    }
}

struct ATMQuickQuotaSummary: Equatable {
    let entries: [ATMQuickQuotaEntry]

    init(entries: [ATMQuickQuotaEntry]) {
        self.entries = entries
    }

    init(quota: ATMQuotaSnapshot) {
        let windowCounts = Dictionary(grouping: quota.cards, by: \.agent).mapValues(\.count)
        let builtIn = quota.cards.map { card in
            let label = ATMAgentDisplay.name(card.agent)
            let percent = card.window.displayPercent
            return ATMQuickQuotaEntry(
                id: card.id,
                agent: card.agent,
                title: "\(label) \(card.window.windowLabel)",
                compactTitle: windowCounts[card.agent, default: 0] > 1
                    ? "\(label) \(card.window.windowLabel)"
                    : label,
                usedPercent: percent,
                help: "\(label) \(card.window.windowLabel) 窗口："
                    + "\(String(format: "%.1f", percent))% 已用，\(card.window.resetText)",
                unavailableText: nil
            )
        }

        let provided = quota.providerCards.flatMap { card in
            if card.payload.metrics.isEmpty {
                return [ATMQuickQuotaEntry(
                    id: "\(card.id):unavailable",
                    agent: card.agent,
                    title: "\(card.providerLabel) \(card.payload.title)",
                    compactTitle: card.providerLabel,
                    usedPercent: nil,
                    help: "\(ATMAgentDisplay.name(card.agent)) · \(card.providerLabel) · "
                        + "\(card.payload.title)：\(card.payload.unavailableText)"
                        + " · 上次观察 \(card.payload.observedTimeLabel) · 点击查看额度详情",
                    unavailableText: card.payload.unavailableText
                )]
            }
            return card.payload.metrics.map { metric in
                ATMQuickQuotaEntry(
                    id: "\(card.id):\(metric.id)",
                    agent: card.agent,
                    title: "\(card.providerLabel) \(metric.label)",
                    compactTitle: card.payload.metrics.count > 1
                        ? "\(card.providerLabel) \(metric.label)"
                        : card.providerLabel,
                    usedPercent: metric.usedPercent,
                    help: "\(ATMAgentDisplay.name(card.agent)) · \(card.providerLabel) · "
                        + "\(card.payload.title)：\(metric.valueText)"
                        + "（\(String(format: "%.1f", metric.usedPercent))% 已用）"
                        + " · 点击查看额度详情",
                    unavailableText: nil
                )
            }
        }
        entries = builtIn + provided
    }

    var usedEntries: [ATMQuickQuotaEntry] { entries.filter(\.isUsed) }
    var unavailableEntries: [ATMQuickQuotaEntry] { entries.filter(\.isUnavailable) }
    var usedCount: Int { usedEntries.count }
    var unusedCount: Int { entries.count - usedCount - unavailableEntries.count }
    var highlightedEntries: [ATMQuickQuotaEntry] {
        var candidates = Array(usedEntries.prefix(1))
        candidates.append(contentsOf: unavailableEntries)
        candidates.append(contentsOf: usedEntries.dropFirst())
        return Array(candidates.prefix(2))
    }

    var statusText: String {
        var parts: [String] = []
        if usedCount > 0 { parts.append("\(usedCount) 个使用中") }
        if unusedCount > 0 { parts.append("\(unusedCount) 个未使用") }
        if !unavailableEntries.isEmpty { parts.append("\(unavailableEntries.count) 个暂无数据") }
        return parts.joined(separator: " · ")
    }

    var remainderText: String? {
        let highlightedIDs = Set(highlightedEntries.map(\.id))
        let hidden = entries.filter { !highlightedIDs.contains($0.id) }
        guard !hidden.isEmpty else { return nil }
        let hiddenUsedCount = hidden.filter(\.isUsed).count
        let hiddenUnavailableCount = hidden.filter(\.isUnavailable).count
        let hiddenUnusedCount = hidden.count - hiddenUsedCount - hiddenUnavailableCount
        let prefix = highlightedEntries.isEmpty ? "全部" : "其他"
        if hiddenUsedCount == 0, hiddenUnavailableCount == 0 {
            return "\(prefix) \(hiddenUnusedCount) 个 未使用"
        }
        if hiddenUnusedCount == 0, hiddenUnavailableCount == 0 {
            return "\(prefix) \(hiddenUsedCount) 个 使用中"
        }
        if hiddenUsedCount == 0, hiddenUnusedCount == 0 {
            return "\(prefix) \(hiddenUnavailableCount) 个 暂无数据"
        }
        return "\(prefix) \(hidden.count) 个"
    }
}
