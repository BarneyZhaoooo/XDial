import SwiftUI

/// These views have no AppState or network side effects, including in previews.
struct ScenarioRuntimeSummaryView: View {
    let title: String
    let detail: String
    let runningScenario: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title).font(.system(size: 14, weight: .semibold))
            Text(detail)
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let runningScenario {
                Text(runningScenario).font(.caption).foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

struct ScenarioRoutingRowView: View {
    let name: String
    let icon: String
    let routing: String
    let activeLabel: String?
    let preparing: Bool
    let configurationIssue: String?
    let actionTitle: String
    let action: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Image(systemName: icon).foregroundStyle(.secondary).frame(width: 20)
                Text(name)
                    .font(.system(size: 13, weight: .medium))
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 4)
                if let activeLabel {
                    Text(activeLabel).font(.caption).foregroundStyle(.secondary)
                } else if preparing {
                    ProgressView().controlSize(.small)
                }
                Button(actionTitle, action: action)
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .fixedSize()
                    .accessibilityLabel(name + ": " + actionTitle)
            }
            Text(routing)
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let configurationIssue {
                Text(configurationIssue)
                    .font(.caption)
                    .foregroundStyle(XDialPalette.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
