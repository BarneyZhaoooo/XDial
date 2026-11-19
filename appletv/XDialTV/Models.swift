import Foundation

struct Line: Codable, Identifiable, Hashable {
    var id: String
    var name: String
    var type: String  // direct / vpn / trojan / shadowsocks / vmess / tailscale
    var enabled: Bool = true
    var verified: Bool = false

    var vpnServer: String = ""
    var vpnUsername: String = ""
    var vpnPassword: String = ""

    var trojanServer: String = ""
    var trojanPort: Int = 443
    var trojanPassword: String = ""
    var trojanSNI: String = ""

    var ssServer: String = ""
    var ssPort: Int = 8388
    var ssMethod: String = "aes-256-gcm"
    var ssPassword: String = ""

    var vmessServer: String = ""
    var vmessPort: Int = 443
    var vmessUUID: String = ""
    var vmessAltID: Int = 0

    // Tailscale 身份（设备名、登录态）是 Profile 全局唯一的一份，见 TailscaleIdentity。
    // 线路上只留「这条线路怎么使用 tailnet」。
    var tailscaleExitNode: String = ""
    var tailscaleMagicDNS: Bool = false
    var tailscaleAuthenticated: Bool = false

    var allowInsecure: Bool = false

    enum CodingKeys: String, CodingKey {
        case id, name, type, enabled, verified
        case allowInsecure = "allow_insecure"
        case vpnServer = "vpn_server"
        case vpnUsername = "vpn_username"
        case vpnPassword = "vpn_password"
        case trojanServer = "trojan_server"
        case trojanPort = "trojan_port"
        case trojanPassword = "trojan_password"
        case trojanSNI = "trojan_sni"
        case ssServer = "ss_server"
        case ssPort = "ss_port"
        case ssMethod = "ss_method"
        case ssPassword = "ss_password"
        case vmessServer = "vmess_server"
        case vmessPort = "vmess_port"
        case vmessUUID = "vmess_uuid"
        case vmessAltID = "vmess_alt_id"
        case tailscaleExitNode = "tailscale_exit_node"
        case tailscaleMagicDNS = "tailscale_magic_dns"
        case tailscaleAuthenticated = "tailscale_authenticated"
    }

    init(id: String, name: String, type: String, enabled: Bool = true, verified: Bool = false,
         vpnServer: String = "", vpnUsername: String = "", vpnPassword: String = "",
         trojanServer: String = "", trojanPort: Int = 443, trojanPassword: String = "", trojanSNI: String = "",
         ssServer: String = "", ssPort: Int = 8388, ssMethod: String = "aes-256-gcm", ssPassword: String = "",
         vmessServer: String = "", vmessPort: Int = 443, vmessUUID: String = "", vmessAltID: Int = 0,
         tailscaleExitNode: String = "", tailscaleMagicDNS: Bool = false,
         tailscaleAuthenticated: Bool = false,
         allowInsecure: Bool = false) {
        self.id = id; self.name = name; self.type = type; self.enabled = enabled; self.verified = verified
        self.vpnServer = vpnServer; self.vpnUsername = vpnUsername; self.vpnPassword = vpnPassword
        self.trojanServer = trojanServer; self.trojanPort = trojanPort; self.trojanPassword = trojanPassword; self.trojanSNI = trojanSNI
        self.ssServer = ssServer; self.ssPort = ssPort; self.ssMethod = ssMethod; self.ssPassword = ssPassword
        self.vmessServer = vmessServer; self.vmessPort = vmessPort; self.vmessUUID = vmessUUID; self.vmessAltID = vmessAltID
        self.tailscaleExitNode = tailscaleExitNode
        self.tailscaleMagicDNS = tailscaleMagicDNS
        self.tailscaleAuthenticated = tailscaleAuthenticated
        self.allowInsecure = allowInsecure
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        type = try c.decode(String.self, forKey: .type)
        enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? true
        verified = try c.decodeIfPresent(Bool.self, forKey: .verified) ?? false
        vpnServer = try c.decodeIfPresent(String.self, forKey: .vpnServer) ?? ""
        vpnUsername = try c.decodeIfPresent(String.self, forKey: .vpnUsername) ?? ""
        vpnPassword = try c.decodeIfPresent(String.self, forKey: .vpnPassword) ?? ""
        trojanServer = try c.decodeIfPresent(String.self, forKey: .trojanServer) ?? ""
        trojanPort = try c.decodeIfPresent(Int.self, forKey: .trojanPort) ?? 443
        trojanPassword = try c.decodeIfPresent(String.self, forKey: .trojanPassword) ?? ""
        trojanSNI = try c.decodeIfPresent(String.self, forKey: .trojanSNI) ?? ""
        ssServer = try c.decodeIfPresent(String.self, forKey: .ssServer) ?? ""
        ssPort = try c.decodeIfPresent(Int.self, forKey: .ssPort) ?? 8388
        ssMethod = try c.decodeIfPresent(String.self, forKey: .ssMethod) ?? "aes-256-gcm"
        ssPassword = try c.decodeIfPresent(String.self, forKey: .ssPassword) ?? ""
        vmessServer = try c.decodeIfPresent(String.self, forKey: .vmessServer) ?? ""
        vmessPort = try c.decodeIfPresent(Int.self, forKey: .vmessPort) ?? 443
        vmessUUID = try c.decodeIfPresent(String.self, forKey: .vmessUUID) ?? ""
        vmessAltID = try c.decodeIfPresent(Int.self, forKey: .vmessAltID) ?? 0
        tailscaleExitNode = try c.decodeIfPresent(String.self, forKey: .tailscaleExitNode) ?? ""
        tailscaleMagicDNS = try c.decodeIfPresent(
            Bool.self,
            forKey: .tailscaleMagicDNS
        ) ?? false
        tailscaleAuthenticated = try c.decodeIfPresent(
            Bool.self,
            forKey: .tailscaleAuthenticated
        ) ?? false
        allowInsecure = try c.decodeIfPresent(Bool.self, forKey: .allowInsecure) ?? false
    }
}

