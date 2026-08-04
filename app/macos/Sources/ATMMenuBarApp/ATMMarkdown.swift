import Foundation

struct ATMMarkdownTaskItem: Equatable {
    let checked: Bool
    let text: String
}

enum ATMMarkdownTableAlignment: Equatable {
    case leading
    case center
    case trailing
}

enum ATMMarkdownBlock: Equatable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case list(ordered: Bool, items: [String])
    case taskList(items: [ATMMarkdownTaskItem])
    case quote(String)
    case divider
    case code(language: String?, content: String)
    case table(
        headers: [String],
        alignments: [ATMMarkdownTableAlignment],
        rows: [[String]]
    )
}

enum ATMMarkdown {
    static func render(_ source: String) -> AttributedString {
        (try? AttributedString(
            markdown: protectBareLinks(in: source),
            options: AttributedString.MarkdownParsingOptions(interpretedSyntax: .full)
        )) ?? AttributedString(source)
    }

    static func blocks(_ source: String) -> [ATMMarkdownBlock] {
        var result: [ATMMarkdownBlock] = []
        var paragraph: [String] = []
        var listItems: [String] = []
        var listIsOrdered = false
        var taskItems: [ATMMarkdownTaskItem] = []
        var quoteLines: [String] = []
        var codeLines: [String] = []
        var codeLanguage: String?
        var isInsideCode = false

        func flushParagraph() {
            let value = paragraph.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty { result.append(.paragraph(value)) }
            paragraph.removeAll(keepingCapacity: true)
        }

        func flushList() {
            if !listItems.isEmpty { result.append(.list(ordered: listIsOrdered, items: listItems)) }
            listItems.removeAll(keepingCapacity: true)
        }

        func flushTaskList() {
            if !taskItems.isEmpty { result.append(.taskList(items: taskItems)) }
            taskItems.removeAll(keepingCapacity: true)
        }

        func flushQuote() {
            let value = quoteLines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty { result.append(.quote(value)) }
            quoteLines.removeAll(keepingCapacity: true)
        }

        func flushTextBlocks() {
            flushParagraph()
            flushList()
            flushTaskList()
            flushQuote()
        }

        func flushCode() {
            let value = codeLines.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !value.isEmpty { result.append(.code(language: codeLanguage, content: value)) }
            codeLines.removeAll(keepingCapacity: true)
            codeLanguage = nil
        }

        let lines = source.components(separatedBy: .newlines)
        var lineIndex = 0
        while lineIndex < lines.count {
            let currentIndex = lineIndex
            let line = lines[currentIndex]
            lineIndex += 1
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") {
                if isInsideCode {
                    flushCode()
                    isInsideCode = false
                } else {
                    flushTextBlocks()
                    let language = String(trimmed.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                    codeLanguage = language.isEmpty ? nil : language
                    isInsideCode = true
                }
                continue
            }
            if isInsideCode {
                codeLines.append(line)
                continue
            }

            if currentIndex + 1 < lines.count,
               let headers = tableCells(from: line),
               let alignments = tableDelimiter(
                   from: lines[currentIndex + 1],
                   columnCount: headers.count
               ) {
                flushTextBlocks()
                lineIndex = currentIndex + 2
                var rows: [[String]] = []
                while lineIndex < lines.count,
                      let cells = tableCells(from: lines[lineIndex]) {
                    var normalizedCells = cells
                    if normalizedCells.count < headers.count {
                        normalizedCells.append(
                            contentsOf: repeatElement("", count: headers.count - normalizedCells.count)
                        )
                    } else if normalizedCells.count > headers.count {
                        normalizedCells = Array(normalizedCells.prefix(headers.count))
                    }
                    rows.append(normalizedCells)
                    lineIndex += 1
                }
                result.append(.table(headers: headers, alignments: alignments, rows: rows))
                continue
            }

            if trimmed.isEmpty {
                flushTextBlocks()
                continue
            }

            if let heading = heading(from: trimmed) {
                flushTextBlocks()
                result.append(.heading(level: heading.level, text: heading.text))
                continue
            }

            if isDivider(trimmed) {
                flushTextBlocks()
                result.append(.divider)
                continue
            }

            if let task = taskItem(from: trimmed) {
                flushParagraph()
                flushQuote()
                flushList()
                taskItems.append(ATMMarkdownTaskItem(checked: task.checked, text: task.text))
                continue
            }

            if let item = listItem(from: trimmed) {
                flushParagraph()
                flushQuote()
                flushTaskList()
                if !listItems.isEmpty, listIsOrdered != item.ordered { flushList() }
                listIsOrdered = item.ordered
                listItems.append(item.text)
                continue
            }

            if trimmed.hasPrefix(">") {
                flushParagraph()
                flushList()
                flushTaskList()
                quoteLines.append(String(trimmed.dropFirst()).trimmingCharacters(in: .whitespaces))
                continue
            }

            flushList()
            flushTaskList()
            flushQuote()
            paragraph.append(line)
        }

        if isInsideCode {
            flushCode()
        } else {
            flushTextBlocks()
        }
        return result
    }

