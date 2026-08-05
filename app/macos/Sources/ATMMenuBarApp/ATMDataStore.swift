import AppKit
import Combine
import Darwin
import Foundation

enum ATMCommandError: LocalizedError {
    case executableNotFound
    case timedOut(arguments: [String], seconds: TimeInterval)
    case failed(arguments: [String], status: Int32, message: String)

    var errorDescription: String? {
        switch self {
        case .executableNotFound:
            return "找不到 atm。请安装到常见路径，或设置 ATM_EXECUTABLE。"
        case .timedOut(let arguments, let seconds):
            return "atm \(arguments.joined(separator: " ")) 超过 \(Int(seconds)) 秒未完成，已停止。请重试或运行 atm doctor。"
        case .failed(let arguments, let status, let message):
            let detail = ATMErrorText.compact(message)
            return "atm \(arguments.joined(separator: " ")) 失败（\(status)）\(detail.isEmpty ? "" : "：\(detail)")"
        }
    }
}

enum ATMCommandPolicy {
    static func timeout(for arguments: [String]) -> TimeInterval {
        if arguments.first == "sync" { return 120 }
        if arguments.starts(with: ["collect", "run"]) { return 300 }
        // One model call per source that has new insights, plus a knowledge write.
        if arguments.starts(with: ["collect", "digest"]) { return 300 }
        if arguments.starts(with: ["collect", "item", "reprocess"]) { return 180 }
        // Connector discovery and history can require multiple network pages.
        if arguments.starts(with: ["collect", "source", "search"]) { return 45 }
        if arguments.starts(with: ["collect", "history"]) { return 45 }
        return 15
    }
}

private struct ATMCommandOutcome<Value> {
    let value: Value?
    let error: String?
    /// Set when the failure was a CLI/App contract skew rather than a bad
    /// payload. Carried as a type, not folded into `error`, because the caller has
    /// to present it differently: nothing refreshed, and the fix is an upgrade.
    let schemaMismatch: ATMDashboardSchemaMismatch?

    init(value: Value?, error: String?, schemaMismatch: ATMDashboardSchemaMismatch? = nil) {
        self.value = value
        self.error = error
        self.schemaMismatch = schemaMismatch
    }
}

/// Shape of `atm config get <key> --json` for boolean settings.
private struct ATMConfigBoolValue: Decodable {
    let value: Bool
}

private func decodeCommand<Value: Decodable>(
    _ runner: ATMCommandRunner,
    arguments: [String],
    as type: Value.Type = Value.self
) async -> ATMCommandOutcome<Value> {
    do {
        let data = try await runner.run(arguments)
        return ATMCommandOutcome(value: try JSONDecoder().decode(type, from: data), error: nil)
    } catch is CancellationError {
        return ATMCommandOutcome(value: nil, error: nil)
    } catch let mismatch as ATMDashboardSchemaMismatch {
        // Not truncated: the whole point of this message is the instruction at the
        // end of it.
        return ATMCommandOutcome(value: nil, error: mismatch.summary, schemaMismatch: mismatch)
    } catch {
        return ATMCommandOutcome(
            value: nil,
            error: ATMErrorText.compact(error.localizedDescription, limit: 180)
        )
    }
}

enum ATMAgentSessionContext {
    private static let environmentKeys = [
        "ATM_SESSION_ID",
        "CODEX_THREAD_ID",
        "CLAUDE_CODE_SESSION_ID",
        "PI_SESSION_ID",
    ]

    static func sessionID(
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> String? {
        environmentKeys.lazy
            .compactMap { environment[$0]?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .first { !$0.isEmpty }
    }
}

private final class ATMRunningProcess: @unchecked Sendable {
    private let lock = NSLock()
    private var process: Process?

    func attach(_ process: Process) {
        lock.lock()
        self.process = process
        lock.unlock()
    }

    func detach() {
        lock.lock()
        process = nil
        lock.unlock()
    }

    func terminate() {
        lock.lock()
        let process = self.process
        lock.unlock()
        if process?.isRunning == true {
            process?.terminate()
        }
    }
}

enum ATMErrorText {
    static func compact(_ value: String, limit: Int = 280) -> String {
        let normalized = value
            .split(whereSeparator: \.isWhitespace)
            .joined(separator: " ")
        guard normalized.count > limit else { return normalized }
        return String(normalized.prefix(limit)) + "…"
    }
}

enum ATMProjectOpenError: LocalizedError {
    case projectDirectoryNotFound(todoID: String, project: String?)
    case visualStudioCodeNotFound

    var errorDescription: String? {
        switch self {
        case .projectDirectoryNotFound(let todoID, let project):
            let projectName = project.flatMap { $0.isEmpty ? nil : $0 } ?? "未分项目"
            return "找不到 \(todoID) 对应的项目目录（\(projectName)）。请先在项目目录中绑定该任务。"
        case .visualStudioCodeNotFound:
            return "找不到 Visual Studio Code。请先安装 VS Code。"
        }
    }
}

enum ATMKnowledgeFeedbackError: LocalizedError {
    case noBoundSession

    var errorDescription: String? {
        "当前没有绑定的 ATM 会话。先绑定任务后再记录知识反馈。"
    }
}

struct ATMTodoSessionBinding: Decodable, Equatable {
    let cwd: String?
    let boundAt: Int64

    enum CodingKeys: String, CodingKey {
        case cwd
        case boundAt = "bound_at"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        cwd = try values.decodeIfPresent(String.self, forKey: .cwd)
        boundAt = try values.decodeIfPresent(Int64.self, forKey: .boundAt) ?? 0
    }

    init(cwd: String?, boundAt: Int64) {
        self.cwd = cwd
        self.boundAt = boundAt
    }
}

struct ATMTodoDetail: Decodable {
    let bindings: [ATMTodoSessionBinding]?
    let sessions: [ATMBoundSession]?
}

struct ATMBoundSession: Decodable, Equatable, Identifiable {
    let sessionID: String
    /// The transcript index's own id, which differs from `sessionID` for codex
    /// (the binding ledger stores the thread uuid, the index the rollout name).
    let indexedID: String?
    let shortID: String
    let agent: String
    let project: String
    let summary: String?
    let indexed: Bool
    let cwd: String?
    let bindingCount: Int
    let firstBoundAt: Int64
    let boundAt: Int64
    let unboundAt: Int64?
    let reason: String?
    let queries: Int
    let toolCalls: Int
    let startedAt: Int64
    let lastAt: Int64
    let inputTokens: Int64
    let outputTokens: Int64
    let costUSD: Double

    var id: String { sessionID }
    var isActive: Bool { unboundAt == nil }

    /// How long the session itself ran, which is not the same as how long it was
    /// bound to this todo — a session is usually bound partway through.
    var activeSeconds: Int64 {
        guard startedAt > 0, lastAt > startedAt else { return 0 }
        return lastAt - startedAt
    }

    enum CodingKeys: String, CodingKey {
        case sessionID = "session_id"
        case indexedID = "indexed_id"
        case shortID = "short_id"
        case agent, project, summary, indexed, cwd, reason, queries
        case bindingCount = "binding_count"
        case firstBoundAt = "first_bound_at"
        case boundAt = "bound_at"
        case unboundAt = "unbound_at"
        case toolCalls = "tool_calls"
        case startedAt = "started_at"
        case lastAt = "last_at"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case costUSD = "cost_usd"
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        let decodedShortID = try values.decodeIfPresent(String.self, forKey: .shortID) ?? ""
        sessionID = try values.decodeIfPresent(String.self, forKey: .sessionID) ?? decodedShortID
        indexedID = try values.decodeIfPresent(String.self, forKey: .indexedID)
        shortID = decodedShortID.isEmpty ? String(sessionID.prefix(8)) : decodedShortID
        agent = try values.decodeIfPresent(String.self, forKey: .agent) ?? ""
        project = try values.decodeIfPresent(String.self, forKey: .project) ?? ""
        summary = try values.decodeIfPresent(String.self, forKey: .summary)
        indexed = try values.decodeIfPresent(Bool.self, forKey: .indexed) ?? true
        cwd = try values.decodeIfPresent(String.self, forKey: .cwd)
        bindingCount = try values.decodeIfPresent(Int.self, forKey: .bindingCount) ?? 1
        boundAt = try values.decodeIfPresent(Int64.self, forKey: .boundAt) ?? 0
        firstBoundAt = try values.decodeIfPresent(Int64.self, forKey: .firstBoundAt) ?? boundAt
        unboundAt = try values.decodeIfPresent(Int64.self, forKey: .unboundAt)
        reason = try values.decodeIfPresent(String.self, forKey: .reason)
        queries = try values.decodeIfPresent(Int.self, forKey: .queries) ?? 0
        toolCalls = try values.decodeIfPresent(Int.self, forKey: .toolCalls) ?? 0
        startedAt = try values.decodeIfPresent(Int64.self, forKey: .startedAt) ?? 0
        lastAt = try values.decodeIfPresent(Int64.self, forKey: .lastAt) ?? 0
        inputTokens = try values.decodeIfPresent(Int64.self, forKey: .inputTokens) ?? 0
        outputTokens = try values.decodeIfPresent(Int64.self, forKey: .outputTokens) ?? 0
        costUSD = try values.decodeIfPresent(Double.self, forKey: .costUSD) ?? 0
    }
}

struct ATMTodoPrompt: Decodable {
    let prompt: String
}

enum ATMProjectFolderResolver {
    static func resolve(
        todo: ATMTodo,
        bindings: [ATMTodoSessionBinding],
        homeDirectory: URL = FileManager.default.homeDirectoryForCurrentUser,
        fileManager: FileManager = .default
    ) -> URL? {
        let bindingCandidates = bindings
            .sorted { $0.boundAt > $1.boundAt }
            .compactMap(\.cwd)
            .filter { !$0.isEmpty }
            .map { URL(fileURLWithPath: $0, isDirectory: true) }

        var projectCandidates: [URL] = []
        if let project = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines),
           !project.isEmpty {
            if project.hasPrefix("/") {
                projectCandidates.append(URL(fileURLWithPath: project, isDirectory: true))
            }
            projectCandidates += [
                homeDirectory.appendingPathComponent("mox/\(project)", isDirectory: true),
                homeDirectory.appendingPathComponent("work/\(project)", isDirectory: true),
                homeDirectory.appendingPathComponent(project, isDirectory: true),
            ]
        }

        for candidate in bindingCandidates + projectCandidates {
            var isDirectory: ObjCBool = false
            if fileManager.fileExists(atPath: candidate.path, isDirectory: &isDirectory),
               isDirectory.boolValue {
                return candidate.standardizedFileURL
            }
        }
        return nil
    }
}

struct ATMCommandRunner: Sendable {
    let executableURL: URL

