# Linux 真实透明入口：隔离来宾验收

2026-11-17；父提交 `be4826f` + 本阶段源码。这里交付的是实际 Linux TProxy
适配与真实内核流量证据，**不是 Linux 完整客户端、权限安装或多发行版已完成**。

## 已实现的边界

- `ConfigureLinuxTProxy` 在连接前选择受保护的 native-flow 模式；流量 Box 和
  候选 Box 无 listener，启动与候选准备都校验精确的防环路 mark。显式 socket mark
  不得另设值，不接受外部 namespace、本地 DNS 或尚未准备的远程规则集。
- `StartLinuxTProxy` 独立持有 sing-box 原生 TProxy listener/UDP NAT。TCP 原目标
  来自内核 socket，UDP 原目标来自控制消息，透明 UDP 回包复用上游实现；XDial
  不再另写一次目标恢复、UDP 转发、DNS 或进程规则裁决。
- 稳定 listener 将流交给已有 generation Router；DNS/route 在同一提交点切换，
  旧流继续至明确退休。停止先拒绝接流，关闭各代际后清理 listener；启动与停止
  并发时，晚完成的 listener 不能发布到已退休入口。直接连接入口地址会被拒绝。
- 流量 dialer 的 `route.default_mark` 与透明 UDP 回包 listener 的 `routing_mark`
  一致，内核捕获规则排除该 mark。当前配置守卫只允许已审查的 sing-box 原生
  dialer；AnyConnect/Tailscale 等协议能力从 JSON 和显式启动/切换参数两处拒绝，
  必须补齐独立 socket/身份租约后解除，不能把拒绝当最终产品范围。
- Linux Underlay 绑定实现 `SO_BINDTODEVICE`，仅绑定已经捕获的 interface index
  对应接口；不存在或权限失败即失败，不改路由、不另选物理接口。此绑定 API 与
  防环路 mark 职责不同，本轮真实流量以 mark 防止捕获自身 socket。
- 平台 API 不隐式修改 nftables、系统 DNS 或 policy route。权限服务须先预留互不
  冲突的 mark/table/priority，建立可撤销的捕获事务；断开先撤销自身捕获，再停止入口。
  这一生产安装/恢复事务还未接上，不能让普通客户端传入 shell 或任意系统规则。

