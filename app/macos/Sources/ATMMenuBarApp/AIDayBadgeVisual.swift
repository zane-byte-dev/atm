import AppKit
import SwiftUI

/// AI Day badges are computational objects, not medals. Their shared language
/// is smoke glass, cold compute light and one warm human core; identity comes
/// from silhouette and spatial structure, so it survives grayscale and 32 px.
struct ATMAIDayBadgeVisual: View {
    let badge: ATMAIDayBadge
    let size: CGFloat
    /// 徽章自带的深色底座。
    ///
    /// 不是装饰，是徽章能不能出现在这里的前提。上面那三样——烟熏玻璃、冷光、一点暖核——都
    /// 是按深色底调的，浅底上冷光和玻璃全失效；角上那两行 `FORM / 01` 和 `AI–05` 更直接，
    /// 它们是写死的白字 38%，浅色 sheet 上根本看不见（`size >= 140` 才画，而两个详情 sheet
    /// 正好都用 150——这个 bug 就是这么来的）。
    ///
    /// 底座跟着徽章走之后，徽章在哪儿都是同一枚：浅色卡片上、系统 sheet 里、列表行里。
    /// 只有已经画在固定深色上的地方（分享卡）传 `false`，否则会多出一个方框。
    var plate: Bool = true

    /// 图形长边占底座直径的比例。
    ///
    /// 圆形底座的内接正方形是直径的 0.707，所以这个值得留在它以下，否则包围盒的角会伸出
    /// 圆外。0.64 是留了余量的：图形极点落在离圆心 0.45 的地方，圆半径 0.5。
    private static let glyphDiameter: CGFloat = 0.64

    private var active: Bool { badge.unlocked || badge.score != nil }
    private var displayLevel: Int { active ? max(1, badge.level) : 0 }
    private var asset: ATMAIDayBadgeAsset? { ATMAIDayBadgeAssets.asset(named: badge.id) }

    /// 未解锁的徽章画小一档，跟降饱和、压亮度一起表示「还没被发现」。
    ///
    /// 此前这件事由 `formScale` 做，它按等级给 0.93 / 1.07 / 1.09 / 1.11 四档——L1 到 L3
    /// 之间差 3.7%，看不出来，只是给本来就不齐的一组图再加一层抖动；而 PNG 一枚徽章只有一张，
    /// 等级根本没有对应的图形变化。现在只保留锁定与否这一个真实区别。
    private var glyphTarget: CGFloat {
        active ? Self.glyphDiameter : Self.glyphDiameter * 0.93
    }

