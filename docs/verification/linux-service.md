# Linux 独立服务验收（2026-11-17）

## 实际交付范围

`cmd/xdial-service`、`core/linuxservice` 和 Linux Unix seqpacket 传输已接通共享
Profile 编译、ConnectionController、稳定 TProxy 入口与 journaled capture。
这是实际跨进程服务/CLI，已在隔离内核处理普通应用流量；不是仅 `net.Pipe`
或控制面 mock。systemd unit、安装/移除脚本和压缩包已生成，**未执行完整发行版
安装、升级或卸载**，仍非完整 Linux 客户端。使用说明见 [服务说明](../agent/linux-service.md)。

## 环境和来源

- 宿主 `ubuntu-211`：Ubuntu 24.04 x64，普通用户 Cindy 原生 SSH；实际 Go 不在
  远端。二进制由本机 Go 1.26.5 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 构建，
  tags `with_gvisor,with_utls`，然后原生执行。不能声称源码在远端构建。
- QEMU 8.2.2，TCG 2 CPU/1536 MiB，独立 Linux `6.8.0-149-generic` initramfs，
  无 NIC、磁盘或宿主挂载。官方 Ubuntu 包按 APT 元数据 SHA256 验证后仅解包。
  不运行 apt install/postinst/sudo，不加载宿主模块，不改变宿主网络。
- 最终源快照是 `cdc6c62 + dirty`；source-manifest 逐文件 SHA，未改写成后续提交：
  `ba25b5a109645a62f98319b866f37e7e38cd273d78edb5bfc4e2204b1bf07d50`。
- fleet 快照：`57d62f4a68eed3ad9a16872c8c062bd5bc78c90397a2e71601f17ed8eccd7efa`。
- 原机测试的服务 binary：
  `f0eb2c5cf6821ec0248a83ca093b701c8c59d4f56609b2dd88a759498cd090a2`。
- 服务 test binary：
  `1f089c5d9211f73f6eed4a51a288f6396c69f9b0814d83293d6aba465230eb13`。
- libbox test binary：
  `dbd6b20a8cc54e1d1c261d10d52e85f50a85fdd01f66a067924bf3d896ce74c8`。

## 最终来宾结果

服务包三个顶层测试通过，另 child 入口在顶层枚举时 skip，实际由独立子进程调用；
libbox 两项真实内核回归通过。原生执行与 `result.json` 均退出 0，日志含
`XDIAL_SERVICE_KERNEL_OK`、`XDIAL_TRANSPARENT_GUEST_OK`。

1. **真实鉴权与有限请求**：服务为 guest root，UID 1001 客户端跨进程调用，UID
   1002 被拒绝。另将已认证 root 连接作为 fd 传给 UID 1002，确认每条消息的
   SCM_CREDENTIALS 会重新拒绝，不能借用 accept-time 身份。顶层伪 UID/PID 被
   严格 JSON 拒绝；错误 server UID 在发 payload 前拒绝。
2. **服务生命周期**：root 私有 policy/state，第二服务进程因锁拒绝；普通 UID
   发送 Profile，通过共享编译器启动无 listener 数据 Box，再启动稳定 TProxy，
   最后安装捕获。过期 revision/旧 service instance 均拒绝，不改变连接。
3. **真实 DNS**：应用向无 DNS 服务的 `192.0.2.54:53` 发普通 UDP 查询；核心
   捕获后按 OS `/etc/resolv.conf` 的 `192.0.2.53` 转发，返回受控 DNS 答案。
   resolver 是真实来宾 UDP socket，带 bypass mark，不是 hosts 注入。测试目标
   路由、地址和 resolver 文件只在来宾建立、最后撤销。
4. **原生应用选择**：`/service-allow` 与 `/service-proxy` 为实际独立可执行进程，
   普通 TCP 不自报 PID/path、不设 mark。提交 Profile 应用条件后，前者 Direct
   echo；后者进入指定 Shadowsocks-none fixture，fixture 实际读到原目标后关闭。
   这是精确进程选线与目标传递证据，**不是外部代理 HTTPS 成功**。
5. **连接中事务**：提交后旧 TCP 继续 echo；连续两个提交的各代际保持至有界退休，
   最终旧流关闭。服务定时器绑定核心对象+revision，不能影响之后的代际。没有声称
   对定时器执行器做专门迟到回调故障注入。
