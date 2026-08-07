import SwiftUI

struct TodoSessionHistoryView: View {
    let todo: ATMTodo
    @ObservedObject var store: ATMDataStore

    @State private var openedSession: ATMBoundSession?
    @State private var launchError: String?

    private var sessions: [ATMBoundSession] {
        store.boundSessions(for: todo.id).sorted { $0.firstBoundAt > $1.firstBoundAt }
    }

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 4) {
            if store.isLoadingBoundSessions(for: todo.id), sessions.isEmpty {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("正在加载 Session…")
                }
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                // Lines the status text up with the rows it will be replaced by,
                // which carry the row surface's own horizontal padding.
                .padding(.horizontal, 10)
            } else if sessions.isEmpty {
                Text("暂无显式绑定的 Agent Session。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .padding(.horizontal, 10)
            } else {
                ForEach(sessions) { session in
                    sessionRow(session)
                    if session.id != sessions.last?.id {
                        Divider()
                    }
                }
            }
        }
        .sheet(item: $openedSession) { session in
            ATMSessionTranscriptSheet(
                agent: session.agent,
                title: session.summary,
                shortID: session.shortID,
                project: session.project,
                store: store
            )
        }
        .alert(
            "无法回到这个会话",
            isPresented: Binding(get: { launchError != nil }, set: { if !$0 { launchError = nil } })
        ) {
            Button("好") { launchError = nil }
        } message: {
            Text(launchError ?? "")
        }
    }

    /// Reading a transcript is an action the row offers, not what the row *is*.
    /// Clicking anywhere used to open it, which made every attempt to select the
    /// session id or reach the launch button a coin flip — and it left the hover
    /// surface wrapped around the summary alone, stopping short of the button
    /// sitting in the same row.
    private func sessionRow(_ session: ATMBoundSession) -> some View {
        HStack(alignment: .top, spacing: 8) {
            sessionSummary(session)
                .frame(maxWidth: .infinity, alignment: .leading)
            sessionActions(session)
                .padding(.top, 1)
        }
        .atmRowSurface(isSelected: false)
    }

    private func sessionActions(_ session: ATMBoundSession) -> some View {
        let route = ATMAgentSessionLaunchRoute.resolve(
            for: session,
            live: store.snapshot.liveStatus.sessions
        )
        return HStack(spacing: 2) {
            ATMIconButton(
                systemImage: "text.bubble",
                // Nothing was ever indexed for the session, so there is no
                // transcript to open — the row already says so.
                help: session.indexed ? "查看完整对话" : "该会话未索引，没有可展示的对话",
                chrome: .bare,
                isEnabled: session.indexed
            ) {
                openedSession = session
            }
            if route.isAvailable {
                ATMIconButton(
                    systemImage: "arrow.up.forward.app",
                    help: "\(route.actionTitle)（\(route.destinationLabel)）",
                    chrome: .bare
                ) {
                    open(route)
                }
            }
        }
    }

    private func sessionSummary(_ session: ATMBoundSession) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                ATMAgentMark(agent: session.agent, size: 14)
                Text(topic(session))
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Text(bindingState(session))
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(session.isActive ? ATMTheme.accent : ATMTheme.secondary)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(
                        (session.isActive ? ATMTheme.accent : ATMTheme.secondary).opacity(0.10),
                        in: Capsule()
                    )
            }

            HStack(spacing: 8) {
                Text(session.shortID)
                    .font(ATMFont.mono(.footnote, .medium))
                    .textSelection(.enabled)
                    .help(session.sessionID)
                Text(bindingTime(session))
                if session.bindingCount > 1 {
                    Text("绑定 \(session.bindingCount) 次")
                }
                if session.indexed {
                    Text(sessionEffort(session))
                } else {
                    Label("详情尚未索引", systemImage: "exclamationmark.circle")
                }
            }
            .font(ATMFont.footnote)
            .foregroundStyle(ATMTheme.secondary)
            .lineLimit(1)
            .minimumScaleFactor(0.82)

            if let cwd = nonEmpty(session.cwd) {
                Text(cwd)
                    .font(ATMFont.mono(.caption))
                    .foregroundStyle(ATMTheme.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
    }

    /// The topic comes from the CLI, which prefers the agent's own session title
    /// and falls back to the opening prompt. Only a session that was never
    /// indexed lands here with nothing.
    private func topic(_ session: ATMBoundSession) -> String {
        nonEmpty(session.summary) ?? "未命名会话"
    }

    /// What the session cost to run, which is not the same as what it spent on
    /// this todo — a session is usually bound partway through.
    private func sessionEffort(_ session: ATMBoundSession) -> String {
        var parts = ["\(session.queries) 次提问", "\(session.toolCalls) 次工具调用"]
        if session.activeSeconds > 0 {
            parts.append("历时 \(NumberFormat.duration(Double(session.activeSeconds)))")
        }
        if session.costUSD > 0 {
            parts.append(NumberFormat.currency(session.costUSD))
        }
        return "Session 总计：" + parts.joined(separator: " · ")
    }

    private func open(_ route: ATMAgentSessionLaunchRoute) {
        do {
            try ATMAgentSessionLauncher.open(route)
        } catch {
            launchError = error.localizedDescription
        }
    }

    private func bindingState(_ session: ATMBoundSession) -> String {
        guard !session.isActive else { return "已绑定" }
        switch session.reason {
        case "rebound": return "已重新绑定"
        case "submit:review": return "已提交"
        case "done", "close:done": return "已完成"
        case "waiting": return "等待中"
        case let reason? where !reason.isEmpty: return reason
        default: return "已解绑"
        }
    }

    private func bindingTime(_ session: ATMBoundSession) -> String {
        guard session.firstBoundAt > 0 else { return "绑定时间未知" }
        let first = Self.dateFormatter.string(from: Date(timeIntervalSince1970: TimeInterval(session.firstBoundAt)))
        if session.bindingCount > 1, session.boundAt > session.firstBoundAt {
            let latest = Self.dateFormatter.string(from: Date(timeIntervalSince1970: TimeInterval(session.boundAt)))
            return "首次 \(first) · 最近 \(latest)"
        }
        return "绑定于 \(first)"
    }

    private func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    private static let dateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = "MM-dd HH:mm"
        return formatter
    }()
}
