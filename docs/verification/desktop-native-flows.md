# Windows/Linux 共享核心与原生流入口

2026-11-17；父提交 `833bbb7` + 本轮源码。此阶段已在两台原机运行共享核心，
不是只有交叉编译；但系统级应用拦截、权限安装和原生窗口仍未完成。

## 交付的代码边界

- 移除 libbox 生产实现的整体 Windows 排除，保留 gVisor、uTLS、移动端 Tailscale
  等特性条件。协议、场景、资源库和事务继续共用 Go；没有新建 Windows 路由引擎。
- Unix TUN fd 的复制/关闭移至 `platform_tun_unix.go`。Windows 明确拒绝将 Unix
  fd 当作 HANDLE 使用，不隐式新建 Wintun。原生 flow 接入不需要该 fd 路径。
- 身份运行锁分为 Unix Flock 与 Windows LockFileEx；共享层保持排他、释放后重入、
  冲突拒绝的语义。Windows 路径使用 JSON 正确转义，原机已验证锁竞争与释放。
  这里验证的是空测试状态目录，未验证真实 Tailscale 身份登录和出口。
- 桌面 Go API `SetNativeFlowIngress` / `RouteNativeTCP` / `RouteNativeUDP` 只接收
  原始端点和可信 OS 观察到的进程事实。输入没有出口/DNS/路由字段，未知身份交由
  sing-box 原搜索器查找。`UserIDKnown` 区分未知与真实 UID 0。此 API 不是可供
  普通 IPC 客户端自报身份的协议，必须由原生适配层提供事实。
- 原生流入口复用已有稳定入口的 generation、提交和退休机制：新流在同一提交点
  切换 DNS 与 route，旧流保持来源代，退休关闭旧流，Stop 禁新流后清理所有代。
  流模式的初始/候选配置不得创建 listener；与独立 TUN 模式互斥。
- 接流方法可能在 sniff/DNS 会话中等待输入，适配器必须在每流 worker 上执行，
  不持系统回调锁。关闭回调应投递生命周期事件，不能同步重入 Stop 等等待本流退出的操作。
  本轮 API 仅编入非 Android 的 Windows/Linux，Apple UI 和 Android 模式保持原入口。

## 旧命令服务的鉴权收敛

Linux `peerAllowed` 改用内核 SO_PEERCRED，只允许与 daemon 有相同 effective UID
的进程；普通连接、已关闭连接、非 Unix 连接的行为经原机测试。没有跨用户登录测试，
不声称已验“另一个真实用户”的拒绝。以 root 运行时只允许 root，不默认授权任意
桌面用户；未来权限服务必须有安装事务确定的调用者策略。

其他无已实现凭据验证的平台改为拒绝，移除原先 `return true`。Windows 的原生
named pipe、token/ACL 与服务安装仍需实现。旧 CLI 的注册交接代码依赖 macOS/Unix
API，本轮尝试完整 Windows CLI 构建仍失败；没有用恢复退役数据面命令来绕过它。
因此本轮交付的是共享核心及接流契约，不是 Windows 可安装完整客户端。

## 原机证据

本机 Go 1.26.5 使用 `with_gvisor,with_utls` 构建测试可执行文件；Linux CLI 凭据
测试单独构建。压缩产物和源码清单通过登记测试机传输，任务目录内解压并核对
SHA256 后原生执行。没有安装驱动、提升权限、修改系统 DNS、路由、VPN 或 CI。

| 原机 | 结果 | 测试可执行文件 SHA256 |
|---|---|---|
| Windows 10.0.26300 x64 | 9 项通过，原生回执与 result 均 0 | `9c77dc84ed4443ef33e0c6a67f65f5aab3d4ea6540eb63d50a5cadedb8603883` |
| Ubuntu 24.04 x64 | libbox 11 项 + CLI 凭据 1 项通过，原生回执与 result 均 0 | libbox `407bf632241065f3af684911a5dcd0caa87902cda03b0c19b8ecc942951fd8e7`；CLI `b4983e0eec5c16f11493345f2ccaf768c0028c134a55e9bf44cefbc4072aee0a` |

