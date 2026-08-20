import Foundation

/// The desktop Knowledge boundary. These requests describe domain intent and
/// deliberately contain no command names, flags, or arbitrary argv payloads.
enum ATMKnowledgeIPCCommand {
    static let catalog = ATMIPCMethod<ATMIPCNoRequest, [ATMKnowledgeCollection]>(
        "knowledge.catalog",
        responseKeyDecoding: .useDefault
    )
    static let query = ATMIPCMethod<ATMKnowledgeQueryRequest, ATMKnowledgeQueryResponse>(
        "knowledge.query",
        responseKeyDecoding: .useDefault
    )
    static let document = ATMIPCMethod<ATMKnowledgeDocumentRequest, ATMKnowledgeDocument>(
        "knowledge.document.get",
        responseKeyDecoding: .useDefault
    )
    static let governance = ATMIPCMethod<ATMKnowledgeGovernanceRequest, ATMKnowledgeGovernance>(
        "knowledge.governance",
        responseKeyDecoding: .useDefault
    )
    static let saveDocument = ATMIPCMethod<ATMKnowledgeDocumentSaveRequest, ATMKnowledgeDocument>(
        "knowledge.document.save",
        responseKeyDecoding: .useDefault
    )
    static let deleteDocument = ATMIPCMethod<ATMKnowledgeDocumentDeleteRequest, ATMKnowledgeDocument>(
        "knowledge.document.delete",
        responseKeyDecoding: .useDefault
    )
    static let importDocument = ATMIPCMethod<ATMKnowledgeImportRequest, [ATMKnowledgeDocument]>(
        "knowledge.document.import",
        timeout: 60,
        responseKeyDecoding: .useDefault
    )
    static let saveCollection = ATMIPCMethod<ATMKnowledgeCollectionSaveRequest, ATMKnowledgeCollection>(
        "knowledge.collection.save",
        responseKeyDecoding: .useDefault
    )
    static let deleteCollection = ATMIPCMethod<ATMKnowledgeCollectionDeleteRequest, ATMKnowledgeCollectionDeletion>(
        "knowledge.collection.delete",
        responseKeyDecoding: .useDefault
    )
    static let feedback = ATMIPCMethod<ATMKnowledgeFeedbackRequest, ATMKnowledgeFeedbackReceipt>(
        "knowledge.feedback",
        responseKeyDecoding: .useDefault
    )
}

struct ATMKnowledgeQueryRequest: Encodable, Equatable {
    let text: String?
    let collection: String?
    let status: String?
    let sessionID: String?
    let limit: Int?

    enum CodingKeys: String, CodingKey {
        case text, collection, status, limit
        case sessionID = "session_id"
    }
}

struct ATMKnowledgeQueryResponse: Decodable, Equatable {
    let documents: [ATMKnowledgeDocumentSummary]
}

struct ATMKnowledgeDocumentRequest: Codable, Equatable {
    let documentID: String

    enum CodingKeys: String, CodingKey {
        case documentID = "document_id"
    }
}

struct ATMKnowledgeDocumentDeleteRequest: Encodable, Equatable {
    let documentID: String
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case confirmed
        case documentID = "document_id"
    }
}

struct ATMKnowledgeGovernanceRequest: Encodable, Equatable {
    let staleDays: Int

    enum CodingKeys: String, CodingKey {
        case staleDays = "stale_days"
    }
}

struct ATMKnowledgeQualityTotals: Decodable, Equatable {
    let documents: Int
    let retrievals: Int
    let adopted: Int
    let corrected: Int
    let rejected: Int
}

struct ATMKnowledgeQualitySnapshot: Decodable, Equatable {
    let qualities: [ATMKnowledgeQuality]
    let totals: ATMKnowledgeQualityTotals
}

struct ATMKnowledgeGovernance: Decodable, Equatable {
    let audit: ATMKnowledgeAuditReport
    let quality: ATMKnowledgeQualitySnapshot
}

struct ATMKnowledgeDocumentCreate: Encodable, Equatable {
    let title: String
    let content: String
    let collection: String
    let domains: [String]
    let tags: [String]
    let projects: [String]
    let producer: String
}

struct ATMKnowledgeDocumentContentSave: Encodable, Equatable {
    let documentID: String
    let content: String

    enum CodingKeys: String, CodingKey {
        case content
        case documentID = "document_id"
    }
}

struct ATMKnowledgeDocumentMetadataSave: Encodable, Equatable {
    let documentID: String
    let title: String?
    let collection: String?
    let status: String?
    let domains: [String]?
    let tags: [String]?
    let projects: [String]?

    enum CodingKeys: String, CodingKey {
        case title, collection, status, domains, tags, projects
        case documentID = "document_id"
    }
}

/// A closed one-of payload. The factories are the only constructors used by
/// the store, so Swift cannot accidentally send two save variants at once.
struct ATMKnowledgeDocumentSaveRequest: Encodable, Equatable {
    private let create: ATMKnowledgeDocumentCreate?
    private let content: ATMKnowledgeDocumentContentSave?
    private let metadata: ATMKnowledgeDocumentMetadataSave?

    static func create(_ draft: ATMKnowledgeDraft) -> ATMKnowledgeDocumentSaveRequest {
        ATMKnowledgeDocumentSaveRequest(
            create: ATMKnowledgeDocumentCreate(
                title: draft.title,
                content: draft.content,
                collection: draft.collection,
                domains: draft.domains,
                tags: draft.tags,
                projects: draft.projects,
                producer: "human"
            ),
            content: nil,
            metadata: nil
        )
    }

    static func content(documentID: String, content: String) -> ATMKnowledgeDocumentSaveRequest {
        ATMKnowledgeDocumentSaveRequest(
            create: nil,
            content: ATMKnowledgeDocumentContentSave(documentID: documentID, content: content),
            metadata: nil
        )
    }

