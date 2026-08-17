import AppKit
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
    }

    @ViewBuilder private var todayView: some View {
        if let result = store.today {
            ScrollView {
                VStack(spacing: 18) {
                    if result.state == "empty" || result.badge == nil {
                        ATMAIDayEmptyCard(day: result.day)
                    } else if let badge = result.badge, let concept = result.concept {
                        HStack(alignment: .top, spacing: 30) {
                            ATMAIDayBadgeVisual(badge: badge, size: 230)
                            VStack(alignment: .leading, spacing: 12) {
                                Text(result.day).font(.caption).foregroundStyle(ATMTheme.secondary)
                                Text(concept.title).font(.system(size: 30, weight: .bold))
                                Text(concept.explanation).font(.system(size: 15)).foregroundStyle(ATMTheme.secondary)
                                HStack {
                                    Label(badge.level > 0 ? "L\(badge.level)" : "进度中", systemImage: "seal.fill")
                                    Text("置信度 \(Int(concept.confidence * 100))%")
                                    Text("基线 \(result.baselineDays) 天")
                                }.font(.caption).foregroundStyle(ATMTheme.secondary)
                                ProgressView(value: badge.progress).frame(maxWidth: 320)
                            }.frame(maxWidth: 460, alignment: .leading)
                        }
                        .padding(28).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 22))

                        evidenceCard(result)
                        HStack {
                            Button { store.submitFeedback(verdict: "accurate") } label: { Label("准确", systemImage: "checkmark.circle") }
                            Button { store.submitFeedback(verdict: "inaccurate") } label: { Label("不准确", systemImage: "xmark.circle") }
                            Button("纠正徽章") { showingCorrection = true }
                            Spacer()
                            Button { showingShare = true } label: { Label("生成分享卡", systemImage: "square.and.arrow.up") }
                                .buttonStyle(.borderedProminent)
                        }
                    }
                }.padding(28).frame(maxWidth: 920)
            }
        } else if store.isLoading { ProgressView("正在生成今天的 AI Day…") }
        else { ATMAIDayEmptyCard(day: "今天") }
    }

    private func evidenceCard(_ result: ATMAIDayResult) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("为什么是它").font(.headline)
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
                metric("会话", result.features.sessionCount)
                metric("轮次", result.features.turnCount)
                metric("工具", result.features.toolCalls)
                metric("Token", result.features.totalTokens)
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
                    if showAtlasMap {
                        ATMAIDayStarMap(badges: atlas.badges) { selectedBadge = $0 }.frame(height: 620)
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190), spacing: 16)], spacing: 16) {
                            ForEach(atlas.badges) { badge in
                                Button { selectedBadge = badge } label: {
                                    VStack(spacing: 12) {
                                        ATMAIDayBadgeVisual(badge: badge, size: 108)
                                        Text(badge.name).font(.headline)
                                        Text(badge.unlocked ? "L\(badge.level) · \(badge.qualifiedDays) 天" : "尚未解锁")
                                            .font(.caption).foregroundStyle(ATMTheme.secondary)
                                        ProgressView(value: badge.progress)
                                    }.padding(18).frame(maxWidth: .infinity).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 16))
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
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 150), spacing: 12)], spacing: 12) {
                    ForEach(history.days) { result in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(result.day).font(.caption).foregroundStyle(ATMTheme.secondary)
                            if let badge = result.badge {
                                ATMAIDayBadgeVisual(badge: badge, size: 70)
                                Text(badge.name).font(.subheadline.bold()).lineLimit(1)
                            } else {
                                Image(systemName: "moon.zzz").font(.title).foregroundStyle(ATMTheme.secondary)
                                Text("无活动").font(.subheadline)
                            }
                        }.padding(14).frame(maxWidth: .infinity, minHeight: 142, alignment: .topLeading).background(ATMTheme.elevated).clipShape(RoundedRectangle(cornerRadius: 14))
                    }
                }.padding(28)
            }
        } else { ProgressView() }
    }

    @ViewBuilder private var privacyView: some View {
        if let privacy = store.privacy {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    Text("数据与隐私").font(.title2.bold())
                    VStack(alignment: .leading, spacing: 14) {
                        Toggle("本地语义分类", isOn: Binding(get: { privacy.semanticEnabled }, set: { store.setSemantic($0) }))
                        Text("分类过程仅在本机短暂读取消息；AI Day 表中不保留原文。原文保留状态：\(privacy.rawContentRetained ? "是" : "否")")
                            .font(.caption).foregroundStyle(ATMTheme.secondary)
                        Stepper("衍生事件保留 \(privacy.retentionDays) 天", value: Binding(get: { privacy.retentionDays }, set: { store.setRetention($0) }), in: 7...3650, step: 7)
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

    private var correctionSheet: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("今天更像哪枚徽章？").font(.title2.bold())
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 160))]) {
                    ForEach(store.atlas?.badges ?? []) { badge in
                        Button { store.submitFeedback(verdict: "corrected", badge: badge.id); showingCorrection = false } label: {
                            HStack { ATMAIDayBadgeVisual(badge: badge, size: 48); Text(badge.name); Spacer() }
                                .padding(10).background(ATMTheme.surface).clipShape(RoundedRectangle(cornerRadius: 10))
                        }.buttonStyle(.plain)
                    }
                }
            }
            Button("取消") { showingCorrection = false }
        }.padding(24).frame(width: 560, height: 520)
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

    private func metric(_ title: String, _ value: Int64) -> some View { VStack(alignment: .leading) { Text(value.formatted()).font(.headline).monospacedDigit(); Text(title).font(.caption).foregroundStyle(ATMTheme.secondary) } }
    private func evidenceLabel(_ metric: String) -> String { ["source_count":"AI 来源","session_count":"会话","turn_count":"对话轮次","tool_calls":"工具调用","total_tokens":"Token","code_events":"代码事件","visual_events":"视觉事件","quality_loops":"质检循环","refinements":"细化","detail_turns":"细节追问","modality_count":"任务模态","corrections":"纠正","acceptances":"直接确认","consecutive_days":"连续使用","user_correction":"用户纠正"][metric] ?? metric }
    private func metricValue(_ evidence: ATMAIDayEvidence) -> String { let value = evidence.value.rounded() == evidence.value ? String(Int(evidence.value)) : String(format: "%.1f", evidence.value); return evidence.unit.map { "\(value) \($0)" } ?? value }
}

