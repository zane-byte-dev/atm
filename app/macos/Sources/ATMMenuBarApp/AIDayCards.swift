import SwiftUI

/// AI Day 的卡片语言：一份调色板、一份指标文案、四个共享块。
///
/// 这一页有两类卡，别把它们当成一类：
///
/// - **日卡**（`ATMAIDayResult`）——今日卡、历史 item、历史详情、分享卡。讲的是某一天：
///   日期 + 概念 + 当天的证据 + 当天的特征统计。
/// - **徽章卡**（`ATMAIDayBadge`）——Atlas 列表卡、星图节点、徽章详情、纠正列表行。讲的是
///   一枚徽章的生涯：等级、进度、距下一级、累计天数，跟具体哪一天无关。
///
/// 两类只共用 `ATMAIDayBadgeVisual` 和 `ATMAIDayFieldBackground`。合并它们只会得到一个
/// 什么都能画、什么都画不准的壳。这里收的是每一类内部真正重复的部分。

/// 只在**固定深色**上成立的那几个颜色。
///
/// 这里剩下的都是画在徽章底座或分享卡上的——那两处的底永远是深的，跟系统外观无关。页面
/// 其余部分（进度条、星图连线、图例、状态 chip、文字）一律走 `ATMTheme`，跟其他工作区同一套。
///
/// 分界线就是「底是不是固定深色」。此前不分：`cold` 这个几乎发白的浅蓝同时当徽章辉光、进度条
/// 底色和图例点，页面一旦跟随外观、浅色模式下白卡上的进度条和图例点就等于没有。
enum ATMAIDayPalette {
    /// 冷计算光。只用在徽章底座内部和分享卡上——白底上它几乎不可见，别拿它当页面强调色，
    /// 那是 `ATMTheme.accent` 的活。
    static let cold = Color(red: 0.60, green: 0.86, blue: 1.0)
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

/// The single place a metric name becomes human-readable.
///
/// It has been duplicated twice. First when the day-detail sheet was added: the
/// copy on the today card fell behind and shipped a raw `generation_seconds` to
/// the user. Then again by the share card, which kept its own English map and
/// its own value formatter — so a metric added to the engine needed three edits
/// in three places to avoid leaking. `testEveryEngineMetricHasALabel` now pins
/// both the full and the compact map against `knownMetrics`.
enum ATMAIDayLabels {
    /// Mirrors the metrics `internal/aiday/reward.go` can emit. A metric with no
    /// entry here falls through to its raw name, which is what leaked before.
    static let knownMetrics = [
        "source_count", "session_count", "turn_count", "tool_calls", "total_tokens",
        "work_tokens", "generation_seconds", "code_events", "visual_events",
        "quality_loops", "refinements", "detail_turns", "modality_count",
        "corrections", "acceptances", "consecutive_days", "modality_share",
        "loop_share", "detail_share", "correction_share", "acceptance_share",
    ]

    private static let full = [
        "source_count": "AI 来源", "session_count": "会话", "turn_count": "对话轮次",
        "tool_calls": "工具调用", "total_tokens": "Token", "work_tokens": "有效 Token",
        "generation_seconds": "生成秒数", "code_events": "代码事件", "visual_events": "视觉事件",
        "quality_loops": "质检循环", "refinements": "细化", "detail_turns": "细节追问",
        "modality_count": "任务模态", "corrections": "纠正", "acceptances": "直接确认",
        "consecutive_days": "连续使用", "modality_share": "模态占比", "loop_share": "质检占比",
        "detail_share": "追问占比", "correction_share": "纠正占比", "acceptance_share": "确认占比",
    ]

    /// 分享卡上是一个大数字配一行小标签，长度比可读性更要紧，所以用短英文词。
    private static let short = [
        "source_count": "sources", "session_count": "sessions", "turn_count": "turns",
        "tool_calls": "tools", "total_tokens": "tokens", "work_tokens": "tokens",
        "generation_seconds": "seconds", "code_events": "code", "visual_events": "visual",
        "quality_loops": "quality", "refinements": "refine", "detail_turns": "detail",
        "modality_count": "modalities", "corrections": "corrections", "acceptances": "accepted",
        "consecutive_days": "streak", "modality_share": "share", "loop_share": "share",
        "detail_share": "share", "correction_share": "share", "acceptance_share": "share",
    ]

    static func evidence(_ metric: String) -> String { full[metric] ?? metric }

    static func compact(_ metric: String) -> String { short[metric] ?? metric }