    init(environment: [String: String] = ProcessInfo.processInfo.environment) throws {
        let fileManager = FileManager.default
        let home = fileManager.homeDirectoryForCurrentUser.path
        let candidates = [
            environment["ATM_EXECUTABLE"],
            "/usr/local/bin/atm",
            "/opt/homebrew/bin/atm",
            "\(home)/.local/bin/atm",
        ].compactMap { $0 }

        guard let path = candidates.first(where: { fileManager.isExecutableFile(atPath: $0) }) else {
            throw ATMCommandError.executableNotFound
        }
        executableURL = URL(fileURLWithPath: path)
    }

    func run(
        _ arguments: [String],
        standardInput: Data? = nil,
        timeout: TimeInterval? = nil
    ) async throws -> Data {
        let executableURL = executableURL
        let processHandle = ATMRunningProcess()
        let timeout = timeout ?? ATMCommandPolicy.timeout(for: arguments)
        let worker = Task.detached(priority: .utility) {
            let process = Process()
            let stdout = Pipe()
            let stderr = Pipe()
            let stdin = standardInput.map { _ in Pipe() }
            process.executableURL = executableURL
            process.arguments = arguments
            process.standardOutput = stdout
            process.standardError = stderr
            process.standardInput = stdin
            var environment = ProcessInfo.processInfo.environment
            let commonPath = "/usr/local/bin:/opt/homebrew/bin:\(FileManager.default.homeDirectoryForCurrentUser.path)/.local/bin"
            environment["PATH"] = commonPath + ":" + (environment["PATH"] ?? "")
            environment["ATM_SKIP_LOCAL_NOTIFICATION"] = "1"
            process.environment = environment

            try Task.checkCancellation()
            try process.run()
            processHandle.attach(process)
            defer { processHandle.detach() }
            if let standardInput, let stdin {
                stdin.fileHandleForWriting.write(standardInput)
                try? stdin.fileHandleForWriting.close()
            }
            let outputTask = Task.detached(priority: .utility) {
                stdout.fileHandleForReading.readDataToEndOfFile()
            }
            let errorTask = Task.detached(priority: .utility) {
                stderr.fileHandleForReading.readDataToEndOfFile()
            }
            let deadline = Date().addingTimeInterval(timeout)
            while process.isRunning {
                if Task.isCancelled {
                    process.terminate()
                    throw CancellationError()
                }
                if Date() >= deadline {
                    process.terminate()
                    let graceDeadline = Date().addingTimeInterval(0.5)
                    while process.isRunning, Date() < graceDeadline {
                        try? await Task.sleep(nanoseconds: 20_000_000)
                    }
                    if process.isRunning {
                        kill(process.processIdentifier, SIGKILL)
                    }
                    throw ATMCommandError.timedOut(arguments: arguments, seconds: timeout)
                }
                try await Task.sleep(nanoseconds: 20_000_000)
            }
            let output = await outputTask.value
            let errorOutput = await errorTask.value

            guard process.terminationStatus == 0 else {
                throw ATMCommandError.failed(
                    arguments: arguments,
                    status: process.terminationStatus,
                    message: String(data: errorOutput, encoding: .utf8) ?? ""
                )
            }
            return output
        }
        return try await withTaskCancellationHandler {
            do {
                let output = try await worker.value
                try Task.checkCancellation()
                return output
            } catch {
                try Task.checkCancellation()
                throw error
            }
        } onCancel: {
            processHandle.terminate()
            worker.cancel()
        }
    }
}

enum ATMSyncPolicy {
    static let interval: TimeInterval = 5 * 60

    static func shouldSync(lastAttemptAt: Date?, now: Date = Date()) -> Bool {
        guard let lastAttemptAt else { return true }
        return now.timeIntervalSince(lastAttemptAt) >= interval
    }
}

enum ATMLiveStatusRefreshPolicy {
    /// Presence is intentionally much faster than the one-minute dashboard,
    /// but only runs while a live-presence surface (Agent workspace or notch)
    /// is visible.
    static let interval: TimeInterval = 3

    /// Interval used while agent hooks are actively reporting. Each poll shells
    /// out to `atm session status`, which runs `ps` over every process and
    /// re-reads transcript files; once hooks are pushing, that work is a backstop
    /// for un-hooked agents rather than the primary signal.
    static let hookBackedInterval: TimeInterval = 8

    /// How long after the last hook event we still trust the push channel.
    /// Comfortably longer than an idle gap between turns, short enough that
    /// uninstalling a hook returns to fast polling within a minute.
    static let hookFreshness: TimeInterval = 45

    /// Coalescing window for event-triggered refreshes. One user action can fire
    /// several hooks at once and each refresh spawns a CLI process.
    static let eventRefreshDebounce: TimeInterval = 0.25

    /// The interval this tick should honour.
    static func interval(lastHookEventAt: Date?, now: Date) -> TimeInterval {
        guard let lastHookEventAt,
              now.timeIntervalSince(lastHookEventAt) < hookFreshness else {
            return interval
        }
        return hookBackedInterval
    }

    static func shouldPreserveFastStatus(
        lastAppliedAt: Date?,
        dashboardRequestStartedAt: Date
    ) -> Bool {
        guard let lastAppliedAt else { return false }
        return lastAppliedAt > dashboardRequestStartedAt
    }
}

enum ATMTodoAction: Equatable {
    case start
    case complete
    case drop
    case delete
    /// Park for later: `todo wait --wake 暂不处理`.
    case deferLater
    /// Back to the open backlog: stop work in progress, reject review, or
    /// unpark deferred work.
    case returnToOpen
}

/// Human-parked todos use waiting + this exact wake string (no new CLI status).
enum ATMTodoDeferred {
    static let wakeCondition = "暂不处理"
}

/// One lifecycle control exposed in the detail toolbar and/or context menu.
struct ATMTodoLifecycleItem: Equatable, Identifiable {
    let action: ATMTodoAction
    /// Context-menu title.
    let title: String
    /// Icon-button tooltip / accessibility label.
    let help: String
    let systemImage: String
    /// Shown as an icon in the detail toolbar (and quick panel). Drop stays menu-only.
    let isPrimary: Bool

