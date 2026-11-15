import SwiftUI

/// 登录公司 VPN：用户名、密码两格，服务器已经预填好，一般不用碰。
struct SignInSheet: View {
    @EnvironmentObject private var app: AppState
    @Environment(\.dismiss) private var dismiss

    let lineID: String
    /// 保存成功后回调，主屏据此直接发起连接。
    var onSaved: () -> Void = {}

    @State private var server = ""
    @State private var username = ""
    @State private var password = ""
    @State private var isEditingServer = false
    @State private var isPasswordVisible = false
    @State private var errorMessage: String?
    @State private var didLoad = false
    @FocusState private var focus: Field?

    private enum Field { case server, username, password }

    private var line: Line? { app.profile.lines.first { $0.id == lineID } }
    private var isLocked: Bool { !app.canMutateConfiguration }
    private var lineName: String {
        let name = line?.name.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? app.tr("公司 VPN", "Company VPN") : name
    }
    private var canSubmit: Bool {
        !isLocked
            && !server.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !username.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !password.isEmpty
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    header

                    if isLocked {
                        Label(
                            app.tr("断开连接后才能修改账号。", "Disconnect before changing the account."),
                            systemImage: "lock.fill"
                        )
                        .font(.subheadline)
                        .foregroundStyle(XDialTheme.warning)
                    }

                    fields
                    serverRow

                    if let errorMessage {
                        Label(errorMessage, systemImage: "exclamationmark.circle.fill")
                            .font(.subheadline)
                            .foregroundStyle(XDialTheme.danger)
                            .accessibilityIdentifier("sign-in-error")
                    }

                    Label(
                        app.tr(
                            "账号和密码只保存在这台手机的钥匙串里。",
                            "Your account stays in this iPhone's Keychain."
                        ),
                        systemImage: "lock.shield"
                    )
                    .font(.footnote)
                    .foregroundStyle(XDialTheme.inkSecondary)
                }
                .padding(.horizontal, 22)
                .padding(.top, 8)
                .padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(XDialTheme.canvas.ignoresSafeArea())
            .safeAreaInset(edge: .bottom) {
                Button(action: submit) {
                    Text(app.tr("登录并连接", "Sign In and Connect"))
                }
                .buttonStyle(XDialPrimaryButtonStyle())
                .disabled(!canSubmit)
                .padding(.horizontal, 22)
                .padding(.vertical, 12)
                .background(.bar)
                .accessibilityIdentifier("sign-in-submit")
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(app.tr("取消", "Cancel")) { close() }
                }
            }
            .navigationBarTitleDisplayMode(.inline)
        }
        .tint(XDialTheme.accent)
        .onAppear(perform: load)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(app.tr("登录\(lineName)", "Sign in to \(lineName)"))
                .font(.largeTitle.bold())
                .foregroundStyle(XDialTheme.ink)
            Text(app.tr(
                "用你的公司账号登录，只需要填这一次。",
                "Use your company account. You only need to do this once."
            ))
            .font(.body)
            .foregroundStyle(XDialTheme.inkSecondary)
        }
    }

    private var fields: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "person")
                    .frame(width: 22)
                    .foregroundStyle(XDialTheme.inkSecondary)
                TextField(app.tr("用户名", "Username"), text: $username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.asciiCapable)
                    .submitLabel(.next)
                    .focused($focus, equals: .username)
                    .onSubmit { focus = .password }
                    .accessibilityIdentifier("sign-in-username")
            }
            .frame(minHeight: 56)

            Divider().overlay(XDialTheme.hairline)

            HStack(spacing: 12) {
                Image(systemName: "key")
                    .frame(width: 22)
                    .foregroundStyle(XDialTheme.inkSecondary)
                Group {
                    if isPasswordVisible {
                        TextField(app.tr("密码", "Password"), text: $password)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    } else {
                        SecureField(app.tr("密码", "Password"), text: $password)
                    }
                }
                .submitLabel(.go)
                .focused($focus, equals: .password)
                .onSubmit { if canSubmit { submit() } }
                .accessibilityIdentifier("sign-in-password")

                Button {
                    isPasswordVisible.toggle()
                } label: {
                    Image(systemName: isPasswordVisible ? "eye.slash" : "eye")
                        .foregroundStyle(XDialTheme.inkSecondary)
                        .frame(width: 44, height: 44)
                }
                .accessibilityLabel(
                    isPasswordVisible
                        ? app.tr("隐藏密码", "Hide password")
                        : app.tr("显示密码", "Show password")
                )
            }
            .frame(minHeight: 56)
        }
        .padding(.horizontal, 16)
        .background(
            XDialTheme.surface,
            in: RoundedRectangle(cornerRadius: XDialTheme.controlRadius, style: .continuous)
        )
        .disabled(isLocked)
    }

    @ViewBuilder
    private var serverRow: some View {
        if isEditingServer {
            HStack(spacing: 12) {
                Image(systemName: "server.rack")
                    .frame(width: 22)
                    .foregroundStyle(XDialTheme.inkSecondary)
                TextField(app.tr("服务器地址:端口", "Server address:port"), text: $server)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    .focused($focus, equals: .server)
                    .accessibilityIdentifier("sign-in-server")
            }
            .frame(minHeight: 56)
            .padding(.horizontal, 16)
            .background(
                XDialTheme.surface,
                in: RoundedRectangle(cornerRadius: XDialTheme.controlRadius, style: .continuous)
            )
            .disabled(isLocked)
        } else {
            HStack(spacing: 6) {
                Text(app.tr("服务器", "Server"))
                    .foregroundStyle(XDialTheme.inkSecondary)
                Text(server)
                    .foregroundStyle(XDialTheme.ink)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 8)
                Button(app.tr("修改", "Edit")) {
                    isEditingServer = true
                    focus = .server
                }
                .disabled(isLocked)
                .accessibilityIdentifier("sign-in-edit-server")
            }
            .font(.subheadline)
            .padding(.horizontal, 6)
        }
    }

    private func load() {
        guard !didLoad, let line else { return }
        didLoad = true
        server = line.vpnServer
        username = line.vpnUsername
        password = line.vpnPassword
        isEditingServer = line.vpnServer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        guard !isLocked else { return }
        if isEditingServer {
            focus = .server
        } else if username.isEmpty {
            focus = .username
        } else if password.isEmpty {
            focus = .password
        }
    }

    private func submit() {
        guard canSubmit else { return }
        guard app.saveAccount(lineID: lineID, server: server, username: username, password: password) else {
            errorMessage = app.tr(
                "没能保存。请解锁手机后再试一次。",
                "Couldn't save. Unlock the iPhone and try again."
            )
            return
        }
        app.engine.lastError = nil
        onSaved()
        close()
    }

    /// 关闭前清空密码框，系统就不会再弹「要保存这个密码吗」。
    private func close() {
        focus = nil
        password = ""
        isPasswordVisible = false
        dismiss()
    }
}
