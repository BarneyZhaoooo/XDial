# Linux 安装升级与崩溃恢复

本阶段针对独立 Linux 服务的安装事务，不替代 Windows、其他发行版、原生 DNS 或
桌面界面的交付。管理员显式执行包内升级命令；普通用户 IPC 仍只提供有限连接操作。

## 实现与恢复边界

`core/linuxinstall` 固定操作已安装 binary、unit、安装摘要三个文件，不接受客户端
目标路径、shell、系统网络规则。它检查 root 所有的安全祖先、文件类型、权限、链接数、
摘要及 systemd fragment/override/enablement，拒绝所有权不明的替换。

私有升级目录保存旧版、新版及版本化 journal；暂存文件和每次 journal 发布均做
文件与父目录 fsync。停止服务并恢复捕获事务后，逐文件使用同目录 rename 发布。
这不是跨文件系统的一次原子写：崩溃后的 journal 决定还原旧版或完成提交，未知状态
保留现场。policy/cache 不参与替换，也不把 VPN 连接意图保存为安装状态。

运行中的服务升级后等待 `Type=notify` 的 READY，再写入已提交记录；候选启动失败
还原旧包并恢复原来的服务运行状态，但保持 VPN 未连接。原来停止的服务升级后仍
停止，不宣称已验证其启动。显式强杀恢复不自动启动服务；只有尚未写初始 journal
的暂存阶段不影响原来的运行服务。enablement 保持原值。

安装器与卸载脚本同时持有全局安装锁和私有状态独占锁；服务 connect/switch 使用
共享状态锁，避免放宽 unit 的文件系统限制。未完成 journal 在安装器死亡后仍阻止
新版服务的连接，查询与用户主动断开继续允许。首次从缺少维护锁的历史包升级时，
不能给旧进程补锁；它在停止前仍执行原请求协议，安装器必须先停止并恢复捕获后再替换。
服务 readiness 只证明恢复和控制端点已准备好，
不代表数据面已经连接或候选线路可用。

systemd 契约依据：[Type=notify 与 READY 通知](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)。

## 完整来宾验收入口

使用与[Debian systemd 生命周期](linux-distro-systemd.md)相同的官方完整镜像、
校验后的任务目录 QEMU、TCG 两 CPU/2GiB、无 NIC/KVM/宿主挂载。包和测试程序
通过只读 ISO 进入来宾；实际 root 安装和网络变化只发生在一次性 overlay 内。
原机没有 Go，程序在本机交叉编译后由来宾原生执行。

`core/linuxinstall/upgrade_linux_test.go` 的测试要求专用 DMI、环境标志、root 与
systemd PID 1，普通宿主不能误运行。`run-distro.py --baseline OLD_BUNDLE` 提供
前一阶段实际发布结构的安装包；`--upgrade-group early|late` 将同一套二进制的
强杀测试分开启动，避免整个矩阵受单次 VM 总时限截断。完整覆盖必须两个组都通过。
每组须有对应 `XDIAL_DISTRO_UPGRADE_OK GROUP`、`XDIAL_DISTRO_EXIT=0` 与 native/result 0。

测试内容包括：

- 实际包内 `install.sh upgrade`，可写来源目录拒绝，旧包更新为新包且 policy 不变。
- 维护期间状态可读、主动断开可用、新连接拒绝；维护完成后可重新连接。
- 真实 systemd 候选启动失败，旧版文件和原 enablement 恢复；未运行服务升级保持停止。
- 在暂存、初始 journal、服务停止、binary/unit/manifest 替换、READY、提交和备份清理
  九处，独立安装子进程真实 SIGKILL；恢复幂等，提交边界前后选择正确版本。
- 半途安装文件被外部替换时拒绝覆盖，恢复成记录中的合法版本后才继续。
- 最终包再次运行真实服务生命周期测试：普通非 root TCP、透明 UDP DNS、systemd
  restart/SIGKILL 恢复，以及连接中卸载。

这些进程强杀在完整磁盘 overlay 上执行，但不是物理掉电或存储设备故障测试。
仅 Debian 一种完整发行版；不代表 Fedora/Arch、默认 resolved/split DNS、IPv6、
公网、企业 VPN 或图形产品已完成。SHA 摘要是内容完整性证据，不是发行签名。

## 验收记录

