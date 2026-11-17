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

## 2026-11-17 固定构建、版本与签名产物

- `scripts/build-android.py --mode ci` 本机连续两次重建双 ABI AAR、Debug APK 和
  unsigned Release APK；两次 APK SHA-256 相同。APK 实际元数据、API 26/36、
  双 ABI、debuggable、签名和 16 KiB ZIP 对齐通过。跨机器字节复现未验证。
- Debug SHA-256：`1bad91ef3d37ceb0e9d38da85da6590933815eb8c8bad8d125e1a45fcff0dc6c`。
  Release unsigned：`78091098701b6721b3e07fdceaa7ea101b2ce6ac796663c17b3fa931688931b6`。
  AAR：`3065ecc2fff7562786fd88e8ea007dcc4567bc54377531fd9223c2472471a59b`。
  Debug 可安装；unsigned Release 不能安装，也没有冒称长期签名测试版。
- 产物目录 `build/android/artifacts/xdial-1-debug-1bad91ef3d37-928e6c7b6f65/` 与
  `xdial-1-unsigned-78091098701b-69db0515cdd3/` 含 APK/manifest/SHA256SUMS。
  manifest 如实记录源码基线 `09ea457` 与 dirty=true（本轮构建代码当时未提交），
  不可反写为未来提交或 clean。目录同时绑定 APK 和来源清单摘要。
- `scripts/tests/test_android_build.py` 五项通过（`build/android/package-tests.log`）：
  CI 版本单调/边界、完整证书指纹、产物不覆盖及真实 keytool/apksigner 流程。
  一天期临时证书仅在隔离目录生成，成功签名后验证错证书拒绝与修改 APK 后签名失败；
  退出删除，没有作为正式身份保留或分发。
- 插件确认模拟器 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B` 实际安装指纹与上述
  Debug APK 相同（matchesLocal=true），版本 0.1.0-dev/code 1。来源库 One/Travel
  保留，选 One；重新连接后 `XDialRuleRefresh updated=0/failed=0/deferred=0`，
  普通 App socket 的 UDP DNS 与 HTTPS 检查均通过。最终手动断开，UI 读回未连接。
  本轮运行仅 arm64；x86_64 仍只有构建/装包检查。
- CI YAML 结构读取与 public-content-gate 通过；CI 尚未推送/执行，没有长期证书或
  内部渠道发布证据。本轮只改 Android 打包/版本和文档，不新增 Apple 运行或 XCTest
  证据；上一节六次 Apple 构建仍是最近一次核心回归。宿主 VPN/iOS 自动化未改变。
- 按用户要求核查 Cindy/火炬 2 既有签名方式，结论与接入边界见 android-release.md。
  不把公开仓库 artifact、Cindy 的空渠道模板或火炬 2 的 Play 上传密钥当作 XDial
  已完成的受限测试分发；不要求用户再提供 Tailscale 测试环境。

## 2026-11-17 地区观察、自动条件编辑与连续重连

- 新共享条件编辑器保留来源库/全局场景/未知字段，验证 SSID 长度、跨场景冲突与地区
  条件冲突；真实库修订号提交拒绝过期编辑。手动再次选择当前场景也关闭自动选择，
  使迟到调查失效。SSID 优先、无地区映射或手动覆盖不发起地区查询。
- `ProbeAndroidUnderlayCountry` 使用独立 sing-box Direct 查询已有固定地区服务，
  系统 DNS 来自捕获快照，所有 socket 必须 protect/bind。没有 TUN、用户流量规则或
  任意 URL 参数；最多 6 秒，未知结果保留场景，不作为 Direct 就绪的必要条件。
  阻塞保护回调/Stop/取消、重复调查及保护失败拒绝均有定向 race 测试。
- 连接前与网络恢复准备期间读取地区；网络回调的事实代数或 LinkProperties 变化使
  旧调查失效。尚未把已连接时地区变化接到无缝 Switch，不能称实时自动切换完成。
  核心也开始拒绝含 TUN 的 `PrepareSwitch` 候选，测试确认拒绝前没有打开第二个 TUN，
  当前 Box 不变。后续需独立稳定入口承接 generation，不能复用 fd 启动两个 gVisor。
- 隔离模拟器 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B`：原生条件对话框给 Travel
  保存 `*`，覆盖安装/重启后重新打开仍为 `*`。从 One 开启自动后连接，实际查询返回 NZ，
  UI 与运行选择为 Travel；日志 queried=true/found=true/protected=2。普通未保护 App
  socket 的 UDP DNS ID/NXDOMAIN 与 HTTPS 200/正文通过，后台规则刷新 0/0/0。
