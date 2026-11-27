// 一集插件示例：覆盖协议的全部函数。
// 用 `go run ./cmd/ichiwa-plugin call internal/plugin/testdata/example <函数> '<JSON>'` 调用。

import { shows, find, person, people, card, personCard, episodes, streams, img } from "./lib/catalog.js";

const PAGE = 2; // 每页张数，故意很小，好演示翻页

function notFound(what) {
  return host.error("notFound", `${what}不存在`);
}

function paginate(list, cursor) {
  const start = cursor ? Number(cursor) : 0;
  if (!Number.isInteger(start) || start < 0) throw host.error("plugin", "cursor 不对");
  const next = start + PAGE;
  return { items: list.slice(start, next).map(card), cursor: next < list.length ? String(next) : undefined };
}

export function navigation() {
  // 记一下打开过几次，演示 host.storage。
  const n = Number(host.storage.get("launches") ?? "0") + 1;
  host.storage.set("launches", String(n));
  console.log("navigation，第", n, "次");
  return {
    items: [
      { id: "home", title: "推荐", icon: "home" },
      { id: "rank", title: "排行", icon: "trophy" },
      { id: "filter", title: "筛选", icon: "filter" },
    ],
  };
}

const genres = ["喜剧", "奇幻", "纪录"];

export async function collection(req) {
  const id = req.id;
  const filters = req.filters ?? {};
  if (id === "home") {
    const page = paginate(shows, req.cursor);
    if (!req.cursor) {
      page.sections = [
        { title: "演员", items: people.map(personCard) },
        { title: "热门", layout: "ranked", collection: "rank", more: "rank" },
      ];
    }
    return page;
  }
  if (id === "rank") {
    const board = filters.board || "hot";
    const sorted = [...shows].sort((a, b) => board === "new" ? b.year - a.year : b.hot - a.hot);
    return {
      title: "排行",
      layout: "ranked",
      filters: [{ key: "board", title: "榜单", value: board, options: [{ value: "hot", title: "热度榜" }, { value: "new", title: "新片榜" }] }],
      items: sorted.map(card),
    };
  }
  if (id === "filter") {
    const genre = filters.genre ?? "";
    // 体裁变了，年份这一行跟着变：只列这个体裁有的年份。
    const pool = genre ? shows.filter((s) => s.genre === genre) : shows;
    const years = [...new Set(pool.map((s) => s.year))].sort();
    const year = years.includes(filters.year) ? filters.year : "";
    const rows = [
      { key: "genre", title: "体裁", value: genre, options: [{ title: "全部" }, ...genres.map((g) => ({ value: g, title: g })), { value: "broken", title: "出错" }] },
      { key: "year", title: "年份", value: year, options: [{ value: "", title: "全部" }, ...years.map((y) => ({ value: y, title: y }))] },
    ];
    if (genre === "broken") {
      // 筛选行拿到了、列表没拿到：返回筛选行，在 error 里说明（第 6 节）。
      return { filters: rows, error: { code: "network", message: "列表暂时取不到，请重试" } };
    }
    const list = pool.filter((s) => !year || s.year === year);
    const page = paginate(list, req.cursor);
    if (!req.cursor) page.filters = rows;
    return page;
  }
  if (id.startsWith("person:")) {
    const p = person(id.slice("person:".length));
    if (!p) throw notFound("演员");
    const works = shows.filter((s) => s.people.includes(p.id));
    return {
      header: { title: p.name, image: img(p.id, 200, 200), shape: "circle", description: p.bio, meta: [{ value: p.role }, { label: "部作品", value: String(works.length) }] },
      items: works.map(card),
    };
  }
  if (id.startsWith("related:")) {
    const s = find(id.slice("related:".length));
    if (!s) throw notFound("作品");
    return { items: shows.filter((x) => x.id !== s.id && x.genre === s.genre).concat(shows.filter((x) => x.genre !== s.genre)).map(card) };
  }
  throw notFound("集合");
}

export function media(req) {
  const s = find(req.id);
  if (!s) throw notFound("作品");
  const cached = host.cache.get(`media:${s.id}`);
  if (cached) return JSON.parse(cached);
  const out = {
    id: s.id,
    title: s.title,
    image: img(s.id),
    backdrop: img(`${s.id}-bg`, 640, 360),
    description: s.description,
    badges: card(s).badges,
    meta: [{ value: s.year }, { label: "热度", value: String(s.hot) }],
    tags: s.tags,
    people: s.people.map(person).map(personCard),
    aspect: s.aspect ?? "landscape",
    seasons: s.seasons?.map((x) => ({ ...x, name: x.id === "bunny" ? "大兔子" : undefined, current: x.id === s.id })),
    episodes: episodes(s),
    sections: [{ title: "相关推荐", collection: `related:${s.id}` }],
  };
  host.cache.set(`media:${s.id}`, JSON.stringify(out), 60);
  return out;
}

export async function play(req) {
  const s = find(req.mediaId);
  if (!s) throw notFound("作品");
  const ep = episodes(s).find((e) => e.id === req.episodeId);
  if (!ep) throw notFound("这一集");
  if (ep.lock) throw host.error("unavailable", "这一集要会员才能看");
  // prefer.hevc 为 false 时不给 HEVC；宿主也会再挑一遍。
  const list = streams(s).filter((x) => req.prefer?.hevc || x.codec !== "hevc");
  return { streams: list, expiresAt: Math.floor(Date.now() / 1000) + 3600 };
}

export function search(req) {
  const q = String(req.query ?? "").trim();
  if (!q) return { items: [] };
  const list = shows.filter((s) => s.title.includes(q) || s.description.includes(q) || s.tags.some((t) => t.includes(q)));
  return { title: `“${q}”`, ...paginate(list, req.cursor) };
}

export function suggest(req) {
  const q = String(req.query ?? "").trim();
  return { items: q ? shows.filter((s) => s.title.includes(q)).map((s) => s.title) : [] };
}

export function searchHome() {
  const hot = [...shows].sort((a, b) => b.hot - a.hot).map(card);
  return { sections: [{ title: "热搜榜", layout: "ranked", items: hot }, { title: "新片", items: [...hot].reverse() }] };
}

export function danmaku(req) {
  const s = find(req.mediaId);
  if (!s) throw notFound("作品");
  // 每页 5 秒的弹幕，两页；id 用 md5 演示 host.crypto。
  const from = req.cursor ? Number(req.cursor) : 0;
  const items = [];
  for (let t = from; t < from + 5; t += 0.5) {
    items.push({ id: host.crypto.md5(`${req.episodeId}:${t}`), text: t % 2 === 0 ? "好看！" : `第 ${t} 秒`, time: t });
  }
  return { items, cursor: from + 5 < 10 ? String(from + 5) : undefined };
}

export function refresh(req) {
  return { items: (req.ids ?? []).map(find).filter(Boolean).map(card) };
}