struct RuleSet: Codable, Identifiable, Hashable {
    var id: String
    var name: String
    var type: String  // url / manual
    var enabled: Bool = true
    var url: String = ""
    var format: String = "auto"
    var domains: [String] = []
    var cidrs: [String] = []

    static let connectivityDirectID = "xdial-connectivity-direct"
    static let connectivityAnyConnectID = "xdial-connectivity-anyconnect"
    /// 保留旧持久化 ID，避免升级时短暂出现第三条规则；语义已经从
    /// “AnyConnect”迁移为当前场景中确定选出的非 Direct 验收出口。
    static let connectivityOutboundID = connectivityAnyConnectID

    static func isConnectivityTestRuleID(_ id: String) -> Bool {
        id == connectivityDirectID || id == connectivityOutboundID
    }

    var isConnectivityTestRule: Bool {
        Self.isConnectivityTestRuleID(id)
    }
}

struct RuleBinding: Codable, Hashable, Identifiable {
    var ruleSetID: String
    var lineID: String = ""
    var subscriptionID: String = ""

    var id: String { ruleSetID }

    var targetID: String {
        get { subscriptionID.isEmpty ? "port:\(lineID)" : "sub:\(subscriptionID)" }
        set {
            if newValue.hasPrefix("sub:") {
                subscriptionID = String(newValue.dropFirst(4)); lineID = ""
            } else {
                lineID = newValue.hasPrefix("port:") ? String(newValue.dropFirst(5)) : newValue
                subscriptionID = ""
            }
        }
    }

    enum CodingKeys: String, CodingKey {
        case ruleSetID = "rule_set_id"
        case lineID = "line_id"
        case subscriptionID = "subscription_id"
    }

    init(ruleSetID: String, lineID: String = "", subscriptionID: String = "") {
        self.ruleSetID = ruleSetID
        self.lineID = lineID
        self.subscriptionID = subscriptionID
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ruleSetID = try c.decode(String.self, forKey: .ruleSetID)
        lineID = try c.decodeIfPresent(String.self, forKey: .lineID) ?? ""
        subscriptionID = try c.decodeIfPresent(String.self, forKey: .subscriptionID) ?? ""
    }
}

struct SubProxyGroup: Codable, Hashable {
    var name: String
    var type: String
    var proxies: [String] = []
    var selected: String = ""
    var url: String = ""
    var interval: Int = 0

