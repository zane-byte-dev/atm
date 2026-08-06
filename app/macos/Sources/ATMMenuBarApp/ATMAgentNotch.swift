import AppKit
import Combine
import SwiftUI

/// How long a finished session lingers in the notch before it auto-hides.
///
/// Ping Island keeps inactive sessions around for thirty minutes; that is the
/// default here too, but it is now the user's call. `always` keeps every
/// observed session for as long as ATM runs (attention always survives
/// regardless — see `isVisibleInAgentNotch`).
enum ATMAgentNotchRetention: Int, CaseIterable, Identifiable {
    case minutes15 = 15
    case minutes30 = 30
    case minutes60 = 60
    /// Never auto-hide on age alone.
    case always = 0

    var id: Int { rawValue }

    static let `default`: ATMAgentNotchRetention = .minutes30

    /// The age ceiling in seconds. `always` maps to `Int.max` so the age check
    /// never trips.
    var seconds: Int {
        self == .always ? .max : rawValue * 60
    }

    var label: String {
        switch self {
        case .minutes15: return "15 分钟"
        case .minutes30: return "30 分钟"
        case .minutes60: return "60 分钟"
        case .always: return "一直保留"
        }
    }
}

/// How long a completion / attention card stays on screen before it collapses
/// back to the compact strip on its own.
///
/// `manual` never runs a timer: the card waits until you click elsewhere or a
/// newer event replaces it. Everything else auto-collapses after `seconds`
/// unless the cursor is still on the card, in which case it collapses on exit.
enum ATMAgentNotchNotificationDwell: Int, CaseIterable, Identifiable {
    case seconds4 = 4
    case seconds8 = 8
    case seconds15 = 15
    /// Do not auto-collapse; dismiss by clicking away or on the next event.
    case manual = 0

    var id: Int { rawValue }

    static let `default`: ATMAgentNotchNotificationDwell = .seconds8

    /// `nil` means "no timer" — the caller skips scheduling a dismissal.
    var seconds: TimeInterval? {
        self == .manual ? nil : TimeInterval(rawValue)
    }

    var label: String {
        switch self {
        case .seconds4: return "4 秒"
        case .seconds8: return "8 秒"
        case .seconds15: return "15 秒"
        case .manual: return "手动收起"
        }
    }
}

/// Which physical screen the notch attaches to.
///
/// `automatic` reproduces the original behavior: prefer a screen with a real
/// notch, then the main screen, then whatever is first. `main` pins it to the
/// active-menu-bar screen, and `display` pins it to one specific monitor by its
/// CoreGraphics display id. The id is stored rather than the index because
/// indices reshuffle when monitors are plugged or unplugged.
enum ATMAgentNotchScreenSelection: RawRepresentable, Equatable {
    case automatic
    case main
    case display(CGDirectDisplayID)

    static let `default`: ATMAgentNotchScreenSelection = .automatic

    var rawValue: String {
        switch self {
        case .automatic: return "auto"
        case .main: return "main"
        case .display(let id): return "display:\(id)"
        }
    }

    init?(rawValue: String) {
        switch rawValue {
        case "auto": self = .automatic
        case "main": self = .main
        default:
            guard rawValue.hasPrefix("display:"),
                  let id = CGDirectDisplayID(rawValue.dropFirst("display:".count)) else {
                return nil
            }
            self = .display(id)
        }
    }
}

/// Where the compact strip sits on a screen with no physical notch.
///
/// Ignored on a notched screen: there the strip has to center on the camera
/// cutout, so honoring a leading/trailing choice would drift it off the notch.
enum ATMAgentNotchStripAlignment: String, CaseIterable, Identifiable {
    case center
    case leading
    case trailing

    var id: String { rawValue }

    static let `default`: ATMAgentNotchStripAlignment = .center

    var label: String {
        switch self {
        case .center: return "顶部居中"
        case .leading: return "靠左"
        case .trailing: return "靠右"
        }
    }
}

enum ATMAgentNotchPreferences {
    static let enabledKey = "ATMAgentNotchEnabled"
    static let retentionKey = "ATMAgentNotchRetentionMinutes"
    static let notificationDwellKey = "ATMAgentNotchNotificationDwellSeconds"
    static let screenSelectionKey = "ATMAgentNotchScreenSelection"
    static let stripAlignmentKey = "ATMAgentNotchStripAlignment"
    static let defaultEnabled = true

    static var isEnabled: Bool {
        if ProcessInfo.processInfo.environment["ATM_DISABLE_AGENT_NOTCH"] == "1" {
            return false
        }
        let defaults = UserDefaults.standard
        guard defaults.object(forKey: enabledKey) != nil else { return defaultEnabled }
        return defaults.bool(forKey: enabledKey)
    }

    static var retention: ATMAgentNotchRetention {
        let defaults = UserDefaults.standard
        guard defaults.object(forKey: retentionKey) != nil else { return .default }
        return ATMAgentNotchRetention(rawValue: defaults.integer(forKey: retentionKey)) ?? .default
    }

    static var notificationDwell: ATMAgentNotchNotificationDwell {
        let defaults = UserDefaults.standard
        guard defaults.object(forKey: notificationDwellKey) != nil else { return .default }
        return ATMAgentNotchNotificationDwell(rawValue: defaults.integer(forKey: notificationDwellKey))
            ?? .default
    }

    static var screenSelection: ATMAgentNotchScreenSelection {
        guard let raw = UserDefaults.standard.string(forKey: screenSelectionKey),
              let selection = ATMAgentNotchScreenSelection(rawValue: raw) else {
            return .default
        }
        return selection
    }

    static var stripAlignment: ATMAgentNotchStripAlignment {
        guard let raw = UserDefaults.standard.string(forKey: stripAlignmentKey),
              let alignment = ATMAgentNotchStripAlignment(rawValue: raw) else {
            return .default
        }
        return alignment
    }

    /// The subset of preferences that changes where or how large the panel is
    /// drawn. Compared before/after a defaults change so the controller only
    /// re-lays-out for a real notch change, not for every unrelated write (the
    /// sound-volume slider writes defaults on every drag tick).
    struct LayoutSignature: Equatable {
        let retention: Int
        let screen: String
        let alignment: String

        static var current: LayoutSignature {
            LayoutSignature(
                retention: ATMAgentNotchPreferences.retention.rawValue,
                screen: ATMAgentNotchPreferences.screenSelection.rawValue,
                alignment: ATMAgentNotchPreferences.stripAlignment.rawValue
            )
        }
    }
}

extension NSScreen {
    /// The CoreGraphics display id, used to pin the notch to a specific monitor
    /// across reconnects. `deviceDescription` hands this back as an `NSNumber`.
    var atmDisplayID: CGDirectDisplayID? {
        (deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.uint32Value
    }
}

extension ATMLiveSession {
    func isVisibleInAgentNotch(recentSeconds: Int) -> Bool {
        activityState != "unobserved"
            && (ageSeconds < recentSeconds || presenceState == .attention)
    }

    var isVisibleInAgentNotch: Bool {
        isVisibleInAgentNotch(recentSeconds: ATMAgentNotchPreferences.retention.seconds)
    }
}

/// Mirrors Ping Island's interaction model: the panel behaves differently
/// depending on why it opened instead of treating every expansion as the same
/// hover state.
enum ATMAgentNotchPresentation: Equatable, CaseIterable {
    case compact
    case hoverExpanded
    case sessionList
    case notification

    var isExpanded: Bool { self != .compact }

    var isPersistent: Bool { self == .sessionList }
}

/// Section heights shared by the window metrics and the SwiftUI content. The
/// panel is sized in AppKit while the rows are drawn in SwiftUI, so both sides
/// have to read the same numbers — otherwise the window ends up taller than
/// what it draws and the surplus shows as dead black space under the last row.
enum ATMAgentNotchLayout {
    /// Ping Island's expanded surface starts with a quiet utility rail rather
    /// than a second navigation bar. It is deliberately taller than the physical
    /// notch so the controls have breathing room on both notched and plain Macs.
    static let expandedToolbarHeight: CGFloat = 44
    static let sessionRowHeight: CGFloat = 80
    static let sessionRowSpacing: CGFloat = 18
    static let listHorizontalInset: CGFloat = 28
    static let listBottomInset: CGFloat = 30
    static let maximumVisibleRows = 3

