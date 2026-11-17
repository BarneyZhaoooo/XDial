# Windows 权限服务通信边界

2026-11-17；父提交 `68a1230` + 本阶段源码。`core/controlpipe` 是可执行、经原机
验证的本地 IPC 传输与鉴权层。**它还不是已安装的 Windows 服务或 WFP 数据面**；
不会恢复旧 daemon 已退役的数据面命令，也没有改动共享机器的服务、驱动或网络。

## 身份和权限

- 复用锁定版本的 `tailscale/go-winio`。管道名只能是 `xdial.` 前缀下的短本地名称，
  不接受 UNC 主机路径。首次创建排他，已有同名 pipe 不能被悄悄接管；上游创建
  标志拒绝远程客户端。原机测试覆盖同名冲突，未做跨主机连接攻击测试。
- 服务端 DACL 仅授予服务进程账号完整权限，以及安装器明确指定的 **logon SID**
  精确读写权限。客户端 ACE 不含 `FILE_CREATE_PIPE_INSTANCE`，不用会隐含该权限
  的 `GENERIC_WRITE`。登录会话与账号 SID 都必须是规范 SID，不能拼入 SDDL 文本。
- 读完每条有界消息后，调用 `ImpersonateNamedPipeClient`，从内核 thread token
  获取 User SID 和 logon group，逐条核对固定准入策略。匿名、查询失败或策略不符
  时关闭连接。请求不能自报 PID/UID 来获得权限；业务处理器只能使用独立传入的
  `Identity`，不得把 payload 中的身份字段当作授权。
- 身份查询在专用、锁定 OS 线程的短生命周期 goroutine 中完成，成功恢复
  `RevertToSelf` 后才允许业务处理器运行。恢复失败时不执行处理器，也不释放该
  线程回运行池；锁定 goroutine 退出，由 Go 销毁该线程。没有注入真实 OS 恢复
  失败，不能把此分支说成 Windows 故障注入测试已覆盖。
- 客户端连接只请求精确权限与 `SecurityIdentification`，发送 payload 前用内核
  pipe server PID 打开进程 token，核对安装器指定的 server SID。PID 只用于取得
  token，不作为授权依据。生产服务 SID/账号的可信来源必须由安装事务提供，
  不从未认证 pipe 的响应、用户消息或扫描结果自动接受。

依据：[Microsoft 管道身份文档](https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-impersonatenamedpipeclient)、
[管道权限文档](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights)。

## 请求与生命周期

每帧最长 64 KiB，长度检查先于内存分配；JSON 顶层未知字段和尾随消息拒绝。请求
携带版本和递增 ID，同一连接重复 ID 不再执行。服务只暴露构造时明确注册的操作，
没有任意 shell、可执行文件、文件路径或原始 nft/WFP 命令入口。处理器错误只返回
固定错误码，不把包含凭据的错误文本原样发出。

最多 32 条同时连接，连接/每帧 IO/处理器 context 有 5 秒上限。关闭服务会取消
处理器 context 并关闭已接受连接；业务处理器必须遵守 context、共享状态机及 CAS
提交契约，取消不自动承诺已提交操作回滚。客户端同连接请求串行，排队时可被自己的
context 立即取消；取消活动请求关闭该连接，避免迟到响应混入下次请求。

## 原机验证与边界

在已登记 Windows 10.0.26300 x64 测试机的独立任务目录执行本机 Go 1.26.5
交叉构建的测试程序；原机没有 Go，没有安装工具链、服务或驱动，没有提升权限。
测试只创建自己的随机 pipe 与自己的短生命周期子进程。

实际覆盖：

1. 合法内核身份多帧通信；未知操作拒绝且处理器不执行；重复 request ID 拒绝。
2. 匿名 SQOS 连接及与准入策略不符的 logon SID 被明确关闭，不以读超时当拒绝。
3. 客户端拒绝错误 server SID，发送业务内容前失败；非本地/非法名称拒绝。
4. 同名 pipe 首次创建冲突拒绝。读取实际内核 DACL，核对客户端 logon ACE 的精确
   mask、无创建实例权限，且无 Everyone/Anonymous 等额外主体。
5. 独立客户端进程经真实 pipe 完成请求，服务端获取实际 token；没有向服务端
   提供伪造 PID/路径来冒充 OS 身份。
6. 服务关闭取消阻塞处理器与客户端；等待同连接前一请求时，后一个请求的 context
   超时能及时返回，不等待前一业务处理结束。
7. 越界/零长度帧、顶层 UID/PID 声明、非法/尾随 JSON、超大响应拒绝。

测试使用同一账号的实际 Windows token；wrong-logon 是配置不匹配的策略测试，
不是另一个真实登录账号的联调。DACL 读回也不等于完成真实跨账号攻击测试。原生
Windows 测试未带 race；本机 wire 编解码 race 和 Windows 目标 `go vet` 通过。

后续必须接上独立服务进程/SCM 生命周期与安装事务、可信安装器的账号准入政策、
共享业务状态机的有限操作、WFP 原目标/程序身份/防环路和隔离系统测试。IPC payload
是控制消息，较大配置资源应另用有所有权和大小校验的资源事务，不开放任意文件读取。
权限服务与实际 Windows 客户端尚未完成，不能用本轮 IPC 测试替代按应用透明交付。

本轮未改既有 Apple/Android 入口；没有重复声称新的设备或 XCTest 验收。

## 最终证据

最终原机 8 个顶层测试通过；子进程专用入口在父进程枚举时跳过，由独立进程测试
实际启动并完成请求。原生执行与 `result.json` 均为 0。测试二进制 SHA256：
`9616135a21d4b6b44bcd50176a56e4e79ab8186f2455d9b15aabd332ddf32d3e`；
上传快照 `00a006c192d639c00cf0e6c078800d47aa88cbc9b609452c12ba9812faa08dfb`；
逐文件源码清单摘要 `27bc025b743d84b0a252f477224546491f05c12974c56f87f47211a5dc40afa2`。
记录如实为 `68a1230` + dirty，不改写事后提交，也不称为远端源码构建。

本地证据：`build/desktop/controlpipe-native-final.log`、
`controlpipe-native-final-result.json`、`controlpipe-wire-final.log`、
`controlpipe-vet-final.log`、`controlpipe-gate-final.log`。三个远端快照任务均只包含
本模块源码摘要与测试程序，执行后按精确 UUID 清理；没有留下安装服务或后台测试进程。

既有 `go-winio` 版本未变，go.mod 仅将它从间接依赖标记为本模块直接使用。Apple/
Android 的最近六构建/双 ABI 证据仍为上一阶段 `68a1230`，不冒称本轮重复执行。
