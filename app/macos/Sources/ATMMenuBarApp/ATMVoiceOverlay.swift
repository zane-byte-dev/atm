import AppKit
import SwiftUI

/// The strip that appears while someone is dictating.
///
/// A panel of its own rather than a state on the notch: the notch carries what agents
/// push at you, and this carries what you are doing right now with a key held down.
/// Putting both in the same place would mean one covering the other at exactly the
/// moment both matter.
///
/// Bottom-centre of whichever screen has the pointer — near where someone is looking
/// while they talk, and far from the notch.
@MainActor
final class ATMVoiceOverlayController {
    static let shared = ATMVoiceOverlayController()

    private static let size = NSSize(width: 420, height: 84)

    private var panel: NSPanel?

    func show(coordinator: ATMVoiceInputCoordinator) {
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

    private func makePanel(coordinator: ATMVoiceInputCoordinator) -> NSPanel {
        let panel = NSPanel(
            contentRect: NSRect(origin: .zero, size: Self.size),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        panel.contentView = NSHostingView(rootView: ATMVoiceOverlayView(coordinator: coordinator))
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
        panel.ignoresMouseEvents = false
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

private struct ATMVoiceOverlayView: View {
    @ObservedObject var coordinator: ATMVoiceInputCoordinator

    var body: some View {
        HStack(spacing: 14) {
            statusGlyph
                .frame(width: 32, height: 32)

            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                Text(detail)
                    .font(ATMFont.footnote)
                    .foregroundStyle(detailColor)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Spacer(minLength: 8)

            if coordinator.state.isActive {
                Button {
                    coordinator.cancel()
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(ATMTheme.secondary)
                        .frame(width: 26, height: 26)
                        .background(ATMTheme.controlFill, in: Circle())
                }
                .buttonStyle(.plain)
                .help("取消（⎋）")
            }
        }
        .padding(.horizontal, 18)
        .frame(width: 420, height: 84)
        .background(.ultraThickMaterial, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(ATMTheme.border, lineWidth: 1)
        )
    }

    @ViewBuilder
    private var statusGlyph: some View {
        switch coordinator.state {
        case .requestingPermission, .processing:
            ProgressView()
                .controlSize(.small)
        case .recording:
            ATMVoiceLevelBars(level: coordinator.inputLevel)
        case .success:
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 24))
                .foregroundStyle(ATMTheme.success)
        case .failed:
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 22))
                .foregroundStyle(ATMTheme.warning)
        case .idle:
            Image(systemName: "waveform")
                .font(.system(size: 20))
                .foregroundStyle(ATMTheme.accent)
        }
    }

    private var title: String {
        switch coordinator.state {
        case .idle:
            return "语音输入"
        case .requestingPermission:
            return "正在打开麦克风…"
        case .recording:
            return coordinator.activeEngineName.isEmpty
                ? "正在听"
                : "正在听 · \(coordinator.activeEngineName)"
        case .processing:
            return "正在转写…"
        case .success:
            return coordinator.lastOutcome == .copiedToPasteboardOnly ? "已复制" : "已写入"
        case .failed:
            return "语音输入失败"
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
        case .success(let text):
            guard coordinator.lastOutcome == .copiedToPasteboardOnly else { return text }
            return "缺少辅助功能权限，没法自动粘贴 —— 按 ⌘V 贴上"
        case .failed(let message):
            return message
        }
    }

    private var detailColor: Color {
        switch coordinator.state {
        case .failed:
            return ATMTheme.warning
        case .success:
            return coordinator.lastOutcome == .copiedToPasteboardOnly
                ? ATMTheme.warning
                : ATMTheme.secondary
        default:
            return ATMTheme.secondary
        }
    }
}

/// Five bars driven by the real microphone level.
///
/// Real RMS rather than a decorative animation: an animation says "we are recording",
/// which is already on the label, while a real meter also answers "is this microphone
/// actually picking me up". Choosing the wrong input device is the most common way
/// dictation silently produces nothing, and a flat meter shows it immediately.
private struct ATMVoiceLevelBars: View {
    let level: Float

    /// Each bar responds to a slightly different slice of the level so the shape reads
    /// as a meter rather than five copies of one number.
    private static let weights: [Float] = [0.55, 0.8, 1.0, 0.8, 0.55]

    var body: some View {
        HStack(alignment: .center, spacing: 2.5) {
            ForEach(Array(Self.weights.enumerated()), id: \.offset) { _, weight in
                Capsule()
                    .fill(ATMTheme.accent)
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
