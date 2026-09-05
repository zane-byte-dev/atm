import AppKit
import SwiftUI

/// Monochrome adaptation of Web's `mark.svg`. The rounded tile and cut-out A
/// keep the same silhouette while remaining a native macOS template image.
private struct ATMCompanionBrandGlyph: View {
    var body: some View {
        Canvas { context, size in
            let side = min(size.width, size.height)
            let scale = side / 64
            let origin = CGPoint(x: (size.width - side) / 2, y: (size.height - side) / 2)
            let color = Color.black

            let tile = CGRect(
                x: origin.x,
                y: origin.y,
                width: side,
                height: side
            )
            var silhouette = Path(roundedRect: tile, cornerRadius: 17 * scale)
            var mark = Path()
            mark.move(to: point(15, 45, origin: origin, scale: scale))
            mark.addLine(to: point(27, 18, origin: origin, scale: scale))
            mark.addLine(to: point(37, 18, origin: origin, scale: scale))
            mark.addLine(to: point(49, 45, origin: origin, scale: scale))
            mark.addLine(to: point(38, 45, origin: origin, scale: scale))
            mark.addLine(to: point(32, 28, origin: origin, scale: scale))
            mark.addLine(to: point(26, 45, origin: origin, scale: scale))
            mark.closeSubpath()
            silhouette.addPath(mark)
            context.fill(silhouette, with: .color(color), style: FillStyle(eoFill: true))

            var crossbar = Path()
            crossbar.move(to: point(26, 39, origin: origin, scale: scale))
            crossbar.addLine(to: point(38, 39, origin: origin, scale: scale))
            context.stroke(
                crossbar,
                with: .color(color),
                style: StrokeStyle(lineWidth: max(4 * scale, 1), lineCap: .butt)
            )
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }

    private func point(_ x: CGFloat, _ y: CGFloat, origin: CGPoint, scale: CGFloat) -> CGPoint {
        CGPoint(x: origin.x + x * scale, y: origin.y + y * scale)
    }
}

@MainActor
enum ATMCompanionBrandAssets {
    static func menuBarImage(scale: CGFloat = NSScreen.main?.backingScaleFactor ?? 2) -> NSImage {
        let pointSize = NSSize(width: 18, height: 18)
        let renderer = ImageRenderer(
            content: ATMCompanionBrandGlyph()
                .frame(width: 16, height: 16)
                .padding(1)
        )
        renderer.scale = scale
        let image = renderer.nsImage ?? NSImage(size: pointSize)
        image.size = pointSize
        image.isTemplate = true
        image.accessibilityDescription = "ATM"
        return image
    }
}
