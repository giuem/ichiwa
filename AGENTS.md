# 一集

## 发布

一集常常发布，有东西可发时一天几次也可以。版本号是发布当天（Asia/Shanghai）的日期加当天的第几次发布，`YYYY.MDD.N`（`2026.1107.2` 是 2026 年 11 月 7 日的第二次发布），从不手写。[RELEASING.md](RELEASING.md) 说明整个流程。

推送到 `main` 的人决定这次推送是不是一次发布：

- **发布**：改动会改变用户得到的东西：修复、新功能、行为或界面文字的变化。
- **不发布**：只改文档、测试、CI、开发工具，或者不改变行为的重构。这些改动搭下一次发布的便车。
- 拿不准就发布。

发布：

1. 把说明写在 `CHANGELOG.md` 顶部的 `## Unreleased` 下（没有就加上）。写给用户的短句：变了什么，而不是怎么改的。和改动一起提交。
2. 在最新的 `main` 上、没有别的待提交内容时运行 `scripts/release.sh tag -push -trailer "Co-Authored-By: <你的提交结尾的那一行>"`。它给说明版本号，提交，打 tag `v<版本>`，把 `main` 和 tag 一起推送；tag 启动 workflow，构建、签名并发布 release，不要自己构建。`-n` 显示它会做什么。
3. 推送因为 `origin/main` 变了被拒绝时，脚本会撤销它的提交和 tag：`git pull --rebase origin main` 后再运行。

每天 00:00（Asia/Shanghai）`.github/workflows/nightly.yml` 运行 `scripts/release.sh auto`：上次发布以来 `main` 上有代码改动（文档、测试、CI、开发工具以外的改动，比如 Renovate 合入的依赖升级），就把它们发布出去；`## Unreleased` 空着时它替用户写上笼统的说明。所以改变用户体验的改动也可以只写好 `## Unreleased`、推送，留给夜里发布；要马上发就照上面自己发。

只改文档、测试、CI、开发工具的推送不碰 `## Unreleased`。不要改已发布版本的标题，不要移动或复用 tag，不要绕过 `scripts/release.sh` 推 `v*` tag。
