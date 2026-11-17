# 全平台客户端交付计划

2026-11-17；基线 origin/main ba959c3。首个交付是 Android 可安装、真实系统
VPN 可连接。AAR、APK 构建成功均不代表全平台完成。

## 能力矩阵（当前实际状态）

| 能力 | macOS | iOS | tvOS | Android | Windows | Linux |
|---|---|---|---|---|---|---|
| Go 场景/规则编译与协议 | 已有 | 已有 | 已有 | AAR 已构建 arm64/x86_64；运行验收 arm64 | CLI 部分；libbox 排除 Windows | CLI 部分 |
| 系统接入 | Transparent Proxy | Packet Tunnel | Packet Tunnel | VpnService 模拟器 direct DNS/HTTPS 通过 | 原生客户端/安装事务缺失 | 原生客户端/权限事务缺失 |
| 原生产品 UI | 已有 | 大拨盘/触感 | 遥控器 | 原生拨盘、配置来源预览/更新、离线规则；完整场景编辑待做 | 缺失 | 缺失 |
| 自动场景 | 已有 | Swift 自动驾驶＋共享 Go picker | 需逐项核对 | 共享 Go 选择器；连接前 SSID 已接入 | 缺失 | 缺失 |
| 安全配置 | 共享 Profile/Keychain | Keychain | Keychain | Keystore AES-GCM + AtomicFile；身份持久化/篡改拒绝已验 | 缺失 | 缺失 |
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
  Tailscale 的端点级保护、Android 登录/节点选择与加密身份已接入，真实出口待隔离联调；
  不以普通代理或本地契约测试冒充企业 VPN 已通过。
- 开发预览支持导入 runtime Profile、手动场景、连接前 SSID 匹配、撤权与清理；
  网络丢失保留连接意图/TUN，恢复后重建唯一核心。地区判断、连接中场景事务、
  在线订阅来源、完整场景编辑与协议资源租约仍缺失，不得将此检查点称为 Android 完成交付。

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
   Android 先形成可审查构建产物，核实稳定 keystore 和受限下载渠道后再启用自动分发；
   公共仓库的 Actions artifact 不称为私有分发。
   不将 debug key 当长期更新身份，不提交公司种子/凭据，不上 Play 正式版。
6. **Windows/Linux**：用户于 2026-11-17 明确首版包含按应用透明接入与多发行版。
   Windows 实做应用识别、WFP 重定向/防环路、管理员服务与驱动签名安装；
   Linux 实做应用识别/透明接入、权限分离和多发行版安装，不以 Ubuntu 单一 TUN 代替。
   首批验证基线采用 Ubuntu/Debian、Fedora、Arch，分别验证安装、升级、卸载和 DNS
   恢复；具体后端需先做隔离原型证明，sing-box 仍独占路由/DNS 裁决。
   原生窗口复用 Go 控制面。驱动签名渠道待具体可审查产物形成后确认；交叉编译不验收。

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
Windows/Linux 范围已确定：首版按应用透明接入及多发行版（2026-11-17 用户回复）。
Android 继续独立推进；Windows/Linux 不再作为待范围确认项。


## 2026-11-17 协议与账号阶段

Android 原生账号密码表单追加 AnyConnect 线路/场景，保留现有配置；凭据仍由平台加密
存储，选线与启动由共享 Go 负责。TLS、DTLS 和 Tailscale 端点保护审计见
[协议边界验收](../verification/android-protocol-sockets.md)。AAR 已构建 arm64/x86_64，
并不代表 x86_64 模拟器已经运行验收。Tailscale 登录、来源配置库、离线规则和发布仍继续。

按用户最新要求，具体进展、测试数字和阶段提交只留本 XDial 任务及仓库，不再主动发往
来源 Telegram 群或来源任务；仅用户询问或确需其提供信息/决策时联系。

## 2026-11-17 Tailscale Android 设置阶段

