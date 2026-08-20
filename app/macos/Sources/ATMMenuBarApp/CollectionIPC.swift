import Foundation

/// Typed desktop methods for the Collection workspace. These are application
/// use cases, not a second spelling of `atm collect ...` argv.
enum ATMCollectionIPCCommand {
    static let snapshot = ATMIPCMethod<ATMCollectionSnapshotRequest, ATMCollectionOverview>(
        "collect.snapshot", responseKeyDecoding: .useDefault
    )
    static let run = ATMIPCMethod<ATMCollectionRunRequest, ATMCollectionRunResult>(
        "collect.run", timeout: 300, responseKeyDecoding: .useDefault
    )
    static let history = ATMIPCMethod<ATMCollectionHistoryRequest, ATMCollectionHistory>(
        "collect.history", timeout: 45, responseKeyDecoding: .useDefault
    )
    static let saveSource = ATMIPCMethod<ATMCollectionSourceSaveRequest, ATMCollectionSourceResult>(
        "collect.source.save", responseKeyDecoding: .useDefault
    )
    static let searchSources = ATMIPCMethod<ATMCollectionSourceSearchRequest, ATMCollectionCandidateList>(
        "collect.source.search", timeout: 45, responseKeyDecoding: .useDefault
    )
    static let setSourceEnabled = ATMIPCMethod<ATMCollectionSourceEnabledRequest, ATMCollectionSourceEnabledResult>(
        "collect.source.enabled", responseKeyDecoding: .useDefault
    )
    static let setSourceMuted = ATMIPCMethod<ATMCollectionSourceMutedRequest, ATMCollectionSourceMutedResult>(
        "collect.source.muted", responseKeyDecoding: .useDefault
    )
    static let deleteSource = ATMIPCMethod<ATMCollectionSourceDeleteRequest, ATMCollectionSourceResult>(
        "collect.source.delete", responseKeyDecoding: .useDefault
    )
    static let reprocessItem = ATMIPCMethod<ATMCollectionItemIDRequest, ATMCollectionItemResult>(
        "collect.item.reprocess", timeout: 180, responseKeyDecoding: .useDefault
    )
    static let saveConclusion = ATMIPCMethod<ATMCollectionSaveConclusionRequest, ATMCollectionItemResult>(
        "collect.item.save_conclusion", responseKeyDecoding: .useDefault
    )
    static let promoteItem = ATMIPCMethod<ATMCollectionPromoteRequest, ATMCollectionItemResult>(
        "collect.item.promote", responseKeyDecoding: .useDefault
    )
    static let correctItem = ATMIPCMethod<ATMCollectionCorrectRequest, ATMCollectionItemResult>(
        "collect.item.correct", responseKeyDecoding: .useDefault
    )
    static let revertItem = ATMIPCMethod<ATMCollectionRevertRequest, ATMCollectionItemResult>(
        "collect.item.revert", responseKeyDecoding: .useDefault
    )
    static let setItemsRead = ATMIPCMethod<ATMCollectionItemsReadRequest, ATMCollectionItemsReadResult>(
        "collect.item.read", responseKeyDecoding: .useDefault
    )
    static let setItemsArchived = ATMIPCMethod<ATMCollectionItemsArchivedRequest, ATMCollectionItemsArchivedResult>(
        "collect.item.archive", responseKeyDecoding: .useDefault
    )
    static let deleteItems = ATMIPCMethod<ATMCollectionItemsDeleteRequest, ATMCollectionItemsDeleteResult>(
        "collect.item.delete", responseKeyDecoding: .useDefault
    )
}

struct ATMCollectionSnapshotRequest: Encodable {
    let itemLimit: Int

    enum CodingKeys: String, CodingKey { case itemLimit = "item_limit" }
}

struct ATMCollectionRunRequest: Encodable {
    let sourceID: String?
    let dueOnly: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case dueOnly = "due_only"
    }
}

struct ATMCollectionRunResult: Decodable {
    let runs: [ATMCollectionRun]
}

struct ATMCollectionHistoryRequest: Encodable {
    let sourceID: String
    let limit: Int
    let local: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case limit, local
    }
}

