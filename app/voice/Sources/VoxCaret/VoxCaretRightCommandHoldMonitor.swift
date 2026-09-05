import AppKit
import Carbon.HIToolbox

/// Turns a modifier-only gesture into a deliberate hold action. A normal Command tap
/// remains untouched; only a hold that survives the threshold starts dictation.
@MainActor
final class VoxCaretLongPressGesture {
    private let threshold: Duration
    private let onLongPress: () -> Void
    private let onReleaseAfterLongPress: () -> Void
    private var pendingTask: Task<Void, Never>?
    private var isPressed = false
    private var didTrigger = false

    init(
        threshold: Duration = .milliseconds(220),
        onLongPress: @escaping () -> Void,
        onReleaseAfterLongPress: @escaping () -> Void
    ) {
        self.threshold = threshold
        self.onLongPress = onLongPress
        self.onReleaseAfterLongPress = onReleaseAfterLongPress
    }

    func press() {
        guard !isPressed else { return }
        isPressed = true
        didTrigger = false
        pendingTask?.cancel()
        pendingTask = Task { [weak self] in
            guard let self else { return }
            do {
                try await Task.sleep(for: threshold)
            } catch {
                return
            }
            guard !Task.isCancelled, isPressed else { return }
            didTrigger = true
            onLongPress()
        }
    }

    func release() {
        guard isPressed else { return }
        isPressed = false
        pendingTask?.cancel()
        pendingTask = nil
        if didTrigger { onReleaseAfterLongPress() }
        didTrigger = false
    }

    func cancel() {
        isPressed = false
        didTrigger = false
        pendingTask?.cancel()
        pendingTask = nil
    }
}

/// Watches the physical right Command key without consuming it. Global modifier
/// events require Accessibility permission, which VoxCaret already needs to write
/// live text into another app; the ordinary configurable Carbon shortcut remains a
/// fallback when that permission has not been granted.
@MainActor
final class VoxCaretRightCommandHoldMonitor {
    static let shared = VoxCaretRightCommandHoldMonitor()

    private var globalMonitor: Any?
    private var localMonitor: Any?
    private var defaultsObserver: NSObjectProtocol?
    private lazy var gesture = VoxCaretLongPressGesture(
        onLongPress: { VoxCaretInputCoordinator.shared.hotKeyPressed() },
        onReleaseAfterLongPress: { VoxCaretInputCoordinator.shared.hotKeyReleased() }
    )

    func start() {
        apply()
        guard defaultsObserver == nil else { return }
        defaultsObserver = NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: UserDefaults.standard,
            queue: .main
        ) { _ in
            Task { @MainActor in VoxCaretRightCommandHoldMonitor.shared.apply() }
        }
    }

    func stop() {
        gesture.cancel()
        if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
        if let localMonitor { NSEvent.removeMonitor(localMonitor) }
        globalMonitor = nil
        localMonitor = nil
        if let defaultsObserver {
            NotificationCenter.default.removeObserver(defaultsObserver)
            self.defaultsObserver = nil
        }
    }

    private func apply() {
        guard VoxCaretInputPreferences.rightCommandHoldEnabled else {
            gesture.cancel()
            if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
            if let localMonitor { NSEvent.removeMonitor(localMonitor) }
            globalMonitor = nil
            localMonitor = nil
            return
        }
        guard globalMonitor == nil, localMonitor == nil else { return }

        globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: .flagsChanged) { event in
            Task { @MainActor in
                VoxCaretRightCommandHoldMonitor.shared.handle(event)
            }
        }
        localMonitor = NSEvent.addLocalMonitorForEvents(matching: .flagsChanged) { event in
            Task { @MainActor in
                VoxCaretRightCommandHoldMonitor.shared.handle(event)
            }
            return event
        }
    }

    private func handle(_ event: NSEvent) {
        guard event.keyCode == UInt16(kVK_RightCommand) else { return }
        if event.modifierFlags.contains(.command) {
            // Both apps would otherwise start recording from the same modifier-only
            // gesture and race to edit the same field. Keep VoxCaret quiet while the
            // reference app is running; quitting it makes this entry point available
            // immediately, without restarting VoxCaret.
            guard NSRunningApplication.runningApplications(
                withBundleIdentifier: "cn.shandianshuo.desktop"
            ).isEmpty else {
                gesture.cancel()
                return
            }
            gesture.press()
        } else {
            gesture.release()
        }
    }
}
