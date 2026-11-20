import XCTest

final class NetworkChangeNoticeTests: XCTestCase {
    private let wifi = NetworkChangeNoticeBaseline.Path(available: true, usesWiFi: true, interfaces: ["en0"], gateways: ["router"])
    private let offline = NetworkChangeNoticeBaseline.Path(available: false, usesWiFi: false, interfaces: [], gateways: [])

    func testSSIDAppearingAndTemporaryPermissionGap() {
        var baseline = NetworkChangeNoticeBaseline()
        XCTAssertFalse(baseline.changed(path: wifi, ssid: nil))
        XCTAssertFalse(baseline.changed(path: wifi, ssid: "Office"))
        XCTAssertFalse(baseline.changed(path: wifi, ssid: nil))
        XCTAssertTrue(baseline.changed(path: wifi, ssid: "Home"))
        XCTAssertFalse(baseline.changed(path: wifi, ssid: "Home"))
    }

    func testOfflineAndRestorationNotifyOnceEach() {
        var baseline = NetworkChangeNoticeBaseline()
        XCTAssertFalse(baseline.changed(path: wifi, ssid: "Office"))
        XCTAssertTrue(baseline.changed(path: offline, ssid: nil))
        XCTAssertFalse(baseline.changed(path: offline, ssid: nil))
        XCTAssertTrue(baseline.changed(path: wifi, ssid: nil))
        XCTAssertFalse(baseline.changed(path: wifi, ssid: "Home"))
    }
}