    static func listHeight(sessionCount: Int) -> CGFloat {
        let rows = max(1, min(sessionCount, maximumVisibleRows))
        let gaps = max(0, rows - 1)
        return expandedToolbarHeight
            + CGFloat(rows) * sessionRowHeight
            + CGFloat(gaps) * sessionRowSpacing
            + listBottomInset
    }
}

/// Detects newly completed turns without replaying results that were already on
/// screen when ATM launched or that briefly disappeared from a later snapshot.
/// The sound tracker answers a different question (which sound wins when several
/// transitions arrive together), so the notch keeps this small presentation
/// tracker independent.
///
/// Only for agents with no hooks. A hooked session's `Stop` event says the turn
/// ended; inferring the same thing from text is not a second opinion but a worse
/// one — Codex writes several `commentary` messages *during* a turn, and each one
/// changes the latest reply, which this tracker cannot tell apart from a final
/// result. So hooked sessions are tracked but never reported; see `hookBacked`.
struct ATMAgentCompletionTransitionTracker {
    private(set) var isPrimed = false
    private var previousInputBySessionID: [String: String] = [:]
    private var previousResultBySessionID: [String: String] = [:]
    private var seenCompletionKeys = Set<String>()
    private(set) var startedSessionIDs = Set<String>()
    private(set) var completedSessionIDs = Set<String>()
    private(set) var newlyCompletedSessionIDs = Set<String>()

    /// - Parameter hookBacked: sessions whose turn state arrives as hook events.
    ///   Their inputs and results are still recorded, so losing hook coverage
    ///   later does not replay a backlog of completions, but they never produce a
    ///   completion from here.
    mutating func nextCompletedSession(
        in sessions: [ATMLiveSession],
        hookBacked: (ATMLiveSession) -> Bool = { _ in false }
    ) -> ATMLiveSession? {
        let visible = sessions.filter { $0.activityState != "unobserved" }
        let inferable = Set(visible.filter { !hookBacked($0) }.map(\.id))
        let currentResults = Dictionary(
            uniqueKeysWithValues: visible.compactMap { session in
                session.latestResultText.map { (session.id, $0) }
            }
        )
        let currentInputs = Dictionary(
            uniqueKeysWithValues: visible.compactMap { session in
                session.latestUserInputText.map { (session.id, $0) }
            }
        )

        // The store publishes an empty placeholder before its first CLI response.
        // Treating that as the baseline would make every already-finished session
        // look newly completed a moment later and replay history on app launch.
        guard isPrimed || !visible.isEmpty else { return nil }

        guard isPrimed else {
            isPrimed = true
            previousInputBySessionID = currentInputs
            previousResultBySessionID = currentResults
            completedSessionIDs = Set(visible.compactMap { session in
                guard inferable.contains(session.id),
                      let result = currentResults[session.id],
                      completionResultBelongsToCurrentTurn(result, session: session) else {
                    return nil
                }
                return session.id
            })
            for (sessionID, result) in currentResults {
                seenCompletionKeys.insert(completionKey(sessionID: sessionID, result: result))
            }
            return nil
        }

        newlyCompletedSessionIDs.removeAll()
        let changedInputSessionIDs = Set(currentInputs.compactMap { sessionID, input in
            previousInputBySessionID[sessionID] != input ? sessionID : nil
        })
        let staleResultSessionIDs: Set<String> = Set(visible.compactMap { session in
            guard session.isCurrentlyActive,
                  let result = currentResults[session.id],
                  !completionResultBelongsToCurrentTurn(result, session: session) else {
                return nil
            }
            return session.id
        })
        // Hooks own a hooked session's whole turn, `started` included: reading a
        // start out of the snapshot too could clear a completion the hook had just
        // set, which is the same conflation in the other direction.
        startedSessionIDs = changedInputSessionIDs
            .union(staleResultSessionIDs)
            .intersection(inferable)
        completedSessionIDs.subtract(startedSessionIDs)

        let newlyCompleted = visible
            .filter { session in
                guard let result = currentResults[session.id],
                      previousResultBySessionID[session.id] != result else { return false }
                // Record the key even for a hooked session, so that if hook
                // coverage stops later this result is not announced belatedly.
                let isFirstTime = seenCompletionKeys.insert(
                    completionKey(sessionID: session.id, result: result)
                ).inserted
                return isFirstTime && inferable.contains(session.id)
            }
        newlyCompletedSessionIDs = Set(newlyCompleted.map(\.id))
        completedSessionIDs.formUnion(newlyCompletedSessionIDs)
        let completed = newlyCompleted.min { lhs, rhs in
                if lhs.ageSeconds != rhs.ageSeconds { return lhs.ageSeconds < rhs.ageSeconds }
                return lhs.id < rhs.id
            }

        previousResultBySessionID = currentResults
        previousInputBySessionID = currentInputs
        return completed
    }

    private func completionKey(sessionID: String, result: String) -> String {
        "\(sessionID)\u{1F}\(result)"
    }

    /// `latest_result` intentionally survives while the next turn is running.
    /// Treat it as the current completion only when the latest visible Agent
    /// reply is that result (allowing for the parser's different truncation
    /// limits). Otherwise the card represents work in progress, not the prior
    /// turn's terminal state.
    private func completionResultBelongsToCurrentTurn(
        _ result: String,
        session: ATMLiveSession
    ) -> Bool {
        guard let answer = session.lastAnswer?.trimmingCharacters(in: .whitespacesAndNewlines),
              !answer.isEmpty else {
            return !session.isCurrentlyActive
        }
        let resultKey = comparableCompletionText(result)
        let answerKey = comparableCompletionText(answer)
        guard !resultKey.isEmpty, !answerKey.isEmpty else { return false }
        return resultKey == answerKey
            || resultKey.hasPrefix(answerKey)
            || answerKey.hasPrefix(resultKey)
    }

    private func comparableCompletionText(_ value: String) -> String {
        ATMMarkdown.plainSummary(value, limit: 4_000)
            .lowercased()
            .split(whereSeparator: { $0.isWhitespace })
            .joined(separator: " ")
    }
}

enum ATMAgentNotchNotificationKind: Equatable {
    case attention
    case completed
}

enum ATMAgentNotchSessionState: Equatable {
    case attention
    case working
    case completed
    case recent

    static func resolve(
        session: ATMLiveSession,
        completedSessionIDs: Set<String>
    ) -> ATMAgentNotchSessionState {
        if session.presenceState == .attention { return .attention }
        if completedSessionIDs.contains(session.id) { return .completed }
        if session.isCurrentlyActive { return .working }
        return .recent
    }

    var title: String {
        switch self {
        case .attention: return "需要你"
        case .working: return "工作中"
        case .completed: return "已完成"
        case .recent: return "最近"
        }
    }

    /// Corner mark drawn on the agent tile.
    ///
    /// `working` is carried by the breathing ring and `recent` by the dimmed
    /// tile, so neither gets a glyph: a badge that appears on every row is not
    /// a signal, it is decoration.
    var cornerGlyph: String? {
        switch self {
        case .attention: return "exclamationmark"
        case .completed: return "checkmark"
        case .working, .recent: return nil
        }
    }
}

/// Breathing ring around a session's agent tile — the row's only "still
/// running" mark now that the 工作中 chip is gone.
///
/// Deliberately confined to the expanded panel. The compact strip is a
/// borderless window pinned above the menu bar on every Space for as long as
/// ATM runs; a `repeatForever` there would repaint the top of the screen around
/// the clock. Opacity and scale are also the two cheapest properties to
/// animate — no path is re-tessellated per frame.
///
/// `working` is the one state with no corner glyph, so with Reduce Motion on
/// the ring must still be the whole signal: it stays, at a steady opacity, and
/// only the breathing stops. Fading it out along with the animation would make
/// a working row indistinguishable from a recent one.
private struct ATMAgentNotchWorkingRing: View {
    let tint: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isBreathing = false

    var body: some View {
        RoundedRectangle(cornerRadius: 11, style: .continuous)
            .stroke(tint, lineWidth: 1.4)
            // Negative padding grows the ring past the 28pt tile the overlay is
            // sized to, so it reads as a halo instead of an inner border.
            .padding(-2.5)
            .opacity(reduceMotion ? 0.72 : (isBreathing ? 0.20 : 0.85))
            .scaleEffect(reduceMotion || !isBreathing ? 1 : 1.07)
            .onAppear { syncBreathing() }
            // Also re-sync on toggle: the setting can change while the panel is
            // open, and `onAppear` will not fire again for a row that is already
            // on screen.
            .onChange(of: reduceMotion) { _ in syncBreathing() }
    }

