# Android 开发检查点（未完成交付）

2026-11-17，工作分支 cindy/proud-kalam，基线 ba959c3。

## 首个检查点执行记录

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

## 首个检查点的限制（后续进展见下文）

不能宣称端到端 VPN 可用、自动场景/登录完成、全平台完成或已发布。当前 App 是开发验收
检查点，不是交付版。未完成 Apple 全量构建/回归；Go 在 Darwin 上的核心测试不等于 Apple
App 验收。没有创建第二个 iOS uploader，没有修改既有自动化，没有推送/PR/合并。

## 首个检查点的待办（下文记录完成情况）

1. 通过服务持有的 core.Diagnostics 观察 TUN 收发、socket保护调用计数；只展示计数与
   阶段错误，不记录配置、凭据或原始目标。修通隔离模拟器 DNS+TCP，再进行 UDP 验收。
2. 将完整 Android LinkProperties / NetworkInterface 事实投射到 SetNetworkInterfaces；
   逐个核实 sing-tun 的 Android fd 属性，不借用 Apple 的隐式假设。
3. 按 ../plans/cross-platform.md 继续业务状态机、登录、自动场景、恢复、原生 UI、分发。

构建日志位于 worktree 的 build/android（忽略目录）。APK：
`android/app/build/outputs/apk/debug/app-debug.apk`。尚未分发，不提交二进制到源码。

## 2026-11-17 后续：真实数据面通过

- 不开 VPN 的浏览器基线可达；开启后保护调用为零。Android 拒绝 Go netlink
  查询，原来的 name/index 快照无法补齐 flags/MTU/address，sing-box 没有可用接口。
- 新增可选 `facts` 快照，由 Android NetworkInterface/LinkProperties 原样提供；
  默认接口、切换候选、复用 Line 使用这些事实，Apple 未传 facts 时保留既有路径。
- 补齐后保护调用增长、失败为零；Android 新配置入口以 LinkProperties 的系统 DNS
  替代移动端硬编码公共 fallback，保持 sing-box 内 DNS dispatcher 与规则所有权。
- VPN 开启时 Chrome 实际显示 `https://example.com/?xdial=verified` 正文；apple.com
  一次出现 ERR_CONNECTION_CLOSED，未将该目标计为通过。
- 独立开发验收探测通过：普通 app socket（未 protect、不绑定 Underlay），先确认
  默认网络具备 TRANSPORT_VPN，再向 TUN 的合成 DNS 发送 UDP 查询并校验响应 ID、
  response bit、NXDOMAIN；HTTPS 访问 Apple connectivity success 页面，校验 200
  和非空正文。模拟器日志明确 `UDP DNS=通过；HTTPS=通过`。
- Go config/libbox 完整测试再次通过，新增接口事实继承/无效快照原子拒绝，以及
  Android DNS 快照与 Apple 旧行为隔离契约测试；public-content-gate 通过。
- 注意旧 Diagnostics 的 TUN 包计数没有插桩，一直是零，不能据此判断无流量。
  诊断改用共享 Router 的 TrafficTotals，加保护成功/失败计数，不记录目标或凭据。

这些证据验证的是模拟器 direct 场景的 DNS/UDP 与 HTTPS，不代表真实企业 VPN
登录、AnyConnect/Tailscale、所有普通 UDP 应用或物理 Android 设备已经验收。


## 2026-11-17 后续：共享场景、拨盘与断网恢复

已实现和验证：

- 共享 Go 场景选择器：SSID 优先、地区映射 fallback、无有效目标不切换；可绑定
  控制器支持手动覆盖、过期观测作废、候选提交/中止、十分钟防反复和未知配置字段保留。
- Android 手动 One → Two → 重启仍为 Two，再选择 One → 重启仍为 One；手动选择关闭
  自动选择。配置与自动偏好一起保存在 Keystore 加密的 AtomicFile 中。
- iOS 的 ScenarioAutoPicker 使用同一 Go picker；Android 不复制 Swift 的选择逻辑。
  Android 连接前可读授权 SSID，地区观测和连接中的场景事务未接入。
