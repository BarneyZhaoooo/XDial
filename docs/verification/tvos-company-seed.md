# Apple TV 公司预置验收（2026-11-20）

正式定位为公司内部使用，保留企业预置。代码、XD 身份、图标、隐私告知与无 LAN 实验仍在同一个 PR #47。
私有来源与凭据不进入公开仓库；本轮只用 `ios/Examples/Seed.example.json`，没有读取公司私有配置。

## 代码与实际构建

- tvOS 工程引用手机的 `MobileSeed` 与移动配置校验器，RootView 将包内 seed 交给 AppState；
  模拟器演示数据只在没有公司预置时添加。文件导入 UI 的 `FileDocument` 仅编入 iOS，未改手机行为。
- `XDIAL_PRIVATE_CONFIG_DIR` 必须显式提供才注入。缺失 `Seed.json` 的显式来源直接失败；
  每次重建先清除旧 `PrivateConfig`，无预置包不回落到开发机私有目录。
- AppState 先恢复用户凭据，再应用更高 revision 的补充；配置与安全库成功提交后才推进 seed 版本，
  旧版 Profile 迁移也走同一补充逻辑。
- 公司包门禁必须带 `--seed-authority`；核对 seed 原文（含 revision、迁移历史）、全部随包规则文件，
  再经 `xdial-seed check --platform tvos` 验移动格式、引用和语义。默认无预置模式继续拒绝私有资源。

验证命令：

```sh
source ~/Code/XD/.tools/env.sh
xcodegen generate --spec appletv/project.yml
# Release + generic/platform=tvOS Simulator，本地无签名编译及 ad-hoc 签名均通过。
# 未构建签名发行归档、上传发行包或访问 Apple 账号。
python3 -m unittest discover -s scripts/tests -p test_tvos_store_app.py -v
go test ./core/seedcheck ./cmd/xdial-seed
make public-content-gate
```

门禁测试共 7 项：6 项合成产物检查，以及可选的实际构建产物检查（设置 `XDIAL_TVOS_TEST_APP`）。
本轮实际示例预置构建共 7 项通过；CI 未提供 tvOS 产物时明确跳过最后一项。
覆盖空包、缺 seed、没有授权来源、seed/revision 漂移、同源但格式无效、离线规则漂移、
多余私有文件与签名密钥。另在同一 derived data 内重建无预置包，实际产物通过无预置检查，无旧 seed 残留。

## 启动与升级

使用独立 tvOS 27.0 Apple TV 4K 模拟器，不操作实体电视；安装回执逐文件核对 `matchesLocal=true`。
本地 ad-hoc 签名保留钥匙串权限；完全无签名产物的钥匙串不可用，不能用于持久化验收。

1. 示例 revision 3 首启，确认首页当前场景为「在本地区」，场景列表有示例中的四个场景，隐私告知仍先展示。
2. 用遥控器主动选择「在公司」并保存。
3. 不清理用户数据，覆盖安装示例 revision 4（在同一份示例上增加 `Seed upgrade` 场景）。
4. 冷启动仍显示当前场景「在公司」，列表出现 `Seed upgrade`，证明启动确实加载并执行 seed 升级，未覆盖用户当前选择。

这不是公司私有 seed 的验收，也不是签名发行包或真实网络连接验证。
正式交付还须对授权公司来源、归档及导出 IPA 分别运行完整签名/身份/引擎/资源门禁，
独立验证真机授权、连接、断开与 GPL/版权。渠道由 Dash 另选，旧 tvOS 70 不作为新公司正式包。
