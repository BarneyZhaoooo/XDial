import ActivityKit
import AppIntents
import SwiftUI
import WidgetKit

/// 连接状态的实时活动：锁屏横幅 + 灵动岛。
struct ConnectionLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: XDialConnectionAttributes.self) { context in
            LockScreenView(attributes: context.attributes, state: context.state)
                .activityBackgroundTint(Color.black.opacity(0.55))
                .activitySystemActionForegroundColor(.white)
        } dynamicIsland: { context in
            let tint = Self.tint(for: context.state.phase)
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    DialGlyph(filled: context.state.phase != .working, color: tint)
                        .frame(width: 44, height: 44)
                        .padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(context.state.title)
                            .font(.headline)
                            .lineLimit(1)
                        Text(context.state.scenarioName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    ElapsedText(connectedAt: context.state.connectedAt)
                        .font(.system(.title3, design: .rounded).monospacedDigit())
                        .foregroundStyle(tint)
                        .frame(maxHeight: .infinity)
                        .padding(.trailing, 4)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(spacing: 10) {
                        if context.state.connectedAt != nil {
                            SpeedRow(state: context.state, tint: tint)
                        }
                        DisconnectButton(title: context.attributes.disconnectTitle)
                    }
                }
            } compactLeading: {
                // 收起时只放网速：左边下载，右边上传，各看各的。哪一侧没有速度可显示就空着不占地方，
                // 两侧都没有时灵动岛就是原样。
                if let speed = context.state.compactDownloadSpeed {
                    CompactSpeed(symbol: "arrow.down", value: speed, tint: tint)
                }
            } compactTrailing: {
                if let speed = context.state.compactUploadSpeed {
                    CompactSpeed(symbol: "arrow.up", value: speed, tint: tint)
                }
            } minimal: {
                DialGlyph(filled: context.state.phase != .working, color: tint)
                    .frame(width: 20, height: 20)
            }
            .keylineTint(tint)
        }
    }

    static func tint(for phase: XDialConnectionAttributes.Phase) -> Color {
        switch phase {
        case .working: return Color(red: 0.64, green: 0.70, blue: 0.74)
        case .connected: return Color(red: 0.44, green: 0.80, blue: 0.61)
        case .attention: return Color(red: 0.88, green: 0.70, blue: 0.40)
        }
    }
}

private struct LockScreenView: View {
    let attributes: XDialConnectionAttributes
    let state: XDialConnectionAttributes.ContentState

    var body: some View {
        let tint = ConnectionLiveActivity.tint(for: state.phase)
        HStack(spacing: 14) {
            DialGlyph(filled: state.phase != .working, color: tint)
                .frame(width: 44, height: 44)

            VStack(alignment: .leading, spacing: 2) {
                Text(state.title)
                    .font(.headline)
                    .foregroundStyle(.white)
                    .lineLimit(1)
                Text(state.scenarioName)
                    .font(.subheadline)
                    .foregroundStyle(.white.opacity(0.7))
                    .lineLimit(1)
            }

            Spacer(minLength: 8)

            VStack(alignment: .trailing, spacing: 4) {
                ElapsedText(connectedAt: state.connectedAt)
                    .font(.system(.title3, design: .rounded).monospacedDigit())
                    .foregroundStyle(tint)
                if state.connectedAt != nil {
                    HStack(spacing: 10) {
                        SpeedLabel(symbol: "arrow.down", value: state.downloadBytesPerSecond)
                        SpeedLabel(symbol: "arrow.up", value: state.uploadBytesPerSecond)
                    }
                    .foregroundStyle(.white.opacity(0.75))
                }
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 16)
    }
}

/// 连上以后的计时。系统自己每秒刷新，不需要 App 在后台更新。
private struct ElapsedText: View {
    let connectedAt: Date?

    var body: some View {
        if let connectedAt {
            Text(timerInterval: connectedAt...Date.distantFuture, countsDown: false)
                .multilineTextAlignment(.trailing)
        } else {
            Text("…")
        }
    }
}

private struct DisconnectButton: View {
    let title: String

    var body: some View {
        Button(intent: Self.intent) {
            Label(title, systemImage: "power")
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .tint(.white)
    }

    private static var intent: SetXDialConnectionIntent {
        let intent = SetXDialConnectionIntent()
        intent.value = false
        return intent
    }
}

/// 灵动岛收起时的一侧网速。
/// 灵动岛会按这里内容的宽度伸缩，所以这里既要放得下，也不能比以前更占地方：
/// - 横着的灵动岛：箭头在左、数字在右，宽度跟着数字走；`short` 最多四个字符，最宽也比以前窄。
/// - 竖着的灵动岛（iPhone Duo 等）每侧只有一枚胶囊宽，箭头放旁边连「12K」都放不下，
///   会被截成「…」；这时只留数字，上面是下载、下面是上传，和横着时左下载、右上传对应，
///   一行字的高度也不比以前高。
/// 排法拿最宽的写法去试，只取决于灵动岛的形状，不会因为网速变化而换排法或字号。
private struct CompactSpeed: View {
    let symbol: String
    let value: Double
    let tint: Color

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 2) {
                Image(systemName: symbol)
                    .font(.system(size: 9, weight: .bold))
                number(size: 12)
            }
            number(size: 11)
            number(size: 10)
        }
        .foregroundStyle(tint)
    }

    private func number(size: CGFloat) -> some View {
        let font = Font.system(size: size, weight: .semibold, design: .rounded).monospacedDigit()
        return WidestDecidesFit {
            Text(XDialSpeedFormat.widestShort).font(font).hidden()
            Text(XDialSpeedFormat.short(value))
                .font(font)
                .contentTransition(.numericText())
        }
        .lineLimit(1)
    }
}

/// 第一个子视图是最宽写法的占位，第二个是实际内容。
/// 判断放不放得下（理想宽度）时按占位算，真正排版时按实际内容的大小来，
/// 这样排法稳定，占的地方又不会比实际内容大。
private struct WidestDecidesFit: Layout {
    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard subviews.count == 2 else { return .zero }
        let content = subviews[1].sizeThatFits(proposal)
        guard proposal.width == nil else { return content }
        let widest = subviews[0].sizeThatFits(.unspecified)
        return CGSize(width: max(widest.width, content.width), height: content.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let origin = CGPoint(x: bounds.minX, y: bounds.midY)
        for subview in subviews {
            subview.place(at: origin, anchor: .leading, proposal: ProposedViewSize(bounds.size))
        }
    }
}

/// 灵动岛展开后的一行网速：下载、上传各占一半。
private struct SpeedRow: View {
    let state: XDialConnectionAttributes.ContentState
    let tint: Color

    var body: some View {
        HStack(spacing: 0) {
            SpeedLabel(symbol: "arrow.down", value: state.downloadBytesPerSecond, tint: tint)
                .frame(maxWidth: .infinity)
            SpeedLabel(symbol: "arrow.up", value: state.uploadBytesPerSecond, tint: tint)
                .frame(maxWidth: .infinity)
        }
        .font(.subheadline)
    }
}

private struct SpeedLabel: View {
    let symbol: String
    let value: Double?
    var tint: Color?

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: symbol)
                .font(.caption2.weight(.bold))
                .foregroundStyle(tint ?? .primary)
            Text(XDialSpeedFormat.full(value))
                .font(.system(.footnote, design: .rounded).monospacedDigit())
                .contentTransition(.numericText())
        }
    }
}
