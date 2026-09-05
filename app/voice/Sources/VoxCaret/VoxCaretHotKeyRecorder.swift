import AppKit
import Carbon.HIToolbox
import SwiftUI

/// The "press the keys you want" field for the global shortcut.
///
/// Records with a *local* NSEvent monitor rather than a first-responder NSView:
/// the settings window is the key window while someone is editing this, so a
/// local monitor sees every keystroke without the permission prompt a global
/// keyboard monitor would trigger, and swallowing the event stops the keystroke
/// from also reaching menus or the surrounding controls.
struct VoxCaretHotKeyRecorder: View {
    @Binding var hotKey: VoxCaretHotKey
    var isEnabled: Bool = true
    /// What "恢复默认" restores. Passed in rather than read from
    /// `VoxCaretGlobalHotKeyPreferences`: more than one shortcut is recorded by this
    /// field now, and each has its own default.
    var defaultHotKey: VoxCaretHotKey = VoxCaretInputPreferences.defaultHotKey

    @State private var isRecording = false
    @State private var monitor: Any?
    @State private var rejectedIncomplete = false

    var body: some View {
        HStack(spacing: 10) {
            Button {
                if isRecording { cancelRecording() } else { startRecording() }
            } label: {
                Text(isRecording ? "按下快捷键…" : hotKey.displayString)
                    .font(VoxCaretFont.mono(.body, .semibold))
                    .foregroundStyle(fieldForeground)
                    .frame(minWidth: 120)
                    .padding(.vertical, 6)
                    .padding(.horizontal, 12)
                    .background(
                        isRecording ? VoxCaretTheme.accentFill : VoxCaretTheme.controlFill,
                        in: RoundedRectangle(cornerRadius: 8, style: .continuous)
                    )
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(isRecording ? VoxCaretTheme.accent : VoxCaretTheme.border, lineWidth: 1)
                    )
            }
            .buttonStyle(.plain)
            .disabled(!isEnabled)
            .help(isRecording ? "按 ⎋ 取消录制" : "点击后按下新的快捷键")

            if isRecording {
                Text(rejectedIncomplete ? "至少需要 ⌘、⌃ 或 ⌥ 之一" : "按 ⎋ 取消")
                    .font(VoxCaretFont.footnote)
                    .foregroundStyle(rejectedIncomplete ? VoxCaretTheme.warning : VoxCaretTheme.secondary)
            } else if hotKey != defaultHotKey {
                Button("恢复默认") { hotKey = defaultHotKey }
                    .buttonStyle(.link)
                    .font(VoxCaretFont.footnote)
                    .disabled(!isEnabled)
            }
        }
        // A monitor left installed after the settings pane goes away would keep
        // eating keystrokes for the rest of the process's life.
        .onDisappear { removeMonitor() }
    }

    private var fieldForeground: Color {
        if !isEnabled { return VoxCaretTheme.secondary }
        return isRecording ? VoxCaretTheme.accent : VoxCaretTheme.primary
    }

    private func startRecording() {
        guard monitor == nil else { return }
        isRecording = true
        rejectedIncomplete = false
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown]) { event in
            record(event)
            return nil
        }
    }

    private func record(_ event: NSEvent) {
        let modifiers = event.modifierFlags.intersection(VoxCaretHotKey.supportedModifiers)
        if event.keyCode == UInt16(kVK_Escape), modifiers.isEmpty {
            cancelRecording()
            return
        }
        let candidate = VoxCaretHotKey(keyCode: event.keyCode, modifiers: modifiers)
        guard candidate.isValid else {
            // Stay in recording mode: a bare letter is almost always a
            // half-finished chord, not a choice to abandon the edit.
            rejectedIncomplete = true
            return
        }
        hotKey = candidate
        cancelRecording()
    }

    private func cancelRecording() {
        removeMonitor()
        isRecording = false
        rejectedIncomplete = false
    }

    private func removeMonitor() {
        if let monitor {
            NSEvent.removeMonitor(monitor)
            self.monitor = nil
        }
    }
}