- Android 原生大拨盘点击连接、拖动断开通过系统触摸验收；按钮无障碍标签随状态变化，
  深浅色已截图检查。振动调用与动画降级已实现，但模拟器不能证明物理触感效果。
- 共享连接控制器 race 测试通过：重复启动租约、断网/恢复、主动停止优先、旧网络与
  旧重试回调忽略、有限退避和终止错误不重试。
- 隔离 Android 模拟器连接后，通过插件关闭 Wi-Fi/移动数据，读回“网络已断开，等待恢复”；
  恢复系统网络后无需点击自动读回“已连接”。随后普通未 protect 的 App socket 再次
  完成 UDP DNS 响应验证与 HTTPS 200/正文验证（XDialProbe 日志通过）。
- 离线时点击连接进入等待，再主动断开，恢复网络后仍为“未连接”；没有自动复活。
  本阶段验收结束时模拟器网络已恢复、VPN 已断开。
- core/mobile race、core/config 与 core/libbox 完整测试通过；iOS/tvOS generic
  Simulator App 构建通过；macOS Debug/Release App 与扩展的 ci-macos-build 通过。
- make test-macos-transaction 通过，macOS XCTest 共 909 项、0 失败；该目标使用
  独立测试产物和 fixture，未覆盖安装/重启宿主 XDial。
- iOS build-for-testing 成功，但未执行 XCTest（插件没有 XCTest runner；独立设备不在
  xcodebuild destination 列表）。iOS 启动只验到 UI 渲染，出现本地配置读取提示，不能
  作为完整功能回归通过；未重置既有设备 Keychain。该设备已关机释放。

尚未验收：Android 真机、Wi-Fi/蜂窝跨网络切换、进程死亡恢复、系统撤权、后台长时间
运行、地区观测、离线规则、登录、AnyConnect/Tailscale 和连接中无缝场景切换。
新网络恢复逻辑保持单个系统 TUN，但会重建数据核心，已有连接不保证续传。
本节是新的阶段证据，不表示全部平台或 Android 产品闭环已完成。


## 2026-11-17 后续：协议保护与账号表单

见 [Android 协议套接字边界](android-protocol-sockets.md)。AnyConnect 保护与启动入口
已接入，原生账号表单通过隔离模拟器错误路径和场景保留验证；切回 Direct 后 DNS/HTTPS
仍通过。Tailscale scoped hooks 的多实例、拒绝回退、DERP proxy 测试通过，登录产品流程
仍未交付。AAR 已实际同时包含 arm64-v8a、x86_64 两种 libgojni；未测试 x86_64 设备。

## 2026-11-17 后续：Tailscale 设置与加密身份

- 新增共享 Go setup/节点选择 API：准备登录保留当前场景；只有 LocalAPI 当前
  `Running` 且选中节点在线才追加独立场景/线路，不改已有场景的出口绑定。
  返回的 Profile 仍有隐私数据，只能进入平台加密存储。Android 未复制 Swift 选线逻辑。
- Android setup 使用同一 sing-box 编译器的 Direct DNS，读取 LinkProperties DNS 快照；
  不创建 TUN、不设置 exit node/accept_routes/system_interface。与正式连接串行持有身份，
  设置结束后才允许启动流量连接。网络变化仍经 epoch 退休旧核心。
- 协议身份通过可绑定 `SecureStateStore` 进入 Android Keystore/AES-GCM + AtomicFile，
  namespace/key 作为认证附加数据且只用哈希命名文件。无备份目录不保存明文状态；
  后端失败不退回 tsnet FileStore。切换候选/Line capability 继承同一安全存储。
  Android 跳过 tsnet 明文 logtail 缓冲与上传器；登录 URL 不进协议日志或诊断。
- 新增原生登录设置窗：取得登录链接、在系统浏览器完成登录、查看在线出口、保存场景、
  显式退出登录；密码/账号设置窗使用 FLAG_SECURE。URL 只驻留内存。
