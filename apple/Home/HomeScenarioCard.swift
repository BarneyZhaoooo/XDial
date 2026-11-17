import SwiftUI

struct HomeScenarioCard: View {
    let id: String
    let name: String
    let summary: String
    let symbol: String
    let isActive: Bool
    let requiresConfirmation: Bool
    var compact = false
    let tr: (String, String) -> String
    var onManualSelection: () -> Void = {}
    let select: () -> Void
    @State private var confirming = false

    var body: some View {
        Button {
            guard !isActive else { return }
            onManualSelection()
            if requiresConfirmation { confirming = true } else { select() }
        } label: {
            HStack(spacing: 14) {
                Image(systemName: symbol)
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(isActive ? XDialTheme.surface : XDialTheme.accent)
                    .frame(width: compact ? 32 : 44, height: compact ? 32 : 44)
                    .background(
                        isActive ? XDialTheme.accent : XDialTheme.accent.opacity(0.12),
                        in: RoundedRectangle(cornerRadius: 13, style: .continuous)
                    )

                VStack(alignment: .leading, spacing: 3) {
                    Text(name)
                        .font(.headline)
                        .foregroundStyle(XDialTheme.ink)
                    Text(summary)
                        .font(.subheadline)
                        .foregroundStyle(XDialTheme.inkSecondary)
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: 8)

                Image(systemName: isActive ? "checkmark.circle.fill" : "circle")
                    .font(.title3)
                    .foregroundStyle(isActive ? XDialTheme.accent : XDialTheme.hairline)
            }
            .padding(compact ? 9 : 16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                XDialTheme.surface,
                in: RoundedRectangle(cornerRadius: XDialTheme.cardRadius, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: XDialTheme.cardRadius, style: .continuous)
                    .strokeBorder(XDialTheme.accent.opacity(isActive ? 1 : 0), lineWidth: 1.5)
            }
            .shadow(color: .black.opacity(0.04), radius: 12, y: 4)
            .contentShape(RoundedRectangle(cornerRadius: XDialTheme.cardRadius, style: .continuous))
        }
        .buttonStyle(.plain)

        .animation(.smooth(duration: 0.25), value: isActive)
        .sensoryFeedback(.selection, trigger: isActive)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isActive ? .isSelected : [])
        .accessibilityIdentifier("home-scenario-\(id)")
        .alert(tr("切换到「\(name)」？", "Switch to “\(name)”?"), isPresented: $confirming) {
            Button(tr("切换", "Switch"), action: select)
            Button(tr("取消", "Cancel"), role: .cancel) {}
        } message: {
            Text(tr(
                "新场景准备好后接手，正在进行的下载或通话可能会短暂中断。",
                "The new routing takes over once ready. Downloads or calls in progress may be interrupted briefly."
            ))
        }
    }
}
