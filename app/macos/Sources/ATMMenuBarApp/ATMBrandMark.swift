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
    private static let resourceBundle: Bundle = {
        let name = "ATMMenuBarApp_ATMMenuBarApp.bundle"
        let candidates = [
            Bundle.main.resourceURL,
            Bundle.main.bundleURL,
            Bundle.main.executableURL?.deletingLastPathComponent(),
        ]
        for baseURL in candidates.compactMap({ $0 }) {
            if let bundle = Bundle(url: baseURL.appendingPathComponent(name, isDirectory: true)) {
                return bundle
            }
        }
        return Bundle.module
    }()

    private static let agentIconImages: [String: NSImage] = {
        let names = [
            "agent-claude", "agent-codex", "agent-pi", "agent-copilot",
            "agent-cursor", "agent-qoder", "agent-grok",
        ]
        return Dictionary(uniqueKeysWithValues: names.compactMap { name in
            guard
                let url = resourceBundle.url(forResource: name, withExtension: "png"),
                let image = NSImage(contentsOf: url)
            else { return nil }
            return (name, image)
        })
    }()

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

    static func agentIconImage(resourceName: String) -> NSImage? {
        agentIconImages[resourceName]
    }
}

extension ATMAgentDisplay {
    /// Brand tile background for the client logo chip.
    static func brandBackground(_ agent: String) -> Color {
        switch key(agent) {
        case "claude":
            return Color(red: 238 / 255, green: 111 / 255, blue: 79 / 255)
        case "codex":
            return Color(red: 9 / 255, green: 121 / 255, blue: 108 / 255)
        case "pi":
            return Color(red: 110 / 255, green: 80 / 255, blue: 164 / 255)
        case "copilot":
            return Color(red: 67 / 255, green: 68 / 255, blue: 70 / 255)
        case "cursor":
            return Color(red: 28 / 255, green: 28 / 255, blue: 29 / 255)
        case "qoder", "qodercli", "qoderwork":
            return Color(red: 28 / 255, green: 112 / 255, blue: 226 / 255)
        case "grokbuild":
            return Color(red: 49 / 255, green: 52 / 255, blue: 59 / 255)
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
/// Known clients use the approved bundled icon set so every surface renders the
/// exact same silhouette and flat background. Unknown clients retain a compact
/// generated monogram rather than showing a missing image.
struct ATMAgentMark: View {
    let agent: String
    var size: CGFloat = 18

    private var key: String { ATMAgentDisplay.key(agent) }

    var body: some View {
        mark
        .frame(width: size, height: size)
        .accessibilityLabel(ATMAgentDisplay.name(agent))
        .help(ATMAgentDisplay.name(agent))
    }

    @ViewBuilder
    private var mark: some View {
        if
            let resourceName = ATMAgentDisplay.iconResourceName(key),
            let image = ATMBrandAssets.agentIconImage(resourceName: resourceName)
        {
            Image(nsImage: image)
                .resizable()
                .interpolation(.high)
                .antialiased(true)
                .clipShape(
                    RoundedRectangle(
                        cornerRadius: size * 0.235,
                        style: .circular
                    )
                )
        } else {
            Canvas { context, canvasSize in
                let side = min(canvasSize.width, canvasSize.height)
                let rect = CGRect(
                    x: (canvasSize.width - side) / 2,
                    y: (canvasSize.height - side) / 2,
                    width: side,
                    height: side
                )
                context.fill(
                    Path(roundedRect: rect, cornerRadius: side * 0.235, style: .circular),
                    with: .color(ATMAgentDisplay.brandBackground(agent))
                )
                let monogram = ATMAgentDisplay.monogram(agent)
                context.draw(
                    Text(monogram)
                        .font(.system(size: side * (monogram.count > 1 ? 0.38 : 0.48), weight: .bold, design: .rounded))
                        .foregroundColor(.white),
                    at: CGPoint(x: rect.midX, y: rect.midY),
                    anchor: .center
                )
            }
        }
    }
}
