import Foundation
import Security
import CryptoKit

// App 与 VPN 扩展共用的自动选场景规则、扩展上报的场景状态，以及共享 Keychain 里的场景目录。
// 只依赖 Foundation / Security：小组件也编译这个目录。

// MARK: - 判断规则

/// 按当前网络选场景的规则。App 写进场景目录，扩展照着判断；两边用同一份代码。
struct ScenarioAutoPolicy: Codable, Equatable, Sendable {
    struct WiFiRule: Codable, Equatable, Sendable {
        let scenarioID: String
        let ssids: [String]

        enum CodingKeys: String, CodingKey {
            case scenarioID = "scenario_id"
            case ssids
        }
    }

    /// 「自动」开关。关着时扩展不判断也不切换。
    var enabled: Bool
    /// 按场景列表顺序排列，先匹配的优先。
    var wifiRules: [WiFiRule]
    /// 打包注入的地区对应表（两位地区代码 → 场景 ID，`*` 表示其他地区）。
    var regionScenarios: [String: String]
    /// Profile 里现有的场景。对应的场景已被删除时不选。
    var scenarioIDs: [String]

    enum CodingKeys: String, CodingKey {
        case enabled
        case wifiRules = "wifi_rules"
        case regionScenarios = "region_scenarios"
        case scenarioIDs = "scenario_ids"
    }

    /// 地区表引用、但当前 Profile 里存在的场景。
    var regionScenarioIDs: Set<String> {
        Set(regionScenarios.values).intersection(scenarioIDs)
    }
}

/// Main actor owned catalog build state. Policy changes survive the first build;
/// cancellation, disconnect and a newer build invalidate an older completion.
struct ScenarioCatalogBuildState {
    private var buildID = UUID()
    private var policy: ScenarioAutoPolicy?

    mutating func begin(policy: ScenarioAutoPolicy) -> UUID {
        buildID = UUID()
        self.policy = policy
        return buildID
    }

    mutating func update(policy: ScenarioAutoPolicy) { self.policy = policy }

    mutating func clear() {
        buildID = UUID()
        policy = nil
    }

    func policyForCompletion(_ id: UUID, cancelled: Bool) -> ScenarioAutoPolicy? {
        !cancelled && id == buildID ? policy : nil
    }
}

/// Include Wi-Fi identity without storing its name in the bounce guard.
enum ScenarioNetworkIdentity {
    static func key(path: String?, ssid: String?) -> String {
        let identity = SHA256.hash(data: Data((ssid ?? "").utf8))
            .map { String(format: "%02x", $0) }.joined()
        return (path ?? "") + ":wifi:" + identity
    }
}

/// 一次判断的结论：选哪个场景，凭什么。
struct ScenarioAutoPick: Codable, Equatable, Sendable {
    enum Reason: Equatable, Sendable {
        /// 连上了某个场景登记过的 Wi-Fi。
        case wifi(String)
        /// 按网络出口所在地。
        case country(String)
    }

    let scenarioID: String
    let reason: Reason

    enum CodingKeys: String, CodingKey {
        case scenarioID = "scenario_id"
        case reasonKind = "reason"
        case reasonValue = "reason_value"
    }

    init(scenarioID: String, reason: Reason) {
        self.scenarioID = scenarioID
        self.reason = reason
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        scenarioID = try container.decode(String.self, forKey: .scenarioID)
        let value = try container.decode(String.self, forKey: .reasonValue)
        switch try container.decode(String.self, forKey: .reasonKind) {
        case "wifi": reason = .wifi(value)
        case "country": reason = .country(value)
        default:
            throw DecodingError.dataCorruptedError(
                forKey: .reasonKind, in: container, debugDescription: "unknown pick reason"
            )
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(scenarioID, forKey: .scenarioID)
        switch reason {
        case .wifi(let ssid):
            try container.encode("wifi", forKey: .reasonKind)
            try container.encode(ssid, forKey: .reasonValue)
        case .country(let code):
            try container.encode("country", forKey: .reasonKind)
            try container.encode(code, forKey: .reasonValue)
        }
    }
}

enum ScenarioAutoDecision {
    /// 当前 Wi-Fi 名称与某个场景登记的名称完全一致（区分大小写）时选它。
    static func wifiPick(ssid: String?, rules: [ScenarioAutoPolicy.WiFiRule]) -> ScenarioAutoPick? {
        guard let ssid = ssid?.trimmingCharacters(in: .whitespacesAndNewlines), !ssid.isEmpty,
              let rule = rules.first(where: { rule in
                  rule.ssids.contains { $0.trimmingCharacters(in: .whitespacesAndNewlines) == ssid }
              }) else { return nil }
        return ScenarioAutoPick(scenarioID: rule.scenarioID, reason: .wifi(ssid))
    }

