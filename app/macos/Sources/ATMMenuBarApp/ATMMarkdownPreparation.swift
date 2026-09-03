import Foundation
import Combine

/// Prepared text contains no font or color attributes. Appearance changes only
/// restyle the views and can reuse exactly the same parsed document.
struct ATMPreparedMarkdown: Sendable {
    let source: String
    let blocks: [ATMMarkdownBlock]
    let inlineContent: [String: AttributedString]

    func inline(_ text: String) -> AttributedString {
        inlineContent[text] ?? AttributedString(text)
    }
}

/// A large table keeps normal Grid column measurement, but initially admits
/// only one page of rows. Page state cannot leak into a different document.
struct ATMMarkdownTablePaging {
    static let pageSize = 100
    private var source: String?
    private var visibleCounts: [Int: Int] = [:]

    func visibleCount(table: Int, source: String, total: Int) -> Int {
        let requested = self.source == source ? visibleCounts[table] ?? Self.pageSize : Self.pageSize
        return min(total, requested)
    }

    mutating func showMore(table: Int, source: String, total: Int) {
        let current = visibleCount(table: table, source: source, total: total)
        if self.source != source {
            visibleCounts.removeAll(keepingCapacity: true)
            self.source = source
        }
        visibleCounts[table] = min(total, current + Self.pageSize)
    }
}

extension ATMMarkdown {
    private static let documentCache = ATMBoundedContentCache<String, ATMPreparedMarkdown>(
        countLimit: 96, costLimit: 16 * 1_024 * 1_024
    )
    private static let preparation = ATMMarkdownPreparationWorker()

    static func cachedDocument(_ source: String) -> ATMPreparedMarkdown? {
        documentCache.value(for: source)
    }

    static func prepareDocument(_ source: String) async -> ATMPreparedMarkdown? {
        if let cached = cachedDocument(source) { return cached }
        return await preparation.document(source)
    }

    static func prepareInline(_ source: String) async -> AttributedString? {
        if let cached = cachedRender(source) { return cached }
        return await preparation.inline(source)
    }

    fileprivate static func cacheDocument(_ document: ATMPreparedMarkdown) {
        documentCache.insert(
            document,
            for: document.source,
            cost: document.source.utf8.count * 10
                + document.blocks.count * 128
                + document.inlineContent.count * 256
        )
    }
}

/// One non-main executor bounds preparation concurrency. A canceled offscreen
/// reader is skipped before parsing, and again between inline blocks. Multiple
/// readers of one source reuse the result after the first preparation finishes.
private actor ATMMarkdownPreparationWorker {
    func document(_ source: String) -> ATMPreparedMarkdown? {
        guard !Task.isCancelled else { return nil }
        if let cached = ATMMarkdown.cachedDocument(source) { return cached }
        let blocks = ATMMarkdown.blocks(source)
        var inlineContent: [String: AttributedString] = [:]

        func prepare(_ text: String) {
            if inlineContent[text] == nil { inlineContent[text] = ATMMarkdown.render(text) }
        }

        for block in blocks {
            guard !Task.isCancelled else { return nil }
            switch block {
            case .heading(_, let text), .paragraph(let text), .quote(let text):
                prepare(text)
            case .list(_, let items):
                for text in items {
                    guard !Task.isCancelled else { return nil }
                    prepare(text)
                }
            case .taskList(let items):
                for item in items {
                    guard !Task.isCancelled else { return nil }
                    prepare(item.text)
                }
            case .table(let headers, _, let rows):
                for text in headers { prepare(text) }
                for row in rows {
                    guard !Task.isCancelled else { return nil }
                    for text in row { prepare(text) }
                }
            case .divider, .code:
                break
            }
        }
        guard !Task.isCancelled else { return nil }
        let document = ATMPreparedMarkdown(source: source, blocks: blocks, inlineContent: inlineContent)
        ATMMarkdown.cacheDocument(document)
        return document
    }

    func inline(_ source: String) -> AttributedString? {
        guard !Task.isCancelled else { return nil }
        return ATMMarkdown.render(source)
    }
}

/// Survives ordinary SwiftUI body recomputations. The revision also guards
/// against a slow prior request completing after a reader switches documents.
@MainActor
final class ATMMarkdownDocumentModel: ObservableObject {
    typealias Loader = (String) async -> ATMPreparedMarkdown?

    @Published private(set) var document: ATMPreparedMarkdown?
    private var revision: UInt64 = 0

    init(source: String) {
        document = ATMMarkdown.cachedDocument(source)
    }

    func load(
        _ source: String,
        using loader: Loader = { await ATMMarkdown.prepareDocument($0) }
    ) async {
        revision &+= 1
        let requestedRevision = revision
        guard document?.source != source else { return }
        let prepared = await loader(source)
        guard !Task.isCancelled, revision == requestedRevision, prepared?.source == source else { return }
        document = prepared
    }
}
