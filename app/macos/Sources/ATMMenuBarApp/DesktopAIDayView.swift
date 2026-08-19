import AppKit
import Combine
import SwiftUI

private enum ATMAIDayTab: String, CaseIterable, Identifiable {
    case today = "今日"
    case atlas = "Atlas"
    case history = "历史"
    case privacy = "数据与隐私"
    var id: String { rawValue }
}

struct DesktopAIDayView: View {
    @StateObject private var store = ATMAIDayStore()
    @State private var tab: ATMAIDayTab = .today
    @State private var selectedBadge: ATMAIDayBadge?
    @State private var showingCorrection = false
    @State private var showingShare = false
    @State private var confirmingDelete = false
    @State private var sourceToDelete: ATMAIDaySource?
    @State private var showAtlasMap = true
    @State private var correctionChoice: String?
    @State private var selectedHistoryDay: ATMAIDayResult?
    @State private var confirmingSemanticOff = false
    @State private var retentionDraft: Int?
    @State private var confirmingRetention = false
    @State private var historyFilter: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ATMDetailTabs { tabs }
            ATMDetailBodySurface {
                Group {
                    switch tab {
                    case .today: todayView
                    case .atlas: atlasView
                    case .history: historyView
                    case .privacy: privacyView
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .atmAnimatedSwap(tab, style: .tab)
            }
        }
        .background(ATMTheme.canvas)
        .task { if store.today == nil { store.refresh() } }
        .sheet(item: $selectedBadge) { ATMAIDayBadgeDetail(badge: $0) }
        .sheet(isPresented: $showingCorrection) { correctionSheet }
        .sheet(isPresented: $showingShare) {
            if let result = store.today { ATMAIDayShareSheet(result: result) }
        }
        .alert("删除所有 AI Day 数据？", isPresented: $confirmingDelete) {
            Button("取消", role: .cancel) {}
            Button("删除", role: .destructive) { store.deleteAll() }
        } message: { Text("只删除 AI Day 的衍生事件、结果与反馈，不删除 ATM 的原始会话索引。") }
        .alert(item: $sourceToDelete) { source in
            Alert(
                title: Text("删除 \(source.source) 的 AI Day 数据？"),
                message: Text("将删除这个来源的衍生事件并暂停来源，不影响 ATM 原始会话索引。"),
                primaryButton: .destructive(Text("删除")) { store.deleteSource(source) },
                secondaryButton: .cancel()
            )
        }
        .alert("AI Day 操作失败", isPresented: Binding(get: { store.errorMessage != nil }, set: { if !$0 { store.errorMessage = nil } })) {
            Button("好") { store.errorMessage = nil }
        } message: { Text(store.errorMessage ?? "未知错误") }
        .sheet(item: $selectedHistoryDay) { day in
            ATMAIDayDetailSheet(result: day)
        }
        .alert("关闭本地语义分类？", isPresented: $confirmingSemanticOff) {
            Button("取消", role: .cancel) {}
            Button("关闭", role: .destructive) { store.setSemantic(false) }
        } message: {
            Text("这不只是暂停：已存储的语义标签会被清空，每日特征与徽章进度会被删除并重算。重新打开开关不会自动恢复，需要运行一次 `atm day rebuild` 重新分类。依赖语义的徽章（质检员、追问者、不易被糊弄等）在关闭期间不会出现。")
        }
        .alert("缩短保留期？", isPresented: $confirmingRetention) {
            Button("取消", role: .cancel) { retentionDraft = nil }
            Button("删除并应用", role: .destructive) {
                if let draft = retentionDraft { store.setRetention(draft) }
                retentionDraft = nil
            }
        } message: {
            Text("超过 \(retentionDraft ?? 0) 天的 AI Day 衍生事件会被立即删除。日结果与徽章历史会保留，但被删除的事件只有最近 31 天能通过重建从原始会话恢复，更早的无法恢复。")
        }
        .onAppear { store.startAutoRefresh() }
        .onDisappear { store.stopAutoRefresh() }
        // A day in progress keeps changing; coming back to the window should not
        // show whatever was true when it was first opened.
        .onChange(of: tab) { newTab in if newTab == .today { store.refreshIfStale() } }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            store.refreshIfStale()
        }
    }

    /// Header only owns page identity and global actions. Navigation lives in
    /// the tab strip below it, immediately above the content card.
    private var header: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .bottom, spacing: 14) {
                title
                Spacer(minLength: 8)
                refreshButton
            }
            VStack(alignment: .leading, spacing: 12) {
                title
                HStack {
                    Spacer()
                    refreshButton
                }
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 22)
        .padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated)
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("AI Day").font(ATMFont.font(.title1, weight: .semibold))
            Text("每天一个概念、一枚徽章、一组可验证证据")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
        }
    }

    /// 四个标签长度不一（「今日」两字、「数据与隐私」五字），正是 `ATMCapsuleTabs` 的用例；
    /// 等宽的 `ATMCompactSegmentedTabs` 会把「数据与隐私」压出格子。
    private var tabs: some View {
        ATMCapsuleTabs(
            selection: $tab,
            items: ATMAIDayTab.allCases.map { (value: $0, title: $0.rawValue) }
        )
    }

    private var refreshButton: some View {
        HStack(spacing: 8) {
            if store.isLoading { ProgressView().controlSize(.small) }
            ATMIconButton(
                systemImage: "arrow.clockwise",
                help: "刷新 AI Day",
                chrome: .bare,
                isEnabled: !store.isLoading,
                side: 30,
                iconTier: .bodyLarge
            ) {
                store.refresh()
            }
        }
    }

    @ViewBuilder private var todayView: some View {
        if let result = store.today {
            ScrollView {
                VStack(spacing: 18) {
                    if !result.hasContent {
                        ATMAIDayEmptyCard(day: result.day)
                    } else if let badge = result.badge, let concept = result.concept {
                        ATMAIDayStatusStrip(result: result, lastRefreshed: store.lastRefreshed)
                        ATMAIDayResultCard(result: result, badge: badge, concept: concept, style: .today)
                            .padding(24)
                            .atmWorkspaceCard()

                        evidenceCard(result)
                        feedbackBar(result)
                    }
                }.padding(28).frame(maxWidth: 920)
            }
        } else if store.isLoading { ProgressView("正在生成今天的 AI Day…") }
        else { ATMAIDayEmptyCard(day: "今天") }
    }

    /// The feedback row reports its own state. Previously every button was a
    /// one-way fire-and-forget click with no visible result and no way back.
    private func feedbackBar(_ result: ATMAIDayResult) -> some View {
        let verdict = result.feedback?.verdict
        return HStack(spacing: 10) {
            Button { store.submitFeedback(verdict: "accurate") } label: {
                Label("准确", systemImage: verdict == "accurate" ? "checkmark.circle.fill" : "checkmark.circle")
            }.tint(verdict == "accurate" ? ATMTheme.accent : nil)
            Button { store.submitFeedback(verdict: "inaccurate") } label: {
                Label("不准确", systemImage: verdict == "inaccurate" ? "xmark.circle.fill" : "xmark.circle")
            }.tint(verdict == "inaccurate" ? ATMTheme.danger : nil)
            Button(verdict == "corrected" ? "重新选择徽章" : "纠正徽章") { showingCorrection = true }
            if let verdict {
                Text(feedbackStateLabel(verdict, result: result))
                    .font(.caption).foregroundStyle(ATMTheme.secondary)
                Button("撤销") { store.clearFeedback() }
                    .buttonStyle(.plain)
                    .foregroundStyle(ATMTheme.accent)
                    .help("删除这一天的反馈，恢复引擎自己的结论")
            }
            Spacer()
            Button { showingShare = true } label: { Label("生成分享卡", systemImage: "square.and.arrow.up") }
                .buttonStyle(.borderedProminent)
        }
    }

    private func feedbackStateLabel(_ verdict: String, result: ATMAIDayResult) -> String {
        switch verdict {
        case "accurate": return "已标记为准确"
        case "inaccurate": return "已标记为不准确"
        case "corrected": return "已改为「\(result.concept?.title ?? "")」"
        default: return ""
        }
    }

    private func evidenceCard(_ result: ATMAIDayResult) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text("为什么是它").font(.headline)
                if result.concept?.isUserCorrected == true {
                    Text("以下证据来自引擎的实际测量，不因纠正而改变")
                        .font(.caption).foregroundStyle(ATMTheme.secondary)
                }
            }
            ATMAIDayEvidenceList(evidence: result.concept?.evidence ?? [], spacing: 14)
            Divider()
            ATMAIDayFeatureStats(features: result.features)
        }.padding(20).atmWorkspaceCard()
    }

    @ViewBuilder private var atlasView: some View {
        if let atlas = store.atlas {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    HStack {
                        Text("徽章星图").font(ATMFont.font(.title2, weight: .semibold))
                        Picker("", selection: $showAtlasMap) { Text("星图").tag(true); Text("列表").tag(false) }.pickerStyle(.segmented).frame(width: 150)
                        Spacer()
                        Text("已解锁 \(atlas.unlocked) / \(atlas.total)").foregroundStyle(ATMTheme.secondary)
                    }
                    ATMAIDayAtlasGuide(badges: atlas.badges)
                    if showAtlasMap {
                        ATMAIDayStarMap(badges: atlas.badges) { selectedBadge = $0 }
                            .frame(height: 620)
                            .atmWorkspaceCard()
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 16)], spacing: 16) {
                            ForEach(atlas.badges) { badge in
                                Button { selectedBadge = badge } label: {
                                    VStack(spacing: 12) {
                                        ATMAIDayBadgeVisual(badge: badge, size: 108)
                                        Text(badge.name).font(ATMFont.font(.body, weight: .semibold)).foregroundStyle(ATMTheme.primary)
                                        Text(badge.unlocked ? "L\(badge.level) · \(badge.qualifiedDays) 天" : "尚未解锁")
                                            .font(ATMFont.caption).foregroundStyle(ATMTheme.secondary)
                                        // "距下一级还差几天" is the part that gives a
                                        // fully-unlocked atlas somewhere left to go.
                                        ATMAIDayProgressLine(badge: badge, style: .atlasTile)
                                    }
                                    .padding(18)
                                    .frame(maxWidth: .infinity)
                                    .atmWorkspaceCard()
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                }.padding(28)
            }
        } else { ProgressView() }
    }

    @ViewBuilder private var historyView: some View {
        if let history = store.history {
            let days = historyFilter == nil ? history.days : history.days.filter { $0.badge?.id == historyFilter }
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    HStack {
                        Text("历史").font(ATMFont.font(.title2, weight: .semibold))
                        Picker("", selection: $historyFilter) {
                            Text("全部徽章").tag(String?.none)
                            ForEach(store.atlas?.badges ?? []) { badge in
                                Text(badge.name).tag(Optional(badge.id))
                            }
                        }.frame(width: 180)
                        Spacer()
                        Text("\(days.count) 天").foregroundStyle(ATMTheme.secondary)
                    }
                    ATMAIDayMonthlyTrend(days: history.days)
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 150), spacing: 12)], spacing: 12) {
                        ForEach(days) { result in
                            // Every card opens the full day. Previously history was a
                            // wall of dates and badges with nothing behind it.
                            Button { openHistoryDay(result) } label: {
                                VStack(alignment: .leading, spacing: 8) {
                                    HStack(spacing: 4) {
                                        Text(result.day).font(.caption).foregroundStyle(ATMTheme.secondary)
                                        if result.concept?.isUserCorrected == true {
                                            Image(systemName: "hand.raised.fill").font(ATMFont.micro).foregroundStyle(ATMTheme.secondary)
                                        }
                                    }
                                    if let badge = result.badge {
                                        ATMAIDayBadgeVisual(badge: badge, size: 70)
                                        Text(badge.name).font(.subheadline.bold()).lineLimit(1)
                                        if let concept = result.concept {
                                            Text("可信度 \(Int(concept.confidence * 100))%")
                                                .font(.caption2).foregroundStyle(ATMTheme.secondary)
                                        }
                                    } else {
                                        Image(systemName: "moon.zzz").font(.title).foregroundStyle(ATMTheme.secondary)
                                        Text("无活动").font(.subheadline)
                                    }
                                }
                                .padding(14).frame(maxWidth: .infinity, minHeight: 152, alignment: .topLeading)
                                .atmWorkspaceCard()
                            }.buttonStyle(.plain)
                        }
                    }
                }.padding(28)
            }
        } else { ProgressView() }
    }

    private func openHistoryDay(_ result: ATMAIDayResult) {
        // The dashboard's history entries are complete results already; re-fetching
        // only matters if a day was built by an older engine version.
        selectedHistoryDay = result
        Task {
            if let fresh = try? await store.loadDay(result.day) { selectedHistoryDay = fresh }
        }
    }

    @ViewBuilder private var privacyView: some View {
        if let privacy = store.privacy {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    Text("数据与隐私").font(ATMFont.font(.title2, weight: .semibold))
                    VStack(alignment: .leading, spacing: 14) {
                        // Turning this off is not a pause: it wipes the labels already
                        // stored and drops the derived projections, and turning it back
                        // on does not restore them without a rebuild. So it confirms.
                        Toggle("本地语义分类", isOn: Binding(
                            get: { privacy.semanticEnabled },
                            set: { enabled in
                                if enabled { store.setSemantic(true) } else { confirmingSemanticOff = true }
                            }
                        ))
                        Text("分类过程仅在本机短暂读取消息；AI Day 表中不保留原文。原文保留状态：\(privacy.rawContentRetained ? "是" : "否")")
                            .font(.caption).foregroundStyle(ATMTheme.secondary)

                        Divider()
                        // Retention changes delete events immediately, so the stepper no
                        // longer applies on each click — it stages a value.
                        let draft = retentionDraft ?? privacy.retentionDays
                        Stepper("衍生事件保留 \(draft) 天", value: Binding(
                            get: { draft },
                            set: { retentionDraft = $0 }
                        ), in: 7...3650, step: 7)
                        HStack {
                            Text(draft < privacy.retentionDays
                                 ? "缩短保留期会立即删除更早的衍生事件，且只有最近 31 天能靠重建恢复。"
                                 : "调整后点击「应用」生效。")
                                .font(.caption).foregroundStyle(draft < privacy.retentionDays ? ATMTheme.danger : ATMTheme.secondary)
                            Spacer()
                            if retentionDraft != nil, draft != privacy.retentionDays {
                                Button("放弃") { retentionDraft = nil }.buttonStyle(.plain).foregroundStyle(ATMTheme.secondary)
                                Button("应用") {
                                    if draft < privacy.retentionDays { confirmingRetention = true } else { store.setRetention(draft); retentionDraft = nil }
                                }.buttonStyle(.borderedProminent)
                            }
                        }
                    }.padding(18).atmWorkspaceCard()

                    Text("来源权限").font(.headline)
                    ForEach(privacy.sources) { source in
                        HStack {
                            VStack(alignment: .leading) { Text(source.source).fontWeight(.medium); Text("\(source.eventCount) 个衍生事件").font(.caption).foregroundStyle(ATMTheme.secondary) }
                            Spacer()
                            Toggle("", isOn: Binding(get: { source.enabled }, set: { store.setSource(source, enabled: $0) })).labelsHidden()
                            Button { sourceToDelete = source } label: { Image(systemName: "trash") }.buttonStyle(.plain).foregroundStyle(ATMTheme.danger).help("删除此来源的 AI Day 衍生数据")
                        }.padding(14).atmWorkspaceCard()
                    }
                    HStack {
                        Button("导出全部 JSON") { exportJSON() }
                        Spacer()
                        Button("删除全部 AI Day 数据", role: .destructive) { confirmingDelete = true }
                    }
                }.padding(28).frame(maxWidth: 760)
            }
        } else { ProgressView() }
    }

    /// Select, then preview, then confirm. Tapping any badge used to submit
    /// immediately, with the badge currently in effect unmarked, so a mis-tap
    /// silently overrode the engine for that day.
    private var correctionSheet: some View {
        let current = store.today?.concept?.id
        let qualified = Set((store.today?.candidates ?? []).map(\.id))
        return VStack(alignment: .leading, spacing: 14) {
            Text("今天更像哪枚徽章？").font(ATMFont.font(.title2, weight: .semibold))
            Text("带 ✦ 的徽章今天本来就达标；其他徽章会作为你的判断记录下来，不会改变下面的实测证据。")
                .font(.caption).foregroundStyle(ATMTheme.secondary)
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 240))], spacing: 8) {
                    ForEach(store.atlas?.badges ?? []) { badge in
                        Button { correctionChoice = badge.id } label: {
                            HStack(spacing: 10) {
                                ATMAIDayBadgeVisual(badge: badge, size: 44)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(badge.name).fontWeight(badge.id == current ? .semibold : .regular)
                                    if badge.id == current {
                                        Text("当前").font(.caption2).foregroundStyle(ATMTheme.accent)
                                    } else if qualified.contains(badge.id) {
                                        Text("✦ 今天达标").font(.caption2).foregroundStyle(ATMTheme.secondary)
                                    }
                                }
                                Spacer()
                                if correctionChoice == badge.id {
                                    Image(systemName: "largecircle.fill.circle").foregroundStyle(ATMTheme.accent)
                                }
                            }
                            .padding(10)
                            .background(correctionChoice == badge.id ? ATMTheme.accent.opacity(0.16) : ATMTheme.surface)
                            .clipShape(RoundedRectangle(cornerRadius: 10))
                            .overlay {
                                if badge.id == current {
                                    RoundedRectangle(cornerRadius: 10).strokeBorder(ATMTheme.accent.opacity(0.5), lineWidth: 1)
                                }
                            }
                        }.buttonStyle(.plain)
                    }
                }
            }
            Divider()
            if let choice = correctionChoice, let badge = (store.atlas?.badges ?? []).first(where: { $0.id == choice }) {
                HStack(spacing: 8) {
                    Image(systemName: "arrow.right.circle.fill").foregroundStyle(ATMTheme.accent)
                    Text("将把今天的结论从「\(store.today?.concept?.title ?? "")」改为「\(badge.name)」，可随时撤销。")
                        .font(.caption)
                }
            }
            HStack {
                Button("取消") { showingCorrection = false; correctionChoice = nil }
                Spacer()
                Button("确认修正") {
                    if let choice = correctionChoice { store.submitFeedback(verdict: "corrected", badge: choice) }
                    showingCorrection = false
                    correctionChoice = nil
                }
                .buttonStyle(.borderedProminent)
                .disabled(correctionChoice == nil || correctionChoice == current)
            }
        }.padding(24).frame(width: 600, height: 560)
    }

    private func exportJSON() {
        Task {
            do {
                let data = try await store.exportData()
                let panel = NSSavePanel(); panel.nameFieldStringValue = "ai-day-export.json"; panel.allowedContentTypes = [.json]
                if panel.runModal() == .OK, let url = panel.url { try data.write(to: url, options: .atomic) }
            } catch { store.errorMessage = error.localizedDescription }
        }
    }

}

