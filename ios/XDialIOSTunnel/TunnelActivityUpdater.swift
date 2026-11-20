import ActivityKit

final class TunnelActivityUpdater: Sendable {
    /// 扩展在后台换了场景：实时活动上的场景名跟着换，不用等 App 回到前台。
    func updateScenarioName(_ name: String) {
        Task.detached(priority: .utility) {
            for activity in Activity<XDialConnectionAttributes>.activities {
                var state = activity.content.state
                guard state.scenarioName != name else { continue }
                state.scenarioName = name
                await activity.update(ActivityContent(state: state, staleDate: state.speedExpiresAt))
            }
        }
    }

    /// 连接结束：把实时活动收掉。
    func endActivities() {
        Task.detached(priority: .utility) {
            for activity in Activity<XDialConnectionAttributes>.activities {
                await activity.end(nil, dismissalPolicy: .immediate)
            }
        }
    }
}