- 断开后再次手动点选当前 Travel，随后连接仍为 Travel，日志
  queried=false/found=false/protected=0。原先手动切回 One 也验证不查询。条件冲突、
  过期编辑和 SSID 优先覆盖在核心测试；没有声称模拟器已切换真实 Wi-Fi SSID。
- 连续重连暴露旧入口问题：应用 TUN 描述符已经归零，系统仍短暂返回旧默认 VPN，
  入口立即失败。修复为订阅默认网络回调后重新读取事实，无 VPN 才申请 Underlay；
  20 秒上限与手动停止清理回调/计时器，不轮询或覆盖其它 VPN。最终包实测
  released(fd=0) → waiting-vpn-release(fd=0) → 新连接成功；一次等待约 2.48 秒，
  同代码另一轮重连后真实 UDP DNS/HTTPS 再通过。持续其它 VPN 的超时与等待中取消
  尚无单独设备证据。最终断开两次清理均 fd=0，UI 未连接、选 One，自动已关闭。
- 最终 APK SHA-256：`c3754ac22c953858f09bef1a8713594892f0f73f23f00248e61933e7eb8da2ce`，
  插件 matchesLocal=true。不可变目录
  `build/android/artifacts/xdial-1-debug-c3754ac22c95-771b8d423e8a/` 含 APK/清单；
  源码清单如实为 `8a961e8` + dirty=true，不回写未来提交。双 ABI 均构建并检验，设备运行
  仅 arm64；仍是开发签名，未发布到内部渠道。
- `country-tests-final.log` 的 mobile/Android country/TUN guard 定向 race 和
  `country-core-tests-final.log` 的 config/libbox/mobile/subscription 常规全量通过；
  `country-apple-final.log` 为最终 Go 变更后的 iOS/tvOS generic 模拟器及 macOS
  Debug/Release App/扩展六次构建通过。之后只改 Android 的系统释放等待和诊断日志。
  本轮未执行 XCTest，也未解决此前全量 libbox race 的基线时限问题。
  Android 构建日志 `country-build-release-wait.log`，设备收据 `country-device.log`，
  public-content-gate 与 diff 检查通过。无宿主安装/VPN 改动、push/PR 或 iOS 自动化修改。

## 2026-11-17 稳定入口与连接中场景事务

实现：`core/libbox/packet_ingress.go` 独立拥有唯一 sing-box TUN/gVisor 入口；运行
Box 和候选 Box 只拥有完整 DNS/路由/协议配置。`PrepareAndroidSwitch` 在保护的
Underlay 上准备规则、校验相同系统 TUN 设置，然后移除候选 TUN。提交原子更新新流
指向，旧 TCP/UDP/native ICMP 按 generation 登记，默认 1 秒关闭；下一候选先退役旧源。
DNS 在同一 generation 的 router 内裁决，已退役的异步响应不再写入 TUN。并发 close
等待借用者退出，迟到 ICMP tracker/AttachFlow 不能借用已关闭 router。TCP/UDP 在
此模式走完整 flow router，未保留原生 PreMatch 快速转发优化；需另测实际性能/功耗。

共享 Line 能力池用于 Android AnyConnect/Tailscale 数据会话。Tailscale 原生 DNS
单一 hook 属于能力 Box，消费者仅借用，不关闭/重置身份 owner。能力 DNS 保留
`auto_detect_interface` 保护路径，Android 对 local/非数值系统 DNS 拒绝准备。
原有 Apple 入口未开启稳定 packet ingress，已有 flow/relay 事务保持。

Android 宿主只处理网络事实、资源生命周期和加密存储；候选选择仍由共享 Go 控制器
生成。原生选择可在连接中发起；持久化失败不提交，提交失败回写原选择，无法回写则
停止连接。来源库没有被候选修改。就绪时切换，再排空旧源。独占协议身份变化由 Go
分类后保存目标并完整重连；这项只有契约测试，未用真实两套企业身份验证。
停止/新意图可取消准备，网络修订变化使候选过期。背景规则刷新在候选开始时取消，
避免提交等待一个无关的 60 秒获取任务。

