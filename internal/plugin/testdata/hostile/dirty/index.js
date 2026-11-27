// 脏数据：各种不合格的项和字段，宿主应该丢掉坏的、留下好的。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection() {
  return {
    title: "  脏数据  ",
    layout: "spiral",
    items: [
      null, 42, "card", { kind: "media" }, { kind: "media", id: "", title: "空 ID" },
      { kind: "media", id: 7, title: "数字 ID", image: "javascript:alert(1)", badges: ["一二三四五六七八九十", 3, "b", "c"] },
      { kind: "media", id: "7", title: "重复" },
      { kind: "media", id: "ok", title: " 好的 ", image: "https://elsewhere.example/x.jpg", shape: "hexagon", note: 5 },
      { kind: "weird", id: "w", title: "未知种类" },
    ],
    cursor: "c".repeat(5000),
  };
}
export function navigation2() {}