2026-11-17，在 ubuntu-211 普通用户的任务目录中运行两次全新 Debian 13.7 来宾，
kernel `6.12.117+deb13-amd64`、systemd PID 1、QEMU 8.2.2 TCG。前组五个子场景
通过（311.87 秒）；后组五个子场景通过（181.12 秒），正常路径在后组明确 skip，
由相同二进制的前组覆盖。另一个最终包服务生命周期测试通过（36.17 秒），含普通
非 root TCP/UDP DNS、低端口权限负面对照、restart/SIGKILL 和连接中 remove。
两次 native/result 都是 0，分别有 early/late 标志，后次还有
`XDIAL_DISTRO_SYSTEMD_OK`，两次均有 `XDIAL_DISTRO_EXIT=0`。

- 升级测试程序 SHA256：`e019dcc88f74a921c847116b4a3cf7bbb8c8809bace46a7c07144f45ea28c172`。
- 服务生命周期测试程序：`ee91aae7805a13e9d48ac275a56369928113b5834fe4b464cd6023712031f66d`。
- 服务 binary：`2df5dac2e65438569e7d3f3fb0ad391d6b62bf3135e571332b454feebaf67a73`。
- unit：`3872e3ceedb3958b5196072cb98e6acfad39da15a4090e277caad12811e72705`。
- 最终包 `build/linux-service/xdial-service-linux-amd64.tar.gz`：
  `6ef38f3b01949cbd9fafe4fd2fbd99b7f65c2720dda9bc9f614a6859b61f6cb2`。
- 升级前基线为前一阶段实际包，保存于 `build/desktop/upgrade-baseline.tar.gz`：
  `b1389bb90b7b5f9f86315089b286c4b9a19e11bbd2abceca2d00588b4fb4085d`。
- 来源 `643eceb` dirty；source-manifest SHA256
  `560ff35b5d3979efbcec6ea651d983a29c53fb43b094a4c44da9c3bc9e06a504`；fleet snapshot
  `562bd384263084e8ea9d7723b1ad22fa6a42b93fcee0189343a7734804436f18`。
  后续文档和提交不改写已受测产物的来源字段。
- Debian 镜像与此前相同，434831360 字节，SHA512
  `e53a93ef33f828ceed0e7278c2cc2753bdc7d5098fbfae15bd64b6d878db350ea3744247763a1c8ecb6b4019a0f623c38caf6038ad6f9c357fb4e01493ddf1a3`；
  下载与工具身份见 `build/desktop/upgrade-image.json`、`upgrade-vm-packages.json`。
- 原生回执 `build/desktop/upgrade-native-{early,late}.json`，helper 结果
  `upgrade-fetch-{early,late}.json`，完整结果 `upgrade-{early,late}-test.log`，
  启动日志 `upgrade-{early,late}-guest.log`。所有日志只来自隔离来宾。

首轮把全部场景放在同次运行，测试总时限 240 秒在 kill-binary 阶段截断，
native/result 1；没有把前面已运行的断言冒称完整通过。保留
`upgrade-initial-test.log`/`upgrade-initial-guest.log` 与 `upgrade-native-initial.json`。
最终按同一套二进制拆组，测试总时限 330 秒、VM 570 秒；生产启动/停止时限不变。
测试机其他构建负载有变化，两组都使用两个虚拟 CPU 和 nice 10，不停止其他任务。

Linux installer/service/CLI 目标 vet、shell/Python 语法、public-content-gate 通过。
本阶段没有共享 Go/Apple/Android 生产入口改动，不重复它们的构建、设备或 XCTest，
不把前一阶段证据算作新增验证。没有 kernel race 或全量 libbox race 新结论。

三个远端任务（初轮、最终双组、工具）均取回结果并 helper clean。先在任务目录
核对路径、UID、包清单 SHA 和无使用中的 QEMU，再移除解包工具树，避免逐模块
SFTP 清理。来宾 overlay/ISO 已由 runner 删除，最后原生读取确认三个目录不存在、
无本任务 QEMU。回执 `upgrade-clean-{initial,final,tools}.json`、
`upgrade-clean-tools-native.json` 和 `upgrade-clean-readback.json`。没有宿主服务、
驱动、系统软件安装或网络更改，也没有保留可续用的远端镜像/工具目录。
