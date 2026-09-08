import XCTest
@testable import VoxCaret

@MainActor
final class VoxCaretLiveTextTargetTests: XCTestCase {
    func testCorrectionsAndRollbackPreserveSurroundingsAndOriginalSelection() async throws {
        let field = LiveTestField(text: "前👋原文后", selection: NSRange(location: 3, length: 2))
        let target = try XCTUnwrap(field.capture())
        try await target.replace(previous: "", with: "你好👨‍👩‍👧‍👦", isCurrent: { true }, commit: field.commit)
        XCTAssertEqual(field.text, "前👋你好👨‍👩‍👧‍👦后")
        try await target.replace(previous: "你好👨‍👩‍👧‍👦", with: "您好🌍", isCurrent: { true }, commit: field.commit)
        XCTAssertEqual(field.text, "前👋您好🌍后")
        try await target.replace(previous: "您好🌍", with: target.originalSelection, isCurrent: { true }, commit: field.commit)
        XCTAssertEqual(field.text, "前👋原文后")
        XCTAssertEqual(field.commitCount, 3)
    }

    func testDifferentControlInTheSameAppNeverReceivesAnEdit() async throws {
        let field = LiveTestField(text: "原文字", selection: NSRange(location: 3, length: 0))
        let target = try XCTUnwrap(field.capture())
        field.isFocusedElement = false
        await assertRejected(target, field: field, previous: "", replacement: "语音")
        XCTAssertEqual(field.selectionCount, 0)
        XCTAssertEqual(field.text, "原文字")
    }

    func testCursorMoveBlocksCorrectionAndRollbackEvenIfItReturnsLater() async throws {
        let field = LiveTestField(text: "原文字", selection: NSRange(location: 3, length: 0))
        let target = try XCTUnwrap(field.capture())
        try await target.replace(previous: "", with: "语音", isCurrent: { true }, commit: field.commit)
        field.selection = NSRange(location: 1, length: 0)
        await assertRejected(target, field: field, previous: "语音", replacement: "更正")
        field.selection = NSRange(location: 5, length: 0)
        await assertRejected(target, field: field, previous: "语音", replacement: "")
        XCTAssertEqual(field.commitCount, 1)
        XCTAssertEqual(field.text, "原文字语音")
    }

    func testSameLengthExternalEditInvalidatesAnchor() async throws {
        let field = LiveTestField(text: "原文字", selection: NSRange(location: 3, length: 0))
        let target = try XCTUnwrap(field.capture())
        field.text = "新文字"
        await assertRejected(target, field: field, previous: "", replacement: "语音")
        XCTAssertEqual(field.text, "新文字")
    }

    func testFocusChangeDuringSelectionDoesNotDispatchPaste() async throws {
        let field = LiveTestField(text: "文字", selection: NSRange(location: 2, length: 0))
        field.onSelect = { field.isFocusedElement = false }
        let target = try XCTUnwrap(field.capture())
        await assertRejected(target, field: field, previous: "", replacement: "语音")
        XCTAssertEqual(field.commitCount, 0)
    }

    func testUnchangedFinalTextStillChecksTheAnchor() async throws {
        let field = LiveTestField(text: "", selection: NSRange(location: 0, length: 0))
        let target = try XCTUnwrap(field.capture())
        try await target.replace(previous: "", with: "语音", isCurrent: { true }, commit: field.commit)
        field.selection = NSRange(location: 0, length: 0)
        await assertRejected(target, field: field, previous: "语音", replacement: "语音")
        XCTAssertEqual(field.commitCount, 1)
    }

    func testUnverifiableFirstInsertionCopiesFinalWithoutUsingAnotherInput() async throws {
        var copied: [String] = []
        let session = VoxCaretLiveInsertionSession(debounce: .zero, replace: { _, _ in
            throw VoxCaretTextInjector.Failure.livePreviewUnavailable
        }, copyFinalText: { text in copied.append(text); return .copiedForChangedTarget })
        let outcome = try await session.finish(with: "完整转写")
        XCTAssertEqual(outcome, .copiedForChangedTarget)
        XCTAssertEqual(copied, ["完整转写"])
        XCTAssertEqual(session.latestTranscript, "完整转写")
    }

    func testRollbackRefusesChangedTargetAndRetainsTranscript() async throws {
        let field = LiveTestField(text: "原文", selection: NSRange(location: 2, length: 0))
        let target = try XCTUnwrap(field.capture())
        let session = VoxCaretLiveInsertionSession(debounce: .zero, replace: { previous, replacement in
            try await target.replace(previous: previous, with: replacement, isCurrent: { true }, commit: field.commit)
            return .injected
        })
        _ = try await session.finish(with: "转写")
        field.isFocusedElement = false
        let restored = await session.rollback()
        XCTAssertFalse(restored)
        XCTAssertEqual(field.text, "原文转写")
        XCTAssertEqual(field.commitCount, 1)
        XCTAssertEqual(session.latestTranscript, "转写")
    }

