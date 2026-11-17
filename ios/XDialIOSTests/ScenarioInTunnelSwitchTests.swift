import Combine
import XCTest
@testable import XDial

// MARK: - 共用规则

final class ScenarioAutoSwitchRuleTests: XCTestCase {
    func testGuardBlocksBouncingBackOnTheSameNetworkOnly() {
        var guardState = ScenarioAutoSwitchGuard()
        let start = Date(timeIntervalSince1970: 1_000)
        XCTAssertTrue(guardState.mayAutoSwitch(to: "outside", network: "wifi:en0:gw-a", now: start))
        guardState.note(from: "office", to: "outside", network: "wifi:en0:gw-a", now: start)

        // 同一个网络里又判断回办公室：两次判断打架，不再切回。
        XCTAssertFalse(guardState.mayAutoSwitch(
            to: "office", network: "wifi:en0:gw-a", now: start.addingTimeInterval(60)
        ))
        // 真的回到了另一个网络（换了网关）：照常切换。
        XCTAssertTrue(guardState.mayAutoSwitch(
            to: "office", network: "wifi:en0:gw-b", now: start.addingTimeInterval(60)
        ))
        // 不带网络特征（App 自己断开重连的旧路径）时维持原来的十分钟规则。
        XCTAssertFalse(guardState.mayAutoSwitch(to: "office", now: start.addingTimeInterval(60)))
        XCTAssertTrue(guardState.mayAutoSwitch(
            to: "office", network: "wifi:en0:gw-a", now: start.addingTimeInterval(601)
        ))
    }

    func testPolicyListsRegisteredWiFiInScenarioOrder() {
        var profile = Profile.bootstrap()
        profile.scenarios = [
            Scenario(id: "office", name: "办公室", matchSSIDs: [" Office ", ""], defaultLineID: "vpn"),
            Scenario(id: "home", name: "家", defaultLineID: "direct"),
            Scenario(id: "cafe", name: "咖啡馆", matchSSIDs: ["Office", "Cafe"], defaultLineID: "direct"),
        ]
        let policy = ScenarioAutoPolicy.make(profile: profile, enabled: true, regionScenarios: ["JP": "home", "*": "gone"])
        XCTAssertEqual(policy.wifiRules, [
            .init(scenarioID: "office", ssids: ["Office"]),
            .init(scenarioID: "cafe", ssids: ["Office", "Cafe"]),
        ])
        XCTAssertEqual(policy.scenarioIDs, ["office", "home", "cafe"])
        XCTAssertEqual(policy.regionScenarioIDs, ["home"])
        XCTAssertEqual(ScenarioAutoDecision.wifiPick(ssid: "Office", rules: policy.wifiRules)?.scenarioID, "office")
        // 对应表指向已删除的场景时不选。
        XCTAssertNil(ScenarioAutoDecision.regionPick(
            countryCode: "NZ", regionScenarios: policy.regionScenarios, scenarioIDs: policy.scenarioIDs
        ))
    }

    func testTunnelStateRoundTripsEveryRecordedFact() throws {
        var guardState = ScenarioAutoSwitchGuard()
        guardState.note(from: "a", to: "b", network: "n", now: Date(timeIntervalSince1970: 10))
        let records: [ScenarioSurveyRecord.ExitLookup] = [
            .notNeeded,
            .found(code: "JP", viaTunnel: true),
            .failed(viaTunnel: true, reason: "timeout"),
            .failed(viaTunnel: false, reason: nil),
        ]
        for exit in records {
            let state = TunnelScenarioState(
                runningScenarioID: "b",
                lastPick: ScenarioAutoPick(scenarioID: "b", reason: .country("JP")),
                lastSurvey: ScenarioSurveyRecord(
                    at: Date(timeIntervalSince1970: 20), ssidRead: true, ssidMatched: false,
                    exit: exit, picked: true, byTunnel: true
                ),
                suggestedScenarioID: "c",
                lastSwitch: .init(
                    from: "a", to: "b", origin: .automatic, mode: .staged,
                    at: Date(timeIntervalSince1970: 30), succeeded: true, failure: nil
                ),
                switchingTo: nil,
                guardState: guardState,
                updatedAt: Date(timeIntervalSince1970: 40)
            )
            let data = try XCTUnwrap(state.encoded())
            XCTAssertEqual(TunnelScenarioState.decode(data), state)
        }
        let wifi = ScenarioAutoPick(scenarioID: "office", reason: .wifi("Office"))
        let decoded = try JSONDecoder().decode(ScenarioAutoPick.self, from: JSONEncoder().encode(wifi))
        XCTAssertEqual(decoded, wifi)
    }

    func testSurveySummaryNamesTheTunnelWithoutTheWiFiName() {
        let record = ScenarioSurveyRecord(
            at: Date(), ssidRead: true, ssidMatched: true, exit: .notNeeded, picked: true, byTunnel: true
        )
        XCTAssertTrue(record.summary(isChinese: true).contains("VPN 扩展"))
        XCTAssertTrue(record.summary(isChinese: false).contains("VPN extension"))
    }

