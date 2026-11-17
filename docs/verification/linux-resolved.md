# Linux systemd-resolved 依赖与 split DNS 验收

2026-11-17。承接 Linux 权限服务和升级事务；本阶段接入实际系统 resolver，
没有安装或改变共享宿主的网络。它不是默认 NSS、Fedora/Arch 或全平台完成报告。

## 实现与边界

- `core/linuxresolver` 只读取系统事实，不配置 resolved、不激活服务、不提供路由策略。
  固定连接 root 保护的系统总线 socket，不读 `DBUS_SYSTEM_BUS_ADDRESS`。核对总线
  owner、PID 1 的 unit MainPID、非委托 cgroup、内核 `/proc` 身份和该进程实际拥有的
  UDP/TCP `127.0.0.53:53` socket。持有 pidfd 和 cgroup v2 handle，观察 owner/进程退出。
- `linuxcapture` 在现有 socket-owned nft 表中，仅给该 cgroup 的 output socket
  增加 return。ID/level 来自持有的内核租约，不在 Policy/Plan JSON 或用户 IPC 中；
  不豁免整个 UID/root/slice。停止先撤 capture，再停止核心和释放 resolver 租约。
- 标准 `/etc/resolv.conf` 链接必须指向 `/run/systemd/resolve/stub-resolv.conf`，
  声明唯一 `127.0.0.53`，系统 resolved 身份必须通过验证。Direct/代理服务器 bootstrap
  由共享编译器使用带 bypass mark 的 sing-box transport 查询这个 stub；代理应用 DNS
  保持精确 Line。不会读取并拍平 resolved 上游列表、切换默认接口或替换公共 DNS。
- resolved 保留 per-link 域路由和自己的缓存。此模式关闭盒内第二层 DNS cache，
  防止系统域路由变化后继续返回旧视角。这个开关也作用于同一 Box 的代理 DNS cache；
  目前接受额外查询成本，未另造平台 DNS 缓存裁决。
- `nss-resolve`/共享 nscd 会把原应用查询变成 resolver/cache 进程的请求。当前拒绝
  这些配置和未知 NSS hosts 模块，返回 `unsupported_native_resolver_ingress`；
  运行中 NSS/nscd 变化会撤销捕获。测试来宾显式选 `files dns`，产品不替用户修改 NSS。
  这仍需后续带内核调用者身份的原生 NSS 入口，不能永久缩减目标。应用显式调用
  D-Bus/Varlink API 的归因也未完成，不能用 DNS socket 证据冒称所有原生解析已接管。
- 解析器退出/身份变化断开连接，不自动重连。普通非 stub resolver 文件仍只接受
  单个非 loopback IPv4 nameserver；其他 resolver 实现、IPv6 用户入口仍待适配。

## 真实完整来宾

共享 Ubuntu 测试机普通用户的独立任务目录中运行 QEMU 8.2.2 TCG，2 CPU/2 GiB、
nice 10；无 NIC、KVM、宿主挂载或 SSH 转发。官方 Ubuntu 工具包按 APT 元数据 SHA
校验后仅解包，不在宿主安装。完整 Debian 13.7、内核 `6.12.117+deb13-amd64`、
systemd PID 1，使用官方 generic amd64 镜像：434831360 字节，SHA512
`e53a93ef33f828ceed0e7278c2cc2753bdc7d5098fbfae15bd64b6d878db350ea3744247763a1c8ecb6b4019a0f623c38caf6038ad6f9c357fb4e01493ddf1a3`。
验证是官方 HTTPS manifest SHA512，非 PGP 签名验证。

来宾内创建两对 veth 与两个独立 netns，只在来宾中配置真实 systemd-resolved 的
per-link DNS/domain。两个受控 UDP DNS 进程分别回答 `.10`/`.20`、TTL 60；它们是
上游测试服务，未替代 resolved、TProxy、内核进程查找或系统安装服务。原机无 Go，
本机 Go 1.26.5 交叉编译后在来宾真实执行，不能称远端从源码构建。

最终三个顶层测试全部通过：

1. NSS 声明准入：合法 wire DNS；拒绝 resolve、未知模块、重复 hosts、缺 DNS 和
   无效 action。真实来宾另验证 connect 拒绝 native NSS 配置。
2. 实际包 install/start，真实 resolved 租约；普通未标记 UDP/TCP DNS 发往没有
   服务的 `192.0.2.54:53`，由内核捕获并经 stub 得到 split DNS 答案。root、UID 1001、
   与 resolver 相同 UID 但不属于其 cgroup 的真实子进程均通过，证明没有 UID 全面豁免。
   连接期间交换两个网络的域路由，同名查询立即得到新答案；没有等待 60 秒 TTL。
   重启 resolved 后连接自动撤销，重新显式连接取得新租约。运行中切换到 `nss-resolve`
   或创建 nscd socket 会断开且拒绝新连接；恢复 wire DNS/移除测试 socket 后可重连。
   最后实际包 remove，无 capture journal 残留。此项 23.04 秒。
