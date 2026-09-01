import Foundation

/// One normalized event pushed by an agent hook.
///
/// Mirrors `internal/agentevent.Envelope` on the Go side. The Pi extension
/// hand-builds this JSON in TypeScript, so the coding keys are the contract for
/// three languages — rename nothing here without changing all three.
struct ATMAgentEvent: Decodable, Equatable {
    /// Envelope schema version the app understands. A newer sender is rejected
    /// rather than partially interpreted.
    static let supportedVersion = 1

    enum Kind: String, Decodable, Equatable {
        case sessionStart = "session_start"
        case started
        case attention
        case resumed
        case completed
        case sessionEnd = "session_end"

        /// Whether receiving this retires a pending attention signal for the
        /// same session. The agent moving on is the real "you are off the hook"
        /// signal; without it a resolved permission prompt would stay orange
        /// until the safety TTL expired.
        var clearsAttention: Bool {
            switch self {
            case .started, .resumed, .completed, .sessionEnd: return true
            case .attention, .sessionStart: return false
            }
        }

        /// Whether the polled snapshot could have changed too, i.e. whether the
        /// app should re-run `atm session status` rather than just re-merge the
        /// overlay it already holds.
        ///
        /// `resumed` is the one event that fires many times per turn, and it
        /// carries nothing the poller wants: the row is already there, its text
        /// has not changed, and the only thing it decides is whether an
        /// attention signal survives. Refreshing on it would pull the effective
        /// poll interval down to the debounce window for the whole time an
        /// agent is working.
        var mayChangeSnapshot: Bool { self != .resumed }

        /// Whether receiving this is evidence that the agent reports its own
        /// turn state, i.e. that snapshot diffing can stand down for it.
        ///
        /// Every other event comes from a lifecycle or notification hook, so
        /// arriving at all proves that channel works. `resumed` comes from a
        /// tool hook, which can fire perfectly while `Stop` never does.
        var provesTurnStateReporting: Bool { self != .resumed }
    }

    let version: Int
    let source: String
    let event: Kind
    let sessionID: String?
    let cwd: String?
    let tool: String?
    let reason: String?
    let text: String?
    let at: String?

    enum CodingKeys: String, CodingKey {
        case version = "v"
        case source
        case event
        case sessionID = "session_id"
        case cwd
        case tool
        case reason
        case text
        case at
    }

    var isSupported: Bool { version == Self.supportedVersion }

    /// The identifiers this event can be joined on, most specific first.
    var joinCandidates: [String] {
        [sessionID, cwd].compactMap { value in
            let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return trimmed.isEmpty ? nil : trimmed
        }
    }

    /// The identifier that names one session rather than one directory.
    ///
    /// Separate from `joinCandidates` because handing a session's state over to
    /// hooks has to be decided per session: a `cwd` match would also cover the
    /// other agents running in the same repo, which have no hooks of their own.
    var sessionKey: String? {
        let trimmed = sessionID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? nil : trimmed
    }

    /// The directory this event came from, when it named one.
    ///
    /// Only ever the fallback identity for a hook that reported no session id:
    /// several agents run in one repository, so a `cwd` names the work, not the
    /// session that is waiting on you.
    var cwdKey: String? {
        let trimmed = cwd?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? nil : trimmed
    }
}

/// Hook registration state for one agent, as reported by
/// `atm agent hook status --json`.
struct ATMAgentHookSource: Decodable, Equatable, Identifiable {
    let source: String
    let path: String?
    let installed: [String]
    let missing: [String]
    let added: [String]
    let removed: [String]
    let conflicts: [String]
    /// Set for agents wired up by hand rather than through a config file (Pi).
    let manual: String?
    let error: String?

    var id: String { source }