/// Explains what the star map is showing and what to aim at next. The map read as
/// decoration before: the connecting lines had no stated meaning, and once every
/// badge was unlocked there was no goal left on screen.
struct ATMAIDayAtlasGuide: View {
    let badges: [ATMAIDayBadge]

    /// How far this badge is from its next level, in the units progression uses.
    static func nextStep(_ badge: ATMAIDayBadge) -> String {
        if let cooldown = badge.cooldownUntil, !cooldown.isEmpty, badge.kind == "instant" {
            return "冷却至 \(cooldown)"
        }
        let remaining = badge.nextLevelDays - badge.qualifiedDays
        if badge.level >= 3 { return "已满级" }
        if remaining <= 0 { return "下次达标即升级" }
        return "距 L\(badge.level + 1) 还差 \(remaining) 天"
    }

    private var closest: [ATMAIDayBadge] {
        badges
            .filter { $0.level < 3 && $0.nextLevelDays > $0.qualifiedDays }
            .sorted { ($0.nextLevelDays - $0.qualifiedDays) < ($1.nextLevelDays - $1.qualifiedDays) }
            .prefix(3)
            .map { $0 }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 18) {
                legend(color: ATMAIDayPalette.cold, text: "连线按获得顺序串起你的徽章轨迹")
                legend(color: ATMTheme.secondary.opacity(0.45), text: "暗色为尚未解锁")
                Spacer()
            }
            if !closest.isEmpty {
                HStack(spacing: 10) {
                    Text("最接近升级").font(.caption.bold()).foregroundStyle(ATMTheme.secondary)
                    ForEach(closest) { badge in
                        Text("\(badge.name) · \(Self.nextStep(badge))")
                            .font(.caption)
                            .padding(.horizontal, 8).padding(.vertical, 3)
                            .background(ATMTheme.surface, in: Capsule())
                    }
                    Spacer()
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .atmWorkspaceCard()
    }

    private func legend(color: Color, text: String) -> some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(text).font(.caption).foregroundStyle(ATMTheme.secondary)
        }
    }
}

