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

    func testSubscriptionsAreRoutedWithOrWithoutDirectLines() {
        for types in [[], ["direct"]] {
            for strategy in ["selector", "urltest", "direct", "vpn", ""] {
                var committed = report(types)
                committed.tasks.append(ConnectionTaskReport(
                    id: "subscription:s", kind: "subscription", name: "Subscription", detail: "",
                    preparation: "start-with-data-plane", resourceID: "s", resourceType: strategy, state: .pending))
                committed.updateTask(id: "subscription:s", state: .ready)
                XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: committed), .routed)
                for state in [ConnectionTaskState.pending, .running, .failed] {
                    var incomplete = committed
                    incomplete.updateTask(id: "subscription:s", state: state)
                    XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: false, report: incomplete), .unconfirmed)
                }
            }
        }
    }

    func testVPNWithSubscriptionStillRequiresEveryTargetReady() {
        var committed = report(["vpn"])
        committed.tasks.append(ConnectionTaskReport(
            id: "subscription:s", kind: "subscription", name: "Subscription", detail: "",
            preparation: "start-with-data-plane", resourceID: "s", resourceType: "selector", state: .pending))
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: true, report: committed), .unconfirmed)
        committed.updateTask(id: "subscription:s", state: .committed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(connected: true, busy: true, report: committed), .vpn)
    }
}
