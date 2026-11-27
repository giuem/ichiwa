// 示例数据：几部开放授权的短片。真实插件在这里用 fetch 调平台的接口。

const img = (seed, w = 300, h = 400) => `https://picsum.photos/seed/${seed}/${w}/${h}`;
const clip = (name, codec, height) =>
  `https://test-videos.co.uk/vids/${name}/mp4/${codec}/${height}/${name[0].toUpperCase()}${name.slice(1)
    .replace("buckbunny", "_Buck_Bunny")}_${height}_10s_1MB.mp4`;

export const people = [
  { id: "p1", name: "大兔子", role: "主演", bio: "一只爱好和平的兔子。" },
  { id: "p2", name: "辛特尔", role: "主演", bio: "寻找小龙的少女。" },
  { id: "p3", name: "水母", role: "客串", bio: "在海里漂。" },
];

export const shows = [
  {
    id: "bunny", title: "大兔子", genre: "喜剧", year: "2008", hot: 98,
    description: "一只大兔子和三只捣蛋鬼的故事。", tags: ["喜剧", "动画"],
    people: ["p1", "p3"], video: "bigbuckbunny", episodes: 6, aspect: "landscape",
    seasons: [{ id: "bunny", title: "第一季" }, { id: "bunny2", title: "第二季" }, { id: "bunny3", title: "第三季", available: false, note: "明年上线" }],
  },
  {
    id: "bunny2", title: "大兔子 第二季", genre: "喜剧", year: "2009", hot: 80,
    description: "大兔子又回来了。", tags: ["喜剧"], people: ["p1"], video: "bigbuckbunny", episodes: 3,
    seasons: [{ id: "bunny", title: "第一季" }, { id: "bunny2", title: "第二季" }, { id: "bunny3", title: "第三季", available: false, note: "明年上线" }],
  },
  {
    id: "sintel", title: "辛特尔", genre: "奇幻", year: "2010", hot: 95,
    description: "少女辛特尔踏上寻找小龙的旅程。", tags: ["奇幻", "冒险"], people: ["p2"], video: "sintel", episodes: 4,
  },
  {
    id: "jellyfish", title: "水母", genre: "纪录", year: "2015", hot: 60,
    description: "一段水母在海里漂的影像。", tags: ["纪录"], people: ["p3"], video: "jellyfish", episodes: 2,
  },
  {
    id: "bipbop", title: "测试信号", genre: "纪录", year: "2016", hot: 30,
    description: "苹果的 HLS 示例流，用来试回环代理。", tags: ["测试"], people: [], hls: true, episodes: 1,
  },
];

export const find = (id) => shows.find((s) => s.id === id);
export const person = (id) => people.find((p) => p.id === id);

export function card(s) {
  return {
    kind: "media",
    id: s.id,
    title: s.title,
    subtitle: s.description,
    image: img(s.id),
    badges: s.episodes > 3 ? [`全${s.episodes}集`] : ["短片"],
    note: `热度 ${s.hot}`,
  };
}

export function personCard(p) {
  return { kind: "collection", id: `person:${p.id}`, title: p.name, subtitle: p.role, image: img(p.id, 200, 200), shape: "circle" };
}

export function episodes(s) {
  const out = [];
  for (let i = 1; i <= s.episodes; i++) {
    out.push({
      id: `${s.id}-${i}`,
      title: `第 ${i} 集`,
      duration: 10,
      image: img(`${s.id}-${i}`, 400, 225),
      // 最后一集演示锁：宿主显示锁，不播放。
      lock: s.episodes > 3 && i === s.episodes ? "vip" : null,
    });
  }
  return out;
}

export function streams(s) {
  if (s.hls) {
    return [{
      url: "https://devstreaming-cdn.apple.com/videos/streaming/examples/img_bipbop_adv_example_ts/master.m3u8",
      container: "hls",
      codec: "h264",
      width: 1920,
      height: 1080,
    }];
  }
  return [
    { url: clip(s.video, "h265", 720), container: "mp4", codec: "hevc", width: 1280, height: 720 },
    { url: clip(s.video, "h264", 720), container: "mp4", codec: "h264", width: 1280, height: 720,
      backupUrls: [clip(s.video, "h264", 360)] },
    { url: clip(s.video, "h264", 360), container: "mp4", codec: "h264", width: 640, height: 360 },
  ];
}

export { img };