    /// 按出口地区查对应表，未列出的地区用 `*`。对应的场景不存在时不选。
    static func regionPick(
        countryCode: String?,
        regionScenarios: [String: String],
        scenarioIDs: [String]
    ) -> ScenarioAutoPick? {
        guard let code = countryCode?.uppercased(), code.count == 2,
              code.allSatisfy({ $0.isASCII && $0.isLetter }),
              let scenarioID = regionScenarios[code] ?? regionScenarios["*"],
              scenarioIDs.contains(scenarioID) else { return nil }
        return ScenarioAutoPick(scenarioID: scenarioID, reason: .country(code))
    }
}

/// 防止来回切换：十分钟内刚从某个场景自动切走，又在同一个网络里判断回该场景时不再自动切。
/// 两次判断互相矛盾时（比如出口查询结果不稳定），宁可停下来提示，也不能反复切换；
/// 网络确实变了（换了 Wi-Fi、接口或网关）时照常切换。
struct ScenarioAutoSwitchGuard: Codable, Equatable, Sendable {
    struct Entry: Codable, Equatable, Sendable {
        let from: String
        let to: String
        let at: Date
        /// 切换时的网络特征；为空表示不区分网络。
        var network: String?
    }

    static let window: TimeInterval = 600
    private(set) var recent: [Entry] = []

    init() {}

    /// network 为空时不区分网络：只要十分钟内刚从 target 切走就不再切回。
    mutating func mayAutoSwitch(to target: String, network: String? = nil, now: Date = Date()) -> Bool {
        recent.removeAll { now.timeIntervalSince($0.at) > Self.window }
        return !recent.contains { entry in
            entry.from == target && (network == nil || entry.network == nil || entry.network == network)
        }
    }

    mutating func note(from: String, to: String, network: String? = nil, now: Date = Date()) {
        recent.removeAll { now.timeIntervalSince($0.at) > Self.window }
        recent.append(Entry(from: from, to: to, at: now, network: network))
    }
}

// MARK: - 判断经过

/// 最近一次自动判断的经过，写进「XDial 诊断」。不记录 Wi-Fi 名称。
struct ScenarioSurveyRecord: Codable, Equatable, Sendable {
    enum ExitLookup: Equatable, Sendable {
        /// 已经靠 Wi-Fi 认出来，或者没有按地区选场景的配置。
        case notNeeded
        /// viaTunnel：已连接时由 VPN 扩展经 Direct 线路查；否则由 App 直接查。
        case found(code: String, viaTunnel: Bool)
        case failed(viaTunnel: Bool, reason: String?)
    }

    let at: Date
    let ssidRead: Bool
    let ssidMatched: Bool
    let exit: ExitLookup
    let picked: Bool
    /// 由 VPN 扩展在后台完成的判断。
    var byTunnel: Bool = false

    init(at: Date, ssidRead: Bool, ssidMatched: Bool, exit: ExitLookup, picked: Bool, byTunnel: Bool = false) {
        self.at = at
        self.ssidRead = ssidRead
        self.ssidMatched = ssidMatched
        self.exit = exit
        self.picked = picked
        self.byTunnel = byTunnel
    }

