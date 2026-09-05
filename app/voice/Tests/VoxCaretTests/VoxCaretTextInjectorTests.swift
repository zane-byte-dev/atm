import XCTest
@testable import VoxCaret

final class VoxCaretTextInjectorTests: XCTestCase {
    func testLiveReplacementSelectsThePreviousPreviewByGrapheme() {
        let plan = VoxCaretTextInjector.LiveReplacementPlan(
            previous: "你好👋",
            replacement: "你好，世界"
        )
        XCTAssertEqual(plan.selectionLength, 3)
        XCTAssertEqual(plan.replacement, "你好，世界")
    }

    func testLiveRollbackSelectsEverythingAndReplacesWithNothing() {
        let plan = VoxCaretTextInjector.LiveReplacementPlan(
            previous: "实时预览",
            replacement: ""
        )
        XCTAssertEqual(plan.selectionLength, 4)
        XCTAssertTrue(plan.replacement.isEmpty)
    }

    @MainActor
    func testLiveSessionCoalescesPartialsAndReconcilesTheFinalText() async throws {
        var edits: [(String, String)] = []
        let session = VoxCaretLiveInsertionSession(debounce: .milliseconds(20)) { previous, replacement in
            edits.append((previous, replacement))
            return .injected
        }

        session.submit("你")
        session.submit("你好")
        session.submit("你好世")
        try await Task.sleep(for: .milliseconds(45))
        let outcome = try await session.finish(with: "你好，世界")

        XCTAssertEqual(outcome, .injected)
        XCTAssertEqual(edits.count, 2)
        XCTAssertEqual(edits[0].0, "")
        XCTAssertEqual(edits[0].1, "你好世")
        XCTAssertEqual(edits[1].0, "你好世")
        XCTAssertEqual(edits[1].1, "你好，世界")
    }

    @MainActor
    func testLiveSessionRollbackRemovesRenderedPreview() async throws {
        var edits: [(String, String)] = []
        let session = VoxCaretLiveInsertionSession(debounce: .milliseconds(5)) { previous, replacement in
            edits.append((previous, replacement))
            return .injected
        }

        session.submit("会被撤回")
        try await Task.sleep(for: .milliseconds(25))
        await session.rollback()

        XCTAssertFalse(session.hasRenderedText)
        XCTAssertEqual(edits.count, 2)
        XCTAssertEqual(edits[0].0, "")
        XCTAssertEqual(edits[0].1, "会被撤回")
        XCTAssertEqual(edits[1].0, "会被撤回")
        XCTAssertEqual(edits[1].1, "")
    }

    @MainActor
    func testGenerationChangeDuringActivationNeverDispatchesPaste() async {
        var current = true
        var activated = false
        do {
            try await VoxCaretInjectionGate.perform(activate: { activated = true }, waitUntilReady: { current = false },
                isCurrent: { current }, isTargetFocused: { true }, paste: { XCTFail("stale transcript pasted") })
            XCTFail("superseded injection accepted")
        } catch is CancellationError { } catch { XCTFail("Unexpected \(error)") }
        XCTAssertTrue(activated)
    }

    @MainActor
    func testFocusChangeDuringActivationNeverDispatchesPaste() async {
        do {
            try await VoxCaretInjectionGate.perform(activate: {}, waitUntilReady: {}, isCurrent: { true },
                isTargetFocused: { false }, paste: { XCTFail("pasted into changed target") })
            XCTFail("focus change accepted")
        } catch VoxCaretTextInjector.Failure.targetApplicationUnavailable { } catch { XCTFail("Unexpected \(error)") }
    }

    @MainActor
    func testPasteKeyHoldYieldsTheMainActorBetweenDownAndUp() async {
        var events: [String] = []
        await VoxCaretPasteKeySequence.perform(
            keyDown: {
                events.append("down")
                Task { @MainActor in events.append("main actor work") }
            },
            keyUp: { events.append("up") }
        )
        XCTAssertEqual(events, ["down", "main actor work", "up"])
    }

    @MainActor
    func testCancelledPasteStillReleasesKeyAfterAsynchronousHold() async {
        var events: [String] = []
        let task = Task { @MainActor in
            await VoxCaretPasteKeySequence.perform(
                keyDown: {
                    events.append("down")
                    Task { @MainActor in events.append("main actor work") }
                },
                keyUp: { events.append("up") }
            )
        }
        task.cancel()
        await task.value
        XCTAssertEqual(events, ["down", "main actor work", "up"])
    }
}
