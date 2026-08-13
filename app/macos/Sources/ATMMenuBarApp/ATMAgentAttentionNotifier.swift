import Combine
import Foundation

/// What changed between two snapshots of "who is blocked on you".
struct ATMAgentAttentionChange: Equatable {
    /// Sessions that just started waiting and deserve a banner.
    var post: [ATMLiveSession] = []
    /// Session ids whose banner should be pulled back.
    var withdraw: [String] = []

    var isEmpty: Bool { post.isEmpty && withdraw.isEmpty }
}

/// Turns a stream of snapshots into at most one banner per blocked session.
///
/// No priming pass, unlike `ATMAgentSoundTransitionTracker`: `attentionSignal` is
/// stamped onto the snapshot from hook events *this process* received, so a fresh
/// launch starts with an empty set by construction. There is no history here to
/// replay.
struct ATMAgentAttentionTracker {
    private var notified = Set<String>()

    mutating func next(for sessions: [ATMLiveSession]) -> ATMAgentAttentionChange {
        let waiting = sessions.filter(\.needsHookAttention)
        let waitingIDs = Set(waiting.map(\.id))

        var change = ATMAgentAttentionChange()
        change.post = waiting.filter { !notified.contains($0.id) }
        // Subtracting from `notified` rather than iterating the snapshot: a
        // session can vanish from the snapshot outright (terminal closed, process
        // gone), and its banner has to go with it.
        change.withdraw = notified.subtracting(waitingIDs).sorted()
        notified = waitingIDs
        return change
    }
}

/// Everything the notch did that was not drawing.
///
/// The notch owned three jobs the app still needs — the live-status poll, the
/// state-change sounds, and noticing that an agent is blocked — and one it did
/// not, which was being a window pinned above the menu bar around the clock.
/// This keeps the three and drops the fourth: the "an agent needs you" moment
/// goes to Notification Center, which already knows about 专注模式 and keeps a
/// history, and the rest stops being displayed at all.
@MainActor
final class ATMAgentAttentionNotifier {
    private let store: ATMDataStore
    private var cancellables = Set<AnyCancellable>()
    private var isPolling = false
    private var attentionTracker = ATMAgentAttentionTracker()
    private var soundTransitionTracker = ATMAgentSoundTransitionTracker()

    init(store: ATMDataStore) {
        self.store = store
        bindState()
        // Unconditional, where the notch gated this on its own visibility toggle.
        // Nothing else keeps the poll alive once the desktop window closes — the
        // normal state for a menu bar app — and without it an unhooked agent's
        // attention would never be noticed at all.
        store.startLiveStatusPolling()
        isPolling = true
    }

    func stop() {
        cancellables.removeAll()
        if isPolling {
            store.stopLiveStatusPolling()
            isPolling = false
        }
    }

    private func bindState() {
        store.$dashboardState
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                self?.handleSessionUpdate()
            }
            .store(in: &cancellables)

        // Sound straight off the event, not off the next snapshot: the event says
        // what happened, so it needs no inference and does not wait for a poll.
        store.agentEvents.didApplyEvent
            .sink { [weak self] event in
                self?.handleAgentEvent(event)
            }
            .store(in: &cancellables)
    }

    /// Attention is read off the merged snapshot rather than off the event, even
    /// though hook events are what produce it.
    ///
    /// `ATMDataStore.startAgentEventListener` re-merges the attention overlay in
    /// memory the instant an event lands — no CLI, no debounce — so the snapshot
    /// is already the fastest view of this, *and* it is the only view that also
    /// covers agents with no hooks. One source means no cross-path deduplication.
    private func handleSessionUpdate() {
        let sessions = store.snapshot.liveStatus.sessions

        // Hook-backed sessions announce themselves through `handleAgentEvent`;
        // diffing snapshots covers the agents that have no hooks.
        let soundEvent = soundTransitionTracker.nextEvent(for: sessions) { [store] session in
            store.agentEvents.isHookBacked(session)
        }

        let change = attentionTracker.next(for: sessions)
        for session in change.post {
            ATMNotificationManager.shared.sendAgentAttention(session)
        }
        for sessionID in change.withdraw {
            ATMNotificationManager.shared.withdrawAgentAttention(sessionID: sessionID)
        }

        if let soundEvent {
            ATMAgentSoundPlayer.shared.play(soundEvent)
        }
    }

    /// Sound only. The banner is decided in `handleSessionUpdate` off the merged
    /// snapshot, which this event has already refreshed.
    private func handleAgentEvent(_ event: ATMAgentEvent) {
        guard let soundEvent = event.event.soundEvent else { return }
        ATMAgentSoundPlayer.shared.play(soundEvent)
    }
}