    var body: some View {
        ZStack {
            // 接触阴影要压在图形底边之下。图形归一化成直径的 64% 之后底边在 0.82，原来那组
            // 数（0.62 宽、偏移 0.34）是按「画布铺满底座」调的，现在会糊到图形中间去；同时
            // 加上模糊也不能溢出圆外——`drawingGroup` 只按方形 frame 裁剪，圆外的污迹会露在
            // 卡片底色上。
            Ellipse()
                .fill(Color.black.opacity(active ? 0.25 : 0.10))
                .frame(width: size * 0.44, height: size * 0.12)
                .blur(radius: size * 0.05)
                .offset(y: size * 0.31)

            if let asset {
                // 画布按图形包围盒放大并平移，而不是铺满底座——这样每枚徽章的光学大小和
                // 光学中心才一致。放大后画布本身可能超出底座，但超出的部分全是透明边，
                // 图形始终在 `glyphTarget` 之内。
                let side = asset.metrics.canvasSide(in: size, target: glyphTarget)
                let shift = asset.metrics.canvasOffset(in: size, target: glyphTarget)
                Image(nsImage: asset.image)
                    .resizable()
                    .interpolation(.high)
                    .antialiased(true)
                    .scaledToFit()
                    .frame(width: side, height: side)
                    .offset(x: shift.width, y: shift.height)
                    .saturation(active ? 0.88 : 0)
                    .contrast(active ? 1.04 : 0.82)
                    .brightness(active ? -0.015 : -0.28)
                    .opacity(active ? 1 : 0.34)
                    .shadow(
                        color: Color(red: 0.56, green: 0.82, blue: 1.0).opacity(active ? 0.22 : 0.04),
                        radius: size * 0.055
                    )
                    .shadow(color: Color.black.opacity(0.42), radius: size * 0.035, y: size * 0.025)
            } else {
                // 兜底路径的手写几何画在 0…1 的方形空间里，得收进圆的内接正方形。
                ATMAIDayComputationalObject(
                    id: badge.id,
                    level: displayLevel,
                    active: active
                )
                .padding(size * 0.16)
            }

            if size >= 140 {
                // 标本号排在圆内底部。此前它和 `FORM / 0N` 一起挂在左上、右上角——圆形底座
                // 没有角，而 `FORM / 0N` 在三个够大的调用点旁边本来就都写着（今日卡的元数据
                // 行、徽章 sheet 的「等级 L1 · 累计 N 天」、日详情的 `L1`），去掉不丢信息。
                Text(ordinal)
                    .font(.system(size: size * 0.042, weight: .medium, design: .monospaced))
                    .tracking(size * 0.004)
                    .foregroundStyle(active ? Color.white.opacity(0.34) : Color.white.opacity(0.13))
                    .frame(maxHeight: .infinity, alignment: .bottom)
                    .padding(.bottom, size * 0.10)
            }
        }
        .frame(width: size, height: size)
        // 底座在 `drawingGroup` 之内：那层离屏光栅化本来就按 frame 裁剪，徽章的投影和辉光
        // 一直是落在这个方框里的，底座正好接住它们。
        .background { if plate { ATMAIDayBadgePlate(size: size) } }
        .drawingGroup(opaque: false, colorMode: .extendedLinear)
        .accessibilityLabel("\(badge.name)，形态等级 \(displayLevel)")
    }

    private var ordinal: String {
        let ids = [
            "autopilot", "deep_collaboration", "model_conductor", "visual_director",
            "code_architect", "quality_inspector", "follow_up", "detail_microscope",
            "generalist", "hard_to_fool", "first_draft_accepted", "streak",
        ]
        return String(format: "AI–%02d", (ids.firstIndex(of: badge.id) ?? 0) + 1)
    }
}

/// 一张徽章图里**图形本身**在画布中的位置和大小。
///
/// 这是「所有图标看起来都居中」那个问题的答案。此前渲染是
/// `Image(...).scaledToFit().padding(size * 0.012)`——`scaledToFit` 对齐的是整张 362×362
/// 画布，不是画布里的图形，而那点 padding 在 94pt 上只有 1.1pt。于是每枚徽章的留白完全由
/// 它自己 PNG 带的透明边决定，实测跨度极大：`model_conductor` 的图形占画布宽 80%，
/// `streak` 只占 49%（光学大小差 1.6 倍），垂直光学中心从 −0.215 到 +0.135，跨了将近三分之
/// 一个底座高。看的人先认出「一组居中图标」，再发现每个都偏一点，读起来就是不齐。
///
/// 量出图形的实际包围盒之后，渲染改成按包围盒对齐：统一的光学直径、统一的光学中心。
struct ATMAIDayGlyphMetrics: Equatable {
    /// 图形包围盒中心，归一化到画布，**y 向下**（跟 SwiftUI 的 offset 同向）。
    let center: CGPoint
    /// 图形包围盒的长边占画布的比例。
    let extent: CGFloat

    /// 量不出来时的退路：当作图形铺满整张画布且居中，等价于归一化前的行为。
    static let untrimmed = ATMAIDayGlyphMetrics(center: CGPoint(x: 0.5, y: 0.5), extent: 1)

    /// 为了让图形的长边正好占 `target`，整张画布该画多大。
    func canvasSide(in size: CGFloat, target: CGFloat) -> CGFloat {
        // 一张几乎全透明的图会让缩放炸到无穷大，宁可原样画出来。
        guard extent > 0.01 else { return size }
        return size * target / extent
    }

