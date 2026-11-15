# 场景运行状态验收

PR #58 仅在现有圆球布局上增加运行状态说明；原纵向列表及其离屏截图已移除，
圆球、悬停操作、失败与仅 SOCKS5 标识、缺项提示沿用 main。

## 状态说明约束

运行状态由 GoEngine.status 与 AppState.wakeReconnectPhase 持有；顶部说明只投影这些事实，
不写入引擎或连接事务。连接准备、断开和重连不能从 isBusy 推断为同一种状态。
报告只决定已连接时的线路类别，不得用旧报告覆盖进行中的断开或恢复。

| 宿主恢复阶段 | 引擎状态 | 缺失、旧成功或失败报告的处理 |
| --- | --- | --- |
| 任一唤醒恢复阶段 | 任意 | 与状态带共用 WakeReconnectPhase 文案，不宣称就绪 |
| 无 | connecting | 准备线路，不引用旧成功报告 |
| 无 | disconnecting | 正在断开，不表述为启用 |
| 无 | reconnecting | 正在重连，不表述为首次启用 |
| 无 | disconnected | 未启用，忽略过期报告 |
| 无 | connected | 只按完整的已提交报告分类；缺失或失败报告显示未确认 |
| 无 | 未知 | 未确认，不推断直连或 VPN |

场景切换准备期间，源连接仍为 connected 且报告仍属于已提交源事务，因此保留源线路说明。
完成、取消、断开、重新连接或延迟到达的报告均按上表重新投影，无独立缓存或清理状态。

## 已验证（2026-11-15）

Xcode 27.1 / macOS arm64，以下 12 项定向测试通过：

- ScenarioConnectionPresentationTests（10 项）：纯直连、已提交 VPN、未提交或失败事务、
  未启用的过期报告、切换期间旧场景、未知类型、订阅加直连与仅订阅、
  订阅未就绪、VPN 加订阅、报告缺失时的中英文状态文案（不引导查看不存在的详情）。订阅策略字符串不能冒充线路类型。
  生命周期矩阵同时覆盖连接、断开、重连、唤醒恢复、旧报告与缺失报告。
- ScenarioConnectionViewsTests（1 项）：状态说明组件中英文、浅深色及长名称渲染。
- ScenarioGridLayoutTests（1 项）：保留圆球均衡布局。

运行命令（先用 xcodegen generate --spec macos/project.yml 生成项目）：

```sh
xcodebuild -project macos/XDial.xcodeproj -scheme XDialTests \
  -configuration Debug -destination 'platform=macOS,arch=arm64' \
  -derivedDataPath /tmp/xdial-pr58-tests CODE_SIGNING_ALLOWED=NO \
  -only-testing:XDialTests/ScenarioConnectionPresentationTests \
  -only-testing:XDialTests/ScenarioConnectionViewsTests \
  -only-testing:XDialTests/ScenarioGridLayoutTests test
```

另通过 MainPopover.swift 语法检查及 git diff --check。

## 尚未验证

已使用本机官方 Go 1.26.5 尝试 make app，仅构建候选包，不安装或启动。
完整构建仍被现有 Makefile 的空格路径处理阻断：
默认构建目录会将绝对路径拆成多个 make target 与 Go 参数；
改用无空格 BUILD_DIR 时，补丁脚本仍输出到仓库内 build/patched-go，
与调用方期望的 GOWORK 路径不符。此外本机未安装 gomobile / gobind，
尚无本工作树的 macOS Libbox.xcframework。

未完成完整 App 构建、签名、安装、Debug 重启或真实直连、AnyConnect、
订阅与切换验收。未修改当前网络配置或凭据；这些运行验证仍需完整构建环境。

## 维护者提供的验证

维护者在 PR #58 的 [f3a9646 复审](https://github.com/kafeifei/XDial/pull/58#pullrequestreview-5909550210)
报告 make ci-macos-build（Debug / Release 未签名）与 make test-macos-transaction 通过，
918 项测试 0 失败。该结果属于上一版提交，不替代本轮修改后的完整构建或 Debug 实测。
当前任务仍未修改网络配置或凭据，直连、AnyConnect、订阅以及切换、断开、重连的 Debug
运行验收尚待具备构建环境和可用测试配置的维护者完成。
