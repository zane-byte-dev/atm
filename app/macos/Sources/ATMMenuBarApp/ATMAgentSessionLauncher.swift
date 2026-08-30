import AppKit
import Foundation

enum ATMAgentSessionLaunchRoute: Equatable {
    case codexThread(threadID: String)
    case terminal(bundleIdentifier: String, tty: String)
    case workspace(bundleIdentifier: String, path: String)
    case application(bundleIdentifier: String)
    case unavailable(reason: String)

    static func resolve(for session: ATMLiveSession) -> Self {
        let tool = normalized(session.tool)
        let client = normalized(session.client)
        let resumeID = nonEmpty(session.resumeID)
        // A Codex subagent's own rollout is an implementation detail rather than
        // a user-owned task. Return deep branches to the visible root task; the
        // direct parent remains the compatibility fallback for older payloads.
        let codexThreadID: String?
        if session.isSubagent {
            codexThreadID = nonEmpty(session.rootSessionID)
                ?? nonEmpty(session.parentSessionID)
                ?? resumeID
        } else {
            codexThreadID = resumeID
        }
        let isCodexDesktop = tool.contains("codex")
            && (client.contains("desktop") || client.contains("app") || (client.isEmpty && session.pid == nil))
        let isGrok = tool.contains("grok")
        let isTerminalAgent = tool.contains("claude")
            || tool.contains("codex")
            || isGrok
            || tool == "pi"

        if isCodexDesktop, let codexThreadID {
            return .codexThread(threadID: codexThreadID)
        }

        if let bundleIdentifier = nonEmpty(session.terminalApp) {
            if let tty = normalizedTTY(session.tty), supportsExactTerminalFocus(bundleIdentifier) {
                return .terminal(bundleIdentifier: bundleIdentifier, tty: tty)
            }
            // Ghostty (and any terminal without AppleScript TTY focus) still
            // gets "open the terminal app" rather than falling through to
            // unavailable once we know which host is running.
            if supportsTerminalActivation(bundleIdentifier) {
                return .application(bundleIdentifier: bundleIdentifier)
            }
            if let cwd = nonEmpty(session.cwd), isWorkspaceApplication(bundleIdentifier) {
                return .workspace(bundleIdentifier: bundleIdentifier, path: cwd)
            }
            return .application(bundleIdentifier: bundleIdentifier)
        }

        if isCodexDesktop {
            return .application(bundleIdentifier: "com.openai.codex")
        }

        if client.contains("qoder") || tool.contains("qoderwork") {
            if let cwd = nonEmpty(session.cwd) {
                return .workspace(bundleIdentifier: "com.qoder.work", path: cwd)
            }
            return .application(bundleIdentifier: "com.qoder.work")
        }

        // Antigravity is a VS Code fork, so it has to be matched before the
        // VS Code branch below. There is no exact conversation deep link, but the
        // summary index records the workspace folder, which reopens the window the
        // conversation belongs to.
        if tool.contains("antigravity") || client.contains("antigravity") {
            if let cwd = nonEmpty(session.cwd) {
                return .workspace(bundleIdentifier: "com.google.antigravity", path: cwd)
            }
            return .application(bundleIdentifier: "com.google.antigravity")
        }

        if client.contains("vs code") || client.contains("vscode") {
            if let cwd = nonEmpty(session.cwd) {
                return .workspace(bundleIdentifier: "com.microsoft.VSCode", path: cwd)
            }
            return .application(bundleIdentifier: "com.microsoft.VSCode")
        }

        // Terminal agents without live process metadata still get a soft
        // landing: open Terminal.app at the session cwd. Labelled "打开来源"
        // rather than "回到会话" because it is not an exact tab focus.
        if isTerminalAgent, let cwd = nonEmpty(session.cwd) {
            return .workspace(bundleIdentifier: "com.apple.Terminal", path: cwd)
        }

        if isGrok {
            return .unavailable(reason: "ATM 还没有采集到这个 Grok 会话的终端或工作目录")
        }

        return .unavailable(reason: "ATM 还没有采集到这个会话的 App、TTY 或窗口标识")
    }

    /// A bound session is a history entry, so the process behind it is usually
    /// gone and there is no TTY left to focus. When it happens to still be
    /// running the live route is exact; otherwise Codex is the one agent that can
    /// reopen a finished thread, because the binding ledger stores exactly the
    /// thread id its deep link takes.
    static func resolve(for session: ATMBoundSession, live: [ATMLiveSession]) -> Self {
        if let match = live.first(where: { isSameSession($0, session) }) {
            return resolve(for: match)
        }
        if normalized(session.agent).contains("codex"), isThreadID(session.sessionID) {
            return .codexThread(threadID: session.sessionID)
        }
        return .unavailable(reason: "这个会话已经结束，ATM 只能给你完整对话记录")
    }