    init(
        name: String,
        type: String,
        proxies: [String] = [],
        selected: String = "",
        url: String = "",
        interval: Int = 0
    ) {
        self.name = name
        self.type = type
        self.proxies = proxies
        self.selected = selected
        self.url = url
        self.interval = interval
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        type = try c.decode(String.self, forKey: .type)
        proxies = try c.decodeIfPresent([String].self, forKey: .proxies) ?? []
        selected = try c.decodeIfPresent(String.self, forKey: .selected) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        interval = try c.decodeIfPresent(Int.self, forKey: .interval) ?? 0
    }

    enum CodingKeys: String, CodingKey {
        case name, type, proxies, selected, url, interval
    }
}

struct SubRule: Codable, Hashable {
    var type: String
    var value: String = ""
    var group: String

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.decode(String.self, forKey: .type)
        value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
        group = try c.decode(String.self, forKey: .group)
    }

    enum CodingKeys: String, CodingKey {
        case type, value, group
    }
}

struct Subscription: Codable, Identifiable, Hashable {
    var id: String
    var name: String
    var url: String
    var format: String = "auto"
    var enabled: Bool = true
    var strategy: String = "urltest"
    var selected: String = ""
    var lines: [Line] = []
    var proxyGroups: [SubProxyGroup] = []
    var rules: [SubRule] = []
    var updatedAt: Int = 0
    var testURL: String = "https://www.gstatic.com/generate_204"
    var testInterval: Int = 300
    var geoIPRuleSetURLTemplate: String = ""

    enum CodingKeys: String, CodingKey {
        case id, name, url, format, enabled, strategy, selected, rules
        case lines = "lines"
        case proxyGroups = "proxy_groups"
        case updatedAt = "updated_at"
        case testURL = "test_url"
        case testInterval = "test_interval"
        case geoIPRuleSetURLTemplate = "geoip_rule_set_url_template"
    }

    init(id: String, name: String, url: String, format: String = "auto",
         strategy: String = "urltest", lines: [Line] = [],
         proxyGroups: [SubProxyGroup] = [], rules: [SubRule] = [], selected: String = "",
         geoIPRuleSetURLTemplate: String = "") {
        self.id = id; self.name = name; self.url = url; self.format = format
        self.strategy = strategy; self.lines = lines
        self.selected = selected
        self.proxyGroups = proxyGroups; self.rules = rules
        self.geoIPRuleSetURLTemplate = geoIPRuleSetURLTemplate
        self.updatedAt = Int(Date().timeIntervalSince1970)
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        url = try c.decode(String.self, forKey: .url)
        format = try c.decodeIfPresent(String.self, forKey: .format) ?? "auto"
        enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? true
        strategy = try c.decodeIfPresent(String.self, forKey: .strategy) ?? "urltest"
        selected = try c.decodeIfPresent(String.self, forKey: .selected) ?? ""
        lines = try c.decodeIfPresent([Line].self, forKey: .lines) ?? []
        proxyGroups = try c.decodeIfPresent([SubProxyGroup].self, forKey: .proxyGroups) ?? []
        rules = try c.decodeIfPresent([SubRule].self, forKey: .rules) ?? []
        updatedAt = try c.decodeIfPresent(Int.self, forKey: .updatedAt) ?? 0
        testURL = try c.decodeIfPresent(String.self, forKey: .testURL) ?? "https://www.gstatic.com/generate_204"
        testInterval = try c.decodeIfPresent(Int.self, forKey: .testInterval) ?? 300
        geoIPRuleSetURLTemplate = try c.decodeIfPresent(
            String.self,
            forKey: .geoIPRuleSetURLTemplate
        ) ?? ""
    }
}

struct Scenario: Codable, Identifiable, Hashable {
    var id: String
    var name: String
    /// 跨平台保存语义 Key，而不是某个平台的 SF Symbol 名；nil 表示自动。
    var iconOverride: String?
    var matchSSIDs: [String] = []
    var bindings: [RuleBinding] = []
    var defaultLineID: String = ""
    var defaultSubscriptionID: String = ""

    var defaultTargetID: String {
        get { defaultSubscriptionID.isEmpty ? "port:\(defaultLineID)" : "sub:\(defaultSubscriptionID)" }
        set {
            if newValue.hasPrefix("sub:") {
                defaultSubscriptionID = String(newValue.dropFirst(4)); defaultLineID = ""
            } else {
                defaultLineID = newValue.hasPrefix("port:") ? String(newValue.dropFirst(5)) : newValue
                defaultSubscriptionID = ""
            }
        }
    }

