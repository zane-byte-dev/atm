import AppKit
import SwiftUI

enum ATMBrandMarkStyle {
    case fullColor
    case template
}

struct ATMBrandGlyph: View {
    var style: ATMBrandMarkStyle = .fullColor

    var body: some View {
        Canvas { context, size in
            let side = min(size.width, size.height)
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            let ringRadius = side * 0.32
            let ringWidth = max(side * 0.085, 1)
            let connectorWidth = max(side * 0.07, 1)
            let haloRadius = side * 0.105
            let nodeRadius = side * 0.064
            let centerRadius = side * 0.052
            let nodeAngles = [-Double.pi / 2, Double.pi / 6, 5 * Double.pi / 6]
            let nodeColors = [
                Color(red: 52 / 255, green: 112 / 255, blue: 246 / 255),
                Color(red: 36 / 255, green: 215 / 255, blue: 226 / 255),
                Color(red: 147 / 255, green: 65 / 255, blue: 243 / 255),
            ]
            let templateColor = Color.black
            let ringColor = style == .template
                ? templateColor
                : Color(red: 52 / 255, green: 112 / 255, blue: 246 / 255)

            let ringRect = CGRect(
                x: center.x - ringRadius,
                y: center.y - ringRadius,
                width: ringRadius * 2,
                height: ringRadius * 2
            )
            context.stroke(
                Path(ellipseIn: ringRect),
                with: .color(ringColor),
                lineWidth: ringWidth
            )

            var connectors = Path()
            for angle in nodeAngles {
                let point = CGPoint(
                    x: center.x + cos(angle) * ringRadius,
                    y: center.y + sin(angle) * ringRadius
                )
                connectors.move(to: center)
                connectors.addLine(to: point)
            }
            context.stroke(
                connectors,
                with: .color(ringColor),
                style: StrokeStyle(lineWidth: connectorWidth, lineCap: .round)
            )

            for (index, angle) in nodeAngles.enumerated() {
                let point = CGPoint(
                    x: center.x + cos(angle) * ringRadius,
                    y: center.y + sin(angle) * ringRadius
                )
                if style == .fullColor {
                    context.fill(
                        Path(ellipseIn: circleRect(center: point, radius: haloRadius)),
                        with: .color(Color(red: 12 / 255, green: 32 / 255, blue: 78 / 255))
                    )
                }
                context.fill(
                    Path(ellipseIn: circleRect(
                        center: point,
                        radius: style == .template ? haloRadius * 0.78 : nodeRadius
                    )),
                    with: .color(style == .template ? templateColor : nodeColors[index])
                )
            }

            context.fill(
                Path(ellipseIn: circleRect(center: center, radius: centerRadius)),
                with: .color(style == .template ? templateColor : .white)
            )
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }

    private func circleRect(center: CGPoint, radius: CGFloat) -> CGRect {
        CGRect(
            x: center.x - radius,
            y: center.y - radius,
            width: radius * 2,
            height: radius * 2
        )
    }
}

struct ATMBrandMark: View {
    var body: some View {
        ATMBrandGlyph()
            .padding(4)
            .background(
                LinearGradient(
                    colors: [
                        Color(red: 28 / 255, green: 52 / 255, blue: 102 / 255),
                        Color(red: 5 / 255, green: 18 / 255, blue: 48 / 255),
                    ],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                in: RoundedRectangle(cornerRadius: 7, style: .continuous)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 7, style: .continuous)
                    .stroke(.white.opacity(0.16), lineWidth: 0.7)
            )
            .accessibilityLabel("ATM")
    }
}

@MainActor
enum ATMBrandAssets {
    static func menuBarImage(scale: CGFloat = NSScreen.main?.backingScaleFactor ?? 2) -> NSImage {
        let pointSize = NSSize(width: 18, height: 18)
        let renderer = ImageRenderer(
            content: ATMBrandGlyph(style: .template)
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

extension ATMAgentDisplay {
    /// Brand tile background for the client logo chip.
    static func brandBackground(_ agent: String) -> Color {
        switch key(agent) {
        case "claude":
            return Color(red: 218 / 255, green: 119 / 255, blue: 86 / 255) // Anthropic coral
        case "codex":
            return Color(red: 16 / 255, green: 163 / 255, blue: 127 / 255) // OpenAI green
        case "pi":
            return Color(red: 108 / 255, green: 66 / 255, blue: 221 / 255)
        case "copilot":
            return Color(red: 36 / 255, green: 41 / 255, blue: 47 / 255) // GitHub ink
        case "cursor":
            return Color(red: 14 / 255, green: 14 / 255, blue: 16 / 255)
        case "qoder", "qodercli", "qoderwork":
            return Color(red: 22 / 255, green: 119 / 255, blue: 255 / 255)
        case "grokbuild":
            return Color(red: 12 / 255, green: 12 / 255, blue: 12 / 255)
        default:
            return Color(nsColor: .controlAccentColor)
        }
    }

    /// Soft accent used when a filled tile would fight surrounding chrome.
    static func tint(_ agent: String) -> Color {
        brandBackground(agent)
    }
}

/// Full-color client logo chip (Agent list, quota, usage, search).
///
/// Drawn in-app so we do not need bundled SVG/PNG packs. Shapes track the
/// familiar product mark enough to read at 13–22pt.
struct ATMAgentMark: View {
    let agent: String
    var size: CGFloat = 18

    private var key: String { ATMAgentDisplay.key(agent) }

    var body: some View {
        Canvas { context, canvasSize in
            let side = min(canvasSize.width, canvasSize.height)
            let rect = CGRect(
                x: (canvasSize.width - side) / 2,
                y: (canvasSize.height - side) / 2,
                width: side,
                height: side
            )
            let corner = side * 0.28
            let bg = Path(roundedRect: rect, cornerRadius: corner, style: .continuous)
            context.fill(bg, with: .color(ATMAgentDisplay.brandBackground(agent)))

            let inset = side * 0.18
            let logoRect = rect.insetBy(dx: inset, dy: inset)
            drawLogo(context: context, in: logoRect, key: key)
        }
        .frame(width: size, height: size)
        .accessibilityLabel(ATMAgentDisplay.name(agent))
        .help(ATMAgentDisplay.name(agent))
    }

    private func drawLogo(context: GraphicsContext, in rect: CGRect, key: String) {
        let white = Color.white
        switch key {
        case "claude":
            drawClaudeStar(context: context, in: rect, color: white)
        case "codex":
            drawOpenAIBlossom(context: context, in: rect, color: white)
        case "pi":
            drawPiGlyph(context: context, in: rect, color: white)
        case "copilot":
            drawCopilotMark(context: context, in: rect, color: white)
        case "cursor":
            drawCursorPointer(context: context, in: rect, color: white)
        case "qoder", "qodercli", "qoderwork":
            drawQoderMark(context: context, in: rect, color: white)
        case "grokbuild":
            drawGrokMark(context: context, in: rect, color: white)
        default:
            drawMonogramFallback(context: context, in: rect, color: white)
        }
    }

    /// Anthropic-style multi-point starburst.
    private func drawClaudeStar(context: GraphicsContext, in rect: CGRect, color: Color) {
        let center = CGPoint(x: rect.midX, y: rect.midY)
        let outer = min(rect.width, rect.height) * 0.48
        let inner = outer * 0.38
        var path = Path()
        let points = 6
        for i in 0..<(points * 2) {
            let radius = i.isMultiple(of: 2) ? outer : inner
            let angle = -Double.pi / 2 + Double(i) * Double.pi / Double(points)
            let point = CGPoint(
                x: center.x + CGFloat(cos(angle)) * radius,
                y: center.y + CGFloat(sin(angle)) * radius
            )
            if i == 0 { path.move(to: point) } else { path.addLine(to: point) }
        }
        path.closeSubpath()
        context.fill(path, with: .color(color))
    }

    /// OpenAI-style hexagonal knot (simplified interlocking petals).
    private func drawOpenAIBlossom(context: GraphicsContext, in rect: CGRect, color: Color) {
        let center = CGPoint(x: rect.midX, y: rect.midY)
        let r = min(rect.width, rect.height) * 0.42
        let nodeR = r * 0.22
        let stroke = max(r * 0.18, 1.1)
        var ring = Path()
        for i in 0..<6 {
            let angle = -Double.pi / 2 + Double(i) * Double.pi / 3
            let point = CGPoint(
                x: center.x + CGFloat(cos(angle)) * r * 0.62,
                y: center.y + CGFloat(sin(angle)) * r * 0.62
            )
            if i == 0 { ring.move(to: point) } else { ring.addLine(to: point) }
        }
        ring.closeSubpath()
        context.stroke(ring, with: .color(color), style: StrokeStyle(lineWidth: stroke, lineJoin: .round))
        for i in 0..<6 {
            let angle = -Double.pi / 2 + Double(i) * Double.pi / 3
            let point = CGPoint(
                x: center.x + CGFloat(cos(angle)) * r * 0.62,
                y: center.y + CGFloat(sin(angle)) * r * 0.62
            )
            context.fill(
                Path(ellipseIn: CGRect(x: point.x - nodeR, y: point.y - nodeR, width: nodeR * 2, height: nodeR * 2)),
                with: .color(color)
            )
        }
    }

    private func drawPiGlyph(context: GraphicsContext, in rect: CGRect, color: Color) {
        let fontSize = min(rect.width, rect.height) * 0.95
        context.draw(
            Text("π")
                .font(.system(size: fontSize, weight: .bold, design: .rounded))
                .foregroundColor(color),
            at: CGPoint(x: rect.midX, y: rect.midY),
            anchor: .center
        )
    }

    /// Copilot-ish pilot head: helmet oval + visor + chin.
    private func drawCopilotMark(context: GraphicsContext, in rect: CGRect, color: Color) {
        let w = rect.width
        let h = rect.height
        var helmet = Path()
        helmet.addEllipse(in: CGRect(
            x: rect.minX + w * 0.12,
            y: rect.minY + h * 0.08,
            width: w * 0.76,
            height: h * 0.72
        ))
        context.stroke(helmet, with: .color(color), style: StrokeStyle(lineWidth: max(w * 0.1, 1), lineCap: .round))

        var visor = Path()
        visor.addRoundedRect(
            in: CGRect(x: rect.minX + w * 0.22, y: rect.minY + h * 0.32, width: w * 0.56, height: h * 0.22),
            cornerSize: CGSize(width: h * 0.1, height: h * 0.1)
        )
        context.fill(visor, with: .color(color))

        var chin = Path()
        chin.move(to: CGPoint(x: rect.midX - w * 0.12, y: rect.minY + h * 0.72))
        chin.addLine(to: CGPoint(x: rect.midX, y: rect.maxY - h * 0.06))
        chin.addLine(to: CGPoint(x: rect.midX + w * 0.12, y: rect.minY + h * 0.72))
        context.stroke(chin, with: .color(color), style: StrokeStyle(lineWidth: max(w * 0.09, 1), lineCap: .round, lineJoin: .round))
    }

    /// Cursor-style pointer glyph.
    private func drawCursorPointer(context: GraphicsContext, in rect: CGRect, color: Color) {
        let w = rect.width
        let h = rect.height
        var path = Path()
        path.move(to: CGPoint(x: rect.minX + w * 0.18, y: rect.minY + h * 0.12))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.18, y: rect.maxY - h * 0.12))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.42, y: rect.minY + h * 0.58))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.55, y: rect.minY + h * 0.78))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.62, y: rect.minY + h * 0.62))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.82, y: rect.minY + h * 0.55))
        path.closeSubpath()
        context.fill(path, with: .color(color))
    }

    /// Qoder: bold Q ring with tail.
    private func drawQoderMark(context: GraphicsContext, in rect: CGRect, color: Color) {
        let stroke = max(min(rect.width, rect.height) * 0.16, 1.2)
        let ring = rect.insetBy(dx: stroke * 0.4, dy: stroke * 0.4)
        context.stroke(
            Path(ellipseIn: ring.insetBy(dx: stroke * 0.55, dy: stroke * 0.55)),
            with: .color(color),
            style: StrokeStyle(lineWidth: stroke, lineCap: .round)
        )
        var tail = Path()
        tail.move(to: CGPoint(x: rect.midX + ring.width * 0.12, y: rect.midY + ring.height * 0.12))
        tail.addLine(to: CGPoint(x: rect.maxX - stroke * 0.2, y: rect.maxY - stroke * 0.15))
        context.stroke(tail, with: .color(color), style: StrokeStyle(lineWidth: stroke, lineCap: .round))
    }

    /// xAI / Grok-ish angular star-X.
    private func drawGrokMark(context: GraphicsContext, in rect: CGRect, color: Color) {
        let center = CGPoint(x: rect.midX, y: rect.midY)
        let outer = min(rect.width, rect.height) * 0.48
        let inner = outer * 0.32
        var path = Path()
        for i in 0..<8 {
            let radius = i.isMultiple(of: 2) ? outer : inner
            let angle = -Double.pi / 2 + Double(i) * Double.pi / 4
            let point = CGPoint(
                x: center.x + CGFloat(cos(angle)) * radius,
                y: center.y + CGFloat(sin(angle)) * radius
            )
            if i == 0 { path.move(to: point) } else { path.addLine(to: point) }
        }
        path.closeSubpath()
        context.fill(path, with: .color(color))
    }

    private func drawMonogramFallback(context: GraphicsContext, in rect: CGRect, color: Color) {
        let glyph = ATMAgentDisplay.monogram(agent)
        let fontSize = min(rect.width, rect.height) * (glyph.count > 1 ? 0.55 : 0.72)
        context.draw(
            Text(glyph)
                .font(.system(size: fontSize, weight: .bold, design: .rounded))
                .foregroundColor(color),
            at: CGPoint(x: rect.midX, y: rect.midY),
            anchor: .center
        )
    }
}
