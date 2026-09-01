import Foundation

/// Typed desktop methods for shared Memory. The public `memory` CLI remains an
/// Agent/human adapter; the App sends intent directly to Knowledge Service.
enum ATMMemoryIPCCommand {
    static let recall = ATMIPCMethod<ATMMemoryRecallRequest, ATMMemoryRecallResponse>(
        "memory.recall",
        responseKeyDecoding: .useDefault
    )
    static let supersede = ATMIPCMethod<ATMMemorySupersedeRequest, ATMMemorySupersedeResponse>(
        "memory.supersede",
        responseKeyDecoding: .useDefault
    )
}

struct ATMMemoryRecallRequest: Encodable, Equatable {
    let query: String?
    let scope: String?
    let limit: Int
}

struct ATMMemoryRecallResponse: Decodable, Equatable {
    let hits: [ATMMemoryHit]
}

struct ATMMemorySupersedeRequest: Encodable, Equatable {
    let targetID: String
    let scope: String
    let content: String
    let tags: [String]
    let source: String?

    enum CodingKeys: String, CodingKey {
        case scope, content, tags, source
        case targetID = "target_id"
    }
}

struct ATMMemoryMutationReceipt: Decodable, Equatable {
    let id: String
}

struct ATMMemorySupersedeResponse: Decodable, Equatable {
    let event: ATMMemoryMutationReceipt
}

struct ATMMemoryIPCClient: Sendable {
    private let ipc: ATMIPCClient

    init(runner: ATMCommandRunner) {
        ipc = ATMIPCClient(runner: runner)
    }

    init() throws {
        ipc = try ATMIPCClient()
    }

    func recall(_ request: ATMMemoryRecallRequest) async throws -> [ATMMemoryHit] {
        try await ipc.call(ATMMemoryIPCCommand.recall, request: request).hits
    }

    func supersede(_ request: ATMMemorySupersedeRequest) async throws {
        _ = try await ipc.call(ATMMemoryIPCCommand.supersede, request: request)
    }
}