    static func metadata(
        documentID: String,
        title: String? = nil,
        collection: String? = nil,
        status: String? = nil,
        domains: [String]? = nil,
        tags: [String]? = nil,
        projects: [String]? = nil
    ) -> ATMKnowledgeDocumentSaveRequest {
        ATMKnowledgeDocumentSaveRequest(
            create: nil,
            content: nil,
            metadata: ATMKnowledgeDocumentMetadataSave(
                documentID: documentID,
                title: title,
                collection: collection,
                status: status,
                domains: domains,
                tags: tags,
                projects: projects
            )
        )
    }
}

struct ATMKnowledgeImportRequest: Encodable, Equatable {
    let path: String
    let collection: String
    let producer: String
}

struct ATMKnowledgeCollectionCreate: Encodable, Equatable {
    let id: String
    let name: String
}

struct ATMKnowledgeCollectionUpdate: Encodable, Equatable {
    let id: String
    let name: String
}

struct ATMKnowledgeCollectionSaveRequest: Encodable, Equatable {
    private let create: ATMKnowledgeCollectionCreate?
    private let update: ATMKnowledgeCollectionUpdate?

    static func create(id: String, name: String) -> ATMKnowledgeCollectionSaveRequest {
        ATMKnowledgeCollectionSaveRequest(
            create: ATMKnowledgeCollectionCreate(id: id, name: name),
            update: nil
        )
    }

    static func update(id: String, name: String) -> ATMKnowledgeCollectionSaveRequest {
        ATMKnowledgeCollectionSaveRequest(
            create: nil,
            update: ATMKnowledgeCollectionUpdate(id: id, name: name)
        )
    }
}

struct ATMKnowledgeCollectionDeleteRequest: Encodable, Equatable {
    let id: String
    let force: Bool
    let moveTo: String?
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case id, force, confirmed
        case moveTo = "move_to"
    }
}

struct ATMKnowledgeCollectionDeletion: Decodable, Equatable {
    let id: String
    let movedTo: String?
    let movedDocuments: Int

    enum CodingKeys: String, CodingKey {
        case id
        case movedTo = "moved_to"
        case movedDocuments = "moved_documents"
    }
}

struct ATMKnowledgeFeedbackRequest: Encodable, Equatable {
    let documentID: String
    let sessionID: String
    let outcome: String
    let note: String?

    enum CodingKeys: String, CodingKey {
        case outcome, note
        case documentID = "document_id"
        case sessionID = "session_id"
    }
}

struct ATMKnowledgeFeedbackReceipt: Decodable, Equatable {
    let id: String
    let documentID: String
    let sessionID: String
    let outcome: String
    let note: String?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, outcome, note, createdAt
        case documentID = "documentId"
        case sessionID = "sessionId"
    }
}

/// Thin Swift application adapter used by ATMDataStore and directly injectable
/// in tests. It is the only ordinary App code that knows Knowledge IPC methods.
struct ATMKnowledgeIPCClient: Sendable {
    private let ipc: ATMIPCClient

    init(runner: ATMCommandRunner) {
        ipc = ATMIPCClient(runner: runner)
    }

    init() throws {
        ipc = try ATMIPCClient()
    }

    func catalog() async throws -> [ATMKnowledgeCollection] {
        try await ipc.call(ATMKnowledgeIPCCommand.catalog)
    }

    func query(_ request: ATMKnowledgeQueryRequest) async throws -> [ATMKnowledgeDocumentSummary] {
        try await ipc.call(ATMKnowledgeIPCCommand.query, request: request).documents
    }

    func document(_ documentID: String) async throws -> ATMKnowledgeDocument {
        try await ipc.call(
            ATMKnowledgeIPCCommand.document,
            request: ATMKnowledgeDocumentRequest(documentID: documentID)
        )
    }

    func governance(staleDays: Int) async throws -> ATMKnowledgeGovernance {
        try await ipc.call(
            ATMKnowledgeIPCCommand.governance,
            request: ATMKnowledgeGovernanceRequest(staleDays: staleDays)
        )
    }

    func saveDocument(_ request: ATMKnowledgeDocumentSaveRequest) async throws -> ATMKnowledgeDocument {
        try await ipc.call(ATMKnowledgeIPCCommand.saveDocument, request: request)
    }

    func deleteDocument(_ documentID: String, confirmed: Bool) async throws {
        _ = try await ipc.call(
            ATMKnowledgeIPCCommand.deleteDocument,
            request: ATMKnowledgeDocumentDeleteRequest(
                documentID: documentID,
                confirmed: confirmed
            )
        )
    }

    func importDocument(path: String, collection: String) async throws -> [ATMKnowledgeDocument] {
        try await ipc.call(
            ATMKnowledgeIPCCommand.importDocument,
            request: ATMKnowledgeImportRequest(
                path: path,
                collection: collection,
                producer: "atm-desktop"
            )
        )
    }

    func saveCollection(_ request: ATMKnowledgeCollectionSaveRequest) async throws {
        _ = try await ipc.call(ATMKnowledgeIPCCommand.saveCollection, request: request)
    }

    func deleteCollection(id: String, force: Bool, moveTo: String?, confirmed: Bool) async throws {
        _ = try await ipc.call(
            ATMKnowledgeIPCCommand.deleteCollection,
            request: ATMKnowledgeCollectionDeleteRequest(
                id: id,
                force: force,
                moveTo: moveTo,
                confirmed: confirmed
            )
        )
    }

    func feedback(_ request: ATMKnowledgeFeedbackRequest) async throws {
        _ = try await ipc.call(ATMKnowledgeIPCCommand.feedback, request: request)
    }
}
