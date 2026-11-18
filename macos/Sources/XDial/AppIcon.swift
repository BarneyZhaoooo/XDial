import AppKit

/// App、Dock、iOS 与 tvOS 图标。盘面与首页共用 DialLayout，底块使用品牌色。
enum AppIcon {
    /// macOS 图标网格：1024 画布上 824 的圆角方块，圆角约 22.5%。
    static let macTileInset: CGFloat = 100 / 1024
    static let macTileCornerRatio: CGFloat = 185.4 / 824
    /// 拨号盘 16 网格占底块边长的比例：实心盘直径约为底块的 71%。
    static let dialGridRatio: CGFloat = 0.86

    static func base(size: CGFloat) -> NSImage {
        primary(size: size, connected: true)
    }

    /// Finder 里 XDial.app 的主图标：圆角底块 + 拨号盘，不表达“设置”。
    static func primary(
        size: CGFloat,
        connected: Bool
    ) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size))
        image.lockFocus()
        NSGraphicsContext.current?.shouldAntialias = true
        let tile = macTileRect(size: size)
        drawTile(in: tile, cornerRadius: tile.width * macTileCornerRatio)
        drawDial(in: dialRect(in: tile), connected: connected)
        image.unlockFocus()
        image.isTemplate = false
        return image
    }

    /// 设置窗口打开时 XDial 才临时出现在 Dock，这个运行时图标在同一拨号盘
    /// 右下角叠加齿轮。拨号盘略缩小并上移让位；齿轮徽标连同阴影都必须留在
    /// 圆角底块内——macOS 26 会把内容超出标准圆角方块的图标整张缩进灰色底板，
    /// 载体退出时的 Dock 动画回退到 icns 就会显示成那副样子。
    static func dock(size: CGFloat, connected: Bool = false) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size))
        image.lockFocus()
        NSGraphicsContext.current?.shouldAntialias = true
        let tile = macTileRect(size: size)
        drawTile(in: tile, cornerRadius: tile.width * macTileCornerRatio)
        let side = tile.width * 0.78
        drawDial(
            in: NSRect(
                x: tile.midX - side / 2 - tile.width * 0.03,
                y: tile.midY - side / 2 + tile.height * 0.04,
                width: side,
                height: side
            ),
            connected: connected
        )
        drawGearBadge(size: size)
        image.unlockFocus()
        image.isTemplate = false
        return image
    }

    // 菜单栏状态图标见 `MenuBarStatusIcon`。

    @MainActor
    static func applyDockState(connected: Bool) {
        NSApp.applicationIconImage = dock(size: 512, connected: connected)
    }

    // MARK: - 组成部件（供 macOS 图标与移动端生成器复用）

    static func macTileRect(size: CGFloat) -> NSRect {
        let inset = size * macTileInset
        return NSRect(x: inset, y: inset, width: size - inset * 2, height: size - inset * 2)
    }

    /// 拨号盘 16 网格在底块内居中占 `dialGridRatio`。
    static func dialRect(in tile: NSRect) -> NSRect {
        let side = min(tile.width, tile.height) * dialGridRatio
        return NSRect(
            x: tile.midX - side / 2,
            y: tile.midY - side / 2,
            width: side,
            height: side
        )
    }

    /// 底块：石板蓝灰纵向微渐变（Accent → Selection），`cornerRadius = 0` 时为
    /// 满幅方块（iOS / tvOS 背景层由系统裁切）。
    static func drawTile(in rect: NSRect, cornerRadius: CGFloat) {
        let tile = NSBezierPath(
            roundedRect: rect,
            xRadius: cornerRadius,
            yRadius: cornerRadius
        )
        NSGradient(
            starting: XDialBrandPalette.color(XDialBrandPalette.accentLightHex),
            ending: XDialBrandPalette.color(XDialBrandPalette.selectionLightHex)
        )?.draw(in: tile, angle: -90)
    }

    /// 只取首页拨盘的静态轮廓，按同一份布局绘制盘面、指孔、指停器与素面圆盖。
    static func drawDial(in rect: NSRect, connected: Bool) {
        let layout = DialLayout.standard
        let scale = min(rect.width, rect.height) / RotaryDial.grid
        let center = NSPoint(x: rect.midX, y: rect.midY)
        let disc = RotaryDial.circle(at: center, radius: DialLayout.discRadius * scale)

        func point(angle: Double, radius: CGFloat) -> NSPoint {
            let radians = angle * .pi / 180
            return NSPoint(x: center.x + cos(radians) * radius * scale,
                           y: center.y - sin(radians) * radius * scale)
        }

        RotaryDial.withGroupOpacity(connected ? 1 : RotaryDial.idleOpacity) {
            if connected {
                XDialBrandPalette.surface.setFill()
                disc.fill()
            } else {
                XDialBrandPalette.surface.setStroke()
                disc.lineWidth = scale
                disc.stroke()
            }
            let ink = connected ? XDialBrandPalette.selection : XDialBrandPalette.surface
            ink.setFill()
            for angle in layout.holeAngles {
                RotaryDial.circle(at: point(angle: angle, radius: layout.holeOrbit),
                                  radius: layout.holeRadius * scale).fill()
            }
            let stop = NSBezierPath()
            stop.move(to: point(angle: layout.stopAngle, radius: layout.stopInnerEdge + layout.stopWidth / 2))
            stop.line(to: point(angle: layout.stopAngle, radius: layout.stopOuterEdge - layout.stopWidth / 2))
            stop.lineWidth = layout.stopWidth * scale
            stop.lineCapStyle = .round
            ink.setStroke()
            stop.stroke()

            let hub = RotaryDial.circle(at: center, radius: layout.hubRadius * scale)
            XDialBrandPalette.divider.withAlphaComponent(0.65).setFill()
            hub.fill()
            let lip = layout.hubRadius * scale * 0.024
            let cap = NSBezierPath(ovalIn: hub.bounds.insetBy(dx: lip, dy: lip))
            NSGradient(starting: XDialBrandPalette.iconHub, ending: XDialBrandPalette.surface)?
                .draw(in: cap, angle: 90)
        }
    }

    private static func drawGearBadge(size: CGFloat) {
        // 底块占 [0.098, 0.902]；徽标 [0.60, 0.86] × [0.14, 0.40] 连阴影都在其内。
        let badgeRect = NSRect(
            x: size * 0.60,
            y: size * 0.14,
            width: size * 0.26,
            height: size * 0.26
        )
        let badge = NSBezierPath(ovalIn: badgeRect)

        NSGraphicsContext.saveGraphicsState()
        let shadow = NSShadow()
        shadow.shadowColor = NSColor.black.withAlphaComponent(0.34)
        shadow.shadowBlurRadius = size * 0.022
        shadow.shadowOffset = NSSize(width: 0, height: -size * 0.012)
        shadow.set()
        XDialBrandPalette.surface.withAlphaComponent(0.98).setFill()
        badge.fill()
        NSGraphicsContext.restoreGraphicsState()

        XDialBrandPalette.divider.setStroke()
        badge.lineWidth = max(1, size * 0.012)
        badge.stroke()

        guard let symbol = NSImage(
            systemSymbolName: "gearshape.fill",
            accessibilityDescription: nil
        ) else {
            return
        }
        let pointConfiguration = NSImage.SymbolConfiguration(
            pointSize: size * 0.15,
            weight: .semibold
        )
        let paletteConfiguration = NSImage.SymbolConfiguration(
            paletteColors: [XDialBrandPalette.accent]
        )
        guard let configured = symbol.withSymbolConfiguration(
            pointConfiguration.applying(paletteConfiguration)
        ) else {
            return
        }
        let gearSide = size * 0.165
        configured.draw(
            in: NSRect(
                x: badgeRect.midX - gearSide / 2,
                y: badgeRect.midY - gearSide / 2,
                width: gearSide,
                height: gearSide
            ),
            from: .zero,
            operation: .sourceOver,
            fraction: 1,
            respectFlipped: true,
            hints: [.interpolation: NSImageInterpolation.high]
        )
    }
}