- 隔离 arm64 模拟器真实 setup 启动成功；状态进入 NeedsLogin，点击浏览器打开实际
  Tailscale 登录页（Sign in / Google / Microsoft / GitHub / Apple 按钮可见）。
  **未输入账号、未完成认证、未加入 tailnet；不能作为出口节点流量联调通过。**
- setup 诊断：running=true、tun_fd_ready=false、上传/下载均 0、protected_sockets=2、
  protection_failures=0。UI 明确显示“账号设置（未接管流量）”。连接控制器的 connected
  此时仅代表 setup runtime 已启动，诊断增加 session_kind 区分。
- 模拟器真实 Keystore 验收：独立非秘密随机命名记录，重新创建存储实例后读回相同数据；
  缺键返回空；将密文复制到另一键名时 AEAD 拒绝；删除后读空。finally 清理验收记录。
- 结束设置后读回未连接；原 One 场景保留，重新连接并由普通未 protect App socket
  再次完成 UDP DNS/NXDOMAIN 和 HTTPS 200/正文验证，随后主动断开。
- 正式 Tailscale 连接现在须等待 Running、在线且已选择的出口，以及 TSMP peer 探测；
  MagicDNS 显式启用时验证 endpoint 的就绪状态和域归属；沿用 NE 原生 DNS transport，
  不把桌面 hosts 内存快照接口误用在移动端 transport。仅 core.Start 返回不显示“已连接”。
  这些就绪门槛有契约测试；真实出口验收待已有授权的隔离测试环境。

完整验收、最终 APK 指纹及本阶段回归结果在完成下述检查后追加，开发版仍不作为完整交付。

回归记录：core/mobile race、Android setup/安全存储/就绪门槛定向 race、core/config
全量测试通过；core/libbox 不带 race 全量 4.392s 通过。全量 libbox race 两次有既有
3 秒启动回调与 2 秒代理探测用例超时，单独运行这些用例通过，未出现 race detector
报告；不把全量 race 写成通过，另做基线对照。补丁套件与 public-content-gate 通过。
iOS/tvOS generic 模拟器、macOS Debug/Release App/扩展构建通过；本阶段未执行 iOS
XCTest 或重新运行 macOS 909 项测试，不引用旧测试冒充本次结果。

最终构建包含 arm64-v8a 与 x86_64；APK SHA-256：
`7e4797d81033e31f4650bab3c41ef7efaab21e9c38b5233d38049759eb59490a`。
插件核对 `matchesLocal=true`，最终标准 core/config/libbox/mobile 全量测试通过。
APK 路径仍为 `android/app/build/outputs/apk/debug/app-debug.apk`；仅本地安装，未分发。

基线对照：将提交 `44ab325` 的 core/go.mod/go.sum 导出到忽略目录，复用同一补丁依赖
（新依赖行为仅 Android 分支，Darwin 不启用）运行全量 libbox race。216.587s 后也出现
启动回调、代理测速、超时上限等时限测试失败，未有数据竞争报告。因此这是全量 race
时序限制的复现，不能将其算作通过或断言根因已修复。日志分别保存为
`build/android/baseline-core-race.log`、`tailscale-core-rerun.log` 和
`tailscale-final-standard.log`。没有调大生产/测试超时来掩盖结果。

## 2026-11-17 Android 受保护的资源准备与缓存恢复

- `PrepareAndroidConfig` 在正式数据面启动前持有可取消的准备租约；按捕获的系统 DNS、
  protector/Network 事实创建无 TUN、无用户场景绑定的独立 sing-box Direct 传输。
  DNS 与数值地址 HTTP 拨号走同一传输，继续使用逐跳 HTTPS、全地址 SSRF、内容语义
  校验和原子缓存。普通 `GenerateAndroidConfig` 改为只读有效缓存，不能旁路保护器。
- 用户停止/撤权可从服务线程立即取消准备，再等待串行清理；Stop 退休准备租约之后
  才允许换保护器。严格 HTTP 临时传输结束时关闭闲置连接，避免越过能力生命周期。
