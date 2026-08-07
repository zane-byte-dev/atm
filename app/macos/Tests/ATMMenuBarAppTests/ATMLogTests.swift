import XCTest
@testable import ATMMenuBarApp

final class ATMLogTests: XCTestCase {
    private var home: URL!

    override func setUp() {
        super.setUp()
        home = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("atm-log-test-\(UUID().uuidString)")
        ATMLog.homeOverride = home
    }

    override func tearDown() {
        ATMLog.homeOverride = nil
        try? FileManager.default.removeItem(at: home)
        super.tearDown()
    }

    private func loggedLines() -> [String] {
        ATMLog.flushForTesting()
        guard let text = try? String(contentsOf: ATMLog.fileURL, encoding: .utf8) else { return [] }
        return text.split(separator: "\n").map(String.init)
    }

    func testFailureWritesOneParseableJSONLine() throws {
        ATMLog.failure("dashboard_refresh_failed", error: "exit status 1", fields: ["source": "timer"])

        let lines = loggedLines()
        XCTAssertEqual(lines.count, 1, "expected exactly one line, got \(lines)")
        let data = Data(lines[0].utf8)
        let record = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(record["level"] as? String, "error")
        XCTAssertEqual(record["event"] as? String, "dashboard_refresh_failed")
        XCTAssertEqual(record["error"] as? String, "exit status 1")
        XCTAssertEqual(record["source"] as? String, "app", "the CLI needs to tell the two logs apart")
        XCTAssertNotNil(record["time"], "an entry with no timestamp cannot be correlated")
        let fields = try XCTUnwrap(record["fields"] as? [String: String])
        XCTAssertEqual(fields["source"], "timer")
    }

    /// The whole point of the marker: a crash cannot write its own log line, so an
    /// unclean exit is only visible as a missing marker beside an existing log.
    func testStartupAfterAnUncleanExitReportsIt() throws {
        ATMLog.failure("seed", error: "creates the log file")
        ATMLog.flushForTesting()
        XCTAssertTrue(FileManager.default.fileExists(atPath: ATMLog.fileURL.path))
        // No clean-exit marker was written, so this startup follows a crash.
        XCTAssertTrue(ATMLog.recordStartup())

        let joined = loggedLines().joined(separator: "\n")
        XCTAssertTrue(joined.contains("previous_run_did_not_exit_cleanly"), joined)
        XCTAssertTrue(joined.contains("DiagnosticReports"), "no pointer to the crash report: \(joined)")
    }

    func testStartupAfterACleanExitDoesNotReportACrash() throws {
        ATMLog.failure("seed", error: "creates the log file")
        ATMLog.recordCleanExit()
        ATMLog.flushForTesting()
        XCTAssertTrue(FileManager.default.fileExists(atPath: ATMLog.cleanExitMarker.path))

        XCTAssertFalse(ATMLog.recordStartup())
        XCTAssertFalse(
            loggedLines().joined().contains("previous_run_did_not_exit_cleanly"),
            "a clean quit was reported as a crash"
        )
        XCTAssertFalse(
            FileManager.default.fileExists(atPath: ATMLog.cleanExitMarker.path),
            "the marker must be cleared at startup, or the next crash looks clean"
        )
    }

    /// A fresh install has no marker either. Reporting that as a crash would fire
    /// on every first launch.
    func testFirstLaunchIsNotACrash() {
        XCTAssertFalse(ATMLog.recordStartup())
        XCTAssertFalse(loggedLines().joined().contains("previous_run_did_not_exit_cleanly"))
    }

    func testRotationCapsTheFileAndKeepsOnePreviousCopy() throws {
        try FileManager.default.createDirectory(at: ATMLog.directory, withIntermediateDirectories: true)
        let atCap = String(repeating: "x", count: 5 << 20)
        try atCap.write(to: ATMLog.fileURL, atomically: true, encoding: .utf8)

        ATMLog.failure("dashboard_refresh_failed", error: "after rotation")
        ATMLog.flushForTesting()

        let size = try XCTUnwrap(
            FileManager.default.attributesOfItem(atPath: ATMLog.fileURL.path)[.size] as? Int
        )
        XCTAssertLessThan(size, 5 << 20, "rotation did not happen before the write")
        XCTAssertTrue(
            FileManager.default.fileExists(
                atPath: ATMLog.directory.appendingPathComponent("app.log.1").path
            ),
            "the previous log was discarded instead of rotated"
        )
        XCTAssertTrue(loggedLines().joined().contains("after rotation"))
    }

