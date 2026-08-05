import Foundation

/// Writes the App's failures to disk, in the same format and directory the CLI
/// uses (~/.atm/logs), so `atm diagnose --bundle` collects both.
///
/// The App had one NSLog line in the entire codebase. It is a resident process
/// that refreshes on a timer, so its failures happen when nobody is looking at
/// it: a refresh cycle that fails every afternoon and recovers, a CLI invocation
/// that started returning garbage, a notification that never fires. All of that
/// was invisible, and "the App stopped working" arrived with no evidence
/// attached.
///
/// Three rules, matching internal/logging in the Go side:
///
/// - Failures only, plus process lifecycle. A log of successful refreshes ages
///   out the line that mattered.
/// - No content. Not session text, not todo/memory/knowledge bodies, not
///   credentials. This file gets attached to public bug reports.
/// - Never disturb the App. Logging is best-effort; a read-only disk must not
///   change what the App does.
enum ATMLog {
    /// Deliberately not the OS logging system. os_log goes to a unified store the
    /// user cannot attach to an issue, and ATM's whole support story is a file a
    /// person can read and send. See DESIGN.md on ATM keeping its own data under
    /// ~/.atm.
    static var directory: URL {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent(".atm/logs", isDirectory: true)
    }

    static var fileURL: URL { directory.appendingPathComponent("app.log") }

    /// Marks a clean shutdown. Its absence at startup is how a crash or a force
    /// quit is detected — the App cannot write a log entry while it is dying.
    static var cleanExitMarker: URL { directory.appendingPathComponent(".app-exited-cleanly") }

    /// macOS writes crash reports here. The path is recorded rather than the
    /// contents: those reports can contain memory the App was holding, which by
    /// this file's rules does not belong in it.
    static var crashReportDirectory: String {
        "~/Library/Logs/DiagnosticReports (filter for ATM)"
    }

    private static let maxBytes = 5 << 20
    private static let queue = DispatchQueue(label: "com.atm.log")

    /// Records a failure. `fields` must hold identifiers, counts and statuses —
    /// never user content.
    static func failure(_ event: String, error: String? = nil, fields: [String: String] = [:]) {
        write(level: "error", event: event, error: error, fields: fields)
    }

    /// Records a process boundary. The only non-failure entries.
    static func lifecycle(_ event: String, fields: [String: String] = [:]) {
        write(level: "info", event: event, error: nil, fields: fields)
    }

    /// Whether the previous run ended abnormally.
    ///
    /// The App cannot write a log line while it is being killed, so a crash is
    /// only visible as a missing clean-exit marker. The second condition is what
    /// keeps that from crying wolf: a first launch has no marker either, and
    /// reporting that as a crash would be a false alarm on every fresh install.
    ///
    /// Separate and pure so it can be tested without touching a real home
    /// directory.
    static func isUncleanExit(markerExists: Bool, logExists: Bool) -> Bool {
        !markerExists && logExists
    }

    /// Called at startup, before the clean-exit marker is cleared. Returns whether
    /// the previous run ended abnormally, having already logged it.
    @discardableResult
    static func recordStartup() -> Bool {
        let manager = FileManager.default
        let markerExists = manager.fileExists(atPath: cleanExitMarker.path)
        let logExists = manager.fileExists(atPath: fileURL.path)
        let unclean = isUncleanExit(markerExists: markerExists, logExists: logExists)
        if unclean {
            failure("previous_run_did_not_exit_cleanly", fields: [
                "crash_reports": crashReportDirectory,
            ])
        }
        try? manager.removeItem(at: cleanExitMarker)
        lifecycle("app_started", fields: [
            "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown",
            "clean_previous_exit": String(!unclean),
        ])
        return unclean
    }

    /// Called on orderly termination. Writing the marker is what makes the next
    /// startup able to tell a crash from a quit.
    static func recordCleanExit() {
        lifecycle("app_exiting")
        ensureDirectory()
        try? Data().write(to: cleanExitMarker)
    }

    private static func ensureDirectory() {
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    private static func write(level: String, event: String, error: String?, fields: [String: String]) {
        // Serialised off the caller's thread: this is called from refresh paths on
        // the main actor and must not add file I/O to them.
        queue.async {
            var record: [String: Any] = [
                "time": ISO8601DateFormatter().string(from: Date()),
                "level": level,
                "event": event,
                "source": "app",
            ]
            if let error, !error.isEmpty {
                record["error"] = error
            }
            if !fields.isEmpty {
                record["fields"] = fields
            }
            guard let data = try? JSONSerialization.data(withJSONObject: record),
                  var line = String(data: data, encoding: .utf8) else { return }
            line += "\n"

            ensureDirectory()
            rotateIfNeeded(adding: line.utf8.count)
            guard let payload = line.data(using: .utf8) else { return }
            if let handle = try? FileHandle(forWritingTo: fileURL) {
                defer { try? handle.close() }
                _ = try? handle.seekToEnd()
                try? handle.write(contentsOf: payload)
            } else {
                try? payload.write(to: fileURL)
            }
        }
    }

    /// Rotates before writing so the cap is a real bound rather than a threshold
    /// the file always sits above. One previous file is kept — enough to survive a
    /// rotation between a failure and someone noticing it.
    private static func rotateIfNeeded(adding incoming: Int) {
        let manager = FileManager.default
        guard let attributes = try? manager.attributesOfItem(atPath: fileURL.path),
              let size = attributes[.size] as? Int,
              size + incoming > maxBytes else { return }
        let rotated = directory.appendingPathComponent("app.log.1")
        try? manager.removeItem(at: rotated)
        try? manager.moveItem(at: fileURL, to: rotated)
    }
}
