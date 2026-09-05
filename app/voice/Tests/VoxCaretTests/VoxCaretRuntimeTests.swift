import XCTest
@testable import VoxCaret

/// Dictation depends on two things that are not code: a real `.app` bundle, and TCC
/// actually answering an authorization request. Both fail silently — no crash, no
/// error, just a permission dialog that never appears and a callback that never
/// arrives — so the guards against them are worth pinning down.
final class VoxCaretRuntimeTests: XCTestCase {
    /// The test bundle is an `.xctest`, not an `.app`, which is exactly the shape
    /// `swift run` produces. So this asserts the detection is actually looking at the
    /// bundle rather than returning a constant.
    func testBundleDetectionRecognizesANonAppProcess() {
        XCTAssertFalse(VoxCaretAppBundle.isBundled)
        XCTAssertNotEqual(Bundle.main.bundleURL.pathExtension, "app")
    }

    @MainActor
    func testMenuBarMarkIsATemplateImage() {
        let icon = VoxCaretBrand.statusIcon()
        XCTAssertEqual(icon.size, NSSize(width: 18, height: 18))
        XCTAssertTrue(icon.isTemplate)
        XCTAssertEqual(icon.accessibilityDescription, VoxCaretBrand.displayName)
    }

    /// Both preconditions need to say what to do about them, not just that something is
    /// wrong: neither is discoverable by looking at the UI.
    func testPreconditionErrorsNameTheFix() {
        let bundleMessage = VoxCaretSpeechTranscriberError.requiresAppBundle.localizedDescription
        XCTAssertTrue(bundleMessage.contains("app/voice/Scripts/build-app.sh"), bundleMessage)
        XCTAssertTrue(bundleMessage.contains("swift run"), bundleMessage)

        let timeoutMessage = VoxCaretSpeechTranscriberError.authorizationTimedOut.localizedDescription
        XCTAssertFalse(timeoutMessage.isEmpty)
        XCTAssertTrue(timeoutMessage.contains("系统设置"), timeoutMessage)
    }

    func testEveryTranscriberErrorHasAMessage() {
        let errors: [VoxCaretSpeechTranscriberError] = [
            .requiresAppBundle,
            .authorizationTimedOut,
            .microphoneDenied,
            .speechRecognitionDenied,
            .recognizerUnavailable,
            .onDeviceRecognitionUnavailable,
            .invalidAudioInput,
        ]

        for error in errors {
            XCTAssertFalse(
                error.localizedDescription.isEmpty,
                "\(error) 没有可展示的说明"
            )
        }
    }

    func testProductionBundleDeclaresMicrophoneAccess() throws {
        let resources = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("Resources", isDirectory: true)
        let info = try XCTUnwrap(
            NSDictionary(contentsOf: resources.appendingPathComponent("Info.plist"))
        )
        let entitlements = try XCTUnwrap(
            NSDictionary(contentsOf: resources.appendingPathComponent("VoxCaret.entitlements"))
        )

        XCTAssertFalse((info["NSMicrophoneUsageDescription"] as? String ?? "").isEmpty)
        XCTAssertFalse((info["NSSpeechRecognitionUsageDescription"] as? String ?? "").isEmpty)
        XCTAssertEqual(info["CFBundleDisplayName"] as? String, "VoxCaret")
        XCTAssertEqual(info["CFBundleExecutable"] as? String, "VoxCaret")
        XCTAssertEqual(info["CFBundleIdentifier"] as? String, "dev.zanebyte.voxcaret")
        XCTAssertEqual(info["CFBundleIconFile"] as? String, "AppIcon")
        XCTAssertTrue(
            FileManager.default.fileExists(atPath: resources.appendingPathComponent("AppIcon.png").path)
        )
        XCTAssertTrue(
            FileManager.default.fileExists(
                atPath: resources.appendingPathComponent("Assets.xcassets/AppIcon.appiconset/Contents.json").path
            )
        )
        let simplifiedChineseName = try String(
            contentsOf: resources.appendingPathComponent("zh-Hans.lproj/InfoPlist.strings"),
            encoding: .utf8
        )
        XCTAssertTrue(simplifiedChineseName.contains("\"声标\""))
        XCTAssertEqual(entitlements["com.apple.security.device.audio-input"] as? Bool, true)
    }

    // MARK: - Authorization deadline

    func testAuthorizationReturnsTheAnswerWhenTheCallbackArrives() async {
        let granted = await VoxCaretAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(true)
        }
        XCTAssertEqual(granted, true)