    func testLogPathsLiveUnderTheATMDataDirectory() {
        ATMLog.homeOverride = nil
        // Not ~/Library/Logs: `atm diagnose --bundle` collects from ~/.atm, and
        // DESIGN.md keeps ATM's own data in one place.
        XCTAssertTrue(ATMLog.directory.path.hasSuffix(".atm/logs"), ATMLog.directory.path)
        XCTAssertEqual(ATMLog.fileURL.lastPathComponent, "app.log")
        XCTAssertTrue(
            ATMLog.cleanExitMarker.path.hasPrefix(ATMLog.directory.path),
            "the marker has to sit beside the log it describes"
        )
    }

    /// The crash pointer is a path, not the report contents: a macOS crash report
    /// can contain memory the App was holding, which is exactly what this log
    /// promises not to carry.
    func testCrashReportHintPointsAtAPathWithoutEmbeddingReports() {
        let hint = ATMLog.crashReportDirectory
        XCTAssertTrue(hint.contains("DiagnosticReports"), hint)
        XCTAssertTrue(hint.contains("ATM"), "the hint has to say which reports to look for: \(hint)")
    }

    /// A first launch has no clean-exit marker and no previous log, which must not
    /// be reported as a crash — that would be a false alarm on every fresh install.
    func testUncleanExitRequiresEvidenceOfAPreviousRun() {
        XCTAssertFalse(
            ATMLog.isUncleanExit(markerExists: false, logExists: false),
            "first launch reported as an unclean exit"
        )
        XCTAssertTrue(
            ATMLog.isUncleanExit(markerExists: false, logExists: true),
            "a previous run with no clean-exit marker is exactly the crash case"
        )
        XCTAssertFalse(
            ATMLog.isUncleanExit(markerExists: true, logExists: true),
            "a clean exit was reported as unclean"
        )
        XCTAssertFalse(
            ATMLog.isUncleanExit(markerExists: true, logExists: false),
            "a marker without a log is not a crash"
        )
    }

    /// The App logs whatever the CLI printed for a failed refresh, and a CLI error
    /// quotes titles and paths. `atm diagnose --bundle` collects app.log next to
    /// cli.log, so this side has to hold the same line the Go side does.
    func testFailureKeepsQuotedContentOutOfTheLog() throws {
        ATMLog.failure(
            "dashboard_refresh_failed",
            error: "item \"把 ACL 密钥换成 sifei 给的那把\": waiting todos require wake"
        )

        let lines = loggedLines()
        XCTAssertEqual(lines.count, 1, "expected exactly one line, got \(lines)")
        let record = try XCTUnwrap(
            try JSONSerialization.jsonObject(with: Data(lines[0].utf8)) as? [String: Any]
        )
        let logged = try XCTUnwrap(record["error"] as? String)
        XCTAssertFalse(logged.contains("ACL"), "the quoted value reached the log: \(logged)")
        XCTAssertTrue(
            logged.contains("waiting todos require wake"),
            "redaction ate the diagnosis: \(logged)"
        )
    }

    /// Same table as TestRedactQuoted on the Go side: the two logs are read
    /// together, so they must not disagree about what a redacted line looks like.
    func testRedactingQuotedMatchesTheCLIRule() {
        let cases: [(String, String)] = [
            ("database is locked", "database is locked"),
            ("item \"secret\": bad", "item \"…\": bad"),
            ("\"a\" and \"b\"", "\"…\" and \"…\""),
            ("item \"he said \\\"hi\\\" loudly\": bad", "item \"…\": bad"),
            ("project \"\": missing", "project \"\": missing"),
            ("item \"half a tit", "item \"…\""),
            ("item \"", "item \"…\""),
        ]
        for (input, want) in cases {
            XCTAssertEqual(
                ATMLog.redactingQuoted(input), want,
                "redactingQuoted(\(input))"
            )
        }
    }
}
