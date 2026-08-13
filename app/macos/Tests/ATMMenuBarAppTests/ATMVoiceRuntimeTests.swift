import XCTest
@testable import ATMMenuBarApp

/// Dictation depends on two things that are not code: a real `.app` bundle, and TCC
/// actually answering an authorization request. Both fail silently — no crash, no
/// error, just a permission dialog that never appears and a callback that never
/// arrives — so the guards against them are worth pinning down.
final class ATMVoiceRuntimeTests: XCTestCase {
    /// The test bundle is an `.xctest`, not an `.app`, which is exactly the shape
    /// `swift run` produces. So this asserts the detection is actually looking at the
    /// bundle rather than returning a constant.
    func testBundleDetectionRecognizesANonAppProcess() {
        XCTAssertFalse(ATMAppBundle.isBundled)
        XCTAssertNotEqual(Bundle.main.bundleURL.pathExtension, "app")
    }

    /// Both preconditions need to say what to do about them, not just that something is
    /// wrong: neither is discoverable by looking at the UI.
    func testPreconditionErrorsNameTheFix() {
        let bundleMessage = ATMSpeechTranscriberError.requiresAppBundle.localizedDescription
        XCTAssertTrue(bundleMessage.contains("run-dev-app.sh"), bundleMessage)
        XCTAssertTrue(bundleMessage.contains("swift run"), bundleMessage)

        let timeoutMessage = ATMSpeechTranscriberError.authorizationTimedOut.localizedDescription
        XCTAssertFalse(timeoutMessage.isEmpty)
        XCTAssertTrue(timeoutMessage.contains("系统设置"), timeoutMessage)
    }

    func testEveryTranscriberErrorHasAMessage() {
        let errors: [ATMSpeechTranscriberError] = [
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

    // MARK: - Authorization deadline

    func testAuthorizationReturnsTheAnswerWhenTheCallbackArrives() async {
        let granted = await ATMAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(true)
        }
        XCTAssertEqual(granted, true)

        let denied = await ATMAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(false)
        }
        XCTAssertEqual(denied, false)
    }

    /// The case that caused the hang: a request whose callback never fires, because the
    /// dialog it was waiting on never appeared. `nil` rather than a thrown error, so the
    /// caller decides what a missing answer means.
    func testAuthorizationTimesOutWhenTheCallbackNeverArrives() async {
        let answer = await ATMAuthorizationRequest.run(timeout: .milliseconds(120)) { _ in
            // Deliberately drops the completion handler on the floor.
        }
        XCTAssertNil(answer)
    }

    /// A late callback must not resume the continuation a second time — that traps and
    /// takes the process with it.
    func testLateCallbackAfterTimeoutIsIgnored() async {
        let box = LateCompletion()
        let answer = await ATMAuthorizationRequest.run(timeout: .milliseconds(100)) { completion in
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
        let answer = await ATMAuthorizationRequest.run(timeout: .seconds(5)) { completion in
            completion(true)
            completion(false)
            completion(true)
        }
        XCTAssertEqual(answer, true)
    }

    /// A dialog someone has to find and click needs a generous deadline; too short and
    /// a real grant gets thrown away.
    func testDefaultDeadlineLeavesTimeToClickTheDialog() {
        XCTAssertGreaterThanOrEqual(ATMAuthorizationRequest.timeout, .seconds(30))
        XCTAssertLessThanOrEqual(ATMAuthorizationRequest.timeout, .seconds(120))
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
