import CloudKit
import CryptoKit
import SwiftUI

/// The runtime profile is shared by every platform. Desktop source ownership and
/// subscription baselines travel alongside it, including through a mobile edit.
struct CloudConfiguration: Codable {
    var version = 1
    var profile: Profile
    var library: Data?
    var settings: [String: String]

    static func encode<T: Encodable>(_ value: T) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return try encoder.encode(value)
    }
}

extension Line {
    fileprivate var forCloud: Line {
        var line = self
        line.verified = line.type == "direct"
        line.tailscaleAuthenticated = false
        line.identityHostname = ""
        return line
    }
}

extension Profile {
    var forCloud: Profile {
        var result = self
        result.tailscale = TailscaleIdentity()
        result.lines = lines.map(\.forCloud)
        for index in result.subscriptions.indices {
            result.subscriptions[index].lines = result.subscriptions[index].lines.map(\.forCloud)
        }
        #if !os(macOS)
        result.ruleSets.removeAll { $0.isConnectivityTestRule }
        for index in result.scenarios.indices {
            result.scenarios[index].matchOrder.removeAll(where: RuleSet.isConnectivityTestRuleID)
            result.scenarios[index].bindings.removeAll { RuleSet.isConnectivityTestRuleID($0.ruleSetID) }
        }
        #endif
        return result
    }

    func restoringDeviceState(from local: Profile?) -> Profile {
        var result = self
        result.tailscale = local?.tailscale ?? TailscaleIdentity()
        func restore(_ lines: [Line], from previous: [Line]) -> [Line] {
            let old = Dictionary(previous.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
            return lines.map { incoming in
                var line = incoming.forCloud
                guard let previous = old[line.id], previous.type == line.type,
                      previous.identityProfileID == line.identityProfileID else { return line }
                line.tailscaleAuthenticated = previous.tailscaleAuthenticated
                if previous.forCloud == line.forCloud { line.verified = previous.verified }
                return line
            }
        }
        result.lines = restore(lines, from: local?.lines ?? [])
        for i in result.subscriptions.indices {
            let old = local?.subscriptions.first { $0.id == result.subscriptions[i].id }
            result.subscriptions[i].lines = restore(result.subscriptions[i].lines, from: old?.lines ?? [])
        }
        return result
    }

}

struct CloudSyncIssue: LocalizedError {
    var errorDescription: String?
    init(_ message: String) { errorDescription = message }
}

@MainActor
final class ConfigurationSync: ObservableObject {
    enum Choice { case automatic, local, cloud }
    @Published private(set) var enabled = false
    @Published private(set) var busy = false
    @Published private(set) var conflict = false
    @Published private(set) var message = ""
    private var conflictTag: String?
    private var defaults: UserDefaults = .standard
    private var capture: (() throws -> CloudConfiguration)?
    private var apply: ((CloudConfiguration) throws -> Void)?
    private var timer: Task<Void, Never>?
    private var accountChanges: Task<Void, Never>?
    private var generation = UUID()
    private let prefix = "xdial.cloud.v1."
    private let recordID: CKRecord.ID

    init(recordName: String = "configuration") {
        recordID = CKRecord.ID(recordName: recordName)
    }

    private var marker: [String: String] {
        get { defaults.dictionary(forKey: prefix + "marker") as? [String: String] ?? [:] }
        set { defaults.set(newValue, forKey: prefix + "marker") }
    }

    func configure(defaults: UserDefaults, capture: @escaping () throws -> CloudConfiguration,
                   apply: @escaping (CloudConfiguration) throws -> Void) {
        self.defaults = defaults
        self.capture = capture
        self.apply = apply
        if defaults.bool(forKey: prefix + "enabled") { setEnabled(true) }
    }