    static func plainSummary(_ source: String, limit: Int) -> String {
        let prose = blocks(source).compactMap { block -> String? in
            switch block {
            case .heading(_, let text), .paragraph(let text), .quote(let text): return text
            case .list(_, let items): return items.first
            case .taskList(let items): return items.first?.text
            case .table(let headers, _, _): return headers.joined(separator: " · ")
            case .divider, .code: return nil
            }
        }.first ?? source
        let firstParagraph = prose.components(separatedBy: "\n\n").first ?? prose
        let rendered = String(render(firstParagraph).characters)
            .replacingOccurrences(of: "\\\n", with: " ")
            .replacingOccurrences(of: "\n", with: " ")
            .split(whereSeparator: \.isWhitespace)
            .joined(separator: " ")
        guard rendered.count > limit else { return rendered }
        return String(rendered.prefix(limit)).trimmingCharacters(in: .whitespacesAndNewlines) + "…"
    }

    static func documentBody(_ source: String, removingTitle title: String) -> String {
        var lines = source.components(separatedBy: .newlines)
        while lines.first?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == true {
            lines.removeFirst()
        }

        if lines.first?.trimmingCharacters(in: .whitespacesAndNewlines) == "---",
           let closingIndex = lines.dropFirst().firstIndex(where: {
               $0.trimmingCharacters(in: .whitespacesAndNewlines) == "---"
           }) {
            lines.removeFirst(closingIndex + 1)
        }

        while lines.first?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == true {
            lines.removeFirst()
        }

        if let first = lines.first?.trimmingCharacters(in: .whitespaces), first.hasPrefix("# ") {
            let heading = String(first.dropFirst(2)).trimmingCharacters(in: .whitespacesAndNewlines)
            if normalizedTitle(heading) == normalizedTitle(title) {
                lines.removeFirst()
            }
        }

        return lines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func normalizedTitle(_ source: String) -> String {
        source.lowercased().unicodeScalars
            .filter { CharacterSet.alphanumerics.contains($0) }
            .map(String.init)
            .joined()
    }

    private static func heading(from line: String) -> (level: Int, text: String)? {
        let hashes = line.prefix { $0 == "#" }
        guard (1...6).contains(hashes.count), line.dropFirst(hashes.count).first == " " else { return nil }
        let text = String(line.dropFirst(hashes.count + 1)).trimmingCharacters(in: .whitespaces)
        return text.isEmpty ? nil : (hashes.count, text)
    }

    private static func isDivider(_ line: String) -> Bool {
        let compact = line.replacingOccurrences(of: " ", with: "")
        guard compact.count >= 3, let first = compact.first, ["-", "*", "_"].contains(first) else { return false }
        return compact.allSatisfy { $0 == first }
    }

    private static func taskItem(from line: String) -> (checked: Bool, text: String)? {
        for marker in ["- ", "* ", "+ "] where line.hasPrefix(marker) {
            let rest = String(line.dropFirst(marker.count))
            let lower = rest.lowercased()
            if lower.hasPrefix("[ ] ") || lower == "[ ]" {
                return (false, String(rest.dropFirst(3)).trimmingCharacters(in: .whitespaces))
            }
            if lower.hasPrefix("[x] ") || lower == "[x]" {
                return (true, String(rest.dropFirst(3)).trimmingCharacters(in: .whitespaces))
            }
            return nil
        }
        return nil
    }

    private static func listItem(from line: String) -> (ordered: Bool, text: String)? {
        for marker in ["- ", "* ", "+ "] where line.hasPrefix(marker) {
            return (false, String(line.dropFirst(marker.count)).trimmingCharacters(in: .whitespaces))
        }
        guard let separator = line.range(of: ". ") else { return nil }
        let prefix = line[..<separator.lowerBound]
        guard !prefix.isEmpty, prefix.allSatisfy(\.isNumber) else { return nil }
        return (true, String(line[separator.upperBound...]).trimmingCharacters(in: .whitespaces))
    }

    private static func tableDelimiter(
        from line: String,
        columnCount: Int
    ) -> [ATMMarkdownTableAlignment]? {
        guard let cells = tableCells(from: line), cells.count == columnCount else { return nil }
        var alignments: [ATMMarkdownTableAlignment] = []
        for cell in cells {
            let value = cell.trimmingCharacters(in: .whitespaces)
            let hasLeadingColon = value.hasPrefix(":")
            let hasTrailingColon = value.hasSuffix(":")
            let delimiter = value.trimmingCharacters(in: CharacterSet(charactersIn: ":"))
            guard delimiter.count >= 3, delimiter.allSatisfy({ $0 == "-" }) else { return nil }
            switch (hasLeadingColon, hasTrailingColon) {
            case (true, true):
                alignments.append(.center)
            case (false, true):
                alignments.append(.trailing)
            default:
                alignments.append(.leading)
            }
        }
        return alignments
    }

    private static func tableCells(from line: String) -> [String]? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.contains("|") else { return nil }

        let characters = Array(trimmed)
        var cells: [String] = []
        var current = ""
        var activeBacktickCount = 0
        var index = 0
        while index < characters.count {
            let character = characters[index]
            if character == "\\", index + 1 < characters.count {
                current.append(character)
                current.append(characters[index + 1])
                index += 2
                continue
            }
            if character == "`" {
                var runLength = 1
                while index + runLength < characters.count,
                      characters[index + runLength] == "`" {
                    runLength += 1
                }
                current.append(String(repeating: "`", count: runLength))
                if activeBacktickCount == 0 {
                    activeBacktickCount = runLength
                } else if activeBacktickCount == runLength {
                    activeBacktickCount = 0
                }
                index += runLength
                continue
            }
            if character == "|", activeBacktickCount == 0 {
                cells.append(current.trimmingCharacters(in: .whitespaces))
                current = ""
            } else {
                current.append(character)
            }
            index += 1
        }
        cells.append(current.trimmingCharacters(in: .whitespaces))

        if cells.first?.isEmpty == true {
            cells.removeFirst()
        }
        if cells.last?.isEmpty == true {
            cells.removeLast()
        }
        return cells.count >= 2 ? cells : nil
    }

