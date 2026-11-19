import XCTest

final class CommittedReportRefreshPolicyTests: XCTestCase {
    func testAdoptsProviderEventsRecordedAfterTheCommit() {
        let presented = committedReport()
        var probed = presented
        probed.updateTask(id: "line:vpn", state: .running)
        probed.note(
            code: "anyconnect-line-reconnecting",
            message: "reconnecting",
            taskID: "line:vpn"
        )

        XCTAssertTrue(shouldAdopt(probed: probed, presented: presented))
    }

    func testUnchangedReportIsNotRepublished() {
        let report = committedReport()

        XCTAssertFalse(shouldAdopt(probed: report, presented: report))
    }

    func testFirstReadOfTheSessionIsAdopted() {
        XCTAssertTrue(shouldAdopt(probed: committedReport(), presented: nil))
    }

    /// Start, recovery and Scenario switch decide for themselves which report
    /// is current; a display refresh must not get ahead of them.
    func testStandsAsideWhileTheHostChangesTheConnection() {
        let presented = committedReport()
        var probed = presented
        probed.note(code: "later", message: "later", taskID: "line:vpn")

        XCTAssertFalse(shouldAdopt(
            probed: probed,
            presented: presented,
            hostIsChangingConnection: true
        ))
        XCTAssertFalse(shouldAdopt(
            probed: probed,
            presented: presented,
            systemSessionConnected: false
        ))
    }

    func testOnlyTheProvenSessionIsFollowed() {
        let presented = committedReport()
        var probed = presented
        probed.note(code: "later", message: "later", taskID: "line:vpn")

        XCTAssertFalse(shouldAdopt(
            probed: probed,
            presented: presented,
            currentTransactionID: "another"
        ))
        XCTAssertFalse(shouldAdopt(
            probed: probed,
            presented: presented,
            connectedSessionTransactionID: nil
        ))
    }

    /// A failing or rolled back transaction is settled by the status-driven
    /// path, which also confirms the system teardown.
    func testTerminalReportsAreLeftToTheStatusPath() {
        let presented = committedReport()
        var failed = presented
        failed.fail(code: "line-lost", message: "lost", taskID: "line:vpn")
        var removed = presented
        removed.systemTakeoverRemoved = true

        XCTAssertFalse(shouldAdopt(probed: failed, presented: presented))
        XCTAssertFalse(shouldAdopt(probed: removed, presented: presented))
    }

    func testProviderCopyBehindTheHostCopyDoesNotEraseHostEvents() {
        let probed = committedReport()
        var presented = probed
        presented.note(code: "host-note", message: "host", taskID: "")

        XCTAssertFalse(shouldAdopt(probed: probed, presented: presented))
    }

    private func shouldAdopt(
        probed: ConnectionReport,
        presented: ConnectionReport?,
        currentTransactionID: String? = "transaction",
        connectedSessionTransactionID: String? = "transaction",
        systemSessionConnected: Bool = true,
        hostIsChangingConnection: Bool = false
    ) -> Bool {
        CommittedReportRefreshPolicy.shouldAdopt(
            probed: probed,
            presented: presented,
            currentTransactionID: currentTransactionID,
            connectedSessionTransactionID: connectedSessionTransactionID,
            systemSessionConnected: systemSessionConnected,
            hostIsChangingConnection: hostIsChangingConnection
        )
    }

    private func committedReport() -> ConnectionReport {
        let plan = ConnectionPlan(
            schemaVersion: 3,
            scenario: ConnectionPlanScenario(id: "scenario", name: "Test"),
            tasks: [
                ConnectionPlanTask(
                    id: "line:vpn",
                    kind: "line",
                    name: "line",
                    preparation: "test"
                ),
                ConnectionPlanTask(
                    id: "ingress:transparent-proxy",
                    kind: "ingress",
                    name: "ingress",
                    preparation: "test"
                ),
            ]
        )
        var report = ConnectionReport(transactionID: "transaction", plan: plan)
        report.updateTask(id: "line:vpn", state: .ready)
        report.updateTask(id: "ingress:transparent-proxy", state: .committed)
        report.systemTakeoverRemoved = false
        report.setState(.committed)
        return report
    }
}
