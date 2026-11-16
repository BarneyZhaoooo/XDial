import XCTest
@testable import XDial

final class ScenarioAutoPilotTests: XCTestCase {
    private let scenarios = SeedFixture.profile().scenarios
    private let regions = ["JP": "home-region", "SG": "harbor", "*": "abroad"]

    private func pick(ssid: String? = nil, country: String? = nil) -> ScenarioAutoPicker.Pick? {
        ScenarioAutoPicker.pick(
            for: NetworkSurroundings(ssid: ssid, countryCode: country),
            scenarios: scenarios,
            regionScenarios: regions
        )
    }

    func testRegisteredWiFiWinsOverWhereTheNetworkExits() {
        XCTAssertEqual(pick(ssid: "Office", country: "JP")?.scenarioID, "office")
        XCTAssertEqual(pick(ssid: "Office")?.reason, .wifi("Office"))
        // Wi-Fi 名称区分大小写，别的 Wi-Fi 不算。
        XCTAssertEqual(pick(ssid: "office", country: "JP")?.scenarioID, "home-region")
        XCTAssertEqual(pick(ssid: "Office-Guest", country: "SG")?.scenarioID, "harbor")
    }

    func testOtherwiseTheExitRegionDecides() {
        XCTAssertEqual(pick(country: "JP")?.scenarioID, "home-region")
        XCTAssertEqual(pick(country: "SG")?.scenarioID, "harbor")
        // 没列出的地区用 "*"。
        XCTAssertEqual(pick(country: "NZ")?.scenarioID, "abroad")
        XCTAssertEqual(pick(country: "nz")?.scenarioID, "abroad")
        XCTAssertEqual(pick(ssid: "Home", country: "NZ")?.reason, .country("NZ"))
    }

    func testNoClueOrMissingScenarioLeavesTheChoiceAlone() {
        XCTAssertNil(pick())
        XCTAssertNil(pick(ssid: "Home"))
        XCTAssertNil(pick(country: "日本"))
        // 用户把对应的场景删了：不硬塞别的场景。
        let withoutHarbor = scenarios.filter { $0.id != "harbor" }
        XCTAssertNil(ScenarioAutoPicker.pick(
            for: NetworkSurroundings(ssid: nil, countryCode: "SG"),
            scenarios: withoutHarbor,
            regionScenarios: regions
        ))
        // 没有按地区选场景的配置（公开构建）：只认 Wi-Fi。
        XCTAssertNil(ScenarioAutoPicker.pick(
            for: NetworkSurroundings(ssid: nil, countryCode: "JP"),
            scenarios: scenarios
        ))
        XCTAssertEqual(
            ScenarioAutoPicker.pick(for: NetworkSurroundings(ssid: "Office", countryCode: "JP"), scenarios: scenarios)?.scenarioID,
            "office"
        )
    }

    func testUserScenariosCanClaimTheirOwnWiFi() {
        var custom = scenarios
        custom.append(Scenario(id: "home", name: "家里", matchSSIDs: ["MyHome", " Cafe "], defaultLineID: "direct"))
        let surroundings = NetworkSurroundings(ssid: "Cafe", countryCode: "JP")
        XCTAssertEqual(
            ScenarioAutoPicker.pick(for: surroundings, scenarios: custom, regionScenarios: regions)?.scenarioID,
            "home"
        )
    }

    @MainActor
    func testAutoSwitchDoesNotBounceBackAndForth() {
        let pilot = ScenarioAutoPilot()
        let start = Date(timeIntervalSince1970: 1_000_000)
        XCTAssertTrue(pilot.mayAutoSwitch(to: "abroad", now: start))
        pilot.noteAutoSwitch(from: "home-region", to: "abroad", now: start)

        // 刚从一个场景自动换走：一分钟后又判断成它，不再自动换回去。
        XCTAssertFalse(pilot.mayAutoSwitch(to: "home-region", now: start.addingTimeInterval(60)))
        // 换到第三个场景不受影响。
        XCTAssertTrue(pilot.mayAutoSwitch(to: "harbor", now: start.addingTimeInterval(60)))
        // 过了十分钟，算是真的换了地方。
        XCTAssertTrue(pilot.mayAutoSwitch(to: "home-region", now: start.addingTimeInterval(601)))
    }

