# Linux 捕获安装与恢复

## 实现边界

`core/linuxcapture` 是 Linux 权限服务使用的连接事务组件，不是已安装的服务，也不替代
sing-box 的 DNS、应用规则或出口裁决。它接收可信安装器预留的 mark、table、priority
及数值 IPv4 回环监听地址；没有默认资源编号，不接受客户端 shell/nft 程序。
调用方先完成 `ConfigureLinuxTProxy`、核心就绪与 `StartLinuxTProxy`，再 `Install`；
断开先 `Session.Close`，然后 `Libbox.Stop`。服务退出应在同一进程持有这些资源，
不能把安装放到执行完即退出的临时 helper。正式服务的授权、就绪与生命周期接线仍待完成。

- 安装前检查路由表、规则优先级、mark mask 冲突，以及会遮住捕获规则的更早策略。
  不改变现有优先级或 Underlay。现有 nft mark/conntrack mark 程序、legacy iptables
  暂明确拒绝，不能声称已支持任意防火墙、TC/eBPF、第三方 VPN 的组合；后续需专门的
  共存保留与变更观察，不允许静默覆盖。私有单播仍由 sing-box 裁决。
- 状态目录必须由安装器创建，所有祖先 root 所有且不可组/全局写，末级私有；逐段
  `openat(O_NOFOLLOW)`，状态文件核对普通文件、属主、权限、单链接，持有非阻塞 flock。
  读写有界严格 JSON；写临时文件、fsync、rename、目录 fsync 后才执行对应内核操作。
- 顺序为专用 local route、fwmark rule、原子 nft 捕获批次。只创建资源，不 replace
  或 flush 现有表。route 使用标准 `RTPROT_STATIC` 与随机 realm；完整恢复记录包含
  随机表名、boot ID、netns inode、精确计划和已开始的步骤，不含 Profile/凭据/流量。
- nft 使用持续 netlink socket 与 `NFT_TABLE_F_OWNER`，不设 persist。内核把表的修改
  权限限制给此 socket，socket 关闭/进程死亡即撤销捕获。正常断开先删捕获，再删除
  精确规则与路由；失败保留恢复记录，重复 Close 保留首次错误，不伪称清理成功。
- 恢复先获取独占状态锁；同 boot 不同 netns 拒绝，前一 boot 的记录只作过期记录删除。
  已存在同名 nft 表不接管、不按名删除。检查完整保留范围后才撤销 route/rule，
  出现额外对象或归属变化就保留现场。绑定依赖的 RuleList 丢弃 action，因此另验原始
  fib_rule_hdr 与所有属性，拒绝替换的 blackhole/unreachable/附加选择器规则。
- 捕获只覆盖本机发出的 IPv4 TCP/UDP；回环回注才进 TProxy。link-local、multicast、
  limited broadcast 不捕获。不改系统 DNS、默认路由、接口、sysctl 或持久防火墙配置。

root 管理者仍能并发修改其他网络子系统；跨 rtnetlink/nft 没有统一原子事务。本组件
不声称抵御恶意 root 重建完全相同对象，也尚未提供与其他网络管理者的持续协调。
这不是完整 Linux 客户端或多发行版安装交付，IPv6 捕获仍待补齐。