    /// 两张表都收了这个指标吗。
    ///
    /// 覆盖率不能靠「翻译结果是否等于原名」来判断：短标签本来就可能跟指标同名（`corrections`
    /// 就是），那样的话缺标签和标签正确长得一模一样。所以由这里直接看键在不在表里，
    /// `testEveryEngineMetricHasALabel` 用它对 `knownMetrics` 逐项核对。
    static func hasLabels(for metric: String) -> Bool {
        full[metric] != nil && short[metric] != nil
    }

    /// 带单位的完整值，用在证据列表里。
    static func value(_ evidence: ATMAIDayEvidence) -> String {
        if evidence.metric.hasSuffix("_tokens") { return ATMAIDayFormat.tokens(Int64(evidence.value)) }
        let number = evidence.value.rounded() == evidence.value
            ? String(Int(evidence.value))
            : String(format: "%.1f", evidence.value)
        guard let unit = evidence.unit, !unit.isEmpty else { return number }
        return unit == "%" ? "\(number)%" : "\(number) \(unit)"
    }

    /// 分享卡的值：单位已经由下面那行标签说了，数字本身不再重复带单位；百分号留着，
    /// 因为去掉它 25 就变成了另一个意思。
    static func compactValue(_ evidence: ATMAIDayEvidence) -> String {
        if evidence.metric.hasSuffix("_tokens") { return ATMAIDayFormat.tokens(Int64(evidence.value)) }
        if evidence.unit == "%" { return "\(Int(evidence.value))%" }
        return String(Int(evidence.value))
    }

    /// 引擎发的是 `recent_p75` 这种内部写法，两处证据列表各自替换过一遍。
    static func comparison(_ raw: String) -> String {
        raw.replacingOccurrences(of: "recent_p", with: "近 30 日 P")
    }
}

/// 徽章脚下那一小块深色场。
///
/// 此前它是整页的底（今日卡、Atlas 卡、星图各铺一大块），于是深色在页面里随机出现：同一枚
/// 徽章在今日卡和星图里躺在深底上，在历史 item 和两个详情 sheet 里躺在浅底上。现在深色只
/// 属于徽章本身，页面其余部分交给 `atmWorkspaceCard()`，跟其他工作区一致。
///
/// 尺寸相关的量都按 `size` 算。做整页底的时候这里的 `endRadius` 是写死的 430、描边写死
/// 0.75pt——按 864pt 宽的卡片调的，套到 44pt 的纠正列表行上，渐变会平成一块死色、描边粗到
/// 像个边框。
///
/// 形状是圆的，不是圆角方形。方的那一版加上居中的图形，一整页 12 枚排下来读起来像
/// Launchpad——连线反倒成了多余的装饰，而这一页要的是星图。圆形节点本来就是星图的语汇，
/// 也更贴「徽章」；代价是可用面积只有直径的 0.707（内接正方形），图形得相应收小，
/// 见 `ATMAIDayBadgeVisual.glyphDiameter`。
struct ATMAIDayBadgePlate: View {
    let size: CGFloat

    var body: some View {
        Circle()
            .fill(Color(red: 0.018, green: 0.026, blue: 0.038))
            .overlay {
                Circle()
                    .fill(
                        RadialGradient(
                            colors: [Color(red: 0.31, green: 0.67, blue: 0.90).opacity(0.13), .clear],
                            center: UnitPoint(x: 0.25, y: 0.12),
                            startRadius: 0,
                            endRadius: size * 0.95
                        )
                    )
            }
            .overlay {
                Circle()
                    .strokeBorder(
                        LinearGradient(
                            colors: [.white.opacity(0.16), .white.opacity(0.025)],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        ),
                        lineWidth: max(0.5, size * 0.0035)
                    )
            }
    }
}

/// 「为什么是它」那几行证据。今日卡和历史详情各写过一遍，逐行一致——包括那句
/// `recent_p` 的替换。
struct ATMAIDayEvidenceList: View {
    let evidence: [ATMAIDayEvidence]
    /// 跟随调用处外层 VStack 的行距，保持两处原有的密度。
    var spacing: CGFloat = 12

