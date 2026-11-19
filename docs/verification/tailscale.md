# Tailscale 运行证据

Line、setup session、Underlay 与 Commit 的语义见 [ARCHITECTURE.md](../../ARCHITECTURE.md)。
桌面 setup 是 helper 中的配置会话，Provider 中的 active / prepared-switch runtime
属于连接事务；它们共享持久身份，但状态和生命周期不同。

## 结构化来源

| 来源 | 表达的事实 |
|---|---|
| 设置页 Line 卡片 → `GoEngine.tailscaleStatus` → helper 的 `tailscale-status` | setup session 的 LocalAPI 登录态与可选 exit node；不是 Provider 已连接的证明 |
| `Libbox.TailscaleStatus` / `PreparedSwitchTailscaleStatus` | 对应 runtime 的 LocalAPI 状态，以及 control、magicsock 的有限诊断 |
| `exit_nodes` | `online`、`selected`、`in_network_map`、`in_magic_sock`、`in_engine`、收发计数和 `has_handshake` |
| `readiness.control` / `readiness.derp`、节点的 `derp_path` | control generation、Home DERP 状态与连接 generation，以及收发、失效、关闭等 sequence |
| `ProbeTailscalePeer` / `ProbePreparedSwitchTailscalePeer` | 对单个 peer 发起真实 Disco / TSMP 探测，返回路径类别、延迟和错误类别 |
| 当前 `ConnectionReport` 与 Debug 路由探针 | 连接提交结果，以及具体流量的 Line 归因；HTTP 入口见 [debug-server.md](../agent/debug-server.md) |

setup IPC 见 [GoEngine.swift](../../macos/Sources/XDial/GoEngine.swift) 与
[daemon.go](../../cmd/xdial/daemon.go)。Provider 数据定义与读取见
[tailscale_status_gvisor.go](../../core/libbox/tailscale_status_gvisor.go)；消费与证据投影见
[EmbeddedSingBoxRuntime.swift](../../macos/TransparentProxyExtension/EmbeddedSingBoxRuntime.swift)
和 [TailscaleReadiness.swift](../../macos/Shared/TailscaleReadiness.swift)。这些 Libbox 方法不是
Debug Server HTTP 动作。

## 结论范围

`has_handshake` 仅表示 LocalAPI 的 `LastHandshake` 非零，不表示本轮 fresh handshake。
exit node 在线、已选中且存在于三份运行结构中，仍可能出现 `tx > 0 / rx = 0` 的 peer
handshake 失败；该组合本身不定位到登录、Underlay 或某个外部服务。

NetInfo 本地调用返回、control HTTP 接受、远端消费 map、fresh handshake 与真实出口是
不同证据。DERP ready/client 数是点时快照；相同汇总不能排除采样间的重连，sequence 与
generation 才能表达期间变化。单端状态不证明远端已消费同一张 map。

出口 IP、单次 HTTPS 和路由表分别只覆盖各自观察；TCP、系统 DNS 和普通 UDP 的实际
Scenario 出口需要对应流量证据。远端进程重启后的成功属于新的运行现场。

## iPhone 独立设置的后台生命周期

不变量：后台停止完成后，仅当 App 仍 active 才恢复设置；否则保留待恢复线路，
直到下次 active。已开始后台停止时，必须等运行时释放资源才结束后台任务；用户主动完成的设置不自动恢复。

所有权：`TailscaleSetupBackgroundGuard` 独占场景 active 标记、宽限期任务、后台任务标识和
停止中的标记；`AppState` 保存待恢复线路并检查线路是否仍适用；运行时独占资源和 idle 状态。
延迟恢复只由保护对象的停止完成路径发起，不在 AppState 排第二份回调。

| 事件 | 设置运行、未停止 | 后台宽限期 | 正在停止 | 已暂停 |
|---|---|---|---|---|
| background | 标记非 active，申请后台时间 | 不重复申请 | 标记非 active，继续等停止 | 标记非 active，不重启 |
| active | 无待恢复线路时不操作 | 取消宽限期，结束后台任务，继续原设置 | 只记录 active，不抢先恢复 | 检查线路后恢复一次 |
| inactive | 标记非 active，不改变运行时 | 标记非 active | 标记非 active | 保留待恢复线路 |
| 宽限期结束 / 系统到期 | 无待处理任务时不操作 | 开始停止 | 不重复停止 | 无待处理任务 |
| 未取得后台时间 | 立即停止 | 不适用 | 继续等原停止 | 无待处理任务 |
| 停止完成 | 不适用 | 不适用 | 清停止标记，结束有效后台任务，仅 active 时尝试恢复 | 不适用 |
| 重复 active / background / 到期 | 无待恢复线路不重启 | 不重复申请 | 不增加恢复回调或停止操作 | 等待下一次 active |

恢复统一经过 `canStartTailscaleSetup`：线路已删除、已有其它设置或正式连接正在接管时不恢复。
已发出的启动结果沿用 AppState 的 generation 检查，后台停止会使旧结果失效。

`TailscaleSetupBackgroundTests` 覆盖同步和延迟停止、系统拒绝后台时间、主动完成设置，
以及停止期间 0–5 次前后台交替；每条路径检查停止只发起一次、后台任务只结束一次，
最终在后台时不启动、之后回到 active 恢复同一线路且只恢复一次。
