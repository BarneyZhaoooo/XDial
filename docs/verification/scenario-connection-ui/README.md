# 场景运行状态验收

PR #58 仅在现有圆球布局上增加运行状态说明；原纵向列表及其离屏截图已移除，
圆球、悬停操作、失败与仅 SOCKS5 标识、缺项提示沿用 main。

## 已验证（2026-11-15）

Xcode 27.1 / macOS arm64，以下 9 项定向测试通过：

- ScenarioConnectionPresentationTests（7 项）：纯直连、已提交 VPN、未提交或失败事务、
  未启用的过期报告、切换期间旧场景、未知类型、订阅加直连与仅订阅、
  订阅未就绪、VPN 加订阅。订阅策略字符串不能冒充线路类型。
- ScenarioConnectionViewsTests（1 项）：状态说明组件中英文、浅深色及长名称渲染。
- ScenarioGridLayoutTests（1 项）：保留圆球均衡布局。

运行命令（先用 xcodegen generate --spec macos/project.yml 生成项目）：

```sh
xcodebuild -project macos/XDial.xcodeproj -scheme XDialTests \
  -configuration Debug -destination 'platform=macOS,arch=arm64' \
  -derivedDataPath /tmp/xdial-pr58-tests CODE_SIGNING_ALLOWED=NO \
  -only-testing:XDialTests/ScenarioConnectionPresentationTests \
  -only-testing:XDialTests/ScenarioConnectionViewsTests \
  -only-testing:XDialTests/ScenarioGridLayoutTests test
```

另通过 MainPopover.swift 语法检查及 git diff --check。

## 尚未验证

已使用本机官方 Go 1.26.5 尝试 make app，仅构建候选包，不安装或启动。
完整构建仍被现有 Makefile 的空格路径处理阻断：
默认构建目录会将绝对路径拆成多个 make target 与 Go 参数；
改用无空格 BUILD_DIR 时，补丁脚本仍输出到仓库内 build/patched-go，
与调用方期望的 GOWORK 路径不符。此外本机未安装 gomobile / gobind，
尚无本工作树的 macOS Libbox.xcframework。

未完成完整 App 构建、签名、安装、Debug 重启或真实直连、AnyConnect、
订阅与切换验收。未修改当前网络配置或凭据；这些运行验证仍需完整构建环境。