Windows 快照摘要 `2f3b57397498f730393e903c7b7026a5373d4ceab5033975af1f3727bcad89b0`；
Linux 快照摘要 `f483a8824f330e6d5e5bf73c87fb306616522fc6eb01fa18685161f266dd58c7`。
两份快照使用同一 `833bbb7` + dirty 源码清单，按平台只携带必要压缩二进制。
远端无 Go，未宣称原机源码重建。

实际覆盖：

1. 经 sing-box 转发到真实 loopback TCP / UDP echo；DNS 经同一 Router 返回受控 hosts 答案。
2. 候选准备期间保持旧 DNS；提交后 DNS 从 `192.0.2.10` 切为 `192.0.2.20`。
   既有 TCP 继续收发，新 TCP 按新进程规则收到明确的 sing-box reject 回调；
   不把读超时当作拒绝。来源退休关闭旧 TCP，新 TCP 仍可收发；Stop 关闭新流。
3. 互斥入口模式、运行时改模式、初始/候选 listener、缺少原始目标均拒绝。
4. 创建真实 TCP socket，**不提供 PID/程序路径**，让 sing-box 从系统 socket 表
   找到当前测试程序，匹配其真实路径后拒绝流量。Windows 与 Linux 均通过。
5. Windows / Unix 身份锁的第二次持有被拒绝，释放后可再次取得。

第 4 项的 OS 查询是真实的，但截获流仍由 `net.Pipe` 提供；其余场景切换测试的
应用路径是受控 fixture。UDP 使用内存 PacketConn 接流、真实 UDP socket 出口。
没有 WFP/nftables 系统拦截、没有真实浏览器透明流量、没有企业 VPN 联调。
不能把上述证据称为完整按应用透明接入或多发行版验收。

初次 Ubuntu 运行有一项 fixture 未设置默认接口而失败；补齐测试输入后全部通过。
初次 DNS worker 同步等待会话超时约 30 秒，改为每流 worker 执行并关闭后等待退出，
最终同一切换测试约 0.9 秒。实际核心没有通过缩短 DNS 生命周期来掩盖问题。

本地日志：`build/desktop/native-windows-final.log`、`native-ubuntu-final.log`，
配套 `*-final-result.json`。完整 fleet 回执保留本机；测试目录按各自 UUID 清理。

## Apple/Android 回归与剩余工作

- Windows 默认特性核心构建及 with_gvisor/with_utls 的 go vet 通过。
- config/libbox/mobile/subscription/CLI 常规测试通过；原有 packet/Android switch、
  flow、状态锁定向 race 通过。桌面专用 native flow 测试在原机普通执行，未冒称 race。
- iOS/tvOS generic 模拟器和 macOS Debug/Release App/扩展六构建通过；无新增 XCTest。
  后续仅修改 Windows/Linux 专用 API/测试，没有再声称重复 Apple 构建。
- Android 双 ABI AAR/APK 重建与签名/内容校验通过；APK SHA256
  `8aad35df15f5d111829de3d62c31f64dd939008f8044a9f6103433c6cc1c6cd6`，路径
  `build/android/artifacts/xdial-1-debug-8aad35df15f5-3d8157376b51/`。
  manifest 如实 `833bbb7` + dirty；此包未新增设备安装或交互证据。
- 系统拦截与防环路仍需原生适配：Windows WFP/命名管道/服务 ACL、Linux 透明入口/
  权限服务和多发行版安装。Linux 用户 namespace 当前受限，必须先准备隔离环境，
  不能直接改共享测试机网络。绑定 Underlay socket 的非 Apple 平台适配也需补齐，
  现有拒绝分支不允许静默绕过。