private struct ATMAIDayBadgeVisual: View {
    let badge: ATMAIDayBadge
    let size: CGFloat

    private var isActive: Bool { badge.unlocked || badge.score != nil }
    private var material: ATMAIDayBadgeMaterial { .init(badge: badge, active: isActive) }

    var body: some View {
        ZStack {
            Circle()
                .fill(material.ambient.opacity(isActive ? 0.16 : 0.035))
                .blur(radius: size * 0.11)
                .frame(width: size * 0.82, height: size * 0.82)
                .offset(y: size * 0.05)

            ATMAIDayMedallionShape()
                .fill(material.rim)
                .overlay {
                    ATMAIDayMedallionShape()
                        .stroke(Color.white.opacity(isActive ? 0.30 : 0.08), lineWidth: max(0.7, size * 0.006))
                }
                .shadow(color: Color.black.opacity(0.34), radius: size * 0.045, y: size * 0.035)
                .padding(size * 0.035)

            Circle()
                .fill(material.core)
                .overlay {
                    Circle().stroke(material.innerRing, lineWidth: max(1, size * 0.012))
                }
                .padding(size * 0.105)

            ATMAIDayBadgeTicks(color: material.detail.opacity(isActive ? 0.48 : 0.17))
                .padding(size * 0.075)

            ATMAIDayFamilyEngraving(family: badge.family, color: material.detail)
                .padding(size * 0.18)

            ATMAIDayBadgeGlyph(id: badge.id, color: material.glyph)
                .padding(size * 0.28)

            LinearGradient(
                colors: [Color.white.opacity(isActive ? 0.16 : 0.035), .clear, Color.black.opacity(0.18)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .clipShape(Circle())
            .padding(size * 0.11)
            .allowsHitTesting(false)

            if size >= 78 {
                VStack {
                    HStack {
                        Text(material.ordinal)
                            .font(.system(size: size * 0.055, weight: .medium, design: .monospaced))
                            .tracking(size * 0.007)
                        Spacer()
                    }
                    Spacer()
                    HStack {
                        Spacer()
                        Text(badge.level > 0 ? "L\(badge.level)" : "AI")
                            .font(.system(size: size * 0.052, weight: .semibold, design: .monospaced))
                    }
                }
                .foregroundStyle(material.microtype)
                .padding(size * 0.17)
            }
        }
        .frame(width: size, height: size)
        .drawingGroup(opaque: false, colorMode: .extendedLinear)
        .accessibilityLabel("\(badge.name)，等级 \(badge.level)")
    }
}

private struct ATMAIDayBadgeMaterial {
    let badge: ATMAIDayBadge
    let active: Bool

    var accent: Color {
        guard active else { return Color(red: 0.34, green: 0.36, blue: 0.39) }
        switch badge.family {
        case "orbit": return Color(red: 0.30, green: 0.55, blue: 0.92)
        case "crystal": return Color(red: 0.32, green: 0.78, blue: 0.82)
        case "prism": return Color(red: 0.78, green: 0.42, blue: 0.62)
        case "lens": return Color(red: 0.58, green: 0.48, blue: 0.88)
        default: return Color(red: 0.32, green: 0.70, blue: 0.58)
        }
    }

    var ambient: Color { accent }
    var detail: Color { active ? accent.opacity(0.62) : Color.white.opacity(0.16) }
    var glyph: Color { active ? Color(red: 0.91, green: 0.94, blue: 0.96) : Color.white.opacity(0.30) }
    var microtype: Color { active ? Color.white.opacity(0.46) : Color.white.opacity(0.16) }
    var innerRing: Color { active ? accent.opacity(0.38) : Color.white.opacity(0.09) }

    var rim: AngularGradient {
        let cool = Color(red: 0.72, green: 0.75, blue: 0.79)
        let dark = Color(red: 0.12, green: 0.13, blue: 0.15)
        let levelColor: Color
        switch badge.level {
        case 3: levelColor = Color(red: 0.78, green: 0.70, blue: 0.92)
        case 2: levelColor = Color(red: 0.77, green: 0.69, blue: 0.52)
        default: levelColor = cool
        }
        return AngularGradient(
            colors: active
                ? [dark, levelColor, dark, accent.opacity(0.75), levelColor, dark]
                : [Color.black.opacity(0.92), Color.white.opacity(0.22), Color.black, Color.white.opacity(0.10), Color.black],
            center: .center
        )
    }

    var core: LinearGradient {
        LinearGradient(
            colors: active
                ? [Color(red: 0.10, green: 0.115, blue: 0.14), accent.opacity(0.17), Color(red: 0.025, green: 0.03, blue: 0.04)]
                : [Color(red: 0.105, green: 0.11, blue: 0.12), Color(red: 0.035, green: 0.038, blue: 0.043)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )
    }

    var ordinal: String {
        let ids = ["autopilot", "deep_collaboration", "model_conductor", "visual_director", "code_architect", "quality_inspector", "follow_up", "detail_microscope", "generalist", "hard_to_fool", "first_draft_accepted", "streak"]
        return String(format: "%02d", (ids.firstIndex(of: badge.id) ?? 0) + 1)
    }
}

private struct ATMAIDayMedallionShape: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        let center = CGPoint(x: rect.midX, y: rect.midY)
        let radius = min(rect.width, rect.height) / 2
        for index in 0..<16 {
            let angle = (Double(index) / 16 * 2 * .pi) - .pi / 2
            let point = CGPoint(x: center.x + cos(angle) * radius, y: center.y + sin(angle) * radius)
            index == 0 ? path.move(to: point) : path.addLine(to: point)
        }
        path.closeSubpath()
        return path
    }
}

private struct ATMAIDayBadgeTicks: View {
    let color: Color
    var body: some View {
        Canvas { context, size in
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            let outer = min(size.width, size.height) / 2
            for index in 0..<24 {
                let angle = Double(index) / 24 * 2 * .pi - .pi / 2
                let inner = outer * (index.isMultiple(of: 6) ? 0.91 : 0.955)
                var path = Path()
                path.move(to: CGPoint(x: center.x + cos(angle) * inner, y: center.y + sin(angle) * inner))
                path.addLine(to: CGPoint(x: center.x + cos(angle) * outer, y: center.y + sin(angle) * outer))
                context.stroke(path, with: .color(color), lineWidth: index.isMultiple(of: 6) ? 1.25 : 0.65)
            }
        }
    }
}

private struct ATMAIDayFamilyEngraving: View {
    let family: String
    let color: Color