    private func syncBreathing() {
        // The zero-duration animation is what actually stops a `repeatForever`
        // already in flight; a plain assignment would leave it cycling.
        withAnimation(
            reduceMotion
                ? .linear(duration: 0)
                : .easeInOut(duration: 1.15).repeatForever(autoreverses: true)
        ) {
            isBreathing = !reduceMotion
        }
    }
}

struct ATMAgentNotchNotification: Equatable {
    let kind: ATMAgentNotchNotificationKind
    let sessionID: String
    let summary: String?
}

/// Hover rules for the notch, kept pure so the parts that used to oscillate can
/// be tested.
///
/// The panel deliberately does not use SwiftUI's `.onHover`. That reports hover
/// through a tracking area on the hosting view, and the view resizes every time
/// the panel expands — which makes AppKit rebuild the tracking area and emit an
/// exit even though the cursor never moved. Acting on that exit collapsed the
/// panel, the cursor was then inside the compact strip again, and the whole thing
/// flapped open and shut. So hover is derived from the real cursor position
/// instead, and every transition is re-verified against it.
enum ATMAgentNotchHover {
    /// How long the cursor must stay in the strip before the panel opens.
    ///
    /// The strip sits on top of the menu bar, so without a dwell every trip to
    /// the clock or the Wi-Fi menu drags the panel open in passing.
    static let openDelay: TimeInterval = 0.18

    /// Grace period before collapsing, so a momentary exit — rounding at the
    /// panel's corners, a jitter across the edge — does not close it.
    static let closeDelay: TimeInterval = 0.22

    /// How far the trigger area is pulled in from each side of the compact strip.
    ///
    /// The strip has to be wide enough to draw the agent mark and the session
    /// count, but those outer few points sit right where the menu bar's own items
    /// are, so treating them as a trigger meant reaching for a status item opened
    /// the panel. The inset keeps the visual size and shrinks only the target.
    /// Clicking still works everywhere the strip is drawn.
    static let horizontalTriggerInset: CGFloat = 20

    /// The strip area that counts as hovering, narrower than the strip is drawn.
    static func triggerFrame(compactFrame: CGRect) -> CGRect {
        // Never inset so far that nothing is left on a narrow strip.
        let inset = min(horizontalTriggerInset, max(0, (compactFrame.width - 80) / 2))
        return compactFrame.insetBy(dx: inset, dy: 0)
    }

    /// The region that counts as hovering.
    ///
    /// While compact this is the inset strip, so the panel opens where it is
    /// visible rather than anywhere the expanded panel would later cover, and not
    /// from the outer edges that overlap the menu bar's own items. Once open it is
    /// the full strip plus the whole panel, so moving down into the session list
    /// keeps it open and the edges stop being a cliff.
    static func region(
        presentation: ATMAgentNotchPresentation,
        compactFrame: CGRect,
        panelFrame: CGRect
    ) -> CGRect {
        presentation == .compact
            ? triggerFrame(compactFrame: compactFrame)
            : compactFrame.union(panelFrame)
    }

    /// Whether a dwell that just elapsed should open the panel.
    static func shouldOpen(
        presentation: ATMAgentNotchPresentation,
        cursorIsInRegion: Bool
    ) -> Bool {
        presentation == .compact && cursorIsInRegion
    }

    /// Whether a grace period that just elapsed should collapse the panel.
    static func shouldClose(
        presentation: ATMAgentNotchPresentation,
        cursorIsInRegion: Bool,
        dismissesNotificationOnExit: Bool
    ) -> Bool {
        // The cursor really being inside outranks whatever event scheduled this.
        // This single check is what makes the flapping impossible.
        guard !cursorIsInRegion else { return false }
        switch presentation {
        case .hoverExpanded: return true
        case .notification: return dismissesNotificationOnExit
        // Pinned by a click: only an explicit dismissal or an outside click closes
        // it, never the cursor wandering off.
        case .sessionList, .compact: return false
        }
    }
}

/// Pure geometry kept separate from AppKit so notch detection and window
/// placement can be covered by unit tests.
struct ATMAgentNotchMetrics: Equatable {
    static let fallbackNotchSize = CGSize(width: 180, height: 32)

    let notchSize: CGSize
    let hasPhysicalNotch: Bool

    static func detect(
        screenFrame: CGRect,
        safeAreaTop: CGFloat,
        auxiliaryTopLeftWidth: CGFloat?,
        auxiliaryTopRightWidth: CGFloat?
    ) -> ATMAgentNotchMetrics {
        let height = ceil(safeAreaTop)
        guard height > 0 else {
            return ATMAgentNotchMetrics(
                notchSize: fallbackNotchSize,
                hasPhysicalNotch: false
            )
        }

        let leftWidth = max(0, auxiliaryTopLeftWidth ?? 0)
        let rightWidth = max(0, auxiliaryTopRightWidth ?? 0)
        let width: CGFloat
        if leftWidth > 0, rightWidth > 0 {
            width = max(
                fallbackNotchSize.width,
                ceil(screenFrame.width - leftWidth - rightWidth + 4)
            )
        } else {
            width = fallbackNotchSize.width
        }
        return ATMAgentNotchMetrics(
            notchSize: CGSize(width: width, height: height),
            hasPhysicalNotch: true
        )
    }

    var compactSize: CGSize {
        if hasPhysicalNotch {
            // Exactly the cutout height: on a notched Mac the menu bar is as tall
            // as the notch, so any surplus hangs the strip's black lip below the
            // menu bar and it stops reading as part of the notch.
            return CGSize(width: notchSize.width + 124, height: notchSize.height)
        }
        return CGSize(width: 286, height: 38)
    }

    func expandedSize(
        screenFrame: CGRect,
        presentation: ATMAgentNotchPresentation,
        sessionCount: Int
    ) -> CGSize {
        let maximumWidth = max(compactSize.width, screenFrame.width - 64)
        switch presentation {
        case .compact:
            return compactSize
        // A hover peek and a notification are both a single card, so they are
        // the same height however many sessions are live. Only the pinned list
        // grows with the session count.
        case .notification, .hoverExpanded:
            return CGSize(
                width: min(maximumWidth, 600),
                height: ATMAgentNotchLayout.listHeight(sessionCount: 1)
            )
        case .sessionList:
            return CGSize(
                width: min(maximumWidth, 600),
                height: ATMAgentNotchLayout.listHeight(sessionCount: sessionCount)
            )
        }
    }

    func windowFrame(
        screenFrame: CGRect,
        presentation: ATMAgentNotchPresentation,
        sessionCount: Int,
        alignment: ATMAgentNotchStripAlignment = .center
    ) -> CGRect {
        let size = expandedSize(
            screenFrame: screenFrame,
            presentation: presentation,
            sessionCount: sessionCount
        )
        // Flush to the top edge on every screen. On a notched Mac that lines the
        // strip up with the cutout; on a notchless one an inset here left the
        // strip floating a few points below the top instead of hugging it.
        return CGRect(
            x: originX(screenFrame: screenFrame, width: size.width, alignment: alignment),
            y: screenFrame.maxY - size.height,
            width: size.width,
            height: size.height
        )
    }

    /// Horizontal origin honoring the strip alignment.
    ///
    /// A physical notch is always centered on the camera cutout — a leading or
    /// trailing choice there would slide the strip off the notch it is meant to
    /// wrap — so `hasPhysicalNotch` forces center regardless of the setting. An
    /// expanded panel is likewise centered: only the compact strip is small
    /// enough for its side position to matter, and a wide panel pinned to a
    /// corner would hang past the screen edge.
    private func originX(
        screenFrame: CGRect,
        width: CGFloat,
        alignment: ATMAgentNotchStripAlignment
    ) -> CGFloat {
        let centered = screenFrame.midX - width / 2
        guard !hasPhysicalNotch else { return centered }
        // Only the compact strip aligns to a side; the expanded panel is wide
        // and stays centered so it never spills off the edge.
        guard width <= compactSize.width + 1 else { return centered }
        let margin: CGFloat = 12
        switch alignment {
        case .center: return centered
        case .leading: return screenFrame.minX + margin
        case .trailing: return screenFrame.maxX - width - margin
        }
    }
}

@MainActor
private final class ATMAgentNotchSurfaceModel: ObservableObject {
    @Published var metrics: ATMAgentNotchMetrics
    @Published var presentation: ATMAgentNotchPresentation = .compact
    @Published var notification: ATMAgentNotchNotification?
    @Published var completedSessionIDs = Set<String>()

    var isExpanded: Bool { presentation.isExpanded }

