// 一集（Ichiwa）片源插件协议的类型定义，协议大版本 1。
// 规范见 https://github.com/giuem/ichiwa/blob/main/docs/plugin-design.md ，
// 上手指南见 docs/plugin-development.md。
//
// 用法：把这个文件放进插件目录，在 index.js 顶部写
//   /// <reference path="./index.d.ts" />
// 或者在 jsconfig.json / tsconfig.json 里引用它，编辑器就能补全和检查。
// 插件导出的函数用 JSDoc 标注类型，例如：
//   /** @param {import("./index").CollectionRequest} req @returns {Promise<import("./index").CollectionPage>} */
//   export async function collection(req) { ... }

// ---------- 清单 plugin.json（第 4 节） ----------

export interface Manifest {
  /** ^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$：不能以 . - _ 结尾，安装后不能变 */
  id: string;
  /** 不超过 32 字 */
  name: string;
  /** 不超过 200 字 */
  description?: string;
  kind: "source";
  /** 语义化版本，和 Release 标签 v<version> 一致 */
  version: string;
  apiVersion: 1;
  /** 用到新加的宿主 API 时填，格式 YYYY.MDD.N */
  minAppVersion?: string;
  /** 入口 ES module，如 "index.js" */
  main: string;
  /** 正方形 PNG/JPEG/WebP，至少 128×128 */
  icon?: string;
  license?: string;
  homepage?: string;
  permissions?: {
    /** 精确域名，或 "*.example.com"（不含 example.com 本身）。不能写 IP、localhost、端口 */
    network?: string[];
  };
  images?: {
    /** 宿主下载图片时附带的请求头，只允许 Referer、User-Agent、Origin */
    headers?: { Referer?: string; "User-Agent"?: string; Origin?: string };
  };
  capabilities?: Capability[];
}

export type Capability = "search" | "search.suggest" | "search.home" | "danmaku" | "refresh";

// ---------- 数据模型（第 6 节） ----------
// 可选字段省略或给 null 都行；字符串首尾空白会被去掉；URL 必须是 http/https，
// 主机在 permissions.network 内，否则这个字段被丢掉。

export type Shape = "portrait" | "landscape" | "square" | "circle";

export interface Card {
  /** media 打开详情页；collection 打开集合页（演员主页、片单） */
  kind: "media" | "collection";
  /** ≤ 1024 字节；给数字会转成字符串 */
  id: string;
  title: string;
  subtitle?: string;
  image?: string;
  /** 默认 portrait；人用 circle */
  shape?: Shape;
  /** 最多 2 个，每个 ≤ 8 字 */
  badges?: string[];
  /** ≤ 24 字 */
  note?: string;
}

export type Icon =
  | "home" | "fire" | "star" | "sparkles" | "trophy" | "filter" | "film" | "tv"
  | "drama" | "anime" | "music" | "news" | "sport" | "kids" | "calendar" | "grid";

export interface NavItem {
  /** 集合 ID */
  id: string;
  /** ≤ 8 字 */
  title: string;
  icon?: Icon;
}

export interface CollectionRequest {
  id: string;
  /** 用户选的筛选项；首次打开为空 */
  filters?: Record<string, string>;
  /** 翻页时原样传回上一页的 cursor */
  cursor?: string;
}

export interface CollectionPage {
  title?: string;
  header?: Header;
  /** 只在第一页（没有 cursor）时读取 */
  filters?: FilterRow[];
  /** 默认 grid；ranked 显示名次 */
  layout?: "grid" | "list" | "ranked";
  /** 只在第一页读取，排在 items 前面 */
  sections?: Section[];
  /** 一页最多 200 张 */
  items?: Card[];
  /** 为空表示没有下一页；≤ 4096 字节 */
  cursor?: string;
  /** 列表部分失败：筛选行、介绍头、分区照常显示，列表处显示错误和重试 */
  error?: PageError;
}

export interface PageError {
  code: ErrorCode;
  message?: string;
  /** 秒 */
  retryAfter?: number;
}

