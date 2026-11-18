import XCTest
@testable import XDial

final class ConnectionTimelineTests: XCTestCase {
    private var directory: URL!

    override func setUp() {
        super.setUp()
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("timeline-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: directory)
        super.tearDown()
    }

    func testAppAndTunnelWritersShareOneSessionAndActiveMarker() {
        let app = ConnectionTimelineStore(directory: directory)
        let tunnel = ConnectionTimelineStore(directory: directory)
        app.beginSession("s1", markActive: false)
        app.append(session: "s1", source: .app, stage: "app.start_requested")
        app.flush()
        tunnel.beginSession("s1", markActive: true)
        tunnel.append(session: "s1", source: .tunnel, stage: "tunnel.engine_started", ms: 1200)
        tunnel.flush()

        let file = app.snapshot()
        XCTAssertEqual(file.activeSessionID, "s1")
        XCTAssertEqual(file.sessions.count, 1)
        XCTAssertEqual(file.sessions[0].events.map(\.src), [.app, .tunnel])
        XCTAssertEqual(file.sessions[0].events.last?.ms, 1200)

        tunnel.endActiveSession("s1")
        tunnel.flush()
        XCTAssertNil(app.snapshot().activeSessionID)
    }

    func testSystemReconnectReassociatesEventsButPendingManualStartKeepsItsID() {
        let app = ConnectionTimelineStore(directory: directory)
        let tunnel = ConnectionTimelineStore(directory: directory)
        app.beginSession("manual", markActive: false)
        app.flush()
        tunnel.beginSession("old", markActive: true)
        tunnel.flush()
        app.append(session: "manual", source: .app, stage: "pending-manual")
        app.flush()
        tunnel.beginSession("new-system", markActive: true)
        tunnel.flush()
        // App cached the old ID while suspended; no disconnected callback.
        app.append(session: "old", preferActiveSession: true, source: .app, stage: "resumed")
        app.flush()
        let file = app.snapshot()
        XCTAssertEqual(file.sessions.first { $0.id == "manual" }?.events.last?.stage, "pending-manual")
        XCTAssertEqual(file.sessions.first { $0.id == "new-system" }?.events.last?.stage, "resumed")
        XCTAssertTrue(file.sessions.first { $0.id == "old" }?.events.isEmpty == true)
    }

    func testStopCompletionFollowsFinalWriteAndActiveMarkerRemoval() async {
        let store = ConnectionTimelineStore(directory: directory)
        store.beginSession("s", markActive: true)
        store.append(session: "s", source: .tunnel, stage: "stopped")
        await withCheckedContinuation { continuation in
            store.endActiveSession("s") { continuation.resume() }
        }
        let file = await store.loadSnapshot()
        XCTAssertNil(file.activeSessionID)
        XCTAssertEqual(file.sessions.first?.events.last?.stage, "stopped")
    }

    func testBusyFileLockDropsDiagnosticsInsteadOfWaitingOrWritingUnlocked() async {
        let store = ConnectionTimelineStore(directory: directory)
        store.beginSession("s", markActive: true)
        store.flush()
        let fd = open(directory.appendingPathComponent(ConnectionTimeline.fileName + ".lock").path,
                      O_RDWR, 0o600)
        XCTAssertGreaterThanOrEqual(fd, 0)
        defer { close(fd) }
        XCTAssertEqual(flock(fd, LOCK_EX | LOCK_NB), 0)
        store.append(session: "s", source: .app, stage: "must-drop")
        let start = ProcessInfo.processInfo.systemUptime
        let busy = await store.loadSnapshot()
        XCTAssertTrue(busy.sessions.isEmpty)
        XCTAssertLessThan(ProcessInfo.processInfo.systemUptime - start, 1)
        flock(fd, LOCK_UN)
        let after = await store.loadSnapshot()
        XCTAssertTrue(after.sessions.first?.events.isEmpty == true)
        XCTAssertEqual(after.activeSessionID, "s")
    }

    func testRequestTimestampRetainsCompletionOrderDespiteDelayedAppend() {
        let store = ConnectionTimelineStore(directory: directory)
        let first = DataPathProbe.CheckRecord(result: .ok, ms: 1000, completedAt: 2000)
        let second = DataPathProbe.CheckRecord(result: .timeout, ms: 15000, completedAt: 16000)
        store.beginSession("s", markActive: false)
        store.append(session: "s", source: .app, stage: "second", at: second.completedAt)
        store.append(session: "s", source: .app, stage: "first", at: first.completedAt)
        store.flush()
        let events = store.snapshot().sessions.first!.events.sorted { $0.t < $1.t }
        XCTAssertEqual(events.map(\.stage), ["first", "second"])
        XCTAssertEqual(events.map(\.t), [2000, 16000])
    }

    func testSessionsAndEventsAreBounded() {
        let store = ConnectionTimelineStore(directory: directory)
        for index in 0..<(ConnectionTimeline.maxSessions + 3) {
            store.beginSession("s\(index)", markActive: index == 0)
        }
        for _ in 0..<(ConnectionTimeline.maxEventsPerSession + 5) {
            store.append(session: "s8", source: .app, stage: "app.tick")
        }
        store.flush()
        let file = store.snapshot()
        XCTAssertEqual(file.sessions.count, ConnectionTimeline.maxSessions)
        XCTAssertFalse(file.sessions.contains { $0.id == "s0" }, "oldest session must rotate out")
        XCTAssertNil(file.activeSessionID, "a rotated-out active session must not stay active")
        let last = file.sessions.first { $0.id == "s8" }
        XCTAssertEqual(last?.events.count, ConnectionTimeline.maxEventsPerSession)
        XCTAssertEqual(last?.droppedEvents, 5)
        let bytes = (try? Data(contentsOf: directory.appendingPathComponent(ConnectionTimeline.fileName)))?.count ?? 0
        XCTAssertLessThanOrEqual(bytes, ConnectionTimeline.maxFileBytes)
    }

    func testLongConnectionCoalescesSurveysAndKeepsTheStopAcrossReconnect() throws {
        let store = ConnectionTimelineStore(directory: directory)
        let url = directory.appendingPathComponent(ConnectionTimeline.fileName)
        store.beginSession("old", markActive: true, origin: .user)
        store.append(session: "old", source: .tunnel, stage: "tunnel.start_begin")
        func survey(_ index: Int, _ result: ConnectionTimeline.Result, _ detail: String) {
            store.append(session: "old", source: .tunnel, stage: "tunnel.scenario_decision",
                         result: result, ms: index, detail: detail, at: Int64(index))
        }
        survey(1, .ok, "CN network=a")
        store.flush()
        let inode = try FileManager.default.attributesOfItem(atPath: url.path)[.systemFileNumber] as? NSNumber
        XCTAssertNotNil(inode)
        for index in 2...2000 { survey(index, .ok, "CN network=a") }
        store.flush()
        // Atomic writes replace the inode. Repeated probes haven't rewritten the file.
        XCTAssertEqual(try FileManager.default.attributesOfItem(atPath: url.path)[.systemFileNumber] as? NSNumber, inode)
        survey(2001, .ok, "CN network=b")
        survey(2002, .dns, "network=b dns-failed")
        survey(2003, .dns, "network=b dns-failed")
        survey(2004, .ok, "CN network=b")
        for index in 0..<40 {
            store.append(session: "old", source: .app, stage: "before-stop-\(index)")
        }
        store.append(session: "old", source: .app, stage: "app.stop_requested")
        store.append(session: "old", source: .tunnel, stage: "tunnel.stop_reason", detail: "reason=3 (noNetworkAvailable)")
        let events = try XCTUnwrap(store.snapshot().sessions.first?.events)
        let surveys = events.filter { $0.stage == "tunnel.scenario_decision" }
        XCTAssertEqual(surveys.map { $0.occurrences ?? 1 }, [2000, 1, 2, 1])
        XCTAssertEqual(surveys.first?.t, 1)
        XCTAssertEqual(surveys.first?.lastAt, 2000)
        for index in 0..<220 {
            store.append(session: "old", source: .app, stage: "tick-\(index)")
        }
        store.endActiveSession("old")
        store.beginSession("new", markActive: true, origin: .autoRetry)
        store.append(session: "new", source: .app, stage: "app.status.connected")
        let snapshot = store.snapshot()
        let old = try XCTUnwrap(snapshot.sessions.first { $0.id == "old" })
        XCTAssertEqual(old.events.count, ConnectionTimeline.maxEventsPerSession)
        XCTAssertEqual(old.events.first?.stage, "tunnel.start_begin")
        XCTAssertEqual(old.events.last?.stage, "tick-219")
        XCTAssertFalse(old.events.contains { $0.stage == "tunnel.stop_reason" })
        let summary = try XCTUnwrap(ConnectionTimelineReport.reconnectSummary(snapshot, isChinese: true))
        for label in ["App 请求断开", "reason=3 (noNetworkAvailable)", "App 自动重试", "已连接"] {
            XCTAssertTrue(summary.contains(label), summary)
        }
    }

    func testLegacyFullSessionWithoutStopDoesNotInventReasonOrDisconnectTime() throws {
        let raw = #"{"version":1,"activeSessionID":"new","sessions":[{"id":"old","startedAt":1000,"events":[{"t":2000,"src":"tunnel","stage":"tunnel.region_probe","result":"dns"}],"droppedEvents":2190},{"id":"new","startedAt":5000,"events":[],"droppedEvents":0}]}"#
        let file = try JSONDecoder().decode(ConnectionTimeline.File.self, from: Data(raw.utf8))
        let text = try XCTUnwrap(ConnectionTimelineReport.reconnectSummary(file, isChinese: true))
        XCTAssertTrue(text.contains("最后记录"))
        XCTAssertTrue(text.contains("停止原因未知"))
        XCTAssertTrue(text.contains("来源未知"))
        XCTAssertFalse(text.contains("已连接"))
    }

    func testCorruptFileIsReplacedInsteadOfBlockingWrites() throws {
        try Data("not json".utf8).write(to: directory.appendingPathComponent(ConnectionTimeline.fileName))
        let store = ConnectionTimelineStore(directory: directory)
        store.append(session: "s1", source: .app, stage: "app.start_requested")
        store.flush()
        XCTAssertEqual(store.snapshot().sessions.first?.events.count, 1)
    }

    func testSanitizerRemovesCredentialsAndTokensAndBoundsLength() {
        XCTAssertEqual(
            ConnectionTimeline.sanitized("fetch https://alice:secret@example.com/a failed"),
            "fetch https://<redacted>@example.com/a failed")
        XCTAssertEqual(ConnectionTimeline.sanitized("password=hunter2 token: abc"),
                       "password=<redacted> token=<redacted>")
        XCTAssertEqual(ConnectionTimeline.sanitized(String(repeating: "A", count: 40)), "<redacted>")
        XCTAssertEqual(ConnectionTimeline.sanitized("line\nbreak"), "line break")
        XCTAssertNil(ConnectionTimeline.sanitized("   "))
        let long = ConnectionTimeline.sanitized(String(repeating: "长", count: 400))
        XCTAssertEqual(long?.count, ConnectionTimeline.maxDetailCharacters)
    }

    func testAddressesAreRemovedAndFailuresReducedToKeywords() {
        XCTAssertEqual(ConnectionTimeline.sanitized("dial 203.0.113.9:443 and [2001:db8::9]:443"),
                       "dial <ip>:443 and <ip>:443")
        XCTAssertEqual(ConnectionTimeline.sanitized("fe80::1%en0 at 15:00:25"), "<ip> at 15:00:25")
        XCTAssertEqual(
            ConnectionTimeline.failureKeywords(
                "无法确认所选线路的出口地址：dial tcp 198.51.100.7:8443: connection refused; checkip: i/o timeout"),
            "io-timeout,timeout,refused")
        XCTAssertEqual(ConnectionTimeline.failureKeywords("unexpected status 503"), "http-503")
        XCTAssertEqual(ConnectionTimeline.failureKeywords("weird"), "other")
        XCTAssertEqual(ConnectionTimeline.failureKeywords(nil), "none")
    }

    func testURLErrorsMapToStableCodes() {
        func code(_ value: Int) -> ConnectionTimeline.Result {
            ConnectionTimeline.classify(NSError(domain: NSURLErrorDomain, code: value))
        }
        XCTAssertEqual(code(NSURLErrorTimedOut), .timeout)
        XCTAssertEqual(code(NSURLErrorCannotFindHost), .dns)
        XCTAssertEqual(code(NSURLErrorSecureConnectionFailed), .tls)
        XCTAssertEqual(code(NSURLErrorCannotConnectToHost), .connect)
        XCTAssertEqual(code(NSURLErrorCancelled), .cancelled)
        XCTAssertEqual(code(NSURLErrorBadServerResponse), .http)
        XCTAssertEqual(ConnectionTimeline.classify(CancellationError()), .cancelled)
        XCTAssertEqual(ConnectionTimeline.classify(NSError(domain: "other", code: 1)), .failed)
    }

    func testProbeFailureTextMapsToStableCodes() {
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: "线路探针超时"), .timeout)
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: "context deadline exceeded"), .timeout)
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: "address probe resolution failed: dns-failed"), .dns)
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: "tls handshake failure"), .tls)
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: "dial tcp: connection refused"), .connect)
        XCTAssertEqual(ConnectionTimeline.classify(probeFailure: nil), .failed)
    }

    func testReportRendersNewestSessionsWithRelativeTimes() {
        var file = ConnectionTimeline.File()
        file.sessions = [
            .init(id: "AAAAAAAA-0000", startedAt: 1_000, events: [
                .init(t: 1_000, src: .app, stage: "app.start_requested"),
            ], droppedEvents: 0),
            .init(id: "BBBBBBBB-1111", startedAt: 50_000, events: [
                .init(t: 52_500, src: .tunnel, stage: "tunnel.engine_started", ms: 2_500),
                .init(t: 50_000, src: .app, stage: "app.start_requested"),
                .init(t: 60_000, src: .app, stage: "app.check.line", result: .timeout, ms: 10_000, detail: "HK"),
            ], droppedEvents: 2),
        ]
        file.activeSessionID = "BBBBBBBB-1111"
        let text = ConnectionTimelineReport.render(file, latest: 1, isChinese: true)
        XCTAssertTrue(text.hasPrefix("连接 1 · "))
        XCTAssertTrue(text.contains("bbbbbbbb · 进行中"))
        XCTAssertFalse(text.contains("aaaaaaaa"), "only the requested number of sessions")
        let lines = text.split(separator: "\n").map(String.init)
        XCTAssertTrue(lines[1].contains("+0.0s app    app.start_requested"))
        XCTAssertTrue(lines[2].contains("+2.5s tunnel tunnel.engine_started 2.5s"))
        XCTAssertTrue(lines[3].contains("+10.0s app    app.check.line timeout 10.0s HK"))
        XCTAssertTrue(lines[4].contains("另有 2 条事件"))
    }

    func testDiagnosticsReportIncludesTimelineButNotProfileSecrets() {
        var profile = Profile()
        profile.lines = [Line(id: "hk", name: "HK", type: "trojan")]
        profile.lines[0].trojanServer = "hk.private.example"
        profile.lines[0].trojanPassword = "s3cret-pass"
        var file = ConnectionTimeline.File()
        file.sessions = [.init(id: "s1", startedAt: 0, events: [
            .init(t: 0, src: .app, stage: "app.check.line", result: .connect, ms: 900,
                  detail: "HK dial hk.private.example:443 refused"),
        ], droppedEvents: 0)]
        let report = MobileDiagnosticsService.report(
            version: "1.2 (34)", status: "已连接", systemProfileInstalled: true,
            profile: profile, lastError: nil, dataPathSummary: nil,
            environment: "1.2 (34) · iOS 27.3 · iPhone", scenarioName: "在国内",
            timeline: file, isChinese: true)
        XCTAssertTrue(report.contains("环境：1.2 (34) · iOS 27.3 · iPhone"))
        XCTAssertTrue(report.contains("当前所选场景（不是地区检测结果）：在国内"))
        XCTAssertTrue(report.contains("app.check.line connect 0.9s"))
        XCTAssertFalse(report.contains("hk.private.example"))
        XCTAssertFalse(report.contains("s3cret-pass"))
    }
}
