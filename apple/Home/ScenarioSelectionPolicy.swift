import Foundation
import CryptoKit

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

    struct PlaceMatch: Codable, Equatable, Sendable {
        let scenarioID: String
        let expires: Date
    }

    var placeMatch: PlaceMatch? = nil

    func placePick(at now: Date = Date()) -> ScenarioAutoPick? {
        guard enabled, let placeMatch, placeMatch.expires > now,
              scenarioIDs.contains(placeMatch.scenarioID) else { return nil }
        return .init(scenarioID: placeMatch.scenarioID, reason: .place)
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
        case placeMatch = "place_match"
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

/// Include Wi-Fi identity without storing its name in the bounce guard.
enum ScenarioNetworkIdentity {
    static func key(path: String?, ssid: String?) -> String {
        let identity = SHA256.hash(data: Data((ssid ?? "").utf8))
            .map { String(format: "%02x", $0) }.joined()
        return (path ?? "") + ":wifi:" + identity
    }

    /// Short, non-reversible label for "which network was this" in diagnostics:
    /// equal on the same network, without gateway addresses or the Wi-Fi name.
    static func diagnosticToken(path: String?, ssid: String?) -> String {
        opaqueToken(key(path: path, ssid: ssid))
    }

    static func opaqueToken(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8))
            .prefix(3).map { String(format: "%02x", $0) }.joined()
    }
}

/// 一次判断的结论：选哪个场景，凭什么。
struct ScenarioAutoPick: Codable, Equatable, Sendable {
    enum Reason: Equatable, Sendable {
        /// 连上了某个场景登记过的 Wi-Fi。
        case wifi(String)
        /// 按网络出口所在地。
        case country(String)
        case place
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
        switch try container.decode(String.self, forKey: .reasonKind) {
        case "place": reason = .place
        case "wifi": reason = .wifi(try container.decode(String.self, forKey: .reasonValue))
        case "country": reason = .country(try container.decode(String.self, forKey: .reasonValue))
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
        case .place:
            try container.encode("place", forKey: .reasonKind)
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