    /// 把图形的光学中心搬到底座正中，画布需要的位移。
    func canvasOffset(in size: CGFloat, target: CGFloat) -> CGSize {
        let side = canvasSide(in: size, target: target)
        return CGSize(width: (0.5 - center.x) * side, height: (0.5 - center.y) * side)
    }
}

/// 徽章图与它的图形度量。度量要扫一遍 alpha（362×362），所以跟图片一起缓存，
/// 每个 id 只算一次。
final class ATMAIDayBadgeAsset {
    let image: NSImage
    let metrics: ATMAIDayGlyphMetrics

    init(image: NSImage, metrics: ATMAIDayGlyphMetrics) {
        self.image = image
        self.metrics = metrics
    }
}

enum ATMAIDayBadgeAssets {
    /// 有界（12 枚徽章）且重算不便宜，所以不用会被回收的 `NSCache`：一次驱逐就要重新
    /// 扫一遍 alpha，正好卡在滚动或切页的那一帧上。
    private static var cache: [String: ATMAIDayBadgeAsset] = [:]
    private static let lock = NSLock()

    static func asset(named name: String) -> ATMAIDayBadgeAsset? {
        lock.lock()
        defer { lock.unlock() }
        if let cached = cache[name] { return cached }
        let url = Bundle.module.url(forResource: name, withExtension: "png", subdirectory: "AIDayBadges")
            ?? Bundle.module.url(forResource: name, withExtension: "png")
        guard let url, let image = NSImage(contentsOf: url) else { return nil }
        let asset = ATMAIDayBadgeAsset(image: image, metrics: glyphMetrics(of: image) ?? .untrimmed)
        cache[name] = asset
        return asset
    }

    /// 图形包围盒 = alpha 高于阈值的最小矩形。
    ///
    /// 自己开一个 RGBA8 premultipliedLast 的位图上下文来量，而不是读 `NSBitmapImageRep`
    /// 的原始字节：后者的分量顺序和 alpha 位置随 `bitmapFormat` 变，按固定偏移读会在某些
    /// 图上量到颜色通道。位图上下文的第一行对应图像顶行，所以这里得到的 y 是向下的——
    /// `testBadgeGlyphMetricsMatchAnIndependentMeasurement` 用外部量的数拴住这个方向，
    /// 搞反了不会「少校正一点」，而是把偏移量加倍。
    static func glyphMetrics(of image: NSImage, alphaThreshold: UInt8 = 8) -> ATMAIDayGlyphMetrics? {
        guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else { return nil }
        let width = cgImage.width, height = cgImage.height
        guard width > 0, height > 0 else { return nil }
        let bytesPerRow = width * 4
        guard let context = CGContext(
            data: nil,
            width: width,
            height: height,
            bitsPerComponent: 8,
            bytesPerRow: bytesPerRow,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ) else { return nil }
        context.draw(cgImage, in: CGRect(x: 0, y: 0, width: width, height: height))
        guard let data = context.data else { return nil }
        let bytes = data.assumingMemoryBound(to: UInt8.self)

        var minX = width, minY = height, maxX = -1, maxY = -1
        for y in 0..<height {
            let row = y * bytesPerRow
            for x in 0..<width where bytes[row + x * 4 + 3] > alphaThreshold {
                if x < minX { minX = x }
                if x > maxX { maxX = x }
                if y < minY { minY = y }
                if y > maxY { maxY = y }
            }
        }
        guard maxX >= minX, maxY >= minY else { return nil }

        let canvasWidth = CGFloat(width), canvasHeight = CGFloat(height)
        let boxWidth = CGFloat(maxX - minX + 1) / canvasWidth
        let boxHeight = CGFloat(maxY - minY + 1) / canvasHeight
        return ATMAIDayGlyphMetrics(
            center: CGPoint(
                x: (CGFloat(minX) + CGFloat(maxX) + 1) / 2 / canvasWidth,
                y: (CGFloat(minY) + CGFloat(maxY) + 1) / 2 / canvasHeight
            ),
            extent: max(boxWidth, boxHeight)
        )
    }
}

private struct ATMAIDayComputationalObject: View {
    let id: String
    let level: Int
    let active: Bool

