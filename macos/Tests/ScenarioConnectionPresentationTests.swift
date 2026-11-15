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
        let value = ScenarioConnectionPresentation.resolve(status: "connected", report: report(["direct"]))
        XCTAssertEqual(value, .direct)
        XCTAssertTrue(value.title { zh, _ in zh }.contains("未建立 VPN"))
    }

    func testOnlyCommittedReadyVPNIsConnected() {
        for state in [ConnectionTransactionState.preparing, .committing, .failed, .rolledBack] {
            XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: report(["vpn"], state: state)), .unconfirmed)
        }
        var committed = report(["direct", "vpn"])
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: committed), .vpn)
        committed.updateTask(id: "line:1", state: .failed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: committed), .unconfirmed)
    }

    func testPreparingSwitchKeepsCommittedSourceStatus() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: report(["direct"])), .direct)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: report(["vpn"])), .vpn)
    }

    func testInactiveIgnoresStaleSuccessfulReport() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "disconnected", report: report(["vpn"])), .inactive)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connecting", report: report(["vpn"])), .preparing)
    }

    func testMissingAndUnknownFactsNeverClaimDirectOrVPN() {
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: nil), .unconfirmed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: report([])), .unconfirmed)
        for type in ["", "selector", "trojan", "tailscale"] {
            XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: report([type])), .routed)
        }
    }

    func testMissingReportExplainsUncertaintyWithoutLinkingToDetails() {
        // A connected engine alone cannot establish readiness when its report is missing.
        let value = ScenarioConnectionPresentation.resolve(status: "connected", report: nil)
        XCTAssertEqual(value, .unconfirmed)
        XCTAssertEqual(value.title { zh, _ in zh }, "正在确认线路状态")
        XCTAssertEqual(value.title { _, en in en }, "Checking line status")
        XCTAssertEqual(value.detail { zh, _ in zh }, "尚无完整的已提交线路状态，暂时无法确认线路是否就绪。")
        XCTAssertEqual(value.detail { _, en in en }, "Complete committed line status is unavailable. Line readiness cannot be confirmed yet.")
    }

    func testEngineLifecycleOverridesMissingOrStaleReports() {
        let phases: [(String, ScenarioConnectionPresentation, String, String)] = [
            ("connecting", .preparing, "正在准备线路…", "Preparing routes…"),
            ("disconnecting", .disconnecting, "正在断开…", "Disconnecting…"),
            ("reconnecting", .reconnecting, "正在重连…", "Reconnecting…"),
            ("disconnected", .inactive, "XDial 未启用", "XDial is inactive"),
            ("unknown", .unconfirmed, "正在确认线路状态", "Checking line status"),
        ]
        let reports: [ConnectionReport?] = [nil, report(["direct"]), report(["vpn"]), report(["vpn"], state: .failed)]
        for (status, expected, zhTitle, enTitle) in phases {
            for report in reports {
                let value = ScenarioConnectionPresentation.resolve(status: status, report: report)
                XCTAssertEqual(value, expected, status)
                XCTAssertEqual(value.title { zh, _ in zh }, zhTitle)
                XCTAssertEqual(value.title { _, en in en }, enTitle)
                if status == "disconnecting" || status == "reconnecting" {
                    XCTAssertFalse(value.detail { zh, _ in zh }.contains("尚未完成启用"))
                    XCTAssertFalse(value.detail { _, en in en }.contains("Activation is not complete"))
                }
            }
        }
    }

    func testWakeRecoveryUsesTheSamePhaseTextAsTheStatusBand() {
        let reports: [ConnectionReport?] = [nil, report(["vpn"])]
        for phase in WakeReconnectPhase.allCases {
            for status in ["connected", "disconnected", "disconnecting", "connecting", "reconnecting"] {
                for report in reports {
                    let value = ScenarioConnectionPresentation.resolve(
                        status: status, wakeReconnectPhase: phase, report: report)
                    XCTAssertEqual(value, .wakeReconnecting(phase))
                    XCTAssertEqual(value.title { zh, _ in zh }, phase.zhStatusText)
                    XCTAssertEqual(value.title { _, en in en }, phase.enStatusText)
                    XCTAssertFalse(value.detail { zh, _ in zh }.contains("尚未完成启用"))
                }
            }
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
                XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: committed), .routed)
                for state in [ConnectionTaskState.pending, .running, .failed] {
                    var incomplete = committed
                    incomplete.updateTask(id: "subscription:s", state: state)
                    XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: incomplete), .unconfirmed)
                }
            }
        }
    }

    func testVPNWithSubscriptionStillRequiresEveryTargetReady() {
        var committed = report(["vpn"])
        committed.tasks.append(ConnectionTaskReport(
            id: "subscription:s", kind: "subscription", name: "Subscription", detail: "",
            preparation: "start-with-data-plane", resourceID: "s", resourceType: "selector", state: .pending))
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: committed), .unconfirmed)
        committed.updateTask(id: "subscription:s", state: .committed)
        XCTAssertEqual(ScenarioConnectionPresentation.resolve(status: "connected", report: committed), .vpn)
    }
}
