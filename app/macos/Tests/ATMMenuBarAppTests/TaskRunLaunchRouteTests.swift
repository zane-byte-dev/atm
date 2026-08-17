import XCTest
@testable import ATMMenuBarApp

final class TaskRunLaunchRouteTests: XCTestCase {
    func testCodexTaskRunOpensItsDurableDesktopThread() throws {
        let run = try decodeRun(
            agent: "codex",
            sessionID: "019fea8d-a0c4-7130-a984-2c8128705934"
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: run, live: nil)

        XCTAssertEqual(
            route,
            .codexThread(threadID: "019fea8d-a0c4-7130-a984-2c8128705934")
        )
        XCTAssertTrue(route.isExact)
        XCTAssertEqual(route.actionTitle, "回到会话")
    }

    func testHeadlessTaskRunDoesNotPretendItsWorkingDirectoryIsTheSession() throws {
        let run = try decodeRun(
            agent: "grokbuild",
            sessionID: "019fea8d-a0c4-7130-a984-2c8128705934"
        )
        let presence = ATMLiveSession(
            tool: "Grok Build",
            sessionID: "019fea8d",
            project: "atm",
            cwd: "/Users/tester/mox/atm",
            ageSeconds: 2
        )

        let route = ATMAgentSessionLaunchRoute.resolve(for: run, live: presence)

        XCTAssertFalse(route.isAvailable)
        XCTAssertTrue(route.destinationLabel.contains("没有可交互"))
    }

    func testTaskRunUsesAnExactLiveTerminalWhenTheAgentReportsOne() throws {
        let run = try decodeRun(
            agent: "pi",
            sessionID: "019fea8d-a0c4-7130-a984-2c8128705934"
        )
        let presence = ATMLiveSession(
            tool: "Pi",
            sessionID: "019fea8d",
            project: "atm",
            client: "Pi",
            ageSeconds: 2,
            pid: "1200",
            tty: "ttys006",
            terminalApp: "com.apple.Terminal"
        )

        XCTAssertEqual(
            ATMAgentSessionLaunchRoute.resolve(for: run, live: presence),
            .terminal(bundleIdentifier: "com.apple.Terminal", tty: "ttys006")
        )
    }

    private func decodeRun(agent: String, sessionID: String) throws -> ATMTaskRun {
        try JSONDecoder().decode(
            ATMTaskRun.self,
            from: Data(
                """
                {"id":"run-1","todo_id":"t261","agent":"\(agent)","project":"atm",
                 "work_dir":"/Users/tester/mox/atm","policy":"guarded","log_path":"/tmp/run.log",
                 "status":"running","start_ts":100,"session_id":"\(sessionID)"}
                """.utf8
            )
        )
    }
}
