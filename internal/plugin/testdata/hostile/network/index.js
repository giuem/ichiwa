// 网络：按请求里给的地址去取，测试用它试探沙箱。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export async function collection(req) {
  const init = req.filters?.init ? JSON.parse(req.filters.init) : {};
  const res = await fetch(req.id, init);
  return { title: await res.text(), items: [] };
}
