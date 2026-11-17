# 全平台客户端交付计划

2026-11-17；基线 origin/main ba959c3。首个交付是 Android 可安装、真实系统
VPN 可连接。AAR、APK 构建成功均不代表全平台完成。

## 能力矩阵（当前实际状态）

| 能力 | macOS | iOS | tvOS | Android | Windows | Linux |
|---|---|---|---|---|---|---|
| Go 场景/规则编译与协议 | 已有 | 已有 | 已有 | AAR 已构建 arm64/x86_64；运行验收 arm64 | libbox 共享核心已原机处理 TCP/UDP/DNS | libbox 共享核心已原机处理 TCP/UDP/DNS |
| 系统接入 | Transparent Proxy | Packet Tunnel | Packet Tunnel | VpnService 模拟器 direct DNS/HTTPS 通过 | 原生客户端/安装事务缺失 | 独立鉴权服务已接 TProxy/共享编译/捕获恢复；完整 Debian 安装、升级、回滚、强杀恢复与卸载通过；默认系统 DNS/其他发行版待做 |
| 核心进程生命周期 | 已有 | 内嵌核心 | 内嵌核心 | 内嵌核心 | Job 原子创建/独立回收原机通过 | 父死亡信号/直接子进程回收原机通过 |
| 原生产品 UI | 已有 | 大拨盘/触感 | 遥控器 | 原生拨盘、共享场景编辑、文件/在线来源预览更新、离线规则 | 缺失 | 缺失 |
| 自动场景 | 已有 | Swift 自动驾驶＋共享 Go picker | 需逐项核对 | 共享 Go 选择器；连接前 SSID/地区已接入，连接中自动观察待做 | 缺失 | 缺失 |
| 安全配置 | 共享 Profile/Keychain | Keychain | Keychain | Keystore AES-GCM + AtomicFile；身份持久化/篡改拒绝已验 | 缺失 | 缺失 |
| 发布 | 已有 | 既有事件驱动 TestFlight | 已有构建 | 双 ABI 本地测试包与事件 CI 已实现；稳定签名/受限分发待接入 | 缺失 | 缺失 |

Linux 最新系统证据见 [隔离来宾验收](../verification/linux-transparent-guest.md)：真实
nft TProxy TCP/UDP/DNS、OS 进程路径分流、场景提交/退休已通过。已新增
[捕获安装/恢复组件](../verification/linux-capture-transaction.md)，已接入[独立授权服务](../agent/linux-service.md)；仍需原生 DNS 后端、
IPv6、多发行版安装和原生 UI；继续推进完整安装生命周期及 Windows WFP
边界，不以本轮测试来宾替代完整桌面交付。

Windows 已新增独立的 [命名管道鉴权层](../verification/windows-control-pipe.md)，
按内核账号/登录会话授权并校验服务端身份，原机跨进程通信与拒绝路径已验；尚未接
SCM 安装、业务控制器或 WFP 系统拦截，不代表 Windows 权限服务已完整交付。

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
  网络丢失保留连接意图/TUN，恢复后重建唯一核心。已有连接前地区判断、稳定入口的
  手动场景事务、场景编辑及受保护在线 JSON 来源；连接中自动观察、非 JSON 来源、
  AnyConnect/Tailscale 资源租约仍缺失，不得将此检查点称为 Android 完成交付。

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

## 2026-11-17 Android 构建与分发准备

- Android 版本改为仓库内单一来源；增加固定工具链的双 ABI 打包入口、APK 元数据/
  签名/对齐校验、不可变产物与来源清单。现有 push/PR CI 增加同 SHA Android job，
  不新增轮询或 iOS 上传器；分支尚未推送，GitHub job 尚未运行。
- 本机连续两次 Debug/unsigned Release 构建字节一致，真实临时签名、错证书与篡改
  拒绝测试通过；模拟器覆盖安装保持来源库，普通 UDP DNS 与 HTTPS 通过，已断开。
  固定工具链步骤不等于已经证明跨主机字节复现。
- 用户选择复用已有 Android 签名和内部渠道，随后要求先查看 Cindy/火炬 2。
  已核查现有源码与非秘密资产描述；Cindy 自签与 OSS/CDN 分发分开，火炬 2 的
  Play 上传密钥不同于商店应用签名。具体接入与剩余核验见
  [Android 构建与测试分发](../agent/android-release.md)。未新建长期身份、复制
  秘密或发布其他应用的渠道；公共 Actions artifact 不算受限分发。