private struct ATMAIDayStarMap: View {
    let badges: [ATMAIDayBadge]
    let select: (ATMAIDayBadge) -> Void
    private let positions: [CGPoint] = [
        .init(x: 0.13, y: 0.18), .init(x: 0.36, y: 0.12), .init(x: 0.62, y: 0.19), .init(x: 0.86, y: 0.13),
        .init(x: 0.23, y: 0.48), .init(x: 0.49, y: 0.40), .init(x: 0.76, y: 0.48), .init(x: 0.91, y: 0.38),
        .init(x: 0.12, y: 0.76), .init(x: 0.38, y: 0.79), .init(x: 0.65, y: 0.72), .init(x: 0.86, y: 0.82),
    ]
    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Canvas { context, size in
                    // 网格和连线原来是写死的白色（0.035 / 0.28）——只在深底上成立。改成主题色
                    // 之后，浅色模式下它们是淡灰蓝而不是「什么都没有」。
                    for fraction in stride(from: 0.12, through: 0.9, by: 0.13) {
                        var vertical = Path()
                        vertical.move(to: CGPoint(x: size.width * fraction, y: 0))
                        vertical.addLine(to: CGPoint(x: size.width * fraction, y: size.height))
                        context.stroke(vertical, with: .color(ATMTheme.chartGrid.opacity(0.55)), lineWidth: 0.5)
                    }
                    var path = Path()
                    for index in 1..<min(positions.count, badges.count) {
                        let previous = positions[index - 1], current = positions[index]
                        path.move(to: CGPoint(x: previous.x * size.width, y: previous.y * size.height))
                        path.addLine(to: CGPoint(x: current.x * size.width, y: current.y * size.height))
                    }
                    context.stroke(
                        path,
                        with: .linearGradient(
                            Gradient(colors: [
                                ATMTheme.accent.opacity(0.20),
                                ATMTheme.accent.opacity(0.70),
                                ATMTheme.accent.opacity(0.20),
                            ]),
                            startPoint: .zero,
                            endPoint: CGPoint(x: size.width, y: size.height)
                        ),
                        style: StrokeStyle(lineWidth: 0.8, dash: [2, 8])
                    )
                }
                ForEach(Array(badges.enumerated()), id: \.element.id) { index, badge in
                    let point = positions[index % positions.count]
                    Button { select(badge) } label: {
                        VStack(spacing: 3) {
                            ATMAIDayBadgeVisual(badge: badge, size: 94)
                            Text(badge.name)
                                .font(ATMFont.mono(.micro, .medium))
                                .tracking(0.5)
                                .foregroundStyle(badge.unlocked ? ATMTheme.primary : ATMTheme.secondary)
                                .lineLimit(1)
                            Text(badge.unlocked ? "L\(badge.level)" : "—")
                                .font(ATMFont.mono(.micro))
                                .foregroundStyle(ATMTheme.secondary)
                        }
                    }.buttonStyle(.plain)
                        // Hovering says what the node is without opening the sheet.
                        .help("\(badge.name) · \(badge.unlocked ? "L\(badge.level) · 累计 \(badge.qualifiedDays) 天" : "尚未解锁")\n\(ATMAIDayAtlasGuide.nextStep(badge))\n\(badge.description)")
                        .position(x: point.x * proxy.size.width, y: point.y * proxy.size.height)
                }
            }
        }
    }
}

