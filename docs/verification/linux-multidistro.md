# Fedora 与 Arch 完整系统验收

本阶段使用发行版官方完整 cloud image，保留自己的内核、systemd、文件系统与安全策略。
这不是更换 initramfs 名称；Debian 的先前证据见 [Debian 安装](linux-distro-systemd.md)、
[升级事务](linux-upgrade.md) 和 [resolved](linux-resolved.md)。

## 可复现入口与隔离

`download-cloud.py` 固定 Fedora 45 / Arch 20261115 镜像名称，从官方 HTTPS checksum
manifest 校验 SHA256，保存实际 URL、摘要、字节数。Fedora 镜像字节来自官方
MirrorManager 列出的 AARNet 镜像，checksum 仍取 Fedora 官方站。没有宣称验证 OpenPGP。
`run-distro.py --distro fedora|arch` 重新核对摘要，检查来宾实际 `/etc/os-release`，
使用只读 base + 一次性 overlay、两个只读 ISO、QEMU TCG 两 CPU / 2 GiB。
没有 NIC、KVM、SSH 转发或宿主目录挂载；共享 Ubuntu 宿主只解包已校验工具，
来宾 root 才进行安装和网络测试。原机无 Go，程序是本机交叉编译后在来宾执行。

Fedora 测试开始与结束均要求 SELinux Enforcing，不执行 setenforce、替换安全策略
或将测试进程切到不受限域。split DNS 测试进入自有网络命名空间时使用
`nsenter --net`，避免 `ip netns exec` 额外重新挂载 sysfs。这不改变生产服务权限。
Arch 无 NIC，远程校时永远无法完成；测试启动配置只停止本次来宾的
`systemd-time-wait-sync.service` 等待，不伪造已同步标记，不修改 XDial unit。
因此本验收不覆盖 NTP/联网校时行为。来宾退出后删除 overlay 与 seed/payload ISO。

当前测试仍明确使用 `hosts: files dns`，不声称适配默认 nss-resolve、共享 nscd 或
应用直接 D-Bus/Varlink 调用的归因；这些产品缺口继续保留。

## 首轮问题与修正

- Fedora 首轮安装生命周期成功，但整个运行返回 1：split DNS 夹具的 `ip netns exec`
  被 cloud_init_t 的 SELinux sysfs 挂载限制拒绝。保留失败日志，改为仅进入所需
  network namespace 后重新执行，没有放宽策略。
- Arch 首轮确认 cloud-init 识别 NoCloud，加载实际 Arch 内核；无 NIC 的远程校时等待
  阻止 cloud-final 执行测试，570 秒期限结束后 QEMU 被 kill/wait、磁盘清理。
  此次没有产品测试通过证据；启动日志和非零结果保留。

## Fedora 暴露的产品修复

第二轮分域 DNS、动态域路由、resolver 重启和不同 UID/cgroup 的真实流量已执行，
但 NSS 切到原生 IPC 路径后没有撤销捕获，整个运行仍返回 1。Fedora 的 NSS 文件
通过链接位于另一配置目录；旧实现仅监视 `/etc`，遗漏了实际目标写入。
`watchConfigPath` 现在先订阅每个经过的父目录，再读取链接与目标，并观察最终 inode；
事件后重新订阅，覆盖原子替换和改指向。保留旧订阅到服务结束，最多 64 个监视、
40 次链接展开，超过边界或观察失败先撤捕获。没有定时轮询或扩大 NSS 准入范围。
新增原机用例验证目标写入、链接原子替换后的新目标写入以及循环链接拒绝；完整
Fedora 仍须用最终程序确认系统布局下的运行时撤销。

## Fedora 升级暴露的 drop-in 准入问题

修复监视后的 Fedora 升级首轮（task `72e63c6d`，run `3afd2ce0`）在正常升级前即拒绝：
`unit overrides require explicit review before upgrade`，native/result 为 1，日志保留为
`build/desktop/multidistro-fedora-upgrade-initial-{test,guest}.log`。原因是升级只接受
`DropInPaths` 为空；Fedora systemd 在 `/usr/lib/systemd/system/service.d/` 提供对所有
service 生效的发行版 drop-in，Debian/Arch 没有。首次安装不检查 drop-in，所以生命周期测试
不受影响。

