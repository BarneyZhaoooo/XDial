# 全平台客户端交付计划

2026-11-17；基线 origin/main ba959c3。首个交付是 Android 可安装、真实系统
VPN 可连接。AAR、APK 构建成功均不代表全平台完成。

## 能力矩阵（当前实际状态）

| 能力 | macOS | iOS | tvOS | Android | Windows | Linux |
|---|---|---|---|---|---|---|
| Go 场景/规则编译与协议 | 已有 | 已有 | 已有 | AAR 已构建 arm64 | CLI 部分；libbox 排除 Windows | CLI 部分 |
| 系统接入 | Transparent Proxy | Packet Tunnel | Packet Tunnel | VpnService 模拟器 direct DNS/HTTPS 通过 | 原生客户端/安装事务缺失 | 原生客户端/权限事务缺失 |
| 原生产品 UI | 已有 | 大拨盘/触感 | 遥控器 | 原生拨盘/拖动/无障碍；产品配置页待做 | 缺失 | 缺失 |
| 自动场景 | 已有 | Swift 自动驾驶 | 需逐项核对 | 共享 Go 选择器；连接前 SSID 已接入 | 缺失 | 缺失 |
| 安全配置 | 共享 Profile/Keychain | Keychain | Keychain | Keystore AES-GCM + AtomicFile，待测试 | 缺失 | 缺失 |
| 发布 | 已有 | 既有事件驱动 TestFlight | 已有构建 | 本地 debug；签名/分发待核实 | 缺失 | 缺失 |

## 边界决策

- 保留稳定 Swift 界面；Android 原生界面。业务决策逐步提取到 Go，禁止复制
  ScenarioAutoPilot.swift 到多个平台。协议、规则和场景裁决继续由同一核心处理。
- 平台只提供 TUN fd、socket protection、系统网络/DNS事实、权限、生命周期、
  凭据存储和触感。sing-box 独占逐流 DNS/路由/转发。
- SocketProtector 同步借用 fd，不关闭 fd；Android 必须先 protect 再绑定捕获的
  Network。保护失败即拨号失败；候选 Box 和复用 Line 继承回调。Apple 保持原行为。
- Android 每个 user/profile 只能有一个 VpnService，不能承诺与另一个 VPN 叠加。
  系统确认可能替换已有 VPN，验收只使用无现存 VPN 的隔离设备。
- AnyConnect 已接入受保护的 TLS/CSTP/DTLS 与共享启动入口，待授权隔离企业联调。
  Tailscale 的端点级保护契约已补齐，仍须完成 Android 专用登录/节点选择后解除产品限制；
  不以普通代理或本地契约测试冒充企业 VPN 已通过。
- 开发预览支持导入 runtime Profile、手动场景、连接前 SSID 匹配、撤权与清理；
  网络丢失保留连接意图/TUN，恢复后重建唯一核心。地区判断、连接中场景事务、
  登录和离线规则产品流程仍缺失，不得将此检查点称为 Android 完成交付。

## 阶段与验收门槛

1. **Android 基础接入（进行中）**：固定 gomobile/NDK/JDK/Gradle，实际 AAR/APK；
   VpnService TUN → sing-box → 测试出口；测试保护失败、撤权、重复连接、后台通知。
2. **共享移动端控制面**：将自动场景、手动覆盖、网络 epoch、连接意图与错误恢复
   提取为 Go 可绑定 API；先用现有 iOS 自动场景测试作为行为基线，Apple 渐进接入。
   场景切换必须保留旧连接承载直到候选提交，不能用 stop/start 假装事务。
3. **Android 产品闭环**：登录/凭据引用、来源配置库、离线规则注入、自动场景与手动
   覆盖、网络切换/恢复；正式大拨盘、触感、无障碍、深浅色。审计所有协议套接字。
4. **验收**：核心/适配契约测试，Android emulator 安装与交互；隔离真实 VPN
   流量证据；macOS/iOS/tvOS 构建及既有测试。准确区分 mock、模拟器、真机。