3. 完整已安装服务回归：首次安装不启动、重复安装不改 policy、普通 UID 1001
   TCP/UDP DNS；移除 `CAP_NET_BIND_SERVICE` 的负面对照只有 DNS 透明回包失败，
   恢复包内 unit 后通过；正常 restart、SIGKILL 自动重启恢复、连接中 remove 通过。
   此项 28.89 秒。NSS fixture 明确设为 wire DNS，不冒称默认 NSS 支持。

native 与 fetch/result 都为 0；`XDIAL_DISTRO_RESOLVED_OK`、
`XDIAL_DISTRO_SYSTEMD_OK`、`XDIAL_DISTRO_EXIT=0` 齐全。
第一轮真实 split DNS/重启与服务回归已通过，但尚无 NSS guard；保留为初轮证据，
最终结论只使用包含 guard 和动态拒绝测试的最终快照。

## 产物和来源

| 内容 | SHA256 |
|---|---|
| `build/linux-service/xdial-service-linux-amd64.tar.gz` | `c331af52990ae93a0f9849ae7e92b0097ada0c8d8132ac813f404cc0d954d922` |
| 包内 service binary | `140b1547844dcc536806d2cc202c7fed02cb9790ef18f9d9908fde7e2108185c` |
| 包内 unit（本阶段未改） | `3872e3ceedb3958b5196072cb98e6acfad39da15a4090e277caad12811e72705` |
| 最终 Linux 测试二进制 | `3cf5d34f2de56e9ca99d3fd4f09ab659f07fd5438a951a871fd7d93a6c911690` |
| source-manifest | `df40686d797c5be4014d033c565f0fafe8398984fc83c97d35665eaca8a26fab` |
| fleet snapshot | `14a2ac6b3a2f0c9da125555c3246dd56f42ff0b4ebcb38951348a3750f532992` |

来源 `2f4c23d` + dirty 如实保留，不回写后来的提交。最终受测服务与包内 binary 相同。
本地收据：`build/desktop/resolved-final-{native,fetch}.json`、
`resolved-final-{test,guest}.log`、`resolved-vm-packages.json`、`resolved-image.json`；
初轮 `resolved-initial-*`。来宾 overlay/ISO 在 QEMU 退出后删除，不保存测试系统密钥。

## 回归与未覆盖项

config/libbox/mobile/subscription 常规全量通过（`resolved-core-final.log`）；Linux
编译器 race 与 Packet/AndroidSwitch/KernelRoutedUnderlay 定向 race 通过
（`resolved-config-race-final.log`、`resolved-shared-race.log`）。Linux service/resolver/
capture/CLI vet 通过。没有 Linux kernel/DBus race 或全量 libbox race 的新结论。

iOS/tvOS generic simulator、macOS Debug/Release App 与扩展六构建通过
（`resolved-apple.log`）；无新增 XCTest/真机验收。Android 双 ABI 构建与签名产物
校验通过（`resolved-android.log`），APK 位于
`build/android/artifacts/xdial-1-debug-1478c2d9a030-3b4d9a6666a4/`，SHA256
`1478c2d9a030d3227e191fb3b15c3296d5b1b2a4759db8eb9681b02b5b4af93e`。
本轮未安装模拟器，不沿用旧的 matchesLocal/流量证据。

无公网/浏览器/企业 VPN/IPv6/物理掉电证据；DNS 上游在隔离来宾内部。没有运行新一轮
升级强杀矩阵，上一阶段升级证据仍见 [linux-upgrade.md](linux-upgrade.md)。Fedora/Arch
完整系统、原生 NSS 入口、Linux 协议租约与 GUI、Windows SCM/WFP，以及移动端和签名
分发缺口保持原目标。没有推送、发布或更改既有 iOS 自动化。

最终数据验收后，initramfs 旧 runner 补写显式 `hosts: files dns`，避免最小系统因没有
NSS 文件而违反新准入条件；只做 Python 语法检查，未重跑旧 initramfs 矩阵，未改变
最终 Debian 受测服务或测试程序。

三个远端 UUID（初轮、最终轮、工具准备）均由 helper clean 完成；另经原生 SSH
读回三目录不存在、无本任务 QEMU，收据 `resolved-cleanup-readback.json`。工具生成树
先核对精确 UUID、UID、包清单 hash 和无活 QEMU 后删除，再执行 helper clean。
没有可续用的远端工具/镜像目录，下一轮须重新准备。

本阶段末主线新增 `878e455`（iOS/tvOS 保留连接后的重查与后台延迟），已在工作分支
以 `bc9c8e9` 合入；没有推送或创建 PR。新增文件只涉及 Apple 客户端，合并后的
`make ios appletv` 再次通过（`resolved-main-apple.log`），无新增 XCTest。Linux 和
Android 产物保持原始来源摘要，不把合并后版本号写进先前受测产物。