export interface Header {
  title: string;
  image?: string;
  shape?: Shape;
  description?: string;
  meta?: Meta[];
}

export interface Meta {
  label?: string;
  value: string;
}

export interface FilterRow {
  key: string;
  title: string;
  /** 第一项通常是“全部”，value 为 ""；省略 value 也按 "" 处理 */
  options: { value?: string; title: string }[];
  /** 当前选中的 value */
  value: string;
}

export interface Section {
  title: string;
  /** 默认 row：横向一行 */
  layout?: "row" | "grid" | "ranked";
  items?: Card[];
  /** 不给 items 时，宿主用这个集合 ID 再调用 collection 取第一页 */
  collection?: string;
  /** “查看全部”打开的集合 ID */
  more?: string;
}

export interface Media {
  id: string;
  title: string;
  /** 竖版海报 */
  image?: string;
  /** 横版背景图 */
  backdrop?: string;
  description?: string;
  badges?: string[];
  meta?: Meta[];
  tags?: string[];
  /** 演职人员，kind 一般是 collection */
  people?: Card[];
  /** 默认 landscape；各集可以单独覆盖 */
  aspect?: "portrait" | "landscape";
  /** 同一部作品的各季，含本季 */
  seasons?: Season[];
  /** 本季全部剧集，按播放顺序；最多 5000 */
  episodes: Episode[];
  /** 相关推荐等，一般给 collection 让宿主延后加载 */
  sections?: Section[];
}

export interface Season {
  /** 这一季的媒体 ID */
  id: string;
  /** 选季按钮上的短名，如“第二季” */
  title: string;
  /** 这一季作品的完整名称 */
  name?: string;
  image?: string;
  current?: boolean;
  /** 已上线；默认 true */
  available?: boolean;
  /** 未上线时显示，如“12 月 1 日上线” */
  note?: string;
}

export interface Episode {
  id: string;
  /** 省略时显示“第 N 集” */
  title?: string;
  /** 秒 */
  duration?: number;
  image?: string;
  badges?: string[];
  aspect?: "portrait" | "landscape";
  /** 省略或 null 表示能播；不认识的值、不是字符串的值按 unavailable 处理 */
  lock?: "login" | "vip" | "paid" | "unavailable" | null;
}

export interface SearchRequest {
  query: string;
  filters?: Record<string, string>;
  cursor?: string;
}

export interface DanmakuRequest {
  mediaId: string;
  episodeId: string;
  cursor?: string;
}

export interface DanmakuPage {
  /** 按 time（秒）排序 */
  items: { id?: string; text: string; time: number }[];
  /** 一页可以为空；宿主继续翻到 cursor 为空 */
  cursor?: string;
}

// ---------- 播放（第 7 节） ----------

export interface PlayRequest {
  mediaId: string;
  episodeId: string;
  prefer: {
    /** 宿主想要的最高分辨率（按短边），目前 1080 */
    maxHeight: number;
    /** 这台设备能硬解 HEVC */
    hevc: boolean;
  };
}

export interface Playback {
  /** 插件按自己的偏好排序；宿主去掉设备放不了的，再按分辨率挑 */
  streams: Stream[];
  /** Unix 秒；过期后宿主重新调用 play */
  expiresAt?: number;
}

export interface Stream {
  url: string;
  /** 同一内容的备用地址，主地址失败时依次尝试 */
  backupUrls?: string[];
  container: "mp4" | "hls";
  /** 请求媒体时附带；不允许 Host、Range、Connection、Content-Length、Transfer-Encoding */
  headers?: Record<string, string>;
  /** "h264"、"hevc"、"av1"…… */
  codec?: string;
  width?: number;
  height?: number;
  /** bit/s */
  bitrate?: number;
  /** 不合格的流整路丢弃，不会当作未加密播放 */
  encryption?: {
    /** MP4 通用加密，AES-128-CTR */
    scheme: "cenc";
    /** 32 位十六进制 */
    key: string;
  };
}

// ---------- 导出函数（第 5、6 节） ----------
// 每个函数接收一个 JSON 对象，返回一个 JSON 对象或它的 Promise。

