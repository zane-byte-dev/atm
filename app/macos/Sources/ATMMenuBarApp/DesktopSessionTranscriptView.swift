import SwiftUI

/// Shared controls for choosing and refreshing one session reading mode. Live
/// Agent details place this beside their page tabs; standalone indexed-session
/// details keep it in the transcript's own toolbar.
struct DesktopSessionReadControls: View {
    let sessionID: String
    @ObservedObject var store: ATMDataStore
    @Binding var mode: ATMSessionReadMode

    var body: some View {
        HStack(spacing: 8) {
            Text("阅读方式")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
            Picker("阅读方式", selection: $mode) {
                ForEach(ATMSessionReadMode.allCases) { readMode in
                    Text(readMode.title).tag(readMode)
                }
            }
            .labelsHidden()
            .pickerStyle(.menu)
            .controlSize(.small)
            .fixedSize(horizontal: true, vertical: false)
            .help(mode.help)
            Button {
                store.loadSessionRead(sessionID, mode: mode, reload: true)
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .controlSize(.small)
            .disabled(store.isLoadingSessionRead(sessionID, mode: mode))
            .help("重新读取这一段")
        }
        .fixedSize(horizontal: true, vertical: false)
    }
}

/// 一个会话的三段式阅读：摘要看结果、时序看它怎么花掉 token、完整看整条链路（含思考）。
///
/// 三段各自是一次独立读取，而不是一份大 payload 前端筛：完整视图要回到 Agent 自己的
/// 转录文件里抽思考链，那是最贵的一步，只想看结论的人不该为它付钱。
struct DesktopSessionTranscriptView: View {
    let sessionID: String
    /// 会话所属 Agent 的展示名，只用于「这个 Agent 不记录思考」这类说明文案。
    let agentLabel: String
    @ObservedObject var store: ATMDataStore
    @Binding var mode: ATMSessionReadMode
    var showsReadControls = true

    var body: some View {
        VStack(spacing: 0) {
            if showsReadControls {
                HStack(spacing: 0) {
                    Spacer(minLength: 0)
                    DesktopSessionReadControls(
                        sessionID: sessionID,
                        store: store,
                        mode: $mode
                    )
                }
                .padding(.horizontal, ATMDetailLayout.horizontalPadding)
                .padding(.vertical, 10)

                Divider()
            }

            VStack(alignment: .leading, spacing: 12) {
                if let error = store.sessionReadError(sessionID, mode: mode) {
                    let presentation = ATMErrorPresentation.resolve(error, fallbackTitle: "会话读取失败")
                    ATMInlineNotice(
                        severity: .warning,
                        title: presentation.title,
                        message: presentation.message,
                        details: error,
                        actionTitle: "重试",
                        onAction: { store.loadSessionRead(sessionID, mode: mode, reload: true) }
                    )
                }
                switch mode {
                case .brief, .full:
                    turnsContent
                case .timeline:
                    timelineContent
                }
            }
            .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .padding(.horizontal, ATMDetailLayout.horizontalPadding)
            .padding(.vertical, 16)
            .atmAnimatedSwap(mode.rawValue, style: .detail)
        }
        // 切段和换会话都重新取一次；已缓存的组合不会真的落到 CLI。
        .task(id: "\(sessionID)|\(mode.rawValue)") {
            store.loadSessionRead(sessionID, mode: mode)
        }
    }

    // MARK: - 问答与思考

    private var turnsContent: some View {
        let transcript = store.sessionTranscript(sessionID, mode: mode)
        return Group {
            if let transcript {
                if transcript.turns.isEmpty {
                    placeholder("这个会话还没有可读的问答")
                } else {
                    VStack(alignment: .leading, spacing: 0) {
                        transcriptNotices(transcript)
                        ForEach(Array(transcript.turns.enumerated()), id: \.element.id) { index, turn in
                            turnBlock(turn)
                            if index < transcript.turns.count - 1 {
                                Divider()
                            }
                        }
                        if transcript.truncated, mode == .brief {
                            // 说清楚这是尾部而不是全部，否则「摘要」会被读成「会话只有这么多」。
                            Text("只显示最近 \(transcript.returnedTurns) / \(transcript.totalTurns) 轮，完整内容切到「完整」")
                                .font(ATMFont.footnote)
                                .foregroundStyle(ATMTheme.secondary)
                        }
                    }
                }
            } else if store.isLoadingSessionRead(sessionID, mode: mode) {
                placeholder("读取中…")
            } else if store.sessionReadError(sessionID, mode: mode) == nil {
                placeholder("暂无内容")
            }
        }
    }

    /// 思考缺失有两种完全不同的原因，不能都渲染成空白面板：文件被 Agent 轮转掉了，
    /// 和这个 Agent 压根不保存思考正文（Claude Code 只留签名）。
    private func transcriptNotices(_ transcript: ATMSessionTranscript) -> some View {
        Group {
            if mode == .full, transcript.thinkingSourceMissing {
                ATMInlineNotice(
                    severity: .info,
                    title: "思考过程已不可读",
                    message: "Agent 的原始转录文件已不在磁盘上，下面的问答来自 ATM 索引。"
                )
            } else if mode == .full, transcript.thinkingAbsent {
                ATMInlineNotice(
                    severity: .info,
                    title: "这个 Agent 不保存思考正文",
                    message: "\(agentLabel) 的转录里只有思考签名，没有可读文本；问答仍然完整。"
                )
            }
        }
    }