    enum CodingKeys: String, CodingKey {
        case id, name, bindings
        case iconOverride = "icon"
        case matchSSIDs = "match_ssids"
        case defaultLineID = "default_line_id"
        case defaultSubscriptionID = "default_subscription_id"
    }

    init(id: String, name: String, matchSSIDs: [String] = [], bindings: [RuleBinding] = [], defaultLineID: String = "", defaultSubscriptionID: String = "", iconOverride: String? = nil) {
        self.id = id; self.name = name; self.iconOverride = iconOverride
        self.matchSSIDs = matchSSIDs
        self.bindings = bindings
        self.defaultLineID = defaultLineID; self.defaultSubscriptionID = defaultSubscriptionID
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        iconOverride = try c.decodeIfPresent(String.self, forKey: .iconOverride)
        if iconOverride?.isEmpty == true { iconOverride = nil }
        matchSSIDs = try c.decodeIfPresent(
            [String].self,
            forKey: .matchSSIDs
        ) ?? []
        bindings = try c.decodeIfPresent([RuleBinding].self, forKey: .bindings) ?? []
        defaultLineID = try c.decodeIfPresent(String.self, forKey: .defaultLineID) ?? ""
        defaultSubscriptionID = try c.decodeIfPresent(String.self, forKey: .defaultSubscriptionID) ?? ""
    }
}

/// 本机在 tailnet 中的身份，整个 Profile 全局唯一一份。
///
/// 一个 tsnet 实例就是 tailnet 里的一台设备，两个实例不能共用同一份 state，
/// 所以身份必须是全局的：所有 Tailscale 线路共享同一次登录，退出一次全部失效。
struct TailscaleIdentity: Codable, Equatable, Hashable {
    /// 注册到 tailnet 的设备名，首次登录时自动生成后固定不变，用户不可编辑。
    var hostname: String = ""

    init(hostname: String = "") {
        self.hostname = hostname
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname) ?? ""
    }
}

/// 打包时带进 App 的预置配置。首次安装时直接用它；已有配置时把它加进去，每个 revision 只加一次。
struct ProfileSeed: Equatable {
    var profile: Profile
    /// 预置内容有增删时加一；老用户升级后据此补上新内容。
    var revision: Int
    /// 预置改过内容的手动规则：规则 ID → 之前各版发过的匹配内容。
    var ruleSetHistory: [String: [ShippedRuleContent]] = [:]
}

/// 预置某一版发出的手动规则匹配内容。比较时不计顺序和大小写。
struct ShippedRuleContent: Hashable {
    var domains: Set<String>
    var cidrs: Set<String>

    init(domains: [String], cidrs: [String]) {
        self.domains = Set(domains.map { $0.trimmingCharacters(in: .whitespaces).lowercased() })
        self.cidrs = Set(cidrs.map { $0.trimmingCharacters(in: .whitespaces).lowercased() })
    }

    init(_ rule: RuleSet) { self.init(domains: rule.domains, cidrs: rule.cidrs) }
}

struct Profile: Codable, Equatable {
    var lines: [Line] = []
    var ruleSets: [RuleSet] = []
    var scenarios: [Scenario] = []
    var subscriptions: [Subscription] = []
    var activeScenarioID: String = ""
    var tailscale = TailscaleIdentity()

    enum CodingKeys: String, CodingKey {
        case lines = "lines"
        case ruleSets = "rule_sets"
        case scenarios = "scenarios"
        case subscriptions
        case activeScenarioID = "active_scenario_id"
        case tailscale
    }

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        lines = try c.decodeIfPresent([Line].self, forKey: .lines) ?? []
        ruleSets = try c.decodeIfPresent([RuleSet].self, forKey: .ruleSets) ?? []
        scenarios = try c.decodeIfPresent(
            [Scenario].self,
            forKey: .scenarios
        ) ?? []
        subscriptions = try c.decodeIfPresent([Subscription].self, forKey: .subscriptions) ?? []
        activeScenarioID = try c.decodeIfPresent(
            String.self,
            forKey: .activeScenarioID
        ) ?? ""
        tailscale = try c.decodeIfPresent(TailscaleIdentity.self, forKey: .tailscale) ?? TailscaleIdentity()
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(lines, forKey: .lines)
        try c.encode(ruleSets, forKey: .ruleSets)
        try c.encode(scenarios, forKey: .scenarios)
        try c.encode(subscriptions, forKey: .subscriptions)
        try c.encode(activeScenarioID, forKey: .activeScenarioID)
        try c.encode(tailscale, forKey: .tailscale)
    }
}

