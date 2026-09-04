import Foundation

struct RuntimeInstance: Decodable {
    let schemaVersion: Int
    let origin: String
    let instanceID: String
    let dataDir: String

    enum CodingKeys: String, CodingKey {
        case origin
        case schemaVersion = "schema_version"
        case instanceID = "instance_id"
        case dataDir = "data_dir"
    }

    var validatedOrigin: URL? { Self.validateOrigin(origin) }

    static func validateOrigin(_ value: String) -> URL? {
        guard let url = URL(string: value), url.scheme == "http", url.host == "127.0.0.1",
              let port = url.port, (1...65535).contains(port), url.user == nil, url.password == nil,
              url.query == nil, url.fragment == nil, url.path.isEmpty || url.path == "/" else { return nil }
        return url
    }

    func isValid(for directory: URL) -> Bool {
        guard schemaVersion == 1, validatedOrigin != nil,
              instanceID.range(of: "^[A-Za-z0-9_-]{1,128}$", options: .regularExpression) != nil else { return false }
        let selected = directory.standardizedFileURL.resolvingSymlinksInPath().path
        let recorded = URL(fileURLWithPath: dataDir).standardizedFileURL.resolvingSymlinksInPath().path
        return !dataDir.isEmpty && selected == recorded
    }
}

struct RuntimeSnapshot: Decodable {
    let activeCount: Int
    let attentionCount: Int

    enum CodingKeys: String, CodingKey {
        case activeCount = "active_count"
        case attentionCount = "attention_count"
    }
}

struct RuntimeNotification: Decodable {
    let sequence: UInt64
    let id: String
    let kind: String
    let action: String
    let title: String
    let subtitle: String?
    let body: String?
    let objectID: String?

    enum CodingKeys: String, CodingKey {
        case sequence, id, kind, action, title, subtitle, body
        case objectID = "object_id"
    }
}

struct NotificationFeed: Decodable {
    let notifications: [RuntimeNotification]?
    let cursor: UInt64

    func advancedCursor(from current: UInt64) -> UInt64 { max(current, cursor) }
}

struct CompanionState: Decodable {
    let snapshot: RuntimeSnapshot
    let feed: NotificationFeed?
    let attentionNotificationIDs: [String]?
    let todos: CompanionTodos?
    let quota: CompanionQuota?
    let todayUsage: CompanionTodayUsage?
    let legacyQuick: CompanionLegacyQuick?

    enum CodingKeys: String, CodingKey {
        case snapshot, feed, todos, quota
        case attentionNotificationIDs = "attention_notification_ids"
        case todayUsage = "today_usage"
        case legacyQuick = "quick"
    }
}

struct CompanionTodo: Decodable, Equatable {
    let id: String
    let title: String
    let status: String
    let priority: String
    let project: String?
    let wakeCondition: String?
    let reviewAt: String?
    let menuState: String?

    enum CodingKeys: String, CodingKey {
        case id, title, status, priority, project
        case wakeCondition = "wake_condition"
        case reviewAt = "review_at"
        case menuState = "menu_state"
    }
}

struct CompanionTodos: Decodable, Equatable {
    let items: [CompanionTodo]
    let total: Int
    let truncated: Bool
    let error: String?
}

struct CompanionQuotaWindow: Decodable, Equatable {
    let agent: String
    let windowMinutes: Int
    let usedPercent: Double?
    let remainingPercent: Double?
    let resetsAt: Int64?
    let observedAt: String?
    let stale: Bool
    let resetElapsed: Bool
    let unavailableReason: String?

    enum CodingKeys: String, CodingKey {
        case agent, stale
        case windowMinutes = "window_minutes"
        case usedPercent = "used_percent"
        case remainingPercent = "remaining_percent"
        case resetsAt = "resets_at"
        case observedAt = "observed_at"
        case resetElapsed = "reset_elapsed"
        case unavailableReason = "unavailable_reason"
    }
}

struct CompanionQuota: Decodable, Equatable {
    let source: String
    let generatedAt: String
    let windows: [CompanionQuotaWindow]
    let truncated: Bool
    let error: String?

    enum CodingKeys: String, CodingKey {
        case source, windows, truncated, error
        case generatedAt = "generated_at"
    }
}

struct CompanionTodayUsage: Decodable, Equatable {
    let totalTokens: Int64
    let sessions: Int
    let queries: Int
    let error: String?

    enum CodingKeys: String, CodingKey {
        case sessions, queries, error
        case totalTokens = "total_tokens"
    }
}

/// Transitional support for a runtime started before `today_usage` existed.
struct CompanionLegacyQuick: Decodable {
    let usage: CompanionLegacyUsage?
}

struct CompanionLegacyUsage: Decodable {
    let ranges: [String: CompanionLegacyUsageRange]
    let error: String?
}

struct CompanionLegacyUsageRange: Decodable {
    let totalTokens: Int64

    enum CodingKeys: String, CodingKey { case totalTokens = "total_tokens" }
}

struct RuntimeJobReply: Decodable, Equatable {
    let id: String
    let status: String
    let phase: String
}

enum RuntimeRoute: Equatable {
    case home
    case tasks
    case newTask
    case task(String)
    case agent(String)
    case collection
    case usage(agent: String?)

