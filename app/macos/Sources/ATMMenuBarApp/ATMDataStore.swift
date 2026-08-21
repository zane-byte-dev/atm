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

/// The complete result of a launched CLI process. `_ipc` needs stdout even when
/// the status is non-zero because the CLI writes its structured error envelope
/// there; ordinary commands continue to use `ATMCommandRunner.run`, which turns
/// this result into the same checked stdout behavior they had before.
struct ATMCommandProcessResult: Sendable {
    let standardOutput: Data
    let standardError: Data
    let terminationStatus: Int32

    func commandError(arguments: [String]) -> ATMCommandError {
        ATMCommandError.failed(
            arguments: arguments,
            status: terminationStatus,
            message: String(data: standardError, encoding: .utf8) ?? ""
        )
    }
}

enum ATMCommandPolicy {
    static func timeout(for arguments: [String]) -> TimeInterval {
        if arguments.first == "sync" { return 120 }
        if arguments.first == "day" { return 60 }
        // Guard decisions cannot use the replayable `_ipc` door. Approving may
        // execute a real network command, so keep the longer CLI ceiling.
        if arguments.starts(with: ["guard", "approve"]) { return 90 }
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
    } catch let mismatch as ATMIPCProtocolMismatch {
        // Same reason as above, and caught separately so the generic branch below
        // does not compact away the upgrade instruction.
        return ATMCommandOutcome(value: nil, error: mismatch.summary)
    } catch {
        return ATMCommandOutcome(
            value: nil,
            error: ATMErrorText.compact(error.localizedDescription, limit: 180)
        )
    }
}