    var body: some View {
        Canvas { context, size in
            let w = size.width, h = size.height
            var paths: [Path] = []
            switch family {
            case "orbit":
                for scale in [0.58, 0.82] {
                    var path = Path(ellipseIn: CGRect(x: w * (1-scale)/2, y: h * (1-scale)/2, width: w*scale, height: h*scale))
                    paths.append(path)
                }
            case "crystal":
                var path = Path(); path.move(to: CGPoint(x:w*0.5,y:h*0.06)); path.addLine(to:CGPoint(x:w*0.86,y:h*0.34));path.addLine(to:CGPoint(x:w*0.72,y:h*0.88));path.addLine(to:CGPoint(x:w*0.28,y:h*0.88));path.addLine(to:CGPoint(x:w*0.14,y:h*0.34));path.closeSubpath();paths.append(path)
                var facet=Path();facet.move(to:CGPoint(x:w*0.5,y:h*0.06));facet.addLine(to:CGPoint(x:w*0.5,y:h*0.94));facet.move(to:CGPoint(x:w*0.14,y:h*0.34));facet.addLine(to:CGPoint(x:w*0.72,y:h*0.88));facet.move(to:CGPoint(x:w*0.86,y:h*0.34));facet.addLine(to:CGPoint(x:w*0.28,y:h*0.88));paths.append(facet)
            case "prism":
                var triangle=Path();triangle.move(to:CGPoint(x:w*0.5,y:h*0.08));triangle.addLine(to:CGPoint(x:w*0.91,y:h*0.82));triangle.addLine(to:CGPoint(x:w*0.09,y:h*0.82));triangle.closeSubpath();paths.append(triangle)
                for offset in [0.34,0.5,0.66] { var ray=Path();ray.move(to:CGPoint(x:w*0.5,y:h*0.5));ray.addLine(to:CGPoint(x:w*0.96,y:h*offset));paths.append(ray) }
            case "lens":
                for scale in [0.42,0.72,0.92] { paths.append(Path(ellipseIn:CGRect(x:w*(1-scale)/2,y:h*(1-scale)/2,width:w*scale,height:h*scale))) }
            default:
                for offset in [0.25,0.5,0.75] { var path=Path();path.move(to:CGPoint(x:w*offset,y:h*0.08));path.addLine(to:CGPoint(x:w*offset,y:h*0.92));path.move(to:CGPoint(x:w*0.08,y:h*offset));path.addLine(to:CGPoint(x:w*0.92,y:h*offset));paths.append(path) }
            }
            for path in paths { context.stroke(path, with: .color(color.opacity(0.24)), lineWidth: max(0.65, w*0.009)) }
        }
    }
}

private struct ATMAIDayBadgeGlyph: View {
    let id: String
    let color: Color

