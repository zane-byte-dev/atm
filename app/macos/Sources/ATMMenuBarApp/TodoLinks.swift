import Foundation

struct ATMTodoLinkSaveRequest: Encodable, Equatable {
    let todoID: String
    let originalURL: String?
    let url: String
    let kind: String
    let title: String
    let relation: String

    enum CodingKeys: String, CodingKey {
        case todoID = "todo_id"
        case originalURL = "original_url"
        case url, kind, title, relation
    }
}

struct ATMTodoLinkRemoveRequest: Encodable, Equatable {
    let todoID: String
    let url: String

    enum CodingKeys: String, CodingKey {
        case todoID = "todo_id"
        case url
    }
}

enum ATMTodoLinkGroup: String, CaseIterable, Identifiable {
    case review = "变更与评审"
    case delivery = "构建与发布"
    case documents = "文档与产物"
    case other = "其他"

    var id: String { rawValue }
}

extension ATMTodoLink {
    static let kindOptions: [(value: String, title: String)] = [
        ("", "自动识别"), ("cr", "CR · 变更单"), ("mr", "MR / PR · 代码评审"),
        ("pipeline", "构建流水线"), ("release", "发布单"), ("preview", "预览地址"),
        ("website", "线上地址"), ("document", "文档"), ("artifact", "交付产物"),
        ("workitem", "需求 / 工作项"), ("other", "其他")
    ]

    // Preview only; Work repeats inference and URL safety validation on save.
    // Keep these rules aligned with work.InferTodoLinkKind.
    static func inferredKind(for raw: String) -> String {
        guard let url = URL(string: raw.trimmingCharacters(in: .whitespacesAndNewlines)) else { return "" }
        let path = url.path.lowercased()
        let host = url.host?.lowercased() ?? ""
        if path.contains("merge_requests") || path.contains("/pull/") || path.contains("/codereview/") { return "mr" }
        if path.contains("/cr/") || path.contains("change-request") { return "cr" }
        if path.contains("pipeline") { return "pipeline" }
        if path.contains("workitem") || path.contains("/issues/") { return "workitem" }
        if path.contains("/release/") || path.contains("/releases/") || path.contains("/deploy/") { return "release" }
        if host == "yuque.com" || host.hasSuffix(".yuque.com") || host == "alidocs.dingtalk.com"
            || host == "docs.google.com" || path.hasSuffix(".pdf") || path.contains("/docs/") { return "document" }
        return ""
    }

    var effectiveKind: String {
        let saved = kind?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() ?? ""
        return saved.isEmpty ? Self.inferredKind(for: url) : saved
    }

    var group: ATMTodoLinkGroup {
        switch effectiveKind {
        case "cr", "mr", "pr": return .review
        case "pipeline", "release", "preview", "website": return .delivery
        case "document", "artifact", "workitem": return .documents
        default: return .other
        }
    }

    var kindLabel: String {
        if effectiveKind == "pr" { return "MR / PR · 代码评审" }
        return Self.kindOptions.first(where: { !$0.value.isEmpty && $0.value == effectiveKind })?.title
            ?? (effectiveKind.isEmpty ? "其他" : effectiveKind)
    }

    var displayTitle: String {
        let value = title?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return value.isEmpty ? url : value
    }

    var relationLabel: String? {
        guard let value = relation?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        switch value {
        case "tracks": return "跟踪"
        case "blocks": return "阻塞参考"
        case "evidence": return "验收证据"
        default: return value
        }
    }

    var destination: URL? {
        guard let value = URL(string: url), let scheme = value.scheme?.lowercased(),
              ["http", "https"].contains(scheme), value.host?.isEmpty == false,
              value.user == nil, value.password == nil else { return nil }
        return value
    }
}
