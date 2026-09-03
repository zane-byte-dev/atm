import XCTest
@testable import ATMMenuBarApp

final class ATMTextInjectorTests: XCTestCase {
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
