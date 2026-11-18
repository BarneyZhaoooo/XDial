import SwiftUI

extension View {
    /// Navigation and actions share Apple's adaptive material. Content and field labels
    /// keep opaque surfaces when Reduce Transparency is enabled or glass is unavailable.
    func xdialGlass<S: Shape>(tint: Color? = nil, interactive: Bool = false,
                              in shape: S, fallback: Color) -> some View {
        modifier(HomeGlass(tint: tint, interactive: interactive, shape: shape, fallback: fallback))
    }

    @ViewBuilder func xdialPrimaryAction() -> some View {
        #if compiler(>=6.2)
        if #available(macOS 26, *) {
            self.buttonStyle(.glassProminent)
        } else {
            self.buttonStyle(.borderedProminent)
        }
        #else
        self.buttonStyle(.borderedProminent)
        #endif
    }
}

private struct HomeGlass<S: Shape>: ViewModifier {
    let tint: Color?
    let interactive: Bool
    let shape: S
    let fallback: Color
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    @ViewBuilder func body(content: Content) -> some View {
        #if compiler(>=6.2)
        if #available(macOS 26, *), !reduceTransparency {
            content.glassEffect(.regular.tint(tint).interactive(interactive), in: shape)
        } else {
            content.background(fallback, in: shape)
        }
        #else
        content.background(fallback, in: shape)
        #endif
    }
}

/// The dark surface keeps the glass dial legible in both appearances.
struct DialInstrumentSurface: View {
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        let graphite = Color(red: 0.045, green: 0.105, blue: 0.15)
        let teal = Color(red: 0.12, green: 0.36, blue: 0.39)
        ZStack {
            LinearGradient(colors: [teal, graphite], startPoint: .topLeading, endPoint: .bottomTrailing)
            RadialGradient(colors: [.white.opacity(scheme == .dark ? 0.09 : 0.15), .clear],
                           center: .top, startRadius: 0, endRadius: 200)
        }
        .accessibilityHidden(true)
        .allowsHitTesting(false)
    }
}

/// The current selection is readable without expanding the route editor.
struct HomeRouteLabel: View {
    let name: String
    let reason: String
    let symbol: String
    let expanded: Bool

    var body: some View {
        HStack(spacing: 12) {
            HomeSymbol(symbol: symbol)
            VStack(alignment: .leading, spacing: 4) {
                Text(name).font(.headline)
                Text(reason).font(.subheadline).foregroundStyle(XDialTheme.inkSecondary)
                    .accessibilityIdentifier("home-auto-note")
            }
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 4)
            Image(systemName: expanded ? "chevron.up" : "chevron.down")
                .font(.caption.weight(.semibold)).foregroundStyle(XDialTheme.inkSecondary)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .xdialGlass(interactive: true, in: RoundedRectangle(cornerRadius: 18), fallback: XDialTheme.surface)
        .contentShape(RoundedRectangle(cornerRadius: 18))
    }
}

struct HomeSymbol: View {
    let symbol: String

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: 17, weight: .semibold))
            .symbolRenderingMode(.hierarchical)
            .foregroundStyle(XDialTheme.accent)
            .frame(width: 34, height: 34)
            .background(XDialTheme.accent.opacity(0.11), in: Circle())
            .accessibilityHidden(true)
    }
}

struct HomeAccountLabel: View {
    let name: String
    let detail: String
    var body: some View {
        HStack(spacing: 12) {
            HomeSymbol(symbol: "person.crop.circle")
            VStack(alignment: .leading, spacing: 3) {
                Text(name).font(.headline)
                Text(detail).font(.subheadline).foregroundStyle(XDialTheme.inkSecondary)
            }
            .multilineTextAlignment(.leading)
            Spacer(minLength: 6)
            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(XDialTheme.inkSecondary)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .xdialGlass(interactive: true, in: RoundedRectangle(cornerRadius: 18), fallback: XDialTheme.surface)
        .contentShape(RoundedRectangle(cornerRadius: 18))
    }
}

struct HomeAccountHeading: View {
    let title: String
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Image(systemName: "person.badge.key.fill")
                .font(.title2.weight(.medium)).foregroundStyle(.white)
                .frame(width: 49, height: 49)
                .background { DialInstrumentSurface().clipShape(RoundedRectangle(cornerRadius: 15)) }
                .accessibilityHidden(true)
            Text(title).font(.system(.title, design: .rounded, weight: .bold))
                .accessibilityAddTraits(.isHeader)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