    /// 「XDial 诊断」里的一行。
    func summary(isChinese: Bool) -> String {
        let time = Self.timeFormatter.string(from: at)
        let wifi = ssidRead
            ? (ssidMatched ? (isChinese ? "已读到并匹配" : "read, matched") : (isChinese ? "已读到，未匹配" : "read, no match"))
            : (isChinese ? "未读到" : "not read")
        let (open, close, colon) = isChinese ? ("（", "）", "：") : (" (", ")", ": ")
        let exitText: String
        switch exit {
        case .notNeeded:
            exitText = isChinese ? "未查询" : "not queried"
        case .found(let code, let viaTunnel):
            let source = viaTunnel ? (isChinese ? "经直连线路" : "via Direct line") : (isChinese ? "本机直接查询" : "queried locally")
            exitText = "\(code)\(open)\(source)\(close)"
        case .failed(let viaTunnel, let reason):
            let source = viaTunnel ? (isChinese ? "经直连线路" : "via Direct line") : (isChinese ? "本机直接查询" : "queried locally")
            let failed = isChinese ? "查询失败" : "failed"
            exitText = "\(failed)\(open)\(source)\(reason.map { "\(colon)\($0)" } ?? "")\(close)"
        }
        let outcome = picked ? (isChinese ? "已判断" : "decided") : (isChinese ? "没判断出来" : "undecided")
        let by = byTunnel ? (isChinese ? "（VPN 扩展）" : " (VPN extension)") : ""
        return isChinese
            ? "\(time) \(outcome)\(by)；Wi-Fi \(wifi)；出口 \(exitText)"
            : "\(time) \(outcome)\(by); Wi-Fi \(wifi); exit \(exitText)"
    }

    private static let timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm:ss"
        return formatter
    }()

    enum CodingKeys: String, CodingKey {
        case at
        case ssidRead = "ssid_read"
        case ssidMatched = "ssid_matched"
        case exitKind = "exit"
        case exitCode = "exit_code"
        case exitViaTunnel = "exit_via_tunnel"
        case exitReason = "exit_reason"
        case picked
        case byTunnel = "by_tunnel"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        at = try container.decode(Date.self, forKey: .at)
        ssidRead = try container.decode(Bool.self, forKey: .ssidRead)
        ssidMatched = try container.decode(Bool.self, forKey: .ssidMatched)
        picked = try container.decode(Bool.self, forKey: .picked)
        byTunnel = try container.decodeIfPresent(Bool.self, forKey: .byTunnel) ?? false
        let viaTunnel = try container.decodeIfPresent(Bool.self, forKey: .exitViaTunnel) ?? false
        switch try container.decode(String.self, forKey: .exitKind) {
        case "found":
            exit = .found(code: try container.decode(String.self, forKey: .exitCode), viaTunnel: viaTunnel)
        case "failed":
            exit = .failed(viaTunnel: viaTunnel, reason: try container.decodeIfPresent(String.self, forKey: .exitReason))
        default:
            exit = .notNeeded
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(at, forKey: .at)
        try container.encode(ssidRead, forKey: .ssidRead)
        try container.encode(ssidMatched, forKey: .ssidMatched)
        try container.encode(picked, forKey: .picked)
        try container.encode(byTunnel, forKey: .byTunnel)
        switch exit {
        case .notNeeded:
            try container.encode("not_needed", forKey: .exitKind)
        case .found(let code, let viaTunnel):
            try container.encode("found", forKey: .exitKind)
            try container.encode(code, forKey: .exitCode)
            try container.encode(viaTunnel, forKey: .exitViaTunnel)
        case .failed(let viaTunnel, let reason):
            try container.encode("failed", forKey: .exitKind)
            try container.encode(viaTunnel, forKey: .exitViaTunnel)
            try container.encodeIfPresent(reason, forKey: .exitReason)
        }
    }
}

// MARK: - 扩展上报的场景状态

/// VPN 扩展当前运行的场景、最近一次判断与切换。扩展写进 App Group，并发 Darwin 通知；
/// App 回到前台或收到通知时读取。
struct TunnelScenarioState: Codable, Equatable, Sendable {
    struct SwitchRecord: Codable, Equatable, Sendable {
        enum Origin: String, Codable, Sendable {
            /// 扩展按网络变化自动切换。
            case automatic
            /// App 请求（用户选择或 App 前台的判断）。
            case app
        }

        enum Mode: String, Codable, Sendable {
            /// 新场景在旁边准备好后接管隧道。
            case staged
            /// 系统 VPN 不停，换一个引擎。
            case restart
        }

        let from: String
        let to: String
        let origin: Origin
        let mode: Mode?
        let at: Date
        let succeeded: Bool
        /// 失败原因，展示给用户；不含凭据。
        let failure: String?
    }

    /// 当前承载流量的场景；扩展没有在运行或不知道时为空。
    var runningScenarioID: String?
    /// 最近一次判断的结论。没判断出来时保留上一次。
    var lastPick: ScenarioAutoPick?
    var lastSurvey: ScenarioSurveyRecord?
    /// 判断出该换、却因为十分钟内刚换过而没有自动换的场景。App 显示为建议。
    var suggestedScenarioID: String?
    var lastSwitch: SwitchRecord?
    /// 正在切换时的目标场景。
    var switchingTo: String?
    var guardState = ScenarioAutoSwitchGuard()
    var updatedAt: Date