    init(metrics: ATMAgentNotchMetrics) {
        self.metrics = metrics
    }
}

private final class ATMAgentNotchPanel: NSPanel {
    init() {
        super.init(
            contentRect: .zero,
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        isFloatingPanel = true
        becomesKeyOnlyIfNeeded = true
        isOpaque = false
        backgroundColor = .clear
        hasShadow = false
        hidesOnDeactivate = false
        isMovable = false
        isReleasedWhenClosed = false
        // Needed for the local mouse monitor to see movement while ATM is the
        // active app; the global monitor covers the usual case, where it is not.
        acceptsMouseMovedEvents = true
        level = .mainMenu + 3
        collectionBehavior = [
            .canJoinAllSpaces,
            .fullScreenAuxiliary,
            .stationary,
            .ignoresCycle,
        ]
    }

    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

@MainActor
final class ATMAgentNotchController {
    private let store: ATMDataStore
    private let panel = ATMAgentNotchPanel()
    private let surfaceModel: ATMAgentNotchSurfaceModel
    private let onOpenSession: (ATMLiveSession) -> Void
    private let onOpenAgents: (ATMLiveSession?) -> Void
    private let onOpenSettings: () -> Void
    private var cancellables = Set<AnyCancellable>()
    private var isPolling = false
    private var isHovering = false
    private var notificationDismissWorkItem: DispatchWorkItem?
    private var shouldDismissNotificationOnHoverExit = false
    private var hoverOpenWorkItem: DispatchWorkItem?
    private var hoverCloseWorkItem: DispatchWorkItem?
    private var localMouseMonitor: Any?
    private var globalMouseMonitor: Any?
    private var previousAttentionIDs: Set<String>?
    private var soundTransitionTracker = ATMAgentSoundTransitionTracker()
    private var completionTransitionTracker = ATMAgentCompletionTransitionTracker()
    private var completionSessionsAlreadyNotifiedByHook = Set<String>()
    private var hasSeededCompletionState = false
    private var layoutSignature = ATMAgentNotchPreferences.LayoutSignature.current

    init(
        store: ATMDataStore,
        onOpenSession: @escaping (ATMLiveSession) -> Void,
        onOpenAgents: @escaping (ATMLiveSession?) -> Void,
        onOpenSettings: @escaping () -> Void
    ) {
        self.store = store
        self.onOpenSession = onOpenSession
        self.onOpenAgents = onOpenAgents
        self.onOpenSettings = onOpenSettings
        let screen = Self.preferredScreen()
        surfaceModel = ATMAgentNotchSurfaceModel(
            metrics: screen.map(Self.metrics(for:))
                ?? ATMAgentNotchMetrics(notchSize: .zero, hasPhysicalNotch: false)
        )

        configurePanel()
        installMouseMonitors()
        bindState()
        updateEnabledState()
        refreshPresentation(animated: false)
    }

    func stop() {
        notificationDismissWorkItem?.cancel()
        notificationDismissWorkItem = nil
        cancelHoverWork()
        removeMouseMonitors()
        if isPolling {
            store.stopLiveStatusPolling()
            isPolling = false
        }
        cancellables.removeAll()
        panel.orderOut(nil)
    }

    private func configurePanel() {
        panel.contentViewController = NSHostingController(
            rootView: ATMAgentNotchView(
                store: store,
                surfaceModel: surfaceModel,
                onExpandSessionList: { [weak self] in
                    self?.expandSessionList()
                },
                onOpenSession: { [weak self] session in
                    self?.present(.compact)
                    self?.onOpenSession(session)
                },
                onOpenAgents: { [weak self] session in
                    self?.onOpenAgents(session)
                },
                onOpenSettings: { [weak self] in
                    self?.onOpenSettings()
                }
            )
        )
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

        NotificationCenter.default.publisher(for: UserDefaults.didChangeNotification)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                self?.updateEnabledState()
                self?.applyLayoutPreferenceChangesIfNeeded()
            }
            .store(in: &cancellables)

        NotificationCenter.default.publisher(for: NSApplication.didChangeScreenParametersNotification)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                self?.refreshPresentation(animated: false)
            }
            .store(in: &cancellables)
    }

    private func updateEnabledState() {
        let enabled = ATMAgentNotchPreferences.isEnabled
        // `isPolling` mirrors the toggle, so this also filters out the unrelated
        // `UserDefaults.didChangeNotification` traffic. Without the guard, any
        // defaults write anywhere in the app ran an unanimated `setFrame` that
        // cut an in-flight hover animation short.
        guard enabled != isPolling else { return }
        if enabled {
            store.startLiveStatusPolling()
            isPolling = true
        } else {
            store.stopLiveStatusPolling()
            isPolling = false
            // Do not come back expanded when the notch is switched on again.
            present(.compact)
        }
        refreshPresentation(animated: false)
    }

    /// Re-lays-out when a screen / retention / alignment preference changes.
    ///
    /// Gated on a signature so the shared `UserDefaults.didChangeNotification`
    /// only moves the panel for a change that actually affects its placement —
    /// not for the sound slider or any other unrelated defaults write, each of
    /// which would otherwise cut an in-flight hover animation short.
    private func applyLayoutPreferenceChangesIfNeeded() {
        let signature = ATMAgentNotchPreferences.LayoutSignature.current
        guard signature != layoutSignature else { return }
        layoutSignature = signature
        // Retention widening can bring hidden sessions back and narrowing can
        // drop the last one; recompute the metrics for the (possibly new)
        // screen and resize to match.
        if let screen = Self.preferredScreen() {
            let metrics = Self.metrics(for: screen)
            if surfaceModel.metrics != metrics {
                surfaceModel.metrics = metrics
            }
        }
        refreshPresentation(animated: false)
    }

    private func handleSessionUpdate() {
        let sessions = liveSessions
        // Hooked sessions get their turn state from `handleAgentEvent`. Letting
        // snapshot diffing also decide made Codex read as finished mid-turn: it
        // writes commentary while it works, and every commentary changed the
        // latest reply, which looks exactly like a fresh result from here.
        let completedSession = completionTransitionTracker.nextCompletedSession(
            in: sessions,
            hookBacked: { [store] session in store.isHookAuthoritative(session) }
        )
        if hasSeededCompletionState {
            surfaceModel.completedSessionIDs.subtract(completionTransitionTracker.startedSessionIDs)
            surfaceModel.completedSessionIDs.formUnion(
                completionTransitionTracker.newlyCompletedSessionIDs
            )
        } else if completionTransitionTracker.isPrimed {
            hasSeededCompletionState = true
            // Union, not assign: a hook completion can arrive before the first
            // snapshot lands, and the seed must not erase it.
            surfaceModel.completedSessionIDs
                .formUnion(completionTransitionTracker.completedSessionIDs)
        }
        // Hook-backed sessions announce themselves through `handleAgentEvent`;
        // diffing snapshots covers the agents that have no hooks.
        let soundEvent = soundTransitionTracker.nextEvent(for: sessions) { [store] session in
            store.agentEvents.isHookBacked(session)
        }
        let attentionIDs = Set(
            sessions.filter { $0.presenceState == .attention }.map(\.id)
        )
        var didResize = false
        let newAttentionIDs = previousAttentionIDs.map { attentionIDs.subtracting($0) } ?? []
        if let attentionSession = sessions.first(where: { newAttentionIDs.contains($0.id) }) {
            if !surfaceModel.presentation.isPersistent {
                surfaceModel.notification = ATMAgentNotchNotification(
                    kind: .attention,
                    sessionID: attentionSession.id,
                    summary: nil
                )
                didResize = present(.notification)
                scheduleNotificationDismiss()
            }
        } else if let completedSession,
                  !completionSessionsAlreadyNotifiedByHook.contains(completedSession.id),
                  !surfaceModel.presentation.isPersistent,
                  surfaceModel.notification?.kind != .attention {
            surfaceModel.notification = ATMAgentNotchNotification(
                kind: .completed,
                sessionID: completedSession.id,
                summary: nil
            )
            didResize = present(.notification)
            scheduleNotificationDismiss()
        }
        previousAttentionIDs = attentionIDs
        // A hook completion and its refreshed snapshot describe the same moment.
        // Suppression lasts for that one refresh only; if the parser reports no
        // changed result, a future completion in the same session must still work.
        completionSessionsAlreadyNotifiedByHook.removeAll()
        if sessions.isEmpty {
            didResize = present(.compact) || didResize
        }
        if let soundEvent {
            ATMAgentSoundPlayer.shared.play(soundEvent)
        }
        // `present` already animated to the new frame. Re-running the refresh
        // here would restart that animation from wherever it currently is,
        // turning one transition into a visible two-stage move.
        guard !didResize else { return }
        refreshPresentation(animated: panel.isVisible)
    }

    /// Handles a pushed hook event immediately. A completion event carries the
    /// final text itself, so unlike the old attention-only path there is no need
    /// to wait for the debounced transcript refresh before showing a useful card.
    private func handleAgentEvent(_ event: ATMAgentEvent) {
        let eventKeys = Set(event.joinCandidates)
        let session = liveSessions.first(where: { candidate in
            !eventKeys.isDisjoint(with: ATMAgentAttentionJoin.joinKeys(for: candidate))
        })

        if let soundEvent = event.event.soundEvent {
            ATMAgentSoundPlayer.shared.play(soundEvent)
        }

        if event.event == .started, let session {
            surfaceModel.completedSessionIDs.remove(session.id)
            return
        }
        guard event.event == .completed, let session else { return }
        surfaceModel.completedSessionIDs.insert(session.id)
        guard !surfaceModel.presentation.isPersistent,
              surfaceModel.notification?.kind != .attention else { return }

        completionSessionsAlreadyNotifiedByHook.insert(session.id)
        surfaceModel.notification = ATMAgentNotchNotification(
            kind: .completed,
            sessionID: session.id,
            summary: event.text
        )
        present(.notification)
        scheduleNotificationDismiss()
    }