/// Says out loud how current and how complete the card is. Without it a day in
/// progress and a finished day looked identical, and a source that had not yet
/// flushed read as "you did none of that today".
private struct ATMAIDayStatusStrip: View {
    let result: ATMAIDayResult
    let lastRefreshed: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                if result.isProvisional {
                    label("今天还没结束，结论会随数据到达变化", icon: "hourglass", tint: ATMTheme.accent)
                }
                Text("数据截至 \(ATMAIDayFormat.clock(result.coverage?.dataThroughDate)) · 更新于 \(ATMAIDayFormat.clock(lastRefreshed ?? result.generatedAtDate))")
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                Spacer()
            }
            if let coverage = result.coverage, !coverage.complete, let missing = coverage.missingSources, !missing.isEmpty {
                label("数据可能不完整：近 7 天活跃的 \(missing.joined(separator: "、")) 今天还没有事件", icon: "exclamationmark.triangle.fill", tint: ATMTheme.warning)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func label(_ text: String, icon: String, tint: Color) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon).font(ATMFont.micro)
            Text(text)
        }
        .font(ATMFont.font(.caption, weight: .medium))
        .foregroundStyle(tint)
        .padding(.horizontal, 10).padding(.vertical, 5)
        .background(tint.opacity(0.12), in: Capsule())
    }
}

