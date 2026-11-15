import XCTest

final class ScenarioConnectionPresentationTests: XCTestCase {
    private func report(_ types: [String], state: ConnectionTransactionState = .committed) -> ConnectionReport {
        var report = ConnectionReport(transactionID: "test", plan: ConnectionPlan(
            schemaVersion: 3,
            scenario: ConnectionPlanScenario(id: "office", name: "公司"),
            tasks: types.enumerated().map { index, type in
                ConnectionPlanTask(id: "line:\(index)", kind: "line", name: type,
                    preparation: "connect", resourceID: "\(index)", resourceType: type)
            }
        ))
        for task in report.tasks { report.updateTask(id: task.id, state: .ready) }
        report.setState(state)
        return report
    }

    func testOfficeNameDoesNotImplyVPNAuthentication() {
        let value = ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: report(["direct"]))
        XCTAssertEqual(value, .direct)
        XCTAssertTrue(value.title { zh, _ in zh }.contains("未建立 VPN"))
    }

    func testOnlyCommittedReadyVPNIsConnected() {
        for state in [ConnectionTransactionState.preparing, .committing, .failed, .rolledBack] {
            XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: report(["vpn"], state: state)), .unconfirmed)
        }
        var committed = report(["direct", "vpn"])
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: committed), .vpn)
        committed.updateTask(id: "line:1", state: .failed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: committed), .unconfirmed)
    }

    func testPreparingSwitchKeepsCommittedSourceStatus() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: true, report: report(["direct"])), .direct)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: true, report: report(["vpn"])), .vpn)
    }

    func testInactiveIgnoresStaleSuccessfulReport() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: false, busy: false, report: report(["vpn"])), .inactive)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: false, busy: true, report: report(["vpn"])), .preparing)
    }

    func testMissingAndUnknownFactsNeverClaimDirectOrVPN() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: nil), .unconfirmed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: report([])), .unconfirmed)
        for type in ["", "selector", "trojan", "tailscale"] {
            XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: report([type])), .routed)
        }
    }

    func testRoutingPreviewShowsPurposeIndependentOfLocationName() {
        var profile = Profile()
        profile.lines = [Line(id: "d", name: "直连", type: "direct"), Line(id: "v", name: "公司 VPN", type: "vpn")]
        profile.ruleSets = [RuleSet(id: "r", name: "公司内网", type: "manual")]
        var scenario = Scenario(id: "s", name: "国内", bindings: [RuleBinding(ruleSetID: "r", lineID: "v")], defaultLineID: "d")
        let text = ScenarioRoutingDescription.text(scenario, in: profile) { zh, _ in zh }
        XCTAssertEqual(text, "配置：公司内网 → 公司 VPN\n其余流量 → 原网络（直连）")
        scenario.name = "海外"
        XCTAssertEqual(ScenarioRoutingDescription.text(scenario, in: profile) { zh, _ in zh }, text)
        scenario.bindings = []
        XCTAssertEqual(ScenarioRoutingDescription.text(scenario, in: profile) { zh, _ in zh }, "配置：全部流量 → 原网络（直连）")
    }

    func testUnavailableTargetIsNotMisrepresentedAsDirect() {
        let scenario = Scenario(id: "s", name: "公司", defaultLineID: "missing")
        XCTAssertEqual(ScenarioRoutingDescription.text(scenario, in: Profile()) { _, en in en }, "Configured: All traffic → Unconfigured line")
    }
}
