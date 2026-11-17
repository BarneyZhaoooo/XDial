import Foundation

/// Profile sources that name a fetch Line are downloaded by the shared Go core
/// through exactly that Line. While an XDial transaction owns the system, the
/// host app's own sockets are captured, so the request runs inside the Provider;
/// otherwise the app runs the same core call itself.
enum ProfileSourceLineFetch {
    static let maxContentBytes = 4 * 1024 * 1024
    /// Runtime profile with credentials plus the URL; far below typical
    /// profile sizes but bounded so a corrupted request cannot exhaust memory.
    static let maxRequestBytes = 8 * 1024 * 1024
    /// Escaped JSON may roughly double the bounded body.
    static let maxResponseBytes = 2 * maxContentBytes + 4096
    /// Go bounds the fetch to 30 s; this covers the Provider round trip.
    static let hostTimeout: TimeInterval = 40

    /// Line types whose protocol owns a process identity. They need the shared
    /// session lease before a resource fetch may run beside traffic.
    static let unsupportedLineTypes: Set<String> = ["tailscale", "vpn", "selector", "urltest"]

    static func isDirect(_ lineID: String?) -> Bool {
        guard let lineID else { return true }
        return lineID.isEmpty || lineID == "direct"
    }

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

struct ProfileSourceLineFetchRequest: Codable, Equatable {
    static let kind = "profile-source-line-fetch"
    static let version = 1

    let kind: String
    let version: Int
    let requestID: String
    let transactionID: String
    let url: String
    let lineID: String
    let profileJSON: String
    let systemDNSJSON: String

    init(transactionID: String, url: String, lineID: String, profileJSON: String, systemDNSJSON: String) {
        kind = Self.kind
        version = Self.version
        requestID = UUID().uuidString
        self.transactionID = transactionID
        self.url = url
        self.lineID = lineID
        self.profileJSON = profileJSON
        self.systemDNSJSON = systemDNSJSON
    }

    var isValid: Bool {
        kind == Self.kind && version == Self.version && !requestID.isEmpty
            && !transactionID.isEmpty && !url.isEmpty && url.utf8.count <= 4096
            && !ProfileSourceLineFetch.isDirect(lineID) && !profileJSON.isEmpty
            && !systemDNSJSON.isEmpty
    }
}

struct ProfileSourceLineFetchResponse: Codable, Equatable {
    let requestID: String
    let transactionID: String
    /// `ok`, `invalid-request`, `transaction-mismatch`, `session-not-committed`,
    /// `busy`, `unsupported-line` or `fetch-failed`.
    let code: String
    let content: String?
}

enum ProfileSourceLineFetchOutcome {
    /// Matches `libbox.ErrProfileSourceLineUnsupported`; the Go core remains the
    /// authority when the Swift type check and the runtime profile disagree.
    static let unsupportedLineMessage = "profile source Line type cannot fetch resources yet"

    static func code(for error: Error) -> String {
        (error as NSError).localizedDescription.contains(unsupportedLineMessage)
            ? "unsupported-line" : "fetch-failed"
    }
}