private struct ATMAIDayBadgeDetail: View {
    let badge: ATMAIDayBadge
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        VStack(spacing: 16) {
            HStack { Spacer(); Button { dismiss() } label: { Image(systemName: "xmark") }.buttonStyle(.plain) }
            ATMAIDayBadgeVisual(badge: badge, size: 150)
            Text(badge.name).font(ATMFont.font(.title1, weight: .semibold))
            Text(badge.description).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center)
            Text(badge.unlocked ? "等级 L\(badge.level) · 累计 \(badge.qualifiedDays) 天" : "尚未解锁")
            ATMAIDayProgressLine(badge: badge, style: .badgeSheet)
            if let cooldown = badge.cooldownUntil, !cooldown.isEmpty {
                Text("即时徽章冷却至 \(cooldown)").font(.caption).foregroundStyle(ATMTheme.secondary)
            }
            if let evidence = badge.evidence, !evidence.isEmpty {
                VStack(alignment: .leading, spacing: 5) {
                    Text("最近一次达成的证据").font(.headline)
                    ForEach(evidence) { item in
                        HStack {
                            Text(ATMAIDayLabels.evidence(item.metric)).font(.caption)
                            Spacer()
                            Text(ATMAIDayLabels.value(item)).font(.caption.monospaced())
                        }
                    }
                }.frame(width: 260)
            }
            if !badge.qualifiedDates.isEmpty {
                Text("最近达成").font(.headline)
                Text(badge.qualifiedDates.prefix(6).joined(separator: "  ·  "))
                    .font(.caption).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center)
            }
            Spacer()
            Button("完成") { dismiss() }
        }.padding(28).frame(width: 440, height: 600)
    }
}