        let denied = await VoxCaretAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(false)
        }
        XCTAssertEqual(denied, false)
    }

    /// The case that caused the hang: a request whose callback never fires, because the
    /// dialog it was waiting on never appeared. `nil` rather than a thrown error, so the
    /// caller decides what a missing answer means.
    func testAuthorizationTimesOutWhenTheCallbackNeverArrives() async {
        let answer = await VoxCaretAuthorizationRequest.run(timeout: .milliseconds(120)) { _ in
            // Deliberately drops the completion handler on the floor.
        }
        XCTAssertNil(answer)
    }

    /// A late callback must not resume the continuation a second time — that traps and
    /// takes the process with it.
    func testLateCallbackAfterTimeoutIsIgnored() async {
        let box = LateCompletion()
        let answer = await VoxCaretAuthorizationRequest.run(timeout: .milliseconds(100)) { completion in
            box.store(completion)
        }
        XCTAssertNil(answer)

        // Would crash if the timeout had not already claimed the continuation.
        box.fire(true)
        box.fire(false)
        try? await Task.sleep(for: .milliseconds(50))
    }

    /// The same guarantee from the other direction: an API that calls back more than
    /// once must not be able to trap either.
    func testRepeatedCallbackYieldsOnlyTheFirstAnswer() async {
        let answer = await VoxCaretAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(true)
            completion(false)
            completion(true)
        }
        XCTAssertEqual(answer, true)
    }

    /// A dialog someone has to find and click needs a generous deadline; too short and
    /// a real grant gets thrown away.
    func testDefaultDeadlineLeavesTimeToClickTheDialog() {
        XCTAssertGreaterThanOrEqual(VoxCaretAuthorizationRequest.timeout, .seconds(30))
        XCTAssertLessThanOrEqual(VoxCaretAuthorizationRequest.timeout, .seconds(120))
    }

    // MARK: - Recognition completion

    func testFinalResultWaiterReturnsTheFinalTranscript() async {
        let results = AsyncStream<String>.makeStream()
        results.continuation.yield("完整句尾")
        results.continuation.finish()

        let value = await VoxCaretFinalResultWaiter.first(
            in: results.stream,
            timeout: .seconds(1)
        )

        XCTAssertEqual(value, "完整句尾")
    }

    func testFinalResultWaiterTimesOutWhenSpeechNeverFinishes() async {
        let results = AsyncStream<String>.makeStream()

        let value = await VoxCaretFinalResultWaiter.first(
            in: results.stream,
            timeout: .milliseconds(80)
        )

        XCTAssertNil(value)
        results.continuation.finish()
    }

    func testSpeechSegmentsJoinChineseWithoutSpaces() {
        XCTAssertEqual(VoxCaretTranscriptSegments.merge("今天去杭州", "出差"), "今天去杭州出差")
    }

    func testSpeechSegmentsJoinEnglishWithAWordBoundary() {
        XCTAssertEqual(VoxCaretTranscriptSegments.merge("hello", "world"), "hello world")
    }

    func testSpeechSegmentsRemoveShortOverlap() {
        XCTAssertEqual(VoxCaretTranscriptSegments.merge("今天去杭州", "杭州出差"), "今天去杭州出差")
    }

    func testSpeechSegmentThatAlreadyContainsHistoryWins() {
        XCTAssertEqual(VoxCaretTranscriptSegments.merge("前半句", "前半句后半句"), "前半句后半句")
    }

    @MainActor
    func testShortRightCommandPressDoesNothing() async {
        var events: [String] = []
        let gesture = VoxCaretLongPressGesture(
            threshold: .milliseconds(80),
            onLongPress: { events.append("start") },
            onReleaseAfterLongPress: { events.append("finish") }
        )

        gesture.press()
        try? await Task.sleep(for: .milliseconds(20))
        gesture.release()
        try? await Task.sleep(for: .milliseconds(90))

        XCTAssertEqual(events, [])
    }

    @MainActor
    func testLongRightCommandPressStartsAndReleaseFinishes() async {
        var events: [String] = []
        let gesture = VoxCaretLongPressGesture(
            threshold: .milliseconds(20),
            onLongPress: { events.append("start") },
            onReleaseAfterLongPress: { events.append("finish") }
        )

        gesture.press()
        try? await Task.sleep(for: .milliseconds(50))
        gesture.release()

        XCTAssertEqual(events, ["start", "finish"])
    }

    // MARK: - Recording isolation

    @MainActor
    func testCancelledRecordingCleanupCannotClearTheNextTranscriber() {
        let first = FakeSpeechTranscriber(name: "first")
        let second = FakeSpeechTranscriber(name: "second")
        var pending = [first, second]
        let sessions = VoxCaretTranscriberSessions {
            pending.removeFirst()
        }

        let stale = sessions.begin()
        sessions.cancelCurrent()
        XCTAssertEqual(first.cancelCount, 1)

        let current = sessions.begin()
        sessions.clearIfCurrent(stale)
        XCTAssertTrue(sessions.active === current)
        XCTAssertTrue(current === second)

        sessions.clearIfCurrent(current)
        XCTAssertNil(sessions.active)
    }
}

/// Holds a completion handler so the test can fire it after the deadline has passed.
private final class LateCompletion: @unchecked Sendable {
    private let lock = NSLock()
    private var completion: (@Sendable (Bool) -> Void)?

    func store(_ completion: @escaping @Sendable (Bool) -> Void) {
        lock.lock()
        self.completion = completion
        lock.unlock()
    }

    func fire(_ value: Bool) {
        lock.lock()
        let completion = self.completion
        lock.unlock()
        completion?(value)
    }
}

@MainActor
private final class FakeSpeechTranscriber: VoxCaretSpeechTranscribing {
    let displayName: String
    private(set) var cancelCount = 0

    init(name: String) {
        displayName = name
    }

    func requestAuthorization() async throws {}
    func start(
        onPartialResult: @escaping (String) -> Void,
        onFailure: @escaping (Error) -> Void
    ) throws {}
    func finish() async throws -> String { "" }
    func cancel() { cancelCount += 1 }
}
