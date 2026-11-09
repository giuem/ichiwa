# 发布

发布由 [`.github/workflows/release.yml`](.github/workflows/release.yml) 完成。已安装的应用会在 release 发布后自动更新。

## 发一个版本

1. 确认测试通过：`go test ./...`
2. 在 [`CHANGELOG.md`](CHANGELOG.md) 顶部加上新版本的一节，标题必须正好是 `## 0.1.1`，否则 release 说明会改用 GitHub 自动生成的内容。
3. 修改 [`mygo.json`](mygo.json) 里的 `version`。
4. 提交、打 tag、推送：

   ```sh
   git commit -am "0.1.1"
   git tag v0.1.1
   git push origin main v0.1.1
   ```

   tag 必须和 `mygo.json` 的版本一致，否则 workflow 会停下。
5. 等 workflow 跑完（`gh run watch`）。它会建一个 **draft** release，以 CHANGELOG 里这一版的内容作为说明，上传 Apple 芯片和 Intel 两个架构的 `.dmg`、更新包和清单。
6. 检查 draft，然后发布：

   ```sh
   gh release edit v0.1.1 --draft=false --latest
   ```

## 发一个预发布版本

版本号带 `-` 的是预发布版本，例如 `0.2.0-beta.1`。步骤和上面的 1～5 相同（CHANGELOG 的小节写 `## 0.2.0-beta.1`），CI 同样只生成 draft。检查 draft 后这样发布：

```sh
gh release view v0.2.0-beta.1 --json isDraft,isPrerelease,assets
gh release edit v0.2.0-beta.1 --draft=false --prerelease
```

- **必须带 `--prerelease`，不要带 `--latest`**，否则普通用户也会收到这个版本。
- 普通用户不会收到预发布版本。已装预发布版本的用户会在下一个正式版发布后自动更新，收到后续预发布版本则需要应用调用 `mygo.Updater.SetPrereleases(true)`。
- 发现问题就发新的版本（`beta.2`），不要覆盖已发布的 tag。

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