    enum CodingKeys: String, CodingKey {
        case runningScenarioID = "running_scenario_id"
        case lastPick = "last_pick"
        case lastSurvey = "last_survey"
        case suggestedScenarioID = "suggested_scenario_id"
        case lastSwitch = "last_switch"
        case switchingTo = "switching_to"
        case guardState = "guard"
        case updatedAt = "updated_at"
    }

    init(
        runningScenarioID: String? = nil,
        lastPick: ScenarioAutoPick? = nil,
        lastSurvey: ScenarioSurveyRecord? = nil,
        suggestedScenarioID: String? = nil,
        lastSwitch: SwitchRecord? = nil,
        switchingTo: String? = nil,
        guardState: ScenarioAutoSwitchGuard = ScenarioAutoSwitchGuard(),
        updatedAt: Date = Date()
    ) {
        self.runningScenarioID = runningScenarioID
        self.lastPick = lastPick
        self.lastSurvey = lastSurvey
        self.suggestedScenarioID = suggestedScenarioID
        self.lastSwitch = lastSwitch
        self.switchingTo = switchingTo
        self.guardState = guardState
        self.updatedAt = updatedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        runningScenarioID = try container.decodeIfPresent(String.self, forKey: .runningScenarioID)
        lastPick = try container.decodeIfPresent(ScenarioAutoPick.self, forKey: .lastPick)
        lastSurvey = try container.decodeIfPresent(ScenarioSurveyRecord.self, forKey: .lastSurvey)
        suggestedScenarioID = try container.decodeIfPresent(String.self, forKey: .suggestedScenarioID)
        lastSwitch = try container.decodeIfPresent(SwitchRecord.self, forKey: .lastSwitch)
        switchingTo = try container.decodeIfPresent(String.self, forKey: .switchingTo)
        guardState = try container.decodeIfPresent(ScenarioAutoSwitchGuard.self, forKey: .guardState)
            ?? ScenarioAutoSwitchGuard()
        updatedAt = try container.decode(Date.self, forKey: .updatedAt)
    }

    static func decode(_ data: Data) -> TunnelScenarioState? {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .secondsSince1970
        return try? decoder.decode(TunnelScenarioState.self, from: data)
    }

    func encoded() -> Data? {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .secondsSince1970
        encoder.outputFormatting = [.sortedKeys]
        return try? encoder.encode(self)
    }
}

enum TunnelScenarioStateStore {
    static let defaultsKey = "xdial.tunnel.scenario-state"
    /// 扩展更新状态后发出的 Darwin 通知。
    static let changedNotification = "com.xd.xdial.tunnel.scenario-state-changed"

    static func load(appGroup: String) -> TunnelScenarioState? {
        guard let data = UserDefaults(suiteName: appGroup)?.data(forKey: defaultsKey) else { return nil }
        return TunnelScenarioState.decode(data)
    }

    /// Wi-Fi 名称只随实时消息交给 App 显示，不写进共享存储。
    static func save(_ state: TunnelScenarioState, appGroup: String) {
        var stored = state
        if case .wifi? = stored.lastPick?.reason, let pick = stored.lastPick {
            stored.lastPick = ScenarioAutoPick(scenarioID: pick.scenarioID, reason: .wifi(""))
        }
        guard let data = stored.encoded() else { return }
        UserDefaults(suiteName: appGroup)?.set(data, forKey: defaultsKey)
        CFNotificationCenterPostNotification(
            CFNotificationCenterGetDarwinNotifyCenter(),
            CFNotificationName(changedNotification as CFString),
            nil, nil, true
        )
    }
}

// MARK: - 场景目录

/// App 为"可能被选中的场景"提前生成的启动包与判断规则。存在 App 与扩展共用的 Keychain 里：
/// 启动包含 AnyConnect 凭据与线路运行身份，不能写进 App Group 文件或诊断。
///
/// 每个启动包就是按需启动信封的 JSON（format / schema_version / transport / parameters /
/// config_json），参数里另带 `scenario_id` 与线路运行身份。
struct ScenarioSwitchCatalog: Codable, Equatable, Sendable {
    static let currentFormat = "xdial-scenario-catalog"
    static let currentSchemaVersion = 1
    static let maxEntries = 8
    static let maxTotalBytes = 12 * 1024 * 1024

