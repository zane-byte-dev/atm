import AppKit
import SwiftUI

/// The legacy ATM status item glyph, kept locally so the lightweight Menu app
/// does not depend on the old monolithic macOS target.
private struct ATMCompanionBrandGlyph: View {
    var body: some View {
        Canvas { context, size in
            let side = min(size.width, size.height)
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            let ringRadius = side * 0.32
            let ringWidth = max(side * 0.085, 1)
            let connectorWidth = max(side * 0.07, 1)
            let nodeRadius = side * 0.082
            let centerRadius = side * 0.052
            let angles = [-Double.pi / 2, Double.pi / 6, 5 * Double.pi / 6]
            let color = Color.black

            let ring = CGRect(
                x: center.x - ringRadius,
                y: center.y - ringRadius,
                width: ringRadius * 2,
                height: ringRadius * 2
            )
            context.stroke(Path(ellipseIn: ring), with: .color(color), lineWidth: ringWidth)

            var connectors = Path()
            for angle in angles {
                let node = CGPoint(
                    x: center.x + cos(angle) * ringRadius,
                    y: center.y + sin(angle) * ringRadius
                )
                connectors.move(to: center)
                connectors.addLine(to: node)
                context.fill(Path(ellipseIn: circle(node, nodeRadius)), with: .color(color))
            }
            context.stroke(
                connectors,
                with: .color(color),
                style: StrokeStyle(lineWidth: connectorWidth, lineCap: .round)
            )
            context.fill(Path(ellipseIn: circle(center, centerRadius)), with: .color(color))
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }

    private func circle(_ center: CGPoint, _ radius: CGFloat) -> CGRect {
        CGRect(x: center.x - radius, y: center.y - radius, width: radius * 2, height: radius * 2)
    }
}

@MainActor
enum ATMCompanionBrandAssets {
    static func menuBarImage(scale: CGFloat = NSScreen.main?.backingScaleFactor ?? 2) -> NSImage {
        let pointSize = NSSize(width: 18, height: 18)
        let renderer = ImageRenderer(
            content: ATMCompanionBrandGlyph()
                .frame(width: pointSize.width, height: pointSize.height)
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