    func testCatalogRejectsTooManyOrTooLargeEntries() {
        let policy = ScenarioAutoPolicy(enabled: true, wifiRules: [], regionScenarios: [:], scenarioIDs: [])
        XCTAssertTrue(ScenarioSwitchCatalog(policy: policy, entries: ["a": Data("{}".utf8)]).isValid)
        let many = Dictionary(uniqueKeysWithValues: (0...ScenarioSwitchCatalog.maxEntries).map { ("s\($0)", Data()) })
        XCTAssertFalse(ScenarioSwitchCatalog(policy: policy, entries: many).isValid)
        let huge = Data(count: ScenarioSwitchCatalog.maxTotalBytes + 1)
        XCTAssertFalse(ScenarioSwitchCatalog(policy: policy, entries: ["a": huge]).isValid)
    }
}

// MARK: - App 侧的隧道内切换

@MainActor
private final class SwitchSpyEngine: TunnelEngine, ObservableObject {
    @Published private(set) var status = "disconnected"
    var lastError: String?
    var dataPathSummary: String?
    var statusPublisher: AnyPublisher<String, Never> { $status.eraseToAnyPublisher() }
    var isConnected: Bool { status == "connected" }
    var isBusy: Bool { status == "connecting" || status == "disconnecting" }

    func start(profileJSON: String) { status = "connected" }
    func stop() { status = "disconnected" }
    func syncStatus() {}
    func setStatus(_ value: String) { status = value }
}

@MainActor
private final class SwitchSpyManager: TunnelManaging {
    let isProfileInstalled = true
    private weak var engine: SwitchSpyEngine?
    private(set) var catalogBuilds: [(ids: [String], policy: ScenarioAutoPolicy, keepingOthers: Bool)] = []
    private(set) var policyUpdates: [ScenarioAutoPolicy] = []
    private(set) var switchRequests: [(scenarioID: String, revision: String)] = []
    private(set) var adoptedScenarios: [String] = []
    private(set) var stopCallCount = 0
    var switchReply: ScenarioSwitchReply = .unsupported
    var evaluationState: TunnelScenarioState?
    var evaluationUnsupported = false

    init(engine: SwitchSpyEngine) {
        self.engine = engine
    }

    func refreshProfileStatus(completion: @escaping @Sendable (Bool) -> Void) { completion(true) }

    func startTunnel(
        profile: Profile,
        anyConnect: AnyConnectCredentials?,
        allowSystemOnDemand: Bool,
        completion: @escaping @Sendable (Result<Void, Error>) -> Void
    ) {
        engine?.setStatus("connected")
        completion(.success(()))
    }

    func stopTunnel() {
        stopCallCount += 1
        engine?.setStatus("disconnected")
    }

    func rebuildScenarioCatalog(
        inputs: [ScenarioCatalogInput],
        policy: ScenarioAutoPolicy,
        keepingOthers: Bool
    ) async -> String? {
        catalogBuilds.append((inputs.map(\.scenarioID), policy, keepingOthers))
        return "revision-\(catalogBuilds.count)"
    }

    func updateScenarioCatalogPolicy(_ policy: ScenarioAutoPolicy) -> Bool {
        policyUpdates.append(policy)
        return true
    }

    func requestScenarioSwitch(to scenarioID: String, revision: String) async -> ScenarioSwitchReply {
        switchRequests.append((scenarioID, revision))
        return switchReply
    }

    func requestScenarioEvaluation() async -> ScenarioEvaluationReply {
        if evaluationUnsupported { return .unsupported }
        return evaluationState.map(ScenarioEvaluationReply.evaluated) ?? .unavailable
    }
    func requestScenarioState() async -> TunnelScenarioState? { evaluationState }
    func adoptSwitchedScenario(_ scenarioID: String) { adoptedScenarios.append(scenarioID) }
}

@MainActor
final class ScenarioInTunnelSwitchTests: XCTestCase {
    private var autoEnabled = true

    private func makeApp() -> (AppState, SwitchSpyEngine, SwitchSpyManager) {
        let persistence = AppPersistenceContext.testing(identifier: "switch-" + UUID().uuidString)
        XCTAssertTrue(persistence.clearForTesting())
        addTeardownBlock { _ = persistence.clearForTesting() }
        let engine = SwitchSpyEngine()
        let manager = SwitchSpyManager(engine: engine)
        let app = AppState(engine: engine, tunnelManager: manager, persistence: persistence)
        var profile = Profile.bootstrap()
        profile.lines[1].vpnServer = "gateway.example.com"
        profile.lines[1].vpnUsername = "tester"
        profile.lines[1].vpnPassword = "secret"
        profile.scenarios = [
            Scenario(id: "office", name: "办公室", matchSSIDs: ["Office"], defaultLineID: "direct"),
            Scenario(id: "outside", name: "外面", defaultLineID: "vpn"),
            Scenario(id: "manual", name: "手动", defaultLineID: "direct"),
        ]
        profile.activeScenarioID = "office"
        profile.ensureConnectivityTestConfiguration()
        app.profile = profile
        XCTAssertTrue(app.save())
        app.scenarioAutoPolicyProvider = { [unowned self] profile in
            ScenarioAutoPolicy.make(profile: profile, enabled: self.autoEnabled, regionScenarios: ["*": "outside"])
        }
        return (app, engine, manager)
    }