    private let cold = Color(red: 0.66, green: 0.87, blue: 1.0)
    private let coldWhite = Color(red: 0.90, green: 0.96, blue: 1.0)
    private let humanCore = Color(red: 1.0, green: 0.38, blue: 0.10)
    private let smoke = Color(red: 0.08, green: 0.11, blue: 0.15)

    var body: some View {
        Canvas { context, size in
            let geometry = ATMAIDayBadgeGeometry.make(id: id, level: level, in: size)
            let scale = min(size.width, size.height)
            let primaryWidth = max(1.2, scale * 0.016)
            let secondaryWidth = max(0.75, scale * 0.008)

            for path in geometry.volumes {
                context.fill(
                    path,
                    with: .linearGradient(
                        Gradient(colors: [
                            coldWhite.opacity(active ? 0.13 : 0.025),
                            smoke.opacity(active ? 0.80 : 0.34),
                            Color.black.opacity(active ? 0.88 : 0.58),
                        ]),
                        startPoint: .zero,
                        endPoint: CGPoint(x: size.width, y: size.height)
                    )
                )
                context.stroke(
                    path,
                    with: .color(cold.opacity(active ? 0.38 : 0.11)),
                    lineWidth: secondaryWidth
                )
            }

            for path in geometry.secondary {
                context.stroke(
                    path,
                    with: .color(cold.opacity(active ? 0.30 : 0.08)),
                    style: StrokeStyle(lineWidth: secondaryWidth, lineCap: .round, lineJoin: .round)
                )
            }

            for path in geometry.primary {
                context.stroke(
                    path,
                    with: .linearGradient(
                        Gradient(colors: [cold.opacity(0.45), coldWhite, cold.opacity(0.55)]),
                        startPoint: CGPoint(x: 0, y: size.height),
                        endPoint: CGPoint(x: size.width, y: 0)
                    ),
                    style: StrokeStyle(lineWidth: primaryWidth, lineCap: .round, lineJoin: .round)
                )
            }

            if active, let first = geometry.nodes.first {
                let glowRect = CGRect(x: first.x - scale * 0.075, y: first.y - scale * 0.075, width: scale * 0.15, height: scale * 0.15)
                context.fill(Path(ellipseIn: glowRect), with: .radialGradient(
                    Gradient(colors: [humanCore.opacity(0.58), humanCore.opacity(0)]),
                    center: first,
                    startRadius: 0,
                    endRadius: scale * 0.075
                ))
                let coreRect = CGRect(x: first.x - scale * 0.014, y: first.y - scale * 0.014, width: scale * 0.028, height: scale * 0.028)
                context.fill(Path(ellipseIn: coreRect), with: .color(humanCore))
            }

            for point in geometry.nodes.dropFirst() {
                let nodeRect = CGRect(x: point.x - scale * 0.010, y: point.y - scale * 0.010, width: scale * 0.020, height: scale * 0.020)
                context.fill(Path(ellipseIn: nodeRect), with: .color(coldWhite.opacity(active ? 0.82 : 0.16)))
            }
        }
    }
}

private struct ATMAIDayBadgeGeometry {
    var volumes: [Path] = []
    var primary: [Path] = []
    var secondary: [Path] = []
    var nodes: [CGPoint] = []

    static func make(id: String, level: Int, in size: CGSize) -> Self {
        switch id {
        case "autopilot": return autopilot(level, size)
        case "deep_collaboration": return collaboration(level, size)
        case "model_conductor": return conductor(level, size)
        case "visual_director": return visual(level, size)
        case "code_architect": return architecture(level, size)
        case "quality_inspector": return inspection(level, size)
        case "follow_up": return recursion(level, size)
        case "detail_microscope": return microscope(level, size)
        case "generalist": return generalist(level, size)
        case "hard_to_fool": return discernment(level, size)
        case "first_draft_accepted": return resolved(level, size)
        default: return streak(level, size)
        }
    }

    private static func p(_ x: Double, _ y: Double, _ s: CGSize) -> CGPoint {
        CGPoint(x: s.width * x, y: s.height * y)
    }

