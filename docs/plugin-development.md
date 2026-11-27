# 写一个片源插件

这份指南带你从零写一个一集的片源插件，在本机调试，最后打包发布。协议的完整规范在 [plugin-design.md](plugin-design.md)，遇到这里没说清的地方以它为准；类型定义在 [sdk/index.d.ts](../sdk/index.d.ts)；一个覆盖全部函数的示例插件在 [internal/plugin/testdata/example](../internal/plugin/testdata/example)。

## 插件是什么

插件是一个文件夹（发布时打成 zip），里面一个 `plugin.json` 和一个 JavaScript 入口。一集在沙箱里调用它导出的函数，函数从视频平台取数据，返回 JSON：导航、列表、详情、播放地址。界面由一集按固定模板画，插件只管数据。

插件运行在 [moejs](https://github.com/MoeForge/moejs) 里，不是 Node.js，也不是浏览器：

- 有标准的 `fetch`、`URL`、`URLSearchParams`、`Headers`、`Request`、`Response`、`AbortController`、`TextEncoder`、`TextDecoder`、`atob`、`btoa`、`setTimeout`、`console`，以及一集提供的全局对象 `host`（见下文）。
- 没有 `require`、`process`、文件系统、WebCrypto、WebAssembly。加解密用 `host.crypto`。
- 不能用 `eval`、`new Function` 和动态 `import()`。

## 准备

需要 Go 1.27 或更高版本，用来运行命令行工具 `ichiwa-plugin`：

```sh
go run github.com/giuem/ichiwa/cmd/ichiwa-plugin@latest help
```

下文简写成 `ichiwa-plugin`。也可以 `go install github.com/giuem/ichiwa/cmd/ichiwa-plugin@latest` 装到 `$GOPATH/bin`。

## 第一个插件

新建一个文件夹，放两个文件。

`plugin.json`：

```json
{
  "id": "hello",
  "name": "你好视频",
  "description": "一个最小的插件。",
  "kind": "source",
  "version": "0.1.0",
  "apiVersion": 1,
  "main": "index.js",
  "license": "MIT",
  "permissions": { "network": ["api.example.com", "*.img.example.com"] }
}
```

`index.js`：

```js
/// <reference path="./index.d.ts" />

export function navigation() {
  return { items: [{ id: "home", title: "推荐", icon: "home" }] };
}

export async function collection(req) {
  const res = await fetch(`https://api.example.com/list?page=${req.cursor ?? 1}`);
  if (!res.ok) throw host.error("network", "列表加载失败");
  const data = await res.json();
  return {
    items: data.list.map((v) => ({ kind: "media", id: v.id, title: v.name, image: v.cover })),
    cursor: data.hasMore ? String(Number(req.cursor ?? 1) + 1) : undefined,
  };
}

export async function media(req) {
  const v = await (await fetch(`https://api.example.com/video/${encodeURIComponent(req.id)}`)).json();
  return {
    id: v.id,
    title: v.name,
    image: v.cover,
    description: v.intro,
    episodes: v.episodes.map((e) => ({ id: e.id, title: e.name, duration: e.seconds })),
  };
}

