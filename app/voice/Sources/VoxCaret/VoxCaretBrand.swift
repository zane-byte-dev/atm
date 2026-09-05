import AppKit
import SwiftUI

enum VoxCaretBrand {
    static let name = "VoxCaret"
    static let chineseName = "声标"
    static let displayName = "\(name) \(chineseName)"
    static let tagline = "按住说话，松开成字"

    /// A one-colour, resolution-independent version of the app mark for the menu bar.
    /// Template rendering lets macOS choose the correct foreground colour for light,
    /// dark, selected, and accessibility menu-bar appearances.
    static func statusIcon() -> NSImage {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: false) { _ in
            NSColor.black.setStroke()

            let waveform = NSBezierPath()
            waveform.move(to: NSPoint(x: 1.3, y: 9.0))
            waveform.line(to: NSPoint(x: 2.6, y: 9.0))
            waveform.line(to: NSPoint(x: 3.7, y: 6.7))
            waveform.line(to: NSPoint(x: 4.9, y: 12.5))
            waveform.line(to: NSPoint(x: 6.2, y: 3.9))
            waveform.line(to: NSPoint(x: 7.6, y: 13.5))
            waveform.line(to: NSPoint(x: 9.0, y: 7.3))
            waveform.line(to: NSPoint(x: 10.5, y: 9.0))
            waveform.line(to: NSPoint(x: 13.0, y: 9.0))
            waveform.lineWidth = 1.65
            waveform.lineCapStyle = .round
            waveform.lineJoinStyle = .round
            waveform.stroke()

            let caret = NSBezierPath()
            caret.move(to: NSPoint(x: 14.8, y: 4.0))
            caret.line(to: NSPoint(x: 14.8, y: 14.0))
            caret.move(to: NSPoint(x: 12.9, y: 4.0))
            caret.line(to: NSPoint(x: 16.7, y: 4.0))
            caret.move(to: NSPoint(x: 12.9, y: 14.0))
            caret.line(to: NSPoint(x: 16.7, y: 14.0))
            caret.lineWidth = 1.65
            caret.lineCapStyle = .round
            caret.lineJoinStyle = .round
            caret.stroke()

            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = displayName
        return image
    }
}

struct VoxCaretBrandMark: Shape {
    func path(in rect: CGRect) -> Path {
        let scaleX = rect.width / 18
        let scaleY = rect.height / 18
        func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
            CGPoint(x: rect.minX + x * scaleX, y: rect.minY + y * scaleY)
        }

        var path = Path()
        path.move(to: point(1.3, 9.0))
        path.addLine(to: point(2.6, 9.0))
        path.addLine(to: point(3.7, 11.3))
        path.addLine(to: point(4.9, 5.5))
        path.addLine(to: point(6.2, 14.1))
        path.addLine(to: point(7.6, 4.5))
        path.addLine(to: point(9.0, 10.7))
        path.addLine(to: point(10.5, 9.0))
        path.addLine(to: point(13.0, 9.0))
        path.move(to: point(14.8, 4.0))
        path.addLine(to: point(14.8, 14.0))
        path.move(to: point(12.9, 4.0))
        path.addLine(to: point(16.7, 4.0))
        path.move(to: point(12.9, 14.0))
        path.addLine(to: point(16.7, 14.0))
        return path
    }
}
