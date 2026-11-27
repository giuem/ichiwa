# 片源插件：协议设计

一集不内置任何片源。片源是第三方用 JavaScript 写、在 [moejs](https://github.com/MoeForge/moejs) 沙箱里运行的插件：插件只返回数据，界面由一集按固定模板画。红果是第一个插件，在独立仓库 [giuem/ichiwa-hongguo](https://github.com/giuem/ichiwa-hongguo) 里维护。

协议不假设内容是短剧。短剧、电视剧、电影、动画、综艺、单个视频都用同一套模型：**卡片**、**集合**、**剧集**和**播放**。

这份文档是协议的规范，实现和插件都以它为准。

## 1. 目标和边界

要做到：

- 第三方不改一集就能接入一个视频平台：导航、列表、筛选、排行、详情、播放，可选搜索和弹幕。
- 插件只能访问它声明的网络地址，碰不到本机文件、局域网、观看记录和其他插件的数据。
- 插件出错只影响它自己：超时、抛错、返回脏数据，界面都给出能看懂的提示。
- 旧插件在新版一集上能用；新插件在旧版一集上尽量能用，用不了时提示更新一集。

v1 不做：

- 登录账号、付费解锁。锁住的集显示锁，不能播。
- 插件自定义界面、脚本、样式。
- 插件改写播放数据流。解密只支持第 7 节列出的标准方案。
- 字幕、多音轨、直播。模型里给它们留了位置（第 12 节），v1 宿主忽略。
- 片源以外的插件种类（主题、语言包）。清单的 `kind` 留着以后扩展。

## 2. 宿主分层

```text
internal/ui        视图：读 app 的状态，发出用户操作；不知道片源和插件
internal/app       状态和流程：当前片源、路由、续播、连播、请求代次
internal/library   本地记录：观看进度、追剧、设置，按片源区分
internal/media     播放数据：选流、下载、解密、缓存、本机回环代理
internal/source    协议模型、能力、错误码、Source 接口
internal/plugin    插件：包、清单、moejs 运行、宿主 API、网络沙箱、安装更新
cmd/ichiwa-plugin  给插件作者的命令行：check、call、pack
```

依赖自上而下。`internal/source` 不依赖 moejs，也不依赖任何平台；`internal/plugin` 把一个插件包适配成 `source.Source`；`internal/media` 只认 `source.Playback`。

```go
package source

// Source 是一个片源。方法可能被并发调用；ctx 取消时尽快返回。
type Source interface {
	Info() Info // ID、名称、图标、版本、能力

	Navigation(ctx context.Context) ([]NavItem, error)
	Collection(ctx context.Context, r CollectionRequest) (*CollectionPage, error)
	Media(ctx context.Context, id string) (*Media, error)
	Play(ctx context.Context, r PlayRequest) (*Playback, error)

	// 可选能力；Info().Has 为 false 时宿主不会调用，实现返回 ErrUnsupported。
	Search(ctx context.Context, r SearchRequest) (*CollectionPage, error)
	SearchHome(ctx context.Context) (*CollectionPage, error)
	Suggest(ctx context.Context, query string) ([]string, error)
	Danmaku(ctx context.Context, r DanmakuRequest) (*DanmakuPage, error)
	Refresh(ctx context.Context, ids []string) ([]Card, error)
}
```

插件和测试用的假片源都实现这个接口。界面一律用 `Info().Has(能力)` 决定显示什么，不做类型断言。

## 3. 片源和身份

- 每个片源有全局唯一的 `id`，就是清单里的 `id`。
- 插件给出的媒体、剧集、集合 ID 只在它自己的片源内有意义，对宿主不透明。宿主内部的引用总是 `(片源 ID, ID)`，插件看不到其他片源的 ID。
- 观看记录、追剧、列表缓存都按片源区分。缓存文件名用 `sha256(片源 ID, 媒体 ID, 剧集 ID)`，不直接用插件给的 ID 拼路径。
- 界面一次浏览一个片源，在侧栏顶部切换。“继续观看”和“追剧”跨片源列在一起，卡片上标出片源；片源被停用或卸载时，记录保留，点开提示“片源不可用”，并给出安装或启用的入口。

## 4. 插件包

插件是一个 zip，根目录放 `plugin.json`，入口是 ES module：

```text
plugin.zip
├── plugin.json
├── index.js        # main 指向的入口
├── lib/*.js        # 只能用 ./ 或 ../ 相对导入，不能导入包外的模块
├── icon.png        # 可选，正方形 PNG/JPEG/WebP，至少 128×128
└── LICENSE
```

- zip 不超过 8 MiB，解压后不超过 32 MiB。路径不能含 `..`、反斜杠、控制字符、符号链接；只差大小写的同名文件算冲突。
- 不能用 `eval`、`new Function` 和动态 `import()`。要用 npm 包，先用打包工具打成一个文件。

### plugin.json

```json
{
  "id": "example",
  "name": "示例视频",
  "description": "一句话介绍，显示在插件列表里。",
  "kind": "source",
  "version": "1.2.0",
  "apiVersion": 1,
  "minAppVersion": "2026.1201.1",
  "main": "index.js",
  "icon": "icon.png",
  "license": "MIT",
  "homepage": "https://github.com/someone/ichiwa-example",
  "permissions": {
    "network": ["api.example.com", "*.cdn-example.com"]
  },
  "images": {
    "headers": { "Referer": "https://www.example.com/" }
  },
  "capabilities": ["search", "search.suggest", "search.home", "danmaku", "refresh"]
}
```

| 字段 | 规则 |
| --- | --- |
| `id` | `^[a-z0-9][a-z0-9._-]{0,62}[a-z0-9]$` 或单个字符 `^[a-z0-9]$`：不能以 `.`、`-`、`_` 结尾（Windows 会去掉目录名末尾的点）。安装后不能变，和发布它的 GitHub 仓库绑定 |
| `name`、`description` | 显示给用户，分别不超过 32 和 200 字 |
| `kind` | 目前只有 `source` |
| `version` | 语义化版本，和 Release 标签 `v<version>` 一致 |
| `apiVersion` | 协议大版本，目前是 `1`。宿主不支持的大版本拒绝安装 |
| `minAppVersion` | 可选。用到某个版本才加入的宿主 API 时填。格式同一集的版本号 `YYYY.MDD.N`，逐段按数字比较 |
| `permissions.network` | 插件能访问的主机：精确域名，或 `*.example.com`（必须是可注册域名的子域）。只能是 ASCII（国际化域名写 punycode）；不能写 IP（包括 `0x08.0x08.0x08.0x08` 这类变形写法）、`localhost`、端口 |
| `images.headers` | 可选。宿主下载图片时附带的请求头，只允许 `Referer`、`User-Agent`、`Origin` |
| `capabilities` | 可选能力，见第 5 节 |

清单里不认识的字段忽略；不认识的能力忽略，对应功能不显示。这样新插件在旧版一集上能用基础功能。

## 5. 导出函数

每个插件必须导出四个函数：

| 函数 | 作用 |
| --- | --- |
| `navigation()` | 侧栏里这个片源的导航项，每项打开一个集合 |
| `collection(req)` | 集合的一页：列表、分区、筛选、排行、演员页都是集合 |
| `media(req)` | 媒体详情：简介、演职人员、季、全部剧集、相关推荐 |
| `play(req)` | 一集的播放地址 |

可选能力在 `capabilities` 里声明，声明了就必须导出对应函数。安装时只编译不执行就能检查：

| 能力 | 函数 | 界面 |
| --- | --- | --- |
| `search` | `search(req)` | 侧栏的搜索、⌘F |
| `search.suggest` | `suggest(req)` | 输入时的联想词，需要 `search` |
| `search.home` | `searchHome()` | 搜索框为空时显示的热搜榜等分区，需要 `search` |
| `danmaku` | `danmaku(req)` | 弹幕开关、D 快捷键 |
| `refresh` | `refresh(req)` | 按 ID 批量取卡片：补全缺图的卡片，刷新追剧的更新进度 |

每个函数接收一个 JSON 对象，返回一个 JSON 对象，或者它的 Promise。

## 6. 数据模型

用 TypeScript 写；插件 SDK 附带同样的 `index.d.ts`，Go 侧结构一一对应。JSON 字段用 camelCase。

约定：时长和弹幕时间用秒，可以有小数；时间点用 Unix 秒；可选字段省略或给 `null` 都行；字符串首尾空白会被去掉；所有 URL 必须是 `http` 或 `https`，主机在 `permissions.network` 内。

### 卡片

列表里的每一项都是卡片。卡片打开什么由 `kind` 决定：

```ts
interface Card {
  kind: "media" | "collection"; // media 打开详情页；collection 打开集合页（如演员主页、片单）
  id: string;                   // media 的 ID 或集合的 ID，≤ 1024 字节
  title: string;
  subtitle?: string;            // 第二行：简介摘要、演员的角色、集数
  image?: string;
  shape?: "portrait" | "landscape" | "square" | "circle"; // 图的形状，默认 portrait；人用 circle
  badges?: string[];            // 图上的角标：“完结”“VIP”“更新至 30 集”，最多 2 个，每个 ≤ 8 字
  note?: string;                // 一行附注：热度、评分、上榜理由，≤ 24 字
}
```

### 集合

导航项、频道、排行榜、筛选页、搜索结果、演员主页、相关推荐都是集合。集合 ID 由插件自己编，比如 `"channel:recommend"`、`"rank"`、`"person:123"`、`"related:456"`。

```ts
interface NavItem {
  id: string;        // 集合 ID
  title: string;     // ≤ 8 字
  icon?: Icon;       // 宿主图标名，见下；不认识的显示默认图标
}

type Icon = "home" | "fire" | "star" | "sparkles" | "trophy" | "filter" | "film" | "tv"
  | "drama" | "anime" | "music" | "news" | "sport" | "kids" | "calendar" | "grid";

interface CollectionRequest {
  id: string;
  filters?: Record<string, string>; // 用户选的筛选项；首次打开为空
  cursor?: string;                   // 翻页时原样传回上一页的 cursor
}

interface CollectionPage {
  title?: string;        // 不给就用导航项或卡片的标题
  header?: Header;       // 集合本身的介绍，如演员主页
  filters?: FilterRow[]; // 只在第一页（没有 cursor）时读取
  layout?: "grid" | "list" | "ranked"; // items 的排法，默认 grid；ranked 显示名次
  sections?: Section[];  // 分区，只在第一页读取，排在 items 前面
  items?: Card[];        // 一页最多 200 张
  cursor?: string;       // 为空表示没有下一页；≤ 4096 字节
  error?: PageError;     // 列表部分失败：筛选行、介绍头、分区照常显示，列表处显示错误和重试
}

interface PageError {
  code: ErrorCode;       // 见第 10 节
  message?: string;
  retryAfter?: number;   // 秒
}

interface Header {
  title: string;
  image?: string;
  shape?: Card["shape"];
  description?: string;
  meta?: Meta[];
}

interface Meta { label?: string; value: string }   // “热度 1.2 亿”“2024”“评分 8.9”

interface FilterRow {
  key: string;
  title: string;                                // “题材”
  options: { value: string; title: string }[];  // 第一项通常是“全部”，value 为 ""；省略 value 也按 "" 处理
  value: string;                                // 当前选中的 value
}

interface Section {
  title: string;
  layout?: "row" | "grid" | "ranked";  // 默认 row：横向一行
  items?: Card[];
  collection?: string;  // 不给 items 时，宿主用这个集合 ID 再调用 collection 取第一页
  more?: string;        // 可选，“查看全部”打开的集合 ID
}
```

筛选：用户改了某一行，宿主带着全部当前选择重新调用 `collection`（不带 cursor），插件在返回的 `filters` 里给出新的各行和选中值。如果筛选行拿到了、列表没拿到，返回筛选行并在 `error` 里说明，用户还能改筛选或重试；整页都拿不到才抛错。各行之间的依赖（比如换了体裁，其他行的选项跟着变）由插件处理。排行榜就是带“榜单”筛选行、`layout: "ranked"` 的集合。

### 媒体详情

```ts
interface Media {
  id: string;
  title: string;
  image?: string;          // 竖版海报
  backdrop?: string;       // 横版背景图，可选
  description?: string;
  badges?: string[];
  meta?: Meta[];
  tags?: string[];
  people?: Card[];         // 演职人员，kind 一般是 collection（打开人物页）
  aspect?: "portrait" | "landscape"; // 画面方向，默认 landscape；各集可以单独覆盖
  seasons?: Season[];      // 同一部作品的各季，含本季
  episodes: Episode[];     // 本季全部剧集，按播放顺序；电影、单个视频只有一集；最多 5000
  sections?: Section[];    // 相关推荐等，一般给 collection 让宿主延后加载
}

interface Season {
  id: string;              // 这一季的媒体 ID
  title: string;           // 选季按钮上的短名，如“第二季”
  name?: string;           // 这一季作品的完整名称，如“某某 第二季”；追剧、记录里用它，省略时用 title
  image?: string;          // 这一季的海报
  current?: boolean;       // 当前这一季
  available?: boolean;     // 已上线；默认 true
  note?: string;           // 未上线时显示，如“12 月 1 日上线”
}

interface Episode {
  id: string;
  title?: string;          // 省略时显示“第 N 集”
  duration?: number;
  image?: string;
  badges?: string[];       // “新”“预告”
  aspect?: "portrait" | "landscape";
  lock?: "login" | "vip" | "paid" | "unavailable"; // 省略或 null 表示能播；不认识的值、不是字符串的值按 unavailable 处理
}
```

宿主行为：本季最后一集播完后，如果自动连播开着，接着播 `seasons` 里下一个 `available` 季的第一集。`lock` 的集显示锁，点了提示原因，自动连播跳过。

### 各函数的输入输出

```ts
navigation(): { items: NavItem[] }                         // 1 到 12 项
collection(req: CollectionRequest): CollectionPage
media(req: { id: string }): Media
play(req: PlayRequest): Playback                           // 见第 7 节

search(req: { query: string; filters?: Record<string, string>; cursor?: string }): CollectionPage
searchHome(): CollectionPage                               // 一般只有 sections
suggest(req: { query: string }): { items: string[] }       // 最多 10 个
danmaku(req: { mediaId: string; episodeId: string; cursor?: string }): DanmakuPage
refresh(req: { ids: string[] }): { items: Card[] }         // media ID，最多 50 个；只返回查到的

interface DanmakuPage {
  items: { id?: string; text: string; time: number }[];   // 按 time 排序
  cursor?: string;  // 一页可以为空；宿主继续翻到 cursor 为空
}
```

### 宿主怎么检查返回值

- 顶层不是对象，或缺必需字段（如 `media` 没有 `episodes`）：这次调用失败，按 `plugin` 错误处理。
- 列表里某一项不合格（不是对象，缺 `id` 或 `title`）：丢掉这一项，其余照常显示。抓取类插件常有个别脏数据，不应该整页报错。
- 可选字段不合格（类型不对、URL 不在白名单、数值为负或不是有限数）：丢掉这个字段。必需字段类型不对，按缺字段处理。
- 数字字段接受小数，需要整数的地方向下取整；`id` 等字符串字段给了数字时转成字符串。
- 同一列表里 `id` 重复的，只保留第一个。
- 不认识的字段和枚举值忽略（枚举值先去掉首尾空白，不认识的按默认处理；`lock` 例外，见上）。超长字符串截断。
- `encryption` 不合格的流整路丢弃，不会当作未加密播放。
- 每次调用最多返回 4 MiB 的 JSON。

## 7. 播放

插件只负责拿到播放地址和密钥；挑哪路流、下载、解密、缓存、交给播放器都由宿主做。

```ts
interface PlayRequest {
  mediaId: string;
  episodeId: string;
  prefer: {
    maxHeight: number;  // 宿主想要的最高分辨率（按短边），目前 1080
    hevc: boolean;      // 这台设备能硬解 HEVC
  };
}

interface Playback {
  streams: Stream[];    // 插件按自己的偏好排序；宿主按设备能力再挑
  expiresAt?: number;   // 地址过期的时间；过期后宿主重新调用 play
}

interface Stream {
  url: string;
  backupUrls?: string[];             // 同一内容的备用地址，主地址失败时依次尝试
  container: "mp4" | "hls";
  headers?: Record<string, string>;  // 请求媒体时附带；不允许 Host、Range、Connection、Content-Length、Transfer-Encoding
  codec?: string;                    // "h264"、"hevc"、"av1"……
  width?: number;
  height?: number;
  bitrate?: number;                  // bit/s
  encryption?: {
    scheme: "cenc";                  // MP4 通用加密，AES-128-CTR
    key: string;                     // 32 位十六进制
  };
}
```

- 媒体地址和跳转后的地址都必须在 `permissions.network` 内。
- `mp4`：宿主下载到缓存后播放；有 `encryption` 时下载完解密。以后可以改成边下边播，协议不变。
- `hls`：宿主起一个只监听 127.0.0.1 的回环代理，替播放器附加请求头。播放列表、分段、HLS 标准 AES-128 的密钥请求都经过代理，同样受白名单约束。
- 挑流：先去掉设备放不了的编码，再选短边不超过 `maxHeight` 里最高的；没有就放宽分辨率。

## 8. 宿主 API

插件运行时提供标准的 `fetch`、`URL`、`URLSearchParams`、`Headers`、`Request`、`Response`、`AbortController`、`TextEncoder`、`TextDecoder`、`atob`、`btoa`、`setTimeout`、`console`，以及全局对象 `host`：

```ts
interface Host {
  storage: {        // 持久化，按插件隔离，总量 64 KiB；存设备号、令牌
    get(key: string): string | undefined;
    set(key: string, value: string): void;   // 超出总量时抛错
    delete(key: string): void;
  };
  cache: {          // 内存，按插件隔离，一集退出即清空；总量 4 MiB（每项另计 64 字节），单项 256 KiB，最多 4096 项，满了先丢最早写入的
    get(key: string): string | undefined;
    set(key: string, value: string, ttlSeconds?: number): void;
    add(key: string, value: string, ttlSeconds?: number): boolean; // 不存在（或已过期）才写，原子操作；写了返回 true。用来在并发的调用之间加锁
    delete(key: string): void;
  };
  crypto: {         // moejs 没有 WebCrypto，由宿主提供
    md5(data: Bytes): string;                       // 十六进制
    sha1(data: Bytes): string;
    sha256(data: Bytes): string;
    hmac(alg: "md5" | "sha1" | "sha256", key: Bytes, data: Bytes): string;
    aes(o: { mode: "ecb" | "cbc" | "ctr" | "gcm"; key: Bytes; iv?: Bytes; data: Bytes;
             decrypt?: boolean; padding?: "pkcs7" | "none"; aad?: Bytes }): Uint8Array;
    rsa(o: { publicKey: string; data: Bytes; padding: "pkcs1" | "oaep-sha1" | "oaep-sha256" }): Uint8Array;
    randomBytes(n: number): Uint8Array;             // n ≤ 1024
    uuid(): string;                                 // v4
  };
  encoding: {
    hex(b: Uint8Array): string;           fromHex(s: string): Uint8Array;
    base64(b: Uint8Array): string;        fromBase64(s: string): Uint8Array;   // 也接受 URL 安全的写法
    utf8(s: string): Uint8Array;          fromUtf8(b: Uint8Array): string;
    gunzip(b: Uint8Array): Uint8Array;    inflate(b: Uint8Array, raw?: boolean): Uint8Array; // 结果 ≤ 4 MiB
  };
  device(): { platform: "macos" | "windows" | "linux" | "tvos"; appVersion: string; locale: string };
  error(code: ErrorCode, message?: string, extra?: { retryAfter?: number }): Error;
}

type Bytes = string | Uint8Array;  // 字符串按 UTF-8 编码
```

`console.log` 等写进这个插件自己的日志（每个插件保留最近 1 MiB），在插件设置里可以查看和导出，方便用户报问题。

## 9. 运行和限制

- 安装和启动时把模块编译一次。每次调用新建一个 moejs 运行时执行，调用结束即销毁：插件不能靠全局变量在调用之间保存状态，要用 `host.storage` 或 `host.cache`。调用返回、出错或被取消后，还没完成的请求、定时器和 `finally` 都不会再执行，所以锁、“进行中”标记这类状态要带过期时间。同一个插件的多个调用会同时运行，各自是独立的运行时。
- 每次调用内存上限 64 MiB；超时默认 15 秒，`media` 和 `play` 30 秒。宿主不再需要结果时（比如用户离开了页面）立即中断调用。
- 同一个插件最多同时运行 4 个调用，其余排队。每次调用最多同时有 16 个请求在路上，总共最多发 256 个请求（跳转也算），超过的 `fetch` 直接失败。
- 宿主取消的调用不算错误，不显示任何提示。
- 不会因为连续失败自动停用插件；插件设置里显示最近的错误和日志入口。

## 10. 错误

插件用 `throw host.error(code, message)` 告诉宿主需要区别对待的情况。抛出的其他异常一律当作 `plugin`：

| code | 含义 | 宿主怎么做 |
| --- | --- | --- |
| `unavailable` | 这集或这部作品放不了：下架、锁定、地区限制、匿名用户受限 | 显示原因；自动连播时跳过这一集，连续 3 集都放不了才停。带 `retryAfter` 时，手动重试也要等这么久 |
| `rateLimited` | 被平台限流 | 等 `retryAfter` 秒（默认 30）后才允许重试，显示倒计时 |
| `notFound` | ID 不存在 | 从列表移除；记录里标为“已不存在” |
| `network` | 连不上平台 | 提示检查网络，可以立即重试 |
| `plugin` | 其他错误、超时、返回的数据不合格 | 提示“片源出错”，带插件日志入口 |

`message` 会显示给用户：写给普通用户看的一句话，不超过 120 字。网络中断、超时、取消由宿主自己判断，插件不用报告。

## 11. 网络和安全

- `fetch` 只能访问 `permissions.network` 里的主机，每次跳转重新检查，最多 10 跳；跳到别的主机时去掉 `Authorization`、`Cookie` 请求头。
- **建立连接时检查解析出的 IP**：环回、私有网段、链路本地、CGNAT、组播、未指定地址、云服务元数据地址一律拒绝。只比对域名不够：插件作者控制的域名可以解析到内网。
- 每个插件有自己的 Cookie 存储，只在内存里，按标准规则保存响应的 `Set-Cookie`、附加到之后的请求；请求自己带了 `Cookie` 头时（值为空也算），宿主不再附加存储里的，也不发送空的 `Cookie` 头。需要长期保存的令牌放 `host.storage`。
- 图片和媒体地址受同一份白名单约束；宿主请求它们时不带插件的 Cookie。
- 插件看不到观看记录、追剧、设置和其他插件的数据，不能读写文件、启动进程、调用系统接口。

插件仍然能做到、安装时要告诉用户的：把用户的搜索词和观看的内容发给它声明的服务器；返回误导性的内容；消耗流量。

## 12. 兼容和演进

- 协议大版本 `apiVersion` 只在不兼容时才升。其余改动只加不减：新字段、新能力、新宿主 API。
- 宿主忽略不认识的字段、枚举值和能力，所以插件可以放心多返回东西。
- 插件用到新的宿主 API 时写 `minAppVersion`，旧版一集会提示“需要更新一集”，而不是运行到一半出错。
- 留给以后的扩展：`Stream.subtitles`、`Stream.audioTracks`、`Media.live`、登录能力 `account`（扫码登录，凭据存 `host.storage`，锁住的集登录后能播）。

## 13. 发布、安装和更新

发布：插件放在公开的 GitHub 仓库，加 topic `ichiwa-plugin`；Release 标签 `v<version>`，附件 `plugin.zip`。一集的仓库提供可复用的 workflow，负责校验、打包和发布：

```yaml
jobs:
  release:
    uses: giuem/ichiwa/.github/workflows/plugin-release.yml@main
```

发现：

- 一集的仓库每天跑一次 workflow：搜索 topic `ichiwa-plugin`，下载并校验每个插件的最新 Release，生成插件索引 `plugins.json`，和官网一起发布。一集读索引，不直接调 GitHub API（未登录时每小时只能调 60 次）。被举报的恶意插件从索引里移除。
- 也可以直接输入 `owner/repo` 安装不在索引里的插件。

安装：

1. 下载 `plugin.zip`，记下 SHA-256；检查清单、路径、导出函数，不执行代码。
2. 告诉用户：名称、来源仓库、版本、许可、要访问的主机，以及第 11 节末尾那段话。
3. 用户确认后原子写入，`id` 和仓库绑定；之后同一 `id` 只接受这个仓库的更新。

更新：

- 每天检查一次。要访问的主机没有增加时自动更新；增加了就在插件列表里提示，用户确认后才更新。
- 先写到暂存位置，检查通过再替换；失败保留旧版本。
- 插件可以停用、卸载。

开发：

- 插件设置里的“开发者模式”可以从本地文件夹加载插件，文件变化后自动重新加载，日志实时显示。
- 命令行工具 `go run github.com/giuem/ichiwa/cmd/ichiwa-plugin@latest`：`check <目录>` 检查清单和导出；`call <目录> <函数> '<JSON>'` 在同样的沙箱里调用一个函数，打印结果和日志；`pack <目录>` 打出 `plugin.zip`。
- `sdk/index.d.ts` 是协议的类型定义，[plugin-development.md](plugin-development.md) 是写给插件作者的上手指南。

## 14. 界面

侧栏从上到下：

1. **片源切换**：当前片源的图标和名称，点开列出所有已启用的片源，底部是“管理片源…”。一个片源都没有时，这里显示“添加片源”。
2. 当前片源的导航项（`navigation()`），第一项是进入片源时的默认页面。
3. 搜索（有 `search` 能力时）。
4. 继续观看、追剧：本地记录，跨片源。
5. 设置。

页面模板：

| 模板 | 用于 |
| --- | --- |
| 集合页 | 导航项、排行、筛选、演员主页、搜索结果：可选的介绍头、筛选行、分区、卡片网格或名次列表，滚动到底自动翻页 |
| 详情播放页 | 播放器、选集（长剧按 50 集分段）、选季、简介、演职人员、相关推荐分区 |
| 搜索页 | 输入框、联想词、`searchHome` 的分区、结果 |
| 记录页 | 继续观看、追剧，跨片源，卡片上标出片源 |
| 片源管理 | 已安装（启用、停用、更新、卸载、日志）、发现（插件索引）、从仓库安装、开发者模式 |

没有某项能力时，相关入口、按钮和快捷键全部隐藏，不显示灰掉的按钮。

## 15. 红果插件

红果插件 `hongguo` 在 [giuem/ichiwa-hongguo](https://github.com/giuem/ichiwa-hongguo)，把 [openhongguo](https://github.com/beilunyang/openhongguo) 的 Go SDK 改写成纯 JavaScript（openhongguo 的 npm 包是 WebAssembly，moejs 不支持），许可证沿用 Apache-2.0。

| 一集现在的功能 | 插件里的写法 |
| --- | --- |
| 推荐、真人剧、漫剧 | 三个导航项，各是一个 grid 集合 |
| 排行（内容 → 榜单，含演员榜） | 导航项“排行”：`ranked` 集合，两个筛选行；演员榜的卡片是 `collection`（人物页） |
| 筛选（换体裁后其他行跟着变） | 导航项“筛选”：带筛选行的集合 |
| 搜索、热搜榜 | `search`、`searchHome` |
| 详情、分集、多季、未上线的季 | `media`，含 `seasons` |
| 相关推荐 | `media` 的 `sections`，集合 `related:<id>` |
| 演员主页、参演作品 | 集合 `person:<id>`：`header` 加作品 |
| 弹幕（表情代码转 emoji） | `danmaku`；表情转换在插件里做 |
| 搜索结果补封面、追剧更新集数 | `refresh` |
| 播放地址、CENC 密钥 | `play`；下载、解密、缓存由宿主完成 |
| 匿名设备注册、换设备重试 | 插件内部，设备信息存 `host.storage` |
| 匿名播放受限、限流 | 错误码 `unavailable`、`rateLimited` |

一集的观看记录迁移时，原有记录归到片源 `hongguo` 名下；用户装上红果插件后，记录自动对上。
