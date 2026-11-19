# App Store 隐私对账（iPhone）

公开版的数据使用说明（`ios/XDialIOS/DataUseNoticeView.swift`）、隐私政策与 App Store 隐私标签都以这份清单为准。
代码新增或改变对外请求时，同步更新这里、说明文案与 `AppState.dataUseNoticeVersion`。

## 自动访问的第三方服务

| 用途 | 目标 | 进程 | 何时 | 代码 |
|---|---|---|---|---|
| 域名解析 | Cloudflare 1.1.1.1:53，未加密 UDP；AnyConnect 线路改用其下发的 DNS；Tailscale 名称由 Tailscale 解析 | 扩展 | 连接期间的所有设备查询 | `core/config/generator.go`（公共 DNS）、`core/libbox/mobile_dns_transport.go` |
| 连通性检测 | `1.0.0.1/cdn-cgi/trace`、`1.1.1.1/cdn-cgi/trace`、`www.apple.com/library/test/success.html`；首个失败时 `223.5.5.5` | App | 每次连接与连接变差时 | `appletv/XDialTV/TunnelManager.swift`、`appletv/XDialTV/Models.swift` |
| 节点测速 | `www.gstatic.com/generate_204`（或订阅指定） | 扩展 | 连接期间每 300 秒（自动选节点） | `core/config/generator.go` |
| 出口地址 | `checkip.amazonaws.com`，其次 `1.1.1.1/cdn-cgi/trace` | 扩展 | 每次连接，按线路 | `core/libbox/libbox.go` |
| 出口地区 | `gspe1-ssl.ls.apple.com/pep/gcc`，其次 Cloudflare trace | App / 扩展 | 仅当预置带 `region_scenarios`（公开版没有，不会发生） | `core/libbox/exit_country_probe.go`、`ios/XDialIOS/ScenarioAutoPilot.swift` |
| 规则文件 | 配置写明的地址（如 `raw.githubusercontent.com`） | App | 连接时缺少缓存才下载 | `core/libbox/configgen.go` |
| 订阅 | 用户填写的地址 | App | 仅用户添加或刷新时 | `core/libbox/subscription.go` |

以上请求都不带账号、密码或配置内容；对方能看到设备的 IP 地址。

## 发往用户自己的服务器

- AnyConnect：按协议发送设备名、网卡地址与由它们生成的设备标识（`build/patched-go/sslcon`，补丁见 `patches/sslcon`）。
- Tailscale：设备名与标准节点信息发往 Tailscale 控制服务器；不上传日志（tailscale fork 固定 `NoLogsNoSupport`）。

## 不离开设备

账号与密码在钥匙串；Wi-Fi 名称只在本机比对场景，不请求定位权限；连接时间线与诊断（含出口 IP）在 App Group，
只有用户导出或分享才离开设备；小组件、快捷指令、实时活动只显示连接状态和场景名；通知默认关闭、在本机产生。
没有开发者运营的收集服务器，没有广告、分析、崩溃收集或追踪 SDK。

## 隐私清单（PrivacyInfo.xcprivacy）

App 与 Packet Tunnel 扩展声明：UserDefaults（App：CA92.1、1C8F.1；扩展：1C8F.1）、SystemBootTime 35F9.1
（`ProcessInfo.systemUptime` 与 Go 运行时的 `mach_absolute_time`，用于计时）、FileTimestamp C617.1
（App Group 内缓存与状态文件的 `stat` 系列）。不收集数据类型，不追踪。

移动端 Libbox 以 `xdial_omit_tailssh` 标签构建（`Makefile` 的 `MOBILE_LIBBOX_TAGS`，
补丁 `patches/sing-box/0018`），不链接 Tailscale SSH 服务端，因此不含 SFTP 的磁盘空间 API（`statfs`）；
`make check-mobile-libbox-deps` 发现 `github.com/pkg/sftp` 时失败。核对方法：对 Release 真机包的
`XDial` 与 `XDialIOSTunnel` 运行 `xcrun nm -u`，只应出现已声明类别的符号。
