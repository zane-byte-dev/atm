import XCTest
@testable import ATMVoice

final class ATMTextInjectorTests: XCTestCase {
    @MainActor
    func testGenerationChangeDuringActivationNeverDispatchesPaste() async {
        var current = true
        var activated = false
        do {
            try await ATMInjectionGate.perform(activate: { activated = true }, waitUntilReady: { current = false },
                isCurrent: { current }, isTargetFocused: { true }, paste: { XCTFail("stale transcript pasted") })
            XCTFail("superseded injection accepted")
        } catch is CancellationError { } catch { XCTFail("Unexpected \(error)") }
        XCTAssertTrue(activated)
    }

    @MainActor
    func testFocusChangeDuringActivationNeverDispatchesPaste() async {
        do {
            try await ATMInjectionGate.perform(activate: {}, waitUntilReady: {}, isCurrent: { true },
                isTargetFocused: { false }, paste: { XCTFail("pasted into changed target") })
            XCTFail("focus change accepted")
        } catch ATMTextInjector.Failure.targetApplicationUnavailable { } catch { XCTFail("Unexpected \(error)") }
    }

    @MainActor
    func testPasteKeyHoldYieldsTheMainActorBetweenDownAndUp() async {
        var events: [String] = []
        await ATMPasteKeySequence.perform(
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
            await ATMPasteKeySequence.perform(
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