    var id: String { "\(title)-\(systemImage)" }
}

/// Status-scoped lifecycle actions. Utility items (edit, copy prompt, delete)
/// stay available everywhere; these only cover work-state transitions.
enum ATMTodoStatusActions {
    static func isDeferred(_ todo: ATMTodo) -> Bool {
        todo.status == "waiting"
            && (todo.wakeCondition ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            == ATMTodoDeferred.wakeCondition
    }

    static func items(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        if isDeferred(todo) {
            // Unpark → open backlog, not in_progress.
            return [
                item(
                    .returnToOpen,
                    title: "移出暂不处理",
                    help: "移出暂不处理，回到待开始",
                    icon: "arrow.uturn.backward"
                ),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        }
        switch todo.status {
        case "review":
            // Human gate: accept the submission, or send it back to the backlog.
            return [
                item(.complete, title: "验收", help: "验收通过", icon: "checkmark"),
                item(
                    .returnToOpen,
                    title: "验收不通过（重新到待办）",
                    help: "验收不通过，重新到待办",
                    icon: "arrow.uturn.backward"
                ),
            ]
        case "open":
            return [
                item(.start, title: "开始", help: "开始此任务", icon: "play.fill"),
                item(.complete, title: "标记完成", help: "标记完成", icon: "checkmark"),
                item(.deferLater, title: "暂不处理", help: "暂不处理", icon: "moon.zzz"),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        case "in_progress":
            // Stopping work in progress means putting it back in the queue, not
            // parking it: 暂不处理 is for backlog you have decided to skip.
            return [
                item(.complete, title: "标记完成", help: "标记完成", icon: "checkmark"),
                item(
                    .returnToOpen,
                    title: "回到待办",
                    help: "回到待办，不再进行中",
                    icon: "arrow.uturn.backward"
                ),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        case "waiting":
            return [
                item(.start, title: "开始", help: "开始此任务", icon: "play.fill"),
                item(.complete, title: "标记完成", help: "标记完成", icon: "checkmark"),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        case "blocked":
            return [
                item(.start, title: "开始", help: "开始此任务", icon: "play.fill"),
                item(.complete, title: "标记完成", help: "标记完成", icon: "checkmark"),
                item(.deferLater, title: "暂不处理", help: "暂不处理", icon: "moon.zzz"),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        case "done", "dropped":
            return [
                item(
                    .start,
                    title: "重新开始",
                    help: "重新开始此任务",
                    icon: "arrow.counterclockwise"
                ),
            ]
        default:
            return [
                item(.start, title: "开始", help: "开始此任务", icon: "play.fill"),
                item(
                    .complete,
                    title: "标记\(todo.completionVerb)",
                    help: "标记\(todo.completionVerb)",
                    icon: "checkmark"
                ),
                item(.drop, title: "放弃", help: "放弃此任务", icon: "xmark", primary: false),
            ]
        }
    }

    static func primaryItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter(\.isPrimary)
    }

    /// Actions always visible as toolbar icon chips: start (or restart) and
    /// complete/accept. These are the stable "常用" controls; everything else
    /// drops into the `···` overflow menu. Derived from `items(for:)` so the
    /// inline set still tracks each status gate (e.g. review offers only
    /// 验收, done/dropped offers only 重新开始).
    static func inlineItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter { $0.action == .start || $0.action == .complete }
    }

    /// Remaining status-scoped actions that go inside the `···` overflow menu:
    /// deferLater / returnToOpen / drop, plus the non-primary seats that used
    /// to hide in the context menu only.
    static func overflowItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter { $0.action != .start && $0.action != .complete }
    }

    /// Launch prompt is for handing work to an agent; review is the human gate.
    static func showsLaunchPrompt(for todo: ATMTodo) -> Bool {
        todo.status != "review"
    }

    private static func item(
        _ action: ATMTodoAction,
        title: String,
        help: String,
        icon: String,
        primary: Bool = true
    ) -> ATMTodoLifecycleItem {
        ATMTodoLifecycleItem(
            action: action,
            title: title,
            help: help,
            systemImage: icon,
            isPrimary: primary
        )
    }
}

struct ATMTodoEdit: Equatable {
    let title: String
    let description: String
    let priority: String
    let project: String
    let lane: String
    let status: String
    let wakeCondition: String
    let reviewAt: String
    let source: String
}

enum ATMCommandBuilder {
    static func todaySessionUsage(sessionID: String? = nil) -> [String] {
        var arguments = ["stats", "--by", "session-usage", "--days", "1", "--json"]
        if let sessionID, !sessionID.isEmpty {
            arguments += ["--agent-session", sessionID]
        }
        return arguments
    }

    static func arguments(for action: ATMTodoAction, todo: ATMTodo) -> [String] {
        switch action {
        case .start:
            return ["todo", "start", todo.id]
        case .complete:
            return ["todo", "done", todo.id, "--reason", "通过 ATM 菜单栏\(todo.completionVerb)"]
        case .drop:
            return ["todo", "drop", todo.id, "--reason", "通过 ATM 菜单栏放弃"]
        case .delete:
            // --yes because the desktop already ran its own confirmation dialog;
            // without it the CLI waits on a stdin nobody can answer.
            return ["todo", "delete", todo.id, "--yes"]
        case .deferLater:
            return ["todo", "wait", todo.id, "--wake", ATMTodoDeferred.wakeCondition]
        case .returnToOpen:
            // Waiting (incl. deferred) has a dedicated wake path that clears
            // wake metadata and lands on open by default.
            if todo.status == "waiting" {
                let reason = ATMTodoStatusActions.isDeferred(todo)
                    ? "通过 ATM 菜单栏移出暂不处理"
                    : "通过 ATM 菜单栏回到待开始"
                return [
                    "todo", "wake", todo.id,
                    "--status", "open",
                    "--reason", reason,
                ]
            }
            return ["todo", "edit", todo.id, "--status", "open"]
        }
    }

    static func addTodo(_ draft: ATMTodoDraft) -> [String] {
        var arguments = ["todo", "add", draft.title, "--priority", draft.priority]
        if !draft.description.isEmpty { arguments += ["--desc", draft.description] }
        if !draft.project.isEmpty { arguments += ["--project", draft.project] }
        if !draft.lane.isEmpty { arguments += ["--lane", draft.lane] }
        // JSON so the desktop can select the new id after create succeeds.
        arguments.append("--json")
        return arguments
    }

    /// Parse the id returned by `atm todo add` (JSON object or plain first line).
    static func createdTodoID(from data: Data) -> String? {
        if let todo = try? JSONDecoder().decode(ATMTodo.self, from: data) {
            let id = todo.id.trimmingCharacters(in: .whitespacesAndNewlines)
            return id.isEmpty ? nil : id
        }
        guard let text = String(data: data, encoding: .utf8) else { return nil }
        let line = text
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .split(whereSeparator: \.isNewline)
            .first
            .map(String.init)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !line.isEmpty, line.range(of: #"^t\d+$"#, options: .regularExpression) != nil else {
            return nil
        }
        return line
    }

    static func editTodo(id: String, edit: ATMTodoEdit) -> [String] {
        [
            "todo", "edit", id,
            "--title", edit.title,
            "--desc", edit.description,
            "--priority", edit.priority,
            "--project", edit.project,
            "--lane", edit.lane,
            "--status", edit.status,
            "--wake", edit.wakeCondition,
            "--review-at", edit.reviewAt,
            "--source", edit.source,
        ]
    }

    static func todoPrompt(id: String) -> [String] {
        ["todo", "prompt", id, "--json"]
    }

    static func moveKnowledgeDocument(id: String, to collectionID: String) -> [String] {
        ["knowledge", "edit", id, "--collection", collectionID, "--json"]
    }

    /// Reading a source can require a connector round trip; `--local` answers
    /// from the synced archive instead. The history sheet uses both:
    /// local first so it opens instantly, then the network read to catch up.
    static func collectionHistory(sourceID: String, limit: Int, local: Bool) -> [String] {
        var arguments = ["collect", "history", sourceID, "--limit", String(limit), "--json"]
        if local { arguments.append("--local") }
        return arguments
    }
}

struct ATMTodaySessionsState: Equatable {
    var sessions: [ATMSessionUsage] = []
    var isLoading = false
    var errorMessage: String?
    var loadedAt: Date?
}

enum ATMTodaySessionsCachePolicy {
    static let timeToLive: TimeInterval = 5 * 60

    static func shouldLoad(
        loadedAt: Date?,
        now: Date = Date(),
        force: Bool = false
    ) -> Bool {
        if force { return true }
        guard let loadedAt else { return true }
        return now.timeIntervalSince(loadedAt) >= timeToLive
    }
}

/// Separate from the minute-refresh dashboard. Merely visiting the usage
/// overview never starts this query, and leaving the tab starts no timer.
@MainActor
final class ATMTodaySessionsStore: ObservableObject {
    @Published private(set) var state = ATMTodaySessionsState()

    func loadIfNeeded(now: Date = Date()) {
        load(now: now, force: false)
    }

    func refresh() {
        load(now: Date(), force: true)
    }

    private func load(now: Date, force: Bool) {
        guard !state.isLoading else { return }
        guard ATMTodaySessionsCachePolicy.shouldLoad(
            loadedAt: state.loadedAt,
            now: now,
            force: force
        ) else {
            return
        }

        state.isLoading = true
        state.errorMessage = nil
        Task {
            let outcome: ATMCommandOutcome<[ATMSessionUsage]>
            do {
                let runner = try ATMCommandRunner()
                outcome = await decodeCommand(
                    runner,
                    arguments: ATMCommandBuilder.todaySessionUsage(
                        sessionID: ATMAgentSessionContext.sessionID()
                    )
                )
            } catch {
                outcome = ATMCommandOutcome(
                    value: nil,
                    error: ATMErrorText.compact(error.localizedDescription, limit: 180)
                )
            }

            state.isLoading = false
            if let sessions = outcome.value {
                state.sessions = sessions
                state.loadedAt = now
                state.errorMessage = nil
            } else if let error = outcome.error {
                // Keep an older successful page visible if a manual refresh
                // fails; the timestamp remains stale so a later visit retries.
                state.errorMessage = error
            }
        }
    }
}

@MainActor
struct ATMStoreDashboardState {
    var snapshot = ATMDashboardSnapshot.empty
    var quota = ATMQuotaSnapshot(agents: [:])
    var allTodos: [ATMTodo] = []
    var errorMessage: String?
}

@MainActor
final class ATMDataStore: ObservableObject {
    /// Dashboard-backed values publish as one unit. A refresh used to assign
    /// quota, todos, snapshot and error independently, invalidating every view
    /// that observed this store several times for the same response.
    @Published private(set) var dashboardState = ATMStoreDashboardState()
    let todaySessionsStore = ATMTodaySessionsStore()

    var snapshot: ATMDashboardSnapshot { dashboardState.snapshot }
    var quota: ATMQuotaSnapshot { dashboardState.quota }
    var allTodos: [ATMTodo] { dashboardState.allTodos }
    private(set) var errorMessage: String? {
        get { dashboardState.errorMessage }
        set {
            guard dashboardState.errorMessage != newValue else { return }
            updateDashboardState { $0.errorMessage = newValue }
        }
    }

    /// Mirrors `grok_live_quota` in ~/.atm/config.json. The toggle is only a
    /// config entry point — the CLI owns fetching, caching and fallback.
    @Published private(set) var grokLiveQuotaEnabled = false
    /// Internal refresh gate only. No view renders this flag, so publishing it
    /// needlessly rebuilt the desktop at both ends of every one-minute refresh.
    private(set) var isLoading = false
    @Published private(set) var isSyncing = false
    @Published private(set) var isActing = false
    @Published private(set) var knowledgeCollections: [ATMKnowledgeCollection] = []
    @Published private(set) var isKnowledgeCatalogLoading = false
    @Published var knowledgeErrorMessage: String?
    @Published private(set) var progressByTodoID: [String: [ATMTodoProgressEntry]] = [:]
    @Published private(set) var loadingProgressTodoIDs: Set<String> = []
    @Published private(set) var boundSessionsByTodoID: [String: [ATMBoundSession]] = [:]
    @Published private(set) var loadingBoundSessionTodoIDs: Set<String> = []
    @Published private(set) var collectionOverview = ATMCollectionOverview.empty
    @Published private(set) var isCollecting = false
    @Published var collectionErrorMessage: String?
    @Published private(set) var agentHookReport: ATMAgentHookReport?
    @Published private(set) var isUpdatingAgentHooks = false
    @Published var agentHookErrorMessage: String?

    private var timer: Timer?
    private var liveStatusTimer: Timer?
    private var liveStatusPollingClients = 0
    /// Hook events arrive here and are joined onto every live-status snapshot.
    let agentEvents = ATMAgentEventBus()
    /// The last CLI snapshot with no attention overlay applied.
    ///
    /// The published snapshot carries the overlay baked into each row, so it
    /// cannot be re-merged against a changed set of signals without losing the
    /// rows themselves. Keeping the unstamped copy is what lets an event repaint
    /// the overlay without shelling out to `atm session status` again.
    private var rawLiveStatus: ATMLiveStatus?
    private var agentEventCancellable: AnyCancellable?
    private var agentEventRefreshWorkItem: DispatchWorkItem?
    private var lastSyncAttemptAt: Date?
    private var lastLiveStatusAppliedAt: Date?
    private var lastLiveStatusPollAt: Date?
    private var activeRefreshIncludesSync = false
    private var pendingRefresh = false
    private var pendingSync = false
    private var isCollectionRefreshing = false
    private var isLiveStatusLoading = false
    private var lastCollectionAttemptAt: Date?
    private var notifiedCollectionRunIDs: Set<String>?
    /// Keep successfully deleted todos hidden until a dashboard read observes
    /// their absence. This prevents an older in-flight refresh from restoring a
    /// row after the CLI has already removed it.
    private var optimisticallyDeletedTodoIDs: Set<String> = []
    private var optimisticallyUpdatedTodos: [String: ATMTodo] = [:]
    /// Prior id→status map for human-facing notifications. nil until first
    /// successful dashboard load (baseline, no historical flood).
    private var notifiedTodoStatus: [String: String]?

    var currentSessionID: String? {
        guard snapshot.currentSession?.bound == true else { return nil }
        return snapshot.currentSession?.binding?.sessionID
    }

    private func updateDashboardState(_ update: (inout ATMStoreDashboardState) -> Void) {
        var next = dashboardState
        update(&next)
        dashboardState = next
    }

    func applyDashboardRefresh(_ state: ATMStoreDashboardState) {
        dashboardState = state
    }

    func start() {
        guard timer == nil else { return }
        loadGrokLiveQuotaSetting()
        refresh()
        refresh(sync: true)
        refreshCollection(runIfDue: true)
        timer = Timer.scheduledTimer(withTimeInterval: 60, repeats: true) { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                self.refresh(sync: ATMSyncPolicy.shouldSync(lastAttemptAt: self.lastSyncAttemptAt))
                self.refreshCollection(runIfDue: true)
            }
        }
    }

    func stop() {
        timer?.invalidate()
        timer = nil
        liveStatusPollingClients = 0
        invalidateLiveStatusPolling()
    }

    func startLiveStatusPolling() {
        liveStatusPollingClients += 1
        guard liveStatusTimer == nil else { return }
        startAgentEventListener()
        refreshLiveStatus()
        // The timer runs at the fast interval and decides per tick whether this
        // tick is due, so hooks can slow polling down without tearing the timer
        // up and rebuilding it.
        let timer = Timer(timeInterval: ATMLiveStatusRefreshPolicy.interval, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.pollLiveStatusIfDue()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        liveStatusTimer = timer
    }

    /// Starts the notch socket and refreshes as soon as an event lands.
    ///
    /// This is the whole point of the hook channel: `atm session status` is a
    /// scrape of transcript files plus `ps`, so it cannot see a blocked
    /// permission prompt and lags the conversation regardless. An event tells us
    /// to look now, and carries the precise reason the poller could not infer.
    private func startAgentEventListener() {
        guard agentEventCancellable == nil else { return }
        agentEventCancellable = agentEvents.didApplyEvent
            .sink { [weak self] event in
                // Repaint from the overlay first, and without the CLI. The
                // signals live in memory, so retiring one is a pure re-merge of
                // the snapshot we already hold — making that wait on a
                // subprocess is what left a resolved permission prompt orange.
                self?.reapplyAttentionOverlay()
                guard event.event.mayChangeSnapshot else { return }
                self?.scheduleAgentEventRefresh()
            }
        agentEvents.start()
        // Not just for the settings pane: which agents have a `Stop` hook decides
        // whether a session already in flight at launch is allowed to have its
        // completion inferred from text. Without this the first minutes after
        // launch fall back to guessing for agents that in fact report properly.
        loadAgentHookStatus()
    }

    /// Whether hook events, not snapshot diffing, own this session's turn state.
    func isHookAuthoritative(_ session: ATMLiveSession) -> Bool {
        agentEvents.isHookAuthoritative(session, report: agentHookReport)
    }

    /// Coalesces the refresh. One user action can fire several hooks at once
    /// (Stop plus a Notification, say), and each refresh shells out to the CLI.
    private func scheduleAgentEventRefresh() {
        agentEventRefreshWorkItem?.cancel()
        let workItem = DispatchWorkItem { [weak self] in
            self?.refreshLiveStatus()
        }
        agentEventRefreshWorkItem = workItem
        DispatchQueue.main.asyncAfter(
            deadline: .now() + ATMLiveStatusRefreshPolicy.eventRefreshDebounce,
            execute: workItem
        )
    }

    /// Skips ticks while hooks are feeding us, where the poller is a backstop
    /// rather than the primary signal. Each poll runs `ps` over every process
    /// and re-reads transcript files, so the saved ticks are real work avoided.
    private func pollLiveStatusIfDue() {
        let interval = ATMLiveStatusRefreshPolicy.interval(
            lastHookEventAt: agentEvents.lastEventAt,
            now: Date()
        )
        if let lastAttempt = lastLiveStatusPollAt,
           Date().timeIntervalSince(lastAttempt) < interval - 0.25 {
            return
        }
        lastLiveStatusPollAt = Date()
        refreshLiveStatus()
    }

    func stopLiveStatusPolling() {
        liveStatusPollingClients = max(0, liveStatusPollingClients - 1)
        guard liveStatusPollingClients == 0 else { return }
        invalidateLiveStatusPolling()
    }

    private func invalidateLiveStatusPolling() {
        liveStatusTimer?.invalidate()
        liveStatusTimer = nil
        lastLiveStatusPollAt = nil
        agentEventRefreshWorkItem?.cancel()
        agentEventRefreshWorkItem = nil
        agentEventCancellable = nil
        // Release the socket so a second ATM instance, or the next launch after
        // a crash, can bind the same path.
        agentEvents.stop()
    }

    func refreshLiveStatus() {
        guard !isLiveStatusLoading else { return }
        isLiveStatusLoading = true
        Task {
            defer { isLiveStatusLoading = false }
            guard let runner = try? ATMCommandRunner() else { return }
            let outcome: ATMCommandOutcome<ATMLiveStatus> = await decodeCommand(
                runner,
                arguments: ["session", "status", "--json"]
            )
            guard let liveStatus = outcome.value else { return }
            lastLiveStatusAppliedAt = Date()
            rawLiveStatus = liveStatus
            reapplyAttentionOverlay()
        }
    }

    /// Re-joins the hook overlay onto the snapshot already in hand.
    ///
    /// Costs nothing but a dictionary lookup per row, so it is the right
    /// response to any event that only changes which sessions are waiting on
    /// the user. Expiring a signal counts: `signals` is purged on every apply,
    /// so a stale one disappears here rather than at the next poll.
    private func reapplyAttentionOverlay() {
        guard let rawLiveStatus else { return }
        updateDashboardState { [agentEvents] state in
            state.snapshot = state.snapshot.replacingLiveStatus(
                rawLiveStatus.applyingAttentionSignals(agentEvents.signals)
            )
        }
    }

    // MARK: - Agent hooks

    /// Reads hook registration state from the CLI rather than tracking it in the
    /// app, so the settings pane shows what the agents' config files actually
    /// contain — including hooks the user added or removed by hand.
    func loadAgentHookStatus() {
        runAgentHookCommand(["agent", "hook", "status", "--json"])
    }

    func installAgentHooks() {
        runAgentHookCommand(["agent", "hook", "install", "--json"])
    }

    func uninstallAgentHooks() {
        runAgentHookCommand(["agent", "hook", "uninstall", "--json"])
    }

    private func runAgentHookCommand(_ arguments: [String]) {
        guard !isUpdatingAgentHooks else { return }
        isUpdatingAgentHooks = true
        agentHookErrorMessage = nil
        Task {
            defer { isUpdatingAgentHooks = false }
            guard let runner = try? ATMCommandRunner() else {
                agentHookErrorMessage = "找不到 atm 命令"
                return
            }
            let outcome: ATMCommandOutcome<ATMAgentHookReport> = await decodeCommand(
                runner,
                arguments: arguments
            )
            if let report = outcome.value {
                agentHookReport = report
                // Surface a per-agent failure (an unparseable settings.json, say)
                // instead of reporting overall success.
                let failures = report.sources.compactMap { source in
                    source.error.map { "\(source.displayName): \($0)" }
                }
                agentHookErrorMessage = failures.isEmpty ? nil : failures.joined(separator: "\n")
            } else {
                agentHookErrorMessage = outcome.error
            }
        }
    }

    /// Reads the effective value (config file + env override) from the CLI so
    /// the toggle reflects reality even when config.json was edited by hand.
    func loadGrokLiveQuotaSetting() {
        Task {
            guard let runner = try? ATMCommandRunner() else { return }
            let outcome: ATMCommandOutcome<ATMConfigBoolValue> = await decodeCommand(
                runner,
                arguments: ["config", "get", "grok_live_quota", "--json"]
            )
            if let value = outcome.value {
                grokLiveQuotaEnabled = value.value
            }
        }
    }

    /// Persists the switch via `atm config set` and refreshes quota, so the
    /// change works identically for plain CLI users. Reverts on failure. After
    /// a successful write it re-reads the effective value: an
    /// ATM_GROK_LIVE_QUOTA env override beats the config file, and the toggle
    /// must show what `atm quota` will actually do.
    func setGrokLiveQuota(_ enabled: Bool) {
        guard enabled != grokLiveQuotaEnabled else { return }
        grokLiveQuotaEnabled = enabled
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["config", "set", "grok_live_quota", enabled ? "true" : "false"])
                loadGrokLiveQuotaSetting()
                refresh()
            } catch {
                grokLiveQuotaEnabled = !enabled
                errorMessage = "实时额度设置未保存：\(ATMErrorText.compact(error.localizedDescription, limit: 160))"
            }
        }
    }