`reviewDropIns` 现在只放行该发行版目录下直接的 `*.conf`，并以与安装文件相同的
root 安全祖先、NOFOLLOW、单链接、普通文件、非组/其他可写规则读取每个文件。
`xdial-service.service.d`、`xdial-.service.d` 前缀目录、`/etc`、`/run`、`/usr/local`
下的任何 drop-in（包括全局 `service.d`）仍需管理员显式审查。这些发行版文件对旧版和候选
版同样生效，升级不改变它们；本修复不扩大首次安装的权限或 unit 内容。
原机单元测试 `TestReviewDropIns` 覆盖允许集合、各类拒绝路径与不安全文件。

## 结果与收据

| 发行版 | 内核 | 运行 | 结果 |
|---|---|---|---|
| Fedora 45 Cloud（SELinux Enforcing 起止） | 7.2.11-300.fc45 | task `72e63c6d` run `23a2bd08` | 监视目标/NSS 准入/resolved 29.12s/systemd 生命周期 36.57s 通过，EXIT 0 |
| Arch 20261115.625457 cloudimg | 7.3.4-arch1-2 | task `0d098d15` run `a6915224` | 同四项通过（resolved 25.58s、systemd 30.68s），EXIT 0 |

两轮使用同一 staging（source manifest `cfc88318…`，服务包 `d5495ff9…`，lifecycle test
`730ee28f…`）。Arch 镜像 SHA256 `dbd02830f6eb0fe07ef6e5edaddbae8b577ce4296d9e79118904704cce0a1f60`，
569933312 字节，来自官方 HTTPS manifest。日志：`build/desktop/multidistro-{fedora,arch}-final-{test,guest}.log`。
drop-in 修复只改升级准入，生命周期结论沿用上述程序。

### 升级与启动失败回滚

修复后重新 staging（source manifest `e201f2da…`，HEAD `11ceb41` dirty；服务包
`967cfe0499c7254cf1cb2b1115ece76fcd4fe8a1a3f7fd19252d9632cc1114f2`；upgrade test
`96e2b889aa04702dfe4cdc6754203e5d6cbf1fef3b66ffc485d69bbdeb473549`；lifecycle test
`cfb171396338fb78d377b9faf5f975e164262720711afc6c323e250d6391c5e2`；旧基线包
`b1389bb9…`；fleet 快照 `86234b8c…`）。每次运行先在 Ubuntu 宿主原机执行
`TestReviewDropIns`，再启动来宾。

| 发行版 | 运行 | 结果 |
|---|---|---|
| Fedora 45（SELinux 起止 Enforcing） | task `9a69d561` run `b0740372` | 实际旧包→新包 `install.sh upgrade`、坏候选 exit 42 触发 systemd 启动失败并自动回滚 94.30s，随后 systemd 生命周期 32.98s，EXIT 0 |
| Arch 20261115.625457 | task `711e9544` run `cd041791` | 同组 70.10s，systemd 生命周期 29.25s，EXIT 0 |

本组是 `XDIAL_UPGRADE_GROUP=normal`：正常升级与启动失败回滚，不含九处强杀矩阵；
强杀恢复的完整证据仍只在 Debian（[升级事务](linux-upgrade.md)）。日志：
`build/desktop/multidistro-{fedora,arch}-upgrade-{test,guest}.log`，失败首轮
`multidistro-fedora-upgrade-initial-*.log`。

### 未覆盖

默认 nss-resolve / nscd / 应用直接 D-Bus·Varlink 调用的归因（测试显式 `hosts: files dns`）、
Fedora/Arch 上的强杀矩阵、NTP、公网与企业 VPN、IPv6、物理掉电、arm64。

四个运行 task（`72e63c6d`、`0d098d15`、`9a69d561`、`711e9544`）已在确认无 QEMU、均有完成结果后
用 helper 清理。只读工具与镜像 task `3bfb6f6e`（解包工具 + Arch 镜像）和 `279a9bef`（Fedora 镜像）
暂时保留，供原生 NSS 入口在同一发行版上复验；复用前须重新核对镜像摘要，阶段结束时清理。
