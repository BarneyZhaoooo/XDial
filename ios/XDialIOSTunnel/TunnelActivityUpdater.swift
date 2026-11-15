import ActivityKit
import Foundation

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
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                guard !Task.isCancelled else { return }
                guard let json = readTotals(),
                      let totals = try? JSONDecoder().decode(Totals.self, from: Data(json.utf8)) else {
                    previous = nil
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
                if let lastShown, lastShown == shown { continue }
                lastShown = shown

                for activity in Activity<XDialConnectionAttributes>.activities {
                    var state = activity.content.state
                    guard state.connectedAt != nil else { continue }
                    state.downloadBytesPerSecond = down
                    state.uploadBytesPerSecond = up
                    await activity.update(ActivityContent(state: state, staleDate: nil))
                }
            }
        }
        lock.lock()
        let old = task
        task = newTask
        lock.unlock()
        old?.cancel()
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