    enum CodingKeys: String, CodingKey {
        case source, path, installed, missing, added, removed, conflicts, manual, error
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        source = try values.decode(String.self, forKey: .source)
        path = try values.decodeIfPresent(String.self, forKey: .path)
        installed = try values.decodeIfPresent([String].self, forKey: .installed) ?? []
        missing = try values.decodeIfPresent([String].self, forKey: .missing) ?? []
        added = try values.decodeIfPresent([String].self, forKey: .added) ?? []
        removed = try values.decodeIfPresent([String].self, forKey: .removed) ?? []
        conflicts = try values.decodeIfPresent([String].self, forKey: .conflicts) ?? []
        manual = try values.decodeIfPresent(String.self, forKey: .manual)
        error = try values.decodeIfPresent(String.self, forKey: .error)
    }

    var displayName: String {
        switch source {
        case "claude": return "Claude Code"
        case "codex": return "Codex"
        case "grokbuild": return "Grok Build"
        case "qoder": return "Qoder"
        case "pi": return "Pi"
        case "antigravity": return "Antigravity"
        default: return source
        }
    }

    /// The hook source behind a parsed session's `tool`, or nil for agents ATM
    /// cannot install hooks into (copilot, qoderwork — they keep the
    /// snapshot-diffing heuristic). Grok Build is installable via
    /// `~/.grok/hooks/atm-notch.json`, Qoder via `~/.qoder/settings.json`.
    ///
    /// Qoder is matched exactly rather than by prefix: the IDE and Qoder CLI read
    /// the same settings document, but QoderWork is a different product with its
    /// own runtime, and claiming it is hooked would silence it outright.
    static func source(forTool tool: String) -> String? {
        let key = tool.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if key.hasPrefix("claude") { return "claude" }
        if key.hasPrefix("codex") { return "codex" }
        if key.hasPrefix("grok") { return "grokbuild" }
        if key == "qoder" || key == "qoder cli" { return "qoder" }
        if key == "pi" { return "pi" }
        return nil
    }

    /// Whether ATM's turn-ending hook is registered for this source. `installed`
    /// carries the CLI's own labels for the specs it found (`Stop`,
    /// `Notification(idle_prompt)`, …).
    var reportsTurnEnd: Bool { error == nil && installed.contains("Stop") }

    /// Fully wired up. Manual sources are never "installed" from here, since the
    /// app cannot verify a file it did not write.
    var isFullyInstalled: Bool {
        manual == nil && error == nil && missing.isEmpty && !installed.isEmpty
    }
}

struct ATMAgentHookReport: Decodable, Equatable {
    let socketPath: String
    let sources: [ATMAgentHookSource]

    enum CodingKeys: String, CodingKey {
        case socketPath = "socket_path"
        case sources
    }
}

/// A live "this session is waiting for you" signal produced by a hook.
struct ATMAgentAttentionSignal: Equatable {
    /// Why the agent stopped, in its own vocabulary (`permission_prompt`,
    /// `idle_prompt`, …). Rendered verbatim so ATM never claims to know more
    /// than the agent told us.
    let reason: String
    let tool: String?
    let text: String?
    let source: String
    let receivedAt: Date

    /// How long a signal survives without a clearing event.
    ///
    /// Purely a safety valve: the agent normally clears the signal by starting
    /// or finishing work. But hooks are best-effort — a crashed CLI, a killed
    /// terminal, or an uninstalled hook can drop the clearing event, and a banner
    /// that never retires is worse than one that forgets.
    static let timeToLive: TimeInterval = 10 * 60

    func isLive(at now: Date) -> Bool {
        now.timeIntervalSince(receivedAt) < Self.timeToLive
    }

    /// Short human label — the subtitle of the notification this raises.
    var displayReason: String {
        switch reason {
        case "permission_prompt", "permission_request": return "等待授权"
        case "idle_prompt": return "等待输入"
        case "agent_needs_input": return "需要补充信息"
        case "elicitation_dialog": return "等待填写"
        case "ask_user_question": return "等待选择"
        case "settled": return "等待下一步"
        default: return "需要你"
        }
    }
}