export async function play(req) {
  const p = await (await fetch(`https://api.example.com/play/${encodeURIComponent(req.episodeId)}`)).json();
  return { streams: [{ url: p.url, container: "mp4", codec: "h264", width: p.w, height: p.h }] };
}
```

把 [sdk/index.d.ts](../sdk/index.d.ts) 复制到文件夹里，编辑器就能补全 `host` 和返回值的字段。

检查，再调用试试：

```sh
ichiwa-plugin check ./hello
ichiwa-plugin call ./hello navigation
ichiwa-plugin call ./hello collection '{"id":"home"}'
ichiwa-plugin call ./hello media '{"id":"123"}'
ichiwa-plugin call ./hello play '{"mediaId":"123","episodeId":"e1"}'
```

`call` 在和一集相同的沙箱里运行：同样的网络白名单、内存和时间限制。它打印一集实际收到的结果（经过检查和清洗，不合格的项已经丢掉），插件的 `console.log` 和宿主的日志打印在标准错误上。加 `-raw` 看插件返回的原始 JSON。

## plugin.json

| 字段 | 说明 |
| --- | --- |
| `id` | 小写字母、数字和 `.` `_` `-`，以字母或数字开头和结尾，不超过 64 个字符。发布后不能改 |
| `name` | 不超过 32 字；`description` 不超过 200 字 |
| `version` | 语义化版本，如 `1.2.0`，和 Release 标签 `v1.2.0` 一致 |
| `apiVersion` | 写 `1` |
| `minAppVersion` | 可选。用到某个版本才加入的宿主 API 时填，如 `2026.1201.1` |
| `main` | 入口文件，`.js` |
| `icon` | 可选，正方形 PNG/JPEG/WebP，至少 128×128 |
| `permissions.network` | 插件能访问的主机。精确域名，或 `*.example.com` 表示 example.com 的所有子域（不含 example.com 本身，要的话另写一项）。只能写 ASCII，国际化域名写 punycode（`xn--…`）。不能写 IP（包括 `0x08.0x08.0x08.0x08` 这类变形写法）、`localhost`、端口，也不能写 `*.com` 这样的公共后缀 |
| `images.headers` | 可选。一集下载图片时带的请求头，只能有 `Referer`、`User-Agent`、`Origin` |
| `capabilities` | 可选能力：`search`、`search.suggest`、`search.home`、`danmaku`、`refresh`。声明了就必须导出对应的函数 |

图片、视频、播放列表里的地址也要在 `permissions.network` 里，否则一集不显示、不播放。用户安装时会看到这份主机列表，写得越少越好。

## 导航和集合

导航项、频道、排行榜、筛选页、搜索结果、演员主页、相关推荐都是“集合”。集合 ID 你自己编，比如 `"channel:recommend"`、`"rank"`、`"person:123"`，一集只会原样传回给 `collection`。

```js
export function collection(req) {
  // req = { id, filters?, cursor? }
}
```

返回一页（字段见 [index.d.ts](../sdk/index.d.ts) 的 `CollectionPage`）：

- `items`：卡片，一页最多 200 张。`kind: "media"` 点开是详情页，`kind: "collection"` 点开是另一个集合（演员主页、片单）。
- `cursor`：还有下一页时给一个字符串，一集翻页时原样传回；不给就是没有了。
- `layout`：`"grid"`（默认）、`"list"`、`"ranked"`（显示名次）。
- `filters`：筛选行，只在第一页（没有 `cursor`）时读取。用户改了某一行，一集带着全部当前选择重新调用 `collection`，你在返回的 `filters` 里给出新的各行和选中值；行之间的依赖（换了体裁，年份跟着变）由你处理。
- `sections`：分区，排在 `items` 前面，只在第一页读取。分区可以直接给 `items`，也可以只给 `collection`，让一集再去取（适合慢的接口）。
- `header`：集合本身的介绍，比如演员主页的头像和简介。
- `error`：筛选行拿到了、列表没拿到时，返回筛选行和 `error: { code: "network", message: "列表暂时取不到" }`，用户还能改筛选或重试。整页都拿不到才抛错。

排行榜就是带“榜单”筛选行、`layout: "ranked"` 的集合。

## 详情和播放

`media({ id })` 返回一部作品：标题、海报、简介、演职人员、各季、全部剧集。`episodes` 必须有，按播放顺序，最多 5000 集；电影和单个视频只有一集。

- 放不了的集写 `lock`：`"vip"`、`"paid"`、`"login"`、`"unavailable"`。一集显示锁，自动连播时跳过。
- 多季作品在 `seasons` 里列出各季（含本季，本季 `current: true`），没上线的季写 `available: false` 和 `note`。
- 相关推荐放在 `sections`，一般只给 `collection`，比如 `"related:123"`。

`play({ mediaId, episodeId, prefer })` 返回播放地址：

```js
return {
  streams: [
    { url, container: "mp4", codec: "h264", width: 1280, height: 720, headers: { Referer: "https://www.example.com/" } },
    { url: hlsUrl, container: "hls", codec: "h264", width: 1920, height: 1080 },
  ],
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
};
```

- `mp4`：一集下载到缓存后播放，有 `encryption: { scheme: "cenc", key }` 时下载完解密。
- `hls`：一集在本机起一个只监听 127.0.0.1 的代理，播放列表、分段、HLS 标准 AES-128 的密钥都经过它，带上你给的 `headers`，同样只能访问白名单里的主机。加密的 HLS 只支持 HLS 自己的 AES-128，不支持 `encryption` 字段。
- 一集先去掉设备放不了的流，再挑短边不超过 `prefer.maxHeight` 里最高的。现在的规则：H.264 都能放；HEVC 只在 `prefer.hevc` 为 true 时（目前只有 macOS）；AV1、VP8、VP9 都不放；Linux 上不放 HLS。`codec` 不写时一集照样试。分辨率相同时按你给的顺序。
- 苹果的播放器只认 `hvc1` 标记的 HEVC MP4，不认 `hev1`。拿不准的话把 H.264 放在前面。
- 请求头不能有 `Host`、`Range`、`Connection`、`Content-Length`、`Transfer-Encoding`。下载媒体时一集不带插件的 Cookie，需要的话写进 `headers`。

## 可选能力

在 `capabilities` 里声明，并导出对应函数：

| 能力 | 函数 | 返回 |
| --- | --- | --- |
| `search` | `search({ query, filters?, cursor? })` | 和 `collection` 一样的一页 |
| `search.suggest` | `suggest({ query })` | `{ items: string[] }`，最多 10 个；需要 `search` |
| `search.home` | `searchHome()` | 一页，一般只有 `sections`（热搜榜等）；需要 `search` |
| `danmaku` | `danmaku({ mediaId, episodeId, cursor? })` | `{ items: [{ id?, text, time }], cursor? }`，一页可以为空，一集翻到 `cursor` 为空 |
| `refresh` | `refresh({ ids })` | `{ items: Card[] }`，按 media ID 批量取卡片，最多 50 个，只返回查到的 |

没声明的能力，一集不显示对应的入口。

## 宿主 API：host

```js
host.storage.get("deviceId");              // 持久保存，总量 64 KiB，超出时 set 抛错
host.storage.set("deviceId", id);
host.cache.set("token", t, 600);           // 内存缓存，一集退出就没了，总量 4 MiB、单项 256 KiB
host.cache.add("lock", id, 5);             // 键不存在或已过期才写，原子操作，写了返回 true：用来加锁
host.crypto.md5("abc");                    // 也有 sha1、sha256、hmac
host.crypto.aes({ mode: "cbc", key, iv, data, decrypt: true });   // ecb、cbc、ctr、gcm
host.crypto.rsa({ publicKey: pem, data, padding: "oaep-sha256" }); // 公钥加密，公钥最多 16384 位
host.crypto.randomBytes(16); host.crypto.uuid();
host.encoding.base64(bytes); host.encoding.fromBase64(s);         // 也有 hex、utf8、gunzip、inflate
host.device();                              // { platform, appVersion, locale }
throw host.error("rateLimited", "请求太频繁", { retryAfter: 60 });
```

`Bytes` 参数可以是字符串（按 UTF-8）或 `Uint8Array`，返回的字节都是 `Uint8Array`。完整签名见 [index.d.ts](../sdk/index.d.ts)。

**每次调用都是一个新的运行时**，调用结束就销毁，全局变量留不到下一次。要跨调用保存的东西放 `host.storage`（设备号、长期令牌）或 `host.cache`（短期令牌、接口结果）。

同一个插件的多个调用会**同时**运行，各是各的运行时，只共享 `host.storage` 和 `host.cache`。调用返回、出错或被取消后，还没完成的请求、定时器和 `finally` 都不会再执行，所以锁和“进行中”这类标记一定要带过期时间。在调用之间加锁用 `host.cache.add`：

```js
const token = host.crypto.uuid();
if (host.cache.add("register-lock", token, 5)) {
  // 拿到了锁；耗时长就定期 host.cache.set 续期，做完 host.cache.delete
} else {
  // 别的调用正在注册：等一会儿再读结果
}
```

`ichiwa-plugin call` 默认每次都从空的缓存开始；加 `-cache <文件>` 可以让 `host.cache` 在多次调用之间保留，复现这类跨调用的行为。

## 网络

- `fetch` 只能访问 `permissions.network` 里的主机，每次跳转都重新检查，最多 10 跳；跳到别的主机、或从 https 跳到 http 时去掉 `Authorization` 和 `Cookie`。
- 连接时一集检查域名解析出的 IP：本机（包括本机的公网地址和它所在的 IPv6 网段）、局域网、链路本地、CGNAT、组播等地址一律拒绝，域名在白名单里也不行。插件的请求不走系统代理。
- 每个插件有自己的 Cookie，只在内存里：响应的 `Set-Cookie` 会保存，之后的请求自动带上。请求自己写了 `Cookie` 头时（值为空也算），一集不再附加保存的，也不发出空的 `Cookie` 头；跳转后也一样。想不带任何 Cookie 发请求，就写 `Cookie: ""`。
- 一次调用最多同时有 16 个请求在路上，多的排队。
- 被拒绝的请求在插件日志里有一行说明。

## 出错

用 `throw host.error(code, message)` 告诉一集需要区别对待的情况：

| code | 什么时候用 |
| --- | --- |
| `unavailable` | 这集或这部作品放不了：下架、锁定、地区限制、匿名用户受限 |
| `rateLimited` | 被平台限流；带上 `retryAfter`（秒），一集倒计时后才允许重试 |
| `notFound` | ID 不存在 |
| `network` | 连不上平台 |
| `plugin` | 其他错误 |

`message` 会显示给用户，写一句普通人看得懂的话，不超过 120 字。抛出的其他异常（包括带 `code` 属性的普通 `Error`）一律算 `plugin`，异常的堆栈记进插件日志。`fetch` 因为网络断了失败、你又没接住时，一集按 `network` 处理；超时、用户离开页面由一集自己判断，不用报告。

## 一集怎么检查返回值

一集对数据很宽容，丢掉坏的、留下好的，所以抓取类插件不用为个别脏数据整页报错：

- 顶层不是对象、缺必需字段（比如 `media` 没有 `episodes`）：这次调用失败。
- 列表里某一项不合格（不是对象、缺 `id` 或 `title`）：丢掉这一项。同一列表里 `id` 重复的只留第一个。
- 可选字段不合格（类型不对、URL 不在白名单、负数）：丢掉这个字段。
- `id` 给了数字会转成字符串；不认识的字段和枚举值忽略；超长的字符串截断。
- 每次调用最多返回 4 MiB 的 JSON。

用 `ichiwa-plugin call` 看到的就是检查后的结果，和 `-raw` 的输出对比，就知道哪些被丢掉了。

## 限制

| 项 | 限制 |
| --- | --- |
| 每次调用的内存 | 64 MiB，超过时调用被中断 |
| 每次调用的时间 | 15 秒；`media` 和 `play` 30 秒 |
| 同一插件同时运行的调用 | 4 个，其余排队 |
| 返回的 JSON | 4 MiB |
| 插件包 | zip 不超过 8 MiB，解压后不超过 32 MiB；路径不能有 `..`、反斜杠、控制字符、符号链接，只差大小写的文件名算冲突 |
| 导入 | 只能用 `./`、`../` 相对导入包里的文件 |

要用 npm 包，先用打包工具打成一个文件，比如：

```sh
npx esbuild src/index.js --bundle --format=esm --platform=neutral --target=es2022 --outfile=index.js
```

文件夹里以 `.` 开头的文件和 `node_modules` 不算插件的一部分，`pack` 也不会打进去。

## 在一集里调试

1. 运行一集，点侧栏顶部的片源名称，选“开发者模式 → 从文件夹加载插件…”，选插件文件夹。
2. 一集加载插件并切换过去。之后文件一变就自动重新加载；改坏了会提示，仍用上一次能用的代码。
3. 侧栏的“插件日志”实时显示 `console.log` 和一集记下的每次调用、耗时、被拒绝的请求和错误。
4. 片源菜单里可以切回红果，或者关闭开发者模式。

一集记住加载过的文件夹，下次启动自动加载。也可以用环境变量 `ICHIWA_PLUGIN_DIR=<文件夹>` 启动一集，直接加载并切换到那个插件。

开发者模式的界面现在还很简陋，片源管理页会在之后的版本里做好。

## 打包和发布

```sh
ichiwa-plugin pack ./hello -o plugin.zip
```

`pack` 先做和 `check` 一样的检查，再打包。发布时：

1. 把插件放在公开的 GitHub 仓库，加上 topic `ichiwa-plugin`。
2. 打标签 `v<version>`（和 `plugin.json` 的 `version` 一致），在 Release 里附上 `plugin.zip`。

一集会提供可复用的 workflow，自动校验、打包和发布，并收录进插件索引；在那之前手动发布即可。

## 发布前检查

- [ ] `ichiwa-plugin check` 通过。
- [ ] 每个函数都用 `ichiwa-plugin call` 试过，包括翻页、筛选、出错的情况。
- [ ] `permissions.network` 只有必须的主机，图片和视频的主机也在里面。
- [ ] 平台限流、内容下架时抛了对应的 `host.error`，`message` 是用户看得懂的话。
- [ ] 没有在全局变量里存跨调用的状态。
- [ ] `version` 改了，和标签一致。