    func setEnabled(_ value: Bool) {
        generation = UUID()
        timer?.cancel()
        accountChanges?.cancel()
        enabled = value
        defaults.set(value, forKey: prefix + "enabled")
        message = ""
        conflict = false
        conflictTag = nil
        guard value else { return }
        accountChanges = Task { [weak self] in
            for await _ in NotificationCenter.default.notifications(named: .CKAccountChanged) {
                guard let self, !Task.isCancelled else { return }
                self.setEnabled(false)
                self.message = "iCloud 账号状态已变更，请确认账号后重新开启同步 · Check your iCloud account to resume"
                return
            }
        }
        timer = Task { [weak self] in
            while !Task.isCancelled {
                await self?.sync()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    func sync(_ choice: Choice = .automatic) async {
        guard enabled, !busy, let capture, let apply else { return }
        busy = true
        defer { busy = false }
        let request = generation
        do {
            // Capture checks consent and local persistence before any network access.
            _ = try capture()
            guard let identifier = Bundle.main.object(forInfoDictionaryKey: "XDialCloudContainer") as? String,
                  identifier.hasPrefix("iCloud.") else {
                throw CloudSyncIssue("构建缺少 iCloud 容器 · Missing iCloud container")
            }
            let container = CKContainer(identifier: identifier)
            let owner = try await container.userRecordID().recordName
            guard generation == request else { return }
            if let previousOwner = marker["owner"], previousOwner != owner {
                setEnabled(false)
                marker = [:]
                message = "iCloud account changed · iCloud 账号已更换，请重新开启同步"
                return
            }
            var checkpoint = marker
            checkpoint["owner"] = owner
            marker = checkpoint
            let database = container.privateCloudDatabase
            let record: CKRecord
            do {
                let metadata = try await database.records(for: [recordID], desiredKeys: [])
                guard let result = metadata[recordID] else { throw CloudSyncIssue("未收到云端记录 · No cloud response") }
                let header = try result.get()
                guard generation == request else { return }
                if header.recordChangeTag == marker["tag"], try digest(capture()) == marker["hash"] {
                    conflict = false
                    message = "已同步 · Up to date"
                    return
                }
                record = try await database.record(for: recordID)
            }
            catch let error as CKError where error.code == .unknownItem {
                record = CKRecord(recordType: "XDialConfiguration", recordID: recordID)
            }
            guard generation == request else { return }
            let local = try capture()
            let localHash = try digest(local)
            let tag = record.recordChangeTag
            let changedHere = marker["hash"] != localHash
            let changedThere = marker["tag"] != tag
            if tag == nil && marker["tag"] != nil {
                setEnabled(false)
                marker = [:]
                message = "Cloud copy removed · 云端配置已删除，请重新开启同步"
                return
            }
            if tag != nil && changedHere && changedThere && (choice == .automatic || conflictTag != tag) {
                conflict = true
                conflictTag = tag
                message = "选择要保留的配置 · Choose the configuration to keep"
                return
            }
            let remote: CloudConfiguration?
            if tag != nil && (changedHere || changedThere) {
                guard let asset = record["archive"] as? CKAsset, let url = asset.fileURL else {
                    throw CloudSyncIssue("云端配置缺失 · Missing cloud configuration")
                }
                let decoded = try JSONDecoder().decode(CloudConfiguration.self, from: Data(contentsOf: url))
                guard decoded.version == 1, !decoded.profile.lines.isEmpty else { throw CloudSyncIssue("请更新 XDial · Update XDial") }
                remote = decoded
            } else { remote = nil }

            if let remote, choice == .cloud || (changedThere && !changedHere) {
                try apply(remote)
                marker = ["owner": owner, "tag": tag!, "hash": try digest(capture())]
            } else if tag == nil || changedHere {
                var archive = local
                if let remote {
                    if archive.library == nil { archive.library = remote.library }
                    archive.settings = remote.settings.merging(local.settings) { _, new in new }
                }
                let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false,
                                                       attributes: [.posixPermissions: 0o700])
                defer { try? FileManager.default.removeItem(at: dir) }
                let url = dir.appendingPathComponent("configuration")
                try CloudConfiguration.encode(archive).write(to: url, options: [.completeFileProtection])
                record["archive"] = CKAsset(fileURL: url)
                let result = try await database.modifyRecords(saving: [record], deleting: [], savePolicy: .ifServerRecordUnchanged, atomically: false)
                guard generation == request else { return }
                guard let saved = try result.saveResults[recordID]?.get(), let newTag = saved.recordChangeTag else {
                    throw CloudSyncIssue("云端未确认保存 · Save was not confirmed")
                }
                marker = ["owner": owner, "tag": newTag, "hash": localHash]
            }
            conflict = false
            message = "已同步 · Up to date"
        } catch {
            guard generation == request else { return }
            message = error.localizedDescription
        }
    }

    private func digest(_ configuration: CloudConfiguration) throws -> String {
        Data(SHA256.hash(data: try CloudConfiguration.encode(configuration))).base64EncodedString()
    }
}

struct CloudConfigurationSettings: View {
    @ObservedObject var sync: ConfigurationSync
    let chinese: Bool
    @State private var confirm = false
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Toggle(chinese ? "iCloud 同步" : "iCloud Sync", isOn: Binding(
                get: { sync.enabled }, set: { if $0 { confirm = true } else { sync.setEnabled(false) } }
            ))
            if sync.enabled {
                if sync.conflict {
                    Button(chinese ? "用 iCloud 配置覆盖本机" : "Replace local configuration with iCloud copy") {
                        Task { await sync.sync(.cloud) }
                    }
                    Button(chinese ? "用本机配置覆盖 iCloud" : "Replace iCloud configuration with local copy") {
                        Task { await sync.sync(.local) }
                    }
                } else {
                    Button(chinese ? "立即同步" : "Sync now") { Task { await sync.sync() } }
                }
            }
            if !sync.message.isEmpty { Text(sync.message).font(.footnote) }
        }
        .disabled(sync.busy)
        .alert(chinese ? "通过 iCloud 同步？" : "Sync through iCloud?", isPresented: $confirm) {
            Button(chinese ? "开启同步" : "Enable sync") { sync.setEnabled(true) }
            Button(chinese ? "取消" : "Cancel", role: .cancel) {}
        } message: {
            Text(chinese ? "配置、偏好、VPN 密码和订阅地址将存入你的私人 iCloud，其他开启同步的 XDial 设备可以读取。" : "Your private iCloud will store configuration, preferences, VPN passwords and subscription URLs for your other XDial devices with sync enabled.")
        }
    }
}