    var body: some View {
        VStack(alignment: .leading, spacing: spacing) {
            ForEach(evidence) { item in
                HStack {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(ATMTheme.accent)
                    Text(ATMAIDayLabels.evidence(item.metric))
                    Spacer()
                    Text(ATMAIDayLabels.value(item)).monospacedDigit().fontWeight(.semibold)
                    if let comparison = item.comparison, !comparison.isEmpty {
                        Text(ATMAIDayLabels.comparison(comparison))
                            .font(.caption).foregroundStyle(ATMTheme.secondary)
                    }
                }
            }
        }
    }
}

/// 会话 / 轮次 / 工具 / Token 这一行。今日卡和历史详情各写一遍，连那个一模一样的
/// `metric` / `stat` 私有 helper 都各留了一份。
struct ATMAIDayFeatureStats: View {
    let features: ATMAIDayFeatures

    var body: some View {
        HStack(spacing: 22) {
            stat("会话", "\(features.sessionCount)")
            stat("轮次", "\(features.turnCount)")
            stat("工具", "\(features.toolCalls)")
            // 缓存读取跟着 context 大小走，不代表做了多少事，而且比其余项大一到两个数量级。
            // 所以「有效 Token」是正文，「含缓存」只做脚注。
            stat("有效 Token", ATMAIDayFormat.tokens(features.workTokens))
            stat("含缓存", ATMAIDayFormat.tokens(features.totalTokens))
        }
    }

    private func stat(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading) {
            Text(value).font(.headline).monospacedDigit()
            Text(title).font(.caption).foregroundStyle(ATMTheme.secondary)
        }
    }
}

/// 等级进度条 + 距下一级。三处各画一遍，而且画得不一样：今日卡用的是自己那句
/// 「距 L(n+1) 还差 N 天」，在 `nextLevelDays <= qualifiedDays` 时整行消失——满级那天
/// Atlas 说「已满级」、今日卡什么都不说；即时徽章的冷却期也只有 Atlas 认。现在三处都走
/// `ATMAIDayAtlasGuide.nextStep`，那是唯一一份「下一步是什么」的判断。
///
/// 分享卡不用这个：它要过 `ImageRenderer`，`ProgressView` 在离屏渲染里不可靠，尺寸又必须
/// 按 1080 宽换算，所以那边保留手画的 Capsule。
struct ATMAIDayProgressLine: View {
    enum Style {
        /// 今日卡：左对齐，压在深色场上。
        case todayCard
        /// Atlas 列表卡：居中，卡片满宽。
        case atlasTile
        /// 徽章详情 sheet：居中，跟随系统外观。
        case badgeSheet
    }

    let badge: ATMAIDayBadge
    let style: Style

    var body: some View {
        // 进度条和「距下一级」是一个单元，贴在一起。此前它们的间距是各调用处外层 VStack 的
        // 行距，于是同一对东西在三处相隔 12 / 12 / 16pt，读起来像两条无关的信息。
        let stack = VStack(alignment: style.alignment, spacing: 6) {
            ProgressView(value: badge.progress)
                .tint(ATMTheme.accent)
                .frame(maxWidth: style.barWidth)
            Text(ATMAIDayAtlasGuide.nextStep(badge))
                .font(style.font)
                .foregroundStyle(ATMTheme.secondary)
        }
        // 居中的两处要占满可用宽度才真的居中。今日卡不能：它是左对齐文字列里的一行，一旦
        // 抢满宽度，那一列就永远顶到 460pt——概念标题短的那天卡片会无端变宽。
        if style == .todayCard {
            stack
        } else {
            stack.frame(maxWidth: .infinity, alignment: style.frameAlignment)
        }
    }
}

private extension ATMAIDayProgressLine.Style {
    var alignment: HorizontalAlignment {
        self == .todayCard ? .leading : .center
    }

    var frameAlignment: Alignment {
        self == .todayCard ? .leading : .center
    }

    var barWidth: CGFloat? {
        switch self {
        case .todayCard: return 320
        case .atlasTile: return nil
        case .badgeSheet: return 260
        }
    }

    /// 三处只差字号：今日卡跟旁边那行等宽元数据对齐所以用 mono，Atlas 卡挤在小格子里用
    /// 最小一档。颜色和进度条色不再分档——它们本来也不该分。
    var font: Font {
        switch self {
        case .todayCard: return ATMFont.mono(.caption)
        case .atlasTile: return ATMFont.micro
        case .badgeSheet: return ATMFont.caption
        }
    }
}

