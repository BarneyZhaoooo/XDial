import Foundation
import Network
import NetworkExtension
import Observation
@preconcurrency import Libbox

/// 当前网络的两条线索：连着哪个 Wi-Fi，流量从哪个国家或地区出去。
struct NetworkSurroundings: Equatable, Sendable {
    var ssid: String?
    /// 两位地区代码，例如 JP、US；没查出来为空。
    var countryCode: String?
}

/// 按当前网络挑场景。纯逻辑，方便测试。
enum ScenarioAutoPicker {
    struct Pick: Equatable, Sendable {
        enum Reason: Equatable, Sendable {
            /// 连上了某个场景登记过的 Wi-Fi。
            case wifi(String)
            /// 按网络出口所在地。
            case country(String)
        }

        let scenarioID: String
        let reason: Reason
    }

    /// 先看 Wi-Fi：连着哪个场景登记过的 Wi-Fi，就用哪个场景。
    /// 否则看网络出口所在的地区：regionScenarios 里列了这个地区就用对应的场景，
    /// 没列就用 "*" 对应的场景。两条线索都没有、或者对应的场景已经被删掉，就不动。
    static func pick(
        for surroundings: NetworkSurroundings,
        scenarios: [Scenario],
        regionScenarios: [String: String] = [:]
    ) -> Pick? {
        struct WirePick: Decodable {
            let scenario_id: String
            let reason: String
        }
        guard let scenariosData = try? JSONEncoder().encode(scenarios),
              let regionsData = try? JSONEncoder().encode(regionScenarios),
              let scenariosJSON = String(data: scenariosData, encoding: .utf8),
              let regionsJSON = String(data: regionsData, encoding: .utf8) else { return nil }
        var error: NSError?
        let raw = LibboxPickScenario(
            scenariosJSON, regionsJSON, surroundings.ssid ?? "", surroundings.countryCode ?? "", &error
        )
        guard error == nil,
              let data = raw.data(using: .utf8),
              let result = try? JSONDecoder().decode(WirePick.self, from: data) else { return nil }
        let reason: Pick.Reason = result.reason == "wifi"
            ? .wifi((surroundings.ssid ?? "").trimmingCharacters(in: .whitespacesAndNewlines))
            : .country((surroundings.countryCode ?? "").uppercased())
        return Pick(scenarioID: result.scenario_id, reason: reason)
    }
}

/// 最近一次自动判断的经过，写进「XDial 诊断」。不记录 Wi-Fi 名称。
struct ScenarioSurveyRecord: Equatable, Sendable {
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
        return isChinese
            ? "\(time) \(outcome)；Wi-Fi \(wifi)；出口 \(exitText)"
            : "\(time) \(outcome); Wi-Fi \(wifi); exit \(exitText)"
    }

    private static let timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm:ss"
        return formatter
    }()
}

/// 观察当前网络并给出该用哪个场景。只在 App 开着的时候工作。
@Observable
@MainActor
final class ScenarioAutoPilot {
    /// 最近一次判断的结果；还没判断过、或者没判断出来时为空。
    private(set) var pick: ScenarioAutoPicker.Pick?
    private(set) var isSurveying = false
    /// 判断过至少一次（用来区分「还在判断」和「没判断出来」）。
    private(set) var hasSurveyed = false
    /// 最近一次判断的经过。设置页的诊断和主屏不在同一个视图里，所以放在共享位置。
    static private(set) var lastRecord: ScenarioSurveyRecord?
    /// 网络变了（换了 Wi-Fi、Wi-Fi 换成蜂窝）时调用，由主屏决定要不要重新判断。
    @ObservationIgnored var onNetworkChange: (() -> Void)?

    @ObservationIgnored private var monitor: NWPathMonitor?
    @ObservationIgnored private var lastPathSignature: String?
    @ObservationIgnored private var changeDebounce: Task<Void, Never>?
    @ObservationIgnored private var recentSwitches: [AutoSwitch] = []

    /// 一次自动换场景的记录，用来防止来回切。
    struct AutoSwitch: Equatable, Sendable {
        let from: String
        let to: String
        let at: Date
    }

    /// 已连接时要不要自动换到 target。刚刚才从 target 自动换走的话就不再换回去——
    /// 两次判断结果打架时（比如查出口的请求没能绕开隧道），宁可停下来提示，也不能反复断线重连。
    func mayAutoSwitch(to target: String, now: Date = Date()) -> Bool {
        recentSwitches.removeAll { now.timeIntervalSince($0.at) > 600 }
        return !recentSwitches.contains { $0.from == target }
    }

    func noteAutoSwitch(from: String, to: String, now: Date = Date()) {
        recentSwitches.append(AutoSwitch(from: from, to: to, at: now))
    }
    @ObservationIgnored private var running: Task<ScenarioAutoPicker.Pick?, Never>?
    @ObservationIgnored private var lastSurveyAt: Date?

    /// 最近一次判断是不是还新鲜（点连接时用，避免每次都等）。
    var isFresh: Bool {
        guard let lastSurveyAt else { return false }
        return Date().timeIntervalSince(lastSurveyAt) < 20
    }

