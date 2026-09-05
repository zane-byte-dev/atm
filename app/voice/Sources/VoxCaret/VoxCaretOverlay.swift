import AppKit
import SwiftUI

/// The strip that appears while someone is dictating.
///
/// The app's only on-screen panel now that agent state has moved to Notification
/// Center. It earns that by being strictly modal to an action in progress: it
/// exists while a key is held down and goes away when it is released.
///
/// Bottom-centre of whichever screen has the pointer — near where someone is looking
/// while they talk, and clear of the menu bar.
@MainActor
final class VoxCaretOverlayController {
    static let shared = VoxCaretOverlayController()

    private static let size = NSSize(width: 224, height: 54)

    private var panel: NSPanel?

    func show(coordinator: VoxCaretInputCoordinator) {
        let panel = panel ?? makePanel(coordinator: coordinator)
        self.panel = panel
        position(panel)
        // Not `makeKeyAndOrderFront`: taking key status would pull focus away from the
        // app the text is about to be pasted into, and the paste would land here.
        panel.orderFrontRegardless()
    }

    func hide() {
        panel?.orderOut(nil)
    }

    private func makePanel(coordinator: VoxCaretInputCoordinator) -> NSPanel {
        let panel = NSPanel(
            contentRect: NSRect(origin: .zero, size: Self.size),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        panel.contentView = NSHostingView(rootView: VoxCaretOverlayView(coordinator: coordinator))
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        // Above full-screen apps and the menu bar: dictation is used inside whatever is
        // already on screen, including things that own the whole display.
        panel.level = .statusBar
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]
        panel.hidesOnDeactivate = false
        panel.isMovable = false
        panel.isReleasedWhenClosed = false
        // The pill is feedback, not a second surface to operate. Ignoring the mouse
        // keeps it from blocking controls beneath it; Esc remains the cancel action.
        panel.ignoresMouseEvents = true
        return panel
    }

    private func position(_ panel: NSPanel) {
        let mouseLocation = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouseLocation, $0.frame, false) }
            ?? NSScreen.main
            ?? NSScreen.screens.first
        guard let visibleFrame = screen?.visibleFrame else { return }
        panel.setFrameOrigin(
            NSPoint(
                x: visibleFrame.midX - Self.size.width / 2,
                y: visibleFrame.minY + 64
            )
        )
    }
}

private struct VoxCaretOverlayView: View {
    @ObservedObject var coordinator: VoxCaretInputCoordinator

    var body: some View {
        HStack(spacing: 11) {
            VoxCaretBrandMark()
                .stroke(
                    .white.opacity(0.92),
                    style: StrokeStyle(lineWidth: 1.55, lineCap: .round, lineJoin: .round)
                )
                .frame(width: 23, height: 15)

            Text(title)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(.white)
                .lineLimit(1)

            Rectangle()
                .fill(.white.opacity(0.22))
                .frame(width: 1, height: 19)

            statusGlyph
                .frame(width: 42, height: 26)
        }
        .padding(.horizontal, 19)
        .frame(width: 224, height: 54)
        .background(Color.black.opacity(0.92), in: Capsule())
        .overlay(
            Capsule().strokeBorder(.white.opacity(0.14), lineWidth: 1)
        )
        .shadow(color: .black.opacity(0.24), radius: 12, y: 5)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(VoxCaretBrand.chineseName)，\(title)。\(detail)")
    }

    @ViewBuilder
    private var statusGlyph: some View {
        switch coordinator.state {
        case .requestingPermission, .processing, .cancelling:
            ProgressView()
                .controlSize(.small)
                .tint(.white)
        case .recording:
            VoxCaretLevelBars(level: coordinator.inputLevel, color: .white)
        case .success:
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 21))
                .foregroundStyle(.white)
        case .failed:
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 20))
                .foregroundStyle(.orange)
        case .idle:
            Image(systemName: "waveform")
                .font(.system(size: 19))
                .foregroundStyle(.white)
        }
    }

    private var title: String {
        switch coordinator.state {
        case .idle:
            return "直接说"
        case .requestingPermission:
            return "准备中"
        case .recording:
            return "直接说"
        case .processing:
            return "转写中"
        case .cancelling:
            return "撤销中"
        case .success:
            return coordinator.lastOutcome == .copiedToPasteboardOnly ? "已复制" : "已写入"
        case .failed:
            return "输入失败"
        }
    }

    private var detail: String {
        switch coordinator.state {
        case .idle:
            return "按住快捷键说话"
        case .requestingPermission:
            return "第一次使用需要授权"
        case .recording(let partial):
            // The level meter already says something is being heard, so this line is
            // free to say what happens next instead.
            if !partial.isEmpty { return partial }
            return coordinator.showsLevelMeter
                ? "松手转写，⎋ 取消"
                : "松手写入，⎋ 取消"
        case .processing:
            return "转写完成后写入刚才那个应用"
        case .cancelling:
            return "正在移除已经实时写入的文字"
        case .success(let text):
            guard coordinator.lastOutcome == .copiedToPasteboardOnly else { return text }
            return "缺少辅助功能权限，没法自动粘贴 —— 按 ⌘V 贴上"
        case .failed(let message):
            return message
        }
    }

}

/// Five bars driven by the real microphone level.
///
/// Real RMS rather than a decorative animation: an animation says "we are recording",
/// which is already on the label, while a real meter also answers "is this microphone
/// actually picking me up". Choosing the wrong input device is the most common way
/// dictation silently produces nothing, and a flat meter shows it immediately.
private struct VoxCaretLevelBars: View {
    let level: Float
    var color: Color = VoxCaretTheme.accent

    /// Each bar responds to a slightly different slice of the level so the shape reads
    /// as a meter rather than five copies of one number.
    private static let weights: [Float] = [0.55, 0.8, 1.0, 0.8, 0.55]

    var body: some View {
        HStack(alignment: .center, spacing: 2.5) {
            ForEach(Array(Self.weights.enumerated()), id: \.offset) { _, weight in
                Capsule()
                    .fill(color)
                    .frame(width: 3, height: height(for: weight))
            }
        }
        .frame(height: 26)
        .animation(.easeOut(duration: 0.08), value: level)
    }

    private func height(for weight: Float) -> CGFloat {
        let minimum: Float = 4
        let maximum: Float = 26
        let scaled = minimum + (maximum - minimum) * min(1, level * weight)
        return CGFloat(scaled)
    }
}
