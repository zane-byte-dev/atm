import AppKit
import SwiftUI

final class FloatingPanel: NSPanel {
    private let effectView: NSVisualEffectView

    /// Called when the panel is asked to close. The panel deliberately has no
    /// close button, so the default `performClose(_:)` would only beep at ⌘W.
    var onDismiss: (() -> Void)?

    init(size: NSSize) {
        effectView = NSVisualEffectView(frame: NSRect(origin: .zero, size: size))
        super.init(
            contentRect: NSRect(origin: .zero, size: size),
            styleMask: [.titled, .resizable, .nonactivatingPanel, .fullSizeContentView, .utilityWindow],
            backing: .buffered,
            defer: false
        )

        titleVisibility = .hidden
        titlebarAppearsTransparent = true
        level = .floating
        collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]
        isOpaque = false
        backgroundColor = .clear
        hasShadow = true
        hidesOnDeactivate = false
        isReleasedWhenClosed = false
        isFloatingPanel = true
        animationBehavior = .utilityWindow
        minSize = NSSize(width: 340, height: 290)

        standardWindowButton(.closeButton)?.isHidden = true
        standardWindowButton(.miniaturizeButton)?.isHidden = true
        standardWindowButton(.zoomButton)?.isHidden = true

        effectView.material = .popover
        effectView.blendingMode = .behindWindow
        effectView.state = .active
        effectView.isEmphasized = false
        effectView.wantsLayer = true
        effectView.layer?.cornerRadius = 16
        effectView.layer?.masksToBounds = true
        contentView = effectView
    }

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }

    override func performClose(_ sender: Any?) {
        onDismiss?()
    }

    func host<Content: View>(_ content: Content) {
        let hostingView = NSHostingView(rootView: content)
        hostingView.translatesAutoresizingMaskIntoConstraints = false
        effectView.subviews.forEach { $0.removeFromSuperview() }
        effectView.addSubview(hostingView)
        NSLayoutConstraint.activate([
            hostingView.leadingAnchor.constraint(equalTo: effectView.leadingAnchor),
            hostingView.trailingAnchor.constraint(equalTo: effectView.trailingAnchor),
            hostingView.topAnchor.constraint(equalTo: effectView.topAnchor),
            hostingView.bottomAnchor.constraint(equalTo: effectView.bottomAnchor),
        ])
    }

    func anchor(to statusButton: NSStatusBarButton) {
        guard let buttonWindow = statusButton.window,
              let screen = buttonWindow.screen ?? NSScreen.main else { return }

        let buttonRect = buttonWindow.convertToScreen(statusButton.convert(statusButton.bounds, to: nil))
        let visible = screen.visibleFrame
        let margin: CGFloat = 10
        let gap: CGFloat = 8

        var x = buttonRect.midX - frame.width / 2
        x = min(max(x, visible.minX + margin), visible.maxX - frame.width - margin)

        var y = buttonRect.minY - frame.height - gap
        if y < visible.minY + margin {
            y = buttonRect.maxY + gap
        }
        y = min(y, visible.maxY - frame.height - margin)
        setFrameOrigin(NSPoint(x: x, y: y))
    }
}