    func testInvalidAXRangesAreRejectedWithoutSubstringAccess() {
        for range in [NSRange(location: NSNotFound, length: 0), NSRange(location: 1, length: 20)] {
            let field = LiveTestField(text: "字", selection: range)
            XCTAssertNil(field.capture())
        }
    }

    func testRollbackWaitsForTheFirstInFlightPreview() async throws {
        let started = LiveTestSignal()
        let release = LiveTestSignal()
        var edits: [(String, String)] = []
        let session = VoxCaretLiveInsertionSession(debounce: .zero, replace: { previous, replacement in
            edits.append((previous, replacement))
            if edits.count == 1 {
                started.signal()
                await release.wait()
            }
            return .injected
        })
        session.submit("刚开始的预览")
        await started.wait()
        XCTAssertEqual(session.renderedText, "")
        let rollback = Task { await session.rollback() }
        release.signal()
        let restored = await rollback.value
        XCTAssertTrue(restored)
        XCTAssertEqual(edits.count, 2)
        XCTAssertEqual(edits[1].0, "刚开始的预览")
        XCTAssertEqual(edits[1].1, "")
        XCTAssertEqual(session.renderedText, "")
    }

    func testEscDuringFinalFirstPasteWaitsAndRemovesTheCommittedText() async throws {
        let started = LiveTestSignal()
        let release = LiveTestSignal()
        var edits: [(String, String)] = []
        let session = VoxCaretLiveInsertionSession(debounce: .zero, replace: { previous, replacement in
            edits.append((previous, replacement))
            if edits.count == 1 {
                started.signal()
                await release.wait()
            }
            return .injected
        }, copyFinalText: { _ in XCTFail("Cancelled final text copied"); return .copiedForChangedTarget })
        let finishing = Task { try await session.finish(with: "最终文字") }
        await started.wait()
        finishing.cancel()
        let rollback = Task { await session.rollback() }
        release.signal()
        _ = try? await finishing.value
        let restored = await rollback.value
        XCTAssertTrue(restored)
        XCTAssertEqual(edits.count, 2)
        XCTAssertEqual(edits[1].0, "最终文字")
        XCTAssertEqual(edits[1].1, "")
        XCTAssertTrue(session.renderedText.isEmpty)
    }

    func testCancelledFinalPasteFailureDoesNotCopyTheTranscript() async throws {
        let started = LiveTestSignal()
        let release = LiveTestSignal()
        let session = VoxCaretLiveInsertionSession(debounce: .zero, replace: { _, _ in
            started.signal()
            await release.wait()
            throw VoxCaretTextInjector.Failure.livePreviewUnavailable
        }, copyFinalText: { _ in XCTFail("Cancelled final text copied"); return .copiedForChangedTarget })
        let finishing = Task { try await session.finish(with: "取消的文字") }
        await started.wait()
        finishing.cancel()
        release.signal()
        do {
            _ = try await finishing.value
            XCTFail("Cancelled finish succeeded")
        } catch is CancellationError {
        } catch { XCTFail("Unexpected error: \(error)") }
        let restored = await session.rollback()
        XCTAssertFalse(restored)
        XCTAssertEqual(session.latestTranscript, "取消的文字")
    }

    private func assertRejected(_ target: VoxCaretLiveTextTarget, field: LiveTestField,
                                previous: String, replacement: String) async {
        let before = field.commitCount
        do {
            try await target.replace(previous: previous, with: replacement, isCurrent: { true }, commit: field.commit)
            XCTFail("Unverified target accepted")
        } catch VoxCaretTextInjector.Failure.livePreviewUnavailable {
        } catch { XCTFail("Unexpected error: \(error)") }
        XCTAssertEqual(field.commitCount, before)
    }
}

@MainActor
private final class LiveTestSignal {
    private var signalled = false
    private var continuation: CheckedContinuation<Void, Never>?

    func wait() async {
        if signalled { return }
        await withCheckedContinuation { continuation = $0 }
    }

    func signal() {
        signalled = true
        continuation?.resume()
        continuation = nil
    }
}

@MainActor
private final class LiveTestField {
    var text: String
    var selection: NSRange
    var isFocusedElement = true
    var onSelect: (() -> Void)?
    var selectionCount = 0
    var commitCount = 0

    init(text: String, selection: NSRange) { self.text = text; self.selection = selection }

    func capture() -> VoxCaretLiveTextTarget? {
        VoxCaretLiveTextTarget(snapshot: .init(text: text, selection: selection), read: {
            self.isFocusedElement ? .init(text: self.text, selection: self.selection) : nil
        }, select: { range in
            self.selectionCount += 1
            self.selection = range
            self.onSelect?()
            return true
        })
    }

    func commit(_ replacement: String) async throws {
        commitCount += 1
        text = (text as NSString).replacingCharacters(in: selection, with: replacement)
        selection = NSRange(location: selection.location + replacement.utf16.count, length: 0)
    }
}