    private static func isSameSession(_ live: ATMLiveSession, _ bound: ATMBoundSession) -> Bool {
        let liveIDs = [live.sessionID, live.resumeID]
        let boundIDs = [bound.sessionID, bound.indexedID, bound.shortID]
        for liveID in liveIDs.compactMap(nonEmpty) {
            for boundID in boundIDs.compactMap(nonEmpty) where idsMatch(liveID, boundID) {
                return true
            }
        }
        return false
    }

    /// Agents disagree about which id they publish: codex presence reports an
    /// 8-character id while the ledger holds the full thread uuid, and the
    /// transcript index holds the rollout filename that ends with it. So a
    /// shorter id matching either end of a longer one counts as the same session.
    private static func idsMatch(_ lhs: String, _ rhs: String) -> Bool {
        let left = lhs.lowercased()
        let right = rhs.lowercased()
        if left == right { return true }
        let (shorter, longer) = left.count < right.count ? (left, right) : (right, left)
        guard shorter.count >= 8 else { return false }
        return longer.hasPrefix(shorter) || longer.hasSuffix(shorter)
    }

    private static func isThreadID(_ value: String) -> Bool {
        UUID(uuidString: value) != nil
    }

    var isAvailable: Bool {
        if case .unavailable = self { return false }
        return true
    }

    var isExact: Bool {
        switch self {
        case .codexThread, .terminal: return true
        case .workspace, .application, .unavailable: return false
        }
    }

    var destinationLabel: String {
        switch self {
        case .codexThread:
            return "Codex Desktop 对应会话"
        case .terminal(let bundleIdentifier, _):
            return "\(Self.applicationName(bundleIdentifier)) 对应终端"
        case .workspace(let bundleIdentifier, _):
            return "\(Self.applicationName(bundleIdentifier)) 项目窗口"
        case .application(let bundleIdentifier):
            return Self.applicationName(bundleIdentifier)
        case .unavailable(let reason):
            return reason
        }
    }

    var actionTitle: String {
        isExact ? "回到会话" : "打开来源"
    }

    private static func normalized(_ value: String?) -> String {
        value?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() ?? ""
    }

    private static func nonEmpty(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return value
    }

    private static func normalizedTTY(_ value: String?) -> String? {
        guard var value = nonEmpty(value) else { return nil }
        value = value.replacingOccurrences(of: "/dev/", with: "")
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "._-"))
        guard value.unicodeScalars.allSatisfy({ safe.contains($0) }) else { return nil }
        return value
    }

    private static func supportsExactTerminalFocus(_ bundleIdentifier: String) -> Bool {
        bundleIdentifier == "com.apple.Terminal" || bundleIdentifier == "com.googlecode.iterm2"
    }

    private static func supportsTerminalActivation(_ bundleIdentifier: String) -> Bool {
        supportsExactTerminalFocus(bundleIdentifier)
            || bundleIdentifier == "com.mitchellh.ghostty"
    }

    private static func isWorkspaceApplication(_ bundleIdentifier: String) -> Bool {
        [
            "com.microsoft.VSCode",
            "com.microsoft.VSCodeInsiders",
            "com.todesktop.230313mzl4w4u92",
            "com.qoder.work",
            "com.google.antigravity",
            "com.apple.Terminal",
        ].contains(bundleIdentifier)
    }

    private static func applicationName(_ bundleIdentifier: String) -> String {
        switch bundleIdentifier {
        case "com.openai.codex": return "Codex Desktop"
        case "com.apple.Terminal": return "Terminal"
        case "com.googlecode.iterm2": return "iTerm"
        case "com.mitchellh.ghostty": return "Ghostty"
        case "com.microsoft.VSCode", "com.microsoft.VSCodeInsiders": return "Visual Studio Code"
        case "com.todesktop.230313mzl4w4u92": return "Cursor"
        case "com.qoder.work": return "Qoder"
        case "com.google.antigravity": return "Antigravity"
        default: return "来源 App"
        }
    }
}

enum ATMAgentSessionLaunchError: LocalizedError {
    case invalidThreadID
    case applicationNotFound(String)
    case terminalSessionNotFound(String)
    case appleScript(String)

    var errorDescription: String? {
        switch self {
        case .invalidThreadID:
            return "无法生成 Codex 会话链接。"
        case .applicationNotFound(let name):
            return "没有找到 \(name)，无法打开来源会话。"
        case .terminalSessionNotFound(let tty):
            return "已找到来源终端，但没有找到 TTY \(tty) 对应的标签页。"
        case .appleScript(let message):
            return "无法定位终端会话：\(message)"
        }
    }
}

@MainActor
enum ATMAgentSessionLauncher {
    /// Opens the best-known host for `session`. Terminal TTY focus is preferred;
    /// when AppleScript cannot select the exact tab (permission denied, stale
    /// TTY, or a host API change), the terminal app is still activated so the
    /// click does not bounce into ATM's own desktop.
    @discardableResult
    static func open(_ session: ATMLiveSession) throws -> ATMAgentSessionLaunchRoute {
        try open(ATMAgentSessionLaunchRoute.resolve(for: session))
    }

