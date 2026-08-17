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
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("AI Day").font(.system(size: 22, weight: .bold))
                    Text("每天一个概念、一枚徽章、一组可验证证据")
                        .font(.caption).foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                if store.isLoading { ProgressView().controlSize(.small) }
                Button { store.refresh() } label: { Image(systemName: "arrow.clockwise") }
                    .buttonStyle(.plain).help("刷新 AI Day")
            }
            .padding(.horizontal, 24).padding(.vertical, 16)

            Picker("", selection: $tab) {
                ForEach(ATMAIDayTab.allCases) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented).labelsHidden().frame(maxWidth: 520).padding(.bottom, 14)

            Divider()
            Group {
                switch tab {
                case .today: todayView
                case .atlas: atlasView
                case .history: historyView
                case .privacy: privacyView
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
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

    @ViewBuilder private var todayView: some View {
        if let result = store.today {
            ScrollView {
                VStack(spacing: 18) {
                    if !result.hasContent {
                        ATMAIDayEmptyCard(day: result.day)
                    } else if let badge = result.badge, let concept = result.concept {
                        ATMAIDayStatusStrip(result: result, lastRefreshed: store.lastRefreshed)
                        HStack(alignment: .top, spacing: 30) {
                            ATMAIDayBadgeVisual(badge: badge, size: 230)
                            VStack(alignment: .leading, spacing: 12) {
                                Text("AI DAY  /  \(result.day)")
                                    .font(.system(size: 11, weight: .medium, design: .monospaced))
                                    .tracking(1.8)
                                    .foregroundStyle(.white.opacity(0.46))
                                Text(concept.title).font(.system(size: 30, weight: .semibold)).foregroundStyle(.white)
                                Text(concept.explanation).font(.system(size: 15)).foregroundStyle(.white.opacity(0.64))
                                if concept.isUserCorrected, let computed = concept.computedTitle, !computed.isEmpty {
                                    HStack(spacing: 6) {
                                        Image(systemName: "hand.raised.fill").font(.system(size: 9))
                                        Text("由你修正 · 引擎原判断「\(computed)」")
                                    }
                                    .font(.system(size: 11, weight: .medium))
                                    .foregroundStyle(Color(red: 1.0, green: 0.83, blue: 0.53))
                                    .padding(.horizontal, 9).padding(.vertical, 4)
                                    .background(Color(red: 1.0, green: 0.83, blue: 0.53).opacity(0.12), in: Capsule())
                                }
                                HStack {
                                    Text(badge.level > 0 ? "FORM / 0\(badge.level)" : "FORM / SEED")
                                    Text("证据强度 \(Int(concept.strength * 100))%")
                                    Text("可信度 \(Int(concept.confidence * 100))%")
                                    Text("基线 \(result.baselineDays) 天")
                                }
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.48))
                                .help("证据强度是今天这枚徽章的信号强弱；可信度综合了基线长度、证据强度和来源覆盖度。用户纠正不会提高可信度。")
                                ProgressView(value: badge.progress).tint(Color(red: 0.60, green: 0.86, blue: 1.0)).frame(maxWidth: 320)
                                if badge.nextLevelDays > badge.qualifiedDays {
                                    Text("距 L\(badge.level + 1) 还差 \(badge.nextLevelDays - badge.qualifiedDays) 天")
                                        .font(.system(size: 11, design: .monospaced))
                                        .foregroundStyle(.white.opacity(0.38))
                                }
                            }.frame(maxWidth: 460, alignment: .leading)
                        }
                        .padding(28)
                        .background { ATMAIDayFieldBackground(cornerRadius: 22) }

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
            ForEach(result.concept?.evidence ?? []) { evidence in
                HStack {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(ATMTheme.accent)
                    Text(evidenceLabel(evidence.metric))
                    Spacer()
                    Text(metricValue(evidence)).monospacedDigit().fontWeight(.semibold)
                    if let comparison = evidence.comparison, !comparison.isEmpty {
                        Text(comparison.replacingOccurrences(of: "recent_p", with: "近 30 日 P"))
                            .font(.caption).foregroundStyle(ATMTheme.secondary)
                    }
                }
            }
            Divider()
            HStack(spacing: 22) {
                metric("会话", "\(result.features.sessionCount)")
                metric("轮次", "\(result.features.turnCount)")
                metric("工具", "\(result.features.toolCalls)")
                // Cache reads are shown as a footnote rather than folded into the
                // headline: they track context size, not work, and dwarf the rest.
                metric("有效 Token", ATMAIDayFormat.tokens(result.features.workTokens))
                metric("含缓存", ATMAIDayFormat.tokens(result.features.totalTokens))
            }
        }.padding(20).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 16))
    }

    @ViewBuilder private var atlasView: some View {
        if let atlas = store.atlas {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    HStack {
                        Text("徽章星图").font(.title2.bold())
                        Picker("", selection: $showAtlasMap) { Text("星图").tag(true); Text("列表").tag(false) }.pickerStyle(.segmented).frame(width: 150)
                        Spacer()
                        Text("已解锁 \(atlas.unlocked) / \(atlas.total)").foregroundStyle(ATMTheme.secondary)
                    }
                    ATMAIDayAtlasGuide(badges: atlas.badges)
                    if showAtlasMap {
                        ATMAIDayStarMap(badges: atlas.badges) { selectedBadge = $0 }.frame(height: 620)
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 16)], spacing: 16) {
                            ForEach(atlas.badges) { badge in
                                Button { selectedBadge = badge } label: {
                                    VStack(spacing: 12) {
                                        ATMAIDayBadgeVisual(badge: badge, size: 108)
                                        Text(badge.name).font(.headline).foregroundStyle(.white)
                                        Text(badge.unlocked ? "L\(badge.level) · \(badge.qualifiedDays) 天" : "尚未解锁")
                                            .font(.caption).foregroundStyle(.white.opacity(0.46))
                                        ProgressView(value: badge.progress).tint(Color(red: 0.60, green: 0.86, blue: 1.0))
                                        // "距下一级还差几天" is the part that gives a
                                        // fully-unlocked atlas somewhere left to go.
                                        Text(ATMAIDayAtlasGuide.nextStep(badge))
                                            .font(.caption2).foregroundStyle(.white.opacity(0.34))
                                    }
                                    .padding(18)
                                    .frame(maxWidth: .infinity)
                                    .background { ATMAIDayFieldBackground(cornerRadius: 16) }
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
                        Text("历史").font(.title2.bold())
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
                                            Image(systemName: "hand.raised.fill").font(.system(size: 8)).foregroundStyle(ATMTheme.secondary)
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
                                .background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 14))
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
                    Text("数据与隐私").font(.title2.bold())
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
                    }.padding(18).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 14))

                    Text("来源权限").font(.headline)
                    ForEach(privacy.sources) { source in
                        HStack {
                            VStack(alignment: .leading) { Text(source.source).fontWeight(.medium); Text("\(source.eventCount) 个衍生事件").font(.caption).foregroundStyle(ATMTheme.secondary) }
                            Spacer()
                            Toggle("", isOn: Binding(get: { source.enabled }, set: { store.setSource(source, enabled: $0) })).labelsHidden()
                            Button { sourceToDelete = source } label: { Image(systemName: "trash") }.buttonStyle(.plain).foregroundStyle(ATMTheme.danger).help("删除此来源的 AI Day 衍生数据")
                        }.padding(14).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 12))
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
            Text("今天更像哪枚徽章？").font(.title2.bold())
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

    private func metric(_ title: String, _ value: String) -> some View { VStack(alignment: .leading) { Text(value).font(.headline).monospacedDigit(); Text(title).font(.caption).foregroundStyle(ATMTheme.secondary) } }
    private func evidenceLabel(_ metric: String) -> String { ["source_count":"AI 来源","session_count":"会话","turn_count":"对话轮次","tool_calls":"工具调用","total_tokens":"Token","code_events":"代码事件","visual_events":"视觉事件","quality_loops":"质检循环","refinements":"细化","detail_turns":"细节追问","modality_count":"任务模态","corrections":"纠正","acceptances":"直接确认","consecutive_days":"连续使用","user_correction":"用户纠正"][metric] ?? metric }
    private func metricValue(_ evidence: ATMAIDayEvidence) -> String { let value = evidence.value.rounded() == evidence.value ? String(Int(evidence.value)) : String(format: "%.1f", evidence.value); return evidence.unit.map { "\(value) \($0)" } ?? value }
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
                legend(color: Color(red: 0.60, green: 0.86, blue: 1.0), text: "连线按获得顺序串起你的徽章轨迹")
                legend(color: .white.opacity(0.30), text: "暗色为尚未解锁")
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
        .background(ATMTheme.elevated)
        .clipShape(RoundedRectangle(cornerRadius: 12))
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
                ATMAIDayFieldBackground(cornerRadius: 20)
                Canvas { context, size in
                    let grid = Color.white.opacity(0.035)
                    for fraction in stride(from: 0.12, through: 0.9, by: 0.13) {
                        var vertical = Path()
                        vertical.move(to: CGPoint(x: size.width * fraction, y: 0))
                        vertical.addLine(to: CGPoint(x: size.width * fraction, y: size.height))
                        context.stroke(vertical, with: .color(grid), lineWidth: 0.5)
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
                                Color(red: 0.58, green: 0.84, blue: 1.0).opacity(0.08),
                                Color.white.opacity(0.28),
                                Color(red: 0.58, green: 0.84, blue: 1.0).opacity(0.08),
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
                                .font(.system(size: 10, weight: .medium, design: .monospaced))
                                .tracking(0.5)
                                .foregroundStyle(.white.opacity(badge.unlocked ? 0.72 : 0.30))
                                .lineLimit(1)
                            Text(badge.unlocked ? "L\(badge.level)" : "—")
                                .font(.system(size: 9, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.34))
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

enum ATMAIDayFormat {
    /// "17.2M" rather than "17,214,880" — the exact digit count of a token total
    /// carries no meaning to a reader and crowds out what does.
    static func tokens(_ value: Int64) -> String {
        let double = Double(value)
        switch value {
        case 1_000_000...: return String(format: "%.1fM", double / 1_000_000)
        case 1_000...: return String(format: "%.1fK", double / 1_000)
        default: return "\(value)"
        }
    }

    static func clock(_ date: Date?) -> String {
        guard let date else { return "未知" }
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm"
        return formatter.string(from: date)
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
                    label("今天还没结束，结论会随数据到达变化", icon: "hourglass", tint: Color(red: 0.62, green: 0.80, blue: 1.0))
                }
                Text("数据截至 \(ATMAIDayFormat.clock(result.coverage?.dataThroughDate)) · 更新于 \(ATMAIDayFormat.clock(lastRefreshed ?? result.generatedAtDate))")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(ATMTheme.secondary)
                Spacer()
            }
            if let coverage = result.coverage, !coverage.complete, let missing = coverage.missingSources, !missing.isEmpty {
                label("数据可能不完整：近 7 天活跃的 \(missing.joined(separator: "、")) 今天还没有事件", icon: "exclamationmark.triangle.fill", tint: Color(red: 1.0, green: 0.78, blue: 0.45))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func label(_ text: String, icon: String, tint: Color) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon).font(.system(size: 10))
            Text(text)
        }
        .font(.system(size: 11, weight: .medium))
        .foregroundStyle(tint)
        .padding(.horizontal, 10).padding(.vertical, 5)
        .background(tint.opacity(0.12), in: Capsule())
    }
}

