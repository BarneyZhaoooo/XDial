# 手机版企业预置示例

`Seed.example.json` 只演示格式，用的都是保留的示例域名，不应写入凭据、私有域名或真实规则地址。
实际的预置配置保存在仓库外或企业私有仓库里，打包时带进 App。

## 放在哪儿

构建时依次查找：

1. 构建设置 `XDIAL_IOS_PRIVATE_CONFIG_DIR` 指向的目录（必须存在）。
2. 被忽略的 `ios/PrivateConfig/`。

目录里可以有：

| 文件 | 作用 |
|---|---|
| `Seed.json` | 预置配置。没有这个文件就什么都不带，App 用空白起步配置。 |
| `RuleSets/manifest.json` | 随包规则清单：规则地址 → 同目录下的文件名。 |
| `RuleSets/*.srs` | 随包的规则文件。 |

## Seed.json

```
{
  "format": "xdial-mobile-seed",
  "schema_version": 1,
  "revision": 1,
  "region_scenarios": { "JP": "场景 ID", "*": "其它地区用的场景 ID" },
  "profile": { … }
}
```

- `profile` 与 App 里「导出配置」的 `profile` 相同，校验规则也相同。不得包含账号密码，
  用户首次连接前自己填写。
- `revision` 是预置的版本。预置内容有增删时加一，已经装了旧包的用户升级后会补上新内容。
- `region_scenarios` 可选：网络出口所在的地区（两位代码）对应哪个场景，`*` 是没列出的地区。
  留空时 App 只按场景登记的 Wi-Fi 自动选场景，也不会去查网络出口。

App 怎么用它：

- 本机没有配置：预置直接成为默认配置。「恢复默认配置」也回到它。
- 本机已有配置、且这个 `revision` 还没加过：只添加缺的线路、规则和场景，用户自己建的、
  改过的和当前选中的场景都不动。预置线路如果和已有线路是同一类型、同一个服务器，
  就沿用已有的那条。每个 `revision` 只加一次，用户之后删掉的不会再回来。

## 随包规则

远程规则平时在连接前现下。所在网络下不到、本机又没有缓存时，连接会直接失败。
把规则文件放进 `RuleSets/` 并在 `manifest.json` 里登记地址，App 会在连接前把它放进缓存，
之后引擎照常自己更新。

## 换一个开发者团队打包

工程默认的应用身份写在 `ios/project.yml` 顶部。企业包不要改这个文件，
打包时用 `xcodebuild -xcconfig <文件>` 覆盖：

```
DEVELOPMENT_TEAM = <团队 ID>
XDIAL_BUNDLE_ID = <主程序包名>
XDIAL_APP_GROUP = <App Group>
XDIAL_SHARED_KEYCHAIN_GROUP = <团队 ID>.<钥匙串分组>
CODE_SIGN_STYLE = Manual
CODE_SIGN_IDENTITY = Apple Distribution
XDIAL_PROFILE_APP = <主程序描述文件名>
XDIAL_PROFILE_TUNNEL = <隧道扩展描述文件名>
XDIAL_PROFILE_WIDGETS = <小组件描述文件名>
```

隧道扩展和小组件的包名是主程序包名加 `.tunnel`、`.widgets`。