    private func expandSessionList() {
        switch surfaceModel.presentation {
        case .compact, .hoverExpanded, .notification:
            present(.sessionList)
        case .sessionList:
            break
        }
    }

    /// Schedules the notification card to collapse.
    ///
    /// With `manual` dwell there is no timer at all: the card stays until an
    /// outside click or the next event replaces it, matching Ping Island's
    /// "keep it until I look" mode. Any in-flight timer is still cancelled so
    /// switching to `manual` mid-card does not leave a stale dismissal armed.
    private func scheduleNotificationDismiss() {
        notificationDismissWorkItem?.cancel()
        notificationDismissWorkItem = nil
        shouldDismissNotificationOnHoverExit = false
        guard let delay = ATMAgentNotchPreferences.notificationDwell.seconds else { return }
        let workItem = DispatchWorkItem { [weak self] in
            guard let self, self.surfaceModel.presentation == .notification else { return }
            if self.isHovering {
                self.shouldDismissNotificationOnHoverExit = true
            } else {
                self.present(.compact)
            }
        }
        notificationDismissWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: workItem)
    }

    /// Returns `true` when the presentation actually changed and the panel was
    /// resized for it.
    @discardableResult
    private func present(_ presentation: ATMAgentNotchPresentation) -> Bool {
        if presentation != .notification {
            notificationDismissWorkItem?.cancel()
            notificationDismissWorkItem = nil
            shouldDismissNotificationOnHoverExit = false
            surfaceModel.notification = nil
        }
        guard surfaceModel.presentation != presentation else { return false }
        surfaceModel.presentation = presentation
        // Resize here, in the same turn as the mutation and *after* the new
        // value is stored. A Combine sink on `$presentation` cannot do this:
        // `@Published` publishes in `willSet`, so the sink would read the
        // previous presentation and size the panel one transition behind —
        // hover-in left the expanded content clipped inside the compact window
        // until the next three-second poll, and hover-out grew the window to
        // list size while the content was already compact.
        refreshPresentation(animated: true)
        return true
    }

    private func installMouseMonitors() {
        // Both monitors are needed: a global monitor does not see events that get
        // delivered to our own process, and a local one sees only those.
        let mask: NSEvent.EventTypeMask = [
            .leftMouseDown, .rightMouseDown,
            .mouseMoved, .leftMouseDragged, .rightMouseDragged,
        ]
        globalMouseMonitor = NSEvent.addGlobalMonitorForEvents(matching: mask) { [weak self] event in
            // Monitors are delivered on the main thread, so stay synchronous:
            // mouse-moved arrives up to a hundred times a second and spawning a
            // Task per event would be pure overhead.
            MainActor.assumeIsolated {
                self?.handleMouseEvent(event.type, at: NSEvent.mouseLocation)
            }
        }
        localMouseMonitor = NSEvent.addLocalMonitorForEvents(matching: mask) { [weak self] event in
            MainActor.assumeIsolated {
                self?.handleMouseEvent(event.type, at: NSEvent.mouseLocation)
            }
            return event
        }
    }

    private func handleMouseEvent(_ type: NSEvent.EventType, at location: CGPoint) {
        switch type {
        case .leftMouseDown, .rightMouseDown:
            handleMouseDown(at: location)
        default:
            updateHover(at: location)
        }
    }

    /// The screen rect the compact strip occupies, independent of the panel's
    /// current size.
    ///
    /// Hover while compact is measured against this rather than `panel.frame` so
    /// the trigger area never depends on how many sessions happen to be live.
    private var compactStripFrame: CGRect {
        guard let screen = Self.preferredScreen() else { return panel.frame }
        return surfaceModel.metrics.windowFrame(
            screenFrame: screen.frame,
            presentation: .compact,
            sessionCount: liveSessions.count,
            alignment: ATMAgentNotchPreferences.stripAlignment
        )
    }

    private var hoverRegion: CGRect {
        ATMAgentNotchHover.region(
            presentation: surfaceModel.presentation,
            compactFrame: compactStripFrame,
            panelFrame: panel.frame
        )
    }

    private var cursorIsInHoverRegion: Bool {
        hoverRegion.contains(NSEvent.mouseLocation)
    }

    private func updateHover(at location: CGPoint) {
        guard panel.isVisible else { return }
        let inside = hoverRegion.contains(location)
        guard inside != isHovering else { return }
        isHovering = inside
        if inside {
            scheduleHoverOpen()
        } else {
            scheduleHoverClose()
        }
    }

    /// Re-checks hover after the panel resizes.
    ///
    /// A resize can move the edge out from under a stationary cursor — the session
    /// list shrinking by a row, for instance — and no mouse event follows, so
    /// nothing else would notice the cursor is now outside.
    private func reevaluateHoverAfterFrameChange() {
        let location = NSEvent.mouseLocation
        let inside = hoverRegion.contains(location)
        guard inside != isHovering else { return }
        isHovering = inside
        if inside {
            scheduleHoverOpen()
        } else {
            scheduleHoverClose()
        }
    }

    private func scheduleHoverOpen() {
        hoverCloseWorkItem?.cancel()
        hoverCloseWorkItem = nil
        guard surfaceModel.presentation == .compact, hoverOpenWorkItem == nil else { return }
        let workItem = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.hoverOpenWorkItem = nil
            // Read the cursor again instead of trusting the event that scheduled
            // this: a fast pass across the strip must not leave a panel open.
            let inside = self.cursorIsInHoverRegion
            self.isHovering = inside
            guard ATMAgentNotchHover.shouldOpen(
                presentation: self.surfaceModel.presentation,
                cursorIsInRegion: inside
            ) else { return }
            self.present(.hoverExpanded)
        }
        hoverOpenWorkItem = workItem
        DispatchQueue.main.asyncAfter(
            deadline: .now() + ATMAgentNotchHover.openDelay,
            execute: workItem
        )
    }

    private func scheduleHoverClose() {
        hoverOpenWorkItem?.cancel()
        hoverOpenWorkItem = nil
        hoverCloseWorkItem?.cancel()
        let workItem = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.hoverCloseWorkItem = nil
            let inside = self.cursorIsInHoverRegion
            self.isHovering = inside
            guard ATMAgentNotchHover.shouldClose(
                presentation: self.surfaceModel.presentation,
                cursorIsInRegion: inside,
                dismissesNotificationOnExit: self.shouldDismissNotificationOnHoverExit
            ) else { return }
            self.present(.compact)
        }
        hoverCloseWorkItem = workItem
        DispatchQueue.main.asyncAfter(
            deadline: .now() + ATMAgentNotchHover.closeDelay,
            execute: workItem
        )
    }

    private func cancelHoverWork() {
        hoverOpenWorkItem?.cancel()
        hoverOpenWorkItem = nil
        hoverCloseWorkItem?.cancel()
        hoverCloseWorkItem = nil
    }

    private func removeMouseMonitors() {
        if let globalMouseMonitor {
            NSEvent.removeMonitor(globalMouseMonitor)
            self.globalMouseMonitor = nil
        }
        if let localMouseMonitor {
            NSEvent.removeMonitor(localMouseMonitor)
            self.localMouseMonitor = nil
        }
    }

    private func handleMouseDown(at screenLocation: CGPoint) {
        guard surfaceModel.presentation.isExpanded,
              !panel.frame.contains(screenLocation) else { return }
        present(.compact)
    }

    private func refreshPresentation(animated: Bool) {
        guard ATMAgentNotchPreferences.isEnabled,
              !liveSessions.isEmpty,
              let screen = Self.preferredScreen() else {
            // Clear hover state along with the panel, or a pending dwell could
            // reopen it after it was hidden.
            cancelHoverWork()
            isHovering = false
            panel.orderOut(nil)
            return
        }

        let metrics = Self.metrics(for: screen)
        if surfaceModel.metrics != metrics {
            surfaceModel.metrics = metrics
        }
        let frame = metrics.windowFrame(
            screenFrame: screen.frame,
            presentation: surfaceModel.presentation,
            sessionCount: liveSessions.count,
            alignment: ATMAgentNotchPreferences.stripAlignment
        )
        if panel.frame != frame {
            setPanelFrame(frame, animated: animated)
            reevaluateHoverAfterFrameChange()
        }
        if !panel.isVisible {
            panel.orderFrontRegardless()
        }
    }

    private func setPanelFrame(_ frame: CGRect, animated: Bool) {
        guard animated, panel.isVisible else {
            panel.setFrame(frame, display: true)
            return
        }

        // AppKit owns the one transition from the current panel frame straight
        // to the final frame. SwiftUI content has no independent transition,
        // so hover never becomes a two-stage animation.
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.22
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            context.allowsImplicitAnimation = true
            panel.animator().setFrame(frame, display: true)
        }
    }

    private var liveSessions: [ATMLiveSession] {
        Self.sortedSessions(
            store.snapshot.liveStatus.sessions.filter(\.isVisibleInAgentNotch)
        )
    }

    private static func sortedSessions(_ sessions: [ATMLiveSession]) -> [ATMLiveSession] {
        sessions.sorted {
            if $0.presenceState != $1.presenceState {
                return presenceOrder($0.presenceState) < presenceOrder($1.presenceState)
            }
            if $0.ageSeconds != $1.ageSeconds { return $0.ageSeconds < $1.ageSeconds }
            return $0.id < $1.id
        }
    }

    private static func presenceOrder(_ state: ATMAgentPresenceState) -> Int {
        switch state {
        case .attention: return 0
        case .active: return 1
        case .recent: return 2
        }
    }

    private static func preferredScreen() -> NSScreen? {
        preferredScreen(for: ATMAgentNotchPreferences.screenSelection)
    }

    /// Resolves a screen selection to an actual `NSScreen`.
    ///
    /// A pinned display that is currently unplugged falls back through the same
    /// chain `automatic` uses, so unplugging the chosen monitor moves the notch
    /// to a sensible screen instead of hiding it entirely.
    static func preferredScreen(for selection: ATMAgentNotchScreenSelection) -> NSScreen? {
        switch selection {
        case .display(let id):
            if let match = NSScreen.screens.first(where: { $0.atmDisplayID == id }) {
                return match
            }
            return automaticScreen()
        case .main:
            return NSScreen.main ?? automaticScreen()
        case .automatic:
            return automaticScreen()
        }
    }

    private static func automaticScreen() -> NSScreen? {
        NSScreen.screens.first { metrics(for: $0).hasPhysicalNotch }
            ?? NSScreen.main
            ?? NSScreen.screens.first
    }

    private static func metrics(for screen: NSScreen) -> ATMAgentNotchMetrics {
        ATMAgentNotchMetrics.detect(
            screenFrame: screen.frame,
            safeAreaTop: screen.safeAreaInsets.top,
            auxiliaryTopLeftWidth: screen.auxiliaryTopLeftArea?.width,
            auxiliaryTopRightWidth: screen.auxiliaryTopRightArea?.width
        )
    }
}

