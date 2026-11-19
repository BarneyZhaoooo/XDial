import SwiftUI

/// 导航路由。用值驱动的 NavigationStack（`navigationDestination(for:)`）而不是
/// 一堆散落的 NavigationLink 目标，方便任意视图 push 且集中定义目的地。
enum Route: Hashable {
    case scenarios
    case credentials
    case settings
}

/// 根视图：唯一持有 AppState 的地方（`@StateObject`），用 NavigationStack 承载
/// tvOS 的层级导航，并把 AppState 通过 environmentObject 注入整棵子树。
///
/// 交互范式：遥控器方向键在各视图内移动焦点，中键触发按钮/NavigationLink，
/// Menu 键回退上一层（NavigationStack 默认行为）。
struct RootView: View {
    @StateObject private var app: AppState

    init() {
        _app = StateObject(wrappedValue: Self.makeState())
    }

    @MainActor
    private static func makeState() -> AppState {
        #if DEBUG && targetEnvironment(simulator)
        let engine = FakeTunnelEngine()
        return AppState(engine: engine, tunnelManager: FakeTunnelManager(engine: engine))
        #else
        let engine = GoEngine.shared
        return AppState(engine: engine, tunnelManager: TunnelManager(engine: engine))
        #endif
    }

    var body: some View {
        NavigationStack {
            ConnectionView()
                .navigationDestination(for: Route.self) { route in
                    switch route {
                    case .scenarios:
                        ScenarioPickerView()
                    case .credentials:
                        CredentialsView()
                    case .settings:
                        SettingsView()
                    }
                }
        }
        .fullScreenCover(isPresented: Binding(get: { !app.dataUseNoticeAccepted }, set: { _ in })) {
            TVDataUseNoticeView(mode: .firstUse).environmentObject(app)
        }
        .environmentObject(app)
        #if DEBUG && targetEnvironment(simulator)
        .onAppear { app.seedDemoDataForSimulator() }
        #endif
    }
}
