import Foundation
import UserNotifications

/// Whether an agent blocking on you raises a system notification.
///
/// Only a master switch: there is deliberately no per-reason granularity, and no
/// quiet-hours setting of ATM's own. Notification Center already runs 专注模式
/// and Do Not Disturb better than this app could, and every knob added here is
/// one more place for the two to disagree.
enum ATMAgentAttentionNotifyPreferences {
    static let enabledKey = "ATMAgentAttentionNotifyEnabled"
    static let defaultEnabled = true

    static func isEnabled(defaults: UserDefaults = .standard) -> Bool {
        guard defaults.object(forKey: enabledKey) != nil else { return defaultEnabled }
        return defaults.bool(forKey: enabledKey)
    }
}

/// Where a click on a delivered notification should land.
///
/// Before this the delegate ignored `userInfo` outright and opened the desktop
/// window for everything. That is still right for a todo, but an agent waiting
/// on 授权 needs its own terminal, not ATM.
enum ATMNotificationRoute: Equatable {
    case todo(String)
    case agentSession(String)
    case guardApproval(String)
    case collection
    case app

    /// Rebuilds the route from a delivered notification's `userInfo`.
    static func from(userInfo: [AnyHashable: Any]) -> ATMNotificationRoute {
        // Checked first: an approval banner is the only one that must not open the
        // agent's terminal, and it is the only one whose decision surface is ATM.
        if let approvalID = userInfo["approval_id"] as? String, !approvalID.isEmpty {
            return .guardApproval(approvalID)
        }
        if let sessionID = userInfo["session_id"] as? String, !sessionID.isEmpty {
            return .agentSession(sessionID)
        }
        if let todoID = userInfo["todo_id"] as? String, !todoID.isEmpty {
            return .todo(todoID)
        }
        if userInfo["event"] as? String == "collection" {
            return .collection
        }
        return .app
    }
}

/// A "this agent is blocked on you" banner.
///
/// Built only from `attentionSignal`, so the subtitle can name the actual reason
/// the agent gave (等待授权 / 等待输入 / …) rather than a guess.
struct ATMAgentAttentionNotificationPayload: Equatable {
    let title: String
    let subtitle: String
    let body: String

    static func make(session: ATMLiveSession) -> ATMAgentAttentionNotificationPayload? {
        guard let signal = session.attentionSignal else { return nil }
        let project = session.project.trimmingCharacters(in: .whitespacesAndNewlines)
        return ATMAgentAttentionNotificationPayload(
            title: project.isEmpty ? "ATM" : "ATM · \(project)",
            subtitle: "\(ATMAgentDisplay.name(session.tool)) \(signal.displayReason)",
            body: session.presenceTitle
        )
    }
}

/// Human-facing todo lifecycle events. Start/edit noise is out of scope; these
/// are the moments a person at the desk should notice.
enum ATMTodoNotifyEvent: String, Equatable {
    case created
    case review
    case completed
    case dropped

    var categoryIdentifier: String {
        switch self {
        case .created: return "ATM_TODO_CREATED"
        case .review: return "ATM_TODO_REVIEW"
        case .completed: return "ATM_TODO_COMPLETED"
        case .dropped: return "ATM_TODO_DROPPED"
        }
    }
}

struct ATMNotificationPayload: Equatable {
    let title: String
    let subtitle: String
    let body: String
    let event: ATMTodoNotifyEvent

    static func todoCreated(_ todo: ATMTodo) -> ATMNotificationPayload {
        base(todo, event: .created, subtitle: "\(todo.id) 新建", body: todo.title)
    }

    /// Agent submitted work; the human gate is open.
    static func todoNeedsReview(_ todo: ATMTodo) -> ATMNotificationPayload {
        base(todo, event: .review, subtitle: "\(todo.id) 待验收", body: todo.title)
    }

    static func todoCompleted(_ todo: ATMTodo, now: Date = Date()) -> ATMNotificationPayload {
        var notificationBody = todo.title
        if let startTS = todo.startTS {
            let elapsed = max(Int(now.timeIntervalSince1970) - Int(startTS), 0)
            notificationBody += "（\(durationText(seconds: elapsed))）"
        }
        // Closing a review-status todo is 验收; otherwise 完成.
        return base(
            todo,
            event: .completed,
            subtitle: "\(todo.id) 已\(todo.completionVerb)",
            body: notificationBody
        )
    }

