import Foundation

struct ATMTodoAdviceRequest: Encodable {
    let todoID: String
    let previous: [ATMTodoAdviceBaseline]

    enum CodingKeys: String, CodingKey {
        case todoID = "todo_id"
        case previous
    }
}

struct ATMTodoAdviceBaseline: Codable {
    let url: String
    let checkedAt: String
    let commentIDs: [Int64]

    enum CodingKeys: String, CodingKey {
        case url
        case checkedAt = "checked_at"
        case commentIDs = "comment_ids"
    }
}

struct ATMTodoAdviceResponse: Decodable {
    let todoID: String
    let checkedAt: String
    let summary: String
    let reviews: [ATMTodoAdviceReview]

    enum CodingKeys: String, CodingKey {
        case todoID = "todo_id"
        case checkedAt = "checked_at"
        case summary, reviews
    }

    var checkedDate: Date? { ISO8601DateFormatter().date(from: checkedAt) }
}

struct ATMTodoAdviceReview: Decodable, Identifiable {
    let url: String
    let repo: String
    let mrID: Int64
    let title: String
    let state: String
    let statusLabel: String
    let suggestion: String
    let severity: String
    let commentCount: Int?
    let unresolvedCount: Int?
    let newCommentCount: Int?
    let comments: [ATMTodoAdviceComment]
    let baseline: ATMTodoAdviceBaseline?
    let errors: [String]

    var id: String { url }

    var messageTitle: String {
        let status = (newCommentCount ?? 0) > 0 ? "有新评论 · \(statusLabel)" : statusLabel
        return "CR \(String(mrID)) · \(status)"
    }

    func evidence(checkedAt: String) -> String {
        var lines = [title, "\(repo) · CR \(String(mrID))", "查询于 \(checkedAt)"]
        if let count = commentCount { lines.append("评论：\(count) 条") }
        if let unresolved = unresolvedCount, unresolved > 0 {
            lines.append("未解决的行内评论：\(unresolved) 条")
        }
        if let new = newCommentCount {
            lines.append(new > 0 ? "较上次查询新增 \(new) 条评论" : "较上次查询无新增评论")
        } else if commentCount != nil {
            lines.append("首次查询，后续刷新时比较新增评论。")
        }
        lines.append(contentsOf: errors)
        if !comments.isEmpty {
            lines.append("\n最近评论")
            lines.append(contentsOf: comments.map { "\($0.author)：\($0.text)" })
        }
        return lines.joined(separator: "\n")
    }

    enum CodingKeys: String, CodingKey {
        case url, repo, title, state, suggestion, severity, comments, baseline, errors
        case mrID = "mr_id"
        case statusLabel = "status_label"
        case commentCount = "comment_count"
        case unresolvedCount = "unresolved_count"
        case newCommentCount = "new_comment_count"
    }
}

struct ATMTodoAdviceComment: Decodable, Identifiable {
    let id: Int64
    let author: String
    let text: String
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id, author, text
        case createdAt = "created_at"
    }
}
