// 超大返回：5 MiB 的 JSON。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection() {
  const items = [];
  for (let i = 0; i < 6000; i++) items.push({ kind: "media", id: "id" + i, title: "t".repeat(900) });
  return { items };
}