    var body: some View {
        Canvas { context, size in
            let w = size.width, h = size.height
            var path = Path()
            switch id {
            case "autopilot":
                path.move(to:CGPoint(x:w*0.18,y:h*0.72));path.addLine(to:CGPoint(x:w*0.82,y:h*0.18));path.addLine(to:CGPoint(x:w*0.64,y:h*0.82));path.addLine(to:CGPoint(x:w*0.48,y:h*0.54));path.closeSubpath()
            case "deep_collaboration":
                path.move(to:CGPoint(x:w*0.18,y:h*0.72));path.addLine(to:CGPoint(x:w*0.5,y:h*0.18));path.addLine(to:CGPoint(x:w*0.82,y:h*0.72));path.addLine(to:CGPoint(x:w*0.18,y:h*0.72));path.move(to:CGPoint(x:w*0.34,y:h*0.48));path.addLine(to:CGPoint(x:w*0.66,y:h*0.48))
            case "model_conductor":
                path.addArc(center:CGPoint(x:w*0.5,y:h*0.5),radius:w*0.30,startAngle:.degrees(200),endAngle:.degrees(520),clockwise:false);path.move(to:CGPoint(x:w*0.22,y:h*0.62));path.addLine(to:CGPoint(x:w*0.5,y:h*0.5));path.addLine(to:CGPoint(x:w*0.76,y:h*0.30))
            case "visual_director":
                path.addRoundedRect(in:CGRect(x:w*0.18,y:h*0.22,width:w*0.64,height:h*0.56),cornerSize:CGSize(width:w*0.08,height:w*0.08));path.move(to:CGPoint(x:w*0.5,y:h*0.22));path.addLine(to:CGPoint(x:w*0.5,y:h*0.78));path.move(to:CGPoint(x:w*0.18,y:h*0.5));path.addLine(to:CGPoint(x:w*0.82,y:h*0.5))
            case "code_architect":
                path.move(to:CGPoint(x:w*0.36,y:h*0.2));path.addLine(to:CGPoint(x:w*0.16,y:h*0.5));path.addLine(to:CGPoint(x:w*0.36,y:h*0.8));path.move(to:CGPoint(x:w*0.64,y:h*0.2));path.addLine(to:CGPoint(x:w*0.84,y:h*0.5));path.addLine(to:CGPoint(x:w*0.64,y:h*0.8));path.move(to:CGPoint(x:w*0.58,y:h*0.16));path.addLine(to:CGPoint(x:w*0.42,y:h*0.84))
            case "quality_inspector":
                path.addEllipse(in:CGRect(x:w*0.16,y:h*0.14,width:w*0.55,height:h*0.55));path.move(to:CGPoint(x:w*0.61,y:h*0.61));path.addLine(to:CGPoint(x:w*0.84,y:h*0.84));path.move(to:CGPoint(x:w*0.27,y:h*0.43));path.addLine(to:CGPoint(x:w*0.39,y:h*0.55));path.addLine(to:CGPoint(x:w*0.59,y:h*0.31))
            case "follow_up":
                path.addArc(center:CGPoint(x:w*0.5,y:h*0.5),radius:w*0.31,startAngle:.degrees(35),endAngle:.degrees(325),clockwise:false);path.move(to:CGPoint(x:w*0.77,y:h*0.24));path.addLine(to:CGPoint(x:w*0.82,y:h*0.45));path.addLine(to:CGPoint(x:w*0.61,y:h*0.39))
            case "detail_microscope":
                path.addEllipse(in:CGRect(x:w*0.18,y:h*0.14,width:w*0.48,height:h*0.48));path.move(to:CGPoint(x:w*0.58,y:h*0.56));path.addLine(to:CGPoint(x:w*0.84,y:h*0.82));path.move(to:CGPoint(x:w*0.34,y:h*0.38));path.addLine(to:CGPoint(x:w*0.5,y:h*0.38));path.move(to:CGPoint(x:w*0.42,y:h*0.3));path.addLine(to:CGPoint(x:w*0.42,y:h*0.46))
            case "generalist":
                path.addEllipse(in:CGRect(x:w*0.15,y:h*0.15,width:w*0.28,height:h*0.28));path.addRect(CGRect(x:w*0.57,y:h*0.15,width:w*0.28,height:h*0.28));path.move(to:CGPoint(x:w*0.29,y:h*0.57));path.addLine(to:CGPoint(x:w*0.43,y:h*0.85));path.addLine(to:CGPoint(x:w*0.15,y:h*0.85));path.closeSubpath();path.move(to:CGPoint(x:w*0.71,y:h*0.55));path.addLine(to:CGPoint(x:w*0.85,y:h*0.71));path.addLine(to:CGPoint(x:w*0.71,y:h*0.87));path.addLine(to:CGPoint(x:w*0.57,y:h*0.71));path.closeSubpath()
            case "hard_to_fool":
                path.move(to:CGPoint(x:w*0.5,y:h*0.12));path.addLine(to:CGPoint(x:w*0.82,y:h*0.27));path.addLine(to:CGPoint(x:w*0.75,y:h*0.68));path.addLine(to:CGPoint(x:w*0.5,y:h*0.88));path.addLine(to:CGPoint(x:w*0.25,y:h*0.68));path.addLine(to:CGPoint(x:w*0.18,y:h*0.27));path.closeSubpath();path.move(to:CGPoint(x:w*0.34,y:h*0.5));path.addLine(to:CGPoint(x:w*0.46,y:h*0.62));path.addLine(to:CGPoint(x:w*0.68,y:h*0.37))
            case "first_draft_accepted":
                path.move(to:CGPoint(x:w*0.2,y:h*0.5));path.addLine(to:CGPoint(x:w*0.42,y:h*0.72));path.addLine(to:CGPoint(x:w*0.82,y:h*0.27))
            default:
                path.move(to:CGPoint(x:w*0.5,y:h*0.1));path.addCurve(to:CGPoint(x:w*0.5,y:h*0.9),control1:CGPoint(x:w*0.82,y:h*0.38),control2:CGPoint(x:w*0.75,y:h*0.75));path.addCurve(to:CGPoint(x:w*0.5,y:h*0.1),control1:CGPoint(x:w*0.2,y:h*0.72),control2:CGPoint(x:w*0.25,y:h*0.38))
            }
            context.stroke(path, with: .color(color), style: StrokeStyle(lineWidth:max(1.6,w*0.055),lineCap:.round,lineJoin:.round))
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
                    var path = Path()
                    for index in 1..<min(positions.count, badges.count) {
                        let previous = positions[index - 1], current = positions[index]
                        path.move(to: CGPoint(x: previous.x * size.width, y: previous.y * size.height))
                        path.addLine(to: CGPoint(x: current.x * size.width, y: current.y * size.height))
                    }
                    context.stroke(path, with: .color(ATMTheme.accent.opacity(0.18)), style: StrokeStyle(lineWidth: 1, dash: [5, 7]))
                }
                ForEach(Array(badges.enumerated()), id: \.element.id) { index, badge in
                    let point = positions[index % positions.count]
                    Button { select(badge) } label: {
                        VStack(spacing: 3) { ATMAIDayBadgeVisual(badge: badge, size: 82); Text(badge.name).font(.caption2.bold()).lineLimit(1) }
                    }.buttonStyle(.plain)
                        .position(x: point.x * proxy.size.width, y: point.y * proxy.size.height)
                }
            }.background(LinearGradient(colors: [ATMTheme.accent.opacity(0.04), .clear], startPoint: .top, endPoint: .bottom)).clipShape(RoundedRectangle(cornerRadius: 20))
        }
    }
}

