import SwiftUI

/// 拨号盘的排布：六个指孔从上方逆时针排到右下角，指停器紧挨着最后一个孔，
/// 缺口留在右侧——和真的拨号盘一样。孔与孔的间隙、孔与指停器的间隙是同一个数。
/// App 里的大拨盘和小尺寸的 DialGlyph 共用这一套数。
/// 单位是 16 网格（盘面半径 6.6），角度是屏幕坐标（0° 朝右，顺时针为正）。
struct DialLayout {
    static let discRadius: CGFloat = 6.6
    static let holeCount = 6
    /// 相邻两个孔圆心之间的直线距离；孔变小，孔位不动，空隙跟着变大。
    static let holePitch: CGFloat = 3.39

    let holeOrbit: CGFloat = 4.65
    let holeRadius: CGFloat
    let hubRadius: CGFloat = 2.4
    /// 指停器：一根圆头杠，里端离圆心的距离固定，外端快到盘沿。
    let stopAngle: Double = 47.5
    let stopWidth: CGFloat
    let stopInnerEdge: CGFloat = 3.43
    let stopOuterEdge: CGFloat = 6.25

    let holeAngles: [Double]

    init(holeRadius: CGFloat, stopWidth: CGFloat) {
        self.holeRadius = holeRadius
        self.stopWidth = stopWidth
        // 最后一个孔和指停器之间的空隙，等于孔与孔之间的空隙。
        let gap = Self.holePitch - 2 * holeRadius
        let spacing = 2 * asin(Double(Self.holePitch / (2 * holeOrbit))) * 180 / .pi
        let stopOffset = asin(Double((holeRadius + stopWidth / 2 + gap) / holeOrbit)) * 180 / .pi
        let last = stopAngle + stopOffset
        holeAngles = (0..<Self.holeCount).map { last + spacing * Double(Self.holeCount - 1 - $0) }
    }

    static let standard = DialLayout(holeRadius: 1.1, stopWidth: 1.2)

    func powerSymbolSize(diameter: CGFloat) -> CGFloat {
        min(diameter * hubRadius / Self.discRadius * 0.44, diameter * 0.15)
    }

    static func ringInset(diameter: CGFloat) -> CGFloat { max(9, diameter * 0.055) }
    static func ringWidth(diameter: CGFloat) -> CGFloat { max(2.5, diameter * 0.014) }

    /// 把第 index 个孔顺时针拨到指停器要转多少度。离指停器最远的第一个孔转得最少，
    /// 紧挨着指停器的最后一个孔要绕一大圈——和真的拨号盘一样。
    func travel(forHole index: Int) -> Double {
        let touch = asin(Double((holeRadius + stopWidth / 2 + 0.12) / holeOrbit)) * 180 / .pi
        var travel = (stopAngle - touch - holeAngles[index]).truncatingRemainder(dividingBy: 360)
        if travel < 0 { travel += 360 }
        return travel
    }
}

/// 小尺寸的品牌拨号盘：灵动岛、锁屏横幅里用。排布与 App 里的大拨盘一致。
struct DialGlyph: View {
    var filled: Bool
    var color: Color
    private let layout = DialLayout.standard

    var body: some View {
        Canvas { context, size in
            let side = min(size.width, size.height)
            let scale = side / (DialLayout.discRadius * 2)
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            // 空心盘外面有一圈环，指孔和指停器整体往里收一点，免得压到环上。
            let inner: CGFloat = filled ? 1 : 0.8
            func point(angle: Double, radius: CGFloat) -> CGPoint {
                let radians = angle * .pi / 180
                return CGPoint(x: center.x + cos(radians) * radius * inner * scale,
                               y: center.y + sin(radians) * radius * inner * scale)
            }
            var marks = Path()
            for angle in layout.holeAngles {
                let c = point(angle: angle, radius: layout.holeOrbit)
                let r = layout.holeRadius * inner * scale
                marks.addEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: r * 2, height: r * 2))
            }
            // 小尺寸下指停器略加粗，否则在灵动岛里看不见。
            let stopWidth = layout.stopWidth * 1.15
            var stop = Path()
            stop.move(to: point(angle: layout.stopAngle, radius: layout.stopInnerEdge + stopWidth / 2))
            stop.addLine(to: point(angle: layout.stopAngle, radius: layout.stopOuterEdge - stopWidth / 2))
            let stopStyle = StrokeStyle(lineWidth: stopWidth * inner * scale, lineCap: .round)
            let disc = Path(ellipseIn: CGRect(x: center.x - side / 2, y: center.y - side / 2, width: side, height: side))

            if filled {
                // 实心盘，指孔和指停器镂空。
                context.drawLayer { layer in
                    layer.fill(disc, with: .color(color))
                    layer.blendMode = .destinationOut
                    layer.fill(marks, with: .color(.black))
                    layer.stroke(stop, with: .color(.black), style: stopStyle)
                }
            } else {
                // 空心盘：一圈外环加实心指孔和指停器。
                let inset = 0.6 * scale
                context.stroke(
                    Path(ellipseIn: CGRect(x: center.x - side / 2, y: center.y - side / 2, width: side, height: side)
                        .insetBy(dx: inset, dy: inset)),
                    with: .color(color),
                    lineWidth: 1.2 * scale
                )
                context.fill(marks, with: .color(color))
                context.stroke(stop, with: .color(color), style: stopStyle)
            }
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }
}
