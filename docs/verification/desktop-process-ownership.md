# Windows/Linux 核心进程归属验收

2026-11-17；父提交 `64bbcc6` 加本轮未提交源码。这里只验收核心进程的启动与
生命周期，不代表 Windows/Linux 按应用透明接入、原生客户端或安装事务已完成。

## 实现与边界

- Windows 从原来的未实现分支改为原生 `CreateProcessW`。使用 Windows 10+
  `PROC_THREAD_ATTRIBUTE_JOB_LIST` 在创建时加入独立 Job，启用
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。只显式继承 stdin/stdout，不继承 Job；
  父进程退出关闭最后一个 Job 句柄时回收所属进程树。关闭一个实例不清理其他实例。
- 不使用“先创建挂起进程、再加入 Job”的窗口；父进程在两步之间退出会遗留进程。
  API 不支持时启动明确失败，不回退为无归属进程。
  参考 [Microsoft 原子 Job 创建说明](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812/)
  与 [Job 生命周期](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)。
- Linux 直接启动核心，`Pdeathsig=SIGKILL`；在整个租约期锁定创建子进程的 OS
  线程，因为 Linux 的父死亡信号跟随创建线程。停止或自然退出释放该线程。
  本实现保证直接核心子进程的归属，不声称回收任意孙进程树。
- Windows/Linux 删除旧的 `taskkill /IM`、`pkill -f` 全局清理。没有归属证据的旧
  进程不按名字杀死；旧版残留进程的恢复仍需明确的安装/恢复事务。
- macOS 继续使用原来的管道 sentinel；本次只适配内部 command/lease 类型。
  复用进程对象启动时复位停止标记，避免随后异常退出被误认为用户主动停止。

## 实际原机测试

本机 Go 1.26.5 交叉构建测试可执行文件，随后上传审查过的最小源码/产物快照，
在登记测试机**原生执行**。远端没有 Go，未宣称远端源码重建。没有安装驱动、
修改权限、DNS、路由、VPN、CI 服务或其他测试任务。

快照摘要：`b09b2c4c0d3e7134609f3df6bcffd20e6bcdc4f5db8c02ea0ae0eada7244c7c5`。
`source-manifest.json` 保存父提交、dirty 状态、源文件和二进制摘要。快照是显式
非 Git staging，辅助传输 manifest 的空 Git HEAD 不替代这份源码归属记录。

| 平台 | 原机 | 测试可执行文件 SHA256 | 结果 |
|---|---|---|---|
| Windows x64 | Windows 10.0.26300 | `dbfc80e4e8675064688367f0a70129dcdfef907a02201d6065a35f2c57d8d4a1` | 4 项通过，原生和收回 result 均 exitCode 0 |
| Linux x64 | Ubuntu 24.04 | `7a6d98bffa9d1ff78492de0adf1b792ea213e6a46b9a9b59c95e561b8d85f02a` | 3 项通过，原生和收回 result 均 exitCode 0 |

执行 `engine-<platform>.test[.exe] -test.v -test.run=TestOwned -test.timeout=90s`。
测试使用同一可执行文件的受控子进程模式和真实 `127.0.0.1` TCP echo：

1. 路径包含空格、中文、引号、分号、`&` 时 argv 保持原样。
2. 两个同名实例同时监听；重复关闭第一个租约，第二个仍可收发。
3. 强制结束独立父进程后，其核心子进程的监听口消失。
4. 非零退出正常传播，不存在的可执行文件启动失败。
5. Windows 额外检查核心属于精确 Job，并验证关闭 Job 后子孙两个监听口都消失。

这些是实际 OS 进程/回环流量测试，**不是** sing-box 出口、WFP、TUN 或企业 VPN
联调。未改变测试机现有网络。Ubuntu 用户网络命名空间探测被权限拒绝，尚无可用
隔离 VPN 测试环境；没有擅自修改 sysctl、用户组或使用提权绕过。

本地收据：`build/desktop/windows-native-tests.log`、`ubuntu-native-tests.log`，
对应保存的命令与 result JSON；完整传输收据保存在本机 fleet task 目录。

## 兼容性验证

- `go test -race -tags with_gvisor,with_utls ./core/engine ./cmd/xdial` 通过。
- iOS/tvOS generic 模拟器、macOS Debug/Release App/扩展六构建成功；日志
  `build/desktop/apple-build.log`。本轮无新增 XCTest，不代表 iOS 完整运行验收。
- Android 双 ABI AAR/APK 重新构建及产物校验通过，日志
  `build/desktop/android-build.log`。APK SHA256
  `505db30e694a1823209ffb82f57481fff26b5fcab85d39d836c06fd3a0e1a6d9`，
  `build/android/artifacts/xdial-1-debug-505db30e694a-206be4727ae9/`。
  manifest 如实为 `64bbcc6` + dirty；本轮未安装该包或新增 Android 设备交互证据。
- public-content gate 通过；没有推送、PR、外部发布或宿主 Debug 重启。

## 后续接入门槛

本地 daemon 的 `peercred_other.go` 仍直接放行，不能作为权限服务安全边界。
下一步应补原生 IPC 对端鉴权和服务归属，再接 Windows WFP/应用身份及 Linux
透明入口/应用身份；逐项验收安装、撤销、升级、故障回滚。Windows 驱动签名和
Linux 隔离测试环境尚未具备。多发行版、透明 TCP/UDP/DNS、防环路及原生窗口
仍在原始交付范围内，不能以本轮进程验收替代。