    func testCountryRepliesAreParsed() {
        XCTAssertEqual(
            NetworkProbe.countryCode(inAppleReply: "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nHK"),
            "HK"
        )
        // 分块传输的正文。
        XCTAssertEqual(
            NetworkProbe.countryCode(inAppleReply: "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n2\r\nCN\r\n0\r\n\r\n"),
            "CN"
        )
        XCTAssertNil(NetworkProbe.countryCode(inAppleReply: "HTTP/1.1 403 Forbidden\r\n\r\nNO"))
        XCTAssertNil(NetworkProbe.countryCode(inAppleReply: "garbage"))

        XCTAssertEqual(
            NetworkProbe.countryCode(inCloudflareTrace: "HTTP/1.1 200 OK\r\n\r\nfl=1\nip=203.0.113.9\nloc=NZ\ncolo=AKL\n"),
            "NZ"
        )
        XCTAssertNil(NetworkProbe.countryCode(inCloudflareTrace: "fl=1\nloc=XX\n"))
        XCTAssertNil(NetworkProbe.countryCode(inCloudflareTrace: "fl=1\ncolo=AKL\n"))
    }

    func testRoutingCheckFailureSaysWhatWasSeen() {
        let before = RoutingProbeSnapshot(
            directTargetDirect: 0, directTargetVPN: 0, directTargetOther: 0,
            anyConnectTargetDirect: 0, anyConnectTargetVPN: 0, anyConnectTargetOther: 0,
            directTargetTags: ["direct": 2], anyConnectTargetTags: [:]
        )
        let after = RoutingProbeSnapshot(
            directTargetDirect: 0, directTargetVPN: 0, directTargetOther: 0,
            anyConnectTargetDirect: 0, anyConnectTargetVPN: 0, anyConnectTargetOther: 0,
            directTargetTags: ["direct": 3, "vpn": 1], anyConnectTargetTags: [:]
        )
        XCTAssertEqual(
            routingHitDetail(
                before: before, after: after,
                labels: ["direct": "直连", "vpn": "AnyConnect"],
                directReply: nil, currentReply: .some("203.0.113.9")
            ),
            "；命中明细：1.0.0.1 直连×1、AnyConnect×1 · 1.1.1.1 没有记到；测试请求：1.0.0.1 无回应 · 1.1.1.1 回应 203.0.113.9"
        )
        XCTAssertEqual(
            routingHitDetail(before: nil, after: after, labels: [:], directReply: "198.51.100.7", currentReply: nil),
            "；命中明细：VPN 扩展没有返回计数；测试请求：1.0.0.1 回应 198.51.100.7"
        )
    }

