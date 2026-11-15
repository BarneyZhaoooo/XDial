import Foundation

/// 把一个场景的分流规则说成一句人话，例如「公司内网走公司 VPN，其余直连」。
enum ScenarioDescriber {
    static func summary(for scenario: Scenario, in profile: Profile, isChinese: Bool) -> String {
        let defaultTarget = target(
            lineID: scenario.defaultLineID,
            subscriptionID: scenario.defaultSubscriptionID,
            in: profile,
            isChinese: isChinese
        )

        // 按出口分组，保持规则在场景里的先后顺序；去往默认出口的规则不用单独说。
        var groups: [(target: RouteTarget, rules: [String])] = []
        for binding in scenario.bindings {
            guard !RuleSet.isConnectivityTestRuleID(binding.ruleSetID),
                  let rule = profile.ruleSets.first(where: { $0.id == binding.ruleSetID }),
                  rule.enabled,
                  let bound = target(
                    lineID: binding.lineID,
                    subscriptionID: binding.subscriptionID,
                    in: profile,
                    isChinese: isChinese
                  ),
                  bound.key != defaultTarget?.key else { continue }
            let name = rule.name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { continue }
            if let index = groups.firstIndex(where: { $0.target.key == bound.key }) {
                groups[index].rules.append(name)
            } else {
                groups.append((bound, [name]))
            }
        }

        guard !groups.isEmpty else {
            guard let defaultTarget else {
                return isChinese ? "还没有设置出口" : "No route is set"
            }
            if defaultTarget.isDirect {
                return isChinese ? "全部直连，不经过 VPN" : "All traffic goes direct, no VPN"
            }
            return isChinese ? "全部流量走\(defaultTarget.name)" : "All traffic via \(defaultTarget.name)"
        }

        var parts = groups.map { group -> String in
            let names = ruleList(group.rules, isChinese: isChinese)
            // 规则名以英文或数字结尾时，和后面的中文之间留一个空格。
            let gap = names.unicodeScalars.last.map { $0.isASCII && $0 != " " } == true ? " " : ""
            if group.target.isDirect {
                return isChinese ? "\(names)\(gap)直连" : "\(names) direct"
            }
            return isChinese ? "\(names)\(gap)走\(group.target.name)" : "\(names) via \(group.target.name)"
        }
        if let defaultTarget {
            if defaultTarget.isDirect {
                parts.append(isChinese ? "其余直连" : "everything else direct")
            } else {
                parts.append(isChinese ? "其余走\(defaultTarget.name)" : "everything else via \(defaultTarget.name)")
            }
        }
        return parts.joined(separator: isChinese ? "，" : ", ")
    }

    /// 场景图标：场景自己指定了图标就用指定的（预制场景都指定了）；
    /// 否则默认出口是 VPN 的用地球，只有部分流量走 VPN 的用楼，全部直连的用 Wi-Fi。
    static func symbol(for scenario: Scenario, in profile: Profile) -> String {
        switch scenario.iconOverride {
        case "office": return "building.2.fill"
        case "regional": return "globe.asia.australia.fill"
        case "travel": return "airplane"
        // "hongkong" 是早期测试包里用过的键，已经存进配置的场景继续认它。
        case "harbor", "hongkong": return "ferry.fill"
        case "home": return "house.fill"
        default: break
        }
        let defaultTarget = target(
            lineID: scenario.defaultLineID,
            subscriptionID: scenario.defaultSubscriptionID,
            in: profile,
            isChinese: true
        )
        if let defaultTarget, !defaultTarget.isDirect { return "globe.asia.australia.fill" }
        let usesTunnel = scenario.bindings.contains { binding in
            guard !RuleSet.isConnectivityTestRuleID(binding.ruleSetID) else { return false }
            let bound = target(
                lineID: binding.lineID,
                subscriptionID: binding.subscriptionID,
                in: profile,
                isChinese: true
            )
            return bound.map { !$0.isDirect } ?? false
        }
        return usesTunnel ? "building.2.fill" : "wifi"
    }

    private struct RouteTarget {
        let key: String
        let name: String
        let isDirect: Bool
    }

    private static func target(
        lineID: String,
        subscriptionID: String,
        in profile: Profile,
        isChinese: Bool
    ) -> RouteTarget? {
        if !subscriptionID.isEmpty {
            guard let subscription = profile.subscriptions.first(where: { $0.id == subscriptionID }) else {
                return nil
            }
            return RouteTarget(key: "sub:\(subscriptionID)", name: subscription.name, isDirect: false)
        }
        guard let line = profile.lines.first(where: { $0.id == lineID }) else { return nil }
        let isDirect = line.type == "direct"
        let trimmed = line.name.trimmingCharacters(in: .whitespacesAndNewlines)
        let name = isDirect ? (isChinese ? "直连" : "direct") : (trimmed.isEmpty ? line.id : trimmed)
        return RouteTarget(key: "line:\(lineID)", name: name, isDirect: isDirect)
    }

    private static func ruleList(_ names: [String], isChinese: Bool) -> String {
        let shown = names.prefix(3).joined(separator: isChinese ? "、" : ", ")
        guard names.count > 3 else { return shown }
        return isChinese ? "\(shown)等 \(names.count) 项" : "\(shown) and \(names.count - 3) more"
    }
}

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

    static func classify(_ rawError: String?, isChinese: Bool) -> ConnectionProblem? {
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
                title: isChinese ? "连不上公司 VPN 服务器" : "Can't reach the company VPN server",
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
        connect()
    }
}
