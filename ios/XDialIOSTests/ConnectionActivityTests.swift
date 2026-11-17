import XCTest
import SwiftUI
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

    func testShortSpeedStaysWithinFourCharacters() {
        let cases: [(Double, String)] = [
            (1_000, "1K"), (12_400, "12K"), (99_400, "99K"),
            (99_600, "0.1M"), (120_000, "0.1M"), (999_000, "1.0M"), (1_240_000, "1.2M"),
            (9_940_000, "9.9M"), (9_960_000, "10M"), (12_000_000, "12M"),
            (99_400_000, "99M"), (99_600_000, "0.1G"), (1_200_000_000, "1.2G"),
        ]
        for (bytesPerSecond, expected) in cases {
            XCTAssertEqual(XDialSpeedFormat.short(bytesPerSecond), expected, "\(bytesPerSecond)")
        }
    }

    @MainActor
    func testWidestShortIsAsWideAsAnyRealValue() async {
        var labels = Set<String>()
        for bytesPerSecond in stride(from: 1_000.0, to: 50_000_000_000, by: 7_777.0 * 13) {
            labels.insert(XDialSpeedFormat.short(bytesPerSecond))
        }
        // Include every compact K/M/G spelling, including low rates the stride skips.
        for scale in [1_000.0, 1_000_000.0, 1_000_000_000.0] {
            for tenths in 1...990 {
                labels.insert(XDialSpeedFormat.short(Double(tenths) / 10 * scale))
            }
        }
        // CompactSpeed is shown only at >= 1 KB/s.
        labels = labels.filter { !$0.hasSuffix("B") }
        for size: CGFloat in [12, 11, 10] {
            let font = Font.system(size: size, weight: .semibold, design: .rounded).monospacedDigit()
            func width(_ label: String) -> CGFloat {
                let host = UIHostingController(rootView: Text(label).font(font).lineLimit(1).fixedSize())
                return host.sizeThatFits(in: CGSize(width: 1_000, height: 100)).width
            }
            let widest = width(XDialSpeedFormat.widestShort)
            for label in labels.sorted() {
                XCTAssertLessThanOrEqual(width(label), widest, "\(label) at \(size) pt")
            }
        }
    }
}