    func testBundledRuleSetsSeedTheCacheWhereTheEngineLooks() throws {
        // 文件名必须和引擎（Go 里的 neRuleSetCachePath）算出来的一样，否则放了也白放。
        // 期望值是 sha256("profile\0region-ips\0<地址>\0binary") 的前 12 个字节。
        let url = "https://rules.example.com/region-ips.srs"
        XCTAssertEqual(
            BundledRuleSets.cacheFileName(ruleSetID: "region-ips", url: url),
            "c3ef40eb94856fabfc648e47.srs"
        )

        // 用一个临时目录冒充 App 包：里面有清单和一份规则文件。
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("xdial-seed-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: root) }
        let bundleURL = root.appendingPathComponent("Fake.bundle")
        let ruleSets = bundleURL.appendingPathComponent("PrivateConfig/RuleSets")
        try FileManager.default.createDirectory(at: ruleSets, withIntermediateDirectories: true)
        try JSONEncoder().encode([url: "region-ips.srs"]).write(to: ruleSets.appendingPathComponent("manifest.json"))
        try Data("SRS-bundled".utf8).write(to: ruleSets.appendingPathComponent("region-ips.srs"))
        let bundle = try XCTUnwrap(Bundle(url: bundleURL))
        let base = root.appendingPathComponent("group")
        let profile = SeedFixture.profile()

        // 清单里只有 region-ips：只放这一份，没带文件的 region-domains 不管。
        XCTAssertEqual(BundledRuleSets.seedCache(for: profile, basePath: base.path, bundle: bundle), ["region-ips"])
        let cached = base.appendingPathComponent("xdial-rule-sets")
            .appendingPathComponent(BundledRuleSets.cacheFileName(ruleSetID: "region-ips", url: url))
        XCTAssertEqual(try Data(contentsOf: cached), Data("SRS-bundled".utf8))

        // 已经有缓存（哪怕是引擎后来更新过的）就不再覆盖。
        try Data("newer".utf8).write(to: cached)
        XCTAssertEqual(BundledRuleSets.seedCache(for: profile, basePath: base.path, bundle: bundle), [])
        XCTAssertEqual(try Data(contentsOf: cached), Data("newer".utf8))

        // 没带规则的构建（公开仓库）：什么都不做。
        XCTAssertEqual(BundledRuleSets.seedCache(for: profile, basePath: base.path, bundle: .main), [])
    }
    @MainActor
    func testWhileConnectedTheTunnelAnswersWhereTheNetworkExits() async {
        let pilot = ScenarioAutoPilot()
        var asked = 0
        let pick = await pilot.survey(scenarios: scenarios, regionScenarios: regions) {
            asked += 1
            return ExitCountryProbeResult(code: "SG", failure: nil)
        }
        XCTAssertEqual(asked, 1)
        XCTAssertEqual(pick, ScenarioAutoPicker.Pick(scenarioID: "harbor", reason: .country("SG")))
        XCTAssertEqual(ScenarioAutoPilot.lastRecord?.exit, .found(code: "SG", viaTunnel: true))
        XCTAssertEqual(ScenarioAutoPilot.lastRecord?.picked, true)
    }

    @MainActor
    func testAFailedTunnelLookupIsRecordedForDiagnostics() async {
        let pilot = ScenarioAutoPilot()
        let pick = await pilot.survey(scenarios: scenarios, regionScenarios: regions) {
            ExitCountryProbeResult(code: nil, failure: "连接引擎未运行")
        }
        XCTAssertNil(pick)
        let record = try? XCTUnwrap(ScenarioAutoPilot.lastRecord)
        XCTAssertEqual(record?.exit, .failed(viaTunnel: true, reason: "连接引擎未运行"))
        XCTAssertEqual(record?.ssidRead, false)
        XCTAssertTrue(record?.summary(isChinese: true).hasSuffix(
            "没判断出来；Wi-Fi 未读到；出口 查询失败（经直连线路：连接引擎未运行）"
        ) == true)
    }

    func testALocalLookupFailureSaysWhichServiceDidNotAnswer() {
        XCTAssertEqual(NetworkProbe.localFailure(apple: nil, cloudflare: nil), "Apple 无回应；Cloudflare 无回应")
        XCTAssertEqual(
            NetworkProbe.localFailure(apple: "HTTP/1.1 403 Forbidden\r\n\r\n", cloudflare: nil),
            "Apple 回复无效；Cloudflare 无回应"
        )
    }

    func testTheDiagnosticLineNeverCarriesTheWiFiName() {
        let record = ScenarioSurveyRecord(
            at: Date(), ssidRead: true, ssidMatched: true, exit: .notNeeded, picked: true
        )
        XCTAssertTrue(record.summary(isChinese: true).hasSuffix("已判断；Wi-Fi 已读到并匹配；出口 未查询"))
        let english = ScenarioSurveyRecord(
            at: Date(), ssidRead: false, ssidMatched: false, exit: .found(code: "CN", viaTunnel: false), picked: true
        ).summary(isChinese: false)
        XCTAssertTrue(english.hasSuffix("decided; Wi-Fi not read; exit CN (queried locally)"))
    }
}
