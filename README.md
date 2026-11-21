<p align="center"><img src="resources/icon.png" width="128" height="128" alt=""></p>
<h1 align="center">一集 Ichiwa</h1>
<p align="center"><b>原生、无广告的短剧桌面客户端</b></p>
<p align="center"><a href="https://github.com/giuem/ichiwa/releases/latest">下载</a> · <a href="#安装">安装</a> · <a href="#使用">使用</a> · <a href="#常见问题">常见问题</a> · <a href="CHANGELOG.md">更新日志</a></p>
<p align="center">
  <a href="https://github.com/giuem/ichiwa/releases/latest"><img src="https://img.shields.io/github/v/release/giuem/ichiwa?label=%E4%B8%8B%E8%BD%BD" alt="最新版本"></a>
  <a href="https://github.com/giuem/ichiwa/actions/workflows/test.yml"><img src="https://github.com/giuem/ichiwa/actions/workflows/test.yml/badge.svg" alt="Test"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/giuem/ichiwa" alt="AGPL-3.0"></a>
</p>

在电脑上看短剧，**没有广告，也没有弹窗**，打开就是剧，再看一集。

它是一个**原生应用**：界面用 [MyGo](https://github.com/egoist/mygo) 的 Native UI 写成，纯 Go、GPU 绘制，不套网页，没有 Electron 和 WebView。安装包只有 6–7 MB，视频交给系统的播放器解码，画面从解码到显示一直留在 GPU 上。

目前的片源是红果短剧，数据来自 [openhongguo](https://github.com/beilunyang/openhongguo)；之后会用插件接入更多平台。macOS 版已经可以用，Windows 和 Linux 版正在开发，进度见 [#15](https://github.com/giuem/ichiwa/issues/15)。

## 功能

- **发现**：推荐、真人剧、漫剧频道，排行榜，搜索与热搜榜
- **筛选**：按体裁、主题、设定、背景、受众、上新时间等条件找剧
- **演员**：演员榜，演员主页里有简介和参演作品
- **播放**：倍速、音量、全屏、自动连播、断点续播、下一集预加载；双击画面切换全屏，按住 → 临时 2 倍速
- **选集**：长剧按 50 集分段，标出看完的集和每集看到哪里，点哪集就从那集上次的位置接着看；下方有相关推荐
- **多季**：在播放页切换季，本季播完自动接下一季，未上线的季显示上线时间
- **继续观看和追剧**：可按在看、看完筛选，卡片上显示观看进度
- **弹幕**：表情代码显示成 emoji，按 D 开关
- **小窗**：窗口置顶（T）、竖屏小窗（V）、定时停止，右键画面可以找到
- **秒开**：推荐、真人剧、漫剧缓存在本地，启动立即显示，新内容在后台刷新
- **系统集成**：菜单栏、媒体键、系统的“正在播放”，播放时不熄屏；跟随系统的浅色/深色外观
- **无障碍**：卡片和按钮都能用键盘和读屏软件操作，支持“增强对比度”
- **本地记录**：观看进度、追剧和设置只存在本机，不需要账号
- **自动更新**：有新版本时在应用里提示，更新包经过签名校验

## 安装

### macOS

需要 macOS 13 或更高版本，支持 Apple 芯片和 Intel。

在终端运行这一行，它会下载适合这台 Mac 的最新版本，校验签名后装进“应用程序”：

```sh
curl -fsSL https://github.com/giuem/ichiwa/releases/latest/download/install.sh | sh
```

也可以从 [Releases](https://github.com/giuem/ichiwa/releases/latest) 下载 `.dmg`（`arm64` 是 Apple 芯片，`amd64` 是 Intel），打开后把“一集”拖进“应用程序”。应用目前没有 Developer ID 签名，用 `.dmg` 安装的话，第一次打开前要执行：

```sh
xattr -dr com.apple.quarantine /Applications/Ichiwa.app
```

### Windows 和 Linux

还没有正式版本，进度见 [#15](https://github.com/giuem/ichiwa/issues/15)。

每个版本都附有 `SHA256SUMS`，可以用 `shasum -a 256 -c SHA256SUMS --ignore-missing` 校验下载的文件。装好以后应用会自己检查更新，不用再手动下载。

## 使用

### 快捷键

Windows 和 Linux 上用 Ctrl 代替 ⌘。

| 按键 | 作用 |
| --- | --- |
| ⌘F | 搜索 |
| 空格 | 播放 / 暂停 |
| ← / → | 快退 / 快进 5 秒；按住 → 临时 2 倍速 |
| ⌘← / ⌘→ | 快退 / 快进 10 秒 |
| ↑ / ↓、⌘↑ / ⌘↓ | 上一集 / 下一集 |
| [ / ] | 降低 / 提高播放速度 |
| - / = | 调低 / 调高音量 |
| M | 静音 |
| F、双击画面 | 全屏 |
| E | 打开 / 收起选集 |
| D | 打开 / 关闭弹幕 |
| A | 打开 / 关闭自动连播 |
| T | 窗口置顶（置顶时收起选集面板并缩小窗口） |
| V | 竖屏小窗 |
| ⌘D | 追剧 / 取消追剧 |
| Esc、⌘[ | 收起选集、退出全屏、返回 |

关闭窗口不会退出应用：播放停止并保存进度，点 Dock 图标重新打开窗口，⌘Q 退出。

### 数据存在哪里

macOS 上：

| 内容 | 位置 |
| --- | --- |
| 观看进度、追剧、设置（`library.db`） | `~/Library/Application Support/Ichiwa/` |
| 匿名设备信息、窗口位置、`sdk.log` | `~/Library/Application Support/Ichiwa/` |
| 封面和剧集缓存（剧集最多 2 GB，旧的先清） | `~/Library/Caches/Ichiwa/` |

卸载时删掉 `/Applications/Ichiwa.app` 和上面两个目录。

## 常见问题

<details>
<summary><b>macOS 打开时提示“无法验证开发者”或“已损坏”？</b></summary>

应用还没有用 Developer ID 签名和公证。用上面的 `install.sh` 安装不会遇到这个问题；用 `.dmg` 安装的话，执行一次 `xattr -dr com.apple.quarantine /Applications/Ichiwa.app` 再打开。

</details>

<details>
<summary><b>需要登录账号吗？</b></summary>

不需要，也不支持。应用会向红果短剧注册一个匿名设备来获取内容，所以需要登录才能看的集也看不了；自动连播遇到这种集会跳过，连续 3 集都不行才停下。账号登录和云端同步的计划见 [#17](https://github.com/giuem/ichiwa/issues/17)。

</details>

<details>
<summary><b>某集播放失败，“重试”要等 30 秒？</b></summary>

红果会限制匿名设备的播放，失败后应用会换一个匿名设备再试。为了不在短时间内注册大量设备，重试会等 30 秒。如果一直失败，可能是红果改了接口，请先更新到最新版本，再到 [Issues](https://github.com/giuem/ichiwa/issues) 反馈，附上数据目录里的 `sdk.log`。

</details>

<details>
<summary><b>我的观看记录会上传吗？</b></summary>

不会。观看进度、追剧和设置只存在本机（见[数据存在哪里](#数据存在哪里)）。应用只和红果的服务器（获取内容）以及 GitHub（检查更新）通信，没有统计和崩溃上报。

</details>

## 参与开发

欢迎 issue 和 PR，开发环境和约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可

Copyright © 2026 giuem

本项目以 [GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）发布。你可以使用、修改和分发它，但分发修改后的版本时，必须以同样的许可证公开完整源码。

## 免责声明

本项目仅供学习和研究，禁止用于商业用途，与红果短剧及其运营方无关。所有内容的版权归原平台和版权方所有。使用本项目即表示你同意 [openhongguo 的免责声明](https://github.com/beilunyang/openhongguo#免责声明)。
