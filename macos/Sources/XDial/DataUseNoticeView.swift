import SwiftUI

extension Notification.Name {
    static let xdialOpenDataUseNotice = Notification.Name("com.xd.xdial.open-data-use-notice")
}

/// The declaration App Review Guideline 5.4 asks a VPN app to show before the
/// service is used. The App Store build opens it at launch until it is confirmed
/// and reopens it when a connection is attempted first; Settings › General can
/// show it again. The content lives in `MacDataUseNotice`.
struct DataUseNoticeView: View {
    static let windowID = "data-use"

    @Environment(\.dismissWindow) private var dismissWindow
    @EnvironmentObject private var state: AppState

    /// Set by the build (Info.plist XDialPrivacyPolicyURL); the same address as App Store Connect.
    static var privacyPolicyURL: URL? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: "XDialPrivacyPolicyURL") as? String,
              let url = URL(string: raw.trimmingCharacters(in: .whitespacesAndNewlines)),
              url.scheme?.lowercased() == "https" else { return nil }
        return url
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    Text(state.tr("数据使用说明", "How XDial Uses Data"))
                        .font(.system(size: 20, weight: .semibold))
                    Text(MacDataUseNotice.introduction(isChinese: isChinese))
                        .fixedSize(horizontal: false, vertical: true)
                    ForEach(sections, id: \.title) { section in
                        VStack(alignment: .leading, spacing: 6) {
                            Label(section.title, systemImage: section.symbol)
                                .font(.headline)
                            ForEach(section.points, id: \.self) { point in
                                HStack(alignment: .firstTextBaseline, spacing: 6) {
                                    Text("•").foregroundStyle(.secondary)
                                    Text(point)
                                        .foregroundStyle(.secondary)
                                        .fixedSize(horizontal: false, vertical: true)
                                }
                            }
                        }
                    }
                    if let url = Self.privacyPolicyURL {
                        Link(state.tr("阅读完整隐私政策", "Read the full privacy policy"), destination: url)
                            .accessibilityIdentifier("data-use-privacy-policy")
                    }
                }
                .padding(22)
            }
            Divider()
            HStack {
                if !state.cloudSync.noticeAccepted {
                    CloudSyncNoticeToggle(sync: state.cloudSync, chinese: isChinese)
                }
                Spacer()
                if state.dataUseNoticeAccepted && state.cloudSync.noticeAccepted {
                    Button(state.tr("完成", "Done")) { dismissWindow(id: Self.windowID) }
                        .keyboardShortcut(.defaultAction)
                } else {
                    Button(state.tr("稍后", "Not Now")) { dismissWindow(id: Self.windowID) }
                    Button(state.tr("继续使用", "Continue")) {
                        state.acceptDataUseNotice()
                        dismissWindow(id: Self.windowID)
                    }
                    .keyboardShortcut(.defaultAction)
                    .accessibilityIdentifier("data-use-accept")
                }
            }
            .padding(16)
        }
        .frame(width: 520, height: 600)
        .background(XDialPalette.canvas)
        .onDisappear { state.postponeDataUseNotice() }
    }

    private var isChinese: Bool { state.tr("zh", "en") == "zh" }

    private var sections: [MacDataUseNotice.Section] {
        MacDataUseNotice.sections(isChinese: isChinese, companyPreset: !state.regionScenarioTargets.isEmpty)
    }
}
