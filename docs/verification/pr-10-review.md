# PR #10 合入前回归与审查修复

## 2026-11-17 本轮修复

- 特权 Linux 配置准入按与 Go JSON 解码一致的大小写折叠比较字段名，递归覆盖订阅线路、URL 模板与 NativeOptions；客户端不能借大小写变体获得 root 文件读取能力。
- 规则副本通过同一已打开描述符 fstat 和有界读取；Unix 非阻塞打开先拒绝 FIFO，避免 stat/read 间替换造成准备线程阻塞。
- 场景选择器使用离线配置结构校验，允许当前场景的线路组尚未配置，仍能切到其它有效场景；真正连接继续执行连接计划校验。
- Android 首次自动连接会申请精确位置权限（同时声明 coarse），系统定位关闭或拒绝权限时明确提示 Wi-Fi 条件不可用，必须确认仅按地区继续或返回；系统 VPN 授权保持独立。
- Android Tailscale 设置复用编译器完整 Direct route，保留 auto_detect_interface，独立 DNS socket 继续通过 protect/bind。
- Linux 升级查询明确忽略 systemctl 对 disabled/inactive 的预期非零状态，仍按精确输出允许列表准入。

本轮按以上边界归并审查意见；没有增加缓存、锁、重试或后台任务。

## 验证

本机 Go 1.26.5：mobile 全量 race；config/libbox/subscription 常规全量；新增 FIFO/超限/正常规则读取、不可运行场景可选择、Android setup route 保护回归通过。
Linux 目标 libbox/linuxinstall/CLI vet 和 public-content-gate 通过。
Ubuntu 24.04 x64 原机实际执行 Linux 准入与 FIFO/超限两项测试通过，native/result 均 0；
本机交叉编译后执行，无系统网络或权限更改。快照摘要
`02641febbcd451908d8bca9e06eed79a8a5940c2480551154f0abc87c747da26`，
收据 `build/desktop/pr-round3-linux-{native,fetch}.json`。
Android 双 ABI AAR/APK 重建、签名与包内容检查通过，产物位于
`build/android/artifacts/xdial-1-debug-564a7671f475-913c4f3304f5/`。
本轮尚未新增 Android 设备权限交互或已认证 Tailscale 出口联调证据。
日志为 `build/desktop/pr-round3-{core,mobile-race,file,linux-vet,gate,android}.log`。

## macOS Debug 条件

此前用户已授权本机 Debug 安装重启。本轮只读查询 Debug Server，安装事务仍返回
`helper-approvalRequired`，应用已就绪，后台 helper 未批准，网络扩展尚待启用。
需要在 macOS 系统设置允许 XDial Debug 后台服务后重新执行安装/重启验收。
这不等于 Debug 全流程通过，不能在此状态下按本 PR 的授权条件合入。
现有生产凭据与当前 VPN 不用于验证；不通过系统偏好文件或权限绕过代替用户批准。
