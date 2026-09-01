import Foundation

enum ATMConnectorLoginError: LocalizedError {
    case appleScript(String)

    var errorDescription: String? {
        switch self {
        case .appleScript(let message): return "打开登录终端失败：\(message)"
        }
    }
}

/// Runs a connector's declared login command where the person can see it.
///
/// Deliberately a terminal window rather than a captured subprocess: the login is
/// interactive — it opens a browser, prints a URL, waits for a scan — so anything
/// that swallowed its output would leave the person watching a spinner for a
/// question they were never shown. ATM never starts this on its own; a button
/// press is what gets here.
enum ATMConnectorLoginLauncher {
    static let actionCategory = "ATM_COLLECTION_AUTH"
    static let actionIdentifier = "ATM_COLLECTION_LOGIN"

    /// AppleScript that runs `command` in a new Terminal window and brings Terminal
    /// forward. Kept pure so the quoting is testable without opening anything.
    static func terminalScript(for command: String) -> String {
        """
        tell application id "com.apple.Terminal"
            activate
            do script "\(appleScriptLiteral(command))"
        end tell
        """
    }

    /// Escapes what an AppleScript string literal cannot hold verbatim. A path with
    /// a space is ordinary here (`~/.qoderwork/bin/dws`), a quote in it is not, and
    /// an unescaped one would silently truncate the command being run.
    static func appleScriptLiteral(_ command: String) -> String {
        command
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
    }

    static func start(_ prompt: ATMCollectionLoginPrompt) throws {
        guard let script = NSAppleScript(source: terminalScript(for: prompt.command)) else {
            throw ATMConnectorLoginError.appleScript("AppleScript 初始化失败")
        }
        var errorInfo: NSDictionary?
        script.executeAndReturnError(&errorInfo)
        if let errorInfo {
            let message = errorInfo[NSAppleScript.errorMessage] as? String ?? errorInfo.description
            throw ATMConnectorLoginError.appleScript(message)
        }
    }
}
