import SwiftUI

struct HomeAccountRequest: Codable, Hashable {
    let lineID: String
    var connectScenarioID: String?
}

struct HomeAccountView: View {
    @EnvironmentObject private var state: AppState
    @Environment(\.dismiss) private var dismiss
    let request: HomeAccountRequest
    @State private var server = ""
    @State private var username = ""
    @State private var password = ""
    @State private var saveError: String?

    private var line: Line? { state.profile.lines.first { $0.id == request.lineID } }
    private var owner: ProfileRecord? { state.profileLibrary.owner(ofLine: request.lineID) }
    private var sourceOwned: Bool { owner?.baseline?.lines.contains { $0.id == request.lineID } == true }
    private var editable: Bool {
        line != nil && !state.isBusy && !state.isConnected && (!sourceOwned || owner?.lineCredentialsBelongToUser == true)
    }

    var body: some View {
        Form {
            if let line {
                Text(line.name).font(.headline)
                Group {
                if sourceOwned && !line.vpnServer.isEmpty {
                    LabeledContent(state.tr("服务器", "Server"), value: line.vpnServer)
                } else {
                    TextField(state.tr("服务器", "Server"), text: $server)
                }
                TextField(state.tr("用户名", "Username"), text: $username)
                    .textContentType(.username).autocorrectionDisabled()
                LabeledContent(state.tr("密码", "Password")) {
                    CredentialSecureField(placeholder: state.tr("密码", "Password"), text: $password)
                }
                }
                .disabled(!editable)
            }
            if sourceOwned && owner?.lineCredentialsBelongToUser == false {
                Text(state.tr("此账号由订阅提供。", "This account is managed by the subscription."))
            } else if state.isConnected || state.isBusy {
                Text(state.tr("断开连接后可修改账号。", "Disconnect to edit the account."))
            }
            if let saveError { Text(saveError).foregroundStyle(.red) }
            HStack {
                Spacer()
                Button(state.tr("取消", "Cancel")) { dismiss() }.keyboardShortcut(.cancelAction)
                Button(request.connectScenarioID == nil ? state.tr("保存", "Save") : state.tr("保存并连接", "Save and Connect")) {
                    submit()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(!editable || server.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                          username.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || password.isEmpty)
            }
        }
        .formStyle(.grouped)
        .frame(width: 380)
        .onAppear {
            server = line?.vpnServer ?? ""
            username = line?.vpnUsername ?? ""
            password = line?.vpnPassword ?? ""
        }
        .onDisappear { password = "" }
    }

    private func submit() {
        guard editable, let index = state.profile.lines.firstIndex(where: { $0.id == request.lineID }) else { return }
        let previousLibrary = state.profileLibrary
        if !sourceOwned || state.profile.lines[index].vpnServer.isEmpty {
            state.profile.lines[index].vpnServer = server.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        state.profile.lines[index].vpnUsername = username
        state.profile.lines[index].vpnPassword = password
        state.profile.lines[index].verified = false
        state.save()
        if let error = state.profilePersistenceError {
            state.profileLibrary = previousLibrary
            state.profile = previousLibrary.snapshot()
            saveError = error
            return
        }
        dismiss()
        if let id = request.connectScenarioID { state.connectScenario(id) }
    }
}