- 后续仍须完成连接中场景事务、地区观察、协议资源租约、在线来源与完整场景编辑，
  以及 Windows/Linux 原生按应用接入。构建发布准备不是全平台交付完成。

## 2026-11-17 Android 地区观察与自动条件编辑

- Android 连接前以及网络恢复时，先按共享控制器匹配 SSID；只有启用自动选择、没有
  SSID 命中且存在地区条件时，才执行有时限的地区查询。查询使用捕获系统 DNS 的独立
  sing-box Direct 传输，每个 socket 都需保护并绑定 Underlay；不创建 TUN，不借当前
  用户场景绕一圈判断地区。未知结果保留选择，停止可取消，网络变化使旧调查失效。
- 当前场景的 Wi-Fi 名称和地区条件可通过原生对话框编辑；共享 Go 编辑器保留来源、
  全局场景其它字段和未知字段，拒绝抢占其它场景的条件。提交经过来源库修订号校验。
  这只是自动条件编辑，尚不是完整场景/规则/出口编辑器。
- 明确点选当前已选场景也关闭自动选择，取消未完成的自动调查；与选择不同场景具有
  相同手动覆盖语义。地区只在连接准备/恢复时观察，连接中的实时自动事务仍未实现。
- 审计发现旧 `PrepareSwitch` 候选会继承同一个 TUN fd，因此不能直接用于 Android。
  核心现在提前拒绝含 TUN 的切换候选，避免两个 gVisor 同时读取一个设备；已有 flow/
  relay 场景事务保留。下一步需独立持有稳定入口，按 generation 将新流交给候选核心，
  保留旧流的退役期限，并覆盖快速路径、DNS 与取消。不得把 stop/start 恢复改名为无缝切换。
- 真正连续重连发现 Android 在本进程 TUN fd 已归零后，仍可能短暂返回旧的默认 VPN。
  原入口立即报错；现在等待系统默认网络回调后重新读取事实，确认无 VPN 才申请 Underlay。
  等待最长 20 秒，手动停止清理监听与计时器；不轮询、不覆盖其它 VPN、不按接口猜出口。

## 2026-11-17 稳定 Android 入口与连接中手动切换

- Android 显式启用独立持有的 TUN 入口，沿用 sing-box 的 TUN/gVisor 接包实现。
  候选数据 Box 不包含 TUN；提交只替换新流使用的完整 DNS/路由 generation，旧流默认
  1 秒退役。开始下一候选前先退役旧源，停止先禁止接新流，再关闭所有 generation 和入口。
  Apple 未启用此模式；原有含 TUN 的候选守卫保留。
- TCP/UDP 在完整 flow 路由入口绑定 generation，避免早期规则判断与后续连接跨代际；
  本模式暂不启用其原生 PreMatch 快速转发优化。ICMP 保留 sing-box 原生 Port，并登记
  FlowHandle，覆盖迟到的 tracker 创建/挂接。DNS 请求与迟到响应也受 generation 租约约束。
  这不是另建 DNS/路由策略，吞吐与功耗仍需真机测量。
- Android 原生场景选择接共享控制器的候选/提交/取消。准备期间旧连接继续；选择保存
  成功后才提交数据面，提交失败恢复原选择。显式选择关闭自动；连续意图使旧候选失效，
  网络修订变化拒绝旧候选。独占协议身份变化由 Go 判断并转完整重连，不伪装会话复用。
- AnyConnect/Tailscale 数据会话使用已有 Line 能力池。Tailscale 原生 DNS 的单一重配置
  hook 归能力 Box，新旧场景借用同一解析器及按消费者标签查找的 endpoint；消费者关闭
  不关闭共享身份。能力 DNS 保留编译器的保护设置，Android 拒绝 local/非数值系统解析器。
  真实企业认证和已登录 tailnet 出口仍未验证。
- 已实测连接中 One/Travel 切换及真实 UDP DNS/HTTPS，失败候选保留 One 和可用流量；
  准备中断开后读回未连接、TUN fd 为零。AnyConnect 尚有 6 秒拨号取消延迟，不称即时
  取消；本阶段未实测成功的多协议认证切换、ICMP 真机、连续 pending 意图覆盖或系统撤权。
