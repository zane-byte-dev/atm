import Foundation

/// What the add-task sheet sends to typed `todo.create`. The composer is one block of
/// requirement text. The complete block becomes the description; `title` is only a
/// local fallback while the App asks the text model for a concise title.
struct ATMTodoDraft: Equatable {
    let title: String
    let description: String
    let project: String
    let priority: String
	let imagePaths: [String]
	let temporaryImagePaths: [String]

    init(
		text: String,
		project: String,
		priority: String,
		imagePaths: [String] = [],
		temporaryImagePaths: [String] = []
	) {
		let normalized = text.trimmingCharacters(in: .whitespacesAndNewlines)
		self.title = Self.fallbackTitle(from: normalized)
		self.description = normalized
        self.project = project.trimmingCharacters(in: .whitespacesAndNewlines)
        self.priority = priority
		self.imagePaths = imagePaths
		self.temporaryImagePaths = temporaryImagePaths
    }

    var isSubmittable: Bool { !description.isEmpty }

	/// Keep creation available when the configured text model is offline. This is
	/// intentionally an excerpt, not a second interpretation of the requirement.
	static func fallbackTitle(from text: String, maximumRunes: Int = 80) -> String {
		let words = text
			.split(whereSeparator: { $0.isWhitespace })
			.map(String.init)
			.joined(separator: " ")
		guard !words.isEmpty else { return "" }
		let runes = Array(words)
		return runes.count <= maximumRunes ? words : String(runes.prefix(maximumRunes))
	}

	func cleanupTemporaryImages() {
		for path in temporaryImagePaths {
			try? FileManager.default.removeItem(atPath: path)
		}
	}
}

enum ATMTodoImageRules {
	static let maximumCount = 10
	static let maximumBytes: Int64 = 10 * 1024 * 1024
	static let allowedExtensions = Set(["png", "jpg", "jpeg", "webp", "gif", "heic"])

	static func validationError(for url: URL, currentCount: Int) -> String? {
		guard currentCount < maximumCount else { return "每个任务最多添加 10 张图片。" }
		let ext = url.pathExtension.lowercased()
		guard allowedExtensions.contains(ext) else {
			return "不支持 .\(ext.isEmpty ? "(无扩展名)" : ext)，请选择 PNG、JPEG、WebP、GIF 或 HEIC。"
		}
		guard url.isFileURL else { return "只能添加本地图片文件。" }
		do {
			let values = try url.resourceValues(forKeys: [.isRegularFileKey, .fileSizeKey])
			guard values.isRegularFile == true else { return "请选择普通图片文件。" }
			if Int64(values.fileSize ?? 0) > maximumBytes { return "单张图片不能超过 10 MB。" }
		} catch {
			return "无法读取图片：\(error.localizedDescription)"
		}
		return nil
	}
}

/// Project and priority inferred from what was typed plus what the existing
/// todos and live sessions already say. Everything here is a recommendation the
/// sheet shows with its reason and lets you override -- picking all three by hand
/// on every task was the part that felt like busywork.
struct ATMTodoSuggestion: Equatable {
    var project: String
    var projectReason: String
    var priority: String
    var priorityReason: String

    static let empty = ATMTodoSuggestion(
        project: "",
        projectReason: "无历史项目可参考",
        priority: "P1",
        priorityReason: "默认",
    )

    /// Words that move a task off the default priority. They are matched against
    /// the raw text, so a recommendation can be wrong -- which is why the sheet
    /// shows it as a chip you can change rather than applying it silently.
    private static let urgentMarkers = [
        "紧急", "立刻", "马上", "尽快", "崩", "挂了", "故障", "线上", "阻塞", "卡住", "严重", "数据丢失",
        "blocker", "asap", "urgent", "outage",
    ]
    private static let deferrableMarkers = [
        "顺手", "有空", "以后", "后续", "欠账", "技术债", "不急", "暂时", "低优", "小问题",
        "later", "someday", "nice to have", "tech debt", "cleanup",
    ]

    static func infer(
        text: String,
        todos: [ATMTodo],
        liveSessions: [ATMLiveSession] = []
    ) -> ATMTodoSuggestion {
        let haystack = text.lowercased()
        var suggestion = empty

        let known = knownProjects(in: todos)
        if let mentioned = known.first(where: { haystack.contains($0.lowercased()) }) {
            suggestion.project = mentioned
            suggestion.projectReason = "文本提到 \(mentioned)"
        } else if let live = liveSessions
            .filter({ !$0.project.isEmpty })
            .min(by: { $0.ageSeconds < $1.ageSeconds }) {
            suggestion.project = matchKnown(live.project, in: known) ?? live.project
            suggestion.projectReason = "当前会话在 \(live.project)"
        } else if let recent = known.first {
            suggestion.project = recent
            suggestion.projectReason = "最近常用项目"
        }

        if let explicit = explicitPriority(in: haystack) {
            suggestion.priority = explicit
            suggestion.priorityReason = "文本写了 \(explicit)"
        } else if let marker = urgentMarkers.first(where: { haystack.contains($0) }) {
            suggestion.priority = "P0"
            suggestion.priorityReason = "文本提到「\(marker)」"
        } else if let marker = deferrableMarkers.first(where: { haystack.contains($0) }) {
            suggestion.priority = "P2"
            suggestion.priorityReason = "文本提到「\(marker)」"
        }


        return suggestion
    }

    /// Projects the user actually files todos under, most recently used first.
    /// Recency beats volume here: the project of this week's work is the better
    /// guess even when an older project has more todos.
    private static func knownProjects(in todos: [ATMTodo]) -> [String] {
        var latest: [String: String] = [:]
        for todo in todos {
            guard let project = todo.project?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !project.isEmpty else { continue }
            if let seen = latest[project], seen >= todo.created { continue }
            latest[project] = todo.created
        }
        return latest.keys.sorted {
            if latest[$0] != latest[$1] { return latest[$0, default: ""] > latest[$1, default: ""] }
            return $0 < $1
        }
    }

    /// Session projects and todo projects are not always spelled the same
    /// ("mox-atm" against "atm"), so fall back to a containment match before
    /// recommending a project name the user has never filed a todo under.
    private static func matchKnown(_ project: String, in known: [String]) -> String? {
        let value = project.lowercased()
        if let exact = known.first(where: { $0.lowercased() == value }) { return exact }
        return known.first { value.contains($0.lowercased()) || $0.lowercased().contains(value) }
    }

    /// A standalone "P0" is the user stating the priority; the word boundaries keep
    /// strings such as "top10" from being read as one.
    private static func explicitPriority(in haystack: String) -> String? {
        guard let range = haystack.range(of: "\\bp[012]\\b", options: .regularExpression) else { return nil }
        return haystack[range].uppercased()
    }

}
