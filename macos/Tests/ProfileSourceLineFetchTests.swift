import XCTest

final class ProfileSourceLineFetchTests: XCTestCase {
    private let lines = [
        Line(id: "direct", name: "直连", type: "direct"),
        Line(id: "proxy", name: "Proxy", type: "shadowsocks"),
        Line(id: "ts", name: "Tailnet", type: "tailscale"),
        Line(id: "corp", name: "Corp", type: "vpn"),
        Line(id: "auto", name: "Auto", type: "urltest"),
    ]

    func testDirectAndMissingLineKeepURLSessionImporter() {
        for lineID in [nil, "", "direct"] {
            XCTAssertEqual(route(lineID, committed: "tx"), .urlSession)
        }
    }

    func testExplicitLineRunsInApplicationOnlyWithoutTransaction() {
        XCTAssertEqual(route("proxy", committed: nil), .application)
    }

    func testCommittedTakeoverRoutesThroughProvider() {
        XCTAssertEqual(route("proxy", committed: "tx-1"), .provider(transactionID: "tx-1"))
    }

    func testInFlightTransactionIsRejectedRatherThanCaptured() {
        guard case .reject = route("proxy", committed: nil, inFlight: true) else {
            return XCTFail("an app-side fetch could be captured mid-transaction")
        }
    }

    func testProtocolOwnedGroupsAndMissingLinesAreRejectedNotDirect() {
        for lineID in ["ts", "corp", "auto", "gone"] {
            guard case .reject = route(lineID, committed: nil) else {
                return XCTFail("\(lineID) fell back instead of being rejected")
            }
        }
    }

    func testSelectableLinesExcludeDirectProtocolOwnedAndGroups() {
        XCTAssertEqual(ProfileSourceLineFetch.selectableLines(lines).map(\.id), ["proxy"])
    }

    func testRequestValidationRejectsDirectAndUnboundedInput() {
        let valid = ProfileSourceLineFetchRequest(
            transactionID: "tx", url: "https://example.com/p", lineID: "proxy",
            profileJSON: "{}", systemDNSJSON: #"["192.0.2.53"]"#)
        XCTAssertTrue(valid.isValid)
        for request in [
            ProfileSourceLineFetchRequest(transactionID: "tx", url: "https://example.com/p", lineID: "direct", profileJSON: "{}", systemDNSJSON: "[]"),
            ProfileSourceLineFetchRequest(transactionID: "", url: "https://example.com/p", lineID: "proxy", profileJSON: "{}", systemDNSJSON: "[]"),
            ProfileSourceLineFetchRequest(transactionID: "tx", url: "https://example.com/" + String(repeating: "a", count: 4096), lineID: "proxy", profileJSON: "{}", systemDNSJSON: "[]"),
        ] {
            XCTAssertFalse(request.isValid)
        }
    }

    func testUnsupportedLineErrorIsRecognisedFromCore() {
        let error = NSError(domain: "go", code: 1, userInfo: [NSLocalizedDescriptionKey: ProfileSourceLineFetchOutcome.unsupportedLineMessage])
        XCTAssertEqual(ProfileSourceLineFetchOutcome.code(for: error), "unsupported-line")
        XCTAssertEqual(ProfileSourceLineFetchOutcome.code(for: NSError(domain: "go", code: 1)), "fetch-failed")
    }

    private func route(_ lineID: String?, committed: String?, inFlight: Bool = false) -> ProfileSourceLineFetch.Route {
        ProfileSourceLineFetch.route(lineID: lineID, lines: lines, committedTransactionID: committed, transactionInFlight: inFlight)
    }
}
