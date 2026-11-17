# Android 协议套接字边界

2026-11-17。此记录区分保护契约、模拟器交互与真实企业 VPN 验收。
测试服务器、账号和密码均为本地 fixture；没有使用生产凭据或改变宿主网络。

## AnyConnect

| 路径 | 原有缺口 | 实现与证据 |
|---|---|---|
| 认证 TLS / CSTP | `auth.InitAuth` 的 net.Dialer 没有 Control | Android Libbox 为 VPNClient 提供平台保护回调；TLS 在 connect 前执行；CSTP 复用同一 TLS 连接 |
| DTLS | `dtls.Dial` 内部直接 ListenUDP | 使用带 Control 的 ListenConfig，再把未连接 PacketConn 交给 dtls.Client；创建失败关闭 fd |
| 延迟 DTLS / 重连 | 运行时配置为进程全局，旧会话不能读取新会话钩子 | ConnSession 在建立时捕获 SocketControl；已有能力的重连继续使用其 VPNClient |
| 服务端 DNS | Android TUN 存续时默认 Go resolver 会进入自身隧道 | 共享 StartAndroidSession 选择活动 VPN Line；独立无 ingress 的 sing-box Direct DNS Box 使用捕获的系统 DNS 和同一保护回调；返回数字 IPv4，保留原主机名作 TLS SNI |
| Android 接口发现 | sslcon CLI 的接口探测依赖 Android 沙箱不允许的系统访问 | Android 使用宿主已提供的网络事实，不再执行 sslcon 的 GetLocalInterface |

Apple 的 AnyConnect 保持原 provider/Underlay socket 行为，不因 Android 适配增加接口绑定。

持久补丁：`patches/sslcon/0003-platform-socket-control.patch`。没有修改 third_party
上游快照；构建脚本按补丁生成依赖。Android 缺保护回调或数字拨号地址时拒绝连接。

验证：TLS 保护拒绝保留错误；DTLS 保护拒绝时本地 UDP 接收端未收到包；允许保护后
接收到真实 DTLS 握手数据报。以上是本地协议边界测试，不是企业 VPN 登录成功。
bootstrap DNS 的保护拒绝与错误脱敏经过 Go race 测试。现有 AnyConnect/核心测试通过。

## Tailscale

| 路径 | 审计与适配 |
|---|---|
| 控制面 HTTP / TLS、DERP TCP | tsnet 的 endpointDialer / HTTPClient 使用 sing-box；控制面 LookupHook 保持 sing-box DNS 所有权 |
| magicsock UDP、STUN / netcheck、端口映射 | netns.Listener 优先使用所属 NetMon 的 XDialSocketControl；在任何数据包之前执行 protect/bind |
| DERP 的 always-direct 和 HTTP(S) proxy | Android scoped dialer 仍回到所属 sing-box Dialer，避免裸 net.Dialer 与默认 DNS；HTTP/HTTPS proxy 的拒绝测试覆盖两条路径 |
| 多端点 / 候选退休 | Android 每个 endpointDialer 持有自己的 PlatformInterface、NetworkManager；NetMon 使用其接口事实，NetNS 使用其保护方法。Android 不注册/清空全局 interfaceGetter/control/listener 钩子 |
| 原始 disco socket | Android netns.UseSocketMark=false，现有代码在 AF_PACKET / 自测 UDP 创建前拒绝此路径 |
| Cloud metadata 探测 | cloudinfo 构建标签排除 Android；dnscache cloud resolver 在 Android 返回禁用 |
| HTTP-only netcheck | 仅 js/tamago/指定 plan9 分支执行，Android 不进入裸 http.DefaultClient 路径 |
| netcheck DNS | magicsock UseDNSCache=true，显式传入含 LookupHook 的每连接 resolver，禁止其 fallback 到默认系统 DNS |

持久补丁：`patches/tailscale/0010-endpoint-scoped-android-transport.patch` 与
`patches/sing-box/0012-endpoint-scoped-android-transport.patch`。

测试证明：两个 monitor 的网络事实互不串用；关闭一个并清空旧全局钩子后，另一个
仍执行自己的保护；缺失保护拒绝发包；DERP proxy 不绕过所属 Line。
Android scoped adapter 只在 Android 端点中启用；Apple 保留既有网络事实入口。
DERP proxy 改走 NetNS 的保护入口同样适用于其他平台。

**后续已接入**：Android setup 通过编译器生成捕获系统 DNS 的受保护传输，不创建 TUN。
登录与在线出口选择复用 Go/LocalAPI，setup 和正式会话串行共享加密身份，已解除
UI 的 Tailscale 拒绝。已在隔离模拟器到达真实登录页，未完成 tailnet 注册或出口联调。
持久状态见 `sing-box/0013`；Android 跳过明文 logtail 缓冲见 `tailscale/0011`。

## Android 账号表单与验收

- 原生表单接入共享 `AddAnyConnectScenario`，追加线路/场景，保留已有规则与未知字段。
  保存设置为手动选择，不启用跳过证书校验。返回文档含凭据，只交给 Android 加密存储。
- 表单隐藏密码、关闭该字段自动填充，在凭据窗口启用 FLAG_SECURE，关闭后清空输入。
- 隔离 Android 36 模拟器保存 `VPN fixture`（保留已有 One/Two），对 `.invalid` 服务器
  的尝试按预期失败；切回 One 后真实 UDP DNS 与 HTTPS 再次通过。没有把该错误路径
  当作 AnyConnect 登录成功。
- AAR/APK 已实际构建 arm64-v8a 与 x86_64；只在 arm64 模拟器执行安装/交互。

尚需：授权隔离企业环境下的认证/CSTP/DTLS/重连；Tailscale 已认证出口流量验收；远程规则
获取在已有 TUN 下的专用准备事务（不能让普通宿主 HTTP socket 递归进入尚未启动的核心）；
离线规则的原生导入/缓存 UI；连接中场景事务；事件驱动分发。此文不宣称全平台完成。


## 可复现检查

```sh
export PATH=/path/to/go/bin:/path/to/gomobile/bin:$PATH
make test-patched-tailscale test-patched-sing-box test-patched-sslcon
GOWORK="$PWD/build/patched-go/xdial.work" go test -race ./core/mobile
GOWORK="$PWD/build/patched-go/xdial.work" go test -tags with_gvisor,with_utls ./core/config ./core/libbox ./core/engine
scripts/build-android-core.sh
android/gradlew -p android assembleDebug
```

需先按计划设置 JAVA_HOME / ANDROID_HOME。协议补丁测试与核心测试本轮通过；
Android 安装包已在插件管理的 arm64 隔离模拟器核对本地指纹。
iOS、tvOS 与 macOS Debug/Release 构建回归通过；没有安装或重启宿主 XDial。
macOS 909 项测试的执行证据见上一共享控制器检查点，本轮未重复宣称 iOS XCTest 已执行。