- 独立 arm64 模拟器导入非秘密 Direct 测试 Profile，绑定显式公开 SRS 来源
  `https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ads-all.srs`。
  首次无缓存连接准备阶段保护 socket 2 次、总计 4 次、失败 0 次。随后普通未 protect
  App socket 的 UDP DNS 响应 ID/NXDOMAIN 与 HTTPS 200/正文均通过。
- 插件断网后显示等待恢复；恢复网络后保留 TUN 并重建唯一核心。准备阶段 socket 0 次，
  使用有效离线副本；总保护 7 次、失败 0 次；普通 DNS/HTTPS 再次通过。这里证明了
  冷缓存首次下载及暖缓存恢复，尚未模拟保留旧 TUN 时冷缓存缺失的完整下载。
- 契约测试覆盖保护拒绝无 fallback、无 TUN 能力、缓存独立编译、取消/Stop 竞争、
  未提供 fetcher 时返回错误而非 panic。显式非 Direct `fetch_line_id` 冷缓存仍拒绝，
  不静默改走 Direct；其他 Line 获取事务、提交后过期刷新与离线管理 UI 待继续。
- Android 定向 race、core/config/libbox/mobile 常规全量测试通过；临时 HTTP 清理修改后
  subscription/libbox 再验证通过。public-content-gate 通过。AAR 双 ABI、APK、iOS/tvOS
  generic 模拟器、macOS Debug/Release App/扩展构建通过；未新增 XCTest 执行证据。
- 最终 APK SHA-256：
  `0f75f2dfa92c46962c2b5d6258d31f581668af8c363bc286c8e4afe2d29535ad`；
  插件 `matchesLocal=true`。最终包安装后缓存连接与普通 DNS/HTTPS 复验通过，
  已主动断开并读回“未连接”，模拟器网络恢复。未改变宿主 VPN、上传或分发。

用户无需额外开通 Tailscale；没有隔离 tailnet 凭据时保留真实认证出口未验证，继续其他
独立功能。此阶段仍为开发验收，不代表 Android 产品或全平台完成。

## 2026-11-17 原生离线规则管理

- 新增共享 `ListOfflineRuleSets / ImportOfflineRuleSet / RemoveOfflineRuleSet`：复用运行时
  RuleSet 身份、URL、格式构成的缓存键，支持 SRS、source JSON 和域名/CIDR 文本。
  内容大小和语义先校验，再原子替换；失败保留旧副本。来源在文件选择期间变化则拒绝，
  不按任意路径读写；列表不返回 URL、缓存路径或规则内容，浏览不创建资源或发请求。
- Android 原生“规则与离线副本”页面显示无副本、有效、较旧、无效；提供系统文件
  选择器、source/text 粘贴和带说明的移除操作。导入不改规则来源或绑定，连接期间只读。
  文件读取在后台且最多 1 MiB；不申请广泛存储权限，不保存 content URI 的长期权限。
- arm64 模拟器用浏览器下载同一公开 SRS 到 Downloads；VPN 未连接。插件切到断网后，
  在界面移除旧副本并读回“尚无离线副本”，通过 ACTION_OPEN_DOCUMENT 选择设备中的
  `geosite-category-ads-all.srs`，读回“副本已校验并保存”及有效 8.18 kB 副本。
  已实际经过文件提供者读取，未使用 adb、宿主复制容器或伪造文件选择回调。
- 恢复网络后连接 One；准备保护 socket 0 次、总保护 3 次、失败 0 次，普通未 protect
  App socket UDP DNS/NXDOMAIN 和 HTTPS 200/正文通过。连接期间页面显示只读提示。
  最终主动断开，设备网络恢复；公开下载文件留在隔离设备 Downloads 供后续验收。
