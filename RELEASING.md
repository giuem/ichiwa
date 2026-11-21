# 发布

红果短剧常常发布，有东西可发就发，一天几次也可以。没人手动构建：推送到 `main` 的人写好说明、跑一条命令，[`.github/workflows/release.yml`](.github/workflows/release.yml) 完成其余的，已安装的应用随后自动更新。

## 什么时候发布

推送到 `main` 的人决定这次推送是不是一次发布：

- **发布**：改动会改变用户得到的东西，比如修复、新功能、行为或界面文字的变化、安装包里的内容。
- **不发布**：只改文档、测试、CI、开发工具，或者不改变行为的重构。这些改动搭下一次发布的便车。
- 拿不准就发布，发布很便宜。

## 版本号

版本号是 `YYYY.MDD.N`：发布当天的日期（Asia/Shanghai；月份不补零，日期两位）加当天的第几次发布（从 1 开始）。例如 `2026.1107.1`、`2026.1107.2`、`2027.105.1`。每一段都是没有前导零的整数，所以是合法的 semver，也适合 macOS 的 bundle 版本。tag 是 `v<版本>`。

版本号从不手写，也不写在源码里：`mygo.json` 里的 `version` 永远是 `0.0.0-dev`，发布时 workflow 按 tag 写进去。从源码构建（`mygo run`、`go build`）显示 `0.0.0-dev`，不会检查更新。`scripts/release.sh next` 打印下一个版本号，会数本地和 origin 上今天已有的 tag。

## 发一个版本

1. 把说明写在 [`CHANGELOG.md`](CHANGELOG.md) 顶部的 `## Unreleased` 下（没有这个标题就加上；之前几次推送的说明可能已经在那里等着）。写给用户看的短句：对他们来说变了什么，而不是怎么改的。
2. 测试通过（`go test ./...`），在最新的 `main` 上、没有别的待提交内容时运行：

   ```sh
   scripts/release.sh tag -push
   ```

   它把 `## Unreleased` 改成新版本号，提交（提交说明就是版本号），打 tag `v<版本>`，再用 `git push --atomic` 把 `main` 和 tag 一起推送。`-n` 只显示会做什么；`-trailer "Co-Authored-By: …"` 给提交说明加一行结尾。`CHANGELOG.md` 里 Unreleased 下没有内容、工作区不干净、或者 `main` 落后于 `origin/main` 时它会拒绝。推送被拒绝时它会撤销提交和 tag，`git pull --rebase origin main` 之后再来一次。
3. tag 启动 release workflow：检查 tag、跑测试、建 release（说明是 CHANGELOG 里这一版的内容）、构建 Apple 芯片和 Intel 两个架构的 `.dmg`、更新包和清单，上传并**自动发布**为最新版本。用 `gh run watch` 看进度。

不推送的改动不碰 `## Unreleased`。不要改已发布版本的标题，不要移动或复用 tag，也不要绕过脚本手推 `v*` tag。

workflow 失败后，修好问题再用 `gh workflow run release.yml -f tag=v<版本>` 重新发布同一个 tag（它用这个 tag 指向的提交构建，所以 CHANGELOG 必须已经在那个提交里）；如果问题要改代码，就发一个新的版本。

## 仓库 Secrets

| Secret | 用途 | 必需 |
|---|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | 给更新包签名。内容是 `mygo-update.key`，公钥在 `mygo.json` 的 `updates.publicKey` | 是 |
| `MACOS_CERTIFICATE`、`MACOS_CERTIFICATE_PASSWORD` | Developer ID 证书（base64 编码的 `.p12`） | 否 |
| `APPLE_ID`、`APPLE_TEAM_ID`、`APPLE_APP_PASSWORD` | 公证 | 否 |

私钥由 `go tool mygo keygen` 生成，存在 `~/Library/Application Support/mygo/update-keys/`。**不要提交到仓库**；丢失后已安装的应用将无法再更新。

## 用 Developer ID 签名

没有证书时应用是 ad-hoc 签名，用户首次打开需要去掉隔离属性（见 README）。有了 Apple Developer 账号后：

1. 把上表中可选的 secrets 填上；
2. 在 `mygo.json` 的 `macos` 下加上：

   ```json
   "signingIdentity": "Developer ID Application: 你的名字 (TEAMID)",
   "notarize": { "keychainProfile": "notary" }
   ```

## 注意

自动更新从 `updates.github` 指向的仓库读取最新 release，**这个仓库必须是公开的**。

以前的版本是 `0.2.0-beta.N`（带 `-` 的是预发布版本，普通用户收不到）。改成日期版本号后每个版本都是正式版本，所有用户都会收到，所以发布前要像对待正式版本一样确认。
