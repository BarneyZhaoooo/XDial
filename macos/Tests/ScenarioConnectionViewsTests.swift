import AppKit
import SwiftUI
import XCTest

final class ScenarioConnectionViewsTests: XCTestCase {
    @MainActor
    func testRoutingChoicesRenderWithCredentialsAndLongNames() throws {
        for english in [false, true] {
            for dark in [false, true] {
                let tr: (String, String) -> String = { english ? $1 : $0 }
                let content = VStack(alignment: .leading, spacing: 16) {
                    Text("XDial").font(.headline)
                    Divider()
                    ScenarioRuntimeSummaryView(
                        title: ScenarioConnectionPresentation.direct.title(tr),
                        detail: ScenarioConnectionPresentation.direct.detail(tr),
                        runningScenario: tr("运行场景：公司", "Running scenario: Office"))
                    Divider()
                    Text(tr("选择分流方式", "Choose routing")).font(.caption)
                    ScenarioRoutingRowView(name: tr("公司", "Office"), icon: "building.2",
                        routing: tr("配置：全部流量 → 原网络（直连）", "Configured: All traffic → Existing network (direct)"),
                        activeLabel: tr("使用中", "Active"), preparing: false,
                        configurationIssue: nil, actionTitle: tr("停用", "Stop"), action: {})
                    Divider()
                    ScenarioRoutingRowView(name: tr("国内 · 一个很长的自定义场景名称", "Domestic — a long custom scenario name"), icon: "globe",
                        routing: tr("配置：公司内网 → 公司 VPN\n其余流量 → 原网络（直连）", "Configured: Company intranet → Company VPN\nOther traffic → Existing network (direct)"),
                        activeLabel: nil, preparing: false,
                        configurationIssue: tr("AnyConnect 线路“公司 VPN”未填写用户名、密码。", "AnyConnect line “Company VPN” is missing its username, password."),
                        actionTitle: tr("去填写", "Fill In"), action: {})
                }
                .padding(16)
                .frame(width: 380)
                .background(XDialPalette.elevated)
                .environment(\.colorScheme, dark ? .dark : .light)
                let renderer = ImageRenderer(content: content)
                renderer.scale = 2
                let image = try XCTUnwrap(renderer.nsImage)
                XCTAssertEqual(image.size.width, 380, accuracy: 1)
                XCTAssertGreaterThan(image.size.height, 300)
                XCTAssertLessThan(image.size.height, 700)
                let attachment = XCTAttachment(image: image)
                attachment.name = "routing-\(english ? "en" : "zh")-\(dark ? "dark" : "light")"
                attachment.lifetime = .keepAlways
                add(attachment)
            }
        }
    }
}