private func decodeIPCCommand<Request: Encodable, Value: Decodable>(
    _ runner: ATMCommandRunner,
    method: ATMIPCMethod<Request, Value>,
    request: Request
) async -> ATMCommandOutcome<Value> {
    do {
        let value = try await ATMIPCClient(runner: runner).call(method, request: request)
        return ATMCommandOutcome(value: value, error: nil)
    } catch is CancellationError {
        return ATMCommandOutcome(value: nil, error: nil)
    } catch let mismatch as ATMDashboardSchemaMismatch {
        return ATMCommandOutcome(value: nil, error: mismatch.summary, schemaMismatch: mismatch)
    } catch let mismatch as ATMIPCProtocolMismatch {
        return ATMCommandOutcome(value: nil, error: mismatch.summary)
    } catch let mismatch as ATMIPCEnvelopeVersionMismatch {
        return ATMCommandOutcome(
            value: nil,
            error: [mismatch.errorDescription, mismatch.recoverySuggestion]
                .compactMap { $0 }
                .joined(separator: " ")
        )
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
    let todo: ATMTodo?
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
    /// The session's last assistant message, straight from the index. This is the
    /// durable copy of what live status calls `latest_result`: a Todo whose
    /// session has aged out of the live window would otherwise lose the outcome
    /// text entirely.
    let latestResult: String?
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
        case latestResult = "latest_result"
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
        latestResult = try values.decodeIfPresent(String.self, forKey: .latestResult)
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
        let result = try await runRaw(
            arguments,
            standardInput: standardInput,
            timeout: timeout
        )
        guard result.terminationStatus == 0 else {
            throw result.commandError(arguments: arguments)
        }
        return result.standardOutput
    }

    func runRaw(
        _ arguments: [String],
        standardInput: Data? = nil,
        timeout: TimeInterval? = nil
    ) async throws -> ATMCommandProcessResult {
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
            return ATMCommandProcessResult(
                standardOutput: output,
                standardError: errorOutput,
                terminationStatus: process.terminationStatus
            )
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
    /// but the attention notifier holds it open for the whole lifetime of the
    /// app, since a blocked agent has to be noticed with no window open.
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

    /// The live status a dashboard refresh should land: the fresher of the two
    /// candidates, always carrying the hook overlay.
    ///
    /// Named and tested rather than inlined because forgetting the overlay on
    /// one write path is exactly the bug this replaces. The dashboard snapshot
    /// carries `live_status` that has never seen a hook event, so landing it raw
    /// retired every attention banner and then re-raised each one at the next
    /// poll — a blocked agent re-notifying about once a minute.
    static func mergedLiveStatus(
        dashboard: ATMLiveStatus,
        fast: ATMLiveStatus,
        preserveFast: Bool,
        signals: [String: ATMAgentAttentionSignal],
        now: Date = Date()
    ) -> ATMLiveStatus {
        (preserveFast ? fast : dashboard).applyingAttentionSignals(signals, now: now)
    }
}

enum ATMTodoAction: Equatable {
    case start
    case complete
    /// Recoverable removal from the working set.
    case archive
    /// Bring a recoverably removed todo back with its lifecycle state intact.
    case restore
    /// Irreversible removal, exposed only from the archive.
    case delete
    /// Back to the open backlog: stop work in progress, reject review, or
    /// clear waiting presentation metadata.
    case returnToOpen
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

/// Lifecycle actions. Utility items (edit, copy prompt, delete) stay available
/// everywhere; these only cover work-state transitions.
///
/// Lifecycle controls expose only the four-state transitions. Archive is a
/// separate retention action supplied by the surrounding menu.
enum ATMTodoStatusActions {
    static func isClosed(_ todo: ATMTodo) -> Bool {
        todo.status == "done"
    }

    static func items(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        if isClosed(todo) {
            return [
                item(
                    .start,
                    title: "重新开始",
                    help: "重新开始此任务",
                    icon: "arrow.counterclockwise"
                ),
            ]
        }
        // Review is the human gate, so closing it is 验收 rather than 完成. Sending
        // it back needs no such wording: 回到待办 already says what happens, and
        // 「验收不通过」 only added a verdict nobody has to pronounce.
        let isReview = todo.status == "review"
        var actions: [ATMTodoLifecycleItem] = []
        if todo.status != "in_progress" {
            actions.append(item(.start, title: "开始", help: "开始此任务", icon: "play.fill"))
        }
        actions.append(
            item(
                .complete,
                title: isReview ? "验收" : "标记\(todo.completionVerb)",
                help: isReview ? "验收通过" : "标记\(todo.completionVerb)",
                icon: "checkmark"
            )
        )
        if todo.status != "open" {
            actions.append(
                item(
                    .returnToOpen,
                    title: "回到待办",
                    help: "回到待办",
                    icon: "arrow.uturn.backward"
                )
            )
        }
        return actions
    }

    static func primaryItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter(\.isPrimary)
    }

    /// Actions visible as toolbar icon chips: start (or restart) and
    /// complete/accept. These are the stable "常用" controls; everything else
    /// drops into the `···` overflow menu.
    static func inlineItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter { $0.action == .start || $0.action == .complete }
    }

    /// The rest of the lifecycle actions, inside the `···` overflow menu:
    /// returnToOpen.
    static func overflowItems(for todo: ATMTodo) -> [ATMTodoLifecycleItem] {
        items(for: todo).filter { $0.action != .start && $0.action != .complete }
    }

    /// Launch actions are for active work; review and closed states are human
    /// gates/history and must not silently restart implementation.
    static func showsLaunchPrompt(for todo: ATMTodo) -> Bool {
        ["open", "in_progress"].contains(todo.status)
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

    static func handoffTodo(id: String) -> [String] {
        ["todo", "handoff", id, "--json"]
    }

    /// Named for what it does rather than for a command that no longer exists:
    /// `todo prompt` was merged into `todo handoff --copy`, since both prepare the
    /// pointer without starting anything. --copy returns the text and opens no
    /// window.
    static func copyTodoPointer(id: String) -> [String] {
        ["todo", "handoff", id, "--copy", "--json"]
    }

    static func guardInstall(tool: String, bin: String) -> [String] {
        var argv = ["guard", "install", tool]
        if !bin.isEmpty { argv += ["--bin", bin] }
        return argv + ["--json"]
    }

    static func guardUninstall(tool: String) -> [String] {
        ["guard", "uninstall", tool, "--json"]
    }

    /// The rule itself travels on stdin, not argv: it is a nested object, and argv
    /// is the one place a value would end up in logs and process listings.
    static func guardRuleSet(tool: String) -> [String] {
        ["guard", "rule", "set", tool, "--json"]
    }

    static func guardRuleRemove(tool: String, ruleID: String) -> [String] {
        ["guard", "rule", "remove", tool, ruleID, "--json"]
    }

    static func guardToolForget(tool: String) -> [String] {
        ["guard", "forget", tool, "--json"]
    }

    /// Guard decisions intentionally remain on the human CLI adapter. `_ipc`
    /// is replayable and cannot prove its caller is the desktop App.
    static func guardDecision(id: String, approve: Bool) -> [String] {
        ["guard", approve ? "approve" : "deny", id, "--by", "panel", "--json"]
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
    /// Mirrors `owner_name` in ~/.atm/config.json: how to name the human when a
    /// todo they filed themselves is displayed. Empty falls back to 我.
    @Published private(set) var ownerName = ""
    /// Mirrors `todo_refine_on_add`. Default off, matching the CLI default:
    /// refining on add rewrote the card before anyone had looked at it, and a
    /// second bare pass on an already-structured card returns the same text —
    /// so the automatic one was in practice the only one. 优化 is an action on
    /// the Todo now; turn this on to also get it on every new todo.
    @Published private(set) var todoRefineOnAdd = false
    @Published private(set) var textModelBaseURL = "https://api.deepseek.com"
    @Published private(set) var textModelName = "deepseek-v4-flash"
    @Published private(set) var textModelSource = "deepseek"
    @Published private(set) var todoRefinePrompt = ""
    @Published private(set) var textModelAPIKeyConfigured = false
    @Published private(set) var isSavingTextModelSettings = false
    @Published private(set) var isTestingTextModelSettings = false
    @Published private(set) var textModelTestSuccessMessage: String?
    @Published var textModelSettingsErrorMessage: String?
    @Published private(set) var refiningTodoIDs: Set<String> = []
    @Published private(set) var refineErrorByTodoID: [String: String] = [:]
    /// Todos whose last refine pass returned `changed: false`. Without this the
    /// action looked broken: the CLI prints "already clear" and the App wrote
    /// nothing, so nothing on screen moved.
    @Published private(set) var refineUnchangedTodoIDs: Set<String> = []
    /// Internal refresh gate only. No view renders this flag, so publishing it
    /// needlessly rebuilt the desktop at both ends of every one-minute refresh.
    private(set) var isLoading = false
    @Published private(set) var isSyncing = false
    @Published private(set) var isActing = false
    @Published private(set) var archivedTodos: [ATMTodo] = []
    @Published private(set) var knowledgeCollections: [ATMKnowledgeCollection] = []
    @Published private(set) var isKnowledgeCatalogLoading = false
    @Published var knowledgeErrorMessage: String?
    private let makeKnowledgeIPCClient: @Sendable () throws -> ATMKnowledgeIPCClient
    private let makeMemoryIPCClient: @Sendable () throws -> ATMMemoryIPCClient
    private let makeSessionIPCClient: @Sendable () throws -> ATMSessionIPCClient
    private let makeTodoIPCClient: @Sendable () throws -> ATMTodoIPCClient
    @Published private(set) var progressByTodoID: [String: [ATMTodoProgressEntry]] = [:]
    @Published private(set) var loadingProgressTodoIDs: Set<String> = []
    @Published private(set) var boundSessionsByTodoID: [String: [ATMBoundSession]] = [:]
    @Published private(set) var loadingBoundSessionTodoIDs: Set<String> = []
    @Published private(set) var collectionOverview = ATMCollectionOverview.empty
    @Published private(set) var isCollecting = false
    @Published private(set) var collectingSourceIDs: Set<String> = []
    @Published private(set) var collectionSourceErrors: [String: String] = [:]
    @Published var collectionErrorMessage: String?
    @Published private(set) var agentHookReport: ATMAgentHookReport?
    @Published private(set) var isUpdatingAgentHooks = false
    @Published var agentHookErrorMessage: String?
    /// The durable session index, newest first, paged in. Live status only ever
    /// shows the sessions inside its activity window, so this is what makes an
    /// older session reachable at all.
    @Published private(set) var indexedSessions: [ATMIndexedSession] = []
    @Published private(set) var isLoadingIndexedSessions = false
    @Published private(set) var indexedSessionsReachedEnd = false
    @Published var indexedSessionsError: String?
    @Published private(set) var sessionTranscripts: [String: ATMSessionTranscript] = [:]
    @Published private(set) var sessionTimelines: [String: [ATMSessionTimelineEntry]] = [:]
    @Published private(set) var sessionReadErrors: [String: String] = [:]
    @Published private(set) var loadingSessionReads: Set<String> = []

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
    /// Whether a full dashboard has ever been applied. Gates `primeWork()`, whose
    /// work-only snapshot carries no statistics and so must never land on top of
    /// one that does.
    private var hasLoadedFullDashboard = false
    private var activeRefreshIncludesSync = false
    private var pendingRefresh = false
    private var pendingSync = false
    private var isCollectionRefreshing = false
    private var collectionReadUpdatesInFlight: Set<String> = []
    /// Outbound actions waiting on a decision. Published because the quick panel
    /// and the menu bar both count them.
    @Published var pendingApprovals: [ATMGuardApproval] = []
    @Published var approvalErrorMessage: String?
    /// Per-request rather than one global flag: with a shared flag, deciding one
    /// request would disable the buttons on every other row.
    private var approvalDecisionsInFlight: Set<String> = []
    /// nil until the first load, so launching with a pile of pending requests does
    /// not produce a pile of banners.
    private var notifiedApprovalIDs: Set<String>?
    @Published var guardTools: [ATMGuardTool] = []
    @Published var guardRules: [ATMGuardRule] = []
    @Published var guardConfigErrorMessage: String?
    /// Which tool the last failed change was about, so the error can be shown next
    /// to it. A message at the top of the pane, far from the button that was
    /// pressed, reads as "nothing happened".
    @Published var guardConfigErrorTool: String?
    @Published var isUpdatingGuardConfig = false
    private var guardRequestCancellable: AnyCancellable?
    private var isLiveStatusLoading = false
    private var lastCollectionAttemptAt: Date?
    private var notifiedCollectionRunIDs: Set<String>?
    /// Keep successfully deleted todos hidden until a dashboard read observes
    /// their absence. This prevents an older in-flight refresh from restoring a
    /// row after the CLI has already removed it.
    private var optimisticallyDeletedTodoIDs: Set<String> = []
    /// Keeps an older in-flight archive listing from resurrecting a row after the
    /// irreversible delete command has succeeded.
    private var optimisticallyPermanentlyDeletedTodoIDs: Set<String> = []
    private var optimisticallyUpdatedTodos: [String: ATMTodo] = [:]
    /// Prior id→status map for human-facing notifications. nil until first
    /// successful dashboard load (baseline, no historical flood).
    private var notifiedTodoStatus: [String: String]?

    init(
        makeKnowledgeIPCClient: @escaping @Sendable () throws -> ATMKnowledgeIPCClient = {
            try ATMKnowledgeIPCClient()
        },
        makeMemoryIPCClient: @escaping @Sendable () throws -> ATMMemoryIPCClient = {
            try ATMMemoryIPCClient()
        },
        makeSessionIPCClient: @escaping @Sendable () throws -> ATMSessionIPCClient = {
            try ATMSessionIPCClient()
        },
        makeTodoIPCClient: @escaping @Sendable () throws -> ATMTodoIPCClient = {
            try ATMTodoIPCClient()
        }
    ) {
        self.makeKnowledgeIPCClient = makeKnowledgeIPCClient
        self.makeMemoryIPCClient = makeMemoryIPCClient
        self.makeSessionIPCClient = makeSessionIPCClient
        self.makeTodoIPCClient = makeTodoIPCClient
    }

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
        hasLoadedFullDashboard = true
        dashboardState = state
    }

    /// Paints the task list from the cheap half of the dashboard.
    ///
    /// The complete dashboard snapshot also computes usage statistics, which on a real
    /// database is about a second of SQL aggregation that no task row reads —
    /// the todos themselves are ready in a few milliseconds. Asking for the work
    /// section first fills the window at launch rather than after the charts.
    ///
    /// Cold start only, and deliberately so. With nothing on screen yet the user
    /// has had nothing to act on, so none of the optimistic-edit bookkeeping that
    /// `refresh()` reconciles against can hold entries. That is what lets this
    /// path assign the todos straight through instead of duplicating a
    /// reconciliation that would then have two places to drift apart. It also
    /// leaves `notifiedTodoStatus` alone, so the full refresh still decides which
    /// status changes are worth a notification.
    private func primeWork() {
        Task {
            let outcome: ATMCommandOutcome<ATMDashboardEnvelope>
            do {
                let runner = try ATMCommandRunner()
                outcome = await decodeIPCCommand(
                    runner,
                    method: ATMDashboardIPCCommand.snapshot,
                    request: ATMDashboardRequest(
                        sections: ["work"],
                        sessionID: ATMAgentSessionContext.sessionID()
                    )
                )
            } catch {
                // The full refresh is already in flight and reports its own
                // failures. A broken fast path only costs the head start.
                return
            }
            guard let value = outcome.value else { return }
            // The full refresh runs concurrently and is authoritative. Once it has
            // landed, this thinner snapshot would blank the statistics it carries.
            guard !hasLoadedFullDashboard else { return }
            guard optimisticallyDeletedTodoIDs.isEmpty,
                  optimisticallyUpdatedTodos.isEmpty,
                  optimisticallyPermanentlyDeletedTodoIDs.isEmpty else { return }
            var next = dashboardState
            next.allTodos = value.todos
            next.snapshot = value.makeSnapshot()
            dashboardState = next
        }
    }

    func start() {
        guard timer == nil else { return }
        loadSettings()
        primeWork()
        refresh()
        refresh(sync: true)
        refreshCollection(runIfDue: true)
        refreshApprovals()
        timer = Timer.scheduledTimer(withTimeInterval: 60, repeats: true) { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                self.refresh(sync: ATMSyncPolicy.shouldSync(lastAttemptAt: self.lastSyncAttemptAt))
                self.refreshCollection(runIfDue: true)
                self.refreshApprovals()
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

    /// Starts the hook socket and refreshes as soon as an event lands.
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
        guardRequestCancellable = agentEvents.didReceiveGuardRequest
            .sink { [weak self] request in self?.applyGuardRequest(request) }
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

    /// Reads every setting the app shows in one `_ipc config.settings` call.
    ///
    /// This used to be four functions issuing eight `atm config get <key>` reads,
    /// four of them concurrently. They were replaced together rather than one at a
    /// time because the reason to merge them was never the spawn cost: it was that
    /// eight argv arrays are eight independent chances to drift from the CLI, and
    /// a drifted one shows up as a setting that silently reads back empty.
    ///
    /// Effective values, not file contents: an env override such as
    /// ATM_GROK_LIVE_QUOTA beats config.json, and a toggle has to show what the
    /// CLI will actually do.
    func loadSettings() {
        Task {
            guard let runner = try? ATMCommandRunner() else { return }
            do {
                let settings = try await ATMIPCClient(runner: runner).call(ATMIPCCommand.settings)
                applySettings(settings)
                textModelSettingsErrorMessage = nil
            } catch is CancellationError {
                return
            } catch let mismatch as ATMIPCProtocolMismatch {
                textModelSettingsErrorMessage = mismatch.summary
            } catch {
                textModelSettingsErrorMessage = ATMErrorText.compact(
                    error.localizedDescription,
                    limit: 180
                )
            }
        }
    }

    /// Publishes one settings snapshot. Shared by the read and the save so the two
    /// cannot disagree about which properties a snapshot updates — a field added to
    /// the payload and wired into only one of them would show up as a setting that
    /// sticks on reload but not on save.
    private func applySettings(_ settings: ATMSettingsSnapshot) {
        grokLiveQuotaEnabled = settings.grokLiveQuota
        ownerName = settings.ownerName
        todoRefineOnAdd = settings.todoRefineOnAdd
        textModelBaseURL = settings.textModelBaseURL
        textModelName = settings.textModelName
        textModelSource = settings.textModelSource
        todoRefinePrompt = settings.todoRefinePrompt
        textModelAPIKeyConfigured = settings.textModelAPIKeyConfigured
    }

    /// Persists the switch through the typed settings service and reverts the
    /// optimistic toggle if the write fails.
    func setTodoRefineOnAdd(_ enabled: Bool) {
        guard enabled != todoRefineOnAdd else { return }
        todoRefineOnAdd = enabled
        Task {
            do {
                let runner = try ATMCommandRunner()
                let saved = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.saveSettings,
                    request: ATMSettingsSave(todoRefineOnAdd: enabled)
                )
                applySettings(saved)
            } catch {
                todoRefineOnAdd = !enabled
                errorMessage = "任务整理设置未保存：\(ATMErrorText.compact(error.localizedDescription, limit: 160))"
            }
        }
    }

    func saveTextModelSettings(
        apiKey: String,
        baseURL: String,
        model: String,
        source: String,
        todoRefinePrompt: String
    ) {
        let trimmedBaseURL = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedModel = model.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedSource = source.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedPrompt = todoRefinePrompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedBaseURL.isEmpty, !trimmedModel.isEmpty, !trimmedSource.isEmpty,
              !isSavingTextModelSettings else { return }
        guard let endpoint = URL(string: trimmedBaseURL),
              endpoint.host != nil,
              endpoint.scheme == "http" || endpoint.scheme == "https" else {
            textModelSettingsErrorMessage = "Endpoint 必须是有效的 http 或 https URL"
            return
        }
        isSavingTextModelSettings = true
        textModelTestSuccessMessage = nil
        textModelSettingsErrorMessage = nil
        Task {
            defer { isSavingTextModelSettings = false }
            do {
                let trimmedAPIKey = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
                let runner = try ATMCommandRunner()
                let client = ATMIPCClient(runner: runner)
                if !trimmedAPIKey.isEmpty {
                    let credential = try await client.call(
                        ATMIPCCommand.saveCredential,
                        request: ATMCredentialSave(apiKey: trimmedAPIKey)
                    )
                    textModelAPIKeyConfigured = credential.configured
                }
                // One write, not four. As four sequential `config set` calls this
                // could not fail as a unit: a value the CLI rejected on the third
                // call left the first two already on disk, and the form then
                // reloaded half of what was typed. `config.save` validates every
                // field before writing any of them.
                let save = ATMSettingsSave(
                    textModelBaseURL: trimmedBaseURL,
                    textModelName: trimmedModel,
                    textModelSource: trimmedSource,
                    todoRefinePrompt: trimmedPrompt
                )
                // The answer is the state after the write, so the form redraws from
                // it instead of paying a second round trip — and what it draws is the
                // effective value, which an env override can differ from what was
                // just saved.
                let saved = try await client.call(
                    ATMIPCCommand.saveSettings,
                    request: save
                )
                applySettings(saved)
            } catch {
                textModelSettingsErrorMessage = "模型设置未保存：\(ATMErrorText.compact(error.localizedDescription, limit: 180))"
            }
        }
    }

    func clearTextModelAPIKey() {
        guard !isSavingTextModelSettings, !isTestingTextModelSettings else { return }
        isSavingTextModelSettings = true
        textModelTestSuccessMessage = nil
        textModelSettingsErrorMessage = nil
        Task {
            defer { isSavingTextModelSettings = false }
            do {
                let runner = try ATMCommandRunner()
                let status = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.deleteCredential
                )
                textModelAPIKeyConfigured = status.configured
            } catch {
                textModelSettingsErrorMessage = "API Key 未删除：\(ATMErrorText.compact(error.localizedDescription, limit: 180))"
            }
        }
    }

    func testTextModelSettings(apiKey: String, baseURL: String, model: String) {
        let trimmedBaseURL = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedModel = model.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedBaseURL.isEmpty, !trimmedModel.isEmpty,
              !isSavingTextModelSettings, !isTestingTextModelSettings else { return }
        guard let endpoint = URL(string: trimmedBaseURL),
              endpoint.host != nil,
              endpoint.scheme == "http" || endpoint.scheme == "https" else {
            textModelTestSuccessMessage = nil
            textModelSettingsErrorMessage = "Endpoint 必须是有效的 http 或 https URL"
            return
        }

        let draftKey = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !draftKey.isEmpty || textModelAPIKeyConfigured else {
            textModelTestSuccessMessage = nil
            textModelSettingsErrorMessage = "请先填写 DeepSeek API Key"
            return
        }

        isTestingTextModelSettings = true
        textModelTestSuccessMessage = nil
        textModelSettingsErrorMessage = nil
        Task {
            defer { isTestingTextModelSettings = false }
            do {
                let runner = try ATMCommandRunner()
                let result = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.checkTextModel,
                    request: ATMTextModelCheck(
                        apiKey: draftKey.isEmpty ? nil : draftKey,
                        baseURL: trimmedBaseURL,
                        model: trimmedModel
                    )
                )
                guard result.ok else {
                    throw ATMCommandError.failed(
                        arguments: ATMIPCCommand.checkTextModel.arguments,
                        status: 1,
                        message: "服务返回 ok=false"
                    )
                }
                textModelTestSuccessMessage = "连接成功 · \(trimmedModel) · \(result.latencyMS) ms"
            } catch {
                textModelSettingsErrorMessage = "连接失败：\(ATMErrorText.compact(error.localizedDescription, limit: 180))"
            }
        }
    }

    func clearTextModelTestResult() {
        textModelTestSuccessMessage = nil
    }

    func refineTodo(id: String, hint: String = "", automatic: Bool = false) {
        let trimmed = id.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !refiningTodoIDs.contains(trimmed) else { return }
        refiningTodoIDs.insert(trimmed)
        refineErrorByTodoID[trimmed] = nil
        refineUnchangedTodoIDs.remove(trimmed)
        Task {
            defer { refiningTodoIDs.remove(trimmed) }
            do {
                let result = try await makeTodoIPCClient().refine(ATMTodoRefineRequest(
                    todoID: trimmed,
                    allowSplit: true,
                    maxChildren: 5,
                    hint: hint,
                    dryRun: false
                ))
                if !result.changed {
                    refineUnchangedTodoIDs.insert(trimmed)
                }
                // Children and a rewritten title only exist after refine returns.
                refresh()
                loadProgress(for: trimmed)
            } catch {
                let message = ATMErrorText.compact(error.localizedDescription, limit: 180)
                refineErrorByTodoID[trimmed] = message
                if !automatic {
                    errorMessage = message
                }
            }
        }
    }

    func dismissRefineError(for id: String) {
        refineErrorByTodoID[id] = nil
    }

    func dismissRefineUnchanged(for id: String) {
        refineUnchangedTodoIDs.remove(id)
    }

    func setGrokLiveQuota(_ enabled: Bool) {
        guard enabled != grokLiveQuotaEnabled else { return }
        grokLiveQuotaEnabled = enabled
        Task {
            do {
                let runner = try ATMCommandRunner()
                let saved = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.saveSettings,
                    request: ATMSettingsSave(grokLiveQuota: enabled)
                )
                applySettings(saved)
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
            let client = ATMIPCClient(runner: runner)
            var status: ATMCollectionOverview?
            do {
                status = try await client.call(
                    ATMCollectionIPCCommand.snapshot,
                    request: ATMCollectionSnapshotRequest(itemLimit: 200)
                )
                if let status { notifyCollectionRuns(status.runs) }
                if let currentStatus = status,
                   runIfDue,
                   shouldRunCollection(currentStatus),
                   currentStatus.summary.enabledSources > 0 {
                    isCollecting = true
                    lastCollectionAttemptAt = Date()
                    defer { isCollecting = false }
                    _ = try await client.call(
                        ATMCollectionIPCCommand.run,
                        request: ATMCollectionRunRequest(sourceID: nil, dueOnly: true)
                    )
                    status = try await client.call(
                        ATMCollectionIPCCommand.snapshot,
                        request: ATMCollectionSnapshotRequest(itemLimit: 200)
                    )
                    if let status { notifyCollectionRuns(status.runs) }
                    refresh()
                }
                if let status {
                    collectionOverview = status
                    collectionErrorMessage = ATMCollectionWorkspaceNotice.banner(for: status)
                }
            } catch {
                // Deliberately not bannering the exit code. `collect run --due` fails
                // if any one source failed, and the latest run is the latest across
                // *all* sources — so one source's hiccup used to raise a card over the
                // whole workspace. The refreshed health below decides instead.
                if let recovered = try? await client.call(
                    ATMCollectionIPCCommand.snapshot,
                    request: ATMCollectionSnapshotRequest(itemLimit: 200)
                ) {
                    collectionOverview = recovered
                    notifyCollectionRuns(recovered.runs)
                    collectionErrorMessage = ATMCollectionWorkspaceNotice.banner(for: recovered)
                } else {
                    // Status itself is unreadable, which is a real problem and not a
                    // connector's flakiness.
                    collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
                }
            }
        }
    }

    func isCollecting(sourceID: String) -> Bool {
        collectingSourceIDs.contains(sourceID)
            || collectionOverview.latestRun(for: sourceID)?.status == "running"
    }

    func collectionError(for sourceID: String) -> String? {
        collectionSourceErrors[sourceID]
            ?? collectionOverview.latestRun(for: sourceID).flatMap { run in
                run.status == "failed" ? run.error : nil
            }
    }

    /// The Collection workspace operates one source at a time. Besides making
    /// the action's scope visible to the user, `--source` prevents an unrelated
    /// disabled, slow, or unhealthy connector from changing this source's run.
    func runCollectionNow(source: ATMCollectionSource) {
        guard source.enabled, !isCollecting else { return }
        notifyCollectionRuns(collectionOverview.runs)
        isCollecting = true
        collectingSourceIDs.insert(source.id)
        collectionSourceErrors[source.id] = nil
        collectionErrorMessage = nil
        lastCollectionAttemptAt = Date()
        Task {
            defer {
                collectingSourceIDs.remove(source.id)
                isCollecting = false
            }
            do {
                let runner = try ATMCommandRunner()
                let client = ATMIPCClient(runner: runner)
                _ = try await client.call(
                    ATMCollectionIPCCommand.run,
                    request: ATMCollectionRunRequest(sourceID: source.id, dueOnly: false)
                )
                collectionOverview = try await client.call(
                    ATMCollectionIPCCommand.snapshot,
                    request: ATMCollectionSnapshotRequest(itemLimit: 200)
                )
                collectionSourceErrors[source.id] = nil
                notifyCollectionRuns(collectionOverview.runs)
                refresh()
            } catch {
                let message = ATMErrorText.compact(error.localizedDescription, limit: 240)
                collectionSourceErrors[source.id] = message
                collectionErrorMessage = message
                refreshCollection()
            }
        }
    }

    func setCollectionEnabled(_ enabled: Bool) {
        guard enabled != collectionOverview.enabled else { return }
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.saveSettings,
                    request: ATMSettingsSave(collectionEnabled: enabled)
                )
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
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMIPCCommand.saveSettings,
                    request: ATMSettingsSave(collectionIntervalMinutes: value)
                )
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
                let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedProject = project.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedExclude = excludePattern.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedInstruction = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
                let trimmedKnowledge = knowledgeCollection.trimmingCharacters(in: .whitespacesAndNewlines)
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.saveSource,
                    request: ATMCollectionSourceSaveRequest(
                        connector: connectorID,
                        kind: target.kind,
                        externalID: target.value,
                        name: trimmedName,
                        project: trimmedProject,
                        excludePattern: trimmedExclude,
                        instruction: trimmedInstruction,
                        knowledgeCollection: trimmedKnowledge,
                        strategy: strategy,
                        decisionUnit: decisionUnit,
                        intervalMinutes: intervalMinutes,
                        priority: priority,
                        enabled: enabled
                    )
                )
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
            let result = try await ATMIPCClient(runner: runner).call(
                ATMCollectionIPCCommand.searchSources,
                request: ATMCollectionSourceSearchRequest(
                    connector: connectorID, kind: kind, keyword: trimmed, limit: 10
                )
            )
            return (result.candidates, nil)
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
            let result = try await ATMIPCClient(runner: runner).call(
                ATMCollectionIPCCommand.history,
                request: ATMCollectionHistoryRequest(
                    sourceID: source.id, limit: limit, local: local
                )
            )
            return (result, nil)
        } catch {
            return (nil, ATMErrorText.compact(error.localizedDescription, limit: 180))
        }
    }

    func setCollectionSource(_ source: ATMCollectionSource, enabled: Bool) {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.setSourceEnabled,
                    request: ATMCollectionSourceEnabledRequest(sourceID: source.id, enabled: enabled)
                )
                collectionSourceErrors[source.id] = nil
                refreshCollection()
            } catch {
                let message = ATMErrorText.compact(error.localizedDescription, limit: 200)
                collectionSourceErrors[source.id] = message
                collectionErrorMessage = message
            }
        }
    }

    /// Takes one source in or out of the desktop notifications. Separate from the
    /// enabled toggle above because it answers a different question: pausing stops
    /// the collecting, muting only stops the banner — the results still arrive and
    /// still count as unread.
    func setCollectionSource(_ source: ATMCollectionSource, muted: Bool) {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.setSourceMuted,
                    request: ATMCollectionSourceMutedRequest(sourceID: source.id, muted: muted)
                )
                collectionSourceErrors[source.id] = nil
                refreshCollection()
            } catch {
                let message = ATMErrorText.compact(error.localizedDescription, limit: 200)
                collectionSourceErrors[source.id] = message
                collectionErrorMessage = message
            }
        }
    }

    func deleteCollectionSource(_ source: ATMCollectionSource) {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.deleteSource,
                    request: ATMCollectionSourceDeleteRequest(sourceID: source.id, confirmed: true)
                )
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    func reprocessCollectionItem(_ item: ATMCollectionItem) {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.reprocessItem,
                request: ATMCollectionItemIDRequest(itemID: item.id)
            )
        }
    }

    /// Insight classification stops at the conclusion. This explicit action is
    /// the only Collection-workspace path that creates central knowledge.
    func saveCollectionItemToKnowledge(_ item: ATMCollectionItem) {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.saveConclusion,
                request: ATMCollectionSaveConclusionRequest(itemID: item.id, collection: nil)
            )
        }
    }

    func promoteCollectionItem(
        _ item: ATMCollectionItem,
        title: String? = nil,
        project: String? = nil,
        priority: String? = nil
    ) {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.promoteItem,
                request: ATMCollectionPromoteRequest(
                    itemID: item.id,
                    correction: ATMCollectionItemCorrectionRequest(
                        title: title, project: project, priority: priority
                    )
                )
            )
        }
    }

    func correctCollectionItem(
        _ item: ATMCollectionItem,
        title: String,
        project: String,
        priority: String
    ) {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.correctItem,
                request: ATMCollectionCorrectRequest(
                    itemID: item.id,
                    correction: ATMCollectionItemCorrectionRequest(
                        title: title, project: project, priority: priority
                    )
                )
            )
        }
    }

    func revertCollectionItem(_ item: ATMCollectionItem) {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.revertItem,
                request: ATMCollectionRevertRequest(itemID: item.id, confirmed: true)
            )
        }
    }

    func setCollectionItemsRead(_ items: [ATMCollectionItem], read: Bool) {
        let ids = Array(Set(items.map(\.id))).sorted()
        guard !ids.isEmpty else { return }
        let pending = Set(ids)
        guard collectionReadUpdatesInFlight.isDisjoint(with: pending) else { return }
        collectionReadUpdatesInFlight.formUnion(pending)
        Task {
            defer { collectionReadUpdatesInFlight.subtract(pending) }
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.setItemsRead,
                    request: ATMCollectionItemsReadRequest(itemIDs: ids, all: false, read: read)
                )
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    /// Manually settles or reopens processing records without deleting their
    /// audit trail, changing linked Todos, or making their messages collectible
    /// again. A create row and its folded supplements are passed together.
    func setCollectionItemsArchived(_ items: [ATMCollectionItem], archived: Bool) {
        let ids = Array(Set(items.map(\.id))).sorted()
        guard !ids.isEmpty else { return }
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.setItemsArchived,
                request: ATMCollectionItemsArchivedRequest(itemIDs: ids, all: false, archived: archived)
            )
        }
    }

    /// Settles every conclusion already read and not saved to knowledge, in one
    /// call. This is the bulk counterpart to 「了结记录」, and it exists because that
    /// per-record action was never going to be pressed sixty times: the class it
    /// clears is the only one with no lifecycle of its own, so before this the
    /// realistic way to shrink the list was deleting rows — which took the audit
    /// trail with them. Scope lives in Go; the App only asks.
    func settleReadCollectionConclusions() {
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.setItemsArchived,
                request: ATMCollectionItemsArchivedRequest(itemIDs: [], all: true, archived: true)
            )
        }
    }

    /// Reads the pending list and reconciles banners against it.
    ///
    /// The socket push is what makes a banner appear promptly; this is what makes
    /// the list correct — including retiring a banner for a request that was
    /// decided in a terminal or quietly expired, which nothing pushes.
    func refreshApprovals() {
        Task {
            do {
                let runner = try ATMCommandRunner()
                let response = try await ATMIPCClient(runner: runner).call(
                    ATMGuardIPCCommand.list,
                    request: ATMGuardListRequest(status: "pending", limit: 50)
                )
                let approvals = response.approvals
                pendingApprovals = approvals
                approvalErrorMessage = nil
                reconcileApprovalBanners(approvals)
            } catch is CancellationError {
                return
            } catch {
                // A gate that was never installed has no table to read, and that is
                // not an error worth showing on every tick.
                approvalErrorMessage = nil
            }
        }
    }

    private func reconcileApprovalBanners(_ approvals: [ATMGuardApproval]) {
        let (diff, notified) = ATMGuardApprovalNotifyDiff.next(
            notified: notifiedApprovalIDs, approvals: approvals)
        notifiedApprovalIDs = notified
        for approval in diff.post {
            ATMNotificationManager.shared.sendGuardApproval(
                ATMGuardApprovalPayload.make(approval: approval), approvalID: approval.id)
        }
        for id in diff.withdraw {
            ATMNotificationManager.shared.withdrawGuardApproval(approvalID: id)
        }
        // The window is the decision surface; the banner is a record that survives
        // dismissing it. Both are published so whoever presents the window can react
        // without polling the store itself.
        approvalArrivals.send((arrived: diff.post, pending: approvals.filter(\.isPending)))
    }

    /// Newly arrived and currently pending requests, for the presenter that owns the
    /// approval window.
    let approvalArrivals = PassthroughSubject<
        (arrived: [ATMGuardApproval], pending: [ATMGuardApproval]), Never
    >()

    /// Raises a banner the moment the CLI reports a new request, instead of waiting
    /// out the poll. The list is refreshed straight after so the panel agrees.
    func applyGuardRequest(_ request: ATMGuardRequest) {
        if notifiedApprovalIDs?.contains(request.id) != true {
            ATMNotificationManager.shared.sendGuardApproval(
                ATMGuardApprovalPayload.make(request: request), approvalID: request.id)
            notifiedApprovalIDs = (notifiedApprovalIDs ?? []).union([request.id])
        }
        // The refresh that follows carries the full row, which is what the window
        // renders; the push only tells us to look now instead of at the next tick.
        refreshApprovals()
    }

    /// Approve runs the command for real. Deny records the refusal, which is also
    /// what answers a retrying agent immediately instead of re-raising the request.
    func decideApproval(_ approval: ATMGuardApproval, approve: Bool) {
        decideApproval(id: approval.id, approve: approve)
    }

    func decideApproval(id: String, approve: Bool) {
        guard !approvalDecisionsInFlight.contains(id) else { return }
        approvalDecisionsInFlight.insert(id)
        // Pull the banner immediately: its buttons are now answered, and leaving a
        // live one up invites a second press that would fail confusingly.
        ATMNotificationManager.shared.withdrawGuardApproval(approvalID: id)
        Task {
            defer { approvalDecisionsInFlight.remove(id) }
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(ATMCommandBuilder.guardDecision(id: id, approve: approve))
                approvalErrorMessage = nil
            } catch {
                // "Approved but the send failed" has to be visible: the CLI exits
                // non-zero and says which, and swallowing that would report success
                // for a message that never went out.
                approvalErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
            }
            refreshApprovals()
        }
    }

    /// Reads which CLIs are gated and what rules they carry. Both come from the
    /// same Guard service the CLI uses, so the settings pane and
    /// `atm guard status` can never disagree.
    func loadGuardConfiguration() {
        Task {
            do {
                let client = try ATMIPCClient()
                async let tools = client.call(
                    ATMGuardIPCCommand.status, request: ATMGuardStatusRequest()
                )
                async let rules = client.call(
                    ATMGuardIPCCommand.ruleList, request: ATMGuardRuleListRequest()
                )
                guardTools = try await tools.states
                guardRules = try await rules.rules
                guardConfigErrorMessage = nil
            } catch is CancellationError {
                return
            } catch {
                guardConfigErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 240)
            }
        }
    }

    /// Installing moves the CLI's own binary aside, so it is a real change to the
    /// machine and its outcome is always re-read rather than assumed.
    func installGuardTool(_ tool: String, bin: String) {
        runGuardConfigChange(ATMCommandBuilder.guardInstall(tool: tool, bin: bin), tool: tool)
    }

    func uninstallGuardTool(_ tool: String) {
        runGuardConfigChange(ATMCommandBuilder.guardUninstall(tool: tool), tool: tool)
    }

    func saveGuardRule(_ draft: ATMGuardRuleDraft) {
        do {
            let tool = draft.tool.trimmingCharacters(in: .whitespaces)
            runGuardConfigChange(
                ATMCommandBuilder.guardRuleSet(tool: tool),
                tool: tool,
                standardInput: try draft.jsonPayload())
        } catch {
            guardConfigErrorMessage = error.localizedDescription
        }
    }

    /// Switching a rule off sends only its id and the flag, so a built-in's matcher
    /// is never restated — a restated copy could drift from the real one and quietly
    /// stop gating.
    func setGuardRuleEnabled(_ rule: ATMGuardRule, enabled: Bool) {
        do {
            runGuardConfigChange(
                ATMCommandBuilder.guardRuleSet(tool: rule.tool),
                tool: rule.tool,
                standardInput: try ATMGuardRuleDraft.togglePayload(ruleID: rule.ruleID, enabled: enabled))
        } catch {
            guardConfigErrorMessage = error.localizedDescription
        }
    }

    func removeGuardRule(_ rule: ATMGuardRule) {
        runGuardConfigChange(
            ATMCommandBuilder.guardRuleRemove(tool: rule.tool, ruleID: rule.ruleID), tool: rule.tool)
    }

    func forgetGuardTool(_ tool: String) {
        runGuardConfigChange(ATMCommandBuilder.guardToolForget(tool: tool), tool: tool)
    }

    /// Which tool is mid-change, so its own card can show that something is
    /// happening instead of a spinner elsewhere on the page.
    @Published var guardToolInFlight: String?

    private func runGuardConfigChange(
        _ arguments: [String], tool: String? = nil, standardInput: Data? = nil
    ) {
        guard !isUpdatingGuardConfig else { return }
        isUpdatingGuardConfig = true
        guardToolInFlight = tool
        guardConfigErrorMessage = nil
        guardConfigErrorTool = nil
        Task {
            defer {
                isUpdatingGuardConfig = false
                guardToolInFlight = nil
            }
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(arguments, standardInput: standardInput)
                guardConfigErrorMessage = nil
                guardConfigErrorTool = nil
            } catch {
                guardConfigErrorTool = tool
                // The CLI refuses several things on purpose — deleting a built-in,
                // forgetting a tool whose shim is still installed — and its wording
                // says what to do instead, so it is surfaced rather than summarised.
                guardConfigErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 400)
            }
            loadGuardConfiguration()
        }
    }

    func isDecidingApproval(_ id: String) -> Bool {
        approvalDecisionsInFlight.contains(id)
    }

    func markAllCollectionItemsRead() {
        Task {
            do {
                let runner = try ATMCommandRunner()
                _ = try await ATMIPCClient(runner: runner).call(
                    ATMCollectionIPCCommand.setItemsRead,
                    request: ATMCollectionItemsReadRequest(itemIDs: [], all: true, read: true)
                )
                refreshCollection()
            } catch {
                collectionErrorMessage = ATMErrorText.compact(error.localizedDescription, limit: 200)
            }
        }
    }

    /// Removes one processing record. `--yes` because the desktop already asked;
    /// the Todo this record wrote stays, so a clean-up here never quietly takes
    /// work with it.
    func deleteCollectionItem(_ item: ATMCollectionItem) {
        deleteCollectionItems([item])
    }

    /// Clears a whole group in one CLI call rather than one per record: a group
    /// runs to dozens of rows, and the command deletes them as a single
    /// transaction — a half-cleared group is indistinguishable on screen from one
    /// that was never cleared.
    func deleteCollectionItems(_ items: [ATMCollectionItem]) {
        guard !items.isEmpty else { return }
        let ids = Array(Set(items.map(\.id))).sorted()
        runCollectionItemAction { client in
            _ = try await client.call(
                ATMCollectionIPCCommand.deleteItems,
                request: ATMCollectionItemsDeleteRequest(itemIDs: ids, confirmed: true)
            )
        }
    }

    private func runCollectionItemAction(
        _ operation: @escaping (ATMIPCClient) async throws -> Void
    ) {
        guard !isCollecting else { return }
        isCollecting = true
        collectionErrorMessage = nil
        Task {
            defer { isCollecting = false }
            do {
                let runner = try ATMCommandRunner()
                try await operation(ATMIPCClient(runner: runner))
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
        ATMNotificationManager.shared.sendCollectionResults(
            newRuns,
            items: collectionOverview.items,
            sources: collectionOverview.sources
        )
    }

    private func shouldRunCollection(_ status: ATMCollectionOverview, now: Date = Date()) -> Bool {
        ATMCollectionSchedulePolicy.shouldRun(
            status,
            lastAttemptAt: lastCollectionAttemptAt,
            now: now
        )
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

                let dashboardRequestStartedAt = Date()
                // Quota lives in the agents' own logs rather than the session
                // index, so it is a separate command. Run it concurrently with
                // the dashboard so the extra read costs no wall-clock time.
                async let dashboardTask: ATMCommandOutcome<ATMDashboardEnvelope> = decodeIPCCommand(
                    runner,
                    method: ATMDashboardIPCCommand.snapshot,
                    request: ATMDashboardRequest(
                        sessionID: ATMAgentSessionContext.sessionID()
                    )
                )
                async let quotaTask: ATMCommandOutcome<ATMQuotaSnapshot> = decodeIPCCommand(
                    runner,
                    method: ATMIPCCommand.quota,
                    request: ATMQuotaRequest(agent: nil)
                )
                async let archiveTask: ATMCommandOutcome<[ATMTodo]> = decodeIPCCommand(
                    runner,
                    method: ATMTodoIPCCommand.list,
                    request: ATMTodoListRequest(status: "archived")
                )
                let (dashboard, quotaOutcome, archiveOutcome) = await (
                    dashboardTask,
                    quotaTask,
                    archiveTask
                )

                var nextState = dashboardState
                if let error = dashboard.error {
                    warnings.append("仪表盘：\(error)")
                }
                if let error = quotaOutcome.error {
                    warnings.append("配额：\(error)")
                }
                if let error = archiveOutcome.error {
                    warnings.append("归档：\(error)")
                }
                if let value = quotaOutcome.value {
                    nextState.quota = value
                }
                if let value = archiveOutcome.value {
                    let permanentlyDeletedIDs = optimisticallyPermanentlyDeletedTodoIDs
                    let restoringIDs = Set(optimisticallyUpdatedTodos.keys)
                    var incomingArchive = value.filter {
                        !permanentlyDeletedIDs.contains($0.id) && !restoringIDs.contains($0.id)
                    }
                    // A listing that started before `todo archive` may not include
                    // the newly moved row. Preserve the optimistic copy until the
                    // dashboard has observed that it left the working set.
                    incomingArchive.append(contentsOf: archivedTodos.filter { optimistic in
                        optimisticallyDeletedTodoIDs.contains(optimistic.id)
                            && !incomingArchive.contains(where: { $0.id == optimistic.id })
                    })
                    archivedTodos = incomingArchive
                    optimisticallyPermanentlyDeletedTodoIDs.formIntersection(
                        Set(value.map(\.id))
                    )
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
                    snapshot = snapshot.replacingLiveStatus(
                        ATMLiveStatusRefreshPolicy.mergedLiveStatus(
                            dashboard: snapshot.liveStatus,
                            fast: dashboardState.snapshot.liveStatus,
                            preserveFast: ATMLiveStatusRefreshPolicy.shouldPreserveFastStatus(
                                lastAppliedAt: lastLiveStatusAppliedAt,
                                dashboardRequestStartedAt: dashboardRequestStartedAt
                            ),
                            signals: agentEvents.signals
                        )
                    )
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

    func dismissDashboardError() {
        errorMessage = nil
    }

    func perform(_ action: ATMTodoAction, on todo: ATMTodo) {
        guard !isActing else { return }
        isActing = true
        errorMessage = nil
        Task {
            defer { isActing = false }
            do {
                let client = try makeTodoIPCClient()
                switch action {
                case .start:
                    _ = try await client.start(todo.id)
                case .complete:
                    _ = try await client.done(
                        todo.id,
                        reason: "通过 ATM 菜单栏\(todo.completionVerb)"
                    )
                case .archive:
                    _ = try await client.archive(todo.id)
                case .restore:
                    _ = try await client.restore(todo.id)
                case .delete:
                    // Already behind the desktop confirmation dialog; the request
                    // carries that confirmation explicitly.
                    _ = try await client.delete(todo.id)
                case .returnToOpen:
                    _ = try await client.update(
                        ATMTodoUpdateRequest(todoID: todo.id, status: "open")
                    )
                }
                applySuccessfulTodoAction(action, on: todo)
                switch action {
                case .complete:
                    ATMNotificationManager.shared.sendTodoCompleted(todo)
                default:
                    break
                }
                refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    /// The CLI mutation is authoritative, so removal should update the working
    /// set and archive immediately while a queued refresh reconciles other fields.
    func applySuccessfulTodoAction(_ action: ATMTodoAction, on todo: ATMTodo) {
        if action == .archive {
            let deletedIDs: Set<String> = [todo.id]
            optimisticallyDeletedTodoIDs.insert(todo.id)
            optimisticallyUpdatedTodos.removeValue(forKey: todo.id)
            updateDashboardState {
                $0.allTodos.removeAll { $0.id == todo.id }
                $0.snapshot = $0.snapshot.removingTodos(withIDs: deletedIDs)
            }
            progressByTodoID.removeValue(forKey: todo.id)
            if !archivedTodos.contains(where: { $0.id == todo.id }) {
                archivedTodos.insert(todo, at: 0)
            }
            return
        }

        if action == .restore {
            archivedTodos.removeAll { $0.id == todo.id }
            optimisticallyDeletedTodoIDs.remove(todo.id)
            optimisticallyUpdatedTodos[todo.id] = todo
            updateDashboardState {
                if !$0.allTodos.contains(where: { $0.id == todo.id }) {
                    $0.allTodos.append(todo)
                }
                $0.snapshot = $0.snapshot.replacingTodo(todo)
            }
            return
        }

        if action == .delete {
            archivedTodos.removeAll { $0.id == todo.id }
            let deletedIDs: Set<String> = [todo.id]
            optimisticallyDeletedTodoIDs.insert(todo.id)
            optimisticallyPermanentlyDeletedTodoIDs.insert(todo.id)
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
        case .returnToOpen:
            updated = todo.replacingLifecycle(status: "open")
        case .archive, .restore, .delete:
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
			defer {
				isActing = false
				draft.cleanupTemporaryImages()
            }
            do {
                let created = try await makeTodoIPCClient().create(
                    ATMTodoCreateRequest(draft: draft)
                )
                let createdID = created.id
                // Prefer the decoded todo so selection can resolve before the next
                // full dashboard refresh lands.
                if !allTodos.contains(where: { $0.id == created.id }) {
                    updateDashboardState { $0.allTodos.append(created) }
                    // Human just filed this in the UI — do not also banner "新建".
                    var seen = notifiedTodoStatus ?? [:]
                    seen[created.id] = created.status
                    notifiedTodoStatus = seen
                } else {
                    var seen = notifiedTodoStatus ?? [:]
                    seen[createdID] = "open"
                    notifiedTodoStatus = seen
                }
                onCreated?(createdID)
                if todoRefineOnAdd {
                    refineTodo(id: createdID, automatic: true)
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
                let normalized = ATMTodoEdit(
                    title: trimmedTitle,
                    description: edit.description.trimmingCharacters(in: .whitespacesAndNewlines),
                    priority: edit.priority,
                    project: edit.project.trimmingCharacters(in: .whitespacesAndNewlines),
                    status: edit.status,
                    wakeCondition: edit.status == "in_progress"
                        ? edit.wakeCondition.trimmingCharacters(in: .whitespacesAndNewlines) : "",
                    reviewAt: edit.status == "in_progress"
                        ? edit.reviewAt.trimmingCharacters(in: .whitespacesAndNewlines) : "",
                    source: edit.source.trimmingCharacters(in: .whitespacesAndNewlines)
                )
                _ = try await makeTodoIPCClient().update(
                    ATMTodoUpdateRequest(todoID: todo.id, edit: normalized)
                )
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
                let detail = try await makeTodoIPCClient().show(todo.id)
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
                let doc = try await makeTodoIPCClient().document(todoID)
                let content = doc.content ?? ""
                progressByTodoID[todoID] = ATMTodoProgressEntry.parse(from: content)
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
                let detail = try await makeTodoIPCClient().show(todoID)
                boundSessionsByTodoID[todoID] = detail.sessions ?? []
            } catch {
                boundSessionsByTodoID[todoID] = []
            }
        }
    }

    /// Opens the Todo in Codex Desktop and stops there.
    ///
    /// Deliberately changes nothing locally: `todo handoff` claims no run and
    /// does not start the Todo, because the human has not pressed Enter yet.
    /// Work is recorded when the Agent runs `atm session bind`, which the next
    /// refresh picks up like any other externally-started session.
    func handoffTodo(_ todo: ATMTodo) {
        guard !isActing else { return }
        isActing = true
        errorMessage = nil
        Task {
            defer { isActing = false }
            do {
                let runner = try ATMCommandRunner()
                _ = try await runner.run(ATMCommandBuilder.handoffTodo(id: todo.id))
            } catch {
                errorMessage = "在 Codex 里打开失败：\(ATMErrorText.compact(error.localizedDescription, limit: 180))"
            }
        }
    }

    /// Fetches the line a human pastes into a fresh agent session. The CLI owns
    /// the wording, so the app cannot drift from `atm todo handoff --copy`; the
    /// caller writes it to the pasteboard, which is where every other copy
    /// action in this app puts its result.
    func launchPrompt(for todo: ATMTodo) async -> String? {
        errorMessage = nil
        do {
            let runner = try ATMCommandRunner()
            let data = try await runner.run(ATMCommandBuilder.copyTodoPointer(id: todo.id))
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
                knowledgeCollections = try await makeKnowledgeIPCClient().catalog()
            } catch {
                knowledgeErrorMessage = error.localizedDescription
            }
        }
    }

    func knowledgeDocuments(collectionID: String, status: String = "active") async throws -> [ATMKnowledgeDocumentSummary] {
        let decoded = try await makeKnowledgeIPCClient().query(ATMKnowledgeQueryRequest(
            text: nil,
            collection: collectionID,
            status: status,
            sessionID: nil,
            limit: nil
        ))
        var seen = Set<String>()
        return decoded.filter { seen.insert($0.documentID).inserted }
    }

    /// Archived documents remain attached to their original collection. The
    /// desktop app presents them through one synthetic library, so fetch the
    /// unscoped list once and aggregate by status rather than querying every
    /// collection separately.
    func archivedKnowledgeDocuments() async throws -> [ATMKnowledgeDocumentSummary] {
        let decoded = try await makeKnowledgeIPCClient().query(ATMKnowledgeQueryRequest(
            text: nil,
            collection: nil,
            status: "archived",
            sessionID: nil,
            limit: nil
        ))
        var seen = Set<String>()
        return decoded.filter { seen.insert($0.documentID).inserted }
    }

    func createCollection(id: String, name: String) async throws {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        try await makeKnowledgeIPCClient().saveCollection(.create(id: id, name: trimmedName))
    }

    func renameCollectionName(id: String, name: String) async throws {
        try await makeKnowledgeIPCClient().saveCollection(.update(id: id, name: name))
    }

    func deleteCollection(id: String, force: Bool, moveTo: String?, confirmed: Bool) async throws {
        try await makeKnowledgeIPCClient().deleteCollection(
            id: id,
            force: force,
            moveTo: moveTo?.isEmpty == false ? moveTo : nil,
            confirmed: confirmed
        )
    }

    func searchKnowledge(_ query: String, status: String = "active") async throws -> [ATMKnowledgeDocumentSummary] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        let decoded = try await makeKnowledgeIPCClient().query(ATMKnowledgeQueryRequest(
            text: trimmedQuery,
            collection: nil,
            status: status,
            sessionID: currentSessionID,
            limit: 200
        ))
        var seen = Set<String>()
        return decoded.filter { seen.insert($0.documentID).inserted }
    }

    func searchTodos(_ query: String) async throws -> [ATMTodo] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        return try await makeTodoIPCClient().list(
            ATMTodoListRequest(status: "all", query: trimmedQuery)
        )
    }

    func searchSessions(_ query: String) async throws -> [ATMSessionSearchHit] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedQuery.isEmpty else { return [] }
        // The palette dedupes message hits down to sessions, so it needs a wider
        // page than the terminal-oriented default.
        let decoded = try await makeSessionIPCClient().search(ATMSessionSearchRequest(
            keyword: trimmedQuery,
            limit: 200
        ))
        var seen = Set<String>()
        return decoded.matches.filter { seen.insert($0.shortID).inserted }
    }

    func knowledgeDocument(_ documentID: String) async throws -> ATMKnowledgeDocument {
        try await makeKnowledgeIPCClient().document(documentID)
    }

    func updateKnowledgeDocument(_ documentID: String, content: String) async throws -> ATMKnowledgeDocument {
        try await makeKnowledgeIPCClient().saveDocument(
            .content(documentID: documentID, content: content)
        )
    }

    func addKnowledgeDocument(_ draft: ATMKnowledgeDraft) async throws -> ATMKnowledgeDocument {
        try await makeKnowledgeIPCClient().saveDocument(.create(draft))
    }

    func importKnowledge(at url: URL, collectionID: String) async throws -> [ATMKnowledgeDocument] {
        try await makeKnowledgeIPCClient().importDocument(path: url.path, collection: collectionID)
    }

    func editKnowledgeDocument(_ documentID: String, edit: ATMKnowledgeMetadataEdit) async throws -> ATMKnowledgeDocument {
        try await makeKnowledgeIPCClient().saveDocument(.metadata(
            documentID: documentID,
            title: edit.title,
            collection: edit.collection,
            status: edit.status,
            domains: edit.domains,
            tags: edit.tags,
            projects: edit.projects
        ))
    }

    func moveKnowledgeDocument(_ documentID: String, to collectionID: String) async throws -> ATMKnowledgeDocument {
        try await makeKnowledgeIPCClient().saveDocument(.metadata(
            documentID: documentID,
            collection: collectionID
        ))
    }

    func deleteKnowledgeDocument(_ documentID: String, confirmed: Bool) async throws {
        try await makeKnowledgeIPCClient().deleteDocument(documentID, confirmed: confirmed)
    }

    func knowledgeGovernance(staleDays: Int = 180) async throws -> ATMKnowledgeGovernance {
        try await makeKnowledgeIPCClient().governance(staleDays: staleDays)
    }

    func recordKnowledgeFeedback(
        documentID: String,
        outcome: String,
        note: String
    ) async throws {
        guard let currentSessionID else {
            throw ATMKnowledgeFeedbackError.noBoundSession
        }
        let trimmedNote = note.trimmingCharacters(in: .whitespacesAndNewlines)
        try await makeKnowledgeIPCClient().feedback(ATMKnowledgeFeedbackRequest(
            documentID: documentID,
            sessionID: currentSessionID,
            outcome: outcome,
            note: trimmedNote.isEmpty ? nil : trimmedNote
        ))
    }

    func doctor() async throws -> ATMDoctorReport {
        try await ATMIPCClient().call(ATMIPCCommand.doctor)
    }

    func memories(query: String) async throws -> [ATMMemoryHit] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return try await makeMemoryIPCClient().recall(ATMMemoryRecallRequest(
            query: trimmedQuery.isEmpty ? nil : trimmedQuery,
            scope: nil,
            limit: 200
        ))
    }

    func supersedeMemory(_ memory: ATMMemoryHit, content: String) async throws {
        let source = memory.metadata["source"]?.trimmingCharacters(in: .whitespacesAndNewlines)
        try await makeMemoryIPCClient().supersede(ATMMemorySupersedeRequest(
            targetID: memory.id,
            scope: memory.scope,
            content: content,
            tags: memory.tags,
            source: source?.isEmpty == false ? source : nil
        ))
    }

    func sessionTranscript(_ sessionID: String) async throws -> String {
        try await makeSessionIPCClient().show(
            ATMSessionShowRequest(sessionID: sessionID)
        ).plainText
    }

    // MARK: - Session index

    /// One page of sessions. Matches the cap the other browse surfaces use: the
    /// point is that every session is reachable, not that a thousand rows render
    /// at once.
    static let indexedSessionPageSize = 200

    /// Loads the durable session index. `reset` starts over at the newest page,
    /// which is also what a filter change needs; otherwise the next page is
    /// appended.
    func loadIndexedSessions(reset: Bool = false, agent: String? = nil, project: String? = nil) {
        guard !isLoadingIndexedSessions else { return }
        if !reset && indexedSessionsReachedEnd { return }
        let offset = reset ? 0 : indexedSessions.count
        isLoadingIndexedSessions = true
        Task {
            defer { isLoadingIndexedSessions = false }
            do {
                let response = try await makeSessionIPCClient().list(ATMSessionListRequest(
                    agent: agent?.isEmpty == false ? agent : nil,
                    project: project?.isEmpty == false ? project : nil,
                    includeAll: true,
                    order: "desc",
                    limit: Self.indexedSessionPageSize,
                    offset: offset
                ))
                let page = response.sessions
                indexedSessionsError = nil
                // A short page is the end of the index. Tracking it explicitly
                // keeps the list from asking for a page that will always be empty
                // every time the view scrolls.
                indexedSessionsReachedEnd = page.count < Self.indexedSessionPageSize
                if reset {
                    indexedSessions = page
                } else {
                    let known = Set(indexedSessions.map(\.id))
                    indexedSessions += page.filter { !known.contains($0.id) }
                }
            } catch {
                indexedSessionsError = ATMErrorText.compact(error.localizedDescription)
            }
        }
    }

    // MARK: - Session reads

    private func sessionReadKey(_ sessionID: String, _ mode: ATMSessionReadMode) -> String {
        "\(mode.rawValue)|\(sessionID)"
    }

    func sessionTranscript(_ sessionID: String, mode: ATMSessionReadMode) -> ATMSessionTranscript? {
        sessionTranscripts[sessionReadKey(sessionID, mode)]
    }

    func sessionTimeline(_ sessionID: String) -> [ATMSessionTimelineEntry]? {
        sessionTimelines[sessionID]
    }

    func sessionReadError(_ sessionID: String, mode: ATMSessionReadMode) -> String? {
        sessionReadErrors[sessionReadKey(sessionID, mode)]
    }

    func isLoadingSessionRead(_ sessionID: String, mode: ATMSessionReadMode) -> Bool {
        loadingSessionReads.contains(sessionReadKey(sessionID, mode))
    }

    /// Reads one session at one depth. Results are cached per (session, mode):
    /// a transcript is immutable history for every mode except the tail of a live
    /// session, and `reload` is what refreshes that case explicitly.
    func loadSessionRead(_ sessionID: String, mode: ATMSessionReadMode, reload: Bool = false) {
        let key = sessionReadKey(sessionID, mode)
        guard !loadingSessionReads.contains(key) else { return }
        if !reload {
            if mode == .timeline, sessionTimelines[sessionID] != nil { return }
            if mode != .timeline, sessionTranscripts[key] != nil { return }
        }
        loadingSessionReads.insert(key)
        Task {
            defer { loadingSessionReads.remove(key) }
            do {
                if mode == .timeline {
                    sessionTimelines[sessionID] = try await makeSessionIPCClient().timeline(
                        ATMSessionTimelineRequest(sessionID: sessionID)
                    )
                } else if let request = mode.showRequest(sessionID: sessionID) {
                    sessionTranscripts[key] = try await makeSessionIPCClient().show(request)
                }
                sessionReadErrors[key] = nil
            } catch {
                sessionReadErrors[key] = ATMErrorText.compact(error.localizedDescription)
            }
        }
    }
}
