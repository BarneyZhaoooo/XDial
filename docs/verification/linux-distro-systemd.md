# 完整 Linux 发行版中的 systemd 验收

本验收使用完整 Debian 13 generic amd64 云镜像，包含发行版自己的磁盘、内核、
PID 1、systemd、AppArmor 与软件包。它与此前 Ubuntu 内核 + initramfs 测试不同；
不能把同一个内核换一个发行版名称当作多发行版验收。

## 隔离与复现入口

- `scripts/linux-vm/prepare-tools.sh` 从测试机已有 APT 元数据下载并校验官方 Ubuntu
  QEMU、genisoimage 等包，只解包到任务目录，不安装、不执行 postinst、不加载宿主模块。
- `scripts/linux-vm/download-debian.py` 从官方 HTTPS 下载 SHA512SUMS 与 exact image，
  校验完整 SHA512 后生成 image.json。记录实际跳转目标与字节数；不是 PGP 签名验证。
- `scripts/linux-vm/run-distro.py` 复核镜像摘要，创建独立 qcow2 overlay，TCG 两 CPU、
  2GiB 内存，无 NIC、KVM、SSH 转发或宿主目录挂载。只读 cidata ISO 提供 NoCloud 数据，
  第二张只读 ISO 提供本任务包与测试程序。包和测试程序进入来宾后再次校验摘要。
- Debian 来宾里的 root 执行安装/网络测试；共享 Ubuntu 宿主始终是普通用户。
  测试同时要求专用 DMI 标识、环境标志、UID 0、真正的 systemd PID 1。
- 主串口只记录启动日志，独立 virtio 串口记录测试结果，避免发行版登录终端重置结果通道。
  QEMU 有有界前台期限，退出或超时 kill/wait 后删除 overlay、seed/payload ISO 和中间树，
  保留结果和镜像身份。来宾生成的身份文件不会进入宿主安装目录或仓库。

全机启动日志中网络等待超时符合无 NIC 的测试条件；测试所用 DNS 是来宾内的受控
数值 resolver。默认 systemd-resolved stub/search/split DNS 仍需独立实现和验收，
不能以此处的透明 DNS 成功替代它。

## 实际测试覆盖

`core/linuxservice/systemd_guest_linux_test.go` 通过包内脚本操作真实安装位置和 unit：

1. 首次安装后服务未启动、无控制 socket；重复安装拒绝，原策略不变。
2. systemctl 启动真实服务，UID 1001 通过内核凭据鉴权；Profile 连接使用共享核心。
3. 先用来宾 runtime drop-in 移除 `CAP_NET_BIND_SERVICE` 作负面对照：普通 TCP 仍应成功，
   受控上游收到 DNS 查询，但原始低端口回包失败；删除 drop-in 后使用包内真实 unit。
4. 普通 UDP DNS 发往没有服务的测试目标，由系统透明捕获后查询受控 OS resolver；
   普通非 root TCP 客户端通过真实核心到达 echo 服务。
5. systemctl restart 后 journal 清理、instance 更新、保持未连接；重新连接后
   systemctl kill SIGKILL，systemd 自动拉起并恢复 journal，仍保持未连接。
6. 连接期间执行真实 remove，先撤捕获再停止/恢复，二进制、unit、socket、session
   journal 消失，策略保留；策略 rule 恢复为测试前状态，专用 route table 无残留。

已有安装的覆盖仍明确拒绝；这不是升级成功证据。测试不证明公网、企业 VPN、
IPv6、物理掉电、Fedora/Arch、图形客户端或系统 DNS 默认配置已经完成。

## 启动问题记录

首次无 VGA 的完整镜像停在 GRUB，未进入 Linux，结束该精确归属的 QEMU 后保留日志。
添加虚拟 VGA 时发现解包工具默认固件路径指向宿主路径；改用已校验 SeaBIOS 包内的
明确 ROM 路径后真正进入发行版内核。之后主串口与 getty 共享造成测试日志缺失，
不把服务曾启动当作通过，改用独立 virtio 结果串口后重新执行。