struct ActiveRouteTargetSummary: Identifiable, Equatable, Sendable {
    let id: String
    let name: String
    let type: String
    let isSubscription: Bool
}

extension Profile {
    private static var connectivityRuleSets: [RuleSet] {
        [
            RuleSet(
                id: RuleSet.connectivityDirectID,
                name: "连接验收 · Direct",
                type: "manual",
                cidrs: ["1.0.0.1/32"]
            ),
            RuleSet(
                id: RuleSet.connectivityOutboundID,
                name: "连接验收 · 非 Direct 出口",
                type: "manual",
                cidrs: ["1.1.1.1/32"]
            ),
        ]
    }

    static func connectivityBindings(outboundLineID: String, directLineID: String) -> [RuleBinding] {
        var bindings: [RuleBinding] = []
        if !directLineID.isEmpty {
            bindings.append(RuleBinding(
                ruleSetID: RuleSet.connectivityDirectID,
                lineID: directLineID
            ))
        }
        if !outboundLineID.isEmpty && outboundLineID != directLineID {
            bindings.append(RuleBinding(
                ruleSetID: RuleSet.connectivityOutboundID,
                lineID: outboundLineID
            ))
        }
        return bindings
    }

    /// 返回场景中用于双出口路由验收的非 Direct 目标。选择顺序是协议的一部分：
    /// 非 Direct 默认出口优先；否则按普通、已启用规则绑定的原始顺序取首个。
    /// 锁定验收规则自身不会参与选择，避免旧错误绑定把迁移继续带错。
    func connectivityOutboundBinding(for scenario: Scenario) -> RuleBinding? {
        func isAvailableNonDirect(_ binding: RuleBinding) -> Bool {
            if !binding.subscriptionID.isEmpty {
                return subscriptions.contains {
                    $0.id == binding.subscriptionID && $0.enabled
                        && $0.lines.contains(where: { $0.enabled })
                }
            }
            guard !binding.lineID.isEmpty,
                  let line = lines.first(where: {
                      $0.id == binding.lineID && $0.enabled
                  }) else { return false }
            if line.type == "tailscale" {
                // An overlay-only endpoint is globally generated for subnet
                // routes, but it is not a public egress for the 1.1.1.1 probe.
                return !line.tailscaleExitNode
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                    .isEmpty
            }
            return line.type != "direct"
        }

        let defaultBinding = RuleBinding(
            ruleSetID: RuleSet.connectivityOutboundID,
            lineID: scenario.defaultLineID,
            subscriptionID: scenario.defaultSubscriptionID
        )
        if isAvailableNonDirect(defaultBinding) {
            return defaultBinding
        }

        for binding in scenario.bindings where !binding.ruleSetID.isEmpty {
            guard !RuleSet.isConnectivityTestRuleID(binding.ruleSetID),
            ruleSets.first(where: { $0.id == binding.ruleSetID })?.enabled == true,
            isAvailableNonDirect(binding) else {
                continue
            }
            return RuleBinding(
                ruleSetID: RuleSet.connectivityOutboundID,
                lineID: binding.lineID,
                subscriptionID: binding.subscriptionID
            )
        }
        return nil
    }