private struct ATMAgentNotchView: View {
    @ObservedObject var store: ATMDataStore
    @ObservedObject var surfaceModel: ATMAgentNotchSurfaceModel
    @AppStorage(ATMAgentSoundPreferences.enabledKey) private var soundsEnabled = true
    @State private var hoveredSessionID: String?
    let onExpandSessionList: () -> Void
    let onOpenSession: (ATMLiveSession) -> Void
    let onOpenAgents: (ATMLiveSession?) -> Void
    let onOpenSettings: () -> Void

    private var sessions: [ATMLiveSession] {
        store.snapshot.liveStatus.sessions
            .filter(\.isVisibleInAgentNotch)
            .sorted {
                if $0.presenceState != $1.presenceState {
                    return presenceOrder($0.presenceState) < presenceOrder($1.presenceState)
                }
                if $0.ageSeconds != $1.ageSeconds { return $0.ageSeconds < $1.ageSeconds }
                return $0.id < $1.id
            }
    }

    private var notificationSession: ATMLiveSession? {
        guard let sessionID = surfaceModel.notification?.sessionID else { return nil }
        return sessions.first { $0.id == sessionID }
    }
    private var representativeSession: ATMLiveSession? { sessions.first }
    private var displaySession: ATMLiveSession? { notificationSession ?? representativeSession }

    var body: some View {
        ZStack(alignment: .top) {
            panelBackground

            VStack(spacing: 0) {
                if surfaceModel.isExpanded {
                    expandedToolbar
                        .frame(height: ATMAgentNotchLayout.expandedToolbarHeight)
                    expandedContent
                } else {
                    compactHeader
                        .frame(height: surfaceModel.metrics.compactSize.height)
                }
            }
        }
        .foregroundStyle(Color.white)
        .contentShape(Rectangle())
        // No drop shadow on purpose. The panel window is sized exactly to this
        // content, so everything a shadow would paint outside the rounded shape is
        // clipped away by the window — except the blur that falls *inside* the
        // rounded corner cutouts, which showed up as a grey translucent wedge at
        // each bottom corner and read as an unfinished graphic. Giving the window
        // a transparent margin to shadow into is not an option either: a borderless
        // panel swallows clicks in its transparent area, and this one sits on the
        // menu bar. The hairline stroke carries the edge instead.
        // No .onHover here on purpose: the controller derives hover from the real
        // cursor position, because a tracking area on a view that resizes reports
        // a false exit whenever the panel expands. See ATMAgentNotchHover.
        .accessibilityElement(children: .contain)
        .accessibilityLabel("ATM Agent")
        .atmHidesScrollBars()
    }

