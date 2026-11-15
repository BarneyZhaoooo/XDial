import Foundation

/// App Group 和隧道扩展的 bundle id。三个 target 的 Info.plist 都写着同一组值，
/// 由 project.yml 里的 XDIAL_BUNDLE_ID / XDIAL_APP_GROUP 决定；换一个开发者团队打包时
/// 只需要在命令行用 xcconfig 覆盖这几个设置，不用改代码。
enum XDialIdentity {
    static let appGroup = value(
        forInfoKey: "XDialAppGroupIdentifier",
        default: "group.com.kafeifei.xdial.ios"
    )
    static let tunnelBundleIdentifier = value(
        forInfoKey: "XDialTunnelBundleIdentifier",
        default: "com.kafeifei.xdial.ios.tunnel"
    )

    private static func value(forInfoKey key: String, default fallback: String) -> String {
        let configured = Bundle.main.object(forInfoDictionaryKey: key) as? String
        let trimmed = configured?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        // 没被替换的 $(…) 说明这个 target 没有配置，退回默认值。
        return trimmed.isEmpty || trimmed.contains("$(") ? fallback : trimmed
    }
}