private struct ATMAIDayBadgeDetail: View {
    let badge: ATMAIDayBadge
    @Environment(\.dismiss) private var dismiss
    var body: some View { VStack(spacing: 18) { HStack { Spacer(); Button { dismiss() } label: { Image(systemName:"xmark") }.buttonStyle(.plain) }; ATMAIDayBadgeVisual(badge: badge, size: 160); Text(badge.name).font(.title.bold()); Text(badge.description).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center); Text("等级 L\(badge.level) · 累计 \(badge.qualifiedDays) 天"); ProgressView(value: badge.progress).frame(width: 260); if let cooldown=badge.cooldownUntil,!cooldown.isEmpty{Text("即时徽章冷却至 \(cooldown)").font(.caption).foregroundStyle(ATMTheme.secondary)}; if !badge.qualifiedDates.isEmpty { Text("最近解锁记录").font(.headline); Text(badge.qualifiedDates.prefix(6).joined(separator: "  ·  ")).font(.caption).foregroundStyle(ATMTheme.secondary).multilineTextAlignment(.center) }; Button("完成") { dismiss() } }.padding(28).frame(width: 440, height: 540) }
}

private struct ATMAIDayEmptyCard: View { let day:String; var body: some View { VStack(spacing:12){Image(systemName:"moon.stars.fill").font(.system(size:48)).foregroundStyle(ATMTheme.secondary);Text("\(day)没有可用的 AI 活动").font(.title3.bold());Text("AI Day 不会为缺失数据编造概念或徽章。").foregroundStyle(ATMTheme.secondary)}.padding(40) } }

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
        ZStack {
            LinearGradient(colors: [Color(red: 0.05, green: 0.07, blue: 0.16), Color(red: 0.16, green: 0.08, blue: 0.28)], startPoint: .topLeading, endPoint: .bottomTrailing)
            VStack(spacing: 46) {
                HStack {
                    Text("AI DAY").font(.system(size: 34, weight: .bold, design: .rounded)).tracking(8)
                    Spacer()
                    if includeDate { Text(result.day).font(.system(size: 24, design: .monospaced)) }
                }.foregroundStyle(.white.opacity(0.75))
                Spacer()
                if let badge = result.badge { ATMAIDayBadgeVisual(badge: badge, size: 390) }
                Text(result.concept?.title ?? "AI Day").font(.system(size: 66, weight: .bold)).foregroundStyle(.white)
                Text(result.concept?.explanation ?? "").font(.system(size: 30)).foregroundStyle(.white.opacity(0.76)).multilineTextAlignment(.center).frame(maxWidth: 820)
                if includeEvidence {
                    HStack(spacing: 28) {
                        ForEach(Array((result.concept?.evidence ?? []).prefix(3))) { evidence in
                            Text("\(evidence.metric)  \(Int(evidence.value))").font(.system(size: 24, weight: .semibold, design: .rounded)).padding(.horizontal, 22).padding(.vertical, 14).background(.white.opacity(0.1)).clipShape(Capsule())
                        }
                    }.foregroundStyle(.white)
                }
                if includeStats { Text("\(result.features.sessionCount) sessions · \(result.features.turnCount) turns · \(result.features.toolCalls) tools").font(.system(size: 23, design: .monospaced)).foregroundStyle(.white.opacity(0.55)) }
                Spacer()
                Text("Generated locally by ATM · no raw content included").font(.system(size: 19)).foregroundStyle(.white.opacity(0.4))
            }.padding(68)
        }.aspectRatio(4/5, contentMode: .fit)
    }
}