- 后续继续连接中自动观察、完整场景编辑/在线来源、协议冷资源租约与取消收敛；签名和
  内部分发仍按既有核查结果推进。Windows/Linux 保持按应用透明接入、多发行版范围，
  不以本阶段 Android 场景事务作为全平台完成。

## 2026-11-17 共享场景编辑与 Android 原生草稿

- Go 提供离线场景编辑及不含线路凭据、资源地址/条件内容的展示模型。Android 只负责
  原生选择器、表单、草稿取消和安全存储；创建/重命名、默认出口、规则绑定/移除/顺序、
  删除保护都由共享代码校验。Wi-Fi/地区条件继续使用既有共享编辑器。
- 修改出口保留部分条件和未知字段；沿用来源交错顺序时不允许直接移动绑定。只有用户
  显式选择“改用列表顺序”才移除交错顺序；“使用完整规则”也需要明确确认，避免静默
  扩大匹配范围。删除当前选择、最后场景或仍有自动条件引用的场景会被拒绝。
- 草稿只在内存中保留，不写 Android 保存状态 Bundle；返回可放弃，保存通过库修订号
  CAS 与 Keystore 原子写。保存和连接启动互斥，控制器失效与读取也串行，防止新连接
  借用保存前的控制器。连接期间编辑器只读；本阶段没有扩展为运行中修改配置事务。
- 隔离设备已验证创建、重命名、两条规则绑定/上移、保存后进程重启、放弃删除绑定草稿、
  当前场景删除拒绝及切换选择后的删除成功。最终包重新验证新建/保存/选择/真实 UDP
  DNS 与 HTTPS。来源原始交错/部分引用、并发旧草稿 CAS 的证据为核心测试，不冒称
  已在设备上覆盖所有导入格式。
- 后续重点是受保护在线来源预览与提交、连接中自动观察、协议冷资源租约及取消；同时
  需进入 Windows/Linux 的实际系统接入与跨机验收，不能继续把桌面支持停留在矩阵。
  签名/内部分发沿既有调查继续；不改 iOS 自动化，不进行未经授权的推送或发布。

## 2026-11-17 受保护在线配置来源

- Android 链接下载复用现有 VpnService 的授权、系统网络事实与 protect/bind，独立
  sing-box 资源会话不建立 TUN。指定线路同时拥有 DNS 和 HTTPS，不静默改用 Direct。
  下载最多 2 MiB，核心 30 秒有界，服务整体 35 秒取消；只接受 XDial/sing-box JSON，
  URL 规则仍是声明，不触发旧导入器的 provider 展开。
- 共享来源记录增加可选 `fetchLineID`，新来源明确记录获取线路，旧记录缺失视为 Direct。
  加密库保存完整 URL（可能含查询令牌），列表不展示地址/凭据。预览绑定下载前的库摘要，
  原链接更新不能改变来源地址或线路，且不得移除被引用的获取线路。文件更新不能冒充
  已记录的在线来源。来源资源更新继续保留全局场景和用户出口选择。
- 下载请求、URL、预览仅在内存协调器中流转，Intent 只含随机请求 ID；离开页面、
  系统撤权、断开、超时和网络快照变化会取消。取消/失败不保存，下载成功也必须预览确认。
  Android 仅手动刷新，本阶段不增加定时订阅拉取。
- Apple Codable 保留新获取线路字段；旧 URLSession 导入器不能履行显式 Line，遇到
  此类来源明确拒绝刷新，原来源字段缺失时保持既有行为。后续应接共享精确 Line 获取
  能力，而不是删除保护或把这项跨端缺口当永久产品范围。专项 XCTest 已通过。
- 独立模拟器已从本仓库公开示例下载、保存和原链接更新，socket 保护 2 次、失败 0、
  TUN 0；断网等待取消后恢复不复活；最终更新后 One 的普通 UDP DNS/HTTPS 通过。
  非 Direct 真正成功的在线导入、设备上的响应体中途取消仍待补验。