验证证据（不混作真机/真实企业 VPN）：

- `packet-race-final.log`：Packet/Android/PooledTailscale/Switch/Stop 定向 `-race`
  通过。真实 sing-box + 内存 TUN 注入原始 DNS 包证明候选准备仍用旧 DNS、提交改用
  新 DNS、退役不关闭入口且只打开一次；本机 TCP/UDP echo 证明旧流提交后继续、退役
  后关闭；ICMP 用原生 FlowTracker 契约替身证明退休/迟到/并发挂接，无真实 ICMP 出口证据。
  Android 生成配置候选证明准备返回不取消候选、退休源不取消新 runtime，取消候选后
  不允许提交。原全量 libbox race 的基线时限问题不在本次通过声明内。
- `packet-core-final.log`：config/libbox/mobile/subscription 常规全量测试通过。
  `packet-apple-final.log`：iOS/tvOS generic 模拟器和 macOS Debug/Release App/扩展
  六构建通过；本次没有新增 XCTest 执行证据。未安装/重启宿主 Debug 或修改宿主 VPN。
- `packet-package-final.log`：固定工具链构建双 ABI AAR 与 Debug APK 成功。最终 APK
  `build/android/artifacts/xdial-1-debug-32b4bc4aef96-5a6d1d4a40b6/xdial-1-debug-32b4bc4aef96.apk`，
  SHA-256 `32b4bc4aef969fcc2f238c5fcabe12afc4e9cf1e32520374d25cdcfe8e10753a`。
  来源清单如实记录 `862c801` + dirty；不能改写为阶段提交后 clean。插件安装指纹
  `matchesLocal=true`。仍仅 arm64 模拟器运行，未验证 x86_64/安卓真机。
