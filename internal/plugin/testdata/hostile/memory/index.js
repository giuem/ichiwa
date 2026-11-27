// 内存爆掉：一直往数组里塞字符串；或者让宿主函数替它造大字符串。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection(req) {
  if (req.id === "hex" || req.id === "base64") {
    const u = new Uint8Array(4 << 20), a = [];
    for (let i = 0; i < 200; i++) a.push(host.encoding[req.id](u));
    return { n: a.length };
  }
  const keep = [];
  const s = "x".repeat(1024);
  for (let i = 0; ; i++) keep.push(s + i);
}
