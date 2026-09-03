import AppKit
import SwiftUI

/// Scroll offsets are deliberately not published: scrolling should not cause
/// the whole reading pane (and its Markdown) to be rebuilt.
@MainActor
final class ATMWorkspaceScrollPositions {
    private var offsets: [String: CGPoint] = [:]
    private var order: [String] = []

    func offset(for key: String) -> CGPoint? { offsets[key] }

    func set(_ offset: CGPoint, for key: String) {
        offsets[key] = offset
        order.removeAll { $0 == key }
        order.append(key)
        if order.count > 128 { offsets.removeValue(forKey: order.removeFirst()) }
    }
}

/// A restore is consumed exactly once. Short asynchronous placeholders defer
/// it; user input cancels it permanently for this mount.
struct ATMWorkspaceInitialScrollRestore {
    private(set) var desiredOffset: CGPoint?
    var isPending: Bool { desiredOffset != nil }

    mutating func begin(at offset: CGPoint) { desiredOffset = offset }
    mutating func cancelForUserInteraction() { desiredOffset = nil }

    mutating func takeOffset(constrainedTo available: CGPoint, contentReady: Bool = true, finalAttempt: Bool = false) -> CGPoint? {
        guard contentReady, let desiredOffset else { return nil }
        guard finalAttempt || available.y + 1 >= desiredOffset.y else { return nil }
        self.desiredOffset = nil
        return available
    }
}

/// An asynchronous reading block reports false while its placeholder is visible.
/// Ordinary synchronous content inherits true without any additional view state.
struct ATMWorkspaceContentReadyPreferenceKey: PreferenceKey {
    static let defaultValue = true

    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = nextValue() && value
    }
}

extension View {
    func atmRetainsScrollPosition(positions: ATMWorkspaceScrollPositions, key: String) -> some View {
        backgroundPreferenceValue(ATMWorkspaceContentReadyPreferenceKey.self) { contentReady in
            ATMWorkspaceScrollPosition(positions: positions, key: key, contentReady: contentReady)
        }
    }
}

/// Place inside scrolling content, so the nearest enclosing NSScrollView is
/// restored after remounting. The shell keeps only the small offset store alive.
struct ATMWorkspaceScrollPosition: NSViewRepresentable {
    let positions: ATMWorkspaceScrollPositions
    let key: String
    let contentReady: Bool

    func makeNSView(context: Context) -> Probe { Probe() }

    func updateNSView(_ view: Probe, context: Context) {
        view.configure(positions: positions, key: key, contentReady: contentReady)
    }

    static func dismantleNSView(_ view: Probe, coordinator: ()) { view.detach() }

    final class Probe: NSView {
        private var positions: ATMWorkspaceScrollPositions?
        private var key = ""
        private weak var scrollView: NSScrollView?
        private var boundsObserver: NSObjectProtocol?
        private var frameObserver: NSObjectProtocol?
        private var interactionMonitor: Any?
        private var initialRestore = ATMWorkspaceInitialScrollRestore()
        private var restoreScheduled = false
        private var contentReady = false
        private var clampScheduled = false
        private var readinessGeneration = 0
        private var generation = 0

        func configure(positions: ATMWorkspaceScrollPositions, key: String, contentReady: Bool) {
            if self.positions !== positions || self.key != key {
                detach()
                self.positions = positions
                self.key = key
            }
            if self.contentReady != contentReady {
                self.contentReady = contentReady
                readinessGeneration += 1
                clampScheduled = false
            }
            attachWhenReady()
            scheduleRestore()
        }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if window != nil { attachWhenReady() }
        }

        override func layout() {
            super.layout()
            attachWhenReady()
        }

        private func attachWhenReady() {
            guard let scroll = enclosingScrollView, scroll !== scrollView else { return }
            scrollView = scroll
            initialRestore.begin(at: positions?.offset(for: key) ?? .zero)
            scroll.contentView.postsBoundsChangedNotifications = true
            boundsObserver = NotificationCenter.default.addObserver(
                forName: NSView.boundsDidChangeNotification,
                object: scroll.contentView,
                queue: .main
            ) { [weak self] _ in
                MainActor.assumeIsolated { self?.recordOffset() }
            }
            if let document = scroll.documentView {
                document.postsFrameChangedNotifications = true
                frameObserver = NotificationCenter.default.addObserver(
                    forName: NSView.frameDidChangeNotification,
                    object: document,
                    queue: .main
                ) { [weak self] _ in
                    MainActor.assumeIsolated { self?.scheduleRestore() }
                }
            }
            // Only observe input during the bounded initial restoration. Return
            // every event unchanged; any interaction in this reader owns its position.
            interactionMonitor = NSEvent.addLocalMonitorForEvents(matching: [.scrollWheel, .leftMouseDown, .keyDown]) { [weak self] event in
                MainActor.assumeIsolated {
                    guard let self, let scroll = self.scrollView,
                          event.window === scroll.window else { return }
                    let isInside: Bool
                    if event.type == .keyDown {
                        isInside = (scroll.window?.firstResponder as? NSView)?.isDescendant(of: scroll) == true
                    } else {
                        isInside = scroll.bounds.contains(scroll.convert(event.locationInWindow, from: nil))
                    }
                    if isInside {
                        self.initialRestore.cancelForUserInteraction()
                        self.stopInitialObservers()
                    }
                }
                return event
            }
            scheduleRestore()
        }

        private func scheduleFinalClampIfNeeded() {
            guard initialRestore.isPending, contentReady, scrollView != nil, !clampScheduled else { return }
            clampScheduled = true
            let generation = generation
            let readinessGeneration = readinessGeneration
            // The deadline begins after preparation completes, never while an
            // uncached Markdown document is still waiting behind another parse.
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in
                guard let self, self.generation == generation,
                      self.readinessGeneration == readinessGeneration else { return }
                self.restoreIfReady(finalAttempt: true)
            }
        }

        private func scheduleRestore() {
            guard initialRestore.isPending, contentReady, scrollView != nil else { return }
            scheduleFinalClampIfNeeded()
            guard !restoreScheduled else { return }
            restoreScheduled = true
            let generation = generation
            DispatchQueue.main.async { [weak self] in
                guard let self, self.generation == generation else { return }
                self.restoreScheduled = false
                self.restoreIfReady()
            }
        }

        private func restoreIfReady(finalAttempt: Bool = false) {
            guard let scrollView, let desired = initialRestore.desiredOffset else { return }
            let clip = scrollView.contentView
            let available = clip.constrainBoundsRect(CGRect(origin: desired, size: clip.bounds.size)).origin
            guard let offset = initialRestore.takeOffset(
                constrainedTo: available,
                contentReady: contentReady,
                finalAttempt: finalAttempt
            ) else { return }
            stopInitialObservers()
            clip.scroll(to: offset)
            scrollView.reflectScrolledClipView(clip)
        }

        private func stopInitialObservers() {
            if let frameObserver { NotificationCenter.default.removeObserver(frameObserver) }
            frameObserver = nil
            if let interactionMonitor { NSEvent.removeMonitor(interactionMonitor) }
            interactionMonitor = nil
        }

        private func recordOffset() {
            guard !initialRestore.isPending, let scrollView, scrollView.window != nil else { return }
            positions?.set(scrollView.contentView.bounds.origin, for: key)
        }

        func detach() {
            recordOffset()
            generation += 1
            initialRestore.cancelForUserInteraction()
            restoreScheduled = false
            clampScheduled = false
            stopInitialObservers()
            if let boundsObserver { NotificationCenter.default.removeObserver(boundsObserver) }
            boundsObserver = nil
            scrollView = nil
        }
    }
}