- 契约测试验证错误版本/无效/超限输入保留原副本、URL/格式变化拒绝、路径注入拒绝、
  过期/损坏状态、文本转换、真实 SRS 编解码、未绑定规则不会因导入激活。
  Offline/Android 定向 race 通过；libbox/mobile/subscription 常规测试通过。config 首次
  测试命令漏带 build/tools 中的 sing-box，补齐 PATH 后全量通过；不将失败运行写成通过。
  public-content-gate、双 ABI AAR/APK 与 Apple 六个构建目标通过，未执行新增 XCTest。
- APK SHA-256：`709bf77e3346071888af4268149162d493053c47001a054ceba8183858ca4f7d`，
  插件核对 matchesLocal=true。构建日志在 `build/android/offline-*`，本阶段仍仅本地安装。

待继续：非 Direct 获取 Line、提交后的过期刷新、配置来源库、地区观察、连接中场景
事务、后台/撤权更完整的验收及事件驱动分发。Windows/Linux 按应用透明接入与多发行版
目标不变。未认证的企业 VPN 和 Tailscale 实际出口仍无流量联调证据。

## 2026-11-17 指定代理资源与提交后刷新

- 新增核心 `GenerateAndroidAcquisition`，复用已有精确 Line 的 DNS/出站编译。
  编译模式明确不生成监听器，不使用虚构接口或临时凭据；Android 获取配置中无 TUN、
  user RuleSet、持久 cache_file 或 type:local resolver。平台仍只给网络事实和 socket
  保护。Direct 解析使用原始系统 DNS 顺序的首项，代理资源 DNS 和 HTTPS 走同一 Line。
- `PrepareAndroidConfig` 支持共享核心已有原生代理类型的冷缓存获取；临时传输关闭后
  再启动正式数据面。AnyConnect/Tailscale 资源会话遇到缺失身份租约仍明确拒绝，
  不换 Direct。常规用户流量的 AnyConnect/Tailscale 支持保持不变。
- `RefreshAndroidRuleSets` 在连接 readiness/commit 后后台执行，持当前运行租约，
  最长 60 秒；每份资源独立关闭临时 Box。新鲜/未引用规则没有请求。失败、取消、过期
  generation 保留旧副本，新的有效内容原子落盘，下次连接生效。返回仅更新/失败/延后
  计数；停止连接取消并等待刷新退出，不重启流量核心。
- 编译契约测试验证 DNS detour 与资源 outbound 相同、不含系统入口、原 Profile 不变、
  协议会话租约缺失时拒绝。最初使用不支持的 SOCKS Line 测试被模型拒绝，改用已有
  Shadowsocks 类型；没有因此放宽配置模型。
- 本地 Shadowsocks-none 协议 fixture 仅接收并关闭连接，不向外转发：实际读到资源 DNS
  的目标 `1.1.1.1:443`，保护回调被调用；保护拒绝时本地监听器没有接受到连接。
  这证明指定代理/DNS/socket 边界，**不是代理 HTTPS 完整成功或真实 VPN 出口验收**。
- 测试覆盖刷新网络失败、无效内容、协议延后、取消/过期 generation 保留副本，
  新鲜/未绑定规则零刷新，以及实际 Stop 与阻塞保护回调的退出竞争。Android 定向 race
  通过；config/libbox/mobile/subscription 常规全量、public-content-gate 通过。
- 双 ABI AAR/APK、iOS/tvOS generic 模拟器、macOS Debug/Release App/扩展构建通过。
  没有新增 XCTest 执行记录。隔离 Android 设备曾自动关机，重新启动同一设备后安装；
  不使用 adb。暖缓存连接后的刷新报告 updated=0/failed=0/deferred=0；准备 socket 0、
  总保护 2、失败 0；普通未 protect App socket 的 UDP DNS 与 HTTPS 再通过。
  过期下载/缓存替换/取消由核心测试验证，未伪称模拟器实测了过期缓存的网络刷新。
- 最终 APK SHA-256：`3e09b983b5e81b698ee936b23e28647ddf6347bfd5d65705b9dd2f30182dd2f4`，
  插件 matchesLocal=true；设备已手动断开。日志为 `build/android/acquisition-*`。
  尚未推送、PR 或分发；仍不是完整 Android 产品或全平台交付。