    static func todoDropped(_ todo: ATMTodo) -> ATMNotificationPayload {
        base(todo, event: .dropped, subtitle: "\(todo.id) 已放弃", body: todo.title)
    }

    private static func base(
        _ todo: ATMTodo,
        event: ATMTodoNotifyEvent,
        subtitle: String,
        body: String
    ) -> ATMNotificationPayload {
        let notificationTitle = todo.project.flatMap { $0.isEmpty ? nil : "ATM · \($0)" } ?? "ATM"
        return ATMNotificationPayload(
            title: notificationTitle,
            subtitle: subtitle,
            body: body,
            event: event
        )
    }

    private static func durationText(seconds: Int) -> String {
        if seconds < 60 { return "\(seconds) 秒" }
        if seconds < 3_600 { return "\(seconds / 60) 分钟" }
        let hours = seconds / 3_600
        let minutes = (seconds % 3_600) / 60
        return minutes == 0 ? "\(hours) 小时" : "\(hours) 小时 \(minutes) 分钟"
    }
}

struct ATMCollectionNotificationPayload: Equatable {
    let subtitle: String
    let body: String

    /// `sources` is what decides whose results may interrupt: a muted source is
    /// left out of the counts entirely, so a run that only touched muted sources
    /// produces no banner at all. Everything else about a muted source is
    /// unchanged — it still collects, and its results still count as unread in
    /// the sidebar and menubar, which is where they are meant to be noticed.
    ///
    /// A run whose source cannot be found still notifies. The source may have
    /// been deleted after the run, or predate source-scoped runs; either way
    /// "unknown" is not the same claim as "muted", and swallowing the banner
    /// would lose a real result to a bookkeeping gap.
    static func make(
        runs: [ATMCollectionRun],
        sources: [ATMCollectionSource] = []
    ) -> ATMCollectionNotificationPayload? {
        let mutedIDs = Set(sources.filter { !$0.notifiesDesktop }.map(\.id))
        let audible = runs.filter { run in
            guard let sourceID = run.sourceID, !sourceID.isEmpty else { return true }
            return !mutedIDs.contains(sourceID)
        }
        let created = audible.reduce(0) { $0 + $1.createdCount }
        let appended = audible.reduce(0) { $0 + $1.appendedCount }
        let insight = audible.reduce(0) { $0 + $1.insightCount }
        let failed = audible.reduce(0) { $0 + $1.failedCount }
        guard created + appended + insight + failed > 0 else { return nil }
        let subtitle = failed > 0 ? "收集有结果需要处理" : "有新的收集待查看"
        let body = "新增 \(created) · 补充 \(appended) · 结论 \(insight) · 失败 \(failed)"
        return ATMCollectionNotificationPayload(subtitle: subtitle, body: body)
    }
}

/// Diff two todo snapshots for external human-relevant changes (agent CLI, other
/// terminals). First observation is a baseline only — no flood of historical work.
///
/// Done/dropped stay out of the refresh path: local UI complete already notifies,
/// and agent `todo done` fires the CLI banner. Create + review are the gaps the
/// menubar must close when an agent mutates work while the app is open.
enum ATMTodoNotificationDiff {
    static func events(
        previous: [String: String]?,
        current: [ATMTodo]
    ) -> [(ATMTodo, ATMTodoNotifyEvent)] {
        guard let previous else { return [] }
        var result: [(ATMTodo, ATMTodoNotifyEvent)] = []
        for todo in current {
            if previous[todo.id] == nil {
                result.append((todo, .created))
                if todo.status == "review" {
                    result.append((todo, .review))
                }
                continue
            }
            if previous[todo.id] != "review", todo.status == "review" {
                result.append((todo, .review))
            }
        }
        return result
    }

    static func statusMap(from todos: [ATMTodo]) -> [String: String] {
        Dictionary(uniqueKeysWithValues: todos.map { ($0.id, $0.status) })
    }