/// 一天的结论：徽章 + 概念标题 + 解释 + 「由你修正」+ 一行元数据。今日卡和历史详情 sheet
/// 是同一块内容的两个密度，此前各写一遍——字号、间距、元数据文案（「证据强度」对「证据」、
/// `FORM / 01` 对 `L1`）三处都不一致，改一处不会带上另一处。
///
/// 度量收在 `Style` 里，内容只有这一份。状态条和反馈栏留在今日页自己身上：那是今天才有的
/// 东西，历史那天已经定了。
struct ATMAIDayResultCard: View {
    enum Style {
        /// 今日页：深色场上的白字，带日期眉标和等级进度。
        case today
        /// 历史详情 sheet：跟随系统外观，日期在 sheet 头上，不重复。
        case sheet
    }

    let result: ATMAIDayResult
    let badge: ATMAIDayBadge
    let concept: ATMAIDayConcept
    let style: Style

    var body: some View {
        HStack(alignment: .top, spacing: style.columnSpacing) {
            ATMAIDayBadgeVisual(badge: badge, size: style.badgeSize)
            VStack(alignment: .leading, spacing: style.textSpacing) {
                if style == .today {
                    Text("AI DAY  /  \(result.day)")
                        .font(ATMFont.mono(.caption, .medium))
                        .tracking(1.8)
                        .foregroundStyle(ATMTheme.secondary)
                }
                Text(concept.title)
                    .font(ATMFont.font(style.titleTier, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                Text(concept.explanation)
                    .font(ATMFont.bodyLarge)
                    .foregroundStyle(ATMTheme.secondary)
                if concept.isUserCorrected, let computed = concept.computedTitle, !computed.isEmpty {
                    correctionNote(computed)
                }
                metaRow
                if style == .today {
                    ATMAIDayProgressLine(badge: badge, style: .todayCard)
                }
            }
            .frame(maxWidth: style.textWidth, alignment: .leading)
        }
    }

    /// 元数据一行。「证据强度」是今天这枚徽章的信号强弱，「可信度」综合了基线长度、证据强度
    /// 和来源覆盖度——用户纠正不会提高可信度，所以两个数要分开看，help 里说清楚。
    private var metaRow: some View {
        HStack(spacing: style == .today ? 8 : 14) {
            Text(style == .today
                 ? (badge.level > 0 ? "FORM / 0\(badge.level)" : "FORM / SEED")
                 : "L\(badge.level)")
            Text("\(style == .today ? "证据强度" : "证据") \(Int(concept.strength * 100))%")
            Text("可信度 \(Int(concept.confidence * 100))%")
            Text("基线 \(result.baselineDays) 天")
        }
        .font(ATMFont.mono(.caption, style.metaWeight))
        .foregroundStyle(ATMTheme.secondary)
        .help("证据强度是今天这枚徽章的信号强弱；可信度综合了基线长度、证据强度和来源覆盖度。用户纠正不会提高可信度。")
    }

    /// 「由你修正」用警告色。它不是错误，但也不是默认状态——这一天的结论是人给的，不是引擎
    /// 测出来的，读的人得能一眼看出来。App 的状态色只有成功 / 警告 / 危险三个，警告正是
    /// 「注意，这里不走默认」那一格；为它单开第七个颜色不值得。
    @ViewBuilder private func correctionNote(_ computed: String) -> some View {
        switch style {
        case .today:
            HStack(spacing: 6) {
                Image(systemName: "hand.raised.fill").font(ATMFont.micro)
                Text("由你修正 · 引擎原判断「\(computed)」")
            }
            .font(ATMFont.font(.caption, weight: .medium))
            .foregroundStyle(ATMTheme.warning)
            .padding(.horizontal, 9).padding(.vertical, 4)
            .background(ATMTheme.warningFill, in: Capsule())
        case .sheet:
            Text("由你修正 · 引擎原判断「\(computed)」")
                .font(ATMFont.caption)
                .foregroundStyle(ATMTheme.warning)
        }
    }
}

private extension ATMAIDayResultCard.Style {
    var badgeSize: CGFloat { self == .today ? 230 : 150 }
    var columnSpacing: CGFloat { self == .today ? 30 : 22 }
    var textSpacing: CGFloat { self == .today ? 12 : 8 }
    var textWidth: CGFloat? { self == .today ? 460 : nil }
    /// 概念标题是这一页最大的一块内容，但仍得落在 `ATMFont` 的阶梯上：此前是裸的 30 / 24pt，
    /// 阶梯里没有这两档。
    var titleTier: ATMFont.Tier { self == .today ? .metric : .title1 }
    var metaWeight: Font.Weight { self == .today ? .medium : .regular }
}
