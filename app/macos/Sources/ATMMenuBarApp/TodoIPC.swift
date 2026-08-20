import Foundation

/// Typed desktop methods for Todo metadata, read models, lifecycle and
/// retention. Dependency and Plan workflows keep their own surfaces.
enum ATMTodoIPCCommand {
    static let list = ATMIPCMethod<ATMTodoListRequest, [ATMTodo]>(
        "todo.list",
        responseKeyDecoding: .useDefault
    )
    static let show = ATMIPCMethod<ATMTodoIDRequest, ATMTodoDetail>(
        "todo.show",
        responseKeyDecoding: .useDefault
    )
    static let document = ATMIPCMethod<ATMTodoIDRequest, ATMTodoDocumentResponse>(
        "todo.doc",
        responseKeyDecoding: .useDefault
    )
    static let create = ATMIPCMethod<ATMTodoCreateRequest, ATMTodo>(
        "todo.create",
        responseKeyDecoding: .useDefault
    )
    static let update = ATMIPCMethod<ATMTodoUpdateRequest, ATMTodo>(
        "todo.update",
        responseKeyDecoding: .useDefault
    )
    static let refine = ATMIPCMethod<ATMTodoRefineRequest, ATMTodoRefineResponse>(
        "todo.refine",
        timeout: 180,
        responseKeyDecoding: .useDefault
    )
    static let start = ATMIPCMethod<ATMTodoIDRequest, ATMTodo>(
        "todo.start",
        responseKeyDecoding: .useDefault
    )
    static let done = ATMIPCMethod<ATMTodoDoneRequest, ATMTodo>(
        "todo.done",
        responseKeyDecoding: .useDefault
    )
    static let archive = ATMIPCMethod<ATMTodoRetentionRequest, ATMTodoRetentionResponse>(
        "todo.archive",
        responseKeyDecoding: .useDefault
    )
    static let restore = ATMIPCMethod<ATMTodoRetentionRequest, ATMTodoRetentionResponse>(
        "todo.restore",
        responseKeyDecoding: .useDefault
    )
    static let delete = ATMIPCMethod<ATMTodoDeleteRequest, ATMTodoDeleteResponse>(
        "todo.delete",
        responseKeyDecoding: .useDefault
    )
}

struct ATMTodoDoneRequest: Encodable, Equatable {
    let todoID: String
    let reason: String

    enum CodingKeys: String, CodingKey {
        case reason
        case todoID = "todo_id"
    }
}

struct ATMTodoRetentionRequest: Encodable, Equatable {
    let todoIDs: [String]

    enum CodingKeys: String, CodingKey { case todoIDs = "todo_ids" }

    init(_ todoID: String) { todoIDs = [todoID] }
}

struct ATMTodoRetentionResponse: Decodable, Equatable {
    let moved: [String]
    let unchanged: [String]?
}

/// Confirmed is sent explicitly rather than implied by calling this at all:
/// deletion is irreversible and the desktop's confirmation dialog is what stands
/// in front of it.
struct ATMTodoDeleteRequest: Encodable, Equatable {
    let todoID: String
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case confirmed
        case todoID = "todo_id"
    }

    init(_ todoID: String) {
        self.todoID = todoID
        confirmed = true
    }
}

struct ATMTodoDeleteResponse: Decodable, Equatable {
    let deleted: [String]
}

struct ATMTodoListRequest: Encodable, Equatable {
    let status: String?
    let query: String?
    let limit: Int
    let offset: Int

    init(status: String? = nil, query: String? = nil, limit: Int = 0, offset: Int = 0) {
        self.status = status
        self.query = query
        self.limit = limit
        self.offset = offset
    }
}

struct ATMTodoIDRequest: Encodable, Equatable {
    let todoID: String

    enum CodingKeys: String, CodingKey { case todoID = "todo_id" }
}

struct ATMTodoDocumentResponse: Decodable, Equatable {
    let exists: Bool
    let content: String?
}

struct ATMTodoCreateRequest: Encodable, Equatable {
    let title: String
    let description: String
    let priority: String
    let project: String
    let imagePaths: [String]

    enum CodingKeys: String, CodingKey {
        case title, description, priority, project
        case imagePaths = "image_paths"
    }

    init(draft: ATMTodoDraft) {
        title = draft.title
        description = draft.description
        priority = draft.priority
        project = draft.project
        imagePaths = draft.imagePaths
    }
}

/// Sparse update: nil omits a field, while an empty non-nil value clears it.
struct ATMTodoUpdateRequest: Encodable, Equatable {
    let todoID: String
    let title: String?
    let description: String?
    let priority: String?
    let project: String?
    let status: String?
    let wakeCondition: String?
    let reviewAt: String?
    let source: String?

    enum CodingKeys: String, CodingKey {
        case title, description, priority, project, status, source
        case todoID = "todo_id"
        case wakeCondition = "wake_condition"
        case reviewAt = "review_at"
    }

    init(
        todoID: String,
        title: String? = nil,
        description: String? = nil,
        priority: String? = nil,
        project: String? = nil,
        status: String? = nil,
        wakeCondition: String? = nil,
        reviewAt: String? = nil,
        source: String? = nil
    ) {
        self.todoID = todoID
        self.title = title
        self.description = description
        self.priority = priority
        self.project = project
        self.status = status
        self.wakeCondition = wakeCondition
        self.reviewAt = reviewAt
        self.source = source
    }