    static func payload(for todo: ATMTodo, event: ATMTodoNotifyEvent) -> ATMNotificationPayload {
        switch event {
        case .created: return .todoCreated(todo)
        case .review: return .todoNeedsReview(todo)
        case .completed: return .todoCompleted(todo)
        case .dropped: return .todoDropped(todo)
        }
    }
}

final class ATMNotificationManager: NSObject, UNUserNotificationCenterDelegate {
    static let shared = ATMNotificationManager()

    // UNUserNotificationCenter 要求进程是正规 .app bundle；
    // swift run / 裸可执行文件没有 bundle，访问会抛 NSInternalInconsistencyException。
    // 此时降级为 nil，所有通知操作变为 no-op，方便本地开发调试。
    private let center: UNUserNotificationCenter? = ATMAppBundle.isBundled
        ? UNUserNotificationCenter.current()
        : nil
    private var onOpen: ((ATMNotificationRoute) -> Void)?
    private var onGuardDecision: ((String, Bool) -> Void)?

    private override init() {
        super.init()
    }

    func start(
        onOpen: @escaping (ATMNotificationRoute) -> Void,
        onGuardDecision: @escaping (String, Bool) -> Void = { _, _ in }
    ) {
        self.onOpen = onOpen
        self.onGuardDecision = onGuardDecision
        guard let center else {
            NSLog("ATMNotificationManager: 无 app bundle，通知功能已禁用（swift run 开发模式）")
            return
        }
        center.delegate = self
        // Until this existed the app set categoryIdentifier on every notification
        // without registering a single category, so those identifiers did nothing.
        // The approval banner is the first one that needs buttons, and buttons only
        // appear for a category the system knows about.
        center.setNotificationCategories([
            UNNotificationCategory(
                identifier: ATMGuardApprovalActions.category,
                actions: [
                    UNNotificationAction(
                        identifier: ATMGuardApprovalActions.approve,
                        title: "批准并发送",
                        // Foreground so the app is up to report a failure: approving
                        // runs a real command, and "approved but it errored" has to
                        // land somewhere the user will see.
                        options: [.foreground]
                    ),
                    UNNotificationAction(
                        identifier: ATMGuardApprovalActions.deny,
                        title: "拒绝",
                        options: []
                    ),
                ],
                intentIdentifiers: [],
                options: []
            )
        ])
        center.getNotificationSettings { [weak self] settings in
            guard settings.authorizationStatus == .notDetermined else { return }
            self?.center?.requestAuthorization(options: [.alert, .sound]) { _, _ in }
        }
    }

    func sendTodoCompleted(_ todo: ATMTodo) {
        send(ATMNotificationPayload.todoCompleted(todo), todoID: todo.id)
    }

    func send(_ payload: ATMNotificationPayload, todoID: String) {
        guard let center else { return }
        let content = UNMutableNotificationContent()
        content.title = payload.title
        content.subtitle = payload.subtitle
        content.body = payload.body
        content.sound = .default
        content.categoryIdentifier = payload.event.categoryIdentifier
        content.userInfo = ["todo_id": todoID, "event": payload.event.rawValue]

        center.add(
            UNNotificationRequest(
                identifier: "atm-todo-\(todoID)-\(payload.event.rawValue)-\(UUID().uuidString)",
                content: content,
                trigger: nil
            )
        )
    }

    /// Identifier for one session's attention banner.
    ///
    /// Stable rather than UUID-suffixed like the todo notifications: an agent can
    /// re-signal the same block, and the second delivery should replace the first
    /// instead of stacking. It also makes the banner withdrawable once the agent
    /// moves on.
    static func agentAttentionIdentifier(sessionID: String) -> String {
        "atm-agent-attention-\(sessionID)"
    }