- 隔离模拟器 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B` 上使用原生点击，One/Travel
  连接中来回切换。`switch-preparing`、`switch-committed`、约 1 秒后的
  `switch-retired` 均为 2 个 TUN 描述符（Android 所持 fd 与 Go 副本），没有再建立
  TUN；单读者另由核心 opener 计数测试证明。切换后普通未 protect 的应用 socket
  UDP DNS ID/NXDOMAIN 与 HTTPS 200/正文检查通过。
- 首次设备检查暴露快速提交时界面仍显示旧名字：500ms 状态轮询错过瞬时准备状态。
  已改为选择修订号通知，修正包 Travel→One→Travel 名称立即更新，重新启动仍保留
  已提交选择。不是用同名出口掩盖 DNS 代际测试，核心使用两个不同 hosts 回答验证。
- 新增无秘密 `Switch failure` fixture（`.invalid` AnyConnect 服务），连接 One 时
  选择后显示准备失败、仍选 One；随后普通 UDP DNS/HTTPS 都通过。新增 `Switch wait`
  fixture（文档保留地址 `192.0.2.1`）用于准备中点击断开：最终未连接、选择仍 One、
  TUN 描述符 0，没有迟到复活。拨号仍受 sslcon 现有 6 秒超时约束；不声明即时取消。
  后来再选 Travel 成功，但工具调用间隔已经超过前一个拨号超时，因此不将此作为
  “旧候选仍 pending 时 latest-wins”设备证据。
- 最终读回未连接、选择 One、自动关闭、TUN 描述符 0；网络未修改。两项无秘密失败
  fixture 留在独立模拟器，未写入真实用户配置。插件收据 `build/android/packet-device.log`。

边界：这不是完整 Android/全平台交付。还缺连接中自动选择、完整场景编辑与在线
订阅来源、AnyConnect/Tailscale 冷资源获取租约、受限签名分发及 Windows/Linux 实际
接入。真实已认证协议复用、不同身份完整重连、真机 ICMP/性能、存储故障回写和系统
撤权路径需继续补验，不借本机替身结果冒称已验证。

最终 Java 收敛还保留同场景 no-op 的旧流排空计时器，并在完整重连清理失败时准确
显示已无连接；该改动后重新打包、覆盖安装，One 同场景手动选择、One→Travel→One
及普通 UDP DNS/HTTPS 再通过。最终 APK 与前一次失败/取消分支测试包核心相同；
失败/取消设备证据来自前一包 `0ef63d2de4e3`，不将未重跑分支改写为最终包全量验收。

## 2026-11-17 场景编辑产品闭环

实现：`core/mobile/scenario_editor.go` 及 gomobile facade 提供共享编辑命令和安全展示
模型；`ScenariosActivity` 使用原生对话框与列表编辑内存草稿，复用共享条件编辑器。
配置库保存仍校验修订号，线路/规则归属来源，场景归全局。移动出口不改变部分匹配；
交错顺序转换和完整规则扩展是两项明确操作。当前/自动条件引用场景不能删除。

验证证据：

- `build/android/editor-mobile-tests-final.log`：mobile 全量 race 通过。覆盖部分引用、
  交错顺序保护、绑定移动/移除、显式完整范围、默认出口、创建/删除、源资源保留、库
  CAS、未知字段、不含资源内容的展示、禁用出口、命令尾随内容及空闲线路组。
  补充测试最初误把空线路组写成空规则组，按真实 schema 修正 fixture 后通过；没有
  为通过测试放宽空规则组的约束。
- `build/android/editor-core-tests.log`：config/libbox/mobile/subscription 常规全量通过。
  不声称全量 libbox race 通过，前述基线时限问题仍保留。
- `build/android/editor-apple.log`：iOS/tvOS generic 模拟器以及 macOS Debug/Release
  App/扩展六项构建通过。无新增 XCTest；没有安装或重启宿主客户端。
- 打包入口执行 public-content-gate、双 ABI AAR、Gradle 和 APK 身份检查；最终结果
  `build/android/editor-package-final.log`。最终 Java 仅进一步串行化场景控制器替换。
- 插件设备 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B` 上新建非秘密 Direct 场景，
  重命名、添加 Public cache fixture/Local 两条绑定并上移 Local；保存并重启后顺序
  保留。移除绑定后按系统返回键，选择放弃，重新打开仍是两条。连接期间尝试新建被
  拒绝；断开后删除当前场景仍被拒绝，切回 One 后可删除并保存。输入插件的清空/粘贴
  未产生预期英文名，实际保存名为 `Editor tripEdi`；以上验收按实际读回名称执行，
  没有把输入请求当成输入结果。
- 上述完整交互来自 `7d4c9d96e714` 包；最终 `db424e8cb6cf` 包再次创建保存
  `Editor final`，选择并连接，普通未 protect App socket 的 UDP DNS 响应 ID/NXDOMAIN
  与 HTTPS 200/正文检查均通过。刷新计数 0/0/0。最后手动断开、选 One、自动关闭，
  日志 `phase=released tun_descriptors=0`。网络未修改。最终非秘密新场景留在该隔离
  设备内；真实企业和已登录 tailnet 联调仍未验证。
- 完整插件收据：`build/android/editor-device.log`。设备安装指纹确认 `matchesLocal=true`。

最终可安装 Debug APK：
`build/android/artifacts/xdial-1-debug-db424e8cb6cf-4426e1b299a9/xdial-1-debug-db424e8cb6cf.apk`

SHA-256：`db424e8cb6cf386375ea8046c5387de140fc09dff4ccb74b49b93ec98a67355b`。
manifest 保留真实 `aaebca5 + dirty=true`，不改写为后续提交。AAR 包含 arm64-v8a 与
x86_64，仅 arm64 设备运行。尚未推送，GitHub CI/长期签名/受限渠道未验证。

此阶段补齐场景编辑入口，不代表完整平台交付。在线来源、连接中自动选择、协议冷资源
租约、取消延迟、系统撤权与存储故障分支、Windows/Linux 按应用透明和多发行版仍需继续。

## 2026-11-17 在线来源下载、预览与更新

