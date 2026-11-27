// 内存爆掉：一直往数组里塞字符串。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection() {
  const keep = [];
  for (let i = 0; ; i++) keep.push("x".repeat(1024) + i);
}
