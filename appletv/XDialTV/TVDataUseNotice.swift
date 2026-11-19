import SwiftUI

/// The declaration App Review Guideline 5.4 asks a VPN app to show before the
/// service is used, for Apple TV. It covers the whole screen until confirmed
/// (`AppState.dataUseNoticeAccepted` holds every connection until then) and can
/// be reread from Settings. Every statement must match the code; the evidence is
/// in docs/agent/app-store-privacy.md (Apple TV section).
struct TVDataUseNoticeView: View {
    enum Mode { case firstUse, review }

    /// The address App Store Connect lists. Apple TV has no browser, so it is shown as text.
    static let privacyPolicyAddress = "https://xindong.github.io/XDial/privacy/"

    @EnvironmentObject private var app: AppState
    let mode: Mode

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    Text(app.tr("数据使用说明", "How XDial Uses Data"))
                        .font(.largeTitle.bold())
                    Text(TVDataUseNotice.introduction(isChinese: app.language == .zh))
                        .font(.body)
                    ForEach(TVDataUseNotice.sections(isChinese: app.language == .zh), id: \.title) { section in
                        // Plain text takes no focus on tvOS; focusable sections let the remote scroll.
                        VStack(alignment: .leading, spacing: 10) {
                            Label(section.title, systemImage: section.symbol)
                                .font(.headline)
                            ForEach(section.points, id: \.self) { point in
                                Text("• " + point)
                                    .font(.callout)
                                    .foregroundStyle(.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                        .padding(20)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .focusable()
                    }
                    Text(app.tr("完整隐私政策：", "Full privacy policy: ") + Self.privacyPolicyAddress)
                        .font(.callout)
                        .padding(20)
                        .focusable()
                        .accessibilityIdentifier("data-use-privacy-policy")
                }
                .padding(.horizontal, 80)
                .padding(.vertical, 40)
            }
            if mode == .firstUse {
                Button {
                    app.acceptDataUseNotice()
                } label: {
                    Text(app.tr("继续使用", "Continue")).frame(minWidth: 400)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 30)
                .accessibilityIdentifier("data-use-accept")
            }
        }
        // A full-screen cover is transparent on tvOS; keep the screen behind it out of view.
        .background(.background)
    }
}

/// The notice's content, kept apart from the view so tests can check it.
enum TVDataUseNotice {
    struct Section: Equatable {
        let title: String
        let symbol: String
        let points: [String]
    }

    static func introduction(isChinese zh: Bool) -> String {
        zh ? "XDial 在这台 Apple TV 上建立 VPN，按你选择的场景把每个连接送到直连，或者你配置的线路（代理服务器、AnyConnect、Tailscale）。使用前请了解它会访问哪些服务、哪些数据只留在设备上。"
           : "XDial sets up a VPN on this Apple TV and sends each connection either directly or through a line you configure (a proxy server, AnyConnect or Tailscale), according to the scenario you choose. Before you start, here is what it contacts and what stays on the device."
    }

    static func sections(isChinese zh: Bool) -> [Section] {
        func t(_ chinese: String, _ english: String) -> String { zh ? chinese : english }
        return [
            Section(title: t("开发者不收集你的数据", "We do not collect your data"), symbol: "hand.raised", points: [
                t("XDial 没有账号系统，也不运行收集用户数据的服务器；App 里没有广告、分析、崩溃收集或追踪 SDK。",
                  "XDial has no account system and runs no server that collects user data; the app contains no advertising, analytics, crash-reporting or tracking SDK."),
                t("你的浏览内容和访问记录不会发给我们。",
                  "What you browse and visit is never sent to us."),
            ]),
            Section(title: t("流量去向", "Where your traffic goes"), symbol: "arrow.triangle.branch", points: [
                t("网络流量按你的配置直接发往目标网站，或发往你配置的服务器。这些服务器由你或你的服务提供方运营，他们如何处理数据以他们的政策为准。",
                  "Traffic goes straight to its destination or to the servers you configure. Those servers are run by you or your provider, whose own policies govern how they handle data."),
                t("连接 AnyConnect 线路时，按该协议向你的 VPN 服务器发送设备名、设备类型（Apple TV）、网卡地址和由它们生成的设备标识；使用 Tailscale 线路时，设备名会发给 Tailscale 的服务器。",
                  "With an AnyConnect line, XDial sends your VPN server the device name, the device type (Apple TV), the network interface address and a device identifier derived from them, as that protocol requires; with a Tailscale line, the device name is sent to Tailscale's servers."),
            ]),
            Section(title: t("自动访问的公共服务", "Public services XDial contacts"), symbol: "network", points: [
                t("域名解析：连接期间，这台 Apple TV 的域名查询以未加密方式发往 Cloudflare 公共 DNS（1.1.1.1）；使用 AnyConnect 线路时改发给该 VPN 提供的 DNS，Tailscale 内的名称由 Tailscale 解析。",
                  "Name resolution: while connected, this Apple TV's DNS queries are sent unencrypted to Cloudflare public DNS (1.1.1.1); with an AnyConnect line they go to the DNS that VPN provides, and Tailscale names are resolved by Tailscale."),
                t("连通性检测：每次连接时访问 Cloudflare（1.0.0.1、1.1.1.1）和 www.apple.com 的测试地址，第一处没有回应时再试阿里公共 DNS（223.5.5.5）；连接期间每隔几分钟经代理节点访问 www.gstatic.com，测出可用和最快的节点。",
                  "Connectivity checks: on each connection XDial requests test addresses at Cloudflare (1.0.0.1, 1.1.1.1) and www.apple.com, and tries Alibaba public DNS (223.5.5.5) if the first one does not answer; while connected it requests www.gstatic.com through proxy nodes every few minutes to find working and fast nodes."),
                t("出口地址：每次连接时，经各条线路访问 checkip.amazonaws.com 和 Cloudflare，查出这条线路对外的 IP 地址，记在本机的连接记录里。",
                  "Exit address: on each connection XDial asks checkip.amazonaws.com and Cloudflare, through each line, for that line's public IP address, which it records in the connection history on the device."),
                t("规则和订阅：配置引用的规则文件在缺少缓存时下载；订阅地址只在添加或刷新订阅时访问。这些地址由你或你的服务提供方给出，地址里带的令牌、账号或密码会随请求发给该地址的服务方。",
                  "Rules and subscriptions: rule files your configuration refers to are downloaded when no cached copy exists; subscription addresses are fetched only when a subscription is added or refreshed. These addresses come from you or your provider; any token, account or password in an address is sent to the service at that address."),
                t("这些服务都会看到你的 IP 地址。发给 DNS 和各项检测、查询服务的请求不含你的账号、密码或配置内容。",
                  "All of these services see your IP address. Requests to the DNS and the check and lookup services carry no account, password or configuration content."),
            ]),
            Section(title: t("只留在这台 Apple TV 上", "What stays on this Apple TV"), symbol: "appletv", points: [
                t("账号和密码保存在系统钥匙串里，只用来连接你配置的线路。",
                  "Accounts and passwords are kept in the system keychain and used only to connect your lines."),
                t("Apple TV 版不按 Wi-Fi 或所在地区自动选场景，不读取 Wi-Fi 名称，也不请求定位权限。",
                  "On Apple TV, XDial does not pick scenarios by Wi-Fi or region, does not read the Wi-Fi name and does not ask for location access."),
                t("连接记录和诊断信息（含出口 IP 地址）保存在本机，只有你主动导出或分享时才会离开设备。",
                  "Connection history and diagnostics (including exit IP addresses) stay on the device and leave it only when you export or share them."),
            ]),
        ]
    }
}