    func refreshCollection(runIfDue: Bool = false) {
        guard !isCollectionRefreshing else { return }
        isCollectionRefreshing = true
        Task {
            defer { isCollectionRefreshing = false }
            guard let runner = try? ATMCommandRunner() else { return }
            var status: ATMCollectionOverview?
            do {
                let data = try await runner.run(["collect", "status", "--limit", "200", "--json"])
                status = try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
                if let status { notifyCollectionRuns(status.runs) }
                if let currentStatus = status,
                   runIfDue,
                   shouldRunCollection(currentStatus),
                   currentStatus.summary.enabledSources > 0 {
                    isCollecting = true
                    lastCollectionAttemptAt = Date()
                    defer { isCollecting = false }
                    _ = try await runner.run(["collect", "run", "--due", "--json"])
                    // Whatever this run filed as an insight is only readable once
                    // it reaches the knowledge base. --due decides for itself
                    // whether enough has accumulated to be worth a model call, so
                    // calling it on every collection cycle is cheap; a failure
                    // here must not lose the run that just succeeded.
                    _ = try? await runner.run(["collect", "digest", "--due", "--json"])
                    let refreshed = try await runner.run(["collect", "status", "--limit", "200", "--json"])
                    status = try JSONDecoder().decode(ATMCollectionOverview.self, from: refreshed)
                    if let status { notifyCollectionRuns(status.runs) }
                    refresh()
                }
                if let status {
                    collectionOverview = status
                    collectionErrorMessage = status.latestRun?.status == "failed"
                        ? status.latestRun?.error
                        : nil
                }
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
                // Refresh status once more after a failed run so its durable
                // audit row is still visible in the Collection workspace.
                if let data = try? await runner.run(["collect", "status", "--limit", "200", "--json"]),
                   let recovered = try? JSONDecoder().decode(ATMCollectionOverview.self, from: data) {
                    collectionOverview = recovered
                    notifyCollectionRuns(recovered.runs)
                }
            }
        }
    }

