import Foundation

enum TunnelRuntimeError: Error {
    case unavailable, invalidTailscaleStatus
}
enum XDialIdentity {
    static let appGroup = XDialBuildIdentity.appGroupIdentifier
}
extension AppState {
    nonisolated static func tailscaleEndpointTag(lineID: String) -> String { "tailscale-" + lineID }
}
