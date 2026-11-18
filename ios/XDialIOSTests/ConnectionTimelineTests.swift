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
        XCTAssertTrue(report.contains("当前场景：在国内"))
        XCTAssertTrue(report.contains("app.check.line connect 0.9s"))
        XCTAssertFalse(report.contains("hk.private.example"))
        XCTAssertFalse(report.contains("s3cret-pass"))
    }
}
