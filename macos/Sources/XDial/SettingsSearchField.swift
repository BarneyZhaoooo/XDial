import SwiftUI

/// Search fields keep the same border while focused; the insertion caret shows
/// where typing will go without adding a second outline around the control.
struct SettingsSearchField: View {
    let prompt: String
    @Binding var text: String

    init(_ prompt: String, text: Binding<String>) {
        self.prompt = prompt
        _text = text
    }

    var body: some View {
        HStack(spacing: 7) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary).accessibilityHidden(true)
            TextField(prompt, text: $text)
                .textFieldStyle(.plain)
        }
        .font(.body)
        .padding(.horizontal, 10)
        .frame(height: 32)
        .background(XDialPalette.surface, in: RoundedRectangle(cornerRadius: 9))
    }
}
