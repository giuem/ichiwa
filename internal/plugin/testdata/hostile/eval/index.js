// 动态代码：eval、new Function、动态 import 都要被拒。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export async function collection(req) {
  if (req.id === "eval") return { title: eval("'evaluated'") };
  if (req.id === "function") return { title: new Function("return 'constructed'")() };
  if (req.id === "indirect") return { title: (0, eval)("'evaluated'") };
  if (req.id === "async-function") { const AF = (async () => {}).constructor; return { title: await AF("return 'constructed'")() }; }
  if (req.id === "import") { const m = await import("./index.js"); return { title: typeof m }; }
  return { title: "none" };
}