    func start() {
        guard monitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            // 只关心物理网络换没换；VPN 自己的虚拟网卡上上下下不算。
            let physical = path.availableInterfaces
                .filter { $0.type == .wifi || $0.type == .cellular || $0.type == .wiredEthernet }
                .map { "\($0.type):\($0.name)" }
                .joined(separator: ",")
            // 换一个 Wi-Fi 时网卡不变、网关会变，所以把网关也算进去。
            let gateways = path.gateways.map { "\($0)" }.joined(separator: ",")
            let signature = "\(path.status):\(physical):\(gateways)"
            Task { @MainActor [weak self] in
                guard let self else { return }
                guard signature != self.lastPathSignature else { return }
                let isFirst = self.lastPathSignature == nil
                self.lastPathSignature = signature
                guard !isFirst else { return }
                // 换网络时系统会连着报好几次，等它稳定下来再判断一次。
                self.lastSurveyAt = nil
                self.changeDebounce?.cancel()
                self.changeDebounce = Task { @MainActor [weak self] in
                    try? await Task.sleep(for: .seconds(1.5))
                    guard !Task.isCancelled else { return }
                    self?.onNetworkChange?()
                }
            }
        }
        monitor.start(queue: DispatchQueue(label: "xdial.scenario-autopilot.path"))
        self.monitor = monitor
    }

    func stop() {
        monitor?.cancel()
        monitor = nil
        lastPathSignature = nil
        changeDebounce?.cancel()
        changeDebounce = nil
    }

    /// 看一眼当前网络，返回该用的场景。同时来的多次调用合并成一次。
    /// regionScenarios 为空时只看 Wi-Fi，不去查网络出口。
    /// 已连接时传入 exitCountryViaTunnel：这时 App 自己的请求按场景分流、域名解析也归隧道，
    /// 只有 VPN 扩展经 Direct 线路才查得到本地网络的出口。
    @discardableResult
    func survey(
        scenarios: [Scenario],
        regionScenarios: [String: String] = [:],
        exitCountryViaTunnel: (@MainActor () async -> ExitCountryProbeResult)? = nil
    ) async -> ScenarioAutoPicker.Pick? {
        if let running { return await running.value }
        isSurveying = true
        let task = Task { () -> ScenarioAutoPicker.Pick? in
            let ssid = await NetworkProbe.currentSSID()
            let ssidRead = !(ssid?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
            // 已经靠 Wi-Fi 认出来了，或者没有按地区选场景的配置，就不用去查出口。
            if let pick = ScenarioAutoPicker.pick(
                for: NetworkSurroundings(ssid: ssid, countryCode: nil),
                scenarios: scenarios
            ) {
                Self.lastRecord = ScenarioSurveyRecord(
                    at: Date(), ssidRead: ssidRead, ssidMatched: true, exit: .notNeeded, picked: true
                )
                return pick
            }
            guard !regionScenarios.isEmpty else {
                Self.lastRecord = ScenarioSurveyRecord(
                    at: Date(), ssidRead: ssidRead, ssidMatched: false, exit: .notNeeded, picked: false
                )
                return nil
            }
            let viaTunnel = exitCountryViaTunnel != nil
            let lookup: ExitCountryProbeResult
            if let exitCountryViaTunnel {
                lookup = await exitCountryViaTunnel()
            } else {
                lookup = await NetworkProbe.exitCountryCode()
            }
            let pick = ScenarioAutoPicker.pick(
                for: NetworkSurroundings(ssid: ssid, countryCode: lookup.code),
                scenarios: scenarios,
                regionScenarios: regionScenarios
            )
            Self.lastRecord = ScenarioSurveyRecord(
                at: Date(),
                ssidRead: ssidRead,
                ssidMatched: false,
                exit: lookup.code.map { .found(code: $0, viaTunnel: viaTunnel) }
                    ?? .failed(viaTunnel: viaTunnel, reason: lookup.failure),
                picked: pick != nil
            )
            return pick
        }
        running = task
        let result = await task.value
        running = nil
        // 这次没查出来（比如刚好断网）就保留上一次的结论，不把界面上的说明清掉。
        if let result { pick = result }
        isSurveying = false
        hasSurveyed = true
        lastSurveyAt = Date()
        return result
    }
}

/// 读 Wi-Fi 名称、查网络出口所在地。
enum NetworkProbe {
    static func currentSSID() async -> String? {
        #if targetEnvironment(simulator)
        // 模拟器读不到 Wi-Fi：-XDialFakeSSID <名称> 用来演示。
        return launchArgument("-XDialFakeSSID")
        #else
        // 需要「访问 Wi-Fi 信息」权限；App 装了 VPN 配置，所以不用再要定位授权。
        return await withCheckedContinuation { continuation in
            NEHotspotNetwork.fetchCurrent { network in
                continuation.resume(returning: network?.ssid)
            }
        }
        #endif
    }