## 2026-11-17 共享来源库与导入预览

- Android 原单份 Profile 改为共享 Go schema 3 来源库：来源只持线路/规则，组/场景全局
  持有。预览与更新不发起网络请求，旧配置来源、场景和离线缓存保持独立。
- 核心定向 race 覆盖旧配置与未知字段保留、源身份、预览不激活、过期预览拒绝、旧控制器
  写入拒绝、连续场景选择提交、全局场景不被来源刷新覆盖、移除依赖拒绝、部分条件与顺序
  丢失拒绝、空组仅可存储不可运行、组重排保留原出口、同服务器本机凭据保留、Tailscale
  来源隔离/旧匿名来源身份命名空间。已有全量 libbox race 的基线时限问题未据此宣称解决。
- `PreviewNativeProfileImport` 仅接 XDial/sing-box JSON（最多 2 MiB）；remote URL
  RuleSet 保留声明。用不可达 `.invalid` 来源完成预览，旧 provider-expanding 文本入口
  被拒绝。当前不含在线订阅更新、Clash/Surge 文本导入或后台来源刷新。
- 新 Android `ProfileSourcesActivity` 提供原生来源列表、粘贴/系统文件入口、预览、
  保存、来源更新、编辑来源与移除；包含未知/不支持规则的预览不提供保存按钮。
  连接中来源只读。输入不写 Bundle/视图保存状态，窗口 FLAG_SECURE；源内容无日志。
- 实际设备仍为 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B`：启动后读取旧 One/Two
  和已有公开 SRS；增加无秘密 Direct 来源 Travel，预览 1 线路/0 规则后保存，返回仍选
  One。手动选 Travel，更新来源为 1 线路/1 手动规则，并改传入模板名；全局 Travel
  名称与选中状态保留。结束进程重新启动后仍选 Travel，证明写入可重读。
- 系统 stylus 首次输入引导截获了初次粘贴；截图确认后通过其 Cancel 退出，再读回正确
  输入继续测试。没有将输入工具的 executed 当作已写入文本。此次来源成功导入走粘贴；
  新来源页面的 SAF 成功导入尚无独立实机证据（前阶段离线 SRS 页面 SAF 已实际通过）。
- 最终包再次执行 One → Travel → One 连续手动选择成功；One 连接后普通未保护 App
  socket 的 UDP DNS 响应 ID/NXDOMAIN 与 HTTPS 200/正文再次通过。提交后刷新
  updated=0/failed=0/deferred=0。此前本阶段 Travel 的 Direct 数据面也实际通过。
- Android 真实 Keystore/AtomicFile 隔离验收：在唯一非秘密 fixture 目录创建旧加密
  Profile 及 `.bak`，迁移、编辑、重新读库，比较旧主文件和备份字节均未变化；翻转新库认证密文后读取拒绝，
  没有回退到仍存在的旧文件。fixture 在 finally 清理，不触碰用户库。身份安全存储的
  持久化、记录隔离、篡改拒绝、删除也通过。日志 `build/android/library-final-device.log`。
- 最终 `library-race.log` 定向 race、`library-core-tests.log` 的 config/libbox/mobile/
  subscription 常规全量与 public-content-gate 通过。双 ABI AAR（arm64-v8a/x86_64）
  与 APK 构建通过；运行只覆盖 arm64。最终核心变更后 iOS/tvOS generic 模拟器、macOS
  Debug/Release App/扩展六次构建通过（`library-apple-builds.log`）；本阶段未执行 XCTest。
- APK SHA-256：`df7596e3cc82c0c55730f3c44b0b01fc65578727e375ccb0a2c7a1c77f36e6c7`；
  插件 matchesLocal=true。最终已手动断开并读回未连接，仍选 One；网络正常。
  路径仍为 `android/app/build/outputs/apk/debug/app-debug.apk`。没有推送、PR、正式签名
  分发、宿主安装重启或 iOS 自动化修改；真实企业认证与 Tailscale 出口仍未验证。