    /// 与生成器的活动目标语义一致：默认出口始终计入；只有 enabled 规则的绑定
    /// 计入；线路和订阅统一去重。主页使用这一份摘要，避免订阅场景被误显示成
    /// “没有线路”，也避免 disabled 规则把未运行目标带进来。
    func activeRouteTargetSummaries(for scenario: Scenario) -> [ActiveRouteTargetSummary] {
        var result: [ActiveRouteTargetSummary] = []
        var seen = Set<String>()

        func append(lineID: String, subscriptionID: String) {
            if !subscriptionID.isEmpty {
                let key = "sub:\(subscriptionID)"
                guard seen.insert(key).inserted,
                      let subscription = subscriptions.first(where: {
                          $0.id == subscriptionID && $0.enabled
                              && $0.lines.contains(where: { $0.enabled })
                      }) else { return }
                result.append(ActiveRouteTargetSummary(
                    id: key,
                    name: subscription.name,
                    type: "subscription",
                    isSubscription: true
                ))
                return
            }
            let key = "port:\(lineID)"
            guard !lineID.isEmpty, seen.insert(key).inserted,
                  let line = lines.first(where: {
                      $0.id == lineID && $0.enabled
                  }) else { return }
            result.append(ActiveRouteTargetSummary(
                id: key,
                name: line.name,
                type: line.type,
                isSubscription: false
            ))
        }

        append(
            lineID: scenario.defaultLineID,
            subscriptionID: scenario.defaultSubscriptionID
        )
        for binding in scenario.bindings {
            guard ruleSets.first(where: { $0.id == binding.ruleSetID })?.enabled == true else {
                continue
            }
            append(lineID: binding.lineID, subscriptionID: binding.subscriptionID)
        }
        return result
    }

    /// 连接验收规则是真实 profile 的一部分，会在配置页展示，而不是生成器
    /// 暗中注入的路由。旧 profile 升级时会恢复两条精确 CIDR；第二条只绑定
    /// 确定选出的非 Direct 活动目标。纯 Direct 场景不伪造第二个出口。
    @discardableResult
    mutating func ensureConnectivityTestConfiguration() -> Bool {
        let before = self

        for expected in Self.connectivityRuleSets {
            if let index = ruleSets.firstIndex(where: { $0.id == expected.id }) {
                ruleSets[index] = expected
            } else {
                ruleSets.append(expected)
            }
        }

        let directLineID = lines.first(where: { $0.type == "direct" && $0.enabled })?.id ?? ""
        for index in scenarios.indices {
            let ordinaryBindings = scenarios[index].bindings.filter {
                $0.ruleSetID != RuleSet.connectivityDirectID
                    && $0.ruleSetID != RuleSet.connectivityOutboundID
            }
            var selectionScenario = scenarios[index]
            selectionScenario.bindings = ordinaryBindings
            let outboundBinding = connectivityOutboundBinding(for: selectionScenario)
            var acceptanceBindings: [RuleBinding] = []
            if !directLineID.isEmpty {
                acceptanceBindings.append(RuleBinding(
                    ruleSetID: RuleSet.connectivityDirectID,
                    lineID: directLineID
                ))
            }
            if let outboundBinding {
                acceptanceBindings.append(outboundBinding)
            }
            scenarios[index].bindings = acceptanceBindings + ordinaryBindings
        }

        return self != before
    }

    /// 把一份预置配置加进已经在用的配置里。只添加缺的线路、规则和场景；用户自己建的、改过的
    /// 和当前选中的场景都不动。预置里的线路如果和已有线路是同一类型、同一个服务器，就沿用已有的
    /// 那条（账号密码还在上面），预置场景改绑到它。返回是否有改动。
    @discardableResult
    mutating func adding(seed: Profile, ruleSetHistory: [String: [ShippedRuleContent]] = [:]) -> Bool {
        let before = self

        // 预置线路 ID → 实际使用的线路 ID。
        var lineIDs: [String: String] = [:]
        for seedLine in seed.lines {
            if lines.contains(where: { $0.id == seedLine.id }) {
                lineIDs[seedLine.id] = seedLine.id
            } else if seedLine.type == "direct",
                      let direct = lines.first(where: { $0.type == "direct" && $0.enabled }) {
                lineIDs[seedLine.id] = direct.id
            } else if seedLine.type == "vpn",
                      !seedLine.vpnServer.isEmpty,
                      let same = lines.first(where: {
                          $0.type == "vpn" && $0.vpnServer.caseInsensitiveCompare(seedLine.vpnServer) == .orderedSame
                      }) {
                lineIDs[seedLine.id] = same.id
            } else {
                lines.append(seedLine)
                lineIDs[seedLine.id] = seedLine.id
            }
        }

        for ruleSet in seed.ruleSets
        where !ruleSet.isConnectivityTestRule && !ruleSets.contains(where: { $0.id == ruleSet.id }) {
            ruleSets.append(ruleSet)
        }
        // 预置改过内容的规则：用户没动过（仍是之前某一版发的内容）才换成新内容，改过的保留。
        for ruleSet in seed.ruleSets {
            guard let shipped = ruleSetHistory[ruleSet.id],
                  let index = ruleSets.firstIndex(where: { $0.id == ruleSet.id }) else { continue }
            let current = ShippedRuleContent(ruleSets[index])
            guard current != ShippedRuleContent(ruleSet) else { continue }
            if shipped.contains(current) {
                ruleSets[index].domains = ruleSet.domains
                ruleSets[index].cidrs = ruleSet.cidrs
            } else {
                appLog("seed: kept the user's edits to rule \(ruleSet.id)")
            }
        }

        let seedScenarioIDs = seed.scenarios.map(\.id)
        for var scenario in seed.scenarios where !scenarios.contains(where: { $0.id == scenario.id }) {
            scenario.defaultLineID = lineIDs[scenario.defaultLineID] ?? scenario.defaultLineID
            scenario.bindings = scenario.bindings
                .filter { !RuleSet.isConnectivityTestRuleID($0.ruleSetID) }
                .map { binding in
                    var bound = binding
                    bound.lineID = lineIDs[binding.lineID] ?? binding.lineID
                    return bound
                }
            scenarios.append(scenario)
        }
        // 预置场景按预置里的顺序排在前面，其余的保持原来的先后。
        let leading = seedScenarioIDs.compactMap { id in scenarios.first(where: { $0.id == id }) }
        scenarios = leading + scenarios.filter { !seedScenarioIDs.contains($0.id) }
        if activeScenarioID.isEmpty { activeScenarioID = seed.activeScenarioID }

        ensureConnectivityTestConfiguration()
        return self != before
    }

