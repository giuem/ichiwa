# 参与开发

欢迎 issue 和 PR。报告问题时写清楚系统版本、应用版本（侧栏左下角）和复现步骤；播放或加载失败的话，附上数据目录里的 `sdk.log`（位置见 [README](README.md#数据存在哪里)）。

改动比较大，或者要加新功能的话，先开一个 issue 聊一下方向，免得白做。

## 开发环境

需要 Go 1.27.3 或更高版本（以 [go.mod](go.mod) 为准），不需要 cgo。macOS 上可以直接开发；Windows 和 Linux 能编译和跑测试，但应用还没在真机上验证过，进度见 [#15](https://github.com/giuem/ichiwa/issues/15)。

```sh
git clone https://github.com/giuem/ichiwa.git
cd ichiwa
go tool mygo dev     # 运行应用，改动后自动重启
go test ./...        # 测试
go tool mygo build   # 打包到 build/<系统>-<架构>/
```

自己打包的应用是临时签名，只适合在本机运行；版本号显示 `0.0.0-dev`，不会检查更新。

`ICHIWA_TOUR=<目录> go tool mygo dev` 会让应用自动浏览各个页面，截图到该目录后退出，改界面后可以用它检查一遍。

## 提交前检查

CI 会跑下面这些，提交前在本地跑一遍：

```sh
gofmt -l .                       # 应该没有输出
go mod tidy                      # 不应该改动 go.mod 和 go.sum
go vet ./... && go tool mygo vet .
GOFLAGS=-tags=nodynamic go test -race ./...
```

CI 在 macOS、Windows 和 Linux 上都会跑测试和构建。

## 约定

- 一个 PR 只做一件事。提交说明用英文祈使句，说清楚改了什么、为什么。
- 界面文字用简体中文，写给普通用户看：短句，说结果，不说实现。
- 改动会改变用户得到的东西（修复、新功能、行为或界面文字的变化）时，在 [CHANGELOG.md](CHANGELOG.md) 顶部的 `## Unreleased` 下加一句给用户看的说明（没有这个标题就加上）。只改文档、测试、CI 或重构的不用写。
- 不要改 `mygo.json` 里的 `version`，也不要打 tag。版本号和发布由维护者用脚本完成，见 [RELEASING.md](RELEASING.md)。

## 许可

提交到本仓库的贡献以 [AGPL-3.0](LICENSE) 发布，和项目其余部分相同。
