# 红果短剧 for Mac

一个原生的红果短剧 macOS 客户端。界面用 [MyGo](https://mygo.egoist.dev/) 的 Native UI 写成（纯 Go，GPU 绘制，没有 WebView），数据来自 [openhongguo](https://github.com/beilunyang/openhongguo)。

> **免责声明**：本项目仅供学习和研究，禁止用于商业用途。所有内容的版权归原平台和版权方所有。使用本项目即表示你同意 [openhongguo 的免责声明](https://github.com/beilunyang/openhongguo#免责声明)。

## 功能

- 推荐、真人剧、漫剧频道，排行榜，搜索与热搜榜
- 按体裁、主题、设定等条件筛选短剧
- 演员榜与演员主页（简介、参演作品）
- 原生播放器：倍速、音量、全屏、自动连播、断点续播、下一集预加载
- 选集面板（长剧按 50 集分段）、相关推荐
- 多季短剧切换季、播完自动接下一季，预告季显示上线时间
- 继续观看、追剧
- 菜单栏、媒体键与控制中心“正在播放”
- 跟随系统的浅色/深色外观，侧栏毛玻璃
- 自动更新

## 安装

从 [Releases](https://github.com/giuem/hongguo-mygo/releases) 下载 `.dmg`。目前没有用 Developer ID 签名，第一次打开前执行：

```sh
xattr -dr com.apple.quarantine /Applications/Hongguo.app
```

需要 macOS 13 或更高版本。

## 开发

需要 Go 1.27+。

```sh
go tool mygo dev     # 开发，改动后自动重启
go test ./...        # 测试
go tool mygo build   # 打包到 build/darwin-<arch>/
```

`HONGGUO_TOUR=<目录>` 会让应用自动浏览各页面、截图到该目录后退出，用来检查界面。

### 结构

```
main.go                 入口，调用 internal/app
internal/
  app/                  窗口与界面（MyGo Native UI）
    app.go              窗口、侧栏、页面切换
    pages.go            频道、排行榜、搜索、继续观看、追剧
    watch.go            播放页：画面、控制条、选集、相关推荐
    kit.go              调色板、按钮、标签、骨架屏等组件
    system.go           菜单栏、媒体键、自动更新
    images.go           封面异步加载与缓存
  catalog/              openhongguo 封装；剧集下载、解密、磁盘缓存（2 GB LRU）
  library/              观看进度、追剧、设置（SQLite：library.db）
  cenc/                 纯 Go 的 CENC（AES-CTR）MP4 原地解密
  mac/                  经 purego 调用 AVFoundation（播放、取帧）和 ImageIO（HEIC），无 cgo
resources/icon.png      应用图标，由 scripts/icon.swift 绘制
```

### 播放是怎么工作的

1. openhongguo 取得分集的播放地址和内容密钥；
2. 下载 MP4，用 `internal/cenc` 原地解密（box 大小不变，只解密样本、恢复原始格式）；
3. AVPlayer 播放解密后的文件，负责声音、时钟和跳转；
4. `AVPlayerItemVideoOutput` 逐帧取出像素，交给 MyGo 的 `ui.Bitmap` 绘制。

## 快捷键

| 按键 | 作用 |
|---|---|
| 空格 | 播放/暂停 |
| ← / → | 快退/快进 5 秒 |
| ⌘← / ⌘→ | 快退/快进 10 秒 |
| ↑ / ↓、⌘↑ / ⌘↓ | 上一集/下一集 |
| F | 全屏 |
| Esc、⌘[ | 退出全屏/返回 |
| ⌘D | 追剧/取消追剧 |
| ⌘F | 搜索 |

## 发布

见 [RELEASING.md](RELEASING.md)。

## 许可

Copyright © 2026 giuem

本项目以 [GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）发布。你可以使用、修改和分发它，但分发修改后的版本时，必须以同样的许可证公开完整源码。

本项目使用的第三方软件及其许可证见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
