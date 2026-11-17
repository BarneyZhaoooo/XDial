# Linux 原生服务构建与安装预览

本入口把共享 Profile/Scenario 编译、内核进程归因、TProxy、连接事务与捕获 journal 接在同一长期服务进程里。它是正在完成的桌面适配入口，**不是完整 Linux 图形客户端或多发行版发行版**。

## 构建（不安装、不改变网络）

使用 go.mod 指定的 Go 与已组装补丁：

```sh
bash scripts/prepare-patched-go-mod.sh
scripts/build-linux-service.sh amd64
```

产物在 `build/linux-service/amd64/`，另有 `xdial-service-linux-amd64.tar.gz`、SHA256、来源 manifest。`arm64` 是可选构建目标；没有实际运行证据时不能把交叉编译视作验收。manifest 如实包含 HEAD、dirty、工具链与各文件摘要；打包不会隐式签名、安装或发布。

## 明确的安装策略

包中的 `install.sh` 只支持首次安装和精确移除。运行前由管理员审查并预留 **两个不同的单 bit mark、空路由表、规则优先级和 IPv4 loopback 监听端口**，选择一个允许控制服务的非 root UID。源码不默认占用固定资源。连接时仍会检查冲突，不能以管理员预留替代内核检查。

安装脚本参数为 `install UID CAPTURE_BIT BYPASS_BIT TABLE PRIORITY LISTENER`；不提供可误用于用户网络的固定资源示例。它验证包摘要、安装父目录、并发锁和策略格式，写入 root 私有 `/var/lib/xdial-service/policy.json`，安装 `/usr/local/libexec/xdial-service` 与 systemd unit，执行 daemon-reload，**不会启动服务**。首次安装失败回滚本事务文件。已有安装明确拒绝覆盖，后续升级事务尚待实现。

审查后管理员可执行 `systemctl enable --now xdial-service`。服务启动恢复 journal、开放本地控制接口，保持未连接；不保存 Profile 明文、不自动恢复连接。示例 unit 需要 systemd；已在完整 Debian 13 来宾中执行首次安装、正常重启、强杀恢复和连接中移除；Fedora/Arch、升级和默认系统 DNS 仍待验收，见[完整发行版证据](../verification/linux-distro-systemd.md)。

## 普通用户操作

允许的 UID 通过 `xdial-service status` 获取 `payload.instance` 和 `payload.revision`，再把两者传给修改请求：

```sh
xdial-service connect --instance INSTANCE_FROM_STATUS --revision REVISION_FROM_STATUS < profile.json
xdial-service switch --instance INSTANCE_FROM_STATUS --revision REVISION_FROM_STATUS < next-profile.json
xdial-service disconnect --instance INSTANCE_FROM_STATUS --revision REVISION_FROM_STATUS
```

Profile 由普通用户进程读取并通过有界 IPC 传递；服务不接受任意文件路径、shell、系统规则或裸 sing-box 配置。服务重启产生新 instance；过期 instance/revision 不执行。`switch` 仍由共享核心一次提交 DNS/route，旧流有界排空。

IPC 使用 Unix seqpacket。每条消息验证内核 SCM_CREDENTIALS，连接时核对 SO_PEERCRED；移交/继承的连接不能借用原用户身份。客户端在发送 Profile 前固定核对服务 UID 0。socket 父目录 root 所有且不可写，独立端点锁防止不同 state 目录抢占，帧上限 64KiB/5 秒/32 连接。服务仅允许四种操作，不按客户端自报 PID/UID 授权。

## 停止、移除与恢复

停止先撤销捕获，再停核心；SIGKILL 后由内核撤掉 socket-owned nft 表，下一次启动按 journal 精确恢复剩余策略路由。恢复不是重新连接。发生所有权冲突保留现场并返回失败。

包中的 `install.sh remove` 先验证已安装二进制/unit 摘要，停止并禁用 unit，运行 `xdial-service recover --state /var/lib/xdial-service`，成功后才删除对应二进制和 unit。策略与缓存目录保留供审查，不按名称批量清规则、杀进程或删除用户配置。未执行过本任务宿主安装/卸载。

## 当前能力和明确缺口

- 实现 IPv4 TCP/UDP 透明接入；应用路径/路径前缀/精确进程名交给 sing-box 原生进程搜索器，不信客户端归因。macOS Bundle 和进程名通配符尚未适配，明确拒绝。
- Direct DNS 与代理服务器 bootstrap 使用捕获的系统 resolver；应用代理 DNS 仍由共享编译器绑定精确 Line。标记套接字由内核按目标选择完整 Underlay，不虚构默认网卡、不重排路由。
- 当前仅支持 `/etc/resolv.conf` 一个非 loopback IPv4 nameserver。systemd-resolved stub、search/split/multiple DNS 需受保护原生后端；明确拒绝，不能改用公共 DNS 规避。DNS/接口事实变化事件先断开，尚无自动恢复。
- 仅允许经过共享编译和验证的离线规则缓存；尚无服务缓存导入/在线资源事务。客户端本地 file URL 和 NativeOptions 文件访问能力被拒绝，不能让 root 读取用户指定私有文件。
- AnyConnect/Tailscale 协议套接字与冷资源租约、IPv6、现有 mark/iptables 共存、GUI、完整配置库接线、多发行版安装/更新仍待完成。保留这些目标，不能将当前拒绝当作最终产品取舍。
- systemd unit 需要 `CAP_NET_BIND_SERVICE`，用于透明 UDP 回包绑定原目标的特权端口（如 DNS 53）；完整 Debian 负面/正面对照已验证。安装脚本不改变系统低端口阈值。

### 原生 DNS 后端接入前的审查结论

当前锁定 sing-box 的 `dns/transport/local/local_resolved_linux.go` 会读取默认接口的
resolved Link DNS，并为其创建绑定该接口的 transport；不能把直接启用这个 `local`
实现当作保留整个 Underlay 的 split DNS。后续后端必须保留系统 resolver 的原生
解析语义，同时保护 resolver 自身的出站依赖、观察其生命周期，避免捕获后递归。
Debian 完整来宾已含真实 systemd-resolved，可在同样隔离条件下进一步验收，不需要
修改共享宿主的 resolver。