/// 空态走全 App 那一份。此前是自己写的一行：48pt 图标、`.title3.bold()` 标题、40pt 内边距，
/// 跟其他页的空态没有一处对得上。
private struct ATMAIDayEmptyCard: View {
    let day: String

    var body: some View {
        ATMEmptyState(
            icon: "moon.stars.fill",
            title: "\(day)没有可用的 AI 活动",
            detail: "AI Day 不会为缺失数据编造概念或徽章。"
        )
    }
}

/// The full result for one past day: what history cards now open into, so a date
/// and a badge name are no longer the whole story.
private struct ATMAIDayDetailSheet: View {
    let result: ATMAIDayResult
    @Environment(\.dismiss) private var dismiss
    @State private var showingShare = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(result.day).font(ATMFont.mono(.body, .medium)).foregroundStyle(ATMTheme.secondary)
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark") }.buttonStyle(.plain)
            }.padding(20)
            Divider()
            ScrollView {
                if let badge = result.badge, let concept = result.concept {
                    VStack(alignment: .leading, spacing: 18) {
                        ATMAIDayResultCard(result: result, badge: badge, concept: concept, style: .sheet)
                        VStack(alignment: .leading, spacing: 10) {
                            Text("证据").font(.headline)
                            ATMAIDayEvidenceList(evidence: concept.evidence, spacing: 10)
                            Divider()
                            ATMAIDayFeatureStats(features: result.features)
                        }.padding(18).atmWorkspaceCard()
                        if !result.candidates.isNilOrEmpty {
                            VStack(alignment: .leading, spacing: 8) {
                                Text("当天其他达标徽章").font(.headline)
                                ForEach(result.candidates ?? []) { candidate in
                                    if candidate.id != badge.id {
                                        HStack {
                                            Text(candidate.name)
                                            Spacer()
                                            Text(String(format: "%.2f", candidate.score ?? 0))
                                                .font(.caption.monospaced()).foregroundStyle(ATMTheme.secondary)
                                        }
                                    }
                                }
                            }.padding(18).atmWorkspaceCard()
                        }
                        Button { showingShare = true } label: { Label("生成分享卡", systemImage: "square.and.arrow.up") }
                            .buttonStyle(.borderedProminent)
                    }.padding(20)
                } else {
                    ATMAIDayEmptyCard(day: result.day)
                }
            }
        }
        .frame(width: 620, height: 640)
        .sheet(isPresented: $showingShare) { ATMAIDayShareSheet(result: result) }
    }

}

