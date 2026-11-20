import Foundation
import Libbox
import os

/// Puts sing-box warn and error lines into the unified log.
///
/// sing-box runs as a library here and its own log goes to the extension's
/// stderr, which nobody keeps. `summary` has addresses, host names and
/// credentials removed by the Go side and is public; `detail` still names the
/// destination and stays private.
final class DataPlaneLogSink: NSObject, LibboxDataPlaneLogSinkProtocol {
    private let logger = Logger(
        subsystem: XDialBuildIdentity.transparentProxyIdentifier,
        category: "data-plane"
    )

    func writeDataPlaneLog(_ level: String?, summary: String?, detail: String?) {
        let level = level ?? "error"
        logger.log(
            level: DataPlaneLogLevel.logType(for: level),
            "data-plane level=\(level, privacy: .public) \(summary ?? "-", privacy: .public) detail=\(detail ?? "-", privacy: .private)"
        )
    }
}
