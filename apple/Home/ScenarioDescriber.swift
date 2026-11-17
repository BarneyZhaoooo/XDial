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
            #if os(iOS)
            if RuleSet.isConnectivityTestRuleID(binding.ruleSetID) { continue }
            #endif
            guard let rule = profile.ruleSets.first(where: { $0.id == binding.ruleSetID }),
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
        #if os(macOS)
        return ScenarioIconCatalog.resolvedPreset(for: scenario).symbol
        #else
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
        #endif
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
