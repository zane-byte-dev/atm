import Combine
import Foundation

/// Holds the attention signals pushed by agent hooks and joins them onto the
/// polled session snapshot.
///
/// Hooks tell us *that* something happened to a session id; `atm session status`
/// still owns what a session *is* (project, model, transcript excerpts). So the
/// bus is an overlay, not a replacement: it never invents rows, it only marks
/// existing ones — which keeps agents that have no hooks working exactly as
/// before.
@MainActor
final class ATMAgentEventBus: ObservableObject {
    /// Signals keyed by whichever identifier the sender had.
    @Published private(set) var signals: [String: ATMAgentAttentionSignal] = [:]
    /// When a hook last reached us at all. Drives the "hooks are live" hint in
    /// settings and lets the store slow its polling down.
    @Published private(set) var lastEventAt: Date?
    /// Identifiers we have ever received an event for, i.e. sessions whose agent
    /// has hooks installed. Used to stop the snapshot-diffing sound tracker from
    /// chiming a second time for a moment an event already announced.
    @Published private(set) var hookBackedKeys: Set<String> = []
    /// The subset of `hookBackedKeys` that identified a *session*, not just a
    /// directory.
    ///
    /// `hookBackedKeys` also collects `cwd`, which is right for "did a hook ever
    /// reach us about this work" but too broad to hand a session's state over to
    /// hooks: two agents in one repo share a cwd, so a hooked Codex session would
    /// make an unhooked copilot session in the same directory look hook-backed and
    /// silence its state forever. Session ids do not collide that way.
    @Published private(set) var hookSessionKeys: Set<String> = []

    private var listener: ATMAgentEventListener?
    /// Non-nil when the listener could not start, so settings can say why
    /// instead of silently showing "not connected".
    @Published private(set) var startupError: String?

    /// Fires after each applied event so the store can refresh immediately
    /// rather than waiting out the poll interval.
    let didApplyEvent = PassthroughSubject<ATMAgentEvent, Never>()

    init() {}

    func start(path: String = ATMAgentEventListener.defaultSocketPath()) {
        guard listener == nil else { return }
        let listener = ATMAgentEventListener(path: path) { [weak self] event in
            // Hop to the main actor: the listener delivers on its own queue.
            Task { @MainActor in self?.apply(event, now: Date()) }
        }
        do {
            try listener.start()
            self.listener = listener
            startupError = nil
        } catch {
            startupError = error.localizedDescription
        }
    }

    func stop() {
        listener?.stop()
        listener = nil
    }

    var isListening: Bool { listener != nil }

    var socketPath: String? { listener?.socketPath }

    /// Applies one event to the overlay. Returns whether the attention overlay
    /// changed; the event is published either way, because a `completed` event on
    /// a session with no pending signal still means the snapshot is stale.
    @discardableResult
    func apply(_ event: ATMAgentEvent, now: Date = Date()) -> Bool {
        lastEventAt = now
        let keys = event.joinCandidates
        guard !keys.isEmpty else { return false }
        // `resumed` is deliberately not counted. Both sets decide who owns a
        // session's *turn* state, and a PostToolUse event only proves the tool
        // hooks fire — it says nothing about whether the turn-end hook does.
        // Grok Build is exactly that case: its tool hooks are reliable while its
        // lifecycle hooks are not, so crediting it here would hand turn state to
        // hooks that never report one, silencing both the completion card and
        // the chime that snapshot diffing would otherwise still produce.
        if event.event.provesTurnStateReporting {
            hookBackedKeys.formUnion(keys)
            if let sessionKey = event.sessionKey {
                hookSessionKeys.insert(sessionKey)
            }
        }

        var changed = false
        switch event.event {
        case .attention:
            let signal = ATMAgentAttentionSignal(
                reason: event.reason ?? "notification",
                tool: event.tool,
                text: event.text,
                source: event.source,
                receivedAt: now
            )
            // A sender that named its session gets exactly that key. Writing the
            // `cwd` alias too is what turned one agent's permission prompt into a
            // 等待授权 banner on every other session in the same repository:
            // `joinKeys` lets any row claim a directory key, and several agents
            // share one. The alias is only the last resort for a hook that
            // reported no session id at all, which `Envelope.Validate` permits.
            for key in event.sessionKey.map({ [$0] }) ?? keys where signals[key] != signal {
                signals[key] = signal
                changed = true
            }
        case .started, .resumed, .completed, .sessionEnd:
            if let sessionKey = event.sessionKey, signals.removeValue(forKey: sessionKey) != nil {
                changed = true
            }
            // Clearing a directory key is the counterpart of the alias above, so
            // it has to be as narrow: another agent's live signal can be sitting
            // under the same `cwd`, and retiring it here would leave that agent
            // waiting with no banner at all.
            if let cwd = event.cwdKey, signals[cwd]?.source == event.source {
                signals.removeValue(forKey: cwd)
                changed = true
            }
        case .sessionStart:
            break
        }

        purgeExpired(now: now)
        didApplyEvent.send(event)
        return changed
    }

