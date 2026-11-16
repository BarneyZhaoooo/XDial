# Android 开发检查点（未完成交付）

2026-11-17，工作分支 cindy/proud-kalam，基线 ba959c3。

## 已执行

- patched Go 核心以 with_gvisor,with_utls 实际 gomobile bind android/arm64 成功。
- JDK 17、AGP 9.2.1、Gradle **9.4.1**、SDK 36、NDK 27.1.12297006 构建 APK 成功。
  初始 wrapper 9.3.1 被 AGP 拒绝，已修正；不可照旧环境目录名判断实际 Gradle 版本。
- `go test -tags with_gvisor,with_utls ./core/config ./core/libbox` 通过。
  先用 `make sing-box-test-validator` 构建检查器并将 build/tools 放进 PATH。
- `make public-content-gate` 通过。
- 插件创建独立模拟器 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B`，Android 36 arm64。
- APK 安装、启动、Profile 导入、系统 VPN 授权已实测。
- 输入法首次启动进入 stylus 引导导致第一次粘贴没写入；改用 type 并读回字段后成功。
  首次连接错误为 FileNotFoundException（配置未保存），不是 TUN 编译失败。
- 再次导入后，服务建立 TUN，core.startStandalone 返回成功，UI 显示“已连接”。
- Chrome 打开 https://example.com，15 秒内未出现 Example Domain：**流量验收未通过**。
  待检查 TUN 计数、DNS/socket protection、Android 网络接口事实。
- 未使用生产 Profile、企业 VPN 凭据，也未安装/重启宿主 Debug，未改变宿主 VPN。

## 当前不能宣称

不能宣称端到端 VPN 可用、自动场景/登录完成、全平台完成或已发布。当前 App 是开发验收
检查点，不是交付版。未完成 Apple 全量构建/回归；Go 在 Darwin 上的核心测试不等于 Apple
App 验收。没有创建第二个 iOS uploader，没有修改既有自动化，没有推送/PR/合并。

## 下一步

1. 通过服务持有的 core.Diagnostics 观察 TUN 收发、socket保护调用计数；只展示计数与
   阶段错误，不记录配置、凭据或原始目标。修通隔离模拟器 DNS+TCP，再进行 UDP 验收。
2. 将完整 Android LinkProperties / NetworkInterface 事实投射到 SetNetworkInterfaces；
   逐个核实 sing-tun 的 Android fd 属性，不借用 Apple 的隐式假设。
3. 按 ../plans/cross-platform.md 继续业务状态机、登录、自动场景、恢复、原生 UI、分发。

构建日志位于 worktree 的 build/android（忽略目录）。APK：
`android/app/build/outputs/apk/debug/app-debug.apk`。尚未分发，不提交二进制到源码。
