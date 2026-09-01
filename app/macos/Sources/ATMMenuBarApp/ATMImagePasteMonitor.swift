import AppKit
import SwiftUI

/// Recognizes the ordinary paste shortcut without stealing Paste and Match Style
/// or other modified variants from the current first responder.
enum ATMImagePasteShortcut {
    static func matches(_ event: NSEvent) -> Bool {
        guard event.type == .keyDown,
              event.charactersIgnoringModifiers?.lowercased() == "v" else { return false }
        let meaningful = event.modifierFlags
            .intersection(.deviceIndependentFlagsMask)
            .subtracting([.capsLock, .numericPad, .function])
        return meaningful == [.command]
    }
}

/// Gives a sheet first refusal on Command-V, regardless of which child view is
/// first responder. AppKit text views do not reliably forward image-only paste
/// through an overridden `paste(_:)`, while a local key event arrives before the
/// responder chain. The monitor is scoped to the representable's own window and
/// is removed with the sheet.
struct ATMImagePasteMonitor: NSViewRepresentable {
    var onPasteImages: (NSPasteboard) -> Bool

    func makeCoordinator() -> Coordinator {
        Coordinator(onPasteImages: onPasteImages)
    }

    func makeNSView(context: Context) -> NSView {
        let view = NSView(frame: .zero)
        context.coordinator.hostView = view
        context.coordinator.start()
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        context.coordinator.onPasteImages = onPasteImages
    }

    static func dismantleNSView(_ nsView: NSView, coordinator: Coordinator) {
        coordinator.stop()
    }

    @MainActor
    final class Coordinator {
        weak var hostView: NSView?
        var onPasteImages: (NSPasteboard) -> Bool
        private var monitor: Any?

        init(onPasteImages: @escaping (NSPasteboard) -> Bool) {
            self.onPasteImages = onPasteImages
        }

        func start() {
            guard monitor == nil else { return }
            monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
                guard let self,
                      ATMImagePasteShortcut.matches(event),
                      let window = self.hostView?.window,
                      event.window === window else { return event }
                return self.onPasteImages(.general) ? nil : event
            }
        }

        func stop() {
            guard let monitor else { return }
            NSEvent.removeMonitor(monitor)
            self.monitor = nil
        }

    }
}