    private static func ellipse(_ x: Double, _ y: Double, _ w: Double, _ h: Double, _ s: CGSize) -> Path {
        Path(ellipseIn: CGRect(x: s.width*x, y: s.height*y, width: s.width*w, height: s.height*h))
    }

    private static func polygon(_ points: [(Double, Double)], _ s: CGSize) -> Path {
        var path = Path()
        for (index, point) in points.enumerated() {
            index == 0 ? path.move(to: p(point.0, point.1, s)) : path.addLine(to: p(point.0, point.1, s))
        }
        path.closeSubpath()
        return path
    }

    private static func autopilot(_ level: Int, _ s: CGSize) -> Self {
        var g = Self()
        g.volumes = [ellipse(0.59, 0.20, 0.15, 0.15, s)]
        var comet = Path(); comet.move(to:p(0.66,0.28,s)); comet.addCurve(to:p(0.22,0.77,s),control1:p(0.55,0.49,s),control2:p(0.38,0.71,s)); g.primary=[comet]
        g.nodes=[p(0.665,0.275,s)]
        if level >= 2 { g.secondary.append(ellipse(0.17,0.19,0.67,0.55,s)); g.nodes.append(p(0.24,0.34,s)) }
        if level >= 3 { g.secondary += [ellipse(0.11,0.10,0.79,0.75,s),ellipse(0.25,0.27,0.52,0.40,s)];g.nodes += [p(0.79,0.65,s),p(0.48,0.81,s)] }
        return g
    }

    private static func collaboration(_ level: Int, _ s: CGSize) -> Self {
        var g=Self();g.volumes=[ellipse(0.20,0.39,0.18,0.18,s),ellipse(0.62,0.39,0.18,0.18,s)];g.nodes=[p(0.29,0.48,s),p(0.71,0.48,s)]
        var bridge=Path();bridge.move(to:p(0.29,0.48,s));bridge.addCurve(to:p(0.71,0.48,s),control1:p(0.40,0.20,s),control2:p(0.60,0.76,s));g.primary=[bridge]
        if level>=2 { g.secondary=[ellipse(0.13,0.27,0.74,0.42,s),ellipse(0.25,0.17,0.50,0.62,s)] }
        if level>=3 { var mobius=Path();mobius.move(to:p(0.15,0.5,s));mobius.addCurve(to:p(0.85,0.5,s),control1:p(0.32,0.08,s),control2:p(0.68,0.92,s));mobius.addCurve(to:p(0.15,0.5,s),control1:p(0.68,0.08,s),control2:p(0.32,0.92,s));g.primary.append(mobius) }
        return g
    }

    private static func conductor(_ level:Int,_ s:CGSize)->Self {
        var g=Self();let prism=polygon([(0.50,0.16),(0.78,0.70),(0.22,0.70)],s);g.volumes=[prism];g.nodes=[p(0.50,0.47,s)]
        var axis=Path();axis.move(to:p(0.50,0.16,s));axis.addLine(to:p(0.50,0.70,s));g.primary=[axis]
        let rayCount=level>=3 ? 5 : level>=2 ? 3 : 1
        for i in 0..<rayCount { let y=0.36+Double(i)*0.09;var ray=Path();ray.move(to:p(0.50,y,s));ray.addLine(to:p(0.90,y-0.13+Double(i)*0.035,s));g.secondary.append(ray);g.nodes.append(p(0.90,y-0.13+Double(i)*0.035,s)) }
        if level>=3 { g.volumes.append(ellipse(0.10,0.16,0.80,0.68,s)) }
        return g
    }

    private static func streak(_ level:Int,_ s:CGSize)->Self {
        var g=Self();let count=level>=3 ? 7 : level>=2 ? 4 : 1
        let points=(0..<count).map { i -> CGPoint in let t=Double(i)/Double(max(1,count-1));return p(0.27+0.48*t,0.74-0.48*t+sin(t*Double.pi*1.5)*0.09,s) };g.nodes=points
        if let first=points.first { g.volumes=[polygon([(Double(first.x/s.width),Double(first.y/s.height)-0.08),(Double(first.x/s.width)+0.07,Double(first.y/s.height)),(Double(first.x/s.width),Double(first.y/s.height)+0.08),(Double(first.x/s.width)-0.07,Double(first.y/s.height))],s)] }
        if points.count>1 { var chain=Path();chain.move(to:points[0]);for point in points.dropFirst(){chain.addLine(to:point)};g.primary=[chain] }
        if level>=3 { var spiral=Path();spiral.addArc(center:p(0.5,0.5,s),radius:s.width*0.35,startAngle:.degrees(20),endAngle:.degrees(310),clockwise:false);g.secondary=[spiral] }
        return g
    }