    static func bootstrap() -> Profile {
        var p = Profile()
        p.lines = [
            Line(id: "direct", name: "直连", type: "direct", verified: true),
            Line(id: "vpn", name: "AnyConnect", type: "vpn"),
            Line(id: "ss", name: "SS 节点", type: "trojan", enabled: false),
        ]
        p.ruleSets = [
            RuleSet(id: "internal", name: "内部域名", type: "manual"),
        ] + connectivityRuleSets
        return p
    }

    static func templateOverseas(ruleSetIDs: [String], vpnLineID: String, directLineID: String) -> Scenario {
        Scenario(
            id: UUID().uuidString,
            name: "海外",
            bindings: connectivityBindings(outboundLineID: vpnLineID, directLineID: directLineID)
                + ruleSetIDs.filter { id in
                    id != RuleSet.connectivityDirectID && id != RuleSet.connectivityAnyConnectID
                }.map { RuleBinding(ruleSetID: $0, lineID: vpnLineID) },
            defaultLineID: directLineID
        )
    }

    static func templateDomestic(ruleSetIDs: [String], remoteRuleSetID: String, vpnLineID: String, directLineID: String) -> Scenario {
        var bindings = connectivityBindings(outboundLineID: vpnLineID, directLineID: directLineID)
            + ruleSetIDs.filter { id in
                id != RuleSet.connectivityDirectID && id != RuleSet.connectivityAnyConnectID
            }.map { RuleBinding(ruleSetID: $0, lineID: vpnLineID) }
        if !remoteRuleSetID.isEmpty {
            bindings.append(RuleBinding(ruleSetID: remoteRuleSetID, lineID: vpnLineID))
        }
        return Scenario(
            id: UUID().uuidString,
            name: "国内",
            bindings: bindings,
            defaultLineID: directLineID
        )
    }

    static func templateDomesticSS(ruleSetIDs: [String], remoteRuleSetID: String, vpnLineID: String, ssLineID: String, directLineID: String) -> Scenario {
        var bindings = connectivityBindings(outboundLineID: ssLineID, directLineID: directLineID)
            + ruleSetIDs.filter { id in
                id != RuleSet.connectivityDirectID && id != RuleSet.connectivityAnyConnectID
            }.map { RuleBinding(ruleSetID: $0, lineID: vpnLineID) }
        if !remoteRuleSetID.isEmpty {
            bindings.append(RuleBinding(ruleSetID: remoteRuleSetID, lineID: ssLineID))
        }
        return Scenario(
            id: UUID().uuidString,
            name: "国内+SS",
            bindings: bindings,
            defaultLineID: directLineID
        )
    }
}