    func applying(to ticketURL: URL) -> URL {
        guard var components = URLComponents(url: ticketURL, resolvingAgainstBaseURL: false) else { return ticketURL }
        switch self {
        case .home:
            break
        case .tasks:
            components.path = "/tasks"
            components.queryItems = nil
        case .newTask:
            components.path = "/tasks"
            components.queryItems = [URLQueryItem(name: "new", value: "1")]
        case .task(let id) where id.range(of: "^t[0-9]+$", options: .regularExpression) != nil:
            components.path = "/tasks/\(id)"
            components.queryItems = nil
        case .agent(let id) where id.range(of: "^[A-Za-z0-9._:-]{1,128}$", options: .regularExpression) != nil:
            components.path = "/agents/\(id)"
            components.queryItems = nil
        case .collection:
            components.path = "/collection"
            components.queryItems = nil
        case .usage(let agent):
            components.path = "/usage"
            if let agent, agent.range(of: "^[A-Za-z0-9._-]{1,80}$", options: .regularExpression) != nil {
                components.queryItems = [URLQueryItem(name: "agent", value: agent)]
            }
        default:
            break
        }
        return components.url ?? ticketURL
    }
}

enum CompanionNotificationDestination: Equatable {
    case web(RuntimeRoute)

    static func resolve(kind: String?, objectID: String?) -> Self {
        let kind = kind?.lowercased() ?? ""
        if kind == "guard" || kind.hasPrefix("guard_") { return .web(.home) }
        if kind == "collection" || kind.hasPrefix("collection_") { return .web(.collection) }
        if kind == "todo" || kind.hasPrefix("todo_") {
            return objectID.map { .web(.task($0)) } ?? .web(.tasks)
        }
        if (kind == "attention" || kind == "agent" || kind.hasPrefix("agent_") || kind == "completed"),
           let objectID, !objectID.isEmpty { return .web(.agent(objectID)) }
        if let objectID, objectID.range(of: "^t[0-9]+$", options: .regularExpression) != nil {
            return .web(.task(objectID))
        }
        return .web(.home)
    }
}

final class RuntimeClient: NSObject, URLSessionTaskDelegate {
    enum ClientError: LocalizedError {
        case notRunning
        case invalidInstance
        case unavailable
        case busy
        case failed(String)

        var errorDescription: String? {
            switch self {
            case .notRunning: return "服务未运行，请运行 atm serve --open。"
            case .invalidInstance: return "服务实例信息无效。"
            case .unavailable: return "无法连接本机服务。"
            case .busy: return "已有同步进行中"
            case .failed(let message): return message
            }
        }
    }

    let dataDirectory: URL

    init(dataDirectory: URL) { self.dataDirectory = dataDirectory }

    static func isValidControlToken(_ value: String) -> Bool {
        (32...1024).contains(value.count)
    }

    static func clientError(code: String?, message: String) -> ClientError {
        code == "busy" ? .busy : .failed(message)
    }

    func instance() throws -> RuntimeInstance {
        guard let data = try? Data(contentsOf: dataDirectory.appendingPathComponent("runtime/server.json")) else {
            throw ClientError.notRunning
        }
        guard data.count < 16_384,
              let instance = try? JSONDecoder().decode(RuntimeInstance.self, from: data),
              instance.isValid(for: dataDirectory) else { throw ClientError.invalidInstance }
        return instance
    }

    func state(instance: RuntimeInstance, clientID: String, after: UInt64, enabled: Bool) async throws -> CompanionState {
        try await call(instance: instance, method: "companion", body: [
            "client_id": clientID,
            "after": after,
            "notifications_enabled": enabled,
        ])
    }

    func acknowledge(instance: RuntimeInstance, clientID: String, sequence: UInt64) async throws {
        let _: EmptyReply = try await call(instance: instance, method: "companion/ack", body: [
            "client_id": clientID,
            "sequence": sequence,
        ])
    }

    func synchronize(instance: RuntimeInstance, idempotencyKey: String) async throws -> RuntimeJobReply {
        try await call(instance: instance, method: "session/sync", body: ["idempotency_key": idempotencyKey])
    }

    func openURL(route: RuntimeRoute = .home) async throws -> URL {
        let instance = try instance()
        let result: OpenReply = try await call(instance: instance, method: "open", body: [:])
        guard let url = URL(string: result.url), let origin = instance.validatedOrigin,
              url.scheme == origin.scheme, url.host == origin.host, url.port == origin.port,
              url.user == nil, url.password == nil else { throw ClientError.invalidInstance }
        return route.applying(to: url)
    }

    private struct Envelope<T: Decodable>: Decodable { let data: T?; let error: WireError? }
    private struct WireError: Decodable { let code: String?; let message: String }
    private struct EmptyReply: Decodable {}
    private struct OpenReply: Decodable { let url: String }

    private func call<T: Decodable>(
        instance: RuntimeInstance,
        method: String,
        body: [String: Any],
        timeout: TimeInterval = 5
    ) async throws -> T {
        guard let origin = instance.validatedOrigin else { throw ClientError.invalidInstance }
        let token = try String(
            contentsOf: dataDirectory.appendingPathComponent("runtime/control.token"),
            encoding: .utf8
        ).trimmingCharacters(in: .whitespacesAndNewlines)
        guard Self.isValidControlToken(token) else { throw ClientError.invalidInstance }

        var request = URLRequest(url: origin.appendingPathComponent("api/v1/control/" + method), timeoutInterval: timeout)
        request.httpMethod = "POST"
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue(instance.instanceID, forHTTPHeaderField: "X-ATM-Instance")

        let session = URLSession(configuration: .ephemeral, delegate: self, delegateQueue: nil)
        defer { session.invalidateAndCancel() }
        let (data, response) = try await session.data(for: request)
        guard data.count <= 2 * 1024 * 1024, let response = response as? HTTPURLResponse else {
            throw ClientError.unavailable
        }
        let envelope = try JSONDecoder().decode(Envelope<T>.self, from: data)
        if let error = envelope.error { throw Self.clientError(code: error.code, message: error.message) }
        guard response.statusCode == 200, let value = envelope.data else { throw ClientError.unavailable }
        return value
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest,
        completionHandler: @escaping (URLRequest?) -> Void
    ) {
        completionHandler(nil)
    }
}