    init(todoID: String, edit: ATMTodoEdit) {
        self.init(
            todoID: todoID,
            title: edit.title,
            description: edit.description,
            priority: edit.priority,
            project: edit.project,
            status: edit.status,
            wakeCondition: edit.wakeCondition,
            reviewAt: edit.reviewAt,
            source: edit.source
        )
    }
}

struct ATMTodoRefineRequest: Encodable, Equatable {
    let todoID: String
    let allowSplit: Bool
    let maxChildren: Int
    let hint: String
    let dryRun: Bool

    enum CodingKeys: String, CodingKey {
        case hint
        case todoID = "todo_id"
        case allowSplit = "allow_split"
        case maxChildren = "max_children"
        case dryRun = "dry_run"
    }

    init(
        todoID: String,
        allowSplit: Bool = true,
        maxChildren: Int = 5,
        hint: String = "",
        dryRun: Bool = false
    ) {
        self.todoID = todoID.trimmingCharacters(in: .whitespacesAndNewlines)
        self.allowSplit = allowSplit
        self.maxChildren = maxChildren
        self.hint = hint.trimmingCharacters(in: .whitespacesAndNewlines)
        self.dryRun = dryRun
    }
}

struct ATMTodoRefineChild: Decodable, Equatable {
    let title: String
    let description: String
    let dependsOnIndexes: [Int]

    enum CodingKeys: String, CodingKey {
        case title, description
        case dependsOnIndexes = "depends_on_indexes"
    }
}

struct ATMTodoRefineProposal: Decodable, Equatable {
    let title: String
    let description: String
    let complexity: String
    let plan: String
    let reason: String
    let children: [ATMTodoRefineChild]
}

struct ATMTodoRefineResponse: Decodable {
    let todo: ATMTodo
    let complexity: String
    let reason: String?
    let titleChanged: Bool
    let descriptionChanged: Bool
    let split: Bool
    let splitSkip: String?
    let plan: String?
    let children: [ATMTodo]
    let dryRun: Bool
    let changed: Bool
    let source: String?
    let proposal: ATMTodoRefineProposal?
    let proposedTitle: String?
    let proposedDescription: String?
    let proposedChildren: [ATMTodoRefineChild]?

    enum CodingKeys: String, CodingKey {
        case todo, complexity, reason, split, plan, children, changed, source, proposal
        case titleChanged = "title_changed"
        case descriptionChanged = "description_changed"
        case splitSkip = "split_skip"
        case dryRun = "dry_run"
        case proposedTitle = "proposed_title"
        case proposedDescription = "proposed_description"
        case proposedChildren = "proposed_children"
    }
}

struct ATMTodoIPCClient: Sendable {
    private let ipc: ATMIPCClient

    init(runner: ATMCommandRunner) {
        ipc = ATMIPCClient(runner: runner)
    }

    init() throws {
        ipc = try ATMIPCClient()
    }

    func list(_ request: ATMTodoListRequest) async throws -> [ATMTodo] {
        try await ipc.call(ATMTodoIPCCommand.list, request: request)
    }

    func show(_ todoID: String) async throws -> ATMTodoDetail {
        try await ipc.call(ATMTodoIPCCommand.show, request: ATMTodoIDRequest(todoID: todoID))
    }

    func document(_ todoID: String) async throws -> ATMTodoDocumentResponse {
        try await ipc.call(ATMTodoIPCCommand.document, request: ATMTodoIDRequest(todoID: todoID))
    }

    func create(_ request: ATMTodoCreateRequest) async throws -> ATMTodo {
        try await ipc.call(ATMTodoIPCCommand.create, request: request)
    }

    func update(_ request: ATMTodoUpdateRequest) async throws -> ATMTodo {
        try await ipc.call(ATMTodoIPCCommand.update, request: request)
    }

    func refine(_ request: ATMTodoRefineRequest) async throws -> ATMTodoRefineResponse {
        try await ipc.call(ATMTodoIPCCommand.refine, request: request)
    }

    func start(_ todoID: String) async throws -> ATMTodo {
        try await ipc.call(ATMTodoIPCCommand.start, request: ATMTodoIDRequest(todoID: todoID))
    }

    func done(_ todoID: String, reason: String) async throws -> ATMTodo {
        try await ipc.call(
            ATMTodoIPCCommand.done,
            request: ATMTodoDoneRequest(todoID: todoID, reason: reason)
        )
    }

    func archive(_ todoID: String) async throws -> ATMTodoRetentionResponse {
        try await ipc.call(ATMTodoIPCCommand.archive, request: ATMTodoRetentionRequest(todoID))
    }

    func restore(_ todoID: String) async throws -> ATMTodoRetentionResponse {
        try await ipc.call(ATMTodoIPCCommand.restore, request: ATMTodoRetentionRequest(todoID))
    }

    func delete(_ todoID: String) async throws -> ATMTodoDeleteResponse {
        try await ipc.call(ATMTodoIPCCommand.delete, request: ATMTodoDeleteRequest(todoID))
    }
}