5. **同步测试分发**：复用 GitHub App push；既有 iOS 任务
   `8b99a267-df49-432a-885b-668d36bd396a` / 自动化
   `5bf44400-3dfc-4fca-8c3a-56bcab855db0` 不新增 uploader、不改范围。
   Android 先私有 CI artifact，核实稳定 keystore 和受限下载渠道后再启用自动分发。
   不将 debug key 当长期更新身份，不提交公司种子/凭据，不上 Play 正式版。
6. **Windows/Linux**：先选择产品接入再实施。Windows 需决定 Wintun 全流量与
   WFP 按应用接入、驱动分发和管理员服务；Linux 需决定桌面发行版、polkit/systemd
   权限分离、系统 DNS 管理器适配与包格式。独立于 Android 推进，交叉编译不验收。

## 构建

Go 1.26.5（go.mod）、SagerNet gomobile v0.1.13（go.mod）、NDK 27.1.12297006、
JDK 17、Android platform 36、AGP 9.2.1、Gradle 9.4.1。

```sh
export ANDROID_HOME=/path/to/Android/sdk
export JAVA_HOME=/path/to/jdk17
# go 须位于 PATH；脚本从 go.mod 固定版本构建 gobind
scripts/build-android-core.sh
cd android
./gradlew assembleDebug
```

产物：`build/android/Libbox.aar`、
`android/app/build/outputs/apk/debug/app-debug.apk`。默认 arm64/x86_64；本轮先验 arm64。
开发身份 `com.xd.xdial.android.debug`，正式身份预留 `com.xd.xdial.android`。
生成 release 签名方案前核实私有签名资产；本仓库没有可据以宣称已配置分发的证据。

参考：[Android VPN 官方契约](https://developer.android.com/develop/connectivity/vpn)。

## 2026-11-17 共享移动端控制面决策

- `core/mobile` 不依赖 libbox 或平台系统调用；`core/libbox` 仅提供 gomobile facade。
  iOS 的纯场景 picker 已调用同一 Go 实现；稳定的 Swift 界面、观测与现有事务逐步接入。
- 场景控制器区分 selected/active/pending；候选必须由宿主准备并提交，失败不改变
  正在承载的场景。手动选择作废未完成的自动观测；成功自动切换才计入十分钟防反复。
- Android 目前只允许断开后手动选场景和连接前 SSID 匹配。不能将桌面候选 Box API
  直接用于同一 TUN：需要独立的稳定移动入口，避免多个 gVisor 读者争抢包。
- 连接控制器管理用户意图、网络/尝试 epoch 与 1/2/4/8 秒有限重试；过期回调被忽略。
  系统网络不可用时等待，明确的配置/权限/认证错误不自动重试。
- Android 使用系统 `requestNetwork(INTERNET, NOT_VPN)` 返回的最佳网络；没有自行
  排序 Wi-Fi/蜂窝接口。使用回调提供的完整 LinkProperties，避免回调内同步查询竞态。
  网络恢复保留系统 TUN、先关闭旧核心再启动唯一新核心；这不是无缝场景切换事务。
- 用户主动停止/撤销授权取消网络请求与重试；前台通知提供断开按钮；不把进程重启
  当作恢复用户连接意图的授权，当前仍使用 START_NOT_STICKY。

依据：[Android NetworkCallback](https://developer.android.com/reference/android/net/ConnectivityManager.NetworkCallback)
和 [ConnectivityManager](https://developer.android.com/reference/android/net/ConnectivityManager)。
Windows/Linux 接入范围已向用户提出：优先原生窗口 + Wintun / Ubuntu TUN，或首版
包含按应用透明接入与多发行版；这不阻塞 Android。


## 2026-11-17 协议与账号阶段

Android 原生账号密码表单追加 AnyConnect 线路/场景，保留现有配置；凭据仍由平台加密
存储，选线与启动由共享 Go 负责。TLS、DTLS 和 Tailscale 端点保护审计见
[协议边界验收](../verification/android-protocol-sockets.md)。AAR 已构建 arm64/x86_64，
并不代表 x86_64 模拟器已经运行验收。Tailscale 登录、来源配置库、离线规则和发布仍继续。

按用户最新要求，具体进展、测试数字和阶段提交只留本 XDial 任务及仓库，不再主动发往
来源 Telegram 群或来源任务；仅用户询问或确需其提供信息/决策时联系。