    private func settle() async {
        for _ in 0..<5 { await Task.yield() }
    }

    private func state(running: String, suggested: String? = nil) -> TunnelScenarioState {
        TunnelScenarioState(
            runningScenarioID: running,
            lastPick: ScenarioAutoPick(scenarioID: running, reason: .country("NZ")),
            suggestedScenarioID: suggested
        )
    }

    func testConnectingPreparesEveryScenarioTheExtensionMayPick() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()

        XCTAssertTrue(app.isConnected)
        let build = try? XCTUnwrap(manager.catalogBuilds.first)
        // 当前场景、登记了 Wi-Fi 的场景、地区表里的场景；只能手动选的场景不需要。
        XCTAssertEqual(build?.ids, ["office", "outside"])
        XCTAssertEqual(build?.keepingOthers, false)
        XCTAssertEqual(build?.policy.enabled, true)
    }

    func testManualSwitchKeepsTheVPNUpAndAdoptsTheNewScenario() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()
        manager.switchReply = .switched(state(running: "manual"))

        let result = await app.switchScenarioInTunnel(to: "manual")
        await settle()

        XCTAssertEqual(result, .switched)
        XCTAssertEqual(manager.stopCallCount, 0, "switching must not stop the system tunnel")
        XCTAssertEqual(manager.switchRequests.map(\.scenarioID), ["manual"])
        // 切换前只生成目标场景，保留目录里其他场景。
        XCTAssertEqual(manager.catalogBuilds[1].ids, ["manual"])
        XCTAssertEqual(manager.catalogBuilds[1].keepingOthers, true)
        XCTAssertEqual(manager.switchRequests.first?.revision, "revision-2")
        XCTAssertEqual(app.profile.activeScenarioID, "manual")
        XCTAssertFalse(app.hasPendingRuntimeChanges)
        XCTAssertEqual(manager.adoptedScenarios, ["manual"])
        // 切完按新配置补齐整个目录。
        XCTAssertEqual(manager.catalogBuilds.last?.ids.first, "manual")
        XCTAssertEqual(manager.catalogBuilds.last?.keepingOthers, false)
    }

    func testFailedSwitchKeepsTheRunningScenarioAndExplainsWhy() async {
        let (app, engine, manager) = makeApp()
        app.connect()
        await settle()
        manager.switchReply = .failed("新场景的线路暂时不可用：timeout", state(running: "office"))

        let result = await app.switchScenarioInTunnel(to: "outside")

        XCTAssertEqual(result, .failed)
        XCTAssertEqual(app.profile.activeScenarioID, "office")
        XCTAssertTrue(app.isConnected)
        XCTAssertEqual(engine.lastError, "新场景的线路暂时不可用：timeout")
        XCTAssertTrue(manager.adoptedScenarios.isEmpty)
    }

    func testOldExtensionFallsBackToTheCaller() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()
        manager.switchReply = .unsupported

        let result = await app.switchScenarioInTunnel(to: "manual")

        XCTAssertEqual(result, .unavailable)
        XCTAssertEqual(app.profile.activeScenarioID, "office")
    }

    func testBackgroundSwitchIsAdoptedWhenTheAppAsks() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()
        manager.evaluationState = state(running: "outside")

        let handled = await app.requestTunnelScenarioEvaluation()

        XCTAssertEqual(handled, .handled)
        XCTAssertEqual(app.profile.activeScenarioID, "outside")
        XCTAssertEqual(app.tunnelScenarioState?.runningScenarioID, "outside")
        XCTAssertFalse(app.hasPendingRuntimeChanges)
        XCTAssertEqual(manager.adoptedScenarios, ["outside"])
    }

    func testEvaluationIsNotHandledWhileDisconnected() async {
        let (app, _, manager) = makeApp()
        manager.evaluationState = state(running: "outside")
        let handled = await app.requestTunnelScenarioEvaluation()
        XCTAssertEqual(handled, .unavailable)
        XCTAssertEqual(app.profile.activeScenarioID, "office")
    }

    func testSlowExtensionIsNotMistakenForAnOldOne() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()
        // 扩展没回应：不能退回断开重连。
        manager.evaluationState = nil
        let slow = await app.requestTunnelScenarioEvaluation()
        XCTAssertEqual(slow, .unavailable)
        manager.evaluationUnsupported = true
        let old = await app.requestTunnelScenarioEvaluation()
        XCTAssertEqual(old, .unsupported)
        XCTAssertEqual(manager.stopCallCount, 0)
    }

    func testTurningAutoOffReachesTheExtension() async {
        let (app, _, manager) = makeApp()
        app.connect()
        await settle()
        autoEnabled = false

        app.updateScenarioAutoPolicy()

        XCTAssertEqual(manager.policyUpdates.last?.enabled, false)
    }
}