    /// Conversation is one reading stream, not a dashboard of independent
    /// objects. Dividers separate turns without lifting every message into its
    /// own card.
    private func turnBlock(_ turn: ATMSessionTurn) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Text("第 \(turn.turn) 轮")
                    .font(ATMFont.font(.footnote, weight: .semibold))
                    .foregroundStyle(ATMTheme.secondary)
                Spacer(minLength: 0)
            }
            if let question = turn.question, !question.isEmpty {
                speaker("你", tint: ATMTheme.accent)
                ATMMarkdownContentView(source: question)
            }
            if let thinking = turn.thinking, !thinking.isEmpty {
                DisclosureGroup {
                    ATMMarkdownContentView(source: thinking)
                        .padding(.top, 8)
                } label: {
                    Label("思考过程", systemImage: "brain")
                        .font(ATMFont.font(.footnote, weight: .medium))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            if let answer = turn.answer, !answer.isEmpty {
                speaker(agentLabel, tint: ATMTheme.success)
                ATMMarkdownContentView(source: answer)
            }
        }
        .padding(.vertical, 15)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func speaker(_ name: String, tint: Color) -> some View {
        Text(name)
            .font(ATMFont.font(.caption, weight: .semibold))
            .foregroundStyle(tint)
    }

    // MARK: - 时序

    private var timelineContent: some View {
        let entries = store.sessionTimeline(sessionID)
        return Group {
            if let entries {
                if entries.isEmpty {
                    placeholder("这个会话没有可读的时间线")
                } else {
                    VStack(alignment: .leading, spacing: 8) {
                        timelineTotals(entries)
                        ForEach(Array(entries.enumerated()), id: \.offset) { _, entry in
                            timelineRow(entry)
                        }
                    }
                }
            } else if store.isLoadingSessionRead(sessionID, mode: .timeline) {
                placeholder("读取中…")
            } else if store.sessionReadError(sessionID, mode: .timeline) == nil {
                placeholder("暂无内容")
            }
        }
    }

    /// 时序视图的价值就在于把消息和模型请求放在一起，所以先给一行总账：
    /// 几轮消息、几次请求、花了多少。
    private func timelineTotals(_ entries: [ATMSessionTimelineEntry]) -> some View {
        let messages = entries.filter(\.isMessage).count
        let requests = entries.count - messages
        let cost = entries.compactMap(\.costUSD).reduce(0, +)
        let output = entries.compactMap(\.outputTokens).reduce(0, +)
        return HStack(spacing: 14) {
            Label("\(messages) 条消息", systemImage: "text.bubble")
            Label("\(requests) 次请求", systemImage: "arrow.up.arrow.down")
            if output > 0 {
                Label("输出 \(NumberFormat.compact(Int(output)))", systemImage: "square.and.arrow.up")
            }
            if cost > 0 {
                Label(NumberFormat.currency(cost), systemImage: "creditcard")
            }
            Spacer(minLength: 0)
        }
        .font(ATMFont.footnote)
        .foregroundStyle(ATMTheme.secondary)
        .padding(.bottom, 4)
    }

    private func timelineRow(_ entry: ATMSessionTimelineEntry) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Text(ATMSessionTimeFormat.clock(entry.date))
                .font(ATMFont.mono(.caption, .medium))
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 58, alignment: .leading)
            if entry.isMessage {
                VStack(alignment: .leading, spacing: 4) {
                    Text(entry.role == "user" ? "你" : agentLabel)
                        .font(ATMFont.font(.caption, weight: .semibold))
                        .foregroundStyle(entry.role == "user" ? ATMTheme.accent : ATMTheme.success)
                    Text(ATMMarkdown.plainSummary(entry.content ?? "", limit: 400))
                        .font(ATMFont.footnote)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
            } else {
                VStack(alignment: .leading, spacing: 4) {
                    Text(entry.model ?? "模型请求")
                        .font(ATMFont.mono(.caption, .medium))
                        .foregroundStyle(ATMTheme.secondary)
                    Text(requestDetail(entry))
                        .font(ATMFont.footnote)
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 6)
        .padding(.horizontal, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(entry.isMessage ? Color.clear : ATMTheme.surface.opacity(0.6))
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private func requestDetail(_ entry: ATMSessionTimelineEntry) -> String {
        var parts: [String] = []
        if let input = entry.inputTokens, input > 0 { parts.append("入 \(NumberFormat.compact(Int(input)))") }
        if let output = entry.outputTokens, output > 0 { parts.append("出 \(NumberFormat.compact(Int(output)))") }
        if let cache = entry.cacheTokens, cache > 0 { parts.append("缓存 \(NumberFormat.compact(Int(cache)))") }
        if let cost = entry.costUSD, cost > 0 { parts.append(NumberFormat.currency(cost)) }
        return parts.isEmpty ? "无用量记录" : parts.joined(separator: " · ")
    }

    private func placeholder(_ text: String) -> some View {
        Text(text)
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)
            .frame(maxWidth: .infinity, minHeight: 120)
    }
}

/// 时间列只用来对齐前后顺序，所以固定到分钟；日期由所在会话的头部负责。
enum ATMSessionTimeFormat {
    static func clock(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "MM-dd HH:mm"
        return formatter.string(from: date)
    }
}
