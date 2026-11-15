import ActivityKit
import Foundation

/// 连接状态的实时活动：锁屏横幅和灵动岛共用这一份数据。
/// 状态文案由主 App 按当前语言写进来；网速由一直在运行的 VPN 扩展刷新。
struct XDialConnectionAttributes: ActivityAttributes {
    enum Phase: String, Codable, Hashable, Sendable {
        /// 正在连接、检查线路或重连。
        case working
        case connected
        /// 已连接但需要处理（线路自检没过、等待授权）。
        case attention
    }

    struct ContentState: Codable, Hashable, Sendable {
        var phase: Phase
        /// 「已连接」「正在连接…」这一行。
        var title: String
        /// 当前场景名。
        var scenarioName: String
        /// 连上的时刻，用来计时；没连上时为空。
        var connectedAt: Date?
        /// 实时网速（字节/秒）；还没有采样时为空。
        var downloadBytesPerSecond: Double?
        var uploadBytesPerSecond: Double?
    }

    /// 「断开」按钮的文字。
    var disconnectTitle: String
}

/// 网速的两种写法：灵动岛收起时地方小，用「1.4M」；其它地方用「1.4 MB/s」。
enum XDialSpeedFormat {
    static func short(_ bytesPerSecond: Double?) -> String {
        guard let value = bytesPerSecond, value >= 0 else { return "—" }
        if value < 1_000 { return "\(Int(value))B" }
        if value < 1_000_000 { return "\(Int((value / 1_000).rounded()))K" }
        if value < 1_000_000_000 { return String(format: "%.1fM", value / 1_000_000) }
        return String(format: "%.1fG", value / 1_000_000_000)
    }

    static func full(_ bytesPerSecond: Double?) -> String {
        guard let value = bytesPerSecond, value >= 0 else { return "—" }
        if value < 1_000 { return "\(Int(value)) B/s" }
        if value < 1_000_000 { return String(format: "%.0f KB/s", value / 1_000) }
        if value < 1_000_000_000 { return String(format: "%.1f MB/s", value / 1_000_000) }
        return String(format: "%.1f GB/s", value / 1_000_000_000)
    }
}