内核接入依据 [Linux TProxy 文档](https://docs.kernel.org/networking/tproxy.html)；
实际转发继续使用仓库锁定版本的 sing-box `protocol/redirect/tproxy.go`。

## 隔离方式与可重跑入口

共享 Ubuntu 主机无法创建无特权 user/net namespace，因此使用任务目录内的 QEMU
8.2.2 TCG。无 KVM、无 NIC、无 TAP/端口映射、无宿主共享挂载、无真实磁盘；来宾
以独立内核和一次性 initramfs 启动。QEMU 由宿主普通用户执行，2 vCPU/1536 MiB，
前台有界运行，超时终止并等待本次子进程，不按进程名清理。

`scripts/linux-vm/prepare-tools.sh` 只下载 Ubuntu 包、对照 APT 元数据校验 SHA256、
解包到新的任务目录。不执行 apt install、维护脚本、提权或宿主模块加载。包版本和
摘要写 `packages.json`；当前使用 Ubuntu `6.8.0-149-generic` 内核。测试 runner
复用解包工具并拷贝其依赖库，因此这仍是 Ubuntu 主机上的隔离内核验收，不能称为
独立发行版系统镜像或跨主机可重现发行包。

```sh
# 本机交叉构建；远端没有 Go，不能称为远端源码重建。
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test \
  -tags with_gvisor,with_utls -c ./core/libbox \
  -ldflags='-s -w' -o /task/stage/linux-tproxy.test

# 经已登记测试机入口上传源码摘要、测试程序和 runner 后，在新任务目录运行。
python3 run-guest.py --tools /task/verified-tools \
  --binary /task/stage/linux-tproxy.test --output /task/new-guest-result
```

修改内核状态的测试同时检查显式环境变量、来宾 cmdline 标记、root 和只有 loopback
接口；普通主机 `go test` 默认跳过此项。测试进程在来宾中创建并删除自己的 nft 表、
精确 fwmark rule 和独立 route table。宿主没有安装软件或改变 DNS/路由/VPN。
最终数据验收后，runner 补上 QEMU 退出后的本次生成树清理，避免逐个 SFTP 删除
内核模块过慢；测试程序和内核接入逻辑未改变。原验收快照保留当时 runner 原文。

## 真实覆盖与初次失败

1. 普通应用 `net.Dial` 进入 nft output → policy route → prerouting TProxy，未经
   `RouteNative*` 注入，不提供伪造 PID/path。TCP 到真实 echo socket，UDP 到真实
   echo socket；UDP 服务端观察到不同的转发源端口，排除“直接绕过核心”的成功。
   应用收到的 UDP 回包仍来自原目标端点。
2. 普通 UDP DNS 请求发往没有 DNS 服务的 `192.0.2.53:53`，由当前 sing-box hosts
   transport 返回答案并核对请求 ID。准备候选时仍为 `.10`，提交后为 `.20`；这
   是真实 DNS 数据包和内核拦截，不是对公网 DNS 服务的连通性验证。
3. `/app-block` 与 `/app-allow` 是实际独立进程。提交前前者可 echo；提交后 sing-box
   通过真实 socket 表和 `/proc` 查得可执行路径，前者得到明确连接关闭、后者仍成功。
   不以超时充当拒绝，不称为浏览器 UI 测试。
4. 旧 TCP 在准备/提交后仍可收发，退休后关闭；新流继续 DNS/echo。停止关闭当前流
   并释放 listener 端口。无保护配置/候选、重复 listener、协议能力旁路均拒绝。
5. 内核 nft counter 同时证明捕获和带保护 mark 的绕过流量；没有依赖宿主外网。
   IPv4 端到端通过，IPv6 仅有建表/撤表能力测试，尚无 IPv6 应用数据面验收。

初次环境启动缺 QEMU module 路径；补齐 `QEMU_MODULE_DIR` 后进入内核。压缩模块
曾被当 ELF 交给内核；在来宾文件树内解压并重新生成依赖索引后通过。精简来宾曾缺
`/dev/stdin`，nft 加载失败，补齐 proc fd 链接后重跑。

首轮数据测试 UDP 超时：受控 echo 服务与透明回包 socket 在同一来宾内占用同一
源端点。给**测试服务**设置 `SO_REUSEADDR` 后原目标回包通过；没有修改核心上游
UDP 回包实现来掩盖错误。最终测试还验证转发源端口不同。

## 尚未覆盖

Windows WFP/原生命名管道与服务 ACL、Linux 权限服务及安装/崩溃恢复、真实桌面 UI、
既有 VPN/复杂 Underlay 叠加、跨主机出口、完整 IPv6、AnyConnect/Tailscale 协议租约、
Ubuntu/Debian/Fedora/Arch 的安装升级卸载矩阵仍需交付。不得直接复制来宾固定
mark/table/priority 到用户系统；它们只适用于没有既有规则的测试来宾。

本轮没有修改或启动宿主 VPN，没有使用生产凭据，没有推送、PR、外部分发或 iOS
自动化变更。Android 本轮仅构建回归，没有新增模拟器安装/流量证据。

## 最终回执与回归

- Ubuntu 24.04 x64 宿主：非 root 普通契约测试通过；独立来宾运行同一二进制，
  契约与内核流量测试均通过，四次独立应用进程检查通过。Cindy 原生回执与任务
  `result.json` 均为 0，来宾输出 `XDIAL_TRANSPARENT_GUEST_OK` 后关机。
- 测试程序 SHA256：
  `3f0222c39f68bfe8e0b8c4b859456fb019c6aece54e4d3e9fc3948ec591736e0`。
  上传快照 `72369ac67b721e8bb7117e0767038695efe7d79abe0f4767780edaea0a557151`；
  逐文件源码清单摘要
  `2852bc41971a48579e3fdf74613c89499813a46143e93d9353def6419788bdba`。
  记录如实为 `be4826f` + dirty，不改写为事后提交。
- 证据：`build/desktop/tproxy-guest-final.log`、配套 stdout/result、
  `tproxy-vm-packages.json`。四次代码快照任务和工具准备任务均按各自 UUID 清理，
  本地回执保留；没有留下后台 QEMU。
  三个初轮目录的 SFTP 逐文件清理过慢，停止已核对 PID 的本任务清理进程后，
  经同一 Cindy SSH 连接核对路径/所有者/源码清单 SHA，再删除这些已结束任务目录。
  原 helper receipt 不伪写完成，另存 `tproxy-native-cleanup-result.json` 与各任务的
  `native-cleanup-result.json`；五个任务路径最终均读回不存在。
- `tproxy-core-final.log`：config/libbox/mobile/subscription/CLI 常规全量通过；
  `tproxy-shared-race-final.log`：Packet/AndroidSwitch/StateLock 定向 race 通过。
  Linux TProxy 的原机/来宾测试本轮不带 race，不称为全量 libbox race 通过。
- Windows 默认特性共享核心构建、Linux with_gvisor/with_utls 的 go vet 通过。
  Apple 六构建（iOS/tvOS generic 模拟器、macOS Debug/Release App/扩展）通过，
  `tproxy-apple-final.log`；没有新增 XCTest 结果。
- Android 双 ABI AAR/APK 构建及签名/内容校验通过，
  `build/android/artifacts/xdial-1-debug-3d64ae6d6c6b-78cece2fad9e/`；APK SHA256
  `3d64ae6d6c6b6249b2c6bc246c3b3777c993ebd7616e110cb5693bd8f7409778`。
  仍是本地 Debug 包，manifest 为 `be4826f` + dirty，无设备 matchesLocal 收据。