内核行为依据：[nftables 官方手册的 owner/persist 语义](https://www.netfilter.org/projects/nftables/manpage.html)
及 [Linux TProxy 文档](https://docs.kernel.org/networking/tproxy.html)。

## 验证方式

沿用 `scripts/linux-vm/prepare-tools.sh` 与 `run-guest.py`：普通用户任务目录校验并
解包 Ubuntu 官方包，QEMU TCG 无 NIC、磁盘或宿主挂载，管理操作只在独立内核中进行。
新增可选 `--capture-binary`，先执行权限事务测试，再跑 libbox 真实透明流量测试。
后者已从手写 nft/ip 测试安装迁移到 `linuxcapture.Install/Close`。

初轮镜像把打包账号属主/组写目录权限带入 initramfs，被生产安全检查拒绝；修复镜像
归档 root 属主和基础目录 0755，未放宽服务目录校验。冲突用例同时改为核对具体错误，
避免把无关的启动拒绝误算成冲突保护成功。

恢复测试还发现两处真实内核表示差异：默认 IPv4 地址的 4/16 字节表示，以及大表号
在 fib_rule_hdr 中使用 `RT_TABLE_COMPAT`。已规范化地址并按完整 `FRA_TABLE` 校验；
没有删掉 action/其他属性或降低归属检查来让测试通过。

## 2026-11-17 执行结果

- 主机 `ubuntu-211`，Ubuntu 24.04 x64；来宾 Linux `6.8.0-149-generic`，QEMU 8.2.2
  TCG、2 CPU、1536 MiB。重任务前负载 0.02、可用内存约 61 GiB；存在低负载 runner，
  没有停止或改变它。官方包只解包到本任务目录，不做 apt install/sudo/宿主模块加载。
- 权限事务 7 个顶层测试通过，另有 child 入口在父枚举中 skip、由实际子进程调用。
  三个步骤失败回滚、三个步骤 SIGKILL 后恢复、活 owner 拒绝、异 socket 不能删除
  owner table、重复 Close、取消、跨 namespace 记录拒绝均通过。
- 优先级/路由表冲突拒绝且外部对象仍可读删；更早策略遮挡拒绝。替换为 blackhole
  action 或更改 route realm 后，恢复明确失败并保留对象；还原后恢复成功。
  腐坏记录/路径 symlink 拒绝，独立的无关 nft table 在完整安装/撤销后仍存在。
- libbox 2 个内核集成测试通过：使用本组件真实安装/撤销，普通应用 socket 的 IPv4
  TCP/UDP/DNS、真实独立进程路径分流、场景准备/提交/退休、Stop 释放 listener 通过。
  并非 net.Pipe 截获替身；没有公网出口/浏览器 UI/企业协议证据。
- 最终原生执行和 `result.json` 都为 0，日志含 `XDIAL_TRANSPARENT_GUEST_OK`。
  本机 Go 1.26.5 交叉构建后执行；远端没有 Go，不称远端源码构建。
  来宾测试不带 race；本机纯计划校验 race、Linux 目标 vet、public-content gate 通过。
  没有改 Apple/Android 生产入口，未新增它们的构建、设备或 XCTest 证据。

最终回执：

| 项目 | SHA256 / 位置 |
|---|---|
| 权限测试程序 | `5dd3d807cb028ab50f43574f166c90d7d04950317da38ae22fef6eb7ddd0b60e` |
| libbox 测试程序 | `38d22bfd8a0deb04f238d5e9e968015b46c5298ccc22c4c509cb7f37d1735db6` |
| 上传快照 | `f93c92da791c6625f337f9f74003e60f1a4a16053a5f3fa67c75e7f75c2c4066` |
| 源码清单 | `2b8e650c24c1ec6dcd3d5e43765e574def8b612d1272605cacd5b004620e98c8` |
| 来源状态 | `4c730a0` + dirty；后续本地提交不改写测试来源 |
| 日志与结果 | `build/desktop/capture-guest-final.log`、`capture-guest-final-result.json` |
| 工具包清单 | `build/desktop/capture-vm-packages.json` |

磁盘掉电、fsync 故障注入、真实多网络/IPv6、其他发行版、正式服务生命周期仍未验。
当前恢复测试在 RAM initramfs 中以实际进程强杀验证，不冒称物理掉电持久性验证。

本轮 5 个测试快照目录与 1 个工具目录均已清理，保留本地日志/manifest/结果回执。
QEMU 退出后先删除自有 guest/cpio；工具目录先在有完成结果的受控清理运行中核对
路径、UID 和包清单摘要，移除生成树，再由传输 helper 清理该 UUID。未留下服务、
驱动、QEMU 或宿主网络变更。
