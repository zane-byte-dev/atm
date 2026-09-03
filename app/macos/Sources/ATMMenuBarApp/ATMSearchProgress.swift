import Foundation

enum ATMSearchDomain: String, CaseIterable, Hashable {
    case tasks, sessions, documents, memories

    var title: String {
        switch self {
        case .tasks: return "任务"
        case .sessions: return "会话"
        case .documents: return "知识"
        case .memories: return "共享记忆"
        }
    }
}

/// Tracks the query that produced each visible section. Beginning a search
/// retains those sections while every domain refreshes independently.
struct ATMSearchProgress {
    private(set) var requestID = UUID()
    private(set) var query = ""
    private(set) var pending: Set<ATMSearchDomain> = []
    private(set) var resultQueries: [ATMSearchDomain: String] = [:]
    private(set) var errors: [ATMSearchDomain: String] = [:]

    var isSearching: Bool { !pending.isEmpty }

    var errorMessage: String? {
        let messages = ATMSearchDomain.allCases.compactMap { domain in
            errors[domain].map { "\(domain.title)：\($0)" }
        }
        return messages.isEmpty ? nil : messages.joined(separator: "；")
    }

    @discardableResult
    mutating func begin(query: String) -> UUID {
        requestID = UUID()
        self.query = query
        pending = query.isEmpty ? [] : Set(ATMSearchDomain.allCases)
        errors = [:]
        if query.isEmpty { resultQueries = [:] }
        return requestID
    }

    func accepts(_ requestID: UUID, query: String) -> Bool {
        self.requestID == requestID && self.query == query
    }

    @discardableResult
    mutating func complete(_ domain: ATMSearchDomain, requestID: UUID, error: String?) -> Bool {
        guard self.requestID == requestID, pending.remove(domain) != nil else { return false }
        resultQueries[domain] = query
        errors[domain] = error
        return true
    }

    func previousQuery(for domain: ATMSearchDomain) -> String? {
        guard let previous = resultQueries[domain], previous != query else { return nil }
        return previous
    }
}