- 下一阶段转入 Windows/Linux 实际运行边界。已只读预检两台登记测试机：Windows
  原生启动函数仍为空，Linux 清理按进程模式过宽。Ubuntu 24.04 用户网络命名空间被
  拒绝，未发现 QEMU/Docker/Podman，当前账号无 /dev/kvm 访问；不改宿主策略、不提权。
  先用隔离任务目录验证进程所有权/回收及共享控制面，再安排真正隔离的透明流量环境。
  这些预检不是桌面 VPN 或多发行版验收。

## 2026-11-17 Windows/Linux 原机进程生命周期

- Windows 已实现创建时原子加入 Job 的核心启动；Linux 使用随创建线程存活的
  父死亡信号租约。删除两平台按进程名/模式清理所有实例的旧逻辑。
- 已在 Windows 10.0.26300 和 Ubuntu 24.04 原机执行回环进程测试：独立实例、
  父进程崩溃回收、错误退出；Windows 另验所属 Job 与子孙回收。源码在本机交叉
  构建后原生执行，不称远端源码构建或透明流量验收。
- Apple 六构建、核心/CLI race、Android 双 ABI 构建通过；没有宿主安装或网络变化。
  详见 [桌面进程证据](../verification/desktop-process-ownership.md)。
- 后续必须补本地服务对端鉴权（当前非 macOS `peerAllowed` 直接放行），再接入
  Windows WFP/应用识别与 Linux 透明入口和多发行版安装。Ubuntu 当前禁止用户
  网络命名空间，不绕过权限改变测试机网络。进程生命周期已实做，桌面产品仍未完成。
- Linux 主动退出另经原机补验：SIGTERM 允许完成清理，无响应才超时强制结束；
  父进程崩溃仍立即回收。最终 Ubuntu 五项通过，Windows 四项通过。
- 已定位后续共享核心的 Windows 编译边界：libbox 普遍 `!windows`，平台 TUN
  使用 Dup/Close，身份锁使用 Flock；应拆出 OS 适配后复用现有流入口和事务，不能
  复制第二套路由核心。sing-box 已有 Windows/Linux process searcher，应用身份
  优先接原生事实/既有 searcher，不在平台层重新实现路由裁决。旧 daemon 的数据面
  命令已退役，新增桌面服务不能直接恢复退役命令来绕过事务边界。

## 2026-11-17 Windows/Linux 共享核心与接流

- libbox 已解除整体 Windows 排除，Unix TUN 与身份文件锁拆成 OS 适配；Windows
  使用 LockFileEx，不能把 Unix fd 强转成 HANDLE 或偷偷建立系统适配器。
- 新原生流 API 接可信 OS 原始端点/进程事实，复用稳定入口与既有切换事务。
  Windows 原机 9 项、Ubuntu libbox 11 项加 CLI 凭据 1 项通过；真实 loopback
  TCP/UDP、受控 DNS、新旧流提交/退休、系统 socket 表程序身份查询均有证据。
  截获端仍为受控流替身，没有 WFP/nftables 系统拦截，不称完整透明客户端。
- Linux 旧 daemon 改 SO_PEERCRED 同 effective UID；其他未实现凭据的平台拒绝，
  不保留全部放行。Windows 原生命名管道/权限服务仍需实现，旧 macOS 注册命令
  不应被移植成 Windows 权限服务。Windows 完整旧 CLI 构建尚未通过。
- Apple 六构建、已有 packet/switch 定向 race 和核心常规测试、Android 双 ABI
  打包校验通过。没有新增 Android 设备交互或 XCTest 证据。详见
  [桌面流入口验收](../verification/desktop-native-flows.md)。
- 下一阶段接真实系统透明入口和防环路：以任务隔离环境验证 Linux 原始目标/UDP
  回包/应用归属与撤销，再补多发行版安装；Windows 完成原生服务/驱动边界与签名
  方案后再请求确需的系统操作。此阶段不安装驱动、不改共享测试机网络。

## 2026-11-17 Linux 捕获连接事务

- 新增独立 `core/linuxcapture`，通过结构化 netlink 安装精确预留的专用路由、规则和
  socket-owned nft 表；先就绪 listener 后接管，先撤销捕获再停核心。私有状态目录、
  独占锁与 fsync 日志支撑失败回滚和崩溃恢复，归属变化保留现场，不清他人配置。
