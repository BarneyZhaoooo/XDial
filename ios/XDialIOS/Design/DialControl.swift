import SwiftUI

/// 拨号盘的四种样子，对应连接的四个阶段。
enum DialPhase: Equatable {
    /// 未连接：盘面在，指孔是暗的。
    case idle
    /// 连接、断开、自检：指孔依次点亮，外圈有一段光弧在走。
    case working
    /// 已连接：盘面染成状态色，指孔全亮。
    case connected
    /// 已连接但需要处理（自检未过、等待授权）。
    case attention
}

/// 品牌拨号盘做成的连接开关：一圈状态环、一排指孔、紧挨着最后一个孔的细长指停器，中间是电源按钮。
struct DialControl: View {
    let phase: DialPhase
    /// 不可用时变淡，但仍可点（用来引导去登录）。
    var isDimmed = false
    /// 盘面直径；状态环在盘面外面再多出一圈。
    var diameter: CGFloat = 216
    var layout: DialLayout = .standard
    let action: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var wheel = DialWheel()
    /// 手指在拨盘上拖动时为 true；手势结束或被系统打断都会自动回到 false。
    @GestureState private var isDialing = false
    @State private var lastTouchAngle: Double?

    private var ringInset: CGFloat { max(9, diameter * 0.055) }
    private var outerDiameter: CGFloat { diameter + ringInset * 2 }
    private var scale: CGFloat { diameter / (DialLayout.discRadius * 2) }

    var body: some View {
        Button(action: action) {
            ZStack {
                halo
                TimelineView(.animation(minimumInterval: 1 / 30, paused: phase != .working)) { context in
                    let time = context.date.timeIntervalSinceReferenceDate
                    ZStack {
                        statusRing(time: time)
                        disc
                    }
                }
                DialHoles(
                    layout: layout,
                    wheel: wheel,
                    isLit: isLit,
                    stateColor: stateColor,
                    scale: scale,
                    side: outerDiameter
                )
                fingerStop
                hub
            }
            .frame(width: outerDiameter, height: outerDiameter)
            .contentShape(Circle())
        }
        .buttonStyle(DialPressStyle())
        // 未连接和已连接时都可以用手指拨着玩：拖动超过几个点就算拨号，不会触发连接或断开；
        // 轻点仍然是开关。连接、断开过程中转盘在自己拨号，不接手势。
        .highPriorityGesture(dialDrag, including: phase == .working ? .subviews : .all)
        .opacity(isDimmed ? 0.55 : 1)
        .animation(.smooth(duration: 0.45), value: phase)
        .sensoryFeedback(.impact(weight: .medium), trigger: phase == .working)
        .sensoryFeedback(.success, trigger: phase == .connected) { _, isConnected in isConnected }
        .onAppear {
            wheel.configure(layout: layout)
            syncAutoDial()
        }
        .onChange(of: phase) { syncAutoDial() }
        .onChange(of: isDialing) { _, dialing in
            if !dialing {
                lastTouchAngle = nil
                wheel.endDrag()
            }
        }
    }

    /// 连接、断开过程中转盘自己拨号；有了结果就松手转回去。
    private func syncAutoDial() {
        if phase == .working, !reduceMotion {
            wheel.endDrag()
            wheel.startAutoDial()
        } else {
            wheel.stopAutoDial()
        }
    }

    // MARK: - 手指拨号

    private var dialDrag: some Gesture {
        DragGesture(minimumDistance: 8)
            .updating($isDialing) { _, state, _ in state = true }
            .onChanged { value in
                let center = CGPoint(x: outerDiameter / 2, y: outerDiameter / 2)
                func angle(_ point: CGPoint) -> Double {
                    atan2(Double(point.y - center.y), Double(point.x - center.x)) * 180 / .pi
                }
                guard let last = lastTouchAngle else {
                    // 起手要落在某个指孔上才算数。
                    guard let hole = holeIndex(at: value.startLocation) else { return }
                    wheel.beginDrag(hole: hole)
                    lastTouchAngle = angle(value.startLocation)
                    return
                }
                let now = angle(value.location)
                lastTouchAngle = now
                // 手指划过圆心附近时角度会乱跳，这一段不算。
                let reach = hypot(value.location.x - center.x, value.location.y - center.y)
                guard reach > layout.hubRadius * 0.5 * scale else { return }
                var delta = now - last
                if delta > 180 { delta -= 360 } else if delta < -180 { delta += 360 }
                wheel.drag(by: delta)
            }
    }