6. **真实强杀恢复**：对实际服务进程 SIGKILL，重启服务清理自身 journal 剩余规则，
   回到未连接，instance 改变；旧 instance 请求拒绝，新的连接/断开成功。不是磁盘
   掉电或多发行版 systemd 重启证据。
7. **网络事实变化**：连接期间将来宾 resolv.conf 改为 `127.0.0.53`，事件使服务
   撤销捕获并断开；新连接明确拒绝 unsupported_underlay，恢复原 resolver 后可重连。
   不回退公共 DNS，也没有在 unsupported 状态自动复活。
8. **既有透明适配回归**：同一最终来宾另运行 libbox 的普通 TCP/UDP/DNS、真实
   `/app-block`/`/app-allow` 分流、prepare/commit/retire、Stop/listener 释放测试。

初轮因平台默认网卡要求失败；显式 Linux kernel-routed mode 修复后不虚构接口，
Apple/Android 原校验保持。第二轮缺测试目标的来宾路由导致 connect ENETUNREACH；
补齐来宾专用目标路由，不放宽生产路由检查。最终通过后又对 timer generation
所有权补充检查，重建最终 binary 再跑完整服务来宾测试通过。

## 本地回归与产物

- `service-core.log`：config/libbox/mobile/subscription 常规全量通过。
- `service-shared-race.log`：KernelRoutedUnderlay/Packet/AndroidSwitch 定向 race 通过；
  `service-config-race.log`：Linux 编译器定向 race 通过。没有全量 libbox race 或
  来宾 kernel race 新结论。
- `service-native-validation.log`：原机普通用户执行 resolver admission、拒绝 root 文件
  访问的 Profile admission，以及 CLI 策略有效/错误参数测试，native/result 均为 0；
  不安装或修改网络。
- `service-vet-final.log`：Linux service/controlpipe/CLI/libbox vet 通过；Windows
  默认核心 build 通过。两个保护层测试仍需区分：本地契约与真正内核鉴权。
- `service-apple.log`：iOS/tvOS generic simulator、macOS Debug/Release App 与扩展
  六构建成功。没有新增 XCTest、真机或安装验证。
- `service-android.log`：双 ABI AAR/APK 与签名/内容校验通过。APK
  `build/android/artifacts/xdial-1-debug-c4f3855a6b53-fa3a5524abc9/xdial-1-debug-c4f3855a6b53.apk`，
  SHA256 `c4f3855a6b53a518ca19a798f1db031b22f335fb36f5474daf3a25130e4121c8`。
  manifest 为 cdc6c62 dirty；本阶段未安装设备，不沿用以前的 matchesLocal 或流量证据。
- Linux 包 `build/linux-service/xdial-service-linux-amd64.tar.gz`，SHA256
  `e043ecbf7b91593b0b33118387ae8320653bd16ba626c3d31347aec6dd8bc0aa`；包内服务 binary
  与最终来宾受测文件相同。安装脚本最后补充首次安装目录/并发锁保护，仅做语法检查，
  未执行安装；不要把该脚本和 unit 宣称为已完成系统安装验收。
- 具体收据：`build/desktop/service-guest-final.log`、`service-guest-final-result.json`、
  `service-native-final-receipt.json`、`service-vm-packages.json`；打包来源在 manifest。

## 仍需推进

真实 systemd-resolved/search/split DNS 后端、自动网络恢复、IPv6、已有
mark/iptables 共存、协议租约和代理出口就绪证明、服务规则缓存/配置库接线、原生
窗口、安装升级以及 Debian/Fedora/Arch 完整来宾验收。没有公网浏览器、企业 VPN、
多用户恶意 root、生产凭据或宿主改网测试；当前边界拒绝不能被当成永久缩减目标。

本阶段五个 fleet task（四个代码快照、一个工具目录）均已完成结果取回并由 helper
清理。工具生成的 root/downloads 在精确目录、UID、包清单摘要核对后先移除，再
清理 task，避免逐个 SFTP 删除内核模块。收据为 `service-clean-{1,2,3,4}.json` 与
`service-tools-clean.json`。没有活跃 QEMU、安装到共享宿主的服务或待续用工具目录。
