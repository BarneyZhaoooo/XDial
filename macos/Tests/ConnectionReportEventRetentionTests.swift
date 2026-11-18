import XCTest

/// A committed transaction records Line recovery for as long as the session
/// lives. Its event list has to stay bounded without losing how the connection
/// was built, what happened most recently, or the facts the UI reads state from.
final class ConnectionReportEventRetentionTests: XCTestCase {
    private let lineTaskID = "line:vpn"

    func testLongRunningTransactionKeepsStartPhaseAndNewestEvents() {
        var report = committedReport()
        let startPhase = report.events
        let cycles = 2_000

        for _ in 0 ..< cycles {
            recordLineRecovery(in: &report)
        }

        XCTAssertEqual(
            Array(report.events.prefix(startPhase.count)),
            startPhase,
            "the events up to the first commit must survive"
        )
        XCTAssertEqual(
            report.events.count,
            startPhase.count + ConnectionReport.recentEventLimit
        )
        XCTAssertEqual(report.events.last?.code, "anyconnect-line-reconnected")
        XCTAssertEqual(
            report.events.last?.sequence,
            startPhase.count + cycles * 4,
            "sequence numbers keep counting; they are not reused"
        )
        XCTAssertEqual(
            report.droppedEventCount,
            cycles * 4 - ConnectionReport.recentEventLimit
        )
        let recent = report.events.suffix(ConnectionReport.recentEventLimit)
            .map(\.sequence)
        XCTAssertEqual(
            recent,
            Array((recent.first ?? 0) ... (recent.last ?? 0)),
            "the recent window has no holes"
        )
        XCTAssertEqual(
            report.events.map(\.sequence),
            report.events.map(\.sequence).sorted()
        )
    }

    func testEventsKeepBeingRecordedPastTheLimit() {
        var report = committedReport()
        for _ in 0 ..< 500 {
            recordLineRecovery(in: &report)
        }
        let before = report.events.last?.sequence ?? 0

        report.note(
            code: "anyconnect-line-reconnecting",
            message: "later",
            taskID: lineTaskID
        )

        XCTAssertEqual(report.events.last?.sequence, before + 1)
        XCTAssertEqual(report.events.last?.message, "later")
    }

    func testShortTransactionIsLeftComplete() {
        var report = committedReport()
        let startCount = report.events.count
        for _ in 0 ..< 10 {
            recordLineRecovery(in: &report)
        }

        XCTAssertEqual(report.events.count, startCount + 40)
        XCTAssertEqual(report.droppedEventCount, 0)
    }

    func testUncommittedTransactionIsNeverTrimmed() {
        var report = makeReport()
        for index in 0 ..< 1_000 {
            report.note(
                code: "preparing",
                message: "step \(index)",
                taskID: lineTaskID
            )
        }

        XCTAssertEqual(report.events.count, 1_001)
        XCTAssertEqual(report.droppedEventCount, 0)
    }

    /// The Line summary reads the current address-family capability from the
    /// newest fact-carrying event of the task. One recorded after the commit
    /// must outlive the chatter that follows it, or the summary would fall back
    /// to the stale capability from the start phase.
    func testNewestFactsOfATaskSurviveTrimming() {
        var report = makeReport()
        let startCapability = LineAddressFamilyCapability(
            ipv4Available: true,
            ipv6Available: false
        )
        report.note(
            code: startCapability.reportCode,
            message: "start",
            taskID: lineTaskID,
            facts: startCapability.reportFacts
        )
        commit(&report)

        let superseded = LineAddressFamilyCapability(
            ipv4Available: false,
            ipv6Available: true
        )
        let current = LineAddressFamilyCapability(
            ipv4Available: true,
            ipv6Available: true
        )
        for capability in [superseded, current, superseded, current] {
            report.note(
                code: capability.reportCode,
                message: "after commit",
                taskID: lineTaskID,
                facts: capability.reportFacts
            )
        }
        for _ in 0 ..< 1_000 {
            recordLineRecovery(in: &report)
        }

        XCTAssertEqual(
            LineConnectionSummaryProjection.addressFamilyCapability(
                taskID: lineTaskID,
                report: report
            ),
            current
        )
        let keptFacts = report.events.filter {
            $0.facts != nil && $0.message == "after commit"
        }
        XCTAssertEqual(
            keptFacts.map(\.code),
            [superseded.reportCode, current.reportCode],
            "one event per task and code is enough to carry the facts"
        )
        XCTAssertGreaterThan(report.droppedEventCount, 0)
    }

    func testDroppedCountSurvivesTheJournalAndOldReportsReadAsComplete() throws {
        var report = committedReport()
        for _ in 0 ..< 200 {
            recordLineRecovery(in: &report)
        }
        XCTAssertGreaterThan(report.droppedEventCount, 0)

        let encoded = try ConnectionReportCodec.encode(report)
        let decoded = try ConnectionReportCodec.decode(encoded)
        XCTAssertEqual(decoded.droppedEventCount, report.droppedEventCount)
        XCTAssertEqual(
            decoded.events.map(\.sequence),
            report.events.map(\.sequence)
        )

        var object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: encoded) as? [String: Any]
        )
        XCTAssertNotNil(object["events_dropped"])
        object.removeValue(forKey: "events_dropped")
        let legacy = try ConnectionReportCodec.decode(
            JSONSerialization.data(withJSONObject: object)
        )
        XCTAssertEqual(legacy.droppedEventCount, 0)
    }

    /// One local AnyConnect recovery as the Provider records it.
    private func recordLineRecovery(in report: inout ConnectionReport) {
        report.updateTask(id: lineTaskID, state: .running)
        report.note(
            code: "anyconnect-line-reconnecting",
            message: "reconnecting",
            taskID: lineTaskID
        )
        report.updateTask(id: lineTaskID, state: .ready)
        report.note(
            code: "anyconnect-line-reconnected",
            message: "reconnected",
            taskID: lineTaskID
        )
    }

    private func committedReport() -> ConnectionReport {
        var report = makeReport()
        commit(&report)
        return report
    }

    private func commit(_ report: inout ConnectionReport) {
        for task in report.tasks where task.kind != "ingress" {
            report.updateTask(id: task.id, state: .ready)
        }
        report.updateTask(id: "ingress:transparent-proxy", state: .committed)
        report.setState(.committed)
    }

    private func makeReport() -> ConnectionReport {
        let plan = ConnectionPlan(
            schemaVersion: 3,
            scenario: ConnectionPlanScenario(id: "scenario", name: "Test"),
            tasks: [
                task("underlay:system", kind: "underlay"),
                task(lineTaskID, kind: "line"),
                task("dns:scenario", kind: "dns"),
                task("data-plane:sing-box", kind: "data_plane"),
                task("ingress:transparent-proxy", kind: "ingress"),
            ]
        )
        return ConnectionReport(transactionID: "transaction", plan: plan)
    }

    private func task(_ id: String, kind: String) -> ConnectionPlanTask {
        ConnectionPlanTask(id: id, kind: kind, name: id, preparation: "test")
    }
}
