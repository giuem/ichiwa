// 网络：按请求里给的地址去取，测试用它试探沙箱。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export async function collection(req) {
  const init = req.filters?.init ? JSON.parse(req.filters.init) : {};
  if (req.filters?.flood) {
    const n = Number(req.filters.flood);
    const all = await Promise.all(Array.from({ length: n }, (_, i) => fetch(req.id.replace("{i}", i), init).then((r) => r.text())));
    return { title: String(all.length), items: [] };
  }
  if (req.filters?.settle) {
    const n = Number(req.filters.settle);
    const all = await Promise.allSettled(Array.from({ length: n }, (_, i) => fetch(req.id.replace("{i}", i), init).then((r) => r.text())));
    return { title: String(all.filter((r) => r.status === "fulfilled").length), items: [] };
  }
  if (req.filters?.caught) {
    try { await fetch(req.id, init); } catch {}
    throw new Error("接住网络错误之后出了别的错");
  }
  const res = await fetch(req.id, init);
  return { title: await res.text(), items: [] };
}