    let format: String
    let schemaVersion: Int
    /// 每次重建目录都换新值。App 请求切换时带上，扩展据此确认读到的是同一份目录。
    let revision: String
    var policy: ScenarioAutoPolicy
    /// 场景 ID → 启动包（信封 JSON 原文）。
    var entries: [String: Data]

    enum CodingKeys: String, CodingKey {
        case format
        case schemaVersion = "schema_version"
        case revision
        case policy
        case entries
    }

    init(revision: String = UUID().uuidString, policy: ScenarioAutoPolicy, entries: [String: Data]) {
        format = Self.currentFormat
        schemaVersion = Self.currentSchemaVersion
        self.revision = revision
        self.policy = policy
        self.entries = entries
    }

    var isValid: Bool {
        format == Self.currentFormat
            && schemaVersion == Self.currentSchemaVersion
            && !revision.isEmpty
            && entries.count <= Self.maxEntries
            && entries.values.reduce(0) { $0 + $1.count } <= Self.maxTotalBytes
    }
}

enum ScenarioSwitchCatalogStore {
    enum LoadResult: Equatable {
        case missing
        case available(ScenarioSwitchCatalog)
        case unavailable(OSStatus)
        case corrupt
    }

    private static let infoKey = "XDialSharedKeychainAccessGroup"
    private static let service = "com.kafeifei.xdial.on-demand"
    private static let account = "scenario-catalog-v1"

    static func load(bundle: Bundle = .main) -> LoadResult {
        guard let accessGroup = accessGroup(bundle: bundle) else {
            return .unavailable(errSecMissingEntitlement)
        }
        var query = baseQuery(accessGroup: accessGroup)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var output: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &output)
        if status == errSecItemNotFound { return .missing }
        guard status == errSecSuccess else { return .unavailable(status) }
        guard let data = output as? Data,
              let catalog = try? JSONDecoder().decode(ScenarioSwitchCatalog.self, from: data),
              catalog.isValid else {
            return .corrupt
        }
        return .available(catalog)
    }

    @discardableResult
    static func save(_ catalog: ScenarioSwitchCatalog, bundle: Bundle = .main) -> OSStatus {
        guard catalog.isValid, let data = try? JSONEncoder().encode(catalog) else { return errSecParam }
        guard let accessGroup = accessGroup(bundle: bundle) else { return errSecMissingEntitlement }
        let query = baseQuery(accessGroup: accessGroup)
        let values: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let updateStatus = SecItemUpdate(query as CFDictionary, values as CFDictionary)
        if updateStatus == errSecSuccess { return errSecSuccess }
        guard updateStatus == errSecItemNotFound else { return updateStatus }
        var item = query
        item.merge(values) { _, newValue in newValue }
        return SecItemAdd(item as CFDictionary, nil)
    }

    @discardableResult
    static func clear(bundle: Bundle = .main) -> OSStatus {
        guard let accessGroup = accessGroup(bundle: bundle) else { return errSecMissingEntitlement }
        let status = SecItemDelete(baseQuery(accessGroup: accessGroup) as CFDictionary)
        return status == errSecItemNotFound ? errSecSuccess : status
    }

    private static func accessGroup(bundle: Bundle) -> String? {
        let configured = bundle.object(forInfoDictionaryKey: infoKey) as? String
        let trimmed = configured?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return !trimmed.isEmpty && !trimmed.contains("$(") ? trimmed : nil
    }

    private static func baseQuery(accessGroup: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrAccessGroup as String: accessGroup,
        ]
    }
}

// MARK: - 启动包参数

/// 场景启动包（按需启动信封）里与切换有关的参数名。旧扩展忽略这些参数。
enum ScenarioStartParameter {
    static let scenarioID = "scenario_id"
    /// 实时活动上显示的场景名。
    static let scenarioName = "scenario_name"
    static let anyConnectLineID = "anyconnect_line_id"
    static let anyConnectRuntimeIdentity = "anyconnect_runtime_identity"
    static let tailscaleLineID = "tailscale_line_id"
    static let tailscaleRuntimeIdentity = "tailscale_runtime_identity"
}
