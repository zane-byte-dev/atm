import XCTest
@testable import ATMMenuBarApp

final class MarkdownPreparationTests: XCTestCase {
    func testContentCacheEvictsLeastRecentlyReadEntry() {
        let cache = ATMBoundedContentCache<String, String>(countLimit: 2, costLimit: 100)
        cache.insert("A", for: "a", cost: 10)
        cache.insert("B", for: "b", cost: 10)
        XCTAssertEqual(cache.value(for: "a"), "A")
        cache.insert("C", for: "c", cost: 10)

        XCTAssertNil(cache.value(for: "b"))
        XCTAssertEqual(cache.value(for: "a"), "A")
        XCTAssertEqual(cache.value(for: "c"), "C")
    }

    func testContentCacheEnforcesByteBudgetAndSkipsOversizedDocuments() {
        let cache = ATMBoundedContentCache<String, String>(countLimit: 20, costLimit: 10)
        cache.insert("A", for: "a", cost: 6)
        cache.insert("B", for: "b", cost: 6)
        XCTAssertNil(cache.value(for: "a"))

        cache.insert("huge", for: "huge", cost: 11)
        XCTAssertNil(cache.value(for: "huge"))
        XCTAssertEqual(cache.value(for: "b"), "B")

        cache.insert("B2", for: "b", cost: 3)
        cache.insert("C", for: "c", cost: 7)
        XCTAssertEqual(cache.value(for: "b"), "B2")
        XCTAssertEqual(cache.value(for: "c"), "C")
    }

    func testPreparedDocumentRetainsRichTextTablesAndLiteralCode() async throws {
        let source = """
        ## **Heading**

        Visit [ATM](https://example.com) and `internal.example.test`.

        - [x] **Ready**

        | Name | Count |
        |:---|---:|
        | `a|b` | **2** |

        ```swift
        let literal = "**not bold**"
        ```
        """
        let result = await ATMMarkdown.prepareDocument(source)
        let document = try XCTUnwrap(result)
        XCTAssertEqual(document.blocks, [
            .heading(level: 2, text: "**Heading**"),
            .paragraph("Visit [ATM](https://example.com) and `internal.example.test`."),
            .taskList(items: [.init(checked: true, text: "**Ready**")]),
            .table(headers: ["Name", "Count"], alignments: [.leading, .trailing], rows: [["`a|b`", "**2**"]]),
            .code(language: "swift", content: "let literal = \"**not bold**\"")
        ])
        XCTAssertEqual(String(document.inline("**Heading**").characters), "Heading")
        XCTAssertEqual(String(document.inline("`a|b`").characters), "a|b")
        XCTAssertEqual(String(document.inline("**2**").characters), "2")
        XCTAssertEqual(
            document.inline("Visit [ATM](https://example.com) and `internal.example.test`.").runs.compactMap(\.link),
            [URL(string: "https://example.com")]
        )
        XCTAssertNil(document.inlineContent["let literal = \"**not bold**\""])
        XCTAssertEqual(ATMMarkdown.cachedDocument(source)?.blocks, document.blocks)
    }

    func testMarkdownCodeScannerKeepsLinksAfterSeveralCodeSpans() {
        let source = """
        https://first.example.test `https://hidden.example.test`
        ``https://also-hidden.example.test `inner` `` https://second.example.test

        ```text
        https://fenced.example.test
        ```

        https://last.example.test
        """
        XCTAssertEqual(ATMMarkdown.render(source).runs.compactMap(\.link), [
            URL(string: "https://first.example.test"),
            URL(string: "https://second.example.test"),
            URL(string: "https://last.example.test")
        ])
    }

    func testCachedSummariesKeepTheirRequestedLimit() {
        let source = "**The complete sentence stays available for a wider preview.**"
        XCTAssertEqual(ATMMarkdown.plainSummary(source, limit: 12), "The complete…")
        XCTAssertEqual(
            ATMMarkdown.plainSummary(source, limit: 100),
            "The complete sentence stays available for a wider preview."
        )
        XCTAssertEqual(ATMMarkdown.plainSummary(source, limit: 12), "The complete…")
    }

    func testLargeTablePagingKeepsRowsReachableAndResetsForAnotherDocument() {
        var paging = ATMMarkdownTablePaging()
        XCTAssertEqual(paging.visibleCount(table: 0, source: "first", total: 20), 20)
        XCTAssertEqual(paging.visibleCount(table: 0, source: "first", total: 225), 100)
        paging.showMore(table: 0, source: "first", total: 225)
        XCTAssertEqual(paging.visibleCount(table: 0, source: "first", total: 225), 200)
        XCTAssertEqual(paging.visibleCount(table: 1, source: "first", total: 225), 100)
        paging.showMore(table: 0, source: "first", total: 225)
        XCTAssertEqual(paging.visibleCount(table: 0, source: "first", total: 225), 225)
        XCTAssertEqual(paging.visibleCount(table: 0, source: "second", total: 225), 100)
    }

    @MainActor
    func testOldDocumentCannotReplaceNewSelection() async {
        let model = ATMMarkdownDocumentModel(source: "old-selection")
        let delayed = SuspendedMarkdownLoad()
        let old = Task {
            await model.load("old-selection") { _ in await delayed.value() }
        }
        await delayed.waitUntilStarted()
        let current = ATMPreparedMarkdown(source: "new-selection", blocks: [.paragraph("new")], inlineContent: [:])
        await model.load("new-selection") { _ in current }
        await delayed.finish(with: ATMPreparedMarkdown(source: "old-selection", blocks: [.paragraph("old")], inlineContent: [:]))
        await old.value

        XCTAssertEqual(model.document?.source, "new-selection")
    }

    @MainActor
    func testCanceledReaderDoesNotPublishItsPreparedDocument() async {
        let model = ATMMarkdownDocumentModel(source: "canceled-selection")
        let delayed = SuspendedMarkdownLoad()
        let reading = Task {
            await model.load("canceled-selection") { _ in await delayed.value() }
        }
        await delayed.waitUntilStarted()
        reading.cancel()
        await delayed.finish(with: ATMPreparedMarkdown(source: "canceled-selection", blocks: [], inlineContent: [:]))
        await reading.value

        XCTAssertNil(model.document)
    }
}

private actor SuspendedMarkdownLoad {
    private var result: CheckedContinuation<ATMPreparedMarkdown?, Never>?
    private var started: CheckedContinuation<Void, Never>?

    func value() async -> ATMPreparedMarkdown? {
        await withCheckedContinuation { continuation in
            result = continuation
            started?.resume()
            started = nil
        }
    }

    func waitUntilStarted() async {
        guard result == nil else { return }
        await withCheckedContinuation { started = $0 }
    }

    func finish(with document: ATMPreparedMarkdown) {
        result?.resume(returning: document)
        result = nil
    }
}
