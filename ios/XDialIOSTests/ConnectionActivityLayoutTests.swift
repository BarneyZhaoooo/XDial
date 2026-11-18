import SwiftUI
import XCTest
@testable import XDial

/// Renders the lock-screen Live Activity banner at real banner widths, in both
/// languages, at default and large text sizes, with small and large speeds.
/// Images go to TEST_RUNNER_XDIAL_SNAPSHOT_DIR when set, for visual review.
@MainActor
final class ConnectionActivityLayoutTests: XCTestCase {
    private struct Case {
        let name: String
        let width: CGFloat
        let chinese: Bool
        let size: DynamicTypeSize
        let state: XDialConnectionAttributes.ContentState
    }

    private func state(
        chinese: Bool, degraded: Bool, scenario: String, down: Double, up: Double
    ) -> XDialConnectionAttributes.ContentState {
        .init(
            phase: degraded ? .attention : .connected,
            title: chinese ? "已连接" : "Connected",
            scenarioName: scenario,
            connectedAt: Date().addingTimeInterval(-102),
            downloadBytesPerSecond: down,
            uploadBytesPerSecond: up,
            detail: degraded ? (chinese ? "自检未通过，正在重新检测" : "Route check failed · rechecking") : nil
        )
    }

    private var cases: [Case] {
        var result: [Case] = []
        // 300 pt: narrowest phones / folded Duo; 345: mini; 365: standard; 412: Max.
        for width: CGFloat in [300, 345, 365, 412] {
            for chinese in [false, true] {
                for (sizeName, size) in [("default", DynamicTypeSize.large), ("xxl", .xxLarge), ("ax3", .accessibility3)] {
                    let label = "\(Int(width))-\(chinese ? "zh" : "en")-\(sizeName)"
                    result.append(Case(name: label + "-degraded-mid", width: width, chinese: chinese, size: size,
                                       state: state(chinese: chinese, degraded: true, scenario: "在国外",
                                                    down: 28_000, up: 181_000)))
                    result.append(Case(name: label + "-ok-high", width: width, chinese: chinese, size: size,
                                       state: state(chinese: chinese, degraded: false,
                                                    scenario: "Company network outside mainland",
                                                    down: 245_600_000, up: 9_870_000)))
                    result.append(Case(name: label + "-ok-extreme", width: width, chinese: chinese, size: size,
                                       state: state(chinese: chinese, degraded: false, scenario: "在国内",
                                                    down: 987_600_000, up: 876_500_000)))
                }
            }
        }
        return result
    }

    func testLockScreenBannerKeepsEveryLineAndSpeedUnitsIntact() throws {
        let outputDirectory = ProcessInfo.processInfo.environment["XDIAL_SNAPSHOT_DIR"].map { URL(fileURLWithPath: $0) }
        if let outputDirectory {
            try FileManager.default.createDirectory(at: outputDirectory, withIntermediateDirectories: true)
        }
        for item in cases {
            let tint = item.state.phase == .attention
                ? Color(red: 0.88, green: 0.70, blue: 0.40) : Color(red: 0.44, green: 0.80, blue: 0.61)
            let view = XDialActivityLockScreenView(state: item.state, tint: tint)
                .frame(width: item.width)
                .background(Color(white: 0.12))
                .environment(\.dynamicTypeSize, item.size)
                .environment(\.locale, Locale(identifier: item.chinese ? "zh-Hans" : "en"))
            let renderer = ImageRenderer(content: view)
            renderer.scale = 2
            let image = try XCTUnwrap(renderer.uiImage, item.name)
            // Four single lines (state, check, scenario, speeds) with padding stay
            // well under 170 pt; any wrapped line would push past it.
            XCTAssertLessThan(image.size.height, 170, "\(item.name) wrapped: \(image.size.height)pt")
            if let outputDirectory, let data = image.pngData() {
                try data.write(to: outputDirectory.appendingPathComponent(item.name + ".png"))
            }
        }
    }

    func testSpeedValueAndUnitAreOneUnit() {
        for value: Double in [0, 999, 28_000, 181_000, 9_870_000, 245_600_000, 1_200_000_000] {
            let label = XDialSpeedLabel(symbol: "arrow.down", value: value)
                .environment(\.dynamicTypeSize, .accessibility3)
            let renderer = ImageRenderer(content: label)
            let height = renderer.uiImage?.size.height ?? 0
            let oneLine = ImageRenderer(content: Text("8").font(.system(.footnote, design: .rounded))
                .environment(\.dynamicTypeSize, .accessibility3)).uiImage?.size.height ?? 0
            XCTAssertLessThanOrEqual(height, oneLine * 1.6, "\(XDialSpeedFormat.full(value)) wrapped")
        }
    }
}
