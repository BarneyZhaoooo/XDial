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
private struct CompactSpeed: View {
    let symbol: String
    let value: Double
    let tint: Color

    var body: some View {
        HStack(spacing: 2) {
            Image(systemName: symbol)
                .font(.system(size: 9, weight: .bold))
            Text(XDialSpeedFormat.short(value))
                .font(.system(.caption, design: .rounded).monospacedDigit().weight(.semibold))
                .contentTransition(.numericText())
        }
        .foregroundStyle(tint)
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