    /// 没连接时网络出口所在的国家或地区。先问苹果，问不到再问 Cloudflare。
    /// 请求绑在物理网卡上。已连接时不用它：系统把域名解析交给隧道，禁止走隧道的请求解析不了，
    /// 改由 VPN 扩展经 Direct 线路查（见 ScenarioAutoPilot.survey）。
    /// 两处都没查到时，failure 写明各自是没有回应还是回复无效，供诊断使用。
    static func exitCountryCode() async -> ExitCountryProbeResult {
        #if targetEnvironment(simulator)
        if let fake = launchArgument("-XDialFakeCountry") {
            return ExitCountryProbeResult(code: fake.uppercased(), failure: nil)
        }
        #endif
        let appleReply = await DirectHTTPSProbe(host: "gspe1-ssl.ls.apple.com", path: "/pep/gcc").run()
        if let code = appleReply.flatMap(countryCode(inAppleReply:)) {
            return ExitCountryProbeResult(code: code, failure: nil)
        }
        let cloudflareReply = await DirectHTTPSProbe(host: "www.cloudflare.com", path: "/cdn-cgi/trace").run()
        if let code = cloudflareReply.flatMap(countryCode(inCloudflareTrace:)) {
            return ExitCountryProbeResult(code: code, failure: nil)
        }
        return ExitCountryProbeResult(code: nil, failure: localFailure(apple: appleReply, cloudflare: cloudflareReply))
    }

    /// 例如「Apple 无回应；Cloudflare 回复无效」。只说结果类别，不带回复内容。
    static func localFailure(apple: String?, cloudflare: String?) -> String {
        func describe(_ reply: String?) -> String { reply == nil ? "无回应" : "回复无效" }
        return "Apple \(describe(apple))；Cloudflare \(describe(cloudflare))"
    }

    /// 苹果的回复正文就是两位地区代码。
    static func countryCode(inAppleReply text: String) -> String? {
        guard text.hasPrefix("HTTP/1.1 200") || text.hasPrefix("HTTP/1.0 200"),
              let bodyStart = text.range(of: "\r\n\r\n") else { return nil }
        return text[bodyStart.upperBound...]
            .split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .first { $0.count == 2 && $0.allSatisfy { $0.isASCII && $0.isUppercase } }
    }

    /// Cloudflare 的回复里有一行 loc=XX。
    static func countryCode(inCloudflareTrace text: String) -> String? {
        for line in text.split(whereSeparator: \.isNewline) where line.hasPrefix("loc=") {
            let code = line.dropFirst(4).trimmingCharacters(in: .whitespaces).uppercased()
            // XX、T1 是 Cloudflare 表示「不知道」的占位值。
            if code.count == 2, code != "XX", code != "T1", code.allSatisfy(\.isLetter) { return code }
        }
        return nil
    }

    #if targetEnvironment(simulator)
    private static func launchArgument(_ name: String) -> String? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let index = arguments.firstIndex(of: name), arguments.indices.contains(index + 1) else { return nil }
        let value = arguments[index + 1]
        return value.isEmpty ? nil : value
    }
    #endif
}

/// 一次最简单的 HTTPS GET，连接禁止走虚拟网卡（VPN 隧道），只能从 Wi-Fi 或蜂窝网络直接出去。
private final class DirectHTTPSProbe: @unchecked Sendable {
    // 所有可变状态都只在 queue 上读写。
    private let queue = DispatchQueue(label: "xdial.direct-https-probe")
    private let connection: NWConnection
    private let request: Data
    private var buffer = Data()
    private var continuation: CheckedContinuation<String?, Never>?

    init(host: String, path: String) {
        let parameters = NWParameters.tls
        parameters.prohibitedInterfaceTypes = [.other, .loopback]
        connection = NWConnection(host: NWEndpoint.Host(host), port: 443, using: parameters)
        request = Data(
            "GET \(path) HTTP/1.1\r\nHost: \(host)\r\nUser-Agent: XDial\r\nAccept: */*\r\nConnection: close\r\n\r\n".utf8
        )
    }

    func run(timeout: TimeInterval = 2.5) async -> String? {
        await withCheckedContinuation { continuation in
            queue.async {
                self.continuation = continuation
                self.connection.stateUpdateHandler = { [weak self] state in
                    guard let self else { return }
                    switch state {
                    case .ready:
                        self.send()
                    case .failed, .cancelled:
                        self.finish()
                    default:
                        break
                    }
                }
                self.connection.start(queue: self.queue)
                self.queue.asyncAfter(deadline: .now() + timeout) { self.finish() }
            }
        }
    }

    private func send() {
        connection.send(content: request, completion: .contentProcessed { [weak self] error in
            guard let self else { return }
            if error != nil { self.finish() } else { self.receive() }
        })
    }

    private func receive() {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 16_384) { [weak self] data, _, isComplete, error in
            guard let self else { return }
            if let data { self.buffer.append(data) }
            if isComplete || error != nil || self.buffer.count > 32_768 {
                self.finish()
            } else {
                self.receive()
            }
        }
    }

    private func finish() {
        guard let continuation else { return }
        self.continuation = nil
        connection.cancel()
        continuation.resume(returning: buffer.isEmpty ? nil : String(decoding: buffer, as: UTF8.self))
    }
}
