import SwiftUI
import UserNotifications

@main
struct XDialIOSApp: App {
    init() {
        UNUserNotificationCenter.current().delegate = ConnectionNoticePresenter.shared
        // 快捷指令和控制中心会在无窗口时后台启动 App，宿主必须在 App 初始化时注册。
        ConnectionNotice.preferences.set(MobileAppRuntime.state.language.rawValue, forKey: "xdial.language")
        LocationScenarios.shared.attach(MobileAppRuntime.state)
        XDialIntentHost.current = MobileIntentHost(app: MobileAppRuntime.state)
        MobileAppRuntime.connectionActivity = ConnectionActivityController(app: MobileAppRuntime.state)
    }

    var body: some Scene {
        WindowGroup {
            MobileRootView()
        }
    }
}

struct MobileRootView: View {
    @StateObject private var app: AppState
    @Environment(\.scenePhase) private var scenePhase

    init() {
        _app = StateObject(wrappedValue: MobileAppRuntime.state)
    }

    var body: some View {
        HomeView()
            .environmentObject(app)
            .onChange(of: scenePhase) { _, phase in
                switch phase {
                case .background: MobileAppRuntime.tailscaleSetupBackgroundGuard.enteredBackground()
                case .active:
                    LocationScenarios.shared.reconcile()
                    MobileAppRuntime.tailscaleSetupBackgroundGuard.becameActive()
                case .inactive: MobileAppRuntime.tailscaleSetupBackgroundGuard.becameInactive()
                @unknown default: break
                }
            }
            .fullScreenCover(isPresented: Binding(
                get: { !app.dataUseNoticeAccepted },
                set: { _ in }
            )) {
                DataUseNoticeView(mode: .firstUse).environmentObject(app)
            }
            #if targetEnvironment(simulator)
            .onAppear {
                // -XDialRealFlow 跳过演示数据，按真机首次安装的流程验证。
                let arguments = ProcessInfo.processInfo.arguments
                if !arguments.contains("-XDialRealFlow") {
                    app.seedDemoDataForSimulator()
                }
                // -XDialPreviewError <原始报错>：在模拟器里直接看某种报错在主屏上的样子。
                if let index = arguments.firstIndex(of: "-XDialPreviewError"),
                   arguments.indices.contains(index + 1) {
                    app.engine.lastError = arguments[index + 1]
                }
            }
            #endif
    }
}