    func sendAgentAttention(_ session: ATMLiveSession) {
        guard let center,
              ATMAgentAttentionNotifyPreferences.isEnabled(),
              let payload = ATMAgentAttentionNotificationPayload.make(session: session)
        else { return }
        let content = UNMutableNotificationContent()
        content.title = payload.title
        content.subtitle = payload.subtitle
        content.body = payload.body
        // Silent on purpose: 提示音 already chimes for `attentionRequired` off the
        // same event, and letting both fire means two sounds for one moment.
        content.sound = nil
        content.categoryIdentifier = "ATM_AGENT_ATTENTION"
        content.userInfo = ["session_id": session.id, "event": "agent_attention"]

        center.add(
            UNNotificationRequest(
                identifier: Self.agentAttentionIdentifier(sessionID: session.id),
                content: content,
                trigger: nil
            )
        )
    }

    /// Pulls a banner back once the agent is no longer waiting. A stale 等待授权
    /// sitting in Notification Center is worse than never having sent it.
    func withdrawAgentAttention(sessionID: String) {
        guard let center else { return }
        let identifiers = [Self.agentAttentionIdentifier(sessionID: sessionID)]
        center.removeDeliveredNotifications(withIdentifiers: identifiers)
        center.removePendingNotificationRequests(withIdentifiers: identifiers)
    }

    /// Stable per request, so a second look at the same request replaces its banner
    /// instead of stacking, and so it can be pulled back once decided.
    static func guardApprovalIdentifier(_ approvalID: String) -> String {
        "atm-guard-approval-\(approvalID)"
    }

    func sendGuardApproval(_ payload: ATMGuardApprovalPayload, approvalID: String) {
        guard let center else { return }
        let content = UNMutableNotificationContent()
        content.title = payload.title
        content.subtitle = payload.subtitle
        content.body = payload.body
        // Silent: the approval *window* is the surface that asks, and it plays the
        // sound. Letting both chime would be two sounds for one decision — the same
        // mistake the agent-attention banner avoids just above. This banner's job is
        // to be the record that survives dismissing the window.
        content.sound = nil
        content.categoryIdentifier = ATMGuardApprovalActions.category
        content.userInfo = ["approval_id": approvalID, "event": "guard_approval"]

        center.add(
            UNNotificationRequest(
                identifier: Self.guardApprovalIdentifier(approvalID),
                content: content,
                trigger: nil
            )
        )
    }

    /// Pulls a banner back once the request is no longer pending — decided here,
    /// decided in a terminal, or expired. A stale 待授权 with live buttons is worse
    /// than never having sent it, because pressing one would now fail confusingly.
    func withdrawGuardApproval(approvalID: String) {
        guard let center else { return }
        let identifiers = [Self.guardApprovalIdentifier(approvalID)]
        center.removeDeliveredNotifications(withIdentifiers: identifiers)
        center.removePendingNotificationRequests(withIdentifiers: identifiers)
    }

    func sendCollectionSummary(_ runs: [ATMCollectionRun], sources: [ATMCollectionSource] = []) {
        guard let center,
              let payload = ATMCollectionNotificationPayload.make(runs: runs, sources: sources)
        else { return }
        let content = UNMutableNotificationContent()
        content.title = "ATM · 收集"
        content.subtitle = payload.subtitle
        content.body = payload.body
        content.sound = .default
        content.categoryIdentifier = "ATM_COLLECTION"
        content.userInfo = ["event": "collection"]
        center.add(
            UNNotificationRequest(
                identifier: "atm-collection-\(UUID().uuidString)",
                content: content,
                trigger: nil
            )
        )
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo
        // A button press is a decision, not a request to open something. Until now
        // actionIdentifier was discarded outright, so a tapped button behaved
        // exactly like a tapped banner.
        if let approvalID = userInfo["approval_id"] as? String, !approvalID.isEmpty,
           response.actionIdentifier == ATMGuardApprovalActions.approve
               || response.actionIdentifier == ATMGuardApprovalActions.deny {
            let approve = response.actionIdentifier == ATMGuardApprovalActions.approve
            DispatchQueue.main.async { [weak self] in
                self?.onGuardDecision?(approvalID, approve)
                completionHandler()
            }
            return
        }
        let route = ATMNotificationRoute.from(userInfo: userInfo)
        DispatchQueue.main.async { [weak self] in
            self?.onOpen?(route)
            completionHandler()
        }
    }
}