相关上游资料：[NoCloud 本地介质](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)、
[Debian GRUB/精简 QEMU 设备启动问题记录](https://alioth-lists.debian.net/pipermail/pkg-grub-devel/2025-August/019545.html)。

## systemd 权限差异

第一次完整服务测试在连接提交后 DNS 超时。上游 sing-box TProxy 的 UDP writer
以原目标地址/端口创建透明回包 socket；仅 NET_ADMIN/NET_RAW 不授权绑定 DNS 53
等特权端口。initramfs 中直接以完整 root 能力启动没有暴露这个 unit 缺口。包内
CapabilityBoundingSet 增加 `CAP_NET_BIND_SERVICE`，不修改系统端口阈值或放宽其余
文件系统/地址族限制；实际负面/正面对照结果记录在下方收据中。

## 2026-11-17 最终收据

完整 Debian 13.7 amd64，发行版 kernel `6.12.117+deb13-amd64`、真正 systemd PID 1；
共享 Ubuntu 24.04 x64 仅运行任务目录中的 QEMU 8.2.2 TCG。一个顶层生命周期测试
及其真实非 root 客户端进程通过，负面对照确认 TCP 成功、上游 DNS 查询数 1、低端口
回包失败；恢复包内权限后全部断言通过。native/result 均 0，日志有
`XDIAL_DISTRO_SYSTEMD_OK` 和 `XDIAL_DISTRO_EXIT=0`。

- 测试程序 SHA256：`a340df417423da891cc4e29291bf74d3b34ec828296958af3f704fdf0571ea7c`。
- 服务 binary SHA256：`f0eb2c5cf6821ec0248a83ca093b701c8c59d4f56609b2dd88a759498cd090a2`；
  本次修复在 unit，服务二进制与上一阶段一致。
- unit SHA256：`8583f58f6e8dd236717e86a509cf5bac04fcb20d5654fae72b0b524ff3bdac3a`。
- 最终包 `build/linux-service/xdial-service-linux-amd64.tar.gz`，SHA256
  `b1389bb90b7b5f9f86315089b286c4b9a19e11bbd2abceca2d00588b4fb4085d`。
- 来源 `7c6b932` dirty；source-manifest SHA256
  `34b26549f0de0b0008814bd5e7bf3d5461b51fdbadfe82db7b41aa6612f7b122`；fleet snapshot
  `d8cb8ecfa020e342e481ad60bad95739fff24bc851bfaeed61772af434c6df54`。
  原机没有 Go，程序由本机 Go 1.26.5 交叉编译后在真实来宾执行，不宣称原机源码构建。
- Debian base image 434831360 字节，SHA512
  `e53a93ef33f828ceed0e7278c2cc2753bdc7d5098fbfae15bd64b6d878db350ea3744247763a1c8ecb6b4019a0f623c38caf6038ad6f9c357fb4e01493ddf1a3`。
- 证据 `build/desktop/distro-systemd-final.log`、`distro-boot-final.log`、
  `distro-cap-native.json`、`distro-cap-fetch.json`、`distro-vm-packages.json`、`distro-image.json`。
  未修复 unit 的真实 DNS 失败保留在 `distro-cap-failure.log`。
- Linux service/CLI vet、Python/shell 语法、public-content-gate 通过。无共享 Go 生产
  代码、Apple/Android 入口变更，本阶段没有重复移动构建、设备或 XCTest 验收；最近的
  六 Apple 构建和双 ABI Android 产物仍对应上一阶段。没有 kernel race 或全量
  libbox race 新结论。

本轮七个远端 task 已全部取回结果并由 helper clean，目录不存在已读回确认。工具
root/downloads 在无活 QEMU 后按精确路径、UID 和包清单摘要检查后删除，再清理
工具 task；来宾磁盘由 runner 在 QEMU 退出后删除。没有残留 VM、安装到共享宿主的
服务或可以继续使用的远端工具/镜像目录。清理收据为 `distro-clean-1.json` 至
`distro-clean-7.json`；下一轮应重新准备已验证工具与镜像。