- 隔离 QEMU 的七项权限测试与两项 libbox 内核测试通过，包含实际子进程强杀、
  真实 TCP/UDP/DNS、OS 应用身份和场景事务。详见
  [安装恢复证据](../verification/linux-capture-transaction.md)。
- 正式 systemd/polkit 授权服务、共享业务控制器生命周期、IPv6、多发行版与原生 UI
  仍需接入。复杂 nft/legacy iptables mark 策略暂明确拒绝，后续补保留/共存协议；
  不能把该拒绝作为永久缩减目标。Windows 继续服务进程/SCM 与 WFP 接入。


## 2026-11-17 Linux 独立权限服务接线

`cmd/xdial-service` 已提供可运行的服务/普通用户 CLI；`core/linuxservice` 复用共享
ConnectionController、Profile/Scenario 编译和 libbox 稳定入口。服务持有 capture
journal/lasting nft socket，先核心就绪再接管，先撤销接管再停止；强杀后下一次启动
恢复自身剩余规则，保持未连接。Unix seqpacket 每条消息使用 SCM_CREDENTIALS，
客户端固定服务 UID 0；实例 nonce + revision 拒绝跨重启/配置过期提交。

编译器把原场景应用条件转换为 sing-box 原生 process_path/name/regex，不复制
业务规则；Linux marked socket 使用内核完整的按目标 Underlay，未伪造默认接口。
当前 OS resolver 后端严格支持一个非 loopback IPv4 nameserver；不能把系统 stub、
search/split/multiple DNS 扁平化。网络事件使捕获退出，尚未自动恢复。cache-only
编译拒绝客户端 file URL/NativeOptions 文件权限；在线资源和协议租约仍待接入。

[服务构建与安装说明](../agent/linux-service.md) 与
[独立服务验收](../verification/linux-service.md) 区分真实服务流量与尚未运行的
systemd 安装包。已有 x64 包、unit、安装/移除脚本和来源摘要；没有执行共享宿主
安装、公开分发或推送。下一步做完整发行版来宾中的原生 DNS/安装/升级/卸载，
并推进 Windows SCM/WFP；不以这个 Ubuntu 内核服务替代多发行版和完整原生产品。


### 2026-11-17：完整 Debian systemd 安装验收

完整 Debian 13.7 官方镜像已实际启动自己的内核/systemd，以包内脚本安装、启动、
重启、强杀恢复、连接中卸载。真实 UID 1001 请求与透明 TCP/DNS 通过；首次安装
默认不启动、重复安装不覆盖策略、重启回未连接、卸载保留策略且无捕获残留通过。
完整系统暴露 unit 缺少低端口权限，已补 `CAP_NET_BIND_SERVICE`；负面对照证明
只有去掉该权限时 TCP/上游 DNS 可达但透明 DNS 回包失败。

[详细收据](../verification/linux-distro-systemd.md) 区分完整发行版和旧 initramfs，
本阶段只覆盖 Debian 的受控 resolver，不能当作默认 systemd-resolved、Fedora/Arch、
升级或全平台完成。下一步继续受保护原生系统 DNS（保留 split DNS）、升级事务和
其他发行版，Windows SCM/WFP 与移动端剩余项继续保留。没有共享宿主系统安装或改网。

### 2026-11-17：Linux 升级闭环

独立 root 安装命令已接持久备份/journal、停止捕获、原子替换、systemd READY 提交、
失败回滚与强杀后显式恢复。新服务连接事务和安装共享维护锁；维护不能阻止用户断开，
未完成记录阻止新服务再次连接。保留策略/缓存/enablement，不恢复 VPN 连接意图。
完整 Debian 中用上阶段包作为旧版，实际包命令、可写来源拒绝、真实候选启动失败、
九处子进程 SIGKILL、外部替换拒绝、恢复幂等与最终包透明 TCP/DNS/卸载均通过。
详见[升级验收](../verification/linux-upgrade.md)，两组测试使用相同二进制。

这仍是显式管理员 CLI 安装，不是签名自动更新器或完整桌面 UI。下一阶段优先补
受保护的原生系统 DNS（包括真实 resolved/split DNS），再用完整 Fedora/Arch 来宾
执行安装与升级；不能用同一个 Debian 镜像换标签。Windows SCM/WFP、Linux 协议
租约/IPv6/既有 mark 共存、移动端和发布缺口保持原范围，避免重复已通过的同一路径。
