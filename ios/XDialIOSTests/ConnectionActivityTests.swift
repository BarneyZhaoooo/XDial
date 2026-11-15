import XCTest
@testable import XDial

final class ConnectionActivityTests: XCTestCase {
    private func state(
        connected: Bool = true,
        download: Double?,
        upload: Double?
    ) -> XDialConnectionAttributes.ContentState {
        .init(
            phase: connected ? .connected : .working,
            title: "",
            scenarioName: "",
            connectedAt: connected ? Date(timeIntervalSince1970: 1_000) : nil,
            downloadBytesPerSecond: download,
            uploadBytesPerSecond: upload
        )
    }

    func testIdleConnectionLeavesTheIslandEmpty() {
        for idle in [state(download: nil, upload: nil), state(download: 0, upload: 0), state(download: 999, upload: 999)] {
            XCTAssertNil(idle.compactDownloadSpeed)
            XCTAssertNil(idle.compactUploadSpeed)
        }
    }

    func testEachSideShowsOnlyItsOwnSpeed() {
        let uploading = state(download: 300, upload: 2_000_000)
        XCTAssertNil(uploading.compactDownloadSpeed)
        XCTAssertEqual(uploading.compactUploadSpeed, 2_000_000)

        let downloading = state(download: 1_000, upload: 40)
        XCTAssertEqual(downloading.compactDownloadSpeed, 1_000)
        XCTAssertNil(downloading.compactUploadSpeed)
    }

    func testNothingShowsBeforeTheConnectionIsUp() {
        let connecting = state(connected: false, download: 5_000_000, upload: 5_000_000)
        XCTAssertNil(connecting.compactDownloadSpeed)
        XCTAssertNil(connecting.compactUploadSpeed)
    }
}
