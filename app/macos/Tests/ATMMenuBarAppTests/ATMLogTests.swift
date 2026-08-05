import XCTest
@testable import ATMMenuBarApp

/// ATMLog writes to a fixed path derived from the real home directory, so these
/// tests exercise the decisions rather than the file system: the format contract
/// the CLI side and the diagnose bundle both depend on, and the one piece of
/// logic that is easy to get wrong — telling a crash from a first launch.
final class ATMLogTests: XCTestCase {
    func testLogPathsLiveUnderTheATMDataDirectory() {
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
}
