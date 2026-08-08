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
        minSize = NSSize(width: 360, height: 320)

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
        setFrameOrigin(Self.anchoredOrigin(
            buttonRect: buttonRect,
            panelSize: frame.size,
            visibleFrame: screen.visibleFrame
        ))
    }

    /// Positions the quick panel directly against the menu bar. `visibleFrame`
    /// already stops at the lower edge of the menu bar, so applying the regular
    /// window margin to its upper edge creates a visible (and unintended) gap.
    static func anchoredOrigin(
        buttonRect: NSRect,
        panelSize: NSSize,
        visibleFrame: NSRect,
        margin: CGFloat = 10,
        gap: CGFloat = 0
    ) -> NSPoint {
        var x = buttonRect.midX - panelSize.width / 2
        x = min(max(x, visibleFrame.minX + margin), visibleFrame.maxX - panelSize.width - margin)

        let lowerBound = visibleFrame.minY + margin
        let belowButton = buttonRect.minY - panelSize.height - gap
        let y: CGFloat
        if belowButton >= lowerBound {
            y = belowButton
        } else {
            let aboveButton = buttonRect.maxY + gap
            let upperBound = visibleFrame.maxY - panelSize.height - margin
            y = min(max(aboveButton, lowerBound), upperBound)
        }

        return NSPoint(x: x, y: y)
    }
}
