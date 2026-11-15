import SwiftUI
import UniformTypeIdentifiers

struct MobileSettingsView: View {
    @EnvironmentObject private var app: AppState

    @State private var exportDocument: MobileJSONDocument?
    @State private var isExporting = false
    @State private var isImporting = false
    @State private var isConfirmingReset = false
    @State private var isConfirmingProfileRemoval = false
    @State private var notice: SettingsNotice?
    @State private var signInTarget: SettingsAccountTarget?
    @State private var showsLiveActivity = ConnectionActivityController.isEnabledByUser
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                if let line = app.accountLine {
                    Section(app.tr("账号", "Account")) {
                        Button {
                            signInTarget = SettingsAccountTarget(id: line.id)
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: "person.crop.circle.fill")
                                    .font(.title)
                                    .foregroundStyle(XDialTheme.accent)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(line.name.isEmpty ? app.tr("公司 VPN", "Company VPN") : line.name)
                                        .foregroundStyle(XDialTheme.ink)
                                    Text(app.accountIsIncomplete
                                         ? app.tr("未登录", "Not signed in")
                                         : line.vpnUsername)
                                        .font(.subheadline)
                                        .foregroundStyle(XDialTheme.inkSecondary)
                                }
                                Spacer()
                                Image(systemName: "chevron.right")
                                    .font(.footnote.weight(.semibold))
                                    .foregroundStyle(XDialTheme.inkTertiary)
                            }
                        }
                        .accessibilityIdentifier("settings-account")
                    }
                }

                Section {
                    Toggle(
                        app.tr("掉线后自动重连", "Reconnect after a drop"),
                        isOn: $app.autoReconnectEnabled
                    )
                    Toggle(
                        app.tr("换网络后自动连回", "Reconnect when the network changes"),
                        isOn: $app.systemOnDemandEnabled
                    )
                    .accessibilityIdentifier("system-on-demand-reconnect")
                    Toggle(
                        app.tr("在灵动岛和锁屏显示", "Show in Dynamic Island and Lock Screen"),
                        isOn: $showsLiveActivity
                    )
                    .onChange(of: showsLiveActivity) { _, isOn in
                        ConnectionActivityController.isEnabledByUser = isOn
                        MobileAppRuntime.connectionActivity?.preferenceChanged()
                    }
                    .accessibilityIdentifier("settings-live-activity")
                    if app.systemOnDemandEnabled {
                        LabeledContent(app.tr("当前状态", "Status")) {
                            Text(systemOnDemandStatusText)
                                .accessibilityIdentifier("system-on-demand-status")
                        }
                    }
                } header: {
                    Text(app.tr("连接", "Connection"))
                } footer: {
                    Text(app.tr(
                        "掉线后最多自动重试 3 次。「换网络后自动连回」由系统接管，手动断开后会暂停，下次连上再恢复。",
                        "Drops are retried up to 3 times. Network-change reconnect is handled by iOS; it pauses after a manual disconnect and resumes on the next connection."
                    ))
                }

                Section {
                    NavigationLink {
                        ConfigurationView()
                    } label: {
                        Label(app.tr("线路与规则", "Lines & Rules"), systemImage: "slider.horizontal.3")
                    }
                    .accessibilityIdentifier("settings-advanced")

                    Button {
                        prepareExport()
                    } label: {
                        Label(app.tr("导出配置", "Export Configuration"), systemImage: "square.and.arrow.up")
                    }

                    Button {
                        isImporting = true
                    } label: {
                        Label(app.tr("导入配置", "Import Configuration"), systemImage: "square.and.arrow.down")
                    }
                    .disabled(app.hasActiveTunnel || app.isBusy)
                } header: {
                    Text(app.tr("高级", "Advanced"))
                } footer: {
                    Text(app.tr(
                        "一般不用改。导出的文件不含账号和密码。",
                        "You rarely need these. Exports never include accounts or passwords."
                    ))
                }

                Section(app.tr("帮助", "Help")) {
                    NavigationLink {
                        MobileDiagnosticsDetailView(
                            title: app.tr("诊断信息", "Diagnostics"),
                            shareTitle: app.tr("分享诊断", "Share diagnostics"),
                            report: diagnosticReport
                        )
                    } label: {
                        Label(app.tr("诊断信息", "Diagnostics"), systemImage: "stethoscope")
                    }
                    .accessibilityIdentifier("settings-diagnostics")

                    Picker(selection: $app.language) {
                        Text("简体中文").tag(Lang.zh)
                        Text("English").tag(Lang.en)
                    } label: {
                        Label(app.tr("语言", "Language"), systemImage: "globe")
                    }

                    LabeledContent {
                        Text(version)
                    } label: {
                        Label(app.tr("版本", "Version"), systemImage: "info.circle")
                    }
                }

                Section {
                    if app.helperInstalled {
                        Button(role: .destructive) {
                            isConfirmingProfileRemoval = true
                        } label: {
                            Label(app.tr("移除系统 VPN 配置", "Remove System VPN Profile"), systemImage: "trash")
                        }
                        .disabled(app.hasActiveTunnel || app.isBusy)
                    }
                    Button(role: .destructive) {
                        isConfirmingReset = true
                    } label: {
                        Label(app.tr("恢复默认配置", "Restore Default Configuration"), systemImage: "arrow.counterclockwise")
                    }
                    .disabled(app.hasActiveTunnel || app.isBusy)
                } footer: {
                    Text(app.tr(
                        "恢复默认会回到公司预置的线路和场景，账号需要重新登录。连接期间不可用。",
                        "Restoring returns to the company's lines and scenarios; you will sign in again. Unavailable while connected."
                    ))
                }
            }
            .xdialGroupedBackground()
            .navigationTitle(app.tr("设置", "Settings"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(app.tr("完成", "Done")) { dismiss() }
                        .accessibilityIdentifier("settings-done")
                }
            }
            .sheet(item: $signInTarget) { target in
                SignInSheet(lineID: target.id)
                    .environmentObject(app)
            }
            .fileExporter(
                isPresented: $isExporting,
                document: exportDocument,
                contentType: .json,
                defaultFilename: "XDial-configuration"
            ) { result in
                if case .failure(let error) = result {
                    showError(app.tr("导出失败", "Export failed"), error: error)
                }
                exportDocument = nil
            }
            .fileImporter(isPresented: $isImporting, allowedContentTypes: [.json]) { result in
                handleImport(result)
            }
            .confirmationDialog(
                app.tr("恢复默认配置？", "Restore the default configuration?"),
                isPresented: $isConfirmingReset,
                titleVisibility: .visible
            ) {
                Button(app.tr("恢复默认", "Restore Defaults"), role: .destructive) {
                    resetConfiguration()
                }
                Button(app.tr("取消", "Cancel"), role: .cancel) {}
            } message: {
                Text(app.tr(
                    "线路、规则和场景会回到公司默认配置，当前配置不会保留。",
                    "Lines, rules and scenarios return to the company defaults. The current configuration is not kept."
                ))
            }
            .confirmationDialog(
                app.tr("移除系统 VPN 配置？", "Remove the system VPN profile?"),
                isPresented: $isConfirmingProfileRemoval,
                titleVisibility: .visible
            ) {
                Button(app.tr("移除", "Remove"), role: .destructive) {
                    removeSystemProfile()
                }
                Button(app.tr("取消", "Cancel"), role: .cancel) {}
            } message: {
                Text(app.tr(
                    "下次连接时系统会再问一次是否允许添加。XDial 里的配置和账号不受影响。",
                    "iOS will ask for permission again on the next connection. Your XDial configuration and account are kept."
                ))
            }
            .alert(item: $notice) { notice in
                Alert(
                    title: Text(notice.title),
                    message: Text(notice.message),
                    dismissButton: .default(Text(app.tr("好", "OK")))
                )
            }
        }
        .tint(XDialTheme.accent)
    }

    private var version: String {
        let short = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "1"
        return "\(short) (\(build))"
    }

    private var diagnosticStatus: String {
        switch app.engine.status {
        case "connected": return app.tr("已连接", "Connected")
        case "connecting": return app.tr("正在连接", "Connecting")
        case "checking": return app.tr("正在验证数据链路", "Checking data path")
        case "action-required": return app.tr("需要登录 Tailscale", "Tailscale sign-in required")
        case "disconnecting": return app.tr("正在断开", "Disconnecting")
        case "reconnecting": return app.tr("正在重连", "Reconnecting")
        default: return app.tr("未连接", "Not connected")
        }
    }

    private var systemOnDemandStatusText: String {
        switch app.systemOnDemandState {
        case .disabled:
            return app.tr("未启用", "Disabled")
        case .pending:
            return app.tr("待连接验收", "Pending connection check")
        case .active:
            return app.tr("系统已启用", "Enabled by system")
        case .failed:
            return app.tr("启用失败", "Enable failed")
        }
    }

    private var diagnosticReport: String {
        MobileDiagnosticsService.report(
            version: version,
            status: diagnosticStatus,
            systemProfileInstalled: app.helperInstalled,
            profile: app.profile,
            lastError: app.engine.lastError,
            dataPathSummary: app.engine.dataPathSummary,
            isChinese: app.language == .zh
        )
    }

    private func prepareExport() {
        do {
            exportDocument = MobileJSONDocument(data: try MobileConfigurationService.exportData(for: app.profile))
            isExporting = true
        } catch {
            showError(app.tr("导出失败", "Export failed"), error: error)
        }
    }

    private func handleImport(_ result: Result<URL, Error>) {
        guard !app.hasActiveTunnel, !app.isBusy else {
            notice = SettingsNotice(
                title: app.tr("当前不可导入", "Import unavailable"),
                message: app.tr("请先断开连接并等待当前操作结束。", "Disconnect and wait for the current operation to finish.")
            )
            return
        }

        do {
            let url = try result.get()
            let accessed = url.startAccessingSecurityScopedResource()
            defer {
                if accessed { url.stopAccessingSecurityScopedResource() }
            }

            if let size = try FileManager.default.attributesOfItem(atPath: url.path)[.size] as? NSNumber,
               size.intValue > MobileConfigurationService.maxImportBytes {
                throw MobileConfigurationError.fileTooLarge
            }

            let data = try Data(contentsOf: url, options: [.mappedIfSafe])
            let imported = try MobileConfigurationService.importProfile(from: data)
            let previousProfile = app.profile
            app.engine.lastError = nil
            guard app.replaceProfileAndSave(imported) else {
                app.profile = previousProfile
                notice = SettingsNotice(
                    title: app.tr("导入失败", "Import failed"),
                    message: app.tr(
                        "配置未能安全保存，原配置仍保留。请解锁设备后重试。",
                        "The configuration could not be saved securely. Your previous configuration is still available. Unlock the device and try again."
                    )
                )
                return
            }
            notice = SettingsNotice(
                title: app.tr("导入成功", "Import complete"),
                message: app.tr(
                    "配置已保存。请重新填写账号、密码、节点密钥、订阅地址和远程规则地址。",
                    "The configuration was saved. Re-enter accounts, passwords, node keys, subscription URLs, and remote-rule URLs."
                )
            )
        } catch {
            showError(app.tr("导入失败", "Import failed"), error: error)
        }
    }

    private func resetConfiguration() {
        guard !app.hasActiveTunnel, !app.isBusy else { return }
        let previousProfile = app.profile
        app.engine.lastError = nil
        guard app.replaceProfileAndSave(app.defaultProfile) else {
            app.profile = previousProfile
            notice = SettingsNotice(
                title: app.tr("重置失败", "Reset failed"),
                message: app.tr(
                    "配置未能安全保存，原配置仍保留。请解锁设备后重试。",
                    "The configuration could not be saved securely. Your previous configuration is still available. Unlock the device and try again."
                )
            )
            return
        }
        notice = SettingsNotice(
            title: app.tr("已重置", "Reset complete"),
            message: app.tr("配置已恢复为初始状态。", "The initial configuration has been restored.")
        )
    }

    private func removeSystemProfile() {
        app.removeTunnelProfile { result in
            Task { @MainActor in
                switch result {
                case .success:
                    notice = SettingsNotice(
                        title: app.tr("已移除", "Profile removed"),
                        message: app.tr("系统描述文件已移除。", "The system profile was removed.")
                    )
                case .failure(let error):
                    showError(app.tr("移除失败", "Removal failed"), error: error)
                }
            }
        }
    }

    private func showError(_ title: String, error: Error) {
        notice = SettingsNotice(title: title, message: configurationErrorMessage(error))
    }

    private func configurationErrorMessage(_ error: Error) -> String {
        guard let error = error as? MobileConfigurationError else {
            let productText = userFacingConnectionText(error.localizedDescription)
            return MobileDiagnosticsService.redacted(productText, using: app.profile)
                ?? app.tr("操作失败，请重试。", "The operation failed. Please try again.")
        }
        switch error {
        case .invalidJSON:
            return app.tr("所选文件不是有效的 XDial JSON 配置。", "The selected file is not a valid XDial JSON configuration.")
        case .unsupportedFormat:
            return app.tr("不支持这个 XDial 配置格式或版本。", "This XDial configuration format or version is not supported.")
        case .missingField(let field):
            return app.tr("配置缺少必要字段：\(field)。", "The configuration is missing the required field: \(field).")
        case .invalidIdentifier(let kind):
            return app.tr("配置中有空的 \(kind) 标识。", "The configuration contains an empty \(kind) identifier.")
        case .duplicateIdentifier(let kind):
            return app.tr("配置中有重复的 \(kind) 标识。", "The configuration contains a duplicate \(kind) identifier.")
        case .unsupportedLineType(let type):
            return app.tr("配置包含不支持的线路类型：\(type)。", "The configuration contains an unsupported line type: \(type).")
        case .invalidActiveScenario:
            return app.tr("当前场景在导入配置中不存在。", "The active scenario does not exist in this configuration.")
        case .invalidReference(let reference):
            return app.tr("配置包含无效引用：\(reference)。", "The configuration contains an invalid reference: \(reference).")
        case .fileTooLarge:
            return app.tr("配置文件超过 2 MB，已拒绝导入。", "The configuration is larger than 2 MB and was not imported.")
        }
    }
}

private struct MobileDiagnosticsDetailView: View {
    let title: String
    let shareTitle: String
    let report: String

    var body: some View {
        ScrollView {
            Text(report)
                .font(.system(.body, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ShareLink(item: report, subject: Text("XDial Diagnostics")) {
                Label(shareTitle, systemImage: "square.and.arrow.up")
            }
        }
    }
}

private struct SettingsNotice: Identifiable {
    let id = UUID()
    let title: String
    let message: String
}

private struct SettingsAccountTarget: Identifiable {
    let id: String
}