    private func holeIndex(at point: CGPoint) -> Int? {
        let reach = layout.holeRadius * 1.5 * scale
        var best: (index: Int, distance: CGFloat)?
        for (index, angle) in layout.holeAngles.enumerated() {
            let center = position(angle: angle + wheel.rotation, radius: layout.holeOrbit)
            let distance = hypot(point.x - center.x, point.y - center.y)
            if distance <= reach, distance < (best?.distance ?? .infinity) {
                best = (index, distance)
            }
        }
        return best?.index
    }

    // MARK: - 颜色

    private var isLit: Bool { phase == .connected || phase == .attention }

    private var stateColor: Color {
        switch phase {
        case .idle, .working: return XDialTheme.accent
        case .connected: return XDialTheme.success
        case .attention: return XDialTheme.warning
        }
    }

    private func position(angle: Double, radius: CGFloat) -> CGPoint {
        let radians = angle * .pi / 180
        return CGPoint(
            x: outerDiameter / 2 + cos(radians) * radius * scale,
            y: outerDiameter / 2 + sin(radians) * radius * scale
        )
    }

    // MARK: - 各层

    private var halo: some View {
        Circle()
            .fill(stateColor.opacity(phase == .idle ? 0.10 : (phase == .working ? 0.20 : 0.30)))
            .frame(width: diameter * 1.42, height: diameter * 1.42)
            .blur(radius: diameter * 0.16)
            .allowsHitTesting(false)
    }

    /// 盘面外的一圈细环：未连接是一道淡线，连接中有光弧绕圈，连上后整圈点亮。
    private func statusRing(time: TimeInterval) -> some View {
        let lineWidth = max(2.5, diameter * 0.014)
        return ZStack {
            Circle()
                .stroke(isLit ? stateColor : XDialTheme.hairline.opacity(0.9), lineWidth: lineWidth)
                .shadow(color: isLit ? stateColor.opacity(0.5) : .clear, radius: 6)
            if phase == .working {
                Circle()
                    .trim(from: 0, to: 0.26)
                    .stroke(
                        AngularGradient(
                            colors: [stateColor.opacity(0), stateColor],
                            center: .center,
                            startAngle: .degrees(0),
                            endAngle: .degrees(94)
                        ),
                        style: StrokeStyle(lineWidth: lineWidth, lineCap: .round)
                    )
                    .rotationEffect(.degrees(reduceMotion ? -90 : time.truncatingRemainder(dividingBy: 1.6) / 1.6 * 360))
            }
        }
        .frame(width: outerDiameter - lineWidth, height: outerDiameter - lineWidth)
    }

