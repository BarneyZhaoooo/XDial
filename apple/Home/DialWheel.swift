import Observation
import QuartzCore
#if os(iOS)
import UIKit
#else
import AppKit
#endif

/// 拨号盘转盘的运动：手指拖着转、松手后匀速回弹、连接时自动「拨号」。
/// 只有读了 rotation / activeHole 的那一层视图会跟着重绘。
@Observable
@MainActor
final class DialWheel {
    /// 转盘顺时针转过的角度，0 是静止位置。
    private(set) var rotation: Double = 0
    /// 手指（或自动拨号时假想的手指）正按着的孔。
    private(set) var activeHole: Int?

    @ObservationIgnored private var travels: [Double] = []
    @ObservationIgnored private var mode: Mode = .rest
    @ObservationIgnored private var link: CADisplayLink?
    @ObservationIgnored private var proxy: LinkProxy?
    @ObservationIgnored private var atStop = false
    #if os(iOS)
    @ObservationIgnored private let tickHaptic = UIImpactFeedbackGenerator(style: .light)
    @ObservationIgnored private let stopHaptic = UIImpactFeedbackGenerator(style: .rigid)
    @ObservationIgnored private let restHaptic = UIImpactFeedbackGenerator(style: .soft)
    #endif

    /// 回弹是匀速的（真机里靠调速器），每转过一格响一下。
    private static let returnSpeed: Double = 330
    private static let pulseStep: Double = 30
    /// 自动拨号时依次拨的孔，长短错开，看起来像在拨一串号码。
    private static let autoSequence = [1, 0, 3, 2, 0, 4]

    private enum Mode {
        case rest
        case dragging(hole: Int)
        /// 松手后的回弹；manual 时带触感。
        case returning(manual: Bool)
        case auto(AutoState)
    }

    private struct AutoState {
        var step = 0
        var stage: Stage = .gap
        var elapsed: Double = 0
        var target: Double = 0

        enum Stage { case pull, hold, back, gap }
    }

    func configure(layout: DialLayout) {
        travels = layout.holeAngles.indices.map { layout.travel(forHole: $0) }
    }

    var isAutoDialing: Bool {
        if case .auto = mode { return true }
        return false
    }

    // MARK: - 手指拨号

    func beginDrag(hole: Int) {
        guard travels.indices.contains(hole) else { return }
        switch mode {
        case .rest, .returning: break
        case .dragging, .auto: return
        }
        mode = .dragging(hole: hole)
        activeHole = hole
        atStop = false
        stopLink()
        #if os(iOS)
        tickHaptic.prepare()
        stopHaptic.prepare()
        #endif
    }

    func drag(by delta: Double) {
        guard case .dragging(let hole) = mode else { return }
        let limit = travels[hole]
        let next = min(max(rotation + delta, 0), limit)
        if Int(next / Self.pulseStep) != Int(rotation / Self.pulseStep) {
            #if os(iOS)
            tickHaptic.impactOccurred(intensity: 0.45)
            #endif
        }
        if next >= limit, !atStop {
            atStop = true
            #if os(iOS)
            stopHaptic.impactOccurred(intensity: 1)
            #endif
        } else if next < limit - 4 {
            atStop = false
        }
        rotation = next
    }

    func endDrag() {
        guard case .dragging = mode else { return }
        activeHole = nil
        if rotation > 0 {
            mode = .returning(manual: true)
            startLink()
        } else {
            mode = .rest
        }
    }

    // MARK: - 自动拨号

    func startAutoDial() {
        guard !travels.isEmpty, !isAutoDialing else { return }
        activeHole = nil
        // 手指刚拨到一半就开始连接：先让转盘转回去，再开始自动拨号。
        var state = AutoState()
        if rotation > 0 { state.stage = .back }
        mode = .auto(state)
        startLink()
    }

    /// 连接有了结果：手松开，转盘从当前位置转回去。
    func stopAutoDial() {
        guard isAutoDialing else { return }
        activeHole = nil
        mode = rotation > 0 ? .returning(manual: false) : .rest
        if rotation > 0 { startLink() } else { stopLink() }
    }

    // MARK: - 每帧

    fileprivate func step(dt: Double) {
        switch mode {
        case .rest, .dragging:
            stopLink()
        case .returning(let manual):
            if unwind(dt: dt, haptics: manual) {
                mode = .rest
                stopLink()
                #if os(iOS)
                if manual { restHaptic.impactOccurred(intensity: 0.7) }
                #endif
            }
        case .auto(var state):
            advance(&state, dt: dt)
            if isAutoDialing { mode = .auto(state) }
        }
    }

    /// 往回转一帧；转到头返回 true。
    private func unwind(dt: Double, haptics: Bool) -> Bool {
        let next = max(0, rotation - Self.returnSpeed * dt)
        if haptics, Int(next / Self.pulseStep) != Int(rotation / Self.pulseStep) {
            #if os(iOS)
            tickHaptic.impactOccurred(intensity: 0.6)
            #endif
        }
        rotation = next
        return next <= 0
    }

    private func advance(_ state: inout AutoState, dt: Double) {
        state.elapsed += dt
        switch state.stage {
        case .gap:
            guard state.elapsed >= 0.16 else { return }
            let hole = Self.autoSequence[state.step % Self.autoSequence.count] % travels.count
            state.step += 1
            state.target = travels[hole]
            state.elapsed = 0
            state.stage = .pull
            activeHole = hole
        case .pull:
            // 手指拨过去：起步和收尾慢，中间快。
            let duration = 0.22 + state.target / 520
            let progress = min(1, state.elapsed / duration)
            rotation = state.target * progress * progress * (3 - 2 * progress)
            if progress >= 1 {
                state.elapsed = 0
                state.stage = .hold
            }
        case .hold:
            guard state.elapsed >= 0.1 else { return }
            activeHole = nil
            state.stage = .back
        case .back:
            if unwind(dt: dt, haptics: false) {
                state.elapsed = 0
                state.stage = .gap
            }
        }
    }

    // MARK: - 帧驱动

    private func startLink() {
        guard link == nil else { return }
        let proxy = LinkProxy(owner: self)
        #if os(macOS)
        guard let link = NSScreen.main?.displayLink(target: proxy, selector: #selector(LinkProxy.tick(_:))) else { return }
        #else
        let link = CADisplayLink(target: proxy, selector: #selector(LinkProxy.tick(_:)))
        #endif
        link.add(to: .main, forMode: .common)
        self.proxy = proxy
        self.link = link
    }

    private func stopLink() {
        link?.invalidate()
        link = nil
        proxy = nil
    }

    /// CADisplayLink 会强引用 target，用一个中间对象转一下，转盘释放后自己停掉。
    @MainActor
    private final class LinkProxy: NSObject {
        weak var owner: DialWheel?

        init(owner: DialWheel) {
            self.owner = owner
        }

        @objc func tick(_ link: CADisplayLink) {
            guard let owner else {
                link.invalidate()
                return
            }
            owner.step(dt: min(max(link.targetTimestamp - link.timestamp, 1.0 / 240), 1.0 / 20))
        }
    }
}
