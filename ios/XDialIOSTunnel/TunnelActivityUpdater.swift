import ActivityKit
import Foundation
import os

/// 在 VPN 扩展里刷新实时活动上的网速。
///
/// 主 App 退到后台几秒后就会被系统挂起，没法再更新灵动岛；扩展在连接期间一直运行，
/// 所以由它每两秒读一次累计流量、算出速率写进实时活动。连接结束时顺手把实时活动收掉，
/// 免得 App 不在时灵动岛还停在「已连接」。
final class TunnelActivityUpdater: @unchecked Sendable {
    private let lock = NSLock()
    private var task: Task<Void, Never>?

    private struct Totals: Decodable {
        let uploadBytes: Int64
        let downloadBytes: Int64

        enum CodingKeys: String, CodingKey {
            case uploadBytes = "upload_bytes"
            case downloadBytes = "download_bytes"
        }
    }

    /// `readTotals` 返回引擎的累计流量 JSON；引擎不在运行时返回 nil。
    func start(readTotals: @escaping @Sendable () -> String?) {
        let newTask = Task.detached(priority: .utility) {
            var previous: (totals: Totals, at: Date)?
            var lastShown: (down: String, up: String)?
            var report = UpdateReport()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                guard !Task.isCancelled else { return }
                // 状态采样不依赖流量是否变化，也不依赖引擎能否提供计数。
                let activities = Activity<XDialConnectionAttributes>.activities
                report.sawActivities(activities.count)
                report.lastActivityState = activities.isEmpty ? "-" : activities
                    .map { String(describing: $0.activityState) }.sorted().joined(separator: ",")
                report.flushIfDue()
                guard let json = readTotals(),
                      let totals = try? JSONDecoder().decode(Totals.self, from: Data(json.utf8)) else {
                    previous = nil
                    report.engineUnavailable += 1
                    continue
                }
                let now = Date()
                defer { previous = (totals, now) }
                guard let last = previous,
                      totals.downloadBytes >= last.totals.downloadBytes,
                      totals.uploadBytes >= last.totals.uploadBytes else { continue }
                let elapsed = now.timeIntervalSince(last.at)
                guard elapsed > 0 else { continue }
                let down = Double(totals.downloadBytes - last.totals.downloadBytes) / elapsed
                let up = Double(totals.uploadBytes - last.totals.uploadBytes) / elapsed

                // 显示出来的数字没变就不更新，省电。
                let shown = (XDialSpeedFormat.full(down), XDialSpeedFormat.full(up))
                if let lastShown, lastShown == shown {
                    report.unchanged += 1
                    continue
                }
                lastShown = shown

                for activity in activities {
                    var state = activity.content.state
                    guard state.connectedAt != nil else {
                        report.notConnected += 1
                        continue
                    }
                    state.downloadBytesPerSecond = down
                    state.uploadBytesPerSecond = up
                    let started = Date()
                    await activity.update(ActivityContent(state: state, staleDate: nil))
                    report.sent(at: started, took: Date().timeIntervalSince(started))
                }
            }
        }
        lock.lock()
        let old = task
        task = newTask
        lock.unlock()
        old?.cancel()
    }

    /// 扩展在后台换了场景：实时活动上的场景名跟着换，不用等 App 回到前台。
    func updateScenarioName(_ name: String) {
        Task.detached(priority: .utility) {
            for activity in Activity<XDialConnectionAttributes>.activities {
                var state = activity.content.state
                guard state.scenarioName != name else { continue }
                state.scenarioName = name
                await activity.update(ActivityContent(state: state, staleDate: nil))
            }
        }
    }

    /// 连接结束：停止采样，并把实时活动收掉。
    func stopAndEndActivities() {
        lock.lock()
        let old = task
        task = nil
        lock.unlock()
        old?.cancel()
        Task.detached(priority: .utility) {
            for activity in Activity<XDialConnectionAttributes>.activities {
                await activity.end(nil, dismissalPolicy: .immediate)
            }
        }
    }
}

/// 扩展里实时活动更新的统计，每 30 秒写一条日志；扩展看得到的实时活动个数变化时立即写。
/// 后台网速常常卡住不动，靠它在真机上分清：扩展是看不到实时活动，还是更新送出去了却没显示。
/// 查看：`log stream --predicate 'subsystem == "com.xd.xdial.ios.tunnel" AND category == "live-activity"'`。
private struct UpdateReport {
    private static let log = Logger(subsystem: "com.xd.xdial.ios.tunnel", category: "live-activity")
    private static let interval: TimeInterval = 30

    var engineUnavailable = 0
    var unchanged = 0
    var notConnected = 0
    var lastActivityState = "-"
    private var ticksWithoutActivity = 0
    private var updatesSent = 0
    private var longestGap: TimeInterval = 0
    private var slowestUpdate: TimeInterval = 0
    private var lastSentAt: Date?
    private var visibleActivities: Int?
    private var windowStart = Date()

    mutating func sawActivities(_ count: Int) {
        if count == 0 { ticksWithoutActivity += 1 }
        guard count != visibleActivities else { return }
        let before = visibleActivities ?? -1
        Self.log.notice("visible activities changed: \(before, privacy: .public) -> \(count, privacy: .public)")
        visibleActivities = count
    }

    mutating func sent(at start: Date, took duration: TimeInterval) {
        updatesSent += 1
        if let lastSentAt { longestGap = max(longestGap, start.timeIntervalSince(lastSentAt)) }
        lastSentAt = start
        slowestUpdate = max(slowestUpdate, duration)
    }

    mutating func flushIfDue() {
        let now = Date()
        let seconds = Int(now.timeIntervalSince(windowStart))
        guard seconds >= Int(Self.interval) else { return }
        let (sent, unchanged, noActivity, notConnected, engineDown) =
            (updatesSent, unchanged, ticksWithoutActivity, notConnected, engineUnavailable)
        let (gap, slowest, state) = (longestGap, slowestUpdate, lastActivityState)
        Self.log.notice("""
            \(seconds, privacy: .public)s: sent=\(sent, privacy: .public) \
            unchanged=\(unchanged, privacy: .public) noActivity=\(noActivity, privacy: .public) \
            notConnected=\(notConnected, privacy: .public) engineDown=\(engineDown, privacy: .public) \
            longestGap=\(gap, format: .fixed(precision: 1), privacy: .public)s \
            slowestUpdate=\(slowest, format: .fixed(precision: 2), privacy: .public)s \
            state=\(state, privacy: .public)
            """)
        let visible = visibleActivities
        let lastSent = lastSentAt
        self = UpdateReport()
        visibleActivities = visible
        // 上一次发送属于整段连接，不能随统计窗口清空，否则长时间停更会被漏掉。
        lastSentAt = lastSent
        windowStart = now
    }
}