    @ViewBuilder
    private var panelBackground: some View {
        if surfaceModel.isExpanded {
            RoundedRectangle(cornerRadius: 22, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [
                            Color.black.opacity(0.99),
                            Color(red: 0.025, green: 0.026, blue: 0.028).opacity(0.99),
                        ],
                        startPoint: .top,
                        endPoint: .bottom
                    )
                )
                .overlay {
                    RoundedRectangle(cornerRadius: 22, style: .continuous)
                        .stroke(Color.white.opacity(0.045), lineWidth: 0.75)
                }
            topCornerSquareOff(fill: Color.black.opacity(0.99))
        } else {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(Color.black.opacity(0.97))
            topCornerSquareOff(fill: Color.black.opacity(0.97))
        }
    }

    /// A plain rectangle over the top half of the panel that squares off the two
    /// upper corners.
    ///
    /// The panel hugs the screen's top edge on every screen, so its top corners
    /// must be flush right angles — a rounded corner there would show the desktop
    /// through the gap. Drawn unconditionally now: it used to be gated on a
    /// physical notch, which left notchless screens with all four corners rounded
    /// and the top two floating away from the edge.
    private func topCornerSquareOff(fill: Color) -> some View {
        Rectangle()
            .fill(fill)
            .frame(height: surfaceModel.metrics.compactSize.height / 2)
    }

    @ViewBuilder
    private var compactHeader: some View {
        if surfaceModel.metrics.hasPhysicalNotch {
            let sideWidth = max(
                56,
                (surfaceModel.metrics.compactSize.width - surfaceModel.metrics.notchSize.width) / 2
            )
            Button(action: onExpandSessionList) {
                HStack(spacing: 0) {
                    compactAgentMarks(size: 17)
                        .frame(width: sideWidth)

                    Color.clear
                        .frame(width: surfaceModel.metrics.notchSize.width)

                    HStack(spacing: 6) {
                        compactStateDot
                        Text("\(sessions.count)")
                            .font(.system(size: 12, weight: .bold, design: .rounded))
                    }
                    .frame(width: sideWidth)
                }
            }
            .buttonStyle(.plain)
            .accessibilityLabel(compactAccessibilityLabel)
        } else {
            Button(action: onExpandSessionList) {
                HStack(spacing: 8) {
                    compactAgentMarks(size: 18)
                    if let session = representativeSession {
                        Text(session.presenceTitle)
                            .font(.system(size: 11.5, weight: .semibold, design: .rounded))
                            .lineLimit(1)
                    }
                    Spacer(minLength: 6)
                    compactStateDot
                    Text("\(sessions.count)")
                        .font(.system(size: 12, weight: .bold, design: .rounded))
                }
                .padding(.horizontal, 14)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(compactAccessibilityLabel)
        }
    }

    private var expandedToolbar: some View {
        HStack(spacing: 8) {
            Text(displaySession.map { ATMAgentDisplay.name($0.tool) } ?? "ATM")
                .font(.system(size: 11.5, weight: .bold, design: .rounded))
                .foregroundStyle(Color.white.opacity(0.68))

            if let quota = quotaSummary {
                HStack(spacing: 5) {
                    Text(quota.window)
                        .foregroundStyle(Color.white.opacity(0.46))
                    Text("\(quota.remaining)% 剩余")
                        .foregroundStyle(quota.tint)
                }
                .font(.system(size: 9.5, weight: .bold, design: .rounded))
                .padding(.horizontal, 8)
                .frame(height: 22)
                .background(quota.tint.opacity(0.10))
                .clipShape(Capsule())
            }

            Spacer(minLength: 20)

            toolbarButton(
                systemName: soundsEnabled ? "speaker.wave.2.fill" : "speaker.slash.fill",
                accessibilityLabel: soundsEnabled ? "关闭 Agent 提示音" : "开启 Agent 提示音"
            ) {
                soundsEnabled.toggle()
            }

            toolbarButton(systemName: "gearshape.fill", accessibilityLabel: "打开设置") {
                onOpenSettings()
            }
        }
        .padding(.horizontal, 16)
    }

    private func toolbarButton(
        systemName: String,
        accessibilityLabel: String,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Image(systemName: systemName)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Color.white.opacity(0.88))
                .frame(width: 28, height: 28)
                .background(Color.white.opacity(0.075))
                .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityLabel)
        .help(accessibilityLabel)
    }

    @ViewBuilder
    private var expandedContent: some View {
        switch surfaceModel.presentation {
        case .hoverExpanded:
            hoverPeekContent
        case .sessionList:
            sessionListContent
        case .notification:
            notificationContent
        case .compact:
            EmptyView()
        }
    }

    /// What a hover gets: the one session that most wants you, and a count of
    /// the rest.
    ///
    /// Hover and click used to render the identical list, which made a 350pt
    /// panel the price of moving the cursor past the notch and left the click
    /// with nothing to add but pinning. A peek is one row; the list is what you
    /// ask for.
    private var hoverPeekContent: some View {
        VStack(spacing: 0) {
            if let session = representativeSession {
                Button {
                    open(session)
                } label: {
                    sessionCard(session)
                }
                .buttonStyle(.plain)
                .padding(.horizontal, ATMAgentNotchLayout.listHorizontalInset)
            }

            if sessions.count > 1 {
                Button("还有 \(sessions.count - 1) 个会话 · 点击展开") {
                    onExpandSessionList()
                }
                .buttonStyle(.plain)
                .font(.system(size: 9.5, weight: .medium, design: .rounded))
                .foregroundStyle(Color.white.opacity(0.46))
                .frame(height: ATMAgentNotchLayout.listBottomInset)
            } else {
                Color.clear.frame(height: ATMAgentNotchLayout.listBottomInset)
            }
        }
    }

    /// The one card a notification shows.
    ///
    /// There used to be a second, near-identical `completionCard` here whose
    /// whole job was to draw a green tick. `sessionMark` now puts that tick on
    /// the agent tile, so the fork bought nothing but seventy lines that had to
    /// be restyled twice — and were not. What survives from it is `isSpotlit`,
    /// which keeps the accent visible without waiting for a hover.
    private var notificationContent: some View {
        VStack(spacing: 0) {
            if let session = notificationSession ?? representativeSession {
                Button {
                    open(session)
                } label: {
                    sessionCard(session, isSpotlit: true)
                }
                .buttonStyle(.plain)
            }
            Color.clear.frame(height: ATMAgentNotchLayout.listBottomInset)
        }
        .padding(.horizontal, ATMAgentNotchLayout.listHorizontalInset)
    }

    private var completionGreen: Color {
        Color(red: 0.38, green: 0.96, blue: 0.61)
    }

    /// The text a hook event carried for this session, if it is the one being
    /// announced.
    ///
    /// A `completed` hook event arrives with the final reply attached, ahead of
    /// the poll that will eventually put the same text in `latestResultText`.
    /// Preferring it is what keeps the notification from opening on a blank or
    /// previous-turn line for the couple of seconds in between.
    private func notificationSummary(for session: ATMLiveSession) -> String? {
        guard let notification = surfaceModel.notification,
              notification.sessionID == session.id,
              let summary = notification.summary?.trimmingCharacters(in: .whitespacesAndNewlines),
              !summary.isEmpty else { return nil }
        return ATMMarkdown.plainSummary(summary, limit: 220)
    }

    private var sessionListContent: some View {
        VStack(spacing: 0) {
            VStack(spacing: ATMAgentNotchLayout.sessionRowSpacing) {
                ForEach(Array(sessions.prefix(ATMAgentNotchLayout.maximumVisibleRows))) { session in
                    Button {
                        open(session)
                    } label: {
                        sessionCard(session)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, ATMAgentNotchLayout.listHorizontalInset)

            if sessions.count > ATMAgentNotchLayout.maximumVisibleRows {
                Button("还有 \(sessions.count - ATMAgentNotchLayout.maximumVisibleRows) 个会话 · 在 ATM 中查看全部") {
                    onOpenAgents(representativeSession)
                }
                .buttonStyle(.plain)
                .font(.system(size: 9.5, weight: .medium, design: .rounded))
                .foregroundStyle(Color.white.opacity(0.46))
                .frame(height: ATMAgentNotchLayout.listBottomInset)
            } else {
                Color.clear.frame(height: ATMAgentNotchLayout.listBottomInset)
            }
        }
    }

    private func open(_ session: ATMLiveSession) {
        let route = ATMAgentSessionLaunchRoute.resolve(for: session)
        if route.isAvailable {
            onOpenSession(session)
        } else {
            onOpenAgents(session)
        }
    }

    /// - Parameter isSpotlit: the card is the subject of a notification, so its
    ///   accent shows without waiting for a hover.
    private func sessionCard(
        _ session: ATMLiveSession,
        isSpotlit: Bool = false
    ) -> some View {
        let state = sessionState(session)
        let isHovered = hoveredSessionID == session.id
        let launchRoute = ATMAgentSessionLaunchRoute.resolve(for: session)
        let accent = stateTint(state)
        let result = notificationSummary(for: session) ?? session.latestResultText
        return HStack(spacing: 12) {
            sessionMark(session, state: state)

            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 7) {
                    HStack(spacing: 7) {
                        Text(projectName(session))
                            .foregroundStyle(Color.white.opacity(0.88))
                        Text("·")
                            .foregroundStyle(Color.white.opacity(0.28))
                        Text(session.presenceTitle)
                            .foregroundStyle(Color.white.opacity(0.96))
                    }
                    .font(.system(size: 13.5, weight: .bold, design: .rounded))
                    .lineLimit(1)

                    Spacer(minLength: 8)

                    // State moved onto the tile, and the agent name was the same
                    // information as the tile's logo. What is left is the two
                    // things the tile cannot say: how old, and where it runs.
                    HStack(spacing: 5) {
                        cardBadge(compactAge(session.ageSeconds), tint: Color.white)
                        if let client = clientLabel(session) {
                            cardBadge(client, tint: Color.white)
                        }
                    }
                    .fixedSize(horizontal: true, vertical: false)
                }

                if let input = session.latestUserInputBelowTitle {
                    conversationLine(label: "You:", text: input, tint: Color.white.opacity(0.56))
                }
                if let result {
                    conversationLine(
                        label: "\(speakerName(session.tool)):",
                        text: result,
                        tint: ATMAgentDisplay.brandBackground(session.tool).opacity(0.95)
                    )
                } else if session.latestUserInputBelowTitle == nil {
                    // Nothing left to quote. The title above already carries
                    // whatever text this session has, so name the host rather
                    // than printing that same sentence a second time.
                    conversationLine(
                        label: nil,
                        text: "\(clientName(session)) · 暂无回复",
                        tint: Color.white.opacity(0.46)
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .clipped()
        }
        .padding(.horizontal, 14)
        .frame(height: ATMAgentNotchLayout.sessionRowHeight)
        .background(
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: rowFillColors(accent: accent, isHovered: isHovered, isSpotlit: isSpotlit),
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
        )
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .stroke(
                    accent.opacity(rowStrokeOpacity(state: state, isHovered: isHovered, isSpotlit: isSpotlit)),
                    lineWidth: 0.75
                )
        }
        .shadow(color: Color.black.opacity(isHovered || isSpotlit ? 0.24 : 0), radius: 8, y: 3)
        .scaleEffect(isHovered ? 1.006 : 1)
        // The 回到会话/打开来源 chip was on every available row, so it carried no
        // information. Only the negative case is worth chrome — and where the
        // click lands is a hover-time question, not a glance-time one.
        .opacity(launchRoute.isAvailable ? 1 : 0.72)
        .help(launchRoute.destinationLabel)
        .animation(.easeOut(duration: 0.16), value: isHovered)
        .contentShape(Rectangle())
        .onHover { updateHoveredSession(session.id, isHovered: $0) }
    }

    /// Spotlit rows are tinted by their state; hovered rows stay neutral white,
    /// so that a `recent` row — whose accent is barely-there grey — still lifts
    /// visibly under the cursor.
    private func rowFillColors(
        accent: Color,
        isHovered: Bool,
        isSpotlit: Bool
    ) -> [Color] {
        if isSpotlit {
            return isHovered
                ? [accent.opacity(0.10), Color.white.opacity(0.055)]
                : [accent.opacity(0.045), Color.white.opacity(0.018)]
        }
        return isHovered
            ? [Color.white.opacity(0.075), Color.white.opacity(0.042)]
            : [Color.white.opacity(0.028), Color.white.opacity(0.014)]
    }

    private func rowStrokeOpacity(
        state: ATMAgentNotchSessionState,
        isHovered: Bool,
        isSpotlit: Bool
    ) -> Double {
        guard isHovered || isSpotlit else { return 0 }
        return state == .recent && !isSpotlit ? 0.14 : 0.28
    }

    private func sessionMark(
        _ session: ATMLiveSession,
        state: ATMAgentNotchSessionState
    ) -> some View {
        ZStack {
            RoundedRectangle(cornerRadius: 9, style: .continuous)
                .fill(Color.white.opacity(0.045))
            ATMAgentMark(agent: session.tool, size: 18)
        }
        .frame(width: 28, height: 28)
        .opacity(state == .recent ? 0.62 : 1)
        .overlay {
            if state == .working {
                ATMAgentNotchWorkingRing(tint: stateTint(state))
            }
        }
        .overlay(alignment: .bottomTrailing) {
            stateCorner(state)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(ATMAgentDisplay.name(session.tool))，\(state.title)")
    }

    @ViewBuilder
    private func stateCorner(_ state: ATMAgentNotchSessionState) -> some View {
        if let glyph = state.cornerGlyph {
            ZStack {
                // Black ring first: the tint has to separate from whatever
                // corner of the logo it happens to land on.
                Circle().fill(Color.black)
                Circle().fill(stateTint(state)).padding(1.5)
                Image(systemName: glyph)
                    .font(.system(size: 6, weight: .black))
                    .foregroundStyle(Color.black)
            }
            .frame(width: 13, height: 13)
            .offset(x: 4, y: 4)
        }
    }

    private func conversationLine(label: String?, text: String, tint: Color) -> some View {
        HStack(spacing: 6) {
            if let label {
                Text(label)
                    .foregroundStyle(tint)
            }
            Text(text)
                .foregroundStyle(Color.white.opacity(0.52))
        }
        .font(.system(size: 10.5, weight: .semibold, design: .rounded))
        .lineLimit(1)
    }

    private func cardBadge(_ title: String, tint: Color) -> some View {
        Text(title)
            .font(.system(size: 8.5, weight: .bold, design: .rounded))
            .foregroundStyle(tint.opacity(0.90))
            .padding(.horizontal, 7)
            .frame(height: 18)
            .background(tint.opacity(0.13))
            .clipShape(Capsule())
            .fixedSize(horizontal: true, vertical: false)
    }

    private func updateHoveredSession(_ sessionID: String, isHovered: Bool) {
        if isHovered {
            hoveredSessionID = sessionID
        } else if hoveredSessionID == sessionID {
            hoveredSessionID = nil
        }
    }

    private func sessionState(_ session: ATMLiveSession) -> ATMAgentNotchSessionState {
        ATMAgentNotchSessionState.resolve(
            session: session,
            completedSessionIDs: surfaceModel.completedSessionIDs
        )
    }

    private func stateTint(_ state: ATMAgentNotchSessionState) -> Color {
        switch state {
        case .attention: return Color.orange
        case .working: return Color(red: 0.36, green: 0.70, blue: 1.0)
        case .completed: return completionGreen
        case .recent: return Color.white.opacity(0.55)
        }
    }

    /// Up to three overlapping agent logos, most urgent first.
    ///
    /// The strip used to draw one logo — the representative session's — beside a
    /// count of *all* sessions, so with two agents running the logo was speaking
    /// for someone else. Stacking answers the same "who" the count is counting.
    /// Deduplicated by agent, not by session: three Claude sessions are one
    /// logo, and the number to their right already says three.
    private func compactAgentMarks(size: CGFloat) -> some View {
        let agents = compactAgents
        return HStack(spacing: -size * 0.36) {
            ForEach(Array(agents.enumerated()), id: \.offset) { index, agent in
                ATMAgentMark(agent: agent, size: size)
                    // Black on a black panel: the ring does not draw a border so
                    // much as bite a gap out of the logo underneath it.
                    .overlay {
                        RoundedRectangle(cornerRadius: size * 0.28, style: .continuous)
                            .stroke(Color.black, lineWidth: 2)
                    }
                    .zIndex(Double(agents.count - index))
            }
        }
        .accessibilityHidden(true)
    }

    private var compactAgents: [String] {
        var seen = Set<String>()
        var agents: [String] = []
        for session in sessions where seen.insert(ATMAgentDisplay.key(session.tool)).inserted {
            agents.append(session.tool)
            if agents.count == 3 { break }
        }
        return agents
    }

    /// The most urgent state across every visible session, or `nil` when they
    /// are all merely recent and there is nothing to flag.
    ///
    /// An aggregate rather than the representative session's own state: one dot
    /// standing next to a count of everything has to describe everything.
    private var aggregateState: ATMAgentNotchSessionState? {
        let states = Set(sessions.map { sessionState($0) })
        for state in [ATMAgentNotchSessionState.attention, .working, .completed]
        where states.contains(state) {
            return state
        }
        return nil
    }

    /// Same vocabulary as the tile corner in the expanded list — orange `!`,
    /// green `✓` — so the strip and the rows are not two separate languages.
    /// `working` stays a bare dot: the breathing ring is for the panel only.
    @ViewBuilder
    private var compactStateDot: some View {
        if let state = aggregateState {
            let tint = stateTint(state)
            ZStack {
                Circle().fill(tint)
                if let glyph = state.cornerGlyph {
                    Image(systemName: glyph)
                        .font(.system(size: 6, weight: .black))
                        .foregroundStyle(Color.black)
                }
            }
            .frame(width: 11, height: 11)
            .shadow(color: tint.opacity(0.65), radius: 4)
        }
    }

    private var compactAccessibilityLabel: String {
        let states = aggregateState.map { "，\($0.title)" } ?? ""
        return "\(sessions.count) 个 Agent 会话\(states)，打开会话列表"
    }

    /// The host the session runs in, or `nil` when ATM never captured one.
    ///
    /// Separate from `clientName` because the session row must be able to drop
    /// the chip entirely: falling back to the agent name there would print the
    /// same thing the tile beside it already draws.
    private func clientLabel(_ session: ATMLiveSession) -> String? {
        let value = session.client?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !value.isEmpty else { return nil }
        switch value.lowercased() {
        case "codex", "codex desktop": return "Codex Desktop"
        case "vscode", "vs code", "visual studio code": return "VS Code 终端"
        case "terminal", "apple_terminal": return "终端"
        case "iterm", "iterm2": return "iTerm"
        default: return value
        }
    }

    private func clientName(_ session: ATMLiveSession) -> String {
        clientLabel(session) ?? ATMAgentDisplay.name(session.tool)
    }

    private func projectName(_ session: ATMLiveSession) -> String {
        let value = session.project.trimmingCharacters(in: .whitespacesAndNewlines)
        return value.isEmpty ? "未知项目" : value
    }

    private func presenceOrder(_ state: ATMAgentPresenceState) -> Int {
        switch state {
        case .attention: return 0
        case .active: return 1
        case .recent: return 2
        }
    }

    private func speakerName(_ tool: String) -> String {
        switch ATMAgentDisplay.key(tool) {
        case "claude": return "Claude"
        default: return ATMAgentDisplay.name(tool)
        }
    }

    private func compactAge(_ seconds: Int) -> String {
        switch seconds {
        case ..<60: return "<1m"
        case ..<3_600: return "\(seconds / 60)m"
        case ..<86_400: return "\(seconds / 3_600)h"
        default: return "\(seconds / 86_400)d"
        }
    }

    private var quotaSummary: (window: String, remaining: Int, tint: Color)? {
        guard let session = displaySession,
              let quota = store.quota.agents[ATMAgentDisplay.key(session.tool)],
              let window = quota.primary ?? quota.secondary else { return nil }
        let remaining = max(0, min(100, Int((100 - window.displayPercent).rounded())))
        let tint: Color = switch remaining {
        case 0..<10: Color(red: 1.0, green: 0.35, blue: 0.35)
        case 10..<25: Color(red: 1.0, green: 0.67, blue: 0.28)
        default: Color(red: 0.38, green: 0.96, blue: 0.61)
        }
        let label: String
        if window.windowMinutes >= 24 * 60,
           window.windowMinutes.isMultiple(of: 24 * 60) {
            label = "\(window.windowMinutes / (24 * 60))d"
        } else {
            label = window.windowLabel
        }
        return (label, remaining, tint)
    }
}