    private static func visual(_ level:Int,_ s:CGSize)->Self {
        var g=Self();let blades=level>=3 ? 8 : 6;let points=(0..<blades).map{ i in let a=Double(i)/Double(blades)*2*Double.pi-Double.pi/2;return (0.5+cos(a)*0.27,0.5+sin(a)*0.27)};g.volumes=[polygon(points,s)];g.nodes=[p(0.50,0.50,s)]
        for i in 0..<blades { let a=Double(i)/Double(blades)*2*Double.pi-Double.pi/2;var blade=Path();blade.move(to:p(0.5+cos(a)*0.10,0.5+sin(a)*0.10,s));blade.addLine(to:p(0.5+cos(a)*0.27,0.5+sin(a)*0.27,s));g.primary.append(blade) }
        if level>=2 { var plane=Path();plane.move(to:p(0.18,0.77,s));plane.addLine(to:p(0.82,0.77,s));g.secondary.append(plane) }
        if level>=3 { g.volumes += [ellipse(0.15,0.18,0.34,0.25,s),ellipse(0.50,0.20,0.30,0.22,s)] }
        return g
    }

    private static func inspection(_ level:Int,_ s:CGSize)->Self {
        var g=Self();g.volumes=[polygon([(0.30,0.18),(0.73,0.18),(0.82,0.50),(0.70,0.82),(0.27,0.82),(0.18,0.50)],s)];g.nodes=[p(0.23,0.32,s)]
        var scan=Path();scan.move(to:p(0.23,0.32,s));scan.addLine(to:p(0.77,0.32,s));g.primary=[scan]
        var crack=Path();crack.move(to:p(0.56,0.25,s));crack.addLine(to:p(0.47,0.44,s));crack.addLine(to:p(0.57,0.54,s));crack.addLine(to:p(0.43,0.75,s));g.secondary=[crack]
        if level>=2 { g.primary.append(Path(ellipseIn:CGRect(x:s.width*0.28,y:s.height*0.27,width:s.width*0.44,height:s.height*0.46))) }
        if level>=3 { g.secondary += [ellipse(0.10,0.10,0.80,0.80,s),ellipse(0.21,0.21,0.58,0.58,s)] }
        return g
    }

    private static func architecture(_ level:Int,_ s:CGSize)->Self {
        var g=Self();g.volumes=[polygon([(0.18,0.69),(0.50,0.50),(0.82,0.69),(0.50,0.88)],s)];g.nodes=[p(0.50,0.50,s)]
        let towers=level>=3 ? [0.27,0.50,0.73] : level>=2 ? [0.34,0.66] : [0.50]
        for (index,x) in towers.enumerated(){let top=0.18+Double(index%2)*0.09;var tower=Path();tower.move(to:p(x,0.67,s));tower.addLine(to:p(x,top,s));tower.addLine(to:p(x+0.08,top+0.05,s));tower.addLine(to:p(x+0.08,0.62,s));g.primary.append(tower);g.nodes.append(p(x,top,s))}
        if level>=3 { g.secondary=[polygon([(0.12,0.72),(0.50,0.46),(0.88,0.72),(0.50,0.95)],s)] }
        return g
    }