实现 `FetchAndroidProfilePreview` 的独立准备租约：捕获系统 DNS/接口/protect+bind，
复用精确 Line 资源编译；不建立 TUN、不展开远程 provider。HTTPS 逐跳校验、全 DNS
地址校验与数值拨号复用严格获取器，新增源 URL 长度/用户信息/片段校验及重定向同约束；
下载最多 2 MiB，响应失败、超限或取消不能产生可保存预览。配置提交仍需旧库摘要 CAS。
`ProfileSourcesActivity` 增加原生链接/线路选择、取消下载和原链接刷新；URL/预览不写
Intent、Bundle 或日志，只进入内存及最终确认后的加密库。

测试：

- `online-race-final.log`：RemoteProfile/RemoteSource/ProfileSource/AndroidProfileSource
  定向 race 通过。覆盖来源地址/线路不可替换、获取线路引用保护、文件不能伪造在线来源、
  全局场景保留、预览 CAS、保护拒绝/准备租约取消。HTTP 测试通过可信本地 TLS 服务器
  和注入的数值拨号回路验证成功、同站点重定向、私网/用户信息重定向拒绝、超限、非 2xx、
  响应等待中取消；没有外网转发，不冒称真实代理端到端。最初拨号次数断言未计入连接复用，
  已修正为实际 6 次（同站点重定向复用连接）。
- `online-core-final.log`：config/libbox/mobile/subscription 常规全量通过。最初命令漏
  `build/tools` 导致 sing-box check 无工具，补齐 PATH 后重跑通过；不算代码测试通过。
- `online-apple.log` 六 Apple 构建通过；补充 Apple 显式获取线路保护后
  `online-macos-final.log` 再次完成四项 macOS App/扩展 Debug/Release 构建。
- `online-source-xctest.log`：macOS 1 项专项 XCTest 通过，验证新字段往返、旧 JSON
  兼容和旧获取器拒绝显式线路。无新增 iOS XCTest，也不把该测试算成全量 macOS 测试。
- `online-gate-final.log` gate 通过；`online-build-final.log` 双 ABI AAR/APK 构建与产物
  检查通过。首次 Android 编译使用了 SDK 未开放的 LinkProperties 拷贝构造器，修为
  捕获系统回调快照后构建通过。

设备：插件独立 `16BAF5BD-2097-4906-9463-6A8D9E10FC9B`，arm64 API 36。

- 从 `https://raw.githubusercontent.com/xindong/XDial/main/docs/examples/profile-next-company.json`
  下载无秘密示例 `Online sample`，指定 Direct。先显示 2 线路/1 规则/2 场景预览，确认
  后库由 6 场景增为 8，当前仍 One；原链接更新显示保留全局场景，重启后仍 8 场景。
- 下载日志 `prepared=true protected=2 failures=0`，清理时 TUN 描述符 0。所含示例
  公司服务器未认证也未连接，不能据此称企业协议联调完成。
- `https://192.0.2.1/config` 立即失败，故不把它当作中途取消证据。实际取消测试是在
  设备断网后更新已有来源，等待网络时点取消；恢复网络后读回“已取消下载，原配置保留”，
  没有复活。响应读取中取消的证据只来自本地 TLS 测试。
- 完整新增/取消交互包为 `c440239a1646`；最终包仅收敛页面销毁时取消服务调用的异常。
  最终 `59e7501d81d3` 包再次原链接更新/保存，返回 One 连接，通过普通未 protect App
  socket UDP DNS 响应 ID/NXDOMAIN 与 HTTPS 200/正文检查；刷新 0/0/0。最终手动断开、
  One、自动关闭，`phase=released tun_descriptors=0`。设备网络已恢复。
- 收据 `build/android/online-device.log`；最终安装 `matchesLocal=true`。

最终 Debug APK：
`build/android/artifacts/xdial-1-debug-59e7501d81d3-ed302584252b/xdial-1-debug-59e7501d81d3.apk`

SHA-256 `59e7501d81d3caa2fc0f748f9aeca0e32ea38b287050f3c30b7870f6f504a42b`；
manifest 如实为 `287cf77 + dirty=true`。arm64-v8a/x86_64 均打包，仅 arm64 运行。
无推送/外部分发，不改宿主 VPN 或既有 iOS 自动化。

仍缺：非 JSON 在线格式、AnyConnect/Tailscale 冷资源租约、连接中自动观察、Apple 显式
Line 来源获取、长期签名/受限渠道、Windows/Linux 按应用透明与多发行版真实验收。