extension Optional where Wrapped == [ATMAIDayBadge] {
    var isNilOrEmpty: Bool { self?.isEmpty ?? true }
}

/// A month-by-month count of days with a result, so history has some shape
/// beyond a flat grid of cards.
private struct ATMAIDayMonthlyTrend: View {
    let days: [ATMAIDayResult]

    private var months: [(label: String, count: Int)] {
        var order: [String] = []
        var counts: [String: Int] = [:]
        for day in days.reversed() where day.hasContent {
            let month = String(day.day.prefix(7))
            if counts[month] == nil { order.append(month) }
            counts[month, default: 0] += 1
        }
        return order.suffix(6).map { ($0, counts[$0] ?? 0) }
    }

    var body: some View {
        let months = self.months
        let peak = max(months.map(\.count).max() ?? 1, 1)
        if months.count > 1 {
            HStack(alignment: .bottom, spacing: 14) {
                ForEach(months, id: \.label) { month in
                    VStack(spacing: 5) {
                        Text("\(month.count)").font(.caption2.monospaced()).foregroundStyle(ATMTheme.secondary)
                        RoundedRectangle(cornerRadius: 3)
                            .fill(ATMTheme.accent.opacity(0.55))
                            .frame(width: 26, height: max(4, CGFloat(month.count) / CGFloat(peak) * 54))
                        Text(String(month.label.suffix(2)) + "月").font(.caption2).foregroundStyle(ATMTheme.secondary)
                    }
                }
                Spacer()
            }
            .padding(16)
            .atmWorkspaceCard()
        }
    }
}

private struct ATMAIDayShareSheet: View {
    let result: ATMAIDayResult
    @Environment(\.dismiss) private var dismiss
    @State private var includeEvidence = true
    @State private var includeStats = true
    @State private var includeDate = true
    var body: some View {
        HStack(spacing: 24) {
            ATMAIDayShareCard(result: result, includeEvidence: includeEvidence, includeStats: includeStats, includeDate: includeDate).frame(width: 360, height: 450).clipShape(RoundedRectangle(cornerRadius: 12)).shadow(radius: 8)
            VStack(alignment: .leading, spacing: 14) { Text("4:5 分享卡").font(ATMFont.font(.title2, weight: .semibold)); Toggle("日期",isOn:$includeDate);Toggle("证据",isOn:$includeEvidence);Toggle("使用统计",isOn:$includeStats);Spacer();Button("导出 1080 × 1350 PNG") { exportPNG() }.buttonStyle(.borderedProminent);Button("取消") { dismiss() } }
        }.padding(28).frame(width: 690, height: 520)
    }
    @MainActor private func exportPNG() {
        let card = ATMAIDayShareCard(result: result, includeEvidence: includeEvidence, includeStats: includeStats, includeDate: includeDate).frame(width:1080,height:1350)
        let renderer = ImageRenderer(content: card); renderer.scale = 1
        guard let image=renderer.nsImage, let tiff=image.tiffRepresentation, let bitmap=NSBitmapImageRep(data:tiff), let data=bitmap.representation(using:.png,properties:[:]) else{return}
        let panel=NSSavePanel();panel.nameFieldStringValue="AI-Day-\(result.day).png";panel.allowedContentTypes=[.png]
        if panel.runModal() == .OK, let url=panel.url { try? data.write(to:url,options:.atomic);dismiss() }
    }
}