    private static func recursion(_ level:Int,_ s:CGSize)->Self {
        var g=Self();g.nodes=[p(0.50,0.50,s)];let loops=level>=3 ? 3 : level>=2 ? 2 : 1
        for i in 0..<loops { let radius=s.width*(0.17+Double(i)*0.10);var arc=Path();arc.addArc(center:p(0.5,0.5,s),radius:radius,startAngle:.degrees(-55+Double(i)*25),endAngle:.degrees(235+Double(i)*35),clockwise:false);g.primary.append(arc);g.nodes.append(p(0.5+radius/s.width,0.5,s)) }
        if level>=3 { g.volumes=[ellipse(0.08,0.08,0.84,0.84,s)] }
        return g
    }

    private static func microscope(_ level:Int,_ s:CGSize)->Self {
        var g=Self();g.volumes=[ellipse(0.20,0.16,0.49,0.49,s)];g.nodes=[p(0.445,0.405,s)];var handle=Path();handle.move(to:p(0.61,0.58,s));handle.addLine(to:p(0.82,0.80,s));g.primary=[handle]
        if level>=2 { var cross=Path();cross.move(to:p(0.28,0.405,s));cross.addLine(to:p(0.61,0.405,s));cross.move(to:p(0.445,0.24,s));cross.addLine(to:p(0.445,0.57,s));g.primary.append(cross) }
        if level>=3 { g.secondary=[ellipse(0.10,0.06,0.69,0.69,s),ellipse(0.28,0.24,0.33,0.33,s)] }
        return g
    }

    private static func generalist(_ level:Int,_ s:CGSize)->Self {
        var g=Self();let poly=polygon([(0.50,0.12),(0.82,0.32),(0.75,0.76),(0.50,0.90),(0.20,0.71),(0.18,0.31)],s);g.volumes=[poly];g.nodes=[p(0.50,0.50,s)]
        for point in [(0.50,0.12),(0.82,0.32),(0.75,0.76),(0.50,0.90),(0.20,0.71),(0.18,0.31)].prefix(level>=3 ? 6 : level>=2 ? 4 : 2){var facet=Path();facet.move(to:p(0.5,0.5,s));facet.addLine(to:p(point.0,point.1,s));g.primary.append(facet);g.nodes.append(p(point.0,point.1,s))}
        if level>=3 { g.secondary=[polygon([(0.50,0.06),(0.91,0.29),(0.82,0.83),(0.49,0.96),(0.10,0.75),(0.09,0.23)],s)] }
        return g
    }

    private static func discernment(_ level:Int,_ s:CGSize)->Self {
        var g=Self();g.nodes=[p(0.50,0.50,s)];var field=Path();field.move(to:p(0.50,0.14,s));field.addCurve(to:p(0.50,0.86,s),control1:p(0.80,0.31,s),control2:p(0.80,0.69,s));field.addCurve(to:p(0.50,0.14,s),control1:p(0.20,0.69,s),control2:p(0.20,0.31,s));g.volumes=[field]
        let rays=level>=3 ? [0.30,0.42,0.58,0.70] : level>=2 ? [0.38,0.62] : [0.50]
        for y in rays { var ray=Path();ray.move(to:p(0.08,y,s));ray.addLine(to:p(0.43,y,s));ray.move(to:p(0.57,y,s));ray.addLine(to:p(0.90,y+(y-0.5)*0.35,s));g.primary.append(ray) }
        if level>=3 { g.secondary=[ellipse(0.12,0.08,0.76,0.84,s)] }
        return g
    }

    private static func resolved(_ level:Int,_ s:CGSize)->Self {
        var g=Self();let crystal=polygon([(0.50,0.16),(0.72,0.40),(0.61,0.78),(0.39,0.78),(0.28,0.40)],s);g.volumes=[crystal];g.nodes=[p(0.50,0.47,s)]
        var check=Path();check.move(to:p(0.34,0.49,s));check.addLine(to:p(0.46,0.61,s));check.addLine(to:p(0.68,0.35,s));g.primary=[check]
        if level>=2 { for x in [0.28,0.50,0.72] { var ray=Path();ray.move(to:p(0.50,0.47,s));ray.addLine(to:p(x,0.86,s));g.secondary.append(ray) } }
        if level>=3 { g.volumes.append(polygon([(0.50,0.06),(0.83,0.34),(0.70,0.88),(0.30,0.88),(0.17,0.34)],s)) }
        return g
    }
}
