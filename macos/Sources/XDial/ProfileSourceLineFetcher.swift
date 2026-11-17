import Foundation
@preconcurrency import Libbox

/// Downloads and imports a profile source through its explicit fetch Line with
/// the shared Go core. DNS, HTTPS, redirects, the body and referenced rule
/// sources all use that Line; a failure is reported and never retried through
/// Direct or the URLSession importer.
@MainActor
enum ProfileSourceLineFetcher {
    private struct ApplicationSession {
        let instance: LibboxLibbox
        let task: Task<String, Error>
    }

    /// App-side sessions open sockets the system takeover would capture, so a
    /// connection start cancels them and waits for them to exit first.
    private static var applicationSessions: [UUID: ApplicationSession] = [:]

    static var hasApplicationSession: Bool { !applicationSessions.isEmpty }

    /// Cancels every app-side session and returns once each has exited.
    static func cancelApplicationSessions() async {
        let sessions = Array(applicationSessions.values)
        for session in sessions {
            session.instance.cancelPreparation()
            session.task.cancel()
        }
        for session in sessions {
            _ = try? await session.task.value
        }
    }

    static func importProfile(source: ProfileSource, lineID: String, profileID: String, state: AppState) async throws -> Profile {
        guard let parsed = URL(string: source.url), parsed.scheme == "https", parsed.host != nil else {
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
        let transactionID: String
        switch route {
        case .urlSession:
            throw ProfileLibraryError.invalid("此来源使用直连下载")
        case let .reject(message):
            throw ProfileLibraryError.invalid(message)
        case .application:
            transactionID = "application"
        case let .provider(committedID):
            transactionID = committedID
        }
        guard let profileJSON = String(data: try JSONEncoder().encode(runtime), encoding: .utf8),
              let systemDNSJSON = TransparentProxyManager.currentSystemDNSJSON() else {
            throw ProfileLibraryError.invalid("无法读取当前系统网络，原配置已保留")
        }
        let request = ProfileSourceLineFetchRequest(
            transactionID: transactionID, url: source.url, lineID: lineID,
            profileJSON: profileJSON, systemDNSJSON: systemDNSJSON,
            namespace: profileID, nodesOnly: source.nodesOnly)
        let imported: String
        if case .provider = route {
            imported = try await importInProvider(request, lineName: lineName)
        } else {
            imported = try await importInApplication(request, lineName: lineName)
        }
        guard !imported.isEmpty, imported.utf8.count <= ProfileSourceLineFetch.maxImportedBytes else {
            throw ProfileLibraryError.invalid("无法解析配置，原配置已保留")
        }
        return try JSONDecoder().decode(Profile.self, from: Data(imported.utf8))
    }

    /// No XDial transaction owns the system, so the app's own sockets reach the
    /// selected Line directly; the core instance is private to this import.
    private static func importInApplication(
        _ request: ProfileSourceLineFetchRequest, lineName: String
    ) async throws -> String {
        guard let instance = LibboxNew(nil) else {
            throw ProfileLibraryError.invalid("无法启动线路下载，原配置已保留")
        }
        let basePath = NSTemporaryDirectory()
        let task = Task.detached(priority: .userInitiated) { () throws -> String in
            var error: NSError?
            let imported = instance.importProfileSource(
                viaLine: request.profileJSON, rawURL: request.url, lineID: request.lineID,
                systemDNSJSON: request.systemDNSJSON, basePath: basePath,
                namespace: request.namespace, nodesOnly: request.nodesOnly, error: &error)
            if let error { throw error }
            return imported
        }
        let id = UUID()
        applicationSessions[id] = ApplicationSession(instance: instance, task: task)
        defer { applicationSessions.removeValue(forKey: id) }
        return try await withTaskCancellationHandler {
            do {
                return try await task.value
            } catch {
                throw failure(code: ProfileSourceLineFetchOutcome.code(for: error), lineName: lineName,
                              cancelled: task.isCancelled)
            }
        } onCancel: {
            instance.cancelPreparation()
            task.cancel()
        }
    }

    private static func importInProvider(
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

    nonisolated static func failure(code: String, lineName: String, cancelled: Bool = false) -> ProfileLibraryError {
        if cancelled {
            return .invalid("XDial 开始连接，已停止通过「\(lineName)」更新订阅；原配置已保留")
        }
        switch code {
        case "unsupported-line":
            return .invalid("「\(lineName)」这类线路暂不支持用于下载订阅，请改用直连或其他代理线路；原配置已保留")
        case "busy":
            return .invalid("另一份订阅正在通过线路下载，稍后再试；原配置已保留")
        case "transaction-mismatch", "session-not-committed":
            return .invalid("XDial 连接状态刚刚变化，请稍后再更新此订阅；原配置已保留")
        default:
            return .invalid("无法通过「\(lineName)」获取订阅，请检查线路及当前网络；原配置已保留")
        }
    }
}
