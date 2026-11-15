import CryptoKit
import Foundation

/// 随 App 带一份远程规则。
///
/// 远程规则平时在连接前现下；所在网络下不到、本机又没有缓存时，连接会直接失败
/// （rule set … is unavailable and has no valid local copy）。企业包可以在打包时把规则文件
/// 一起带进来（见 ios/project.yml 的「Embed private build configuration」）：连接前先把带来的
/// 那份放进缓存目录，没网也能连；之后引擎照常按自己的节奏去更新。没带规则时这里什么都不做。
enum BundledRuleSets {
    /// App 包里 PrivateConfig/RuleSets/manifest.json 的内容：规则地址 → 同目录下的文件名。
    static func manifest(bundle: Bundle = .main) -> [String: String] {
        guard let url = bundle.url(forResource: "manifest", withExtension: "json", subdirectory: "PrivateConfig/RuleSets"),
              let data = try? Data(contentsOf: url),
              let manifest = try? JSONDecoder().decode([String: String].self, from: data) else { return [:] }
        return manifest
    }

    /// 引擎给一份远程规则算出来的缓存文件名，必须和 Go 里的 neRuleSetCachePath 一致：
    /// sha256("profile\0规则ID\0地址\0格式") 的前 12 个字节。
    static func cacheFileName(ruleSetID: String, url: String) -> String {
        let key = ["profile", ruleSetID, url, "binary"].joined(separator: "\u{0}")
        let digest = SHA256.hash(data: Data(key.utf8))
        return digest.prefix(12).map { String(format: "%02x", $0) }.joined() + ".srs"
    }

    /// 给还没有缓存的规则放上随 App 带的那份。已经有缓存的不动。返回这次放了哪些规则。
    @discardableResult
    static func seedCache(
        for profile: Profile,
        basePath: String,
        bundle: Bundle = .main,
        fileManager: FileManager = .default
    ) -> [String] {
        guard !basePath.isEmpty else { return [] }
        let manifest = manifest(bundle: bundle)
        guard !manifest.isEmpty else { return [] }
        let directory = URL(fileURLWithPath: basePath).appendingPathComponent("xdial-rule-sets", isDirectory: true)
        var seeded: [String] = []
        for ruleSet in profile.ruleSets where ruleSet.type == "url" && ruleSet.enabled {
            // 只认二进制规则；用户改过地址或格式的，按用户的来。
            guard ["srs", "binary", "auto", ""].contains(ruleSet.format.lowercased()),
                  let fileName = manifest[ruleSet.url], fileName.hasSuffix(".srs"),
                  !fileName.contains("/"),
                  let source = bundle.url(
                      forResource: String(fileName.dropLast(4)),
                      withExtension: "srs",
                      subdirectory: "PrivateConfig/RuleSets"
                  ) else { continue }
            let target = directory.appendingPathComponent(cacheFileName(ruleSetID: ruleSet.id, url: ruleSet.url))
            let existingSize = (try? fileManager.attributesOfItem(atPath: target.path)[.size] as? NSNumber)?.int64Value ?? 0
            guard existingSize <= 0 else { continue }
            do {
                try fileManager.createDirectory(
                    at: directory,
                    withIntermediateDirectories: true,
                    attributes: [.posixPermissions: 0o700]
                )
                try Data(contentsOf: source).write(to: target, options: .atomic)
                // 把时间标成两天前：引擎会当它是「能用但该更新了」，连上以后自己去拉新的。
                try? fileManager.setAttributes(
                    [.modificationDate: Date().addingTimeInterval(-2 * 24 * 3600)],
                    ofItemAtPath: target.path
                )
                seeded.append(ruleSet.id)
            } catch {
                continue
            }
        }
        return seeded
    }
}