struct ATMCollectionSourceSaveRequest: Encodable {
    let connector: String
    let kind: String
    let externalID: String
    let name: String
    let project: String
    let excludePattern: String
    let instruction: String
    let knowledgeCollection: String
    let strategy: String
    let decisionUnit: String
    let intervalMinutes: Int
    let priority: String
    let autoDispatch: Bool
    let enabled: Bool

    enum CodingKeys: String, CodingKey {
        case connector, kind, name, project, instruction, strategy, priority, enabled
        case externalID = "external_id"
        case excludePattern = "exclude_pattern"
        case knowledgeCollection = "knowledge_collection"
        case decisionUnit = "decision_unit"
        case intervalMinutes = "interval_minutes"
        case autoDispatch = "auto_dispatch"
    }
}

struct ATMCollectionSourceSearchRequest: Encodable {
    let connector: String
    let kind: String
    let keyword: String
    let limit: Int
}

struct ATMCollectionSourceEnabledRequest: Encodable {
    let sourceID: String
    let enabled: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case enabled
    }
}

struct ATMCollectionSourceMutedRequest: Encodable {
    let sourceID: String
    let muted: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case muted
    }
}

struct ATMCollectionSourceDeleteRequest: Encodable {
    let sourceID: String
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case confirmed
    }
}

struct ATMCollectionSourceResult: Decodable {
    let source: ATMCollectionSource
}

struct ATMCollectionSourceEnabledResult: Decodable {
    let id: String
    let enabled: Bool
}

struct ATMCollectionSourceMutedResult: Decodable {
    let id: String
    let muted: Bool
}

struct ATMCollectionItemIDRequest: Encodable {
    let itemID: String

    enum CodingKeys: String, CodingKey { case itemID = "item_id" }
}

struct ATMCollectionItemCorrectionRequest: Encodable {
    let title: String?
    let project: String?
    let priority: String?
}

struct ATMCollectionPromoteRequest: Encodable {
    let itemID: String
    let correction: ATMCollectionItemCorrectionRequest

    enum CodingKeys: String, CodingKey {
        case itemID = "item_id"
        case correction
    }
}

struct ATMCollectionCorrectRequest: Encodable {
    let itemID: String
    let correction: ATMCollectionItemCorrectionRequest

    enum CodingKeys: String, CodingKey {
        case itemID = "item_id"
        case correction
    }
}

struct ATMCollectionSaveConclusionRequest: Encodable {
    let itemID: String
    let collection: String?

    enum CodingKeys: String, CodingKey {
        case itemID = "item_id"
        case collection
    }
}

struct ATMCollectionRevertRequest: Encodable {
    let itemID: String
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case itemID = "item_id"
        case confirmed
    }
}

struct ATMCollectionItemResult: Decodable {
    let item: ATMCollectionItem
}

struct ATMCollectionItemsReadRequest: Encodable {
    let itemIDs: [String]
    let all: Bool
    let read: Bool

    enum CodingKeys: String, CodingKey {
        case itemIDs = "item_ids"
        case all, read
    }
}

struct ATMCollectionItemsReadResult: Decodable {
    let items: [ATMCollectionItem]?
    let count: Int
    let read: Bool
}

struct ATMCollectionItemsArchivedRequest: Encodable {
    let itemIDs: [String]
    let archived: Bool

    enum CodingKeys: String, CodingKey {
        case itemIDs = "item_ids"
        case archived
    }
}

struct ATMCollectionItemsArchivedResult: Decodable {
    let items: [ATMCollectionItem]
    let count: Int
    let archived: Bool
}

struct ATMCollectionItemsDeleteRequest: Encodable {
    let itemIDs: [String]
    let confirmed: Bool

    enum CodingKeys: String, CodingKey {
        case itemIDs = "item_ids"
        case confirmed
    }
}

struct ATMCollectionDeletedItem: Decodable {
    let id: String
    let todoID: String?

    enum CodingKeys: String, CodingKey {
        case id
        case todoID = "todo_id"
    }
}

struct ATMCollectionItemsDeleteResult: Decodable {
    let deleted: [ATMCollectionDeletedItem]
    let count: Int
}