    @discardableResult
    static func open(_ route: ATMAgentSessionLaunchRoute) throws -> ATMAgentSessionLaunchRoute {
        switch route {
        case .codexThread(let threadID):
            guard let encoded = threadID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed),
                  let url = URL(string: "codex://threads/\(encoded)"),
                  NSWorkspace.shared.open(url) else {
                throw ATMAgentSessionLaunchError.invalidThreadID
            }
        case .terminal(let bundleIdentifier, let tty):
            do {
                try focusTerminal(bundleIdentifier: bundleIdentifier, tty: tty)
            } catch {
                // Exact tab focus is best-effort. Activating the terminal the
                // session is known to live in is still better than opening ATM.
                try activateApplication(bundleIdentifier: bundleIdentifier)
            }
        case .workspace(let bundleIdentifier, let path):
            try openWorkspace(bundleIdentifier: bundleIdentifier, path: path)
        case .application(let bundleIdentifier):
            try activateApplication(bundleIdentifier: bundleIdentifier)
        case .unavailable(let reason):
            throw ATMAgentSessionLaunchError.appleScript(reason)
        }
        return route
    }

    private static func focusTerminal(bundleIdentifier: String, tty: String) throws {
        // iTerm reports `/dev/ttys000`; status may store either form. Match by
        // suffix so a short id still finds the tab, and coerce to text so
        // AppleScript string compare is reliable across iTerm versions.
        let shortTTY = tty.replacingOccurrences(of: "/dev/", with: "")
        let source: String
        switch bundleIdentifier {
        case "com.apple.Terminal":
            source = """
            set shortTTY to "\(shortTTY)"
            set fullTTY to "/dev/\(shortTTY)"
            tell application id "com.apple.Terminal"
                activate
                repeat with theWindow in windows
                    repeat with theTab in tabs of theWindow
                        try
                            set tabTTY to (tty of theTab as text)
                            if tabTTY is shortTTY or tabTTY is fullTTY or tabTTY ends with shortTTY then
                                set selected of theTab to true
                                set frontmost of theWindow to true
                                return "ok"
                            end if
                        end try
                    end repeat
                end repeat
                return "not-found"
            end tell
            """
        case "com.googlecode.iterm2":
            source = """
            set shortTTY to "\(shortTTY)"
            set fullTTY to "/dev/\(shortTTY)"
            tell application id "com.googlecode.iterm2"
                activate
                repeat with theWindow in windows
                    repeat with theTab in tabs of theWindow
                        repeat with theSession in sessions of theTab
                            try
                                set sessionTTY to (tty of theSession as text)
                                if sessionTTY is shortTTY or sessionTTY is fullTTY or sessionTTY ends with shortTTY then
                                    set miniaturized of theWindow to false
                                    select theTab
                                    select theSession
                                    select theWindow
                                    return "ok"
                                end if
                            end try
                        end repeat
                    end repeat
                end repeat
                return "not-found"
            end tell
            """
        default:
            try activateApplication(bundleIdentifier: bundleIdentifier)
            return
        }

        guard let script = NSAppleScript(source: source) else {
            throw ATMAgentSessionLaunchError.appleScript("AppleScript 初始化失败")
        }
        var errorInfo: NSDictionary?
        let result = script.executeAndReturnError(&errorInfo)
        if let errorInfo {
            let message = errorInfo[NSAppleScript.errorMessage] as? String ?? errorInfo.description
            throw ATMAgentSessionLaunchError.appleScript(message)
        }
        guard result.stringValue == "ok" else {
            throw ATMAgentSessionLaunchError.terminalSessionNotFound(shortTTY)
        }
    }

    static func activateApplication(bundleIdentifier: String) throws {
        if let application = NSRunningApplication.runningApplications(withBundleIdentifier: bundleIdentifier).first {
            application.activate(options: [.activateIgnoringOtherApps])
            return
        }
        guard let applicationURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleIdentifier) else {
            throw ATMAgentSessionLaunchError.applicationNotFound(
                ATMAgentSessionLaunchRoute.application(bundleIdentifier: bundleIdentifier).destinationLabel
            )
        }
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = true
        NSWorkspace.shared.openApplication(at: applicationURL, configuration: configuration)
    }

    private static func openWorkspace(bundleIdentifier: String, path: String) throws {
        guard let applicationURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleIdentifier) else {
            throw ATMAgentSessionLaunchError.applicationNotFound(
                ATMAgentSessionLaunchRoute.application(bundleIdentifier: bundleIdentifier).destinationLabel
            )
        }
        let workspaceURL = URL(fileURLWithPath: path, isDirectory: true)
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = true
        NSWorkspace.shared.open(
            [workspaceURL],
            withApplicationAt: applicationURL,
            configuration: configuration
        )
    }
}
