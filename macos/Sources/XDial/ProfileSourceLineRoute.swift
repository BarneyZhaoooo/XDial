import Foundation

/// Routing needs the app's Line model, which the Provider target does not
/// carry; the Provider only serves already-routed requests.
extension ProfileSourceLineFetch {
    enum Route: Equatable {
        case urlSession
        case application
        case provider(transactionID: String)
        case reject(String)
    }

    /// `committedTransactionID` is non-nil only when XDial's system takeover is
    /// committed; `transactionInFlight` covers connecting, switching and teardown.
    static func route(
        lineID: String?,
        lines: [Line],
        committedTransactionID: String?,
        transactionInFlight: Bool
    ) -> Route {
        guard !isDirect(lineID), let lineID else { return .urlSession }
        guard let line = lines.first(where: { $0.id == lineID }) else {
            return .reject("此来源指定的获取线路已不存在，请在订阅设置中重新选择；原配置已保留")
        }
        guard !unsupportedLineTypes.contains(line.type) else {
            return .reject("「\(line.name)」这类线路暂不支持用于下载订阅，请改用直连或其他代理线路；原配置已保留")
        }
        if let committedTransactionID, !committedTransactionID.isEmpty {
            return .provider(transactionID: committedTransactionID)
        }
        guard !transactionInFlight else {
            return .reject("XDial 正在连接或切换，稍后再更新此订阅；原配置已保留")
        }
        return .application
    }

    /// Lines a user may pick for downloading a source.
    static func selectableLines(_ lines: [Line]) -> [Line] {
        lines.filter { !isDirect($0.id) && !unsupportedLineTypes.contains($0.type) }
    }
}
