import Foundation
@preconcurrency import Libbox

/// Downloads a profile source through its explicit fetch Line with the shared
/// Go core. DNS, HTTPS, redirects and the body all use that Line; a failure is
/// reported and never retried through Direct or the URLSession importer.
@MainActor
enum ProfileSourceLineFetcher {
    static func fetch(url: String, lineID: String, state: AppState) async throws -> String {
        guard let parsed = URL(string: url), parsed.scheme == "https", parsed.host != nil else {
            throw ProfileLibraryError.invalid("请填写完整的 HTTPS 订阅链接")
        }
        let runtime = state.profileLibrary.snapshot()
        let committed = TransparentProxyManager.shared.committedProviderTransactionID
        let inFlight = state.engine.isBusy
            || (state.engine.status != "disconnected" && committed == nil)
        let route = ProfileSourceLineFetch.route(
            lineID: lineID, lines: runtime.lines,
            committedTransactionID: committed, transactionInFlight: inFlight
        )
        let lineName = runtime.lines.first { $0.id == lineID }?.name ?? lineID
        guard let profileJSON = String(data: try JSONEncoder().encode(runtime), encoding: .utf8),
              let systemDNSJSON = TransparentProxyManager.captureSystemDNSJSON() else {
            throw ProfileLibraryError.invalid("无法读取当前系统网络，原配置已保留")
        }
        let content: String
        switch route {
        case .urlSession:
            throw ProfileLibraryError.invalid("此来源使用直连下载")
        case let .reject(message):
            throw ProfileLibraryError.invalid(message)
        case .application:
            content = try await fetchInApplication(
                url: url, lineID: lineID, profileJSON: profileJSON,
                systemDNSJSON: systemDNSJSON, lineName: lineName
            )
        case let .provider(transactionID):
            content = try await fetchInProvider(ProfileSourceLineFetchRequest(
                transactionID: transactionID, url: url, lineID: lineID,
                profileJSON: profileJSON, systemDNSJSON: systemDNSJSON
            ), lineName: lineName)
        }
        guard !content.isEmpty, content.utf8.count <= ProfileSourceLineFetch.maxContentBytes else {
            throw ProfileLibraryError.invalid("订阅为空或超过 4 MiB，原配置已保留")
        }
        return content
    }

    /// No XDial transaction owns the system, so the app's own sockets reach the
    /// selected Line directly; the core instance is private to this download.
    private static func fetchInApplication(
        url: String, lineID: String, profileJSON: String, systemDNSJSON: String, lineName: String
    ) async throws -> String {
        guard let instance = LibboxNew(nil) else {
            throw ProfileLibraryError.invalid("无法启动线路下载，原配置已保留")
        }
        let basePath = NSTemporaryDirectory()
        return try await withTaskCancellationHandler {
            try await Task.detached(priority: .userInitiated) {
                do {
                    return try instance.fetchProfileSourceViaLine(
                        profileJSON, rawURL: url, lineID: lineID,
                        systemDNSJSON: systemDNSJSON, basePath: basePath)
                } catch {
                    throw failure(code: ProfileSourceLineFetchOutcome.code(for: error), lineName: lineName)
                }
            }.value
        } onCancel: {
            instance.cancelPreparation()
        }
    }

    private static func fetchInProvider(
        _ request: ProfileSourceLineFetchRequest, lineName: String
    ) async throws -> String {
        let response: ProfileSourceLineFetchResponse = try await withCheckedThrowingContinuation { continuation in
            TransparentProxyManager.shared.sendProfileSourceLineFetch(request) { result in
                continuation.resume(with: result.mapError { _ in failure(code: "fetch-failed", lineName: lineName) })
            }
        }
        guard response.code == "ok", let content = response.content else {
            throw failure(code: response.code, lineName: lineName)
        }
        return content
    }

    nonisolated static func failure(code: String, lineName: String) -> ProfileLibraryError {
        switch code {
        case "unsupported-line":
            .invalid("「\(lineName)」这类线路暂不支持用于下载订阅，请改用直连或其他代理线路；原配置已保留")
        case "busy":
            .invalid("另一份订阅正在通过线路下载，稍后再试；原配置已保留")
        case "transaction-mismatch", "session-not-committed":
            .invalid("XDial 连接状态刚刚变化，请稍后再更新此订阅；原配置已保留")
        default:
            .invalid("无法通过「\(lineName)」获取订阅，请检查线路及当前网络；原配置已保留")
        }
    }
}