    private static func protectBareLinks(in source: String) -> String {
        guard let detector = try? NSDataDetector(
            types: NSTextCheckingResult.CheckingType.link.rawValue
        ) else {
            return source
        }

        let original = source as NSString
        let mutable = NSMutableString(string: source)
        let matches = detector.matches(
            in: source,
            range: NSRange(location: 0, length: original.length)
        )

        for match in matches.reversed() where match.resultType == .link {
            guard !isInsideMarkdownCode(at: match.range.location, in: original) else { continue }
            let previousCharacter = match.range.location > 0
                ? original.substring(with: NSRange(location: match.range.location - 1, length: 1))
                : ""
            guard previousCharacter != "(", previousCharacter != "<" else { continue }

            let visibleURL = original.substring(with: match.range)
            mutable.replaceCharacters(in: match.range, with: "<\(visibleURL)>")
        }
        return mutable as String
    }

    private static func isInsideMarkdownCode(at offset: Int, in source: NSString) -> Bool {
        var activeBacktickCount = 0
        var index = 0
        while index < offset {
            let character = source.character(at: index)
            if character == 92, index + 1 < offset {
                index += 2
                continue
            }
            if character == 96 {
                var runLength = 1
                while index + runLength < offset,
                      source.character(at: index + runLength) == 96 {
                    runLength += 1
                }
                if activeBacktickCount == 0 {
                    activeBacktickCount = runLength
                } else if activeBacktickCount == runLength {
                    activeBacktickCount = 0
                }
                index += runLength
                continue
            }
            index += 1
        }
        return activeBacktickCount != 0
    }
}