    func runCollectionNow() {
        guard !isCollecting else { return }
        notifyCollectionRuns(collectionOverview.runs)
        isCollecting = true
        collectionErrorMessage = nil
        lastCollectionAttemptAt = Date()
        Task {
            defer { isCollecting = false }
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["collect", "run", "--json"])
                let data = try await runner.run(["collect", "status", "--limit", "200", "--json"])
                collectionOverview = try JSONDecoder().decode(ATMCollectionOverview.self, from: data)
                notifyCollectionRuns(collectionOverview.runs)
                refresh()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
                refreshCollection()
            }
        }
    }

    func setCollectionEnabled(_ enabled: Bool) {
        guard enabled != collectionOverview.enabled else { return }
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["collect", enabled ? "enable" : "disable", "--json"])
                refreshCollection(runIfDue: enabled)
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    func setCollectionInterval(_ minutes: Int) {
        let value = max(minutes, 1)
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["config", "set", "collection_interval_minutes", String(value)])
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    func addCollectionSource(
        connector: String,
        target: ATMCollectionSourceTarget,
        name: String,
        project: String,
        priority: String,
        excludePattern: String,
        instruction: String,
        knowledgeCollection: String,
        strategy: String,
        decisionUnit: String,
        intervalMinutes: Int,
        enabled: Bool
    ) {
        let connectorID = connector.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !connectorID.isEmpty,
              !target.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        Task {
            do {
                let runner = try ATMCommandRunner()
                var arguments = ["collect", "source", "add", "--connector", connectorID] + target.arguments
                    + ["--priority", priority, "--strategy", strategy,
                       "--decision-unit", decisionUnit,
                       "--interval", String(intervalMinutes), "--json"]
                let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedProject = project.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedExclude = excludePattern.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedInstruction = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedKnowledge = knowledgeCollection.trimmingCharacters(in: .whitespacesAndNewlines)
                if !trimmedName.isEmpty { arguments += ["--name", trimmedName] }
                if !trimmedProject.isEmpty { arguments += ["--project", trimmedProject] }
                if !trimmedExclude.isEmpty { arguments += ["--exclude", trimmedExclude] }
                if !trimmedInstruction.isEmpty { arguments += ["--instruction", trimmedInstruction] }
                if !trimmedKnowledge.isEmpty { arguments += ["--knowledge-collection", trimmedKnowledge] }
                if !enabled { arguments.append("--disabled") }
                _ = try await runner.run(arguments)
                refreshCollection(runIfDue: collectionOverview.enabled)
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 220)
            }
        }
    }

    /// Lets a configured connector resolve human-readable names to stable IDs.
    func searchCollectionSources(
        connector: String,
        kind: String,
        keyword: String
    ) async -> ([ATMCollectionCandidate], String?) {
        let trimmed = keyword.trimmingCharacters(in: .whitespacesAndNewlines)
        let connectorID = connector.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !connectorID.isEmpty, !trimmed.isEmpty else { return ([], nil) }
        do {
            let runner = try ATMCommandRunner()
            let outcome: ATMCommandOutcome<ATMCollectionCandidateList> = await decodeCommand(
                runner,
                arguments: ["collect", "source", "search", trimmed, "--connector", connectorID,
                            "--kind", kind, "--limit", "10", "--json"]
            )
            return (outcome.value?.candidates ?? [], outcome.error)
        } catch {
            return ([], ATMErrorText.compact(error.localizedDescription, limit: 180))
        }
    }

    /// Reads a source's recent messages. `local` answers from the synced archive
    /// without calling the connector. The result belongs to whoever asked for it rather
    /// than to published state: this is a one-off look, not app-wide data.
    func collectionHistory(
        source: ATMCollectionSource,
        limit: Int = 50,
        local: Bool = false
    ) async -> (ATMCollectionHistory?, String?) {
        do {
            let runner = try ATMCommandRunner()
            let outcome: ATMCommandOutcome<ATMCollectionHistory> = await decodeCommand(
                runner,
                arguments: ATMCommandBuilder.collectionHistory(
                    sourceID: source.id, limit: limit, local: local
                )
            )
            return (outcome.value, outcome.error)
        } catch {
            return (nil, ATMErrorText.compact(error.localizedDescription, limit: 180))
        }
    }

    func setCollectionSource(_ source: ATMCollectionSource, enabled: Bool) {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["collect", "source", enabled ? "enable" : "disable", source.id, "--json"])
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    func deleteCollectionSource(_ source: ATMCollectionSource) {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["collect", "source", "delete", source.id, "--yes", "--json"])
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    func reprocessCollectionItem(_ item: ATMCollectionItem) {
        runCollectionItemAction(["reprocess", item.id])
    }

    func promoteCollectionItem(
        _ item: ATMCollectionItem,
        title: String? = nil,
        project: String? = nil,
        priority: String? = nil
    ) {
        runCollectionItemAction(
            collectionItemArguments(
                action: "promote", item: item, title: title,
                project: project, priority: priority
            )
        )
    }

    func correctCollectionItem(
        _ item: ATMCollectionItem,
        title: String,
        project: String,
        priority: String
    ) {
        runCollectionItemAction(
            collectionItemArguments(
                action: "correct", item: item, title: title,
                project: project, priority: priority
            )
        )
    }

    func revertCollectionItem(_ item: ATMCollectionItem) {
        runCollectionItemAction(["revert", item.id, "--yes"])
    }

    private func collectionItemArguments(
        action: String,
        item: ATMCollectionItem,
        title: String?,
        project: String?,
        priority: String?
    ) -> [String] {
        var arguments = [action, item.id]
        if let title { arguments += ["--title", title] }
        if let project { arguments += ["--project", project] }
        if let priority { arguments += ["--priority", priority] }
        return arguments
    }

    private func runCollectionItemAction(_ arguments: [String]) {
        guard !isCollecting else { return }
        isCollecting = true
        collectionErrorMessage = nil
        Task {
            defer { isCollecting = false }
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(["collect", "item"] + arguments + ["--json"])
                refreshCollection()
                refresh()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
                refreshCollection()
            }
        }
    }

    private func notifyCollectionRuns(_ runs: [ATMCollectionRun]) {
        let currentIDs = Set(runs.map(\.id))
        guard let previous = notifiedCollectionRunIDs else {
            notifiedCollectionRunIDs = currentIDs
            return
        }
        let newRuns = runs.filter { !previous.contains($0.id) && $0.status != "running" }
        notifiedCollectionRunIDs = previous.union(currentIDs)
        ATMNotificationManager.shared.sendCollectionSummary(newRuns)
    }

    private func shouldRunCollection(_ status: ATMCollectionOverview, now: Date = Date()) -> Bool {
        guard status.enabled else { return false }
        let sourceInterval = status.sources
            .filter(\.enabled)
            .map(\.effectiveIntervalMinutes)
            .min() ?? status.intervalMinutes
        // The global interval is the scheduler polling ceiling. A source may
        // request a faster cadence; `collect run --due` still decides which
        // individual sources actually perform network/model work.
        let interval = TimeInterval(max(min(status.intervalMinutes, sourceInterval), 1) * 60)
        if let attempt = lastCollectionAttemptAt, now.timeIntervalSince(attempt) < interval {
            return false
        }
        guard let latest = status.latestRun else { return true }
        return now.timeIntervalSince1970 - TimeInterval(latest.startedAt) >= interval
    }

    func refresh(sync: Bool = false) {
        guard !isLoading else {
            if sync && !activeRefreshIncludesSync {
                pendingSync = true
            } else if !sync {
                pendingRefresh = true
            }
            return
        }
        isLoading = true
        activeRefreshIncludesSync = sync
        Task {
            defer {
                isLoading = false
                activeRefreshIncludesSync = false
                if pendingSync || pendingRefresh {
                    let shouldSync = pendingSync
                    pendingSync = false
                    pendingRefresh = false
                    refresh(sync: shouldSync)
                }
            }
            do {
                let runner = try ATMCommandRunner()
                var warnings: [String] = []
                if sync {
                    isSyncing = true
                    lastSyncAttemptAt = Date()
                    defer { isSyncing = false }
                    do {
                        _ = try await runner.run(["sync"])
                    } catch {
                        warnings.append("同步：\(ATMErrorText.compact(error.localizedDescription, limit: 160))")
                    }
                }

                let dashboardArguments: [String] = {
                    var arguments = ["dashboard", "--json"]
                    if let sessionID = ATMAgentSessionContext.sessionID() {
                        arguments += ["--agent-session", sessionID]
                    }
                    return arguments
                }()
                let dashboardRequestStartedAt = Date()
                // Quota lives in the agents' own logs rather than the session
                // index, so it is a separate command. Run it concurrently with
                // the dashboard so the extra read costs no wall-clock time.
                async let dashboardTask: ATMCommandOutcome<ATMDashboardEnvelope> = decodeCommand(
                    runner,
                    arguments: dashboardArguments
                )
                async let quotaTask: ATMCommandOutcome<ATMQuotaSnapshot> = decodeCommand(
                    runner,
                    arguments: ["quota", "--json"]
                )
                let (dashboard, quotaOutcome) = await (dashboardTask, quotaTask)

                var nextState = dashboardState
                if let error = dashboard.error {
                    warnings.append("仪表盘：\(error)")
                }
                if let error = quotaOutcome.error {
                    warnings.append("配额：\(error)")
                }
                if let value = quotaOutcome.value {
                    nextState.quota = value
                }
                if let value = dashboard.value {
                    let deletedIDs = optimisticallyDeletedTodoIDs
                    let updatedTodos = optimisticallyUpdatedTodos
                    let serverTodoIDs = Set(value.todos.map(\.id))
                    var observedTransitionIDs: Set<String> = []
                    var incoming = value.todos.compactMap { serverTodo -> ATMTodo? in
                        guard !deletedIDs.contains(serverTodo.id) else { return nil }
                        guard let updated = updatedTodos[serverTodo.id] else {
                            return serverTodo
                        }
                        if serverTodo.status == updated.status {
                            observedTransitionIDs.insert(serverTodo.id)
                            return serverTodo
                        }
                        return updated
                    }
                    incoming.append(contentsOf: updatedTodos.values.filter {
                        !serverTodoIDs.contains($0.id) && !deletedIDs.contains($0.id)
                    })
                    let events = ATMTodoNotificationDiff.events(
                        previous: notifiedTodoStatus,
                        current: incoming
                    )
                    for (todo, event) in events {
                        ATMNotificationManager.shared.send(
                            ATMTodoNotificationDiff.payload(for: todo, event: event),
                            todoID: todo.id
                        )
                    }
                    notifiedTodoStatus = ATMTodoNotificationDiff.statusMap(from: incoming)
                    nextState.allTodos = incoming
                    var snapshot = value.makeSnapshot()
                        .removingTodos(withIDs: deletedIDs)
                    if ATMLiveStatusRefreshPolicy.shouldPreserveFastStatus(
                        lastAppliedAt: lastLiveStatusAppliedAt,
                        dashboardRequestStartedAt: dashboardRequestStartedAt
                    ) {
                        snapshot = snapshot.replacingLiveStatus(dashboardState.snapshot.liveStatus)
                    }
                    for updated in updatedTodos.values
                    where !observedTransitionIDs.contains(updated.id) {
                        snapshot = snapshot.replacingTodo(updated)
                    }
                    nextState.snapshot = snapshot
                    optimisticallyDeletedTodoIDs.formIntersection(
                        Set(value.todos.map(\.id))
                    )
                    for id in observedTransitionIDs {
                        optimisticallyUpdatedTodos.removeValue(forKey: id)
                    }
                    if let attempt = value.indexHealth.sync.lastAttemptAt,
                       let date = ISO8601DateFormatter().date(from: attempt) {
                        lastSyncAttemptAt = date
                    }
                }
                // A contract skew is not "some data did not refresh" — the App and
                // the CLI cannot talk at all, and wrapping it in that prefix next to
                // unrelated warnings buries the one thing the user must act on.
                if let mismatch = dashboard.schemaMismatch {
                    nextState.errorMessage = mismatch.summary
                    ATMLog.failure("dashboard_schema_mismatch", fields: [
                        "cli_version": String(mismatch.cliVersion),
                        "app_version": String(mismatch.appVersion),
                    ])
                } else {
                    nextState.errorMessage = warnings.isEmpty
                        ? nil
                        : "部分数据未刷新：" + warnings.prefix(3).joined(separator: "；")
                            + (warnings.count > 3 ? "；另有 \(warnings.count - 3) 项" : "")
                }
                // On screen this is replaced by the next successful cycle, which is
                // why an intermittent failure was impossible to investigate: by the
                // time anyone looked, the evidence was gone.
                if let error = dashboard.error {
                    ATMLog.failure("dashboard_refresh_failed", error: error)
                }
                if let error = quotaOutcome.error {
                    ATMLog.failure("quota_refresh_failed", error: error)
                }
                applyDashboardRefresh(nextState)
            } catch {
                errorMessage = error.localizedDescription
                ATMLog.failure("refresh_failed", error: error.localizedDescription)
            }
        }
    }

    func perform(_ action: ATMTodoAction, on todo: ATMTodo) {
        guard !isActing else { return }
        isActing = true
        errorMessage = nil
        Task {
            defer { isActing = false }
            do {
                let runner = try ATMCommandRunner()
                let arguments = ATMCommandBuilder.arguments(for: action, todo: todo)
                _ = try await runner.run(arguments)
                applySuccessfulTodoAction(action, on: todo)
                switch action {
                case .complete:
                    ATMNotificationManager.shared.sendTodoCompleted(todo)
                case .drop:
                    ATMNotificationManager.shared.send(
                        .todoDropped(todo),
                        todoID: todo.id
                    )
                default:
                    break
                }
                refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    /// The CLI mutation is authoritative, so deletion should disappear from the
    /// UI immediately and lifecycle actions should move to their destination
    /// section without waiting for a complete dashboard reload. A queued refresh
    /// reconciles every other field and eventually clears the optimistic state.
    func applySuccessfulTodoAction(_ action: ATMTodoAction, on todo: ATMTodo) {
        if action == .delete {
            let deletedIDs: Set<String> = [todo.id]
            optimisticallyDeletedTodoIDs.insert(todo.id)
            optimisticallyUpdatedTodos.removeValue(forKey: todo.id)
            updateDashboardState {
                $0.allTodos.removeAll { $0.id == todo.id }
                $0.snapshot = $0.snapshot.removingTodos(withIDs: deletedIDs)
            }
            progressByTodoID.removeValue(forKey: todo.id)
            return
        }

        let updated: ATMTodo
        switch action {
        case .start:
            updated = todo.replacingLifecycle(status: "in_progress")
        case .complete:
            updated = todo.replacingLifecycle(status: "done")
        case .drop:
            updated = todo.replacingLifecycle(status: "dropped")
        case .deferLater:
            updated = todo.replacingLifecycle(
                status: "waiting",
                wakeCondition: ATMTodoDeferred.wakeCondition
            )
        case .returnToOpen:
            updated = todo.replacingLifecycle(status: "open")
        case .delete:
            return
        }
        optimisticallyUpdatedTodos[todo.id] = updated
        updateDashboardState {
            if let index = $0.allTodos.firstIndex(where: { $0.id == todo.id }) {
                $0.allTodos[index] = updated
            } else {
                $0.allTodos.append(updated)
            }
            $0.snapshot = $0.snapshot.replacingTodo(updated)
        }
    }

    /// Creates a todo, refreshes the list, then invokes `onCreated` with the new id
    /// so the UI can select it (default behavior for the add-task sheet).
    func addTodo(_ draft: ATMTodoDraft, onCreated: ((String) -> Void)? = nil) {
        guard draft.isSubmittable, !isActing else { return }
        isActing = true
        errorMessage = nil
        Task {
            defer { isActing = false }
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(ATMCommandBuilder.addTodo(draft))
                let createdID = ATMCommandBuilder.createdTodoID(from: data)
                // Prefer the decoded todo so selection can resolve before the next
                // full dashboard refresh lands.
                if let created = try? JSONDecoder().decode(ATMTodo.self, from: data),
                   !allTodos.contains(where: { $0.id == created.id }) {
                    updateDashboardState { $0.allTodos.append(created) }
                    // Human just filed this in the UI — do not also banner "新建".
                    var seen = notifiedTodoStatus ?? [:]
                    seen[created.id] = created.status
                    notifiedTodoStatus = seen
                } else if let createdID {
                    var seen = notifiedTodoStatus ?? [:]
                    seen[createdID] = "open"
                    notifiedTodoStatus = seen
                }
                if let createdID {
                    onCreated?(createdID)
                }
                refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func editTodo(_ todo: ATMTodo, edit: ATMTodoEdit) {
        let trimmedTitle = edit.title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedTitle.isEmpty, !isActing else { return }
        isActing = true
        errorMessage = nil
        Task {
            defer { isActing = false }
            do {
                let runner = try ATMCommandRunner()
                let normalized = ATMTodoEdit(
                    title: trimmedTitle,
                    description: edit.description.trimmingCharacters(in: .whitespacesAndNewlines),
                    priority: edit.priority,
                    project: edit.project.trimmingCharacters(in: .whitespacesAndNewlines),
                    lane: edit.lane.trimmingCharacters(in: .whitespacesAndNewlines),
                    status: edit.status,
                    wakeCondition: edit.wakeCondition.trimmingCharacters(in: .whitespacesAndNewlines),
                    reviewAt: edit.reviewAt.trimmingCharacters(in: .whitespacesAndNewlines),
                    source: edit.source.trimmingCharacters(in: .whitespacesAndNewlines)
                )
                _ = try await runner.run(ATMCommandBuilder.editTodo(id: todo.id, edit: normalized))
                refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func openTodoProjectInVSCode(_ todo: ATMTodo) {
        errorMessage = nil
        Task {
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(["todo", "show", todo.id, "--json"])
                let detail = try JSONDecoder().decode(ATMTodoDetail.self, from: data)
                guard let folderURL = ATMProjectFolderResolver.resolve(
                    todo: todo,
                    bindings: detail.bindings ?? []
                ) else {
                    throw ATMProjectOpenError.projectDirectoryNotFound(
                        todoID: todo.id,
                        project: todo.project
                    )
                }

                let workspace = NSWorkspace.shared
                let bundleIDs = ["com.microsoft.VSCode", "com.microsoft.VSCodeInsiders"]
                guard let applicationURL = bundleIDs.lazy.compactMap({
                    workspace.urlForApplication(withBundleIdentifier: $0)
                }).first else {
                    throw ATMProjectOpenError.visualStudioCodeNotFound
                }

                let configuration = NSWorkspace.OpenConfiguration()
                configuration.activates = true
                workspace.open(
                    [folderURL],
                    withApplicationAt: applicationURL,
                    configuration: configuration
                ) { [weak self] _, error in
                    guard let error else { return }
                    Task { @MainActor in
                        self?.errorMessage = error.localizedDescription
                    }
                }
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func progress(for todoID: String) -> [ATMTodoProgressEntry] {
        progressByTodoID[todoID] ?? []
    }

    func isLoadingProgress(for todoID: String) -> Bool {
        loadingProgressTodoIDs.contains(todoID)
    }

    func loadProgress(for todoID: String) {
        guard !loadingProgressTodoIDs.contains(todoID) else { return }
        loadingProgressTodoIDs.insert(todoID)
        Task {
            defer { loadingProgressTodoIDs.remove(todoID) }
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(["todo", "doc", todoID, "--json"])
                // content is optional for older CLI builds that returned exists:false
                // without a body when the markdown card was missing.
                struct Doc: Decodable {
                    let content: String?
                    let exists: Bool?
                }
                let doc = try JSONDecoder().decode(Doc.self, from: data)
                progressByTodoID[todoID] = ATMTodoProgressEntry.parse(from: doc.content ?? "")
            } catch {
                progressByTodoID[todoID] = []
            }
        }
    }

    func boundSessions(for todoID: String) -> [ATMBoundSession] {
        boundSessionsByTodoID[todoID] ?? []
    }

    func isLoadingBoundSessions(for todoID: String) -> Bool {
        loadingBoundSessionTodoIDs.contains(todoID)
    }

    func loadBoundSessions(for todoID: String) {
        guard !loadingBoundSessionTodoIDs.contains(todoID) else { return }
        loadingBoundSessionTodoIDs.insert(todoID)
        Task {
            defer { loadingBoundSessionTodoIDs.remove(todoID) }
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(["todo", "show", todoID, "--json"])
                let detail = try JSONDecoder().decode(ATMTodoDetail.self, from: data)
                boundSessionsByTodoID[todoID] = detail.sessions ?? []
            } catch {
                boundSessionsByTodoID[todoID] = []
            }
        }
    }

    /// Fetches the line a human pastes into a fresh agent session. The CLI owns
    /// the wording, so the app cannot drift from `atm todo prompt`; the caller
    /// writes it to the pasteboard, which is where every other copy action in
    /// this app puts its result.
    func launchPrompt(for todo: ATMTodo) async -> String? {
        errorMessage = nil
        do {
            let runner = try ATMCommandRunner()
            let data = try await runner.run(ATMCommandBuilder.todoPrompt(id: todo.id))
            return try JSONDecoder().decode(ATMTodoPrompt.self, from: data).prompt
        } catch {
            errorMessage = error.localizedDescription
            return nil
        }
    }

    func refreshKnowledgeCatalog() {
        guard !isKnowledgeCatalogLoading else { return }
        isKnowledgeCatalogLoading = true
        knowledgeErrorMessage = nil
        Task {
            defer { isKnowledgeCatalogLoading = false }
            do {
                let runner = try ATMCommandRunner()
                let data = try await runner.run(["knowledge", "catalog", "--json"])
                knowledgeCollections = try JSONDecoder().decode([ATMKnowledgeCollection].self, from: data)
            } catch {
                knowledgeErrorMessage = error.localizedDescription
            }
        }
    }

    func knowledgeDocuments(collectionID: String, status: String = "active") async throws -> [ATMKnowledgeDocumentSummary] {
        let runner = try ATMCommandRunner()
        let arguments = ["knowledge", "list", "--collection", collectionID, "--json"]
        let data = try await runner.run(arguments)
        let decoded = try JSONDecoder().decode([ATMKnowledgeDocumentSummary].self, from: data)
        var seen = Set<String>()
        return decoded.filter { ($0.status ?? "active") == status && seen.insert($0.documentID).inserted }
    }

    func createCollection(id: String, name: String) async throws {
        let runner = try ATMCommandRunner()
        var arguments = ["knowledge", "collection", "create", id]
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedName.isEmpty { arguments += ["--name", trimmedName] }
        arguments.append("--json")
        _ = try await runner.run(arguments)
    }

    func renameCollectionName(id: String, name: String) async throws {
        let runner = try ATMCommandRunner()
        _ = try await runner.run(["knowledge", "collection", "edit", id, "--name", name, "--json"])
    }

    func deleteCollection(id: String, force: Bool, moveTo: String?) async throws {
        let runner = try ATMCommandRunner()
        var arguments = ["knowledge", "collection", "delete", id]
        if let moveTo, !moveTo.isEmpty { arguments += ["--move-to", moveTo] }
        if force { arguments.append("--force") }
        arguments.append("--json")
        _ = try await runner.run(arguments)
    }

    func searchKnowledge(_ query: String, status: String = "active") async throws -> [ATMKnowledgeDocumentSummary] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        let runner = try ATMCommandRunner()
        var arguments = [
            "knowledge", "search", trimmedQuery, "--status", status, "--limit", "200", "--json",
        ]
        if let currentSessionID {
            arguments += ["--session", currentSessionID]
        }
        let data = try await runner.run(arguments)
        let decoded = try JSONDecoder().decode([ATMKnowledgeDocumentSummary].self, from: data)
        var seen = Set<String>()
        return decoded.filter { seen.insert($0.documentID).inserted }
    }

    func searchTodos(_ query: String) async throws -> [ATMTodo] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        let runner = try ATMCommandRunner()
        let data = try await runner.run([
            "todo", "list", "--status", "all", "--query", trimmedQuery, "--json",
        ])
        return try JSONDecoder().decode([ATMTodo].self, from: data)
    }

    func searchSessions(_ query: String) async throws -> [ATMSessionSearchHit] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        let runner = try ATMCommandRunner()
        // An explicit limit, matching knowledge search: the palette dedupes down
        // to sessions, so it needs more message matches than the CLI default
        // reserves for a terminal reader.
        let data = try await runner.run([
            "session", "search", trimmedQuery, "--limit", "200", "--json",
        ])
        let decoded = try JSONDecoder().decode(ATMSessionSearchResult.self, from: data)
        var seen = Set<String>()
        return decoded.matches.filter { seen.insert($0.shortID).inserted }
    }

    func knowledgeDocument(_ documentID: String) async throws -> ATMKnowledgeDocument {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(["knowledge", "get", documentID, "--json"])
        return try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
    }

    func updateKnowledgeDocument(_ documentID: String, content: String) async throws -> ATMKnowledgeDocument {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(
            ["knowledge", "update", documentID, "--file", "-", "--json"],
            standardInput: Data(content.utf8)
        )
        return try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
    }

    func addKnowledgeDocument(_ draft: ATMKnowledgeDraft) async throws -> ATMKnowledgeDocument {
        let runner = try ATMCommandRunner()
        var arguments = [
            "knowledge", "add", draft.title,
            "--collection", draft.collection,
            "--producer", "human",
        ]
        appendValues(draft.domains, flag: "--domain", to: &arguments)
        appendValues(draft.tags, flag: "--tag", to: &arguments)
        appendValues(draft.projects, flag: "--project", to: &arguments)
        arguments += ["--file", "-", "--json"]
        let data = try await runner.run(arguments, standardInput: Data(draft.content.utf8))
        return try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
    }

    func importKnowledge(at url: URL, collectionID: String) async throws -> [ATMKnowledgeDocument] {
        let runner = try ATMCommandRunner()
        let data = try await runner.run([
            "knowledge", "import", url.path,
            "--collection", collectionID,
            "--producer", "atm-desktop",
            "--json",
        ])
        return try JSONDecoder().decode([ATMKnowledgeDocument].self, from: data)
    }

    func editKnowledgeDocument(_ documentID: String, edit: ATMKnowledgeMetadataEdit) async throws -> ATMKnowledgeDocument {
        let runner = try ATMCommandRunner()
        var arguments = [
            "knowledge", "edit", documentID,
            "--title", edit.title,
            "--collection", edit.collection,
            "--status", edit.status,
        ]
        appendReplacementValues(edit.domains, flag: "--domain", to: &arguments)
        appendReplacementValues(edit.tags, flag: "--tag", to: &arguments)
        appendReplacementValues(edit.projects, flag: "--project", to: &arguments)
        arguments.append("--json")
        let data = try await runner.run(arguments)
        return try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
    }

    func moveKnowledgeDocument(_ documentID: String, to collectionID: String) async throws -> ATMKnowledgeDocument {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(ATMCommandBuilder.moveKnowledgeDocument(id: documentID, to: collectionID))
        return try JSONDecoder().decode(ATMKnowledgeDocument.self, from: data)
    }

    func deleteKnowledgeDocument(_ documentID: String) async throws {
        let runner = try ATMCommandRunner()
        _ = try await runner.run(["knowledge", "delete", documentID, "--json"])
    }

    func knowledgeAudit(staleDays: Int = 180) async throws -> ATMKnowledgeAuditReport {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(["knowledge", "audit", "--stale-days", String(staleDays), "--json"])
        return try JSONDecoder().decode(ATMKnowledgeAuditReport.self, from: data)
    }

    func knowledgeQuality() async throws -> [ATMKnowledgeQuality] {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(["knowledge", "quality", "--json"])
        return try JSONDecoder().decode([ATMKnowledgeQuality].self, from: data)
    }

    func recordKnowledgeFeedback(
        documentID: String,
        outcome: String,
        note: String
    ) async throws {
        guard let currentSessionID else {
            throw ATMKnowledgeFeedbackError.noBoundSession
        }
        let runner = try ATMCommandRunner()
        var arguments = [
            "knowledge", "feedback", documentID,
            "--session", currentSessionID,
            "--outcome", outcome,
        ]
        let trimmedNote = note.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedNote.isEmpty {
            arguments += ["--note", trimmedNote]
        }
        arguments.append("--json")
        _ = try await runner.run(arguments)
    }

    func doctor() async throws -> ATMDoctorReport {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(["doctor", "--json"])
        return try JSONDecoder().decode(ATMDoctorReport.self, from: data)
    }

    private func appendValues(_ values: [String], flag: String, to arguments: inout [String]) {
        for value in values where !value.isEmpty { arguments += [flag, value] }
    }

    private func appendReplacementValues(_ values: [String], flag: String, to arguments: inout [String]) {
        if values.isEmpty {
            arguments += [flag, ""]
        } else {
            appendValues(values, flag: flag, to: &arguments)
        }
    }

    func memories(query: String) async throws -> [ATMMemoryHit] {
        let runner = try ATMCommandRunner()
        var arguments = ["memory", "recall"]
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedQuery.isEmpty { arguments.append(trimmedQuery) }
        arguments += ["--limit", "200", "--json"]
        let data = try await runner.run(arguments)
        return try JSONDecoder().decode([ATMMemoryHit].self, from: data)
    }

    func supersedeMemory(_ memory: ATMMemoryHit, content: String) async throws {
        let runner = try ATMCommandRunner()
        var arguments = ["memory", "supersede", memory.id, "--scope", memory.scope, "--file", "-"]
        for tag in memory.tags {
            arguments += ["--tag", tag]
        }
        if let source = memory.metadata["source"], !source.isEmpty {
            arguments += ["--source", source]
        }
        arguments.append("--json")
        _ = try await runner.run(arguments, standardInput: Data(content.utf8))
    }

    func sessionTranscript(_ sessionID: String) async throws -> String {
        let runner = try ATMCommandRunner()
        let data = try await runner.run(["session", "show", sessionID])
        return String(decoding: data, as: UTF8.self)
    }
}