    @ViewBuilder
    private var disc: some View {
        if #available(iOS 26.0, *) {
            // 液态玻璃盘面：未连接时是透明玻璃，连上后染成状态色。
            Circle()
                .fill(.clear)
                .frame(width: diameter, height: diameter)
                .glassEffect(
                    XDialGlass.make(tint: isLit ? stateColor : nil, interactive: true),
                    in: Circle()
                )
        } else {
            Circle()
                .fill(isLit ? stateColor : XDialTheme.dialFace)
                .frame(width: diameter, height: diameter)
                .shadow(color: .black.opacity(0.10), radius: 22, y: 12)
                .shadow(color: .black.opacity(0.05), radius: 3, y: 1)
        }
    }

    private var fingerStop: some View {
        let width = layout.stopWidth
        let bar = Path { path in
            path.move(to: position(angle: layout.stopAngle, radius: layout.stopInnerEdge + width / 2))
            path.addLine(to: position(angle: layout.stopAngle, radius: layout.stopOuterEdge - width / 2))
        }
        return ZStack {
            // 已连接时指孔和指停器都是白的；转盘转动、孔从指停器下面经过时，
            // 靠这一圈和盘面同色的描边把两者分开。
            bar.stroke(
                stateColor,
                style: StrokeStyle(lineWidth: width * scale + 5, lineCap: .round)
            )
            .opacity(isLit ? 1 : 0)
            bar.stroke(
                isLit ? Color.white.opacity(0.94) : XDialTheme.dialInk.opacity(0.40),
                style: StrokeStyle(lineWidth: width * scale, lineCap: .round)
            )
            .shadow(color: .black.opacity(isLit ? 0.12 : 0.10), radius: 1.5, y: 1)
        }
        .frame(width: outerDiameter, height: outerDiameter)
        .allowsHitTesting(false)
    }

    /// 中间微微鼓起的电源按钮。
    private var hub: some View {
        let size = layout.hubRadius * 2 * scale
        return ZStack {
            Circle()
                .fill(isLit ? Color.white.opacity(0.20) : XDialTheme.dialFace)
                .overlay {
                    Circle().strokeBorder(
                        isLit ? Color.white.opacity(0.38) : XDialTheme.hairline.opacity(0.8),
                        lineWidth: 1
                    )
                }
                .shadow(color: .black.opacity(isLit ? 0.10 : 0.14), radius: size * 0.10, y: size * 0.05)
            Image(systemName: "power")
                .font(.system(size: min(size * 0.44, diameter * 0.15), weight: .semibold))
                .foregroundStyle(isLit ? Color.white : (phase == .working ? stateColor : XDialTheme.dialInk))
        }
        .frame(width: size, height: size)
        .allowsHitTesting(false)
    }
}

private struct DialPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.955 : 1)
            .animation(.spring(response: 0.3, dampingFraction: 0.62), value: configuration.isPressed)
    }
}

/// 六个指孔。转盘转的时候只有这一层重绘；孔是整圈一起转的，但每个孔的受光方向不变。
private struct DialHoles: View {
    let layout: DialLayout
    let wheel: DialWheel
    let isLit: Bool
    let stateColor: Color
    let scale: CGFloat
    let side: CGFloat

    var body: some View {
        let size = layout.holeRadius * 2 * scale
        let rotation = wheel.rotation
        let active = wheel.activeHole
        ZStack {
            ForEach(layout.holeAngles.indices, id: \.self) { index in
                // 手指按着的那个孔：未连接时填上强调色，已连接时（孔本来是亮的）压暗一点。
                let pressed = active == index
                ZStack {
                    Circle()
                        .fill(
                            XDialTheme.dialInk.opacity(0.16)
                                .shadow(.inner(color: .black.opacity(0.28), radius: size * 0.09, y: size * 0.06))
                        )
                        .opacity(isLit ? 0 : 1)
                    Circle()
                        .fill(isLit ? Color.white.opacity(0.94) : stateColor)
                        .opacity(isLit || pressed ? 1 : 0)
                        .shadow(
                            color: (isLit ? Color.white : stateColor).opacity(isLit ? 0.35 : (pressed ? 0.45 : 0)),
                            radius: size * 0.18
                        )
                        .animation(.easeOut(duration: 0.16), value: pressed)
                    Circle()
                        .fill(stateColor.opacity(0.55))
                        .opacity(isLit && pressed ? 1 : 0)
                        .animation(.easeOut(duration: 0.16), value: pressed)
                }
                .frame(width: size, height: size)
                .position(center(angle: layout.holeAngles[index] + rotation))
            }
        }
        .frame(width: side, height: side)
        .allowsHitTesting(false)
    }

    private func center(angle: Double) -> CGPoint {
        let radians = angle * .pi / 180
        return CGPoint(
            x: side / 2 + cos(radians) * layout.holeOrbit * scale,
            y: side / 2 + sin(radians) * layout.holeOrbit * scale
        )
    }
}