    /// Whether this session's agent is reporting through hooks, so snapshot
    /// diffing should not also announce its transitions.
    func isHookBacked(_ session: ATMLiveSession) -> Bool {
        ATMAgentAttentionJoin.joinKeys(for: session).contains { hookBackedKeys.contains($0) }
    }

    /// Whether hooks own this session's turn state outright, so snapshot diffing
    /// must not infer completion for it at all.
    ///
    /// Stricter than `isHookBacked` about identity: this decides who is allowed
    /// to say a turn finished, and a `cwd` match would cover the unhooked agents
    /// running in the same repo. Broader about evidence: an installed `Stop` hook
    /// counts even before the first event arrives — see `ATMAgentHookAuthority`.
    func isHookAuthoritative(
        _ session: ATMLiveSession,
        report: ATMAgentHookReport? = nil
    ) -> Bool {
        ATMAgentHookAuthority.isAuthoritative(
            session: session,
            seenSessionKeys: hookSessionKeys,
            report: report,
            isListening: isListening
        )
    }

    /// Drops signals past their TTL. Called on every event and before each join
    /// so a session cannot stay orange after the agent went away without
    /// reporting.
    func purgeExpired(now: Date = Date()) {
        for (key, signal) in signals where !signal.isLive(at: now) {
            signals.removeValue(forKey: key)
        }
    }

    /// Returns the live signal for a session, if any.
    func signal(for session: ATMLiveSession, now: Date = Date()) -> ATMAgentAttentionSignal? {
        ATMAgentAttentionJoin.signal(for: session, in: signals, now: now)
    }
}

/// Decides who owns a session's turn state: its hooks, or the snapshot diffing
/// that covers agents without any.
///
/// Kept pure and separate from the bus so the rule can be tested without a
/// listener, a store, or a running app.
enum ATMAgentHookAuthority {
    /// - Parameters:
    ///   - seenSessionKeys: session identifiers a hook event has already arrived
    ///     for (`ATMAgentEventBus.hookSessionKeys`).
    ///   - report: what `atm agent hook status` found in the agents' config files.
    ///   - isListening: whether the hook socket is actually accepting events. If
    ///     it is not, no hook can reach us and handing the state over would leave
    ///     the session with no source at all.
    static func isAuthoritative(
        session: ATMLiveSession,
        seenSessionKeys: Set<String>,
        report: ATMAgentHookReport?,
        isListening: Bool
    ) -> Bool {
        if ATMAgentAttentionJoin.sessionKeys(for: session).contains(where: seenSessionKeys.contains) {
            return true
        }
        // No event for this session yet. That is the normal state for every
        // conversation that was already running when ATM launched, and waiting
        // for the first one is what kept the launch window guessing from text —
        // a guess that is degenerate for Claude Code, whose parser never fills
        // `latest_result`, so "is this result from the current turn" ends up
        // comparing the latest reply against itself and is always true.
        //
        // A registered `Stop` hook is enough to know who owns the state for
        // Claude/Codex. Grok Build is different: its lifecycle hooks are not
        // always observed even when the file is installed (tool hooks still
        // fire), and its transcript parser *does* fill `latest_result`. Handing
        // Grok to hooks before the first event would silence the text path and
        // leave the island darker than with no install at all.
        //
        // Qoder is held to the same bar for a different reason: it reads
        // ~/.qoder/settings.json once at launch, so a freshly installed hook does
        // not fire until the app is restarted. Trusting the file would leave
        // every Qoder session dark in between. Its `UserPromptSubmit` arrives at
        // the start of a turn, before any assistant text a snapshot could
        // misread, so waiting for the first event costs nothing and heals itself.
        guard isListening,
              let source = ATMAgentHookSource.source(forTool: session.tool),
              let report else { return false }
        if source == "grokbuild" || source == "qoder" {
            return false
        }
        return report.sources.contains { $0.source == source && $0.reportsTurnEnd }
    }
}

