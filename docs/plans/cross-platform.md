# 全平台客户端交付计划

2026-11-17；基线 origin/main ba959c3。首个交付是 Android 可安装、真实系统
VPN 可连接。AAR、APK 构建成功均不代表全平台完成。

## 能力矩阵（当前实际状态）

| 能力 | macOS | iOS | tvOS | Android | Windows | Linux |
|---|---|---|---|---|---|---|
| Go 场景/规则编译与协议 | 已有 | 已有 | 已有 | AAR 已构建 arm64 | CLI 部分；libbox 排除 Windows | CLI 部分 |
| 系统接入 | Transparent Proxy | Packet Tunnel | Packet Tunnel | VpnService 开发验收实现，待设备验证 | 原生客户端/安装事务缺失 | 原生客户端/权限事务缺失 |
| 原生产品 UI | 已有 | 大拨盘/触感 | 遥控器 | 开发预览；正式拨盘待做 | 缺失 | 缺失 |
| 自动场景 | 已有 | Swift 自动驾驶 | 需逐项核对 | 待共享状态机移植 | 缺失 | 缺失 |
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
- 当前 Android 开发检查点拒绝 AnyConnect/Tailscale，直到完成协议自建 socket
  的防环路审计；不以普通代理的保护测试冒充所有协议已支持。
- 开发预览目前仅支持导入 runtime Profile、手动连接、撤权与清理；网络丢失断开并
  提示，尚非目标要求的自动恢复。不得将此检查点称为 Android 完成交付。

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
# go、gobind 须位于 PATH；gobind 使用 go.mod 同版本
scripts/build-android-core.sh
cd android
./gradlew assembleDebug
```

产物：`build/android/Libbox.aar`、
`android/app/build/outputs/apk/debug/app-debug.apk`。默认 arm64/x86_64；本轮先验 arm64。
开发身份 `com.xd.xdial.android.debug`，正式身份预留 `com.xd.xdial.android`。
生成 release 签名方案前核实私有签名资产；本仓库没有可据以宣称已配置分发的证据。

参考：[Android VPN 官方契约](https://developer.android.com/develop/connectivity/vpn)。