已接上原生浏览器登录/节点列表/场景保存，解除开发 UI 对全部 Tailscale Profile 的
一刀切拒绝。共享控制面检查在线节点；数据连接等待认证和出口探测。Android 独立加密
身份适配，不改 Apple 状态目录/凭据行为。登录准备实际到达公开登录页；没有使用生产
凭据或完成 tailnet 注册，真实数据面仍待隔离环境。详细证据见 Android 检查点。

下一步继续离线规则与受保护的资源获取、配置来源库、地区观察和连接中的场景事务。
Windows/Linux 首版范围已按用户回答扩大到按应用透明接入及多发行版，不再等待范围回答。

## 2026-11-17 离线资源准备阶段

Android Direct 资源下载已进入受保护的独立准备事务；有效缓存可在断网恢复时直接
使用，不通过宿主默认 HTTP/DNS。下一步提供原生离线规则管理，并补齐其他获取 Line
与提交后的过期刷新。Tailscale 真实出口验证缺少隔离环境时不要求用户额外开通服务，
保留未验证标记并继续独立功能。

离线管理现已支持原生文件选择、内容校验、有效/较旧/无效状态和移除副本；已实测
模拟器断网时从系统文件提供者导入 SRS，恢复后无下载连接并通过普通 DNS/HTTPS。
其余获取 Line 和后台刷新仍须实现；不把已有本机副本等同于完整来源配置库。

## 2026-11-17 指定代理获取与后台刷新

资源编译器现在复用既有精确 Line 的 DNS/出口编译，生成不带系统入口、监听器和规则
绑定的 Android 能力；将本地 resolver 换成捕获的系统 DNS，socket 仍由保护/绑定回调
管理。原生代理获取不再一概拒绝。连接就绪之后刷新过期的有效缓存，失败保留原副本，
新副本下次连接生效。AnyConnect/Tailscale 获取会话仍须补齐进程/身份租约及已有会话
复用，不允许背景任务干扰当前连接；这项不能作为最终功能缩减。

## 2026-11-17 共享来源库与 Android 原生导入

- `core/config/profile_library.go` 实现 schema 3 的资源来源、全局组/场景、临时运行
  投影与纯内存导入预览。Android 不再直接覆盖单份 runtime Profile；Swift 稳定配置
  界面暂未迁移到该实现，其 schema 3 所有权、刷新与引用校验是本次行为基线。
- 本轮来源为本机 XDial/sing-box JSON，支持粘贴、SAF 文件入口、添加/更新预览、编辑
  来源选择及有引用保护的移除。导入命名空间由既有核心转换器生成，URL 规则只声明，
  不调用会从宿主 HTTP 展开 provider 的旧 `ImportProfile` 分支。订阅 URL 和非 JSON
  格式的受保护导入尚未接入；不能把此入口描述为全部订阅格式已完成。
- 更新替换来源资源，保留全局场景、SSID、默认出口与局部规则引用；移除固定组成员、
  部分匹配内容或匹配顺序引用时拒绝整次更新。跟随来源的组重排前固定原隐式选项。
  本机来源中相对基线修改的凭据保留；若服务器变更则拒绝携带本机凭据继续更新。
- 预览以原库摘要提交；普通编辑需运行投影的修订号。长生命周期场景控制器只可提交
  场景选择/自动偏好，且资源结构必须仍相同，避免旧调查覆盖新导入资源。
- Android Keystore + AES-GCM（库独立 AAD）写入 `profiles.enc`；仅新库不存在时
  只读转换 `profile.enc`，损坏/不可解密不回退。旧文件原字节保留。无旧 source ID
  的 Tailscale 身份通过库记录的精确安全存储命名空间映射继续使用，不复制 node key，
  不向其他来源回退。Tailscale setup 按当前编辑来源选择/创建线路。
- 验证见 android-checkpoint；这不是全部 Android 产品完成。后续继续连接中场景事务、
  地区观察、完整场景编辑、在线来源/协议资源租约以及事件驱动签名分发，Windows/Linux
  保持用户要求的按应用透明接入和多发行版范围。