private struct ATMAIDayFieldBackground: View {
    let cornerRadius: CGFloat

    var body: some View {
        RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
            .fill(Color(red: 0.018, green: 0.026, blue: 0.038))
            .overlay {
                RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                    .fill(
                        RadialGradient(
                            colors: [Color(red: 0.31, green: 0.67, blue: 0.90).opacity(0.13), .clear],
                            center: UnitPoint(x: 0.25, y: 0.12),
                            startRadius: 0,
                            endRadius: 430
                        )
                    )
            }
            .overlay {
                RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                    .strokeBorder(
                        LinearGradient(
                            colors: [.white.opacity(0.16), .white.opacity(0.025)],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        ),
                        lineWidth: 0.75
                    )
            }
    }
}

private struct ATMAIDayBadgeDetail: View {
    let badge: ATMAIDayBadge
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        VStack(spacing: 16) {
            HStack { Spacer(); Button { dismiss() } label: { Image(systemName: "xmark") }.buttonStyle(.plain) }
            ATMAIDayBadgeVisual(badge: badge, size: 150)
            Text(badge.name).font(.title.bold())
            Text(badge.description).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center)
            Text(badge.unlocked ? "等级 L\(badge.level) · 累计 \(badge.qualifiedDays) 天" : "尚未解锁")
            ProgressView(value: badge.progress).frame(width: 260)
            Text(ATMAIDayAtlasGuide.nextStep(badge)).font(.caption).foregroundStyle(ATMTheme.accent)
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

private struct ATMAIDayEmptyCard: View { let day:String; var body: some View { VStack(spacing:12){Image(systemName:"moon.stars.fill").font(.system(size:48)).foregroundStyle(ATMTheme.secondary);Text("\(day)没有可用的 AI 活动").font(.title3.bold());Text("AI Day 不会为缺失数据编造概念或徽章。").foregroundStyle(ATMTheme.secondary)}.padding(40) } }

/// The full result for one past day: what history cards now open into, so a date
/// and a badge name are no longer the whole story.
private struct ATMAIDayDetailSheet: View {
    let result: ATMAIDayResult
    @Environment(\.dismiss) private var dismiss
    @State private var showingShare = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(result.day).font(.system(size: 13, weight: .medium, design: .monospaced)).foregroundStyle(ATMTheme.secondary)
                Spacer()
                Button { dismiss() } label: { Image(systemName: "xmark") }.buttonStyle(.plain)
            }.padding(20)
            Divider()
            ScrollView {
                if let badge = result.badge, let concept = result.concept {
                    VStack(alignment: .leading, spacing: 18) {
                        HStack(alignment: .top, spacing: 22) {
                            ATMAIDayBadgeVisual(badge: badge, size: 150)
                            VStack(alignment: .leading, spacing: 8) {
                                Text(concept.title).font(.system(size: 24, weight: .semibold))
                                Text(concept.explanation).foregroundStyle(ATMTheme.secondary)
                                if concept.isUserCorrected, let computed = concept.computedTitle, !computed.isEmpty {
                                    Text("由你修正 · 引擎原判断「\(computed)」")
                                        .font(.caption).foregroundStyle(Color(red: 1.0, green: 0.83, blue: 0.53))
                                }
                                HStack(spacing: 14) {
                                    Text("L\(badge.level)")
                                    Text("证据 \(Int(concept.strength * 100))%")
                                    Text("可信度 \(Int(concept.confidence * 100))%")
                                    Text("基线 \(result.baselineDays) 天")
                                }
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundStyle(ATMTheme.secondary)
                            }
                        }
                        VStack(alignment: .leading, spacing: 10) {
                            Text("证据").font(.headline)
                            ForEach(concept.evidence) { evidence in
                                HStack {
                                    Image(systemName: "checkmark.seal.fill").foregroundStyle(ATMTheme.accent)
                                    Text(ATMAIDayLabels.evidence(evidence.metric))
                                    Spacer()
                                    Text(ATMAIDayLabels.value(evidence)).monospacedDigit().fontWeight(.semibold)
                                    if let comparison = evidence.comparison, !comparison.isEmpty {
                                        Text(comparison.replacingOccurrences(of: "recent_p", with: "近 30 日 P"))
                                            .font(.caption).foregroundStyle(ATMTheme.secondary)
                                    }
                                }
                            }
                            Divider()
                            HStack(spacing: 22) {
                                stat("会话", "\(result.features.sessionCount)")
                                stat("轮次", "\(result.features.turnCount)")
                                stat("工具", "\(result.features.toolCalls)")
                                stat("有效 Token", ATMAIDayFormat.tokens(result.features.workTokens))
                                stat("含缓存", ATMAIDayFormat.tokens(result.features.totalTokens))
                            }
                        }.padding(18).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 14))
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
                            }.padding(18).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 14))
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

    private func stat(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading) {
            Text(value).font(.headline).monospacedDigit()
            Text(title).font(.caption).foregroundStyle(ATMTheme.secondary)
        }
    }
}

