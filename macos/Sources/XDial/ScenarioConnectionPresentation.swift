import Foundation

/// Reads the committed transaction, never the editable Profile or the selected
/// scenario. A selected scenario and a prepared candidate are not a VPN login.
enum ScenarioConnectionPresentation: Equatable {
    case inactive, preparing, direct, vpn, routed, unconfirmed

    static func resolve(connected: Bool, busy: Bool, report: ConnectionReport?) -> Self {
        guard connected else { return busy ? .preparing : .inactive }
        guard let report, report.state == .committed, report.error == nil else { return .unconfirmed }
        let targets = report.tasks.filter { $0.kind == "line" || $0.kind == "subscription" }
        guard !targets.isEmpty, targets.allSatisfy({
            ($0.state == .ready || $0.state == .committed) && $0.error == nil
        }) else { return .unconfirmed }
        if targets.allSatisfy({ $0.kind == "line" && $0.resourceType == "direct" }) { return .direct }
        if targets.contains(where: { $0.kind == "line" && $0.resourceType == "vpn" }) { return .vpn }
        // Groups, proxies, subscriptions and older reports must not be called
        // either a VPN connection or direct merely because their type is unknown.
        return .routed
    }

    func title(_ tr: (String, String) -> String) -> String {
        switch self {
        case .inactive: return tr("XDial 未启用", "XDial is inactive")
        case .preparing: return tr("正在准备线路…", "Preparing routes…")
        case .direct: return tr("直连运行中 · 未建立 VPN", "Direct routing · No XDial VPN")
        case .vpn: return tr("VPN 线路已连接", "VPN line connected")
        case .routed: return tr("分流运行中", "Routing is active")
        case .unconfirmed: return tr("正在确认线路状态", "Checking line status")
        }
    }

    func detail(_ tr: (String, String) -> String) -> String {
        switch self {
        case .inactive: return tr("沿用原网络。选择场景并启用后，XDial 才会处理流量。", "Using your existing network. Activate a scenario to let XDial handle traffic.")
        case .preparing: return tr("尚未完成启用；请等待线路准备和网络接管完成。", "Activation is not complete. Waiting for routes and network setup.")
        case .direct: return tr("XDial 沿用原网络，不通过 XDial VPN，也不进行 VPN 账号认证。", "XDial uses your existing network without an XDial VPN or VPN sign-in.")
        case .vpn: return tr("仅按运行场景的规则使用 VPN；不代表所有流量都走 VPN。", "VPN use follows the running scenario's rules, not necessarily all traffic.")
        case .routed: return tr("流量按运行场景处理；场景启用不等于 VPN 已连接。", "Traffic follows the running scenario. An active scenario does not imply a VPN connection.")
        case .unconfirmed: return tr("尚无完整的已提交线路状态，请查看下方连接详情。", "Complete committed line status is unavailable. See connection details below.")
        }
    }
}