export interface Plugin {
  navigation(): Result<{ items: NavItem[] }>;
  collection(req: CollectionRequest): Result<CollectionPage>;
  media(req: { id: string }): Result<Media>;
  play(req: PlayRequest): Result<Playback>;
  /** capabilities 含 search */
  search?(req: SearchRequest): Result<CollectionPage>;
  /** capabilities 含 search.home */
  searchHome?(): Result<CollectionPage>;
  /** capabilities 含 search.suggest；最多 10 个 */
  suggest?(req: { query: string }): Result<{ items: string[] }>;
  /** capabilities 含 danmaku */
  danmaku?(req: DanmakuRequest): Result<DanmakuPage>;
  /** capabilities 含 refresh；最多 50 个 ID，只返回查到的 */
  refresh?(req: { ids: string[] }): Result<{ items: Card[] }>;
}

export type Result<T> = T | Promise<T>;

// ---------- 错误（第 10 节） ----------

export type ErrorCode = "unavailable" | "rateLimited" | "notFound" | "network" | "plugin";

// ---------- 宿主 API（第 8 节） ----------

export type Bytes = string | Uint8Array;

export interface Host {
  /** 持久化，按插件隔离，总量 64 KiB（键和值的字节数之和）；存设备号、令牌 */
  storage: {
    get(key: string): string | undefined;
    /** 超出总量时抛错 */
    set(key: string, value: string): void;
    delete(key: string): void;
  };
  /** 内存，按插件隔离，一集退出即清空；总量 4 MiB，单项 256 KiB，满了先丢最早写入的 */
  cache: {
    get(key: string): string | undefined;
    set(key: string, value: string, ttlSeconds?: number): void;
    /** 键不存在或已过期才写入，原子操作；写了返回 true。用来在同时运行的调用之间加锁，锁要带过期时间 */
    add(key: string, value: string, ttlSeconds?: number): boolean;
    delete(key: string): void;
  };
  crypto: {
    /** 十六进制 */
    md5(data: Bytes): string;
    sha1(data: Bytes): string;
    sha256(data: Bytes): string;
    hmac(alg: "md5" | "sha1" | "sha256", key: Bytes, data: Bytes): string;
    /** ecb、cbc 默认 pkcs7 填充；ctr、gcm 不填充；gcm 的密文末尾带 16 字节标签 */
    aes(o: {
      mode: "ecb" | "cbc" | "ctr" | "gcm";
      key: Bytes;
      iv?: Bytes;
      data: Bytes;
      decrypt?: boolean;
      padding?: "pkcs7" | "none";
      aad?: Bytes;
    }): Uint8Array;
    /** publicKey：PEM（PKIX 或 PKCS #1），或不带头尾的 base64 DER */
    rsa(o: { publicKey: string; data: Bytes; padding: "pkcs1" | "oaep-sha1" | "oaep-sha256" }): Uint8Array;
    /** n ≤ 1024 */
    randomBytes(n: number): Uint8Array;
    /** v4 */
    uuid(): string;
  };
  encoding: {
    hex(b: Bytes): string;
    fromHex(s: string): Uint8Array;
    base64(b: Bytes): string;
    /** 也接受 URL 安全的写法，有无填充都行 */
    fromBase64(s: string): Uint8Array;
    utf8(s: string): Uint8Array;
    fromUtf8(b: Uint8Array): string;
    /** 结果 ≤ 4 MiB */
    gunzip(b: Bytes): Uint8Array;
    /** zlib；raw 为 true 时是不带头的 deflate。结果 ≤ 4 MiB */
    inflate(b: Bytes, raw?: boolean): Uint8Array;
  };
  device(): { platform: "macos" | "windows" | "linux" | "tvos"; appVersion: string; locale: string };
  /** throw host.error("rateLimited", "请求太频繁", { retryAfter: 60 }) */
  error(code: ErrorCode, message?: string, extra?: { retryAfter?: number }): Error;
}

declare global {
  const host: Host;
}
