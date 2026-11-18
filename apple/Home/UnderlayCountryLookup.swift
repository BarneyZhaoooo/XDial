import Foundation
import Network

enum UnderlayCountryLookup {
    static func lookup() async -> (code: String?, failure: String?) {
        let appleReply = await DirectHTTPSProbe(host: "gspe1-ssl.ls.apple.com", path: "/pep/gcc").run()
        if let code = appleReply.flatMap(countryCode(inAppleReply:)) {
            return (code: code, failure: nil)
        }
        let cloudflareReply = await DirectHTTPSProbe(host: "www.cloudflare.com", path: "/cdn-cgi/trace").run()
        if let code = cloudflareReply.flatMap(countryCode(inCloudflareTrace:)) {
            return (code: code, failure: nil)
        }
        return (code: nil, failure: localFailure(apple: appleReply, cloudflare: cloudflareReply))
    }

    /// 例如「Apple 无回应；Cloudflare 回复无效」。只说结果类别，不带回复内容。
    static func localFailure(apple: String?, cloudflare: String?) -> String {
        func describe(_ reply: String?) -> String { reply == nil ? "无回应" : "回复无效" }
        return "Apple \(describe(apple))；Cloudflare \(describe(cloudflare))"
    }

    /// 苹果的回复正文就是两位地区代码。
    static func countryCode(inAppleReply text: String) -> String? {
        guard text.hasPrefix("HTTP/1.1 200") || text.hasPrefix("HTTP/1.0 200"),
              let bodyStart = text.range(of: "\r\n\r\n") else { return nil }
        return text[bodyStart.upperBound...]
            .split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .first { $0.count == 2 && $0.allSatisfy { $0.isASCII && $0.isUppercase } }
    }

    /// Cloudflare 的回复里有一行 loc=XX。
    static func countryCode(inCloudflareTrace text: String) -> String? {
        for line in text.split(whereSeparator: \.isNewline) where line.hasPrefix("loc=") {
            let code = line.dropFirst(4).trimmingCharacters(in: .whitespaces).uppercased()
            // XX、T1 是 Cloudflare 表示「不知道」的占位值。
            if code.count == 2, code != "XX", code != "T1", code.allSatisfy(\.isLetter) { return code }
        }
        return nil
    }

}

/// 一次最简单的 HTTPS GET，连接禁止走虚拟网卡（VPN 隧道），只能从 Wi-Fi 或蜂窝网络直接出去。
private final class DirectHTTPSProbe: @unchecked Sendable {
    // 所有可变状态都只在 queue 上读写。
    private let queue = DispatchQueue(label: "xdial.direct-https-probe")
    private let connection: NWConnection
    private let request: Data
    private var buffer = Data()
    private var continuation: CheckedContinuation<String?, Never>?

    init(host: String, path: String) {
        let parameters = NWParameters.tls
        #if os(iOS)
        parameters.prohibitedInterfaceTypes = [.other, .loopback]
        #endif
        connection = NWConnection(host: NWEndpoint.Host(host), port: 443, using: parameters)
        request = Data(
            "GET \(path) HTTP/1.1\r\nHost: \(host)\r\nUser-Agent: XDial\r\nAccept: */*\r\nConnection: close\r\n\r\n".utf8
        )
    }

    func run(timeout: TimeInterval = 2.5) async -> String? {
        await withCheckedContinuation { continuation in
            queue.async {
                self.continuation = continuation
                self.connection.stateUpdateHandler = { [weak self] state in
                    guard let self else { return }
                    switch state {
                    case .ready:
                        self.send()
                    case .failed, .cancelled:
                        self.finish()
                    default:
                        break
                    }
                }
                self.connection.start(queue: self.queue)
                self.queue.asyncAfter(deadline: .now() + timeout) { self.finish() }
            }
        }
    }

    private func send() {
        connection.send(content: request, completion: .contentProcessed { [weak self] error in
            guard let self else { return }
            if error != nil { self.finish() } else { self.receive() }
        })
    }

    private func receive() {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 16_384) { [weak self] data, _, isComplete, error in
            guard let self else { return }
            if let data { self.buffer.append(data) }
            if isComplete || error != nil || self.buffer.count > 32_768 {
                self.finish()
            } else {
                self.receive()
            }
        }
    }

    private func finish() {
        guard let continuation else { return }
        self.continuation = nil
        connection.cancel()
        continuation.resume(returning: buffer.isEmpty ? nil : String(decoding: buffer, as: UTF8.self))
    }
}
