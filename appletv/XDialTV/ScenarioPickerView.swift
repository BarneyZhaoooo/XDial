import SwiftUI

/// 场景选择列表。展示 AppState.profile.scenarios，中键选中即设为 activeScenario。
///
/// tvOS 标准交互：List 每行是一个 Button（可聚焦），方向键上下移动焦点，
/// 中键触发选中。@FocusState 记录当前聚焦行，进入时自动落在当前 active 场景上。
struct ScenarioPickerView: View {
    @EnvironmentObject private var app: AppState
    @FocusState private var focusedScenarioID: String?

    var body: some View {
        List {
            ForEach(app.profile.scenarios) { scenario in
                Button { select(scenario) } label: { row(for: scenario) }
                    .focused($focusedScenarioID, equals: scenario.id)
                    .disabled(!app.canMutateConfiguration)
            }
            if !availableLines.isEmpty {
                Section(app.tr("创建场景", "Create Scenario")) {
                    ForEach(availableLines) { line in
                        Button {
                            createScenario(for: line)
                        } label: {
                            Label(lineName(line), systemImage: "plus")
                        }
                    }
                }
                .disabled(!app.canMutateConfiguration)
            }
        }
        .navigationTitle(app.tr("选择场景", "Select Scenario"))
        .onAppear {
            // 优先聚焦当前激活场景，否则第一条
            focusedScenarioID = app.activeScenario?.id ?? app.profile.scenarios.first?.id
        }
    }

    // A public installation starts with Direct and an unconfigured AnyConnect
    // line. Creating a scenario is explicit and never starts the tunnel.
    private var availableLines: [Line] {
        app.profile.lines.filter { line in
            line.enabled && (line.type == "direct" || line.type == "vpn") &&
                !app.profile.scenarios.contains { scenario in
                    scenario.defaultLineID == line.id && scenario.defaultSubscriptionID.isEmpty &&
                        scenario.bindings.allSatisfy { RuleSet.isConnectivityTestRuleID($0.ruleSetID) }
                }
        }
    }

    private func lineName(_ line: Line) -> String {
        line.type == "direct" ? app.tr("直连", "Direct") : line.name
    }

    private func createScenario(for line: Line) {
        guard app.canMutateConfiguration else { return }
        let scenario = Scenario(id: UUID().uuidString, name: lineName(line), defaultLineID: line.id)
        app.profile.scenarios.append(scenario)
        if app.activeScenario == nil { app.profile.activeScenarioID = scenario.id }
        if app.save() { focusedScenarioID = scenario.id }
    }

    private func row(for scenario: Scenario) -> some View {
        HStack(spacing: 20) {
            Image(systemName: isActive(scenario) ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(isActive(scenario) ? .green : .secondary)
            VStack(alignment: .leading, spacing: 4) {
                Text(scenario.name)
                    .font(.headline)
                Text(subtitle(for: scenario))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(.vertical, 8)
    }

    private func isActive(_ scenario: Scenario) -> Bool {
        app.profile.activeScenarioID == scenario.id
    }

    /// 场景的分流规则数量作为副标题，纯数据派生，不依赖连接结果。
    private func subtitle(for scenario: Scenario) -> String {
        let count = scenario.bindings.filter { !RuleSet.isConnectivityTestRuleID($0.ruleSetID) }.count
        return app.tr("\(count) 条分流绑定", "\(count) binding(s)")
    }

    private func select(_ scenario: Scenario) {
        app.profile.activeScenarioID = scenario.id
        app.save()
    }
}
