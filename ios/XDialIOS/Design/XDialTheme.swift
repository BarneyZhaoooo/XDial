import SwiftUI
import UIKit

/// 手机端的颜色、圆角和通用样式。色值与 macOS 的 XDialBrandPalette 保持一致，
/// 让图标、桌面版和手机版是同一套石青色系。
enum XDialTheme {
    static let canvas = dynamic(light: 0xEFF2F3, dark: 0x15181A)
    static let surface = dynamic(light: 0xFFFFFF, dark: 0x23272A)
    static let surfaceMuted = dynamic(light: 0xF3F5F6, dark: 0x1D2123)
    static let hairline = dynamic(light: 0xD3DADD, dark: 0x363C40)

    static let ink = dynamic(light: 0x1C2327, dark: 0xEDF0F1)
    static let inkSecondary = dynamic(light: 0x5B676D, dark: 0x9BA6AB)
    static let inkTertiary = dynamic(light: 0x8D989D, dark: 0x6C777C)

    static let accent = dynamic(light: 0x3F5C69, dark: 0xA2B3BC)
    static let success = dynamic(light: 0x2F7D57, dark: 0x6FCB9B)
    static let warning = dynamic(light: 0x9A6A16, dark: 0xE0B265)
    static let danger = dynamic(light: 0xA6453A, dark: 0xE79584)

    /// 拨号盘：盘面与指孔。
    static let dialFace = dynamic(light: 0xFFFFFF, dark: 0x2B3033)
    static let dialInk = dynamic(light: 0x34464F, dark: 0xC9D3D8)

    static let cardRadius: CGFloat = 22
    static let controlRadius: CGFloat = 16

    private static func dynamic(light: UInt32, dark: UInt32) -> Color {
        Color(UIColor { traits in
            UIColor(hex: traits.userInterfaceStyle == .dark ? dark : light)
        })
    }
}

private extension UIColor {
    convenience init(hex: UInt32) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: 1
        )
    }
}

extension View {
    /// 表单和列表统一用主屏的底色，而不是系统默认的灰蓝色。
    func xdialGroupedBackground() -> some View {
        self
            .scrollContentBackground(.hidden)
            .background(XDialTheme.canvas.ignoresSafeArea())
    }

    /// 主屏和弹层共用的卡片底：白色圆角面，极轻的投影。
    func xdialCard(padding: CGFloat = 18) -> some View {
        self
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                XDialTheme.surface,
                in: RoundedRectangle(cornerRadius: XDialTheme.cardRadius, style: .continuous)
            )
            .shadow(color: .black.opacity(0.04), radius: 12, y: 4)
    }
}

/// 整宽主按钮。
struct XDialPrimaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    var tint: Color = XDialTheme.accent

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .foregroundStyle(XDialTheme.surface)
            .frame(maxWidth: .infinity, minHeight: 54)
            .xdialGlass(
                tint: tint.opacity(isEnabled ? 1 : 0.35),
                interactive: true,
                in: RoundedRectangle(cornerRadius: XDialTheme.controlRadius, style: .continuous),
                fallback: tint.opacity(isEnabled ? 1 : 0.35)
            )
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.snappy(duration: 0.18), value: configuration.isPressed)
    }
}

/// 卡片里的次要小按钮。
struct XDialPillButtonStyle: ButtonStyle {
    var tint: Color = XDialTheme.accent

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 14)
            .frame(minHeight: 36)
            .xdialGlass(
                tint: tint.opacity(0.12),
                interactive: true,
                in: Capsule(),
                fallback: tint.opacity(configuration.isPressed ? 0.2 : 0.12)
            )
    }
}

/// 小节标题：字小、字距略宽，像仪表上的刻字。
struct XDialSectionLabel: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(XDialTheme.inkSecondary)
            .textCase(.uppercase)
            .kerning(0.6)
            .padding(.horizontal, 6)
            .accessibilityAddTraits(.isHeader)
    }
}