extension Optional where Wrapped == [ATMAIDayBadge] {
    var isNilOrEmpty: Bool { self?.isEmpty ?? true }
}

enum ATMAIDayLabels {
    static func evidence(_ metric: String) -> String {
        [
            "source_count": "AI 来源", "session_count": "会话", "turn_count": "对话轮次",
            "tool_calls": "工具调用", "total_tokens": "Token", "work_tokens": "有效 Token",
            "generation_seconds": "生成秒数", "code_events": "代码事件", "visual_events": "视觉事件",
            "quality_loops": "质检循环", "refinements": "细化", "detail_turns": "细节追问",
            "modality_count": "任务模态", "corrections": "纠正", "acceptances": "直接确认",
            "consecutive_days": "连续使用", "modality_share": "模态占比", "loop_share": "质检占比",
            "detail_share": "追问占比", "correction_share": "纠正占比", "acceptance_share": "确认占比",
        ][metric] ?? metric
    }

    static func value(_ evidence: ATMAIDayEvidence) -> String {
        if evidence.metric.hasSuffix("_tokens") { return ATMAIDayFormat.tokens(Int64(evidence.value)) }
        let number = evidence.value.rounded() == evidence.value
            ? String(Int(evidence.value))
            : String(format: "%.1f", evidence.value)
        guard let unit = evidence.unit, !unit.isEmpty else { return number }
        return unit == "%" ? "\(number)%" : "\(number) \(unit)"
    }
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
            .background(ATMTheme.elevated)
            .clipShape(RoundedRectangle(cornerRadius: 12))
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
            VStack(alignment: .leading, spacing: 14) { Text("4:5 分享卡").font(.title2.bold()); Toggle("日期",isOn:$includeDate);Toggle("证据",isOn:$includeEvidence);Toggle("使用统计",isOn:$includeStats);Spacer();Button("导出 1080 × 1350 PNG") { exportPNG() }.buttonStyle(.borderedProminent);Button("取消") { dismiss() } }
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
                        ATMAIDayBadgeVisual(badge: badge, size: width * 0.47)
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
                                    Text(shareEvidenceValue(evidence))
                                        .font(.system(size: 46 * unit, weight: .medium, design: .monospaced))
                                        .foregroundStyle(.white.opacity(0.92))
                                        .lineLimit(1).minimumScaleFactor(0.5)
                                    Text(shareEvidenceLabel(evidence.metric).uppercased())
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
                                    Capsule().fill(Color(red: 0.60, green: 0.86, blue: 1.0).opacity(0.85))
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

    private func shareEvidenceLabel(_ metric: String) -> String {
        ["source_count": "sources", "session_count": "sessions", "turn_count": "turns", "tool_calls": "tools", "total_tokens": "tokens", "work_tokens": "tokens", "generation_seconds": "seconds", "code_events": "code", "visual_events": "visual", "quality_loops": "quality", "refinements": "refine", "detail_turns": "detail", "modality_count": "modalities", "corrections": "corrections", "acceptances": "accepted", "consecutive_days": "streak", "modality_share": "share", "loop_share": "share", "detail_share": "share", "correction_share": "share", "acceptance_share": "share"][metric] ?? metric
    }

    /// Token counts get the friendly form; percentages keep their sign.
    private func shareEvidenceValue(_ evidence: ATMAIDayEvidence) -> String {
        if evidence.metric.hasSuffix("_tokens") { return ATMAIDayFormat.tokens(Int64(evidence.value)) }
        if evidence.unit == "%" { return "\(Int(evidence.value))%" }
        return String(Int(evidence.value))
    }
}