/// The join between hook signals and polled sessions.
///
/// Kept out of `ATMAgentEventBus` so it is not main-actor isolated: the snapshot
/// merge runs wherever the decode happens, and the rules are worth testing
/// without a listener or a running app.
enum ATMAgentAttentionJoin {
    static func signal(
        for session: ATMLiveSession,
        in signals: [String: ATMAgentAttentionSignal],
        now: Date
    ) -> ATMAgentAttentionSignal? {
        for key in sessionKeys(for: session) {
            guard let signal = signals[key], signal.isLive(at: now) else { continue }
            return signal
        }
        // The directory fallback, and only for the agent that raised it. A repo
        // routinely holds a Codex session, a Claude Code session and an
        // unattended run at once; matching on `cwd` alone made one agent's
        // 等待授权 appear on all of them, naming the wrong tool and pointing at
        // sessions that were not waiting for anything.
        guard let cwd = trimmed(session.cwd),
              let signal = signals[cwd],
              signal.isLive(at: now),
              ATMAgentHookSource.source(forTool: session.tool) == signal.source
        else { return nil }
        return signal
    }

    /// The identifiers a session can be matched by, most specific first.
    ///
    /// `resumeID` matters as much as `sessionID`: the Codex parser truncates its
    /// session id to eight characters and keeps the full thread id — the one the
    /// hook reports — in `resumeID`. `cwd` is the last resort for agents whose
    /// hook session id does not correspond to anything the parser stores.
    ///
    /// Used by `isHookBacked`, where being generous only silences a duplicate
    /// chime. The attention join deliberately does not use this: see `signal`,
    /// which gates the `cwd` fallback on the agent matching.
    static func joinKeys(for session: ATMLiveSession) -> [String] {
        var keys = sessionKeys(for: session)
        if let cwd = trimmed(session.cwd), !keys.contains(cwd) {
            keys.append(cwd)
        }
        return keys
    }

    /// The identifiers that name this session specifically, without the `cwd`
    /// fallback that `joinKeys` ends with.
    static func sessionKeys(for session: ATMLiveSession) -> [String] {
        var keys: [String] = []
        for value in [session.sessionID, session.resumeID] {
            guard let value = trimmed(value), !keys.contains(value) else { continue }
            keys.append(value)
        }
        return keys
    }

    private static func trimmed(_ value: String?) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !value.isEmpty else { return nil }
        return value
    }

    /// Stamps each session with its live hook signal.
    static func merge(
        _ sessions: [ATMLiveSession],
        signals: [String: ATMAgentAttentionSignal],
        now: Date
    ) -> [ATMLiveSession] {
        guard !signals.isEmpty else { return sessions }
        return sessions.map { session in
            var copy = session
            copy.attentionSignal = signal(for: session, in: signals, now: now)
            return copy
        }
    }
}