/// 分享卡是这一页唯一一块**故意**不跟随系统外观的东西，也是唯一一块该用裸字号的地方。
///
/// 它导出成 1080 × 1350 的 PNG 发出去，得在别人的设备上长成同一个样，所以底色固定深色，
/// 跟本机是浅色还是深色无关。字号全部写成 `N * unit`（`unit = width / 1080`），因为同一个
/// 视图既要在 360pt 的预览里显示、又要在 1080px 的画布上渲染，只有按宽度换算两者才一致——
/// 换成 `ATMFont` 的固定档位会让预览和导出图的排版不一样。别把这里「统一」掉。
private struct ATMAIDayShareCard: View {
    let result: ATMAIDayResult;let includeEvidence:Bool;let includeStats:Bool;let includeDate:Bool
    var body: some View {
        GeometryReader { proxy in
            let width = proxy.size.width
            let unit = width / 1080
            ZStack {
                Color(red: 0.012, green: 0.018, blue: 0.027)
                RadialGradient(
                    colors: [Color(red: 0.22, green: 0.58, blue: 0.84).opacity(0.19), .clear],
                    center: UnitPoint(x: 0.50, y: 0.30),
                    startRadius: 0,
                    endRadius: width * 0.58
                )
                Canvas { context, size in
                    for index in 0..<9 {
                        let x = size.width * (0.08 + Double(index) * 0.105)
                        var path = Path()
                        path.move(to: CGPoint(x: x, y: 0))
                        path.addLine(to: CGPoint(x: x, y: size.height))
                        context.stroke(path, with: .color(.white.opacity(0.025)), lineWidth: max(0.5, unit))
                    }
                }
                VStack(spacing: 0) {
                    HStack(alignment: .firstTextBaseline) {
                        Text("AI DAY")
                            .font(.system(size: 25 * unit, weight: .medium, design: .monospaced))
                            .tracking(7 * unit)
                        Spacer()
                        if includeDate {
                            Text(result.day)
                                .font(.system(size: 20 * unit, weight: .regular, design: .monospaced))
                        }
                    }
                    .foregroundStyle(.white.opacity(0.52))

                    Spacer(minLength: 12 * unit)
                    if let badge = result.badge {
                        ATMAIDayBadgeVisual(badge: badge, size: width * 0.47, plate: false)
                    }
                    Spacer(minLength: 18 * unit)

                    Text(result.concept?.title ?? "AI Day")
                        .font(.system(size: 62 * unit, weight: .semibold))
                        .foregroundStyle(.white)
                        .multilineTextAlignment(.center)
                    Text(result.concept?.explanation ?? "")
                        .font(.system(size: 27 * unit, weight: .regular))
                        .foregroundStyle(.white.opacity(0.62))
                        .multilineTextAlignment(.center)
                        .lineSpacing(8 * unit)
                        .frame(maxWidth: width * 0.76)
                        .padding(.top, 24 * unit)

                    Spacer(minLength: 26 * unit)
                    if includeEvidence {
                        // Scaled up and given a level line: the evidence is the
                        // substance of the card, and it used to be the smallest,
                        // faintest thing on it with the lower third left empty.
                        HStack(alignment: .top, spacing: 0) {
                            ForEach(Array((result.concept?.evidence ?? []).prefix(3))) { evidence in
                                VStack(spacing: 10 * unit) {
                                    Text(ATMAIDayLabels.compactValue(evidence))
                                        .font(.system(size: 46 * unit, weight: .medium, design: .monospaced))
                                        .foregroundStyle(.white.opacity(0.92))
                                        .lineLimit(1).minimumScaleFactor(0.5)
                                    Text(ATMAIDayLabels.compact(evidence.metric).uppercased())
                                        .font(.system(size: 16 * unit, weight: .medium, design: .monospaced))
                                        .tracking(1.6 * unit)
                                        .foregroundStyle(.white.opacity(0.46))
                                        .multilineTextAlignment(.center)
                                }
                                .frame(maxWidth: .infinity)
                                .overlay(alignment: .leading) {
                                    Rectangle().fill(.white.opacity(0.12)).frame(width: max(0.5, unit))
                                }
                            }
                        }
                        .frame(maxWidth: width * 0.84)
                    }
                    if let badge = result.badge {
                        HStack(spacing: 10 * unit) {
                            Text(badge.unlocked ? "FORM 0\(badge.level)" : "SEED")
                                .font(.system(size: 15 * unit, weight: .semibold, design: .monospaced))
                                .tracking(1.2 * unit)
                                .foregroundStyle(.white.opacity(0.60))
                            Capsule().fill(.white.opacity(0.14)).frame(width: width * 0.24, height: 4 * unit)
                                .overlay(alignment: .leading) {
                                    Capsule().fill(ATMAIDayPalette.cold.opacity(0.85))
                                        .frame(width: width * 0.24 * max(0.02, badge.progress), height: 4 * unit)
                                }
                            Text("\(badge.qualifiedDays)/\(badge.nextLevelDays) 天")
                                .font(.system(size: 14 * unit, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.42))
                        }
                        .padding(.top, 30 * unit)
                    }
                    if includeStats {
                        Text("\(result.features.sessionCount) SESSIONS  /  \(result.features.turnCount) TURNS  /  \(result.features.toolCalls) TOOLS  /  \(ATMAIDayFormat.tokens(result.features.workTokens)) TOKENS")
                            .font(.system(size: 18 * unit, weight: .regular, design: .monospaced))
                            .tracking(0.9 * unit)
                            .foregroundStyle(.white.opacity(0.40))
                            .padding(.top, 22 * unit)
                    }
                    Spacer(minLength: 24 * unit)
                    HStack {
                        Text(result.isProvisional ? "PROVISIONAL / DAY IN PROGRESS" : "COMPUTED LOCALLY")
                        Spacer()
                        Text("DATA THROUGH \(ATMAIDayFormat.clock(result.coverage?.dataThroughDate))")
                        Spacer()
                        Text("ATM")
                    }
                    .font(.system(size: 13 * unit, weight: .medium, design: .monospaced))
                    .tracking(1.4 * unit)
                    .foregroundStyle(.white.opacity(0.26))
                }
                .padding(68 * unit)
            }
        }
        .aspectRatio(4/5, contentMode: .fit)
    }
}
