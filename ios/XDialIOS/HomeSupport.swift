import Foundation

/// 把引擎给出的原始报错翻译成「出了什么事、现在该做什么」。
struct ConnectionProblem: Equatable {
    enum Kind: Equatable {
        /// 账号或密码不对，需要重新登录。
        case credentials
        /// 网络不通或太慢。
        case network
        /// 服务器地址或证书有问题。
        case server
        /// 本机保存的配置已损坏，需要恢复默认。
        case needsReset
        case other
    }

    let kind: Kind
    let title: String
    let advice: String
    /// 原始报错，放在「查看详情」里。
    let detail: String

    static func classify(_ rawError: String?, isChinese: Bool, companyPreset: Bool = true) -> ConnectionProblem? {
        guard let detail = rawError?.trimmingCharacters(in: .whitespacesAndNewlines), !detail.isEmpty else {
            return nil
        }
        let text = detail.lowercased()
        func has(_ needles: String...) -> Bool { needles.contains { text.contains($0) } }

        if has("安全配置版本不一致", "secure configuration versions") {
            return ConnectionProblem(
                kind: .needsReset,
                title: isChinese ? "本机保存的配置读不出来了" : "Saved configuration can't be read",
                advice: isChinese
                    ? "恢复默认配置后重新登录即可，账号需要再填一次。"
                    : "Restore the default configuration, then sign in again.",
                detail: detail
            )
        }
        if has("login failed", "enter your password", "enter your username", "authentication failed",
               "账号或密码", "认证失败", "permission denied") {
            return ConnectionProblem(
                kind: .credentials,
                title: isChinese ? "账号或密码不正确" : "Wrong username or password",
                advice: isChinese
                    ? "检查后重新登录。连续输错可能会被暂时锁定。"
                    : "Check them and sign in again. Repeated failures may lock the account.",
                detail: detail
            )
        }
        if has("certificate", "x509", "证书", "no such host", "server endpoint", "服务器地址",
               "connection refused", "dial tcp") {
            return ConnectionProblem(
                kind: .server,
                title: companyPreset
                    ? (isChinese ? "连不上公司 VPN 服务器" : "Can't reach the company VPN server")
                    : (isChinese ? "连不上 VPN 服务器" : "Can't reach the VPN server"),
                advice: isChinese
                    ? "多半是当前网络拦住了。换个网络再试，或者在账号里核对服务器地址。"
                    : "The current network is probably blocking it. Try another network, or check the server address in Account.",
                detail: detail
            )
        }
        if has("已自动断开", "deadline exceeded", "timeout", "timed out", "超时", "unreachable",
               "no route", "不通", "connection reset", "offline", "network is down") {
            return ConnectionProblem(
                kind: .network,
                title: isChinese ? "网络不通或太慢" : "Network is down or too slow",
                advice: isChinese
                    ? "换个网络，或者稍等一会儿再连。"
                    : "Switch networks, or wait a moment and try again.",
                detail: detail
            )
        }
        return ConnectionProblem(
            kind: .other,
            title: isChinese ? "没连上" : "Couldn't connect",
            advice: isChinese
                ? "再试一次；还不行的话把详情发给管理员。"
                : "Try again. If it keeps failing, send the details to your admin.",
            detail: detail
        )
    }
}

@MainActor
extension AppState {
    /// 需要账号密码的那条公司 VPN 线路：优先当前场景用到的，其次任意一条启用的。
    var accountLine: Line? {
        if let id = activeAnyConnectLineIDs.sorted().first,
           let line = profile.lines.first(where: { $0.id == id }) {
            return line
        }
        return profile.lines.first { $0.type == "vpn" && $0.enabled }
    }

    /// 账号线路还缺服务器、用户名或密码。
    var accountIsIncomplete: Bool {
        guard let line = accountLine else { return false }
        return line.vpnServer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || line.vpnUsername.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || line.vpnPassword.isEmpty
    }

    /// 当前场景要走公司 VPN，但账号还没填完：主屏据此显示「登录」。
    var activeScenarioNeedsSignIn: Bool {
        guard !persistenceRequiresRecovery, !activeAnyConnectLineIDs.isEmpty else { return false }
        return accountIsIncomplete
    }

    /// 保存登录表单。连接期间不允许改账号。
    func saveAccount(lineID: String, server: String, username: String, password: String) -> Bool {
        guard canMutateConfiguration,
              let index = profile.lines.firstIndex(where: { $0.id == lineID }) else { return false }
        profile.lines[index].vpnServer = server.trimmingCharacters(in: .whitespacesAndNewlines)
        profile.lines[index].vpnUsername = username.trimmingCharacters(in: .whitespacesAndNewlines)
        profile.lines[index].vpnPassword = password
        return save()
    }

    /// 未连接时直接切换场景。
    @discardableResult
    func selectScenario(_ id: String) -> Bool {
        guard canMutateConfiguration,
              profile.scenarios.contains(where: { $0.id == id }) else { return false }
        guard profile.activeScenarioID != id else { return true }
        profile.activeScenarioID = id
        return save()
    }

    /// 已连接时切换场景：先断开，换好场景，再用新场景连上。
    func switchScenarioAndReconnect(to id: String) async {
        guard profile.scenarios.contains(where: { $0.id == id }),
              profile.activeScenarioID != id else { return }
        if hasActiveTunnel || isBusy {
            disconnect()
            let deadline = ContinuousClock.now + .seconds(15)
            while hasActiveTunnel || isBusy, ContinuousClock.now < deadline {
                try? await Task.sleep(for: .milliseconds(150))
            }
        }
        guard selectScenario(id), canConnect else { return }
        connect(origin: .scenarioChange)
    }
}
